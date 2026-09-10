package agentloop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
)

func TestTruncatedMappingGetsOneSmallerResponseRetry(t *testing.T) {
	for _, recover := range []bool{true, false} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			calls := 0
			var defs []providers.ToolDefinition
			if err := json.Unmarshal([]byte(`[{"type":"function","function":{"name":"extract_pdf"}}]`), &defs); err != nil {
				t.Fatal(err)
			}
			p := &pdfImportProvider{chat: func(messages []providers.Message, _ []providers.ToolDefinition) (*providers.LLMResponse, error) {
				calls++
				if calls == 2 {
					last := messages[len(messages)-1]
					if last.Role != "user" || !strings.Contains(last.Content, "ONE SMALL structural mapping part") {
						t.Fatal("retry must change instructions instead of repeating oversized generation")
					}
					if recover {
						return &providers.LLMResponse{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{ID: "complete", Name: "save_local_json"}}}, nil
					}
				}
				return &providers.LLMResponse{FinishReason: "truncated", ToolCalls: []providers.ToolCall{{ID: "incomplete", Name: "save_local_json"}}}, nil
			}}
			o := &observer{delegate: p, stats: &stats{}}
			messages := []providers.Message{
				{Role: "user", Content: "Export PDF"},
				{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "index", Name: "index_pdf"}}},
				{Role: "tool", ToolCallID: "index", Content: "{}"},
			}
			response, err := o.Chat(t.Context(), messages, defs, "test", nil)
			if calls != 2 {
				t.Fatalf("expected one retry, got %d calls", calls)
			}
			if recover {
				if err != nil || response == nil || response.ToolCalls[0].ID != "complete" {
					t.Fatalf("complete smaller response not recovered: %+v %v", response, err)
				}
			} else if err == nil || response != nil {
				t.Fatalf("incomplete tool call reached executor: %+v %v", response, err)
			}
		})
	}
}

type stalledProvider struct{ calls int }

func (*stalledProvider) GetDefaultModel() string { return "stalled" }
func (p *stalledProvider) Chat(ctx context.Context, _ []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]any) (*providers.LLMResponse, error) {
	p.calls++
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestProviderDeadlineStopsWithoutThreeLongRetries(t *testing.T) {
	for _, withStatus := range []bool{false, true} {
		p := &stalledProvider{}
		o := &observer{delegate: p, stats: &stats{}, requestTimeout: 20 * time.Millisecond}
		if withStatus {
			o.cb.OnStatus = func(string, string) {}
		}
		_, err := o.Chat(t.Context(), nil, nil, "test", nil)
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "DZZZR_LLM_REQUEST_TIMEOUT_SECONDS") {
			t.Fatalf("missing timeout diagnostic: %v", err)
		}
		if p.calls != 1 {
			t.Fatalf("timed-out generation retried %d times", p.calls)
		}
	}
}

func TestIncompleteProviderResponseNeverExecutesTools(t *testing.T) {
	for _, reason := range []string{"length", "truncated", "max_tokens", "content_filter", "safety", "canceled"} {
		t.Run(reason, func(t *testing.T) {
			p := &pdfImportProvider{chat: func([]providers.Message, []providers.ToolDefinition) (*providers.LLMResponse, error) {
				return &providers.LLMResponse{FinishReason: reason, ToolCalls: []providers.ToolCall{{ID: "partial", Name: "save_local_json", Arguments: map[string]any{"path": "partial.json", "content": `{"levels":[]}`}}}}, nil
			}}
			o := &observer{delegate: p, stats: &stats{}}
			response, err := o.Chat(t.Context(), nil, nil, "test", nil)
			if err == nil || response != nil {
				t.Fatalf("incomplete response reached tool loop: %v %v", response, err)
			}
		})
	}
}

