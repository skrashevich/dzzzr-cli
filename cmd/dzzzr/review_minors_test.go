package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
)

// M1: a sign-in cancelled (or timed out) while the code was being exchanged
// must not be saved behind the user's back.
func TestCodexSignInCancelledDuringExchangeIsNotSaved(t *testing.T) {
	issuer := codexSignedInIssuer()
	srv, manager := newCodexTestServer(t, issuer)
	flow, _ := startCodexLogin(t, srv, manager, `{"no_browser":true}`)
	manager.exchange = func(cfg auth.OAuthProviderConfig, code, verifier, redirect string) (*auth.AuthCredential, error) {
		flow.finish("", errors.New("вход отменён"))
		return issuer.exchange(cfg, code, verifier, redirect)
	}
	if _, err := manager.submitCode(flow.ID, "code-1"); err == nil {
		t.Fatal("a cancelled sign-in reported success")
	}
	if hasCodexCredential() {
		t.Fatal("a sign-in cancelled mid-exchange was saved")
	}
	if got := codexFlowStatus(t, srv, flow.ID)["status"]; got != codexLoginError {
		t.Fatalf("status = %v", got)
	}
}

// M1: the redirect and a pasted code racing each other: only one exchange
// runs, and the loser does not record an error over the winner.
func TestCodexSignInSecondCompletionWaitsItsTurn(t *testing.T) {
	issuer := codexSignedInIssuer()
	srv, manager := newCodexTestServer(t, issuer)
	flow, _ := startCodexLogin(t, srv, manager, `{"no_browser":true}`)
	started, release := make(chan struct{}), make(chan struct{})
	manager.exchange = func(cfg auth.OAuthProviderConfig, code, verifier, redirect string) (*auth.AuthCredential, error) {
		close(started)
		<-release
		return issuer.exchange(cfg, code, verifier, redirect)
	}
	done := make(chan error, 1)
	go func() { _, err := manager.submitCode(flow.ID, "code-1"); done <- err }()
	<-started
	if _, err := manager.submitCode(flow.ID, "code-2"); err == nil {
		t.Fatal("a second completion ran while the first was exchanging")
	}
	if got := codexFlowStatus(t, srv, flow.ID)["status"]; got != codexLoginPending {
		t.Fatalf("the loser recorded %v over a sign-in still in progress", got)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := codexFlowStatus(t, srv, flow.ID)["status"]; got != codexLoginSuccess || !hasCodexCredential() {
		t.Fatalf("status = %v, signed in = %v", got, hasCodexCredential())
	}
}

// M2: on the ChatGPT subscription the panel shows the model the subscription
// uses, and OPENROUTER_MODEL — which it ignores — is not reported as an
// override.
func TestWebLLMSettingsCodexShowsTheSubscriptionModel(t *testing.T) {
	srv := newLLMSettingsTestServer(t)
	useCodexCLIFixture(t)
	t.Setenv("DZZZR_LLM_PROVIDER", "codex")
	t.Setenv("OPENROUTER_MODEL", "anthropic/claude")
	_, payload, raw := llmSettingsRequest(t, srv, http.MethodGet, "")
	for _, o := range payload.EnvOverrides {
		if o.Field == "model" {
			t.Fatalf("OPENROUTER_MODEL reported as overriding the subscription: %s", raw)
		}
	}
	if payload.Effective.Model.Value != payload.Agent.Model || payload.Agent.Model == "" {
		t.Fatalf("effective model %q, agent model %q", payload.Effective.Model.Value, payload.Agent.Model)
	}
}

// M3: a settings file that does not parse is reported by the wizard's own
// status, so the model step can say so before the user clicks on.
func TestWebOnboardingReportsABrokenSettingsFile(t *testing.T) {
	hub := newTestHub(t, nil)
	path := mustLLMSettingsFile(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := onboardingServer(t, hub)
	_, payload, raw := onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	if !strings.Contains(payload.Error, path) {
		t.Fatalf("payload %s: want the settings file named in error", raw)
	}
}

// M4: DZZZR_CODEX_AUTH_FILE is the Codex CLI file the agent uses, so the
// status reports that one.
func TestCodexStatusHonoursTheAuthFileVariable(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	file := codexAuthFixture(t, "cli-token", "cli-account")
	t.Setenv("DZZZR_CODEX_AUTH_FILE", file)
	status := codexStatusForWeb()
	if !status.CLISignIn || status.CLIPath != file {
		t.Fatalf("status = %+v", status)
	}
}

// M5: a damaged sign-in of dzzzr's own is reported, not mistaken for no
// sign-in at all.
func TestResolveLLMConfigReportsABrokenOwnSignIn(t *testing.T) {
	path := isolateCodexAuth(t)
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := resolveLLMConfig()
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v, want the broken sign-in named", err)
	}
}

// M6: with no sign-in anywhere the error says which Codex CLI file was looked
// for.
func TestCodexWithoutASignInNamesTheFileItLookedFor(t *testing.T) {
	isolateCodexAuth(t)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("DZZZR_LLM_PROVIDER", "codex")
	_, err := resolveLLMConfig()
	if err == nil || !strings.Contains(err.Error(), filepath.Join(home, "auth.json")) {
		t.Fatalf("err = %v", err)
	}
}

var _ = time.Second
