package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	providers "github.com/skrashevich/dzzzr-cli/agentprotocol"
	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// Tool is what the loop can call. The engine catalog's own tool type already
// satisfies it, so a caller adds tools of its own without wrapping the
// catalog.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, args map[string]any) agenttools.Result
}

var _ Tool = (*agenttools.Tool)(nil)

// registry supplies the same tool contract to the shared native/browser loop.
type registry struct {
	list    []Tool
	byName  map[string]Tool
	rt      *runtime
	schemas map[string]*jsonschema.Resolved
}
type runtime struct {
	cb    Callbacks
	stats *stats
}

func newRegistry(list []Tool, rt *runtime) (*registry, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("agentloop: the agent has no tools")
	}
	r := &registry{list: list, byName: map[string]Tool{}, rt: rt, schemas: map[string]*jsonschema.Resolved{}}
	for _, t := range list {
		if r.byName[t.Name()] != nil {
			return nil, fmt.Errorf("duplicate tool %s", t.Name())
		}
		r.byName[t.Name()] = t
		raw, err := json.Marshal(t.Parameters())
		if err != nil {
			return nil, err
		}
		var schema jsonschema.Schema
		if err = json.Unmarshal(raw, &schema); err != nil {
			return nil, err
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return nil, fmt.Errorf("tool %s: %w", t.Name(), err)
		}
		r.schemas[t.Name()] = resolved
	}
	return r, nil
}
func (r *registry) execute(ctx context.Context, name string, args map[string]any) (out agenttools.Result) {
	argsJSON := encodeArgs(args)
	r.rt.cb.status("tool", "Вызов инструмента: "+name)
	r.rt.cb.emit(Event{Type: EventToolStart, ToolName: name, ToolArgs: argsJSON})
	started := time.Now()
	defer func() {
		if p := recover(); p != nil {
			out = agenttools.Result{Content: fmt.Sprintf("internal panic in tool %s: %v", name, p), IsError: true}
		}
		r.rt.stats.addTool(time.Since(started))
		r.rt.cb.emit(Event{Type: EventToolDone, ToolName: name, ToolArgs: argsJSON, ToolResult: out.Content, ToolError: out.IsError})
	}()
	if t := r.byName[name]; t != nil {
		var value any
		raw, err := json.Marshal(args)
		if err == nil {
			err = json.Unmarshal(raw, &value)
		}
		if err == nil {
			err = r.schemas[name].Validate(value)
		}
		if err != nil {
			return agenttools.Result{Content: "неверные аргументы: " + err.Error(), IsError: true}
		}
		return t.Execute(ctx, args)
	}
	return agenttools.Result{Content: "неизвестный инструмент: " + name, IsError: true}
}

type loopResult struct {
	Content    string
	Iterations int
}

func runToolLoop(ctx context.Context, p providers.LLMProvider, model string, r *registry, maxTurns int, messages []providers.Message) (*loopResult, error) {
	defs := make([]providers.ToolDefinition, 0, len(r.list))
	for _, t := range r.list {
		defs = append(defs, providers.ToolDefinition{Type: "function", Function: providers.ToolFunctionDefinition{Name: t.Name(), Description: t.Description(), Parameters: t.Parameters()}})
	}
	out := &loopResult{}
	for turn := 0; turn < maxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out.Iterations++
		response, err := p.Chat(ctx, messages, defs, model, map[string]any{})
		if err != nil {
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}
		if len(response.ToolCalls) == 0 {
			out.Content = response.Content
			return out, nil
		}
		calls := append([]providers.ToolCall(nil), response.ToolCalls...)
		for i := range calls {
			c := &calls[i]
			if c.Function != nil {
				c.Name = c.Function.Name
				if c.Function.Arguments != "" {
					if err := json.Unmarshal([]byte(c.Function.Arguments), &c.Arguments); err != nil {
						return nil, fmt.Errorf("неверные аргументы инструмента %s: %w", c.Name, err)
					}
				}
			}
			if c.Arguments == nil {
				c.Arguments = map[string]any{}
			}
			b, err := json.Marshal(c.Arguments)
			if err != nil {
				return nil, err
			}
			c.Type = "function"
			signature := c.ThoughtSignature
			if c.Function != nil && c.Function.ThoughtSignature != "" {
				signature = c.Function.ThoughtSignature
			}
			c.Function = &providers.FunctionCall{Name: c.Name, Arguments: string(b), ThoughtSignature: signature}
			if c.ID == "" {
				c.ID = fmt.Sprintf("call-%d-%d", turn, i)
			}
		}
		messages = append(messages, providers.Message{Role: "assistant", Content: response.Content, ToolCalls: calls, ReasoningContent: response.ReasoningContent})
		for _, c := range calls {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			result := r.execute(ctx, c.Name, c.Arguments)
			messages = append(messages, providers.Message{Role: "tool", Content: result.Content, ToolCallID: c.ID})
		}
	}
	return out, nil
}

// collectTools merges the catalog's tools with the caller's own. The catalog
// has already applied its policy, so a tool it withholds never reaches the
// model.
func collectTools(catalog *agenttools.Catalog, extra []Tool) []Tool {
	var list []Tool
	if catalog != nil {
		for _, t := range catalog.Tools() {
			list = append(list, t)
		}
	}
	for _, t := range extra {
		if t != nil {
			list = append(list, t)
		}
	}
	return list
}

// encodeArgs renders a tool call's arguments for display and debugging.
func encodeArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	data, err := json.Marshal(args)
	if err != nil {
		return fmt.Sprintf("%v", args)
	}
	return string(data)
}
