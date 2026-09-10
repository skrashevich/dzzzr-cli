package dzzzr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Credentials is the team's game access: the captain's site login and the
// numeric PIN the organizer issued for the current game. The engine reads them
// from HTTP Basic as "{city}_{captain}:{pin}".
type Credentials struct {
	Captain string `json:"captain"`
	Pin     string `json:"pin"`
}

// SetCredentials replaces the captain/PIN pair.
func (c *Client) SetCredentials(cr Credentials) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.creds = Credentials{Captain: strings.TrimSpace(cr.Captain), Pin: strings.TrimSpace(cr.Pin)}
}

// Credentials returns the captain/PIN pair.
func (c *Client) Credentials() Credentials {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.creds
}

// SetSession installs a site session token (the userToken from Login).
func (c *Client) SetSession(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session = strings.TrimSpace(token)
}

// Session returns the current site session token, empty when signed out.
func (c *Client) Session() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.session
}

// LoginName returns the site login remembered from the last successful Login.
func (c *Client) LoginName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.login
}

// SetAdminCredentials sets the organizer login and password.
func (c *Client) SetAdminCredentials(login, password string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.adminLogin, c.adminPassword = login, password
}

// HasAdminCredentials reports whether organizer credentials are set.
func (c *Client) HasAdminCredentials() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.adminLogin != ""
}

// AdminLogin returns the organizer login.
func (c *Client) AdminLogin() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.adminLogin
}

// LoginResponse is the reply of API/login.php.
type LoginResponse struct {
	Code      FlexInt `json:"code"`
	Error     string  `json:"error"`
	UserName  string  `json:"userName"`
	UserToken string  `json:"userToken"`
}

// OK reports whether the sign-in succeeded.
func (r *LoginResponse) OK() bool { return r != nil && r.Code == LoginOK && r.UserToken != "" }

// Login authenticates a site account through API/login.php. On success the
// session token is stored on the client and sent with every later request.
//
// The endpoint sits behind the API's Basic wall, which only checks that a
// header is present (see basicIdentity), so the login being attempted is used
// as the identity when no captain and PIN are configured. That is what a
// Classic player has: their teams carry no PIN.
func (c *Client) Login(ctx context.Context, login, password string) (*LoginResponse, error) {
	q := url.Values{"login": {login}, "password": {password}}
	req, err := c.newRequest(ctx, http.MethodGet, "API/login.php", q, nil, requestOptions{basic: true, basicLogin: login})
	if err != nil {
		return nil, err
	}
	status, _, body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		return nil, &AuthError{Kind: AuthBasic, Message: strings.TrimSpace(string(body))}
	}
	if status != http.StatusOK {
		return nil, &HTTPError{StatusCode: status, Context: "login"}
	}
	var resp LoginResponse
	body = trimEngineNoise(body)
	if isEmptyBody(body) {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "login", Err: errEmptyBody}
	}
	if err := decodeEngineJSON(body, &resp); err != nil {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "login", Err: err, Body: truncate(body)}
	}
	if resp.Code == 0 && resp.UserToken == "" {
		// The auth-error envelope from APIconst.php has only "error".
		return nil, &AuthError{Kind: AuthBasic, Message: resp.Error}
	}
	if resp.OK() {
		c.mu.Lock()
		c.session = resp.UserToken
		c.login = login
		c.mu.Unlock()
	}
	return &resp, nil
}

// savedSession is the on-disk form of a signed-in client.
type savedSession struct {
	Token         string `json:"token,omitempty"`
	Login         string `json:"login,omitempty"`
	Captain       string `json:"captain,omitempty"`
	Pin           string `json:"pin,omitempty"`
	AdminLogin    string `json:"admin_login,omitempty"`
	AdminPassword string `json:"admin_password,omitempty"`
}

// ExportSession serializes the session token and credentials so a later
// process can continue without signing in again. The output contains
// secrets (PIN, organizer password) and must be stored with owner-only
// permissions.
func (c *Client) ExportSession() ([]byte, error) {
	c.mu.RLock()
	s := savedSession{
		Token: c.session, Login: c.login,
		Captain: c.creds.Captain, Pin: c.creds.Pin,
		AdminLogin: c.adminLogin, AdminPassword: c.adminPassword,
	}
	c.mu.RUnlock()
	return json.Marshal(s)
}

// ImportSession restores what ExportSession produced. Fields absent from the
// data leave the current values untouched.
func (c *Client) ImportSession(data []byte) error {
	var s savedSession
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("dzzzr: import session: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if s.Token != "" {
		c.session = s.Token
	}
	if s.Login != "" {
		c.login = s.Login
	}
	if s.Captain != "" || s.Pin != "" {
		c.creds = Credentials{Captain: s.Captain, Pin: s.Pin}
	}
	if s.AdminLogin != "" {
		c.adminLogin, c.adminPassword = s.AdminLogin, s.AdminPassword
	}
	return nil
}

// Logout forgets the session token and the remembered login. Credentials and
// organizer settings stay.
func (c *Client) Logout() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session, c.login = "", ""
}

func truncate(b []byte) string {
	const n = 512
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
