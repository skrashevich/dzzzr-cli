package agentloop

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// ExtraBody carries provider-specific request fields — Polza's upstream
// preferences — into every chat completion.
func TestExtraBodyReachesTheRequest(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	p := newProvider(Config{
		APIKey:    "k",
		BaseURL:   srv.URL,
		ExtraBody: map[string]any{"provider": map[string]any{"sort": "throughput"}},
	})
	if _, err := p.Chat(t.Context(), []providers.Message{{Role: "user", Content: "hi"}}, nil, "m", nil); err != nil {
		t.Fatal(err)
	}
	provider, _ := got["provider"].(map[string]any)
	if provider["sort"] != "throughput" {
		t.Fatalf("request body = %v", got)
	}
}
