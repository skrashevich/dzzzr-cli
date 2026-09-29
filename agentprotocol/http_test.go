package agentprotocol

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPortableHTTPPreservesToolHistoryAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("wrong URL or authorization")
		}
		var body map[string]any
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(body)
		for _, want := range []string{`"tool_call_id":"call-1"`, `"arguments":"{\"path\":\"source.txt\"}"`, `"reasoning_content":"thinking"`, `"provider":{"order":["preferred"]}`} {
			if !strings.Contains(string(raw), want) {
				t.Errorf("missing %s: %s", want, raw)
			}
		}
		_ = json.MarshalWrite(w, map[string]any{"choices": []any{map[string]any{"finish_reason": "length", "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "call-2", "function": map[string]any{"name": "write", "arguments": "{\"incomplete\":"}}}}}}, "usage": map[string]any{"prompt_tokens": 30, "completion_tokens": 7, "total_tokens": 37}})
	}))
	defer server.Close()
	p := HTTPProvider{Endpoint: server.URL + "/v1", Key: "test-key", Model: "test", ExtraBody: map[string]any{"provider": map[string]any{"order": []string{"preferred"}}}}
	r, err := p.Chat(t.Context(), []Message{{Role: "assistant", ReasoningContent: "thinking", ToolCalls: []ToolCall{{ID: "call-1", Type: "function", Function: &FunctionCall{Name: "read", Arguments: `{"path":"source.txt"}`}}}}, {Role: "tool", Content: "source", ToolCallID: "call-1"}}, nil, "test", nil)
	if err != nil || r.FinishReason != "length" || r.Usage.TotalTokens != 37 || len(r.ToolCalls) != 1 {
		t.Fatal(r, err)
	}
}
