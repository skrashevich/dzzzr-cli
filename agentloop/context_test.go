package agentloop

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/providers"
)

func TestBoundToolHistoryPreservesConversationAndCallIDs(t *testing.T) {
	messages := []providers.Message{{Role: "system", Content: "instructions"}, {Role: "user", Content: "find my games"}}
	for range 15 {
		messages = append(messages, providers.Message{Role: "tool", Content: strings.Repeat("статистика ", 5000), ToolCallID: "call"})
	}
	out, err := boundToolHistory(messages, 0)
	if err != nil {
		t.Fatal(err)
	}
	size := 0
	for _, m := range out {
		size += len(m.Content)
	}
	if size > 60000 {
		t.Fatalf("unbounded history: %d", size)
	}
	if out[0].Content != "instructions" || out[1].Content != "find my games" {
		t.Fatal("lost instructions")
	}
	if len(out) != len(messages) || out[2].ToolCallID != "call" {
		t.Fatal("broken tool protocol")
	}
	if !strings.Contains(out[2].Content, "omitted") {
		t.Fatal("omission not disclosed")
	}
	if messages[2].Content != strings.Repeat("статистика ", 5000) {
		t.Fatal("original transcript mutated")
	}
}

func TestObserverRetainsAllPDFPages(t *testing.T) {
	p := &contextCheckingProvider{fakeProvider: fakeProvider{turns: []turn{{content: "saved"}}}}
	o := &observer{delegate: p, stats: &stats{}}
	var messages []providers.Message
	for page := 1; page <= 72; page += 10 {
		id := fmt.Sprintf("page-%d", page)
		messages = append(messages,
			providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: id, Name: "read_pdf"}}},
			providers.Message{Role: "tool", ToolCallID: id, Content: fmt.Sprintf(`{"start_page":%d,"text":"%s"}`, page, strings.Repeat("сценарий ", 1500))},
		)
	}
	if _, err := o.Chat(t.Context(), messages, nil, "fake", nil); err != nil {
		t.Fatal(err)
	}
	for i := range messages {
		if p.seen[i].Content != messages[i].Content {
			t.Fatalf("PDF pages lost at %s: original %d bytes, sent %d", messages[i].ToolCallID, len(messages[i].Content), len(p.seen[i].Content))
		}
	}
}

func TestSourceContextOverflowStopsBeforeProvider(t *testing.T) {
	for _, name := range []string{"read_pdf", "read_local_file"} {
		t.Run(name, func(t *testing.T) {
			p := &contextCheckingProvider{fakeProvider: fakeProvider{turns: []turn{{content: "must not run"}}}}
			o := &observer{delegate: p, stats: &stats{}, sourceContextBytes: 100}
			messages := []providers.Message{
				{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "source", Function: &providers.FunctionCall{Name: name}}}},
				{Role: "tool", ToolCallID: "source", Content: strings.Repeat("x", 101)},
			}
			if _, err := o.Chat(t.Context(), messages, nil, "fake", nil); err == nil || !strings.Contains(err.Error(), "DZZR_LLM_SOURCE_CONTEXT_BYTES") {
				t.Fatalf("expected actionable overflow, got %v", err)
			}
			if p.seen != nil {
				t.Fatal("incomplete source sent to model")
			}
			if messages[1].Content != strings.Repeat("x", 101) {
				t.Fatal("source mutated")
			}
			o.sourceContextBytes = 101
			if _, err := o.Chat(t.Context(), messages, nil, "fake", nil); err != nil {
				t.Fatal(err)
			}
			if p.seen[1].Content != messages[1].Content {
				t.Fatal("source truncated at budget boundary")
			}
		})
	}
}

func TestBoundToolHistoryCompactsRepeatedSourceReads(t *testing.T) {
	const budget = 4000
	body := strings.Repeat("сценарий", budget/32)
	var messages []providers.Message
	for i := range 3 {
		id := fmt.Sprintf("read-%d", i)
		messages = append(messages,
			providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: id, Function: &providers.FunctionCall{Name: "read_local_file", Arguments: `{"path":"/tmp/scenario.txt"}`}}}},
			providers.Message{Role: "tool", ToolCallID: id, Content: body},
		)
	}
	out, err := boundToolHistory(messages, budget)
	if err != nil {
		t.Fatal(err)
	}
	if out[1].Content != body {
		t.Fatal("first read lost")
	}
	for _, i := range []int{3, 5} {
		if !strings.Contains(out[i].Content, "omitted") || len(out[i].Content) >= len(body) {
			t.Fatalf("duplicate at %d not compacted: %d bytes", i, len(out[i].Content))
		}
		if out[i].Role != "tool" || out[i].ToolCallID != messages[i].ToolCallID {
			t.Fatal("broken tool protocol")
		}
	}
	if messages[3].Content != body {
		t.Fatal("original transcript mutated")
	}
}

