package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
)

func codexAuthFixture(t *testing.T, token, account string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	writeCodexFixture(t, path, token, account)
	return path
}

func writeCodexFixture(t *testing.T, path, token, account string) {
	t.Helper()
	if err := os.WriteFile(path, fmt.Appendf(nil, `{"auth_mode":"chatgpt","tokens":{"access_token":%q,"account_id":%q}}`, token, account), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCodexConfig(t *testing.T) {
	isolate(t)
	path := codexAuthFixture(t, "test-token", "test-account")
	t.Setenv("CODEX_HOME", filepath.Dir(path))
	t.Setenv("DZZR_LLM_PROVIDER", "codex")
	t.Setenv("OPENROUTER_MODEL", "anthropic/claude")
	t.Setenv("DZZR_LLM_API_KEY", "must-not-be-used")
	t.Setenv("DZZR_LLM_BASE_URL", "https://example.invalid")
	cfg, err := llmConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider == nil || cfg.APIKey != "" || cfg.BaseURL != "https://chatgpt.com/backend-api/codex" || cfg.Model == "anthropic/claude" {
		t.Fatalf("incorrect subscription config")
	}
	t.Setenv("DZZR_LLM_MODEL", "openai/gpt-5.4")
	cfg, err = llmConfig()
	if err != nil || cfg.Model != "gpt-5.4" {
		t.Fatalf("model = %q, err = %v", cfg.Model, err)
	}
	t.Setenv("DZZR_LLM_MODEL", "anthropic/claude")
	if _, err := llmConfig(); err == nil {
		t.Fatal("accepted incompatible model")
	}
	t.Setenv("DZZR_LLM_PROVIDER", "typo")
	if _, err := llmConfig(); err == nil {
		t.Fatal("accepted unknown provider")
	}
}

func TestCodexAuthErrors(t *testing.T) {
	expired := "header." + base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"exp":%d}`, time.Now().Add(-time.Hour).Unix())) + ".signature"
	for _, tc := range []struct{ name, body, want string }{
		{"missing", "", "прочитать"},
		{"malformed", `{"tokens":"secret-value"`, "JSON"},
		{"api-key", `{"auth_mode":"apikey","OPENAI_API_KEY":"secret-value"}`, "ChatGPT"},
		{"no-account", `{"tokens":{"access_token":"secret-value"}}`, "account_id"},
		{"expired", fmt.Sprintf(`{"tokens":{"access_token":%q,"account_id":"a"}}`, expired), "истёк"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			if tc.body != "" {
				if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, _, err := readCodexAuth(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

type codexTransport func(*http.Request) (*http.Response, error)

func (f codexTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCodexResponsesToolRoundTrip(t *testing.T) {
	isolate(t)
	path := codexAuthFixture(t, "first-token", "first-account")
	t.Setenv("DZZR_CODEX_AUTH_FILE", path)
	t.Setenv("DZZR_LLM_PROVIDER", "chatgpt")
	cfg, err := llmConfig()
	if err != nil {
		t.Fatal(err)
	}
	oldTransport := http.DefaultClient.Transport
	t.Cleanup(func() { http.DefaultClient.Transport = oldTransport })
	calls := 0
	http.DefaultClient.Transport = codexTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		token, account := "first-token", "first-account"
		if calls == 2 {
			token, account = "second-token", "second-account"
		}
		if r.URL.String() != "https://chatgpt.com/backend-api/codex/responses" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Chatgpt-Account-Id") != account {
			t.Error("wrong endpoint or credentials")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"store":false`) || !strings.Contains(string(body), "game_status") {
			t.Errorf("unexpected request: %s", body)
		}
		output := `[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"game_status","arguments":"{}","status":"completed"}]`
		if calls == 2 {
			if !strings.Contains(string(body), "function_call_output") || !strings.Contains(string(body), "level one") {
				t.Error("tool result lost")
			}
			output = `[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Done","annotations":[]}]}]`
		}
		sse := `data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","output":` + output + `,"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}` + "\n\n"
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse)), Request: r}, nil
	})
	messages := []providers.Message{{Role: "system", Content: "Use game tools"}, {Role: "user", Content: "status"}}
	defs := []providers.ToolDefinition{{Type: "function", Function: providers.ToolFunctionDefinition{Name: "game_status", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}}}
	first, err := cfg.Provider.Chat(t.Context(), messages, defs, cfg.Model, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Name != "game_status" {
		t.Fatalf("tool calls: %+v", first)
	}
	writeCodexFixture(t, path, "second-token", "second-account")
	messages = append(messages, providers.Message{Role: "assistant", ToolCalls: first.ToolCalls}, providers.Message{Role: "tool", ToolCallID: first.ToolCalls[0].ID, Content: "level one"})
	second, err := cfg.Provider.Chat(t.Context(), messages, defs, cfg.Model, nil)
	if err != nil || second.Content != "Done" || second.Usage.TotalTokens != 15 {
		t.Fatalf("response = %+v, error = %v", second, err)
	}
}

func TestCodexErrorDetail(t *testing.T) {
	isolate(t)
	t.Setenv("DZZR_CODEX_AUTH_FILE", codexAuthFixture(t, "token", "account"))
	t.Setenv("DZZR_LLM_PROVIDER", "codex")
	cfg, err := llmConfig()
	if err != nil {
		t.Fatal(err)
	}
	old := http.DefaultClient.Transport
	t.Cleanup(func() { http.DefaultClient.Transport = old })
	http.DefaultClient.Transport = codexTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"detail":"The 'gpt-5.6-astra' model is not supported when using Codex with a ChatGPT account."}`)), Request: r}, nil
	})
	_, err = cfg.Provider.Chat(t.Context(), []providers.Message{{Role: "user", Content: "hi"}}, nil, "gpt-5.6-astra", nil)
	if err == nil || !strings.Contains(err.Error(), "not supported when using Codex") {
		t.Fatalf("missing server detail: %v", err)
	}
}
