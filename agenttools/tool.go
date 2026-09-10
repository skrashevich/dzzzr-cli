package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// Tool is one engine capability exposed to an agent.
type Tool struct {
	name           string
	description    string
	parameters     map[string]any
	mutating       bool
	noCache        bool // Local files can change; local output creation must execute once per call.
	writesLocal    bool
	run            func(ctx context.Context, args arguments) (any, error)
	gate           *gate
	cache          *readCache
	reauthenticate func(context.Context) error
}

// Name returns the tool's name as the model sees it.
func (t *Tool) Name() string { return t.name }

// Description returns the tool's description.
func (t *Tool) Description() string { return t.description }

// Parameters returns a copy of the JSON Schema of the tool's arguments, so a
// provider handing it around cannot corrupt the catalog.
func (t *Tool) Parameters() map[string]any { return cloneSchema(t.parameters) }

// Mutating reports whether the tool changes game state and therefore needs
// authorization.
func (t *Tool) Mutating() bool { return t.mutating }

// WritesLocalFiles distinguishes artifact creation from game mutations. Local
// writes are available with readonly engine access, but are not read-only MCP calls.
func (t *Tool) WritesLocalFiles() bool { return t.writesLocal }

// Result is what Execute returns: either a JSON payload or a refusal.
type Result struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// Execute authorizes the call when needed, serves cached reads, runs the tool
// and encodes the result as JSON.
func (t *Tool) Execute(ctx context.Context, args map[string]any) Result {
	if t.mutating {
		if allowed, refusal := t.gate.authorize(ctx, t.name, args); !allowed {
			return Result{Content: refusal, IsError: true}
		}
	}
	key, cacheable := "", false
	if !t.mutating && !t.noCache {
		key, cacheable = cacheKey(t.name, args)
		if cacheable {
			if v, hit := t.cache.get(key); hit {
				return encodeValue(t.name, v)
			}
		}
	}
	value, err := t.run(ctx, arguments(args))
	if !t.mutating && dzzzr.AuthErrorKindOf(err) == dzzzr.AuthSession && t.reauthenticate != nil {
		t.cache.clear()
		if authErr := t.reauthenticate(ctx); authErr != nil {
			err = fmt.Errorf("session renewal failed: %w", authErr)
		} else {
			value, err = t.run(ctx, arguments(args))
		}
	}
	if t.mutating {
		// A failed batch may already have changed the engine.
		t.cache.clear()
	}
	if err != nil {
		if value != nil {
			result := encodeValue(t.name, map[string]any{"result": value, "error": err.Error()})
			result.IsError = true
			return result
		}
		return Result{Content: fmt.Sprintf("%s failed: %v", t.name, err), IsError: true}
	}
	if !t.mutating && cacheable {
		t.cache.put(key, value)
	}
	return encodeValue(t.name, value)
}

func encodeValue(name string, value any) Result {
	if s, ok := value.(string); ok {
		return Result{Content: s}
	}
	b, err := json.Marshal(value)
	if err != nil {
		return Result{Content: fmt.Sprintf("%s: cannot encode result: %v", name, err), IsError: true}
	}
	return Result{Content: string(b)}
}

func cloneSchema(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		switch tv := v.(type) {
		case map[string]any:
			out[k] = cloneSchema(tv)
		case []any:
			cp := make([]any, len(tv))
			copy(cp, tv)
			out[k] = cp
		case []string:
			cp := make([]string, len(tv))
			copy(cp, tv)
			out[k] = cp
		default:
			out[k] = v
		}
	}
	return out
}

// arguments is a tool call's arguments with lenient accessors: providers
// disagree about number typing, so an integer may arrive as float64, string
// or json.Number.
type arguments map[string]any

func (a arguments) optionalInt(key string) (int, bool) {
	v, ok := a[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			f, err := n.Float64()
			if err != nil {
				return 0, false
			}
			return int(f), true
		}
		return int(i), true
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}
		i, err := strconv.Atoi(s)
		if err != nil {
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return 0, false
			}
			return int(f), true
		}
		return i, true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func (a arguments) requireInt(key string) (int, error) {
	v, ok := a.optionalInt(key)
	if !ok {
		return 0, fmt.Errorf("argument %q must be an integer", key)
	}
	return v, nil
}

func (a arguments) optionalString(key string) (string, bool) {
	v, ok := a[key]
	if !ok || v == nil {
		return "", false
	}
	switch s := v.(type) {
	case string:
		t := strings.TrimSpace(s)
		return t, t != ""
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64), true
	case json.Number:
		return s.String(), true
	case bool:
		return strconv.FormatBool(s), true
	}
	return "", false
}

func (a arguments) requireString(key string) (string, error) {
	v, ok := a.optionalString(key)
	if !ok {
		return "", fmt.Errorf("argument %q must be a non-empty string", key)
	}
	return v, nil
}

func (a arguments) optionalBool(key string) (bool, bool) {
	v, ok := a[key]
	if !ok || v == nil {
		return false, false
	}
	switch b := v.(type) {
	case bool:
		return b, true
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true", "1", "yes", "да":
			return true, true
		case "false", "0", "no", "нет", "":
			return false, true
		}
	case float64:
		return b != 0, true
	}
	return false, false
}

// schema builds a JSON Schema object for a tool's parameters.
func schema(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	} else {
		s["required"] = []string{}
	}
	return s
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
