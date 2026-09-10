package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/tools"
	toolshared "github.com/sipeed/picoclaw/pkg/tools/shared"
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

// runtime turns the tools of one run into PicoClaw tools.
type runtime struct {
	cb    Callbacks
	stats *stats

	// mu serializes tool execution. PicoClaw runs a batch of tool calls in
	// parallel, and two concurrent calls would interleave the start/done
	// events of a transcript and — under the approve policy — race for the
	// user's answer on one terminal.
	mu sync.Mutex
}

// picoTool adapts one Tool to PicoClaw's tool interface.
type picoTool struct {
	tool Tool
	rt   *runtime
}

func (t *picoTool) Name() string               { return t.tool.Name() }
func (t *picoTool) Description() string        { return t.tool.Description() }
func (t *picoTool) Parameters() map[string]any { return t.tool.Parameters() }

func (t *picoTool) Execute(ctx context.Context, args map[string]any) *toolshared.ToolResult {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	name := t.Name()
	argsJSON := encodeArgs(args)
	t.rt.cb.status("tool", "Вызов инструмента: "+name)
	t.rt.cb.emit(Event{Type: EventToolStart, ToolName: name, ToolArgs: argsJSON})

	started := time.Now()
	res := t.tool.Execute(ctx, args)
	t.rt.stats.addTool(time.Since(started))

	t.rt.cb.emit(Event{
		Type:       EventToolDone,
		ToolName:   name,
		ToolArgs:   argsJSON,
		ToolResult: res.Content,
		ToolError:  res.IsError,
	})

	out := toolshared.SilentResult(res.Content)
	out.IsError = res.IsError
	return out
}

// newRegistry registers every tool of the run with PicoClaw.
func newRegistry(list []Tool, rt *runtime) (*tools.ToolRegistry, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("agentloop: the agent has no tools")
	}
	registry := tools.NewToolRegistry()
	for _, t := range list {
		registry.Register(&picoTool{tool: t, rt: rt})
	}
	return registry, nil
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
