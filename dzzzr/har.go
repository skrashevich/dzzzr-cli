package dzzzr

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// HARRecorder captures HTTP traffic in HAR 1.2 form. It is a RoundTripper
// wrapper; every request through the wrapped transport becomes one entry.
//
// Secrets are redacted where the recorder can recognize them: the session
// token and the PIN in URLs, the Authorization header, and the userToken a
// sign-in reply carries. A capture is still the team's game in full — task
// texts, codes it has found, the organizer's messages — so read one before
// handing it to anybody.
type HARRecorder struct {
	mu      sync.Mutex
	entries []harEntry
}

// NewHARRecorder creates an empty recorder.
func NewHARRecorder() *HARRecorder { return &HARRecorder{} }

type harLog struct {
	Log struct {
		Version string     `json:"version"`
		Creator harCreator `json:"creator"`
		Entries []harEntry `json:"entries"`
	} `json:"log"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}    `json:"cache"`
	Timings         harTimings  `json:"timings"`
}

type harTimings struct {
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
}

type harNV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harRequest struct {
	Method      string       `json:"method"`
	URL         string       `json:"url"`
	HTTPVersion string       `json:"httpVersion"`
	Cookies     []harNV      `json:"cookies"`
	Headers     []harNV      `json:"headers"`
	QueryString []harNV      `json:"queryString"`
	PostData    *harPostData `json:"postData,omitempty"`
	HeadersSize int          `json:"headersSize"`
	BodySize    int          `json:"bodySize"`
}

type harPostData struct {
	MimeType string  `json:"mimeType"`
	Params   []harNV `json:"params,omitempty"`
	Text     string  `json:"text"`
}

type harResponse struct {
	Status      int        `json:"status"`
	StatusText  string     `json:"statusText"`
	HTTPVersion string     `json:"httpVersion"`
	Cookies     []harNV    `json:"cookies"`
	Headers     []harNV    `json:"headers"`
	Content     harContent `json:"content"`
	RedirectURL string     `json:"redirectURL"`
	HeadersSize int        `json:"headersSize"`
	BodySize    int        `json:"bodySize"`
}

type harContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

type harTransport struct {
	rec  *HARRecorder
	next http.RoundTripper
}

// Wrap returns a RoundTripper that records through next.
func (r *HARRecorder) Wrap(next http.RoundTripper) http.RoundTripper {
	return &harTransport{rec: r, next: next}
}

func (t *harTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	var reqBody []byte
	if req.Body != nil && req.Body != http.NoBody {
		reqBody, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	// The body is bounded like the client's own reads: a capture must not be
	// able to hold more of a runaway response than the client itself would.
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	e := harEntry{
		StartedDateTime: start.UTC().Format(time.RFC3339Nano),
		Time:            float64(time.Since(start).Microseconds()) / 1000,
	}
	e.Timings.Wait = e.Time
	e.Request = harRequest{
		Method: req.Method, URL: redactURL(req.URL), HTTPVersion: req.Proto,
		Cookies: []harNV{}, Headers: headersNV(req.Header, true), QueryString: []harNV{},
		HeadersSize: -1, BodySize: len(reqBody),
	}
	for k, vs := range req.URL.Query() {
		for _, v := range vs {
			if k == "s" || k == "pin" || k == "password" || k == "login" {
				v = "***"
			}
			e.Request.QueryString = append(e.Request.QueryString, harNV{k, v})
		}
	}
	if len(reqBody) > 0 {
		e.Request.PostData = &harPostData{MimeType: req.Header.Get("Content-Type"), Text: string(reqBody)}
	}
	e.Response = harResponse{
		Status: resp.StatusCode, StatusText: http.StatusText(resp.StatusCode), HTTPVersion: resp.Proto,
		Cookies: []harNV{}, Headers: headersNV(resp.Header, false),
		Content:     harContent{Size: len(respBody), MimeType: resp.Header.Get("Content-Type"), Text: redactBody(respBody)},
		RedirectURL: resp.Header.Get("Location"), HeadersSize: -1, BodySize: len(respBody),
	}
	t.rec.mu.Lock()
	t.rec.entries = append(t.rec.entries, e)
	t.rec.mu.Unlock()
	return resp, nil
}

// tokenRe finds the session token in a sign-in reply. API/login.php builds
// its JSON by hand, so the field is matched textually rather than decoded.
var tokenRe = regexp.MustCompile(`("userToken"\s*:\s*")([^"]*)(")`)

// redactBody removes the session token from a captured reply. It is the one
// secret the engine puts in a body, and it is enough to play as the team.
func redactBody(body []byte) string {
	return tokenRe.ReplaceAllString(string(body), "${1}***${3}")
}

func headersNV(h http.Header, redactAuth bool) []harNV {
	out := make([]harNV, 0, len(h))
	for k, vs := range h {
		for _, v := range vs {
			if redactAuth && strings.EqualFold(k, "Authorization") {
				v = "Basic ***"
			}
			if strings.EqualFold(k, "Cookie") || strings.EqualFold(k, "Set-Cookie") {
				v = "***"
			}
			out = append(out, harNV{k, v})
		}
	}
	return out
}

// Len returns the number of captured entries.
func (r *HARRecorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

// Clear drops captured entries.
func (r *HARRecorder) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = nil
}

// Export renders the capture as a HAR 1.2 JSON document.
func (r *HARRecorder) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var doc harLog
	doc.Log.Version = "1.2"
	doc.Log.Creator = harCreator{Name: defaultUserAgent, Version: "1"}
	doc.Log.Entries = append([]harEntry{}, r.entries...)
	if doc.Log.Entries == nil {
		doc.Log.Entries = []harEntry{}
	}
	return json.MarshalIndent(doc, "", "  ")
}
