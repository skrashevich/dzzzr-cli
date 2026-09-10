package agentfiles

import (
	"fmt"
	"strings"
)

// args is one tool call's arguments. PicoClaw has already checked them against
// the schema, so the accessors only supply defaults.
type args map[string]any

func (a args) stringOr(key, def string) string {
	s, ok := a[key].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func (a args) requireString(key string) (string, error) {
	s := a.stringOr(key, "")
	if s == "" {
		return "", fmt.Errorf("аргумент %q обязателен", key)
	}
	return s, nil
}

func (a args) intOr(key string, def int) int {
	switch n := a[key].(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return def
}

func (a args) boolOr(key string, def bool) bool {
	if b, ok := a[key].(bool); ok {
		return b
	}
	return def
}

// schema builds the JSON Schema object of a tool's parameters.
func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	} else {
		s["required"] = []string{}
	}
	return s
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
