package main

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// onboardingServer serves the wizard over a hub whose client talks to base —
// the player double, the organizer double, or nothing at all.
func onboardingServer(t *testing.T, hub *webHub) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(hub.newMux())
	t.Cleanup(srv.Close)
	return srv
}

// playerOnboardingHub is a hub on the player double, with nobody signed in.
func playerOnboardingHub(t *testing.T) *webHub {
	t.Helper()
	hub := newTestHub(t, nil)
	_, base := newEngine(t)
	hub.cfg.baseURL = base
	hub.client = dzzzr.New("moscow", dzzzr.WithBaseURL(base))
	return hub
}

func onboardingRequest(t *testing.T, srv *httptest.Server, method, path, body string) (int, onboardingStatusPayload, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var payload onboardingStatusPayload
	_ = json.Unmarshal(raw, &payload)
	return res.StatusCode, payload, string(raw)
}

func onboardingStepByID(t *testing.T, payload onboardingStatusPayload, id string) onboardingStep {
	t.Helper()
	for _, s := range payload.Steps {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no step %q in %+v", id, payload.Steps)
	return onboardingStep{}
}

func TestWebOnboardingRequiredOnACleanMachine(t *testing.T) {
	srv := onboardingServer(t, newTestHub(t, nil))
	status, payload, raw := onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	if status != http.StatusOK || !payload.Required || payload.Completed {
		t.Fatalf("status %d payload %s", status, raw)
	}
	if step := onboardingStepByID(t, payload, "llm"); step.Done {
		t.Fatalf("llm step done on a clean machine: %+v", step)
	}
	if step := onboardingStepByID(t, payload, "auth"); step.Done {
		t.Fatalf("auth step done on a clean machine: %+v", step)
	}
}

func TestWebOnboardingLLMStepFollowsTheTransport(t *testing.T) {
	srv := onboardingServer(t, newTestHub(t, nil))
	writeTestLLMSettings(t, llmSettings{APIKey: "sk-test", Model: "some/model"})
	_, payload, _ := onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	step := onboardingStepByID(t, payload, "llm")
	if !step.Done || !strings.Contains(step.Detail, "some/model") {
		t.Fatalf("llm step = %+v", step)
	}
}

func TestWebOnboardingCompleteAsPlayerPersists(t *testing.T) {
	hub := playerOnboardingHub(t)
	srv := onboardingServer(t, hub)
	status, payload, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete",
		`{"role":"player","login":"demo","password":"secret"}`)
	if status != http.StatusOK || !payload.Completed || payload.Required {
		t.Fatalf("status %d payload %s", status, raw)
	}
	if step := onboardingStepByID(t, payload, "auth"); !step.Done {
		t.Fatalf("auth step after a player login = %+v", step)
	}
	state, err := loadOnboardingState()
	if err != nil || !state.Completed || state.CompletedAt == "" {
		t.Fatalf("state = %+v, err = %v", state, err)
	}
	// The player login is worth as much as «dzzzr login»: the session is saved.
	path, _ := sessionPath("moscow")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session not saved: %v", err)
	}
}

func TestWebOnboardingCompleteAsOrganizer(t *testing.T) {
	_, base := newAdminEngine(t)
	hub := newAdminWebHub(t, base, false)
	srv := onboardingServer(t, hub)
	status, payload, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete",
		`{"role":"organizer","login":"org","password":"secret"}`)
	if status != http.StatusOK || !payload.Completed {
		t.Fatalf("status %d payload %s", status, raw)
	}
	if step := onboardingStepByID(t, payload, "auth"); !step.Done || !strings.Contains(step.Detail, "org") {
		t.Fatalf("auth step after an organizer login = %+v", step)
	}
}

