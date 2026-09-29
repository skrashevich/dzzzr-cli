package agentprotocol

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// HTTPProvider uses Go's HTTP transport (browser Fetch on js/wasm).
// Browser credentials are supplied by the caller and never written to storage.
type HTTPProvider struct {
	Endpoint, Key, Model string
	ExtraBody            map[string]any
}

func (p *HTTPProvider) GetDefaultModel() string { return p.Model }
func (p *HTTPProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]any) (*LLMResponse, error) {
	body := map[string]any{"model": model, "messages": messages, "stream": false}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	for k, v := range p.ExtraBody {
		body[k] = v
	}
	for k, v := range options {
		body[k] = v
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(p.Endpoint, "/")+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.Key != "" {
		req.Header.Set("Authorization", "Bearer "+p.Key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err = io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("LLM HTTP %d: %s", res.StatusCode, raw[:min(len(raw), 1200)])
	}
	var reply struct {
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
		Usage *UsageInfo `json:"usage"`
	}
	if err = json.Unmarshal(raw, &reply); err != nil {
		return nil, err
	}
	if len(reply.Choices) == 0 {
		return nil, fmt.Errorf("провайдер вернул пустой ответ")
	}
	m := reply.Choices[0]
	return &LLMResponse{Content: m.Message.Content, ReasoningContent: m.Message.ReasoningContent, ToolCalls: m.Message.ToolCalls, FinishReason: m.FinishReason, Usage: reply.Usage}, nil
}