func TestRunRetriesAbortedProviderResponseWithoutReplayingTools(t *testing.T) {
	previousDelay := retryDelay
	retryDelay = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { retryDelay = previousDelay })
	for _, recover := range []bool{true, false} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			calls := 0
			echo := &echoTool{}
			var history string
			provider := &pdfImportProvider{chat: func(messages []providers.Message, _ []providers.ToolDefinition) (*providers.LLMResponse, error) {
				calls++
				if calls >= 2 && calls <= 4 {
					raw, err := json.Marshal(messages)
					if err != nil {
						t.Fatal(err)
					}
					if calls == 2 {
						history = string(raw)
					} else if string(raw) != history {
						t.Fatal("retry lost successful history or included aborted tool calls")
					}
					if !recover || calls < 4 {
						return &providers.LLMResponse{FinishReason: "error", ToolCalls: []providers.ToolCall{{ID: "aborted", Name: "echo", Arguments: map[string]any{"text": "must not execute"}}}}, nil
					}
				}
				if calls == 1 || calls == 4 {
					return &providers.LLMResponse{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{ID: fmt.Sprint(calls), Name: "echo", Arguments: map[string]any{"text": "complete"}}}}, nil
				}
				return &providers.LLMResponse{FinishReason: "stop", Content: "done"}, nil
			}}
			_, err := Run(t.Context(), Config{Model: "fake", Provider: provider}, &RunInput{Extra: []Tool{echo}}, Callbacks{})
			if recover {
				if err != nil || calls != 5 || echo.calls != 2 {
					t.Fatalf("recovery failed: calls=%d tool calls=%d err=%v", calls, echo.calls, err)
				}
			} else if err == nil || calls != 4 || echo.calls != 1 || !strings.Contains(err.Error(), "finish_reason=error") {
				t.Fatalf("retry bound or tool safety failed: calls=%d tool calls=%d err=%v", calls, echo.calls, err)
			}
		})
	}
}

func TestHTTPProviderNormalizesLengthBeforeObserver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"partial","type":"function","function":{"name":"save_local_json","arguments":"{\"path\":\"partial.json\",\"content\":\"{}\"}"}}]},"finish_reason":"length"}]}`)
	}))
	defer server.Close()
	o := &observer{delegate: newProvider(Config{BaseURL: server.URL}), stats: &stats{}}
	response, err := o.Chat(t.Context(), []providers.Message{{Role: "user", Content: "save"}}, nil, "test", nil)
	if err == nil || response != nil {
		t.Fatalf("normalized incomplete response reached tools: response=%+v error=%v", response, err)
	}
}

func TestEmptyHTTPResponseRetriesInsteadOfFinishing(t *testing.T) {
	for _, recover := range []bool{true, false} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if recover && calls == 2 {
					_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"complete"},"finish_reason":"stop"}]}`)
					return
				}
				_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()
			o := &observer{delegate: newProvider(Config{BaseURL: server.URL}), stats: &stats{}}
			response, err := o.Chat(t.Context(), nil, nil, "test", nil)
			if recover {
				if err != nil || response == nil || response.Content != "complete" || calls != 2 {
					t.Fatalf("empty response not recovered: %+v %v calls=%d", response, err, calls)
				}
			} else if !errors.Is(err, errEmptyModelResponse) || response != nil || calls != maxAttempts {
				t.Fatalf("empty response reported success or unbounded retry: %+v %v calls=%d", response, err, calls)
			}
		})
	}
}

func TestHTTPProgressSeparatesHeadersFromCompleteBody(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"complete"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	var mu sync.Mutex
	var statuses []string
	firstByte := make(chan struct{})
	var once sync.Once
	o := &observer{delegate: newProvider(Config{BaseURL: server.URL}), stats: &stats{}, requestTimeout: time.Second, cb: Callbacks{OnStatus: func(_ string, message string) {
		mu.Lock()
		statuses = append(statuses, message)
		mu.Unlock()
		if strings.Contains(message, "Начало HTTP-ответа") {
			once.Do(func() { close(firstByte) })
		}
	}}}
	done := make(chan error, 1)
	go func() {
		response, err := o.Chat(t.Context(), []providers.Message{{Role: "user", Content: "test"}}, nil, "test", nil)
		if err == nil && response.Content != "complete" {
			err = errors.New("body incomplete")
		}
		done <- err
	}()
	<-started
	select {
	case <-firstByte:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("missing response-header diagnostic")
	}
	select {
	case err := <-done:
		close(release)
		t.Fatalf("headers treated as complete response: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(statuses, "\n"), "finish_reason=\"stop\"") {
		t.Fatal("missing completion diagnostic")
	}
}