func TestWebOnboardingWrongOrganizerPasswordKeepsThePrevious(t *testing.T) {
	_, base := newAdminEngine(t)
	hub := newAdminWebHub(t, base, true)
	srv := onboardingServer(t, hub)
	status, _, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete",
		`{"role":"organizer","login":"x","password":"wrong"}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("status %d: %s", status, raw)
	}
	if hub.cfg.adminLogin != "org" || hub.client.AdminLogin() != "org" {
		t.Fatalf("previous organizer lost: cfg %q client %q", hub.cfg.adminLogin, hub.client.AdminLogin())
	}
	if st, _ := loadOnboardingState(); st.Completed {
		t.Fatal("the wizard must not complete on a refused login")
	}
}

func TestWebOnboardingRejectsMissingCredentials(t *testing.T) {
	srv := onboardingServer(t, playerOnboardingHub(t))
	for _, body := range []string{
		`{"role":"player","login":"","password":"secret"}`,
		`{"role":"organizer","login":"org","password":""}`,
	} {
		if status, _, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete", body); status != http.StatusBadRequest {
			t.Fatalf("%s: status %d: %s", body, status, raw)
		}
	}
	if st, _ := loadOnboardingState(); st.Completed {
		t.Fatal("the wizard completed without credentials")
	}
}

func TestWebOnboardingRejectsUnknownRole(t *testing.T) {
	srv := onboardingServer(t, playerOnboardingHub(t))
	for _, body := range []string{
		`{"role":"judge","login":"a","password":"b"}`,
		`{"login":"a","password":"b"}`,
	} {
		status, _, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete", body)
		if status != http.StatusBadRequest || !strings.Contains(raw, "роль") {
			t.Fatalf("%s: status %d: %s", body, status, raw)
		}
	}
}

func TestWebOnboardingResetMakesItRequiredAgain(t *testing.T) {
	srv := onboardingServer(t, newTestHub(t, nil))
	if err := saveOnboardingState(onboardingState{Completed: true}); err != nil {
		t.Fatal(err)
	}
	if _, payload, _ := onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", ""); payload.Required {
		t.Fatal("a completed wizard is still required")
	}
	status, payload, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/reset", "")
	if status != http.StatusOK || !payload.Required {
		t.Fatalf("status %d payload %s", status, raw)
	}
}

func TestSaveOnboardingStateFilePermissions(t *testing.T) {
	dir := isolateLLMEnv(t)
	if err := saveOnboardingState(onboardingState{Completed: true}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "onboarding", "state.json")
	checkFileMode(t, path, 0o600)
}

// A player logout deletes the city's session file; everything the wizard
// configured must outlive it.
func TestWebOnboardingSurvivesPlayerLogout(t *testing.T) {
	hub := playerOnboardingHub(t)
	srv := onboardingServer(t, hub)
	if status, _, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete",
		`{"role":"player","login":"demo","password":"secret"}`); status != http.StatusOK {
		t.Fatalf("complete: %d %s", status, raw)
	}
	writeTestLLMSettings(t, llmSettings{APIKey: "k"})
	writeTestCodexCredential(t, &codexCredential{AccessToken: "a"})
	if code := webDo(t, srv, http.MethodPost, "/api/v1/auth/logout", "", nil); code != http.StatusOK {
		t.Fatalf("logout %d", code)
	}
	st, _ := loadOnboardingState()
	s, _ := loadLLMSettings()
	if !st.Completed || s.APIKey != "k" || !hasCodexCredential() {
		t.Fatalf("logout wiped state: onboarding %+v settings %+v codex %v", st, s, hasCodexCredential())
	}
}

// A state file that does not parse must not lock the user out of the wizard
// that would fix it.
func TestWebOnboardingBrokenStateStillAnswers(t *testing.T) {
	hub := newTestHub(t, nil)
	path := filepath.Join(os.Getenv("DZZZR_CONFIG_DIR"), "onboarding", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := onboardingServer(t, hub)
	status, payload, raw := onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	if status != http.StatusOK || !payload.Required || payload.Error == "" {
		t.Fatalf("status %d payload %s", status, raw)
	}
}

// A user who already runs dzzzr — a model from the environment and a session
// on disk — upgrades without being walked through setup they have done.
func TestWebOnboardingNotRequiredWhenAlreadyConfigured(t *testing.T) {
	hub := playerOnboardingHub(t)
	srv := onboardingServer(t, hub)
	t.Setenv("DZZZR_LLM_API_KEY", "sk-env")
	if code := webDo(t, srv, http.MethodPost, "/api/v1/auth/login", `{"login":"demo","password":"secret"}`, nil); code != http.StatusOK {
		t.Fatalf("login %d", code)
	}
	_, payload, raw := onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	if payload.Required || payload.Completed {
		t.Fatalf("payload %s: want not required, yet not completed either", raw)
	}
	// Half the setup is not enough.
	t.Setenv("DZZZR_LLM_API_KEY", "")
	if _, payload, raw := onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", ""); !payload.Required {
		t.Fatalf("payload %s: a missing model must still require the wizard", raw)
	}
}
