package dzzzr

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultHost       = "classic.dzzzr.ru"
	defaultUserAgent  = "dzzzr-cli"
	defaultTimeout    = 15 * time.Second
	defaultAdminDelay = 500 * time.Millisecond
	maxBodyBytes      = 4 << 20
)

// Client talks to one city of the Dozor Classic engine.
//
// A Client is safe for concurrent use. Credentials and the session token are
// guarded by a mutex because mobile callers change them while requests run.
type Client struct {
	city    string
	baseURL *url.URL

	httpClient    *http.Client
	rootTransport *http.Transport
	transport     *switchableTransport
	debugLog      func(string, ...any)

	// mu guards everything a caller can change while requests are in flight.
	// The mobile bindings do exactly that, and the setters used to race with
	// newRequest, ExportHAR and paceAdmin.
	mu            sync.RWMutex
	userAgent     string
	har           *HARRecorder
	harEnabled    bool
	creds         Credentials
	session       string
	login         string
	adminLogin    string
	adminPassword string
	adminDelay    time.Duration

	adminMu       sync.Mutex
	lastAdminPost time.Time
}

// switchableTransport lets the transport chain be rebuilt while requests are
// in flight; http.Client.Transport itself must never be mutated concurrently.
type switchableTransport struct {
	inner atomic.Pointer[http.RoundTripper]
}

func (t *switchableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt := t.inner.Load()
	if rt == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	return (*rt).RoundTrip(req)
}

// Option configures the Client.
type Option func(*Client)

// WithBaseURL points the client at a custom engine root, for example a local
// mock server: "http://127.0.0.1:18090/moscow/". The city segment must be part of
// the URL. A missing trailing slash is added.
func WithBaseURL(raw string) Option {
	return func(c *Client) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return
		}
		if !strings.HasSuffix(u.Path, "/") {
			u.Path += "/"
		}
		c.baseURL = u
	}
}

// WithHTTP uses plain HTTP for the default host.
func WithHTTP() Option {
	return func(c *Client) { c.baseURL.Scheme = "http" }
}

// WithInsecureTLS skips TLS certificate verification.
func WithInsecureTLS() Option {
	return func(c *Client) {
		c.rootTransport.TLSClientConfig.InsecureSkipVerify = true
	}
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithDebugLogger installs a printf-style logger that receives one line per
// request and response.
func WithDebugLogger(logf func(string, ...any)) Option {
	return func(c *Client) { c.debugLog = logf }
}

// WithHARRecording turns HTTP traffic capture on; see ExportHAR.
func WithHARRecording(enabled bool) Option {
	return func(c *Client) { c.harEnabled = enabled }
}

// WithCredentials sets the captain login and game PIN sent as HTTP Basic.
func WithCredentials(cr Credentials) Option {
	return func(c *Client) { c.creds = cr }
}

// WithSession installs a previously obtained site session token.
func WithSession(token string) Option {
	return func(c *Client) { c.session = token }
}

// WithAdminCredentials sets the organizer login and password for the
// administration area.
func WithAdminCredentials(login, password string) Option {
	return func(c *Client) {
		c.adminLogin = login
		c.adminPassword = password
	}
}

// WithAdminDelay sets the pause between consecutive admin POSTs. The engine
// is a shared PHP host with no rate limiting of its own; pacing keeps bulk
// edits from piling up. Zero disables the pause (use it with httptest).
func WithAdminDelay(d time.Duration) Option {
	return func(c *Client) { c.adminDelay = d }
}

// New creates a client for the given city (the path segment after the host,
// e.g. "moscow").
func New(city string, opts ...Option) *Client {
	city = strings.Trim(strings.TrimSpace(city), "/")
	c := &Client{
		city:       city,
		baseURL:    &url.URL{Scheme: "https", Host: defaultHost, Path: "/" + city + "/"},
		userAgent:  defaultUserAgent,
		adminDelay: defaultAdminDelay,
		transport:  &switchableTransport{},
	}
	c.rootTransport = &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	c.httpClient = &http.Client{
		Timeout:   defaultTimeout,
		Transport: c.transport,
		// The engine answers actions with a redirect whose query carries the
		// result code; following it would lose the code and, for most
		// actions, land on the HTML page instead of the JSON one.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, o := range opts {
		o(c)
	}
	if c.harEnabled && c.har == nil {
		c.har = NewHARRecorder()
	}
	c.rebuildTransport()
	return c
}

// rebuildTransport composes the transport chain from the current settings.
// It is called from New before the client is shared; later changes go through
// installTransport, which takes the recorder as an argument instead of
// reading the guarded fields.
func (c *Client) rebuildTransport() {
	c.installTransport(c.harEnabled, c.har)
}

func (c *Client) installTransport(record bool, rec *HARRecorder) {
	var rt http.RoundTripper = c.rootTransport
	if record && rec != nil {
		rt = rec.Wrap(rt)
	}
	c.transport.inner.Store(&rt)
}

// City returns the city segment the client was created for.
func (c *Client) City() string { return c.city }

// BaseURL returns the engine root including the city and a trailing slash.
func (c *Client) BaseURL() string { return c.baseURL.String() }

// SetUserAgent changes the User-Agent header at runtime.
func (c *Client) SetUserAgent(ua string) {
	if ua == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.userAgent = ua
}

// UserAgent returns the User-Agent header the client sends.
func (c *Client) UserAgent() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.userAgent
}

