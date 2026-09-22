package agenttools_test

import (
	"reflect"
	"testing"

	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// TestExportedSchemasMatchTools is the guard that keeps the browser editor and
// the agent on one description of a game and a level: the exported schema must
// be exactly the "params" property the real tool carries, not a copy of it.
func TestExportedSchemasMatchTools(t *testing.T) {
	e := newFakeEngine()
	e.admin = true
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true})

	for _, tc := range []struct {
		tool     string
		exported map[string]any
	}{
		{"admin_create_game", agenttools.GameParamsSchema()},
		{"admin_create_level", agenttools.LevelParamsSchema()},
	} {
		tool, ok := c.Lookup(tc.tool)
		if !ok {
			t.Fatalf("tool %q not found", tc.tool)
		}
		props, ok := tool.Parameters()["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: parameters have no properties object", tc.tool)
		}
		params, ok := props["params"].(map[string]any)
		if !ok {
			t.Fatalf("%s: parameters.properties has no params object", tc.tool)
		}
		if !reflect.DeepEqual(params, tc.exported) {
			t.Errorf("%s: exported schema differs from the tool's params schema:\ntool     = %#v\nexported = %#v", tc.tool, params, tc.exported)
		}
		if len(tc.exported["properties"].(map[string]any)) == 0 {
			t.Errorf("%s: exported schema has no properties", tc.tool)
		}
	}
}

// TestExportedSchemasAreFresh proves the doc comments: a caller that mangles
// the returned map cannot poison the next tool call.
func TestExportedSchemasAreFresh(t *testing.T) {
	for name, build := range map[string]func() map[string]any{
		"game":  agenttools.GameParamsSchema,
		"level": agenttools.LevelParamsSchema,
	} {
		first := build()
		props := first["properties"].(map[string]any)
		clear(props)
		first["type"] = "wrecked"
		second := build()
		if len(second["properties"].(map[string]any)) == 0 || second["type"] != "object" {
			t.Errorf("%s: schema shares state between calls: %#v", name, second)
		}
	}
}