func TestBoundToolHistoryStillReportsOversizedUniqueSources(t *testing.T) {
	const budget = 4000
	messages := []providers.Message{{Role: "assistant", ToolCalls: []providers.ToolCall{
		{ID: "a", Function: &providers.FunctionCall{Name: "read_local_file", Arguments: `{"path":"/tmp/a.txt"}`}},
		{ID: "b", Function: &providers.FunctionCall{Name: "read_local_file", Arguments: `{"path":"/tmp/b.txt"}`}},
	}}}
	messages = append(messages,
		providers.Message{Role: "tool", ToolCallID: "a", Content: strings.Repeat("a", budget*7/10)},
		providers.Message{Role: "tool", ToolCallID: "b", Content: strings.Repeat("b", budget*7/10)},
	)
	if _, err := boundToolHistory(messages, budget); err == nil || !strings.Contains(err.Error(), "DZZR_LLM_SOURCE_CONTEXT_BYTES") {
		t.Fatalf("expected actionable overflow, got %v", err)
	}
}

func TestBoundToolHistoryKeepsWindowedReadsOfOneSource(t *testing.T) {
	const budget = 4000
	var messages []providers.Message
	for window := range 4 {
		id := fmt.Sprintf("window-%d", window)
		args := fmt.Sprintf(`{"path":"/tmp/scenario.pdf","start_page":%d,"end_page":%d}`, window*10+1, window*10+10)
		messages = append(messages,
			providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: id, Function: &providers.FunctionCall{Name: "read_pdf", Arguments: args}}}},
			providers.Message{Role: "tool", ToolCallID: id, Content: fmt.Sprintf("страница %d: %s", window, strings.Repeat("текст", 200))},
		)
	}
	if _, err := boundToolHistory(messages, budget); err == nil || !strings.Contains(err.Error(), "DZZR_LLM_SOURCE_CONTEXT_BYTES") {
		t.Fatalf("expected actionable overflow, got %v", err)
	}
	for i, message := range messages {
		if message.Role == "tool" && strings.Contains(message.Content, "omitted") {
			t.Fatalf("window %d discarded as redundant", i)
		}
	}
}

func TestBoundToolHistoryKeepsAlikeResultsOfDifferentSources(t *testing.T) {
	// Two files can read alike; only the re-read of one file is redundant.
	const budget = 5000
	body := strings.Repeat("сценарий", 125)
	messages := []providers.Message{{Role: "assistant", ToolCalls: []providers.ToolCall{
		{ID: "a", Function: &providers.FunctionCall{Name: "read_local_file", Arguments: `{"path":"/tmp/a.txt"}`}},
		{ID: "b", Function: &providers.FunctionCall{Name: "read_local_file", Arguments: `{"path":"/tmp/b.txt"}`}},
		{ID: "a-again", Function: &providers.FunctionCall{Name: "read_local_file", Arguments: `{"path":"/tmp/a.txt"}`}},
	}}}
	messages = append(messages,
		providers.Message{Role: "tool", ToolCallID: "a", Content: body},
		providers.Message{Role: "tool", ToolCallID: "b", Content: body},
		providers.Message{Role: "tool", ToolCallID: "a-again", Content: body},
	)
	out, err := boundToolHistory(messages, budget)
	if err != nil {
		t.Fatal(err)
	}
	if out[1].Content != body || out[2].Content != body {
		t.Fatal("distinct sources collapsed into one")
	}
	if !strings.Contains(out[3].Content, "omitted") {
		t.Fatal("re-read of the same file not compacted")
	}
}

type contextCheckingProvider struct {
	fakeProvider
	seen []providers.Message
}

func (p *contextCheckingProvider) Chat(ctx context.Context, messages []providers.Message, defs []providers.ToolDefinition, model string, options map[string]any) (*providers.LLMResponse, error) {
	p.seen = messages
	return p.fakeProvider.Chat(ctx, messages, defs, model, options)
}
func TestObserverBoundsHistoryBeforeProvider(t *testing.T) {
	p := &contextCheckingProvider{fakeProvider: fakeProvider{turns: []turn{{content: "done"}}}}
	o := &observer{delegate: p, stats: &stats{}}
	original := strings.Repeat("данные ", 20000)
	_, err := o.Chat(t.Context(), []providers.Message{{Role: "tool", Content: original, ToolCallID: "c1"}}, nil, "fake", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.seen) != 1 || len(p.seen[0].Content) > 16000 || !utf8.ValidString(p.seen[0].Content) {
		t.Fatal("oversized or invalid UTF-8 reached provider")
	}
}