// SetHARRecordingEnabled toggles traffic capture at runtime.
func (c *Client) SetHARRecordingEnabled(enabled bool) {
	c.mu.Lock()
	c.harEnabled = enabled
	if enabled && c.har == nil {
		c.har = NewHARRecorder()
	}
	rec := c.har
	c.mu.Unlock()
	c.installTransport(enabled, rec)
}

// ExportHAR returns captured traffic as a HAR 1.2 document, or an empty log
// when recording was never enabled.
func (c *Client) ExportHAR() ([]byte, error) {
	c.mu.RLock()
	rec := c.har
	c.mu.RUnlock()
	if rec == nil {
		return NewHARRecorder().Export()
	}
	return rec.Export()
}

// AdminDelay returns the configured pause between admin POSTs.
func (c *Client) AdminDelay() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.adminDelay
}

// SetAdminDelay changes the pause between admin POSTs at runtime.
func (c *Client) SetAdminDelay(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.adminDelay = d
}

func (c *Client) debugf(format string, args ...any) {
	if c.debugLog != nil {
		c.debugLog(format, args...)
	}
}

// resolve joins a relative engine path ("go/", "API/login.php") with the base.
func (c *Client) resolve(path string, query url.Values) string {
	u := *c.baseURL
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + strings.TrimPrefix(path, "/")
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

// requestOptions tunes newRequest.
type requestOptions struct {
	basic       bool   // send the game identity as Basic
	session     bool   // add s=<token> to the query
	siteCookie  bool   // also send the token as the public site cookie
	pin         bool   // add pin=<pin> to the query
	admin       bool   // send organizer credentials as Basic
	requireAuth bool   // fail early when the needed credential is absent
	basicLogin  string // identity to fall back on before a login is remembered
	cp1251Form  bool   // encode the body as windows-1251 (the admin area)
}

// basicIdentity returns the HTTP Basic pair the API wall expects.
//
// The engine's APIconst.php only checks that a Basic header is present: its
// lookup of the team's application ends in "|| true", so any user and password
// pass. In Dozor Classic teams have no PIN at all (switchAction.php sets it to
// an empty string for the Classic project), so requiring one would lock out
// every Classic player. The captain and PIN are therefore used when the
// organizer issued them, and the site login with an empty password otherwise.
// Measured against classic.dzzzr.ru on 2026-09-08: no header gives 401, any
// header is accepted.
func (c *Client) basicIdentity(loginHint string) (user, pass string, ok bool) {
	c.mu.RLock()
	creds, login := c.creds, c.login
	c.mu.RUnlock()
	switch {
	case creds.Captain != "":
		return c.city + "_" + creds.Captain, creds.Pin, true
	case loginHint != "":
		return c.city + "_" + loginHint, "", true
	case login != "":
		return c.city + "_" + login, "", true
	}
	return "", "", false
}

func (c *Client) newRequest(ctx context.Context, method, path string, query, form url.Values, o requestOptions) (*http.Request, error) {
	c.mu.RLock()
	creds, session := c.creds, c.session
	adminLogin, adminPassword := c.adminLogin, c.adminPassword
	userAgent := c.userAgent
	c.mu.RUnlock()

	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	if o.session {
		if session == "" && o.requireAuth {
			return nil, &AuthError{Kind: AuthSession, Message: "no session token, call Login first"}
		}
		if session != "" {
			q.Set("s", session)
		}
	}
	if o.pin && creds.Pin != "" {
		q.Set("pin", creds.Pin)
	}

	var body io.Reader
	encodedForm := ""
	if form != nil {
		encodedForm = form.Encode()
		if o.cp1251Form {
			encodedForm = encodeFormCP1251(form)
		}
		body = strings.NewReader(encodedForm)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.resolve(path, q), body)
	if err != nil {
		return nil, fmt.Errorf("dzzzr: build request: %w", err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(encodedForm)), nil }
	}
	if o.siteCookie && session != "" {
		req.AddCookie(&http.Cookie{Name: "dozorSiteSession", Value: session})
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/html;q=0.9, */*;q=0.8")
	switch {
	case o.admin:
		if adminLogin == "" && o.requireAuth {
			return nil, &AuthError{Kind: AuthAdmin, Message: "no organizer credentials"}
		}
		if adminLogin != "" {
			req.SetBasicAuth(adminLogin, adminPassword)
		}
	case o.basic:
		user, pass, ok := c.basicIdentity(o.basicLogin)
		if !ok && o.requireAuth {
			return nil, &AuthError{Kind: AuthBasic, Message: "no game identity: sign in, or pass the captain login and PIN"}
		}
		if ok {
			req.SetBasicAuth(user, pass)
		}
	}
	return req, nil
}

// do performs the request and reads the whole body (bounded), so callers get
// bytes plus status and headers and never touch the connection.
func (c *Client) do(req *http.Request) (int, http.Header, []byte, error) {
	start := time.Now()
	c.debugf("→ %s %s", req.Method, redactURL(req.URL))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.debugf("✗ %s %s: %v", req.Method, redactURL(req.URL), err)
		return 0, nil, nil, fmt.Errorf("dzzzr: %s %s: %w", req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return resp.StatusCode, resp.Header.Clone(), nil, fmt.Errorf("dzzzr: read response: %w", err)
	}
	c.debugf("← %d %s (%d bytes, %s)%s", resp.StatusCode, req.URL.Path, len(body), time.Since(start).Round(time.Millisecond), redirectNote(resp))
	return resp.StatusCode, resp.Header.Clone(), body, nil
}

func redirectNote(resp *http.Response) string {
	if loc := resp.Header.Get("Location"); loc != "" {
		if u, err := url.Parse(loc); err == nil {
			return " → " + redactURL(u)
		}
		return " → " + loc
	}
	return ""
}

// redactURL hides the credentials a URL carries: the session token, the PIN,
// and the site account, whose name is worth as little to a reader of a debug
// log as it is worth to an attacker who has it.
func redactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	cp := *u
	q := cp.Query()
	changed := false
	for _, k := range []string{"s", "pin", "password", "login"} {
		if q.Has(k) {
			q.Set(k, "***")
			changed = true
		}
	}
	if changed {
		cp.RawQuery = q.Encode()
	}
	return cp.String()
}

// looksLikeHTML reports whether a body is an HTML document rather than JSON.
func looksLikeHTML(body []byte) bool {
	t := bytes.TrimSpace(body)
	return len(t) > 0 && t[0] == '<'
}
