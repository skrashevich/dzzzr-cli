package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
)

// I1: a renewal that lands after a sign-out must not write the old credential
// back, or the sign-out is silently undone.
func TestLateRefreshDoesNotUndoASignOut(t *testing.T) {
	path := isolateCodexAuth(t)
	cred := &codexCredential{AccessToken: "old", RefreshToken: "r", AccountID: "a", ExpiresAt: time.Now().Add(time.Minute)}
	writeTestCodexCredential(t, cred)
	store := sharedCodexTokenStore(path, cred)
	release := make(chan struct{})
	started := make(chan struct{})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		close(started)
		<-release
		return &auth.AuthCredential{AccessToken: "renewed", RefreshToken: "r2", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	done := make(chan struct{})
	go func() { _, _, _ = store.tokenSource()(); close(done) }()
	<-started

	srv := httptest.NewServer(newTestHubKeepingEnv(t).newMux())
	defer srv.Close()
	if code := webDo(t, srv, http.MethodPost, "/api/v1/llm/codex/logout", "", nil); code != http.StatusOK {
		t.Fatalf("logout %d", code)
	}
	close(release)
	<-done
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a late renewal brought the signed-out credential back: %v", err)
	}
}

// I2: an organizer who never needed a model is not trapped in the wizard
// after upgrading; the model step is optional, the sign-in is not.
func TestWebOnboardingNotRequiredForASignedInUserWithoutAModel(t *testing.T) {
	_, base := newAdminEngine(t)
	hub := newAdminWebHub(t, base, true)
	srv := onboardingServer(t, hub)
	_, payload, raw := onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	if payload.Required {
		t.Fatalf("payload %s: a signed-in organizer without a model must not be forced through the wizard", raw)
	}
	if onboardingStepByID(t, payload, "llm").Done {
		t.Fatal("the model step must still report that no model is configured")
	}
}

// I3: a page on another site must not be able to change settings through the
// local server; the browser sends a text/plain POST without a preflight.
func TestCrossSiteWritesAreRefused(t *testing.T) {
	hub := newTestHub(t, nil)
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"foreign origin", map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"cross-site fetch", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"another local port", map[string]string{"Origin": "http://127.0.0.1:1", "Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"same origin", map[string]string{"Origin": srv.URL, "Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"no browser headers", nil, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/onboarding/reset", strings.NewReader(""))
			req.Header.Set("Content-Type", "text/plain")
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = res.Body.Close()
			if res.StatusCode != tc.want {
				t.Fatalf("status %d, want %d", res.StatusCode, tc.want)
			}
		})
	}
	// Reads stay open: the page itself is what issues them.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/onboarding", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET status %d", res.StatusCode)
	}
}

// I4: a key saved for one provider must not be sent to another one because
// only the address changed and the key field was left empty.
func TestWebLLMSettingsKeyDoesNotFollowANewHost(t *testing.T) {
	srv := newLLMSettingsTestServer(t)
	writeTestLLMSettings(t, llmSettings{AuthMethod: authMethodAPIKey, BaseURL: polzaBaseURL, APIKey: "polza-key", Model: "m"})

	status, _, raw := llmSettingsRequest(t, srv, http.MethodPut, `{"auth_method":"apikey","base_url":"https://openrouter.ai/api/v1","model":"m"}`)
	if status != http.StatusBadRequest || !strings.Contains(raw, "ключ") {
		t.Fatalf("status %d: %s", status, raw)
	}
	if s, _ := loadLLMSettings(); s.APIKey != "polza-key" || s.BaseURL != polzaBaseURL {
		t.Fatalf("a refused save changed the settings: %+v", s)
	}
	// A local server needs no key: the old one is dropped instead.
	status, _, raw = llmSettingsRequest(t, srv, http.MethodPut, `{"auth_method":"apikey","base_url":"http://127.0.0.1:8080/v1","model":"m"}`)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, raw)
	}
	if s, _ := loadLLMSettings(); s.APIKey != "" {
		t.Fatalf("the Polza key followed the address to a local server: %+v", s)
	}
	// Same host, empty key field: the key is kept, as the panel promises.
	writeTestLLMSettings(t, llmSettings{AuthMethod: authMethodAPIKey, APIKey: "or-key"})
	if status, _, raw := llmSettingsRequest(t, srv, http.MethodPut, `{"auth_method":"apikey","base_url":"https://openrouter.ai/api/v1","model":"x"}`); status != http.StatusOK {
		t.Fatalf("status %d: %s", status, raw)
	}
	if s, _ := loadLLMSettings(); s.APIKey != "or-key" {
		t.Fatalf("key lost on a same-host save: %+v", s)
	}
}

// newTestHubKeepingEnv is newTestHub without its own isolate: the caller has
// already pointed the configuration somewhere and wants the hub to use it.
func newTestHubKeepingEnv(t *testing.T) *webHub {
	t.Helper()
	cfg := &config{city: "moscow", security: "approve", stdin: strings.NewReader(""), stdout: io.Discard, stderr: io.Discard}
	return &webHub{cfg: cfg, client: newClient(cfg), store: newChatStore(t.TempDir()), sse: newSSEHub()}
}
