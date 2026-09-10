package agentloop

import (
	"encoding/json"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// Source documents must remain complete for cross-page interpretation. This
// budget is independent of the shorter, replaceable engine-state history.
const DefaultSourceContextBytes = 512 << 10

// Keep source documents intact and bound replaceable engine-state results
// separately. Preserve messages and call IDs so the tool-call protocol stays
// valid. Neither byte budget is a guarantee of a model's token context capacity.
func boundToolHistory(messages []providers.Message, sourceBudget int) ([]providers.Message, error) {
	if sourceBudget <= 0 {
		sourceBudget = DefaultSourceContextBytes
	}
	sourceCalls := map[string]bool{}
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			name := call.Name
			if name == "" && call.Function != nil {
				name = call.Function.Name
			}
			if name == "read_pdf" || name == "index_pdf" || name == "read_local_file" {
				sourceCalls[call.ID] = true
			}
		}
	}
	out := slices.Clone(messages)
	if sourceBytes(out, sourceCalls) > sourceBudget {
		// A run overruns this budget mostly by re-reading what it already has,
		// so drop the redundancy before declaring the source itself too large.
		compactSourceResults(out, sourceCalls)
		if sourceBytes(out, sourceCalls) > sourceBudget {
			return nil, fmt.Errorf("source document results exceed context budget (%d bytes); stopped without discarding pages. Increase DZZZR_LLM_SOURCE_CONTEXT_BYTES if the model supports it, or process the document in separate sections", sourceBudget)
		}
	}
	const totalBudget = 48000
	const perResult = 16000
	const omitted = "[Tool result omitted from model context to save space. Do not infer missing data; call the tool with narrower filters or pagination if needed.]"
	const suffix = "\n[Remaining tool result omitted from model context. This is an incomplete excerpt, not a complete JSON document. Request narrower filters or pagination for missing data.]"
	remaining := totalBudget
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role != "tool" || sourceCalls[out[i].ToolCallID] {
			continue
		}
		text := out[i].Content
		budget := min(perResult, remaining)
		if len(text) <= budget {
			remaining -= len(text)
			continue
		}
		if budget <= len(suffix)+len(omitted) {
			out[i].Content = omitted
			continue
		}
		end := budget - len(suffix)
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}
		out[i].Content = text[:end] + suffix
		remaining -= len(out[i].Content)
	}
	return out, nil
}

func sourceBytes(messages []providers.Message, sourceCalls map[string]bool) int {
	total := 0
	for _, message := range messages {
		if message.Role == "tool" && sourceCalls[message.ToolCallID] {
			total += len(message.Content)
		}
	}
	return total
}

const duplicateSource = "[Identical to an earlier read of the same source; omitted to save context.]"

// Replace byte-identical repeats of an earlier source result in place, keeping
// every message and call ID so the tool-call protocol stays valid and keeping
// the earliest full copy so references the model already made still resolve.
// Only exact repeats are redundant: every source tool reads a window (a page
// range, a byte offset), so two different results for one path are two
// different parts of it and both must survive.
func compactSourceResults(out []providers.Message, sourceCalls map[string]bool) {
	paths := sourcePaths(out, sourceCalls)
	seen := map[string]bool{}
	for i, message := range out {
		if message.Role != "tool" || !sourceCalls[message.ToolCallID] || message.Content == "" {
			continue
		}
		// Two files can legitimately read alike, so the path joins the key
		// whenever the originating call still names one.
		key := paths[message.ToolCallID] + "\x00" + message.Content
		if seen[key] {
			out[i].Content = duplicateSource
			continue
		}
		seen[key] = true
	}
}

func sourcePaths(messages []providers.Message, sourceCalls map[string]bool) map[string]string {
	paths := map[string]string{}
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			if !sourceCalls[call.ID] {
				continue
			}
			if path := callPath(call); path != "" {
				paths[call.ID] = path
			}
		}
	}
	return paths
}

// Every source tool names its file "path". A call whose arguments cannot be
// read simply yields no path, which only weakens the duplicate key.
func callPath(call providers.ToolCall) string {
	var args struct {
		Path string `json:"path"`
	}
	if call.Function != nil && call.Function.Arguments != "" {
		_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
	}
	if args.Path == "" {
		if path, ok := call.Arguments["path"].(string); ok {
			args.Path = path
		}
	}
	return args.Path
}
