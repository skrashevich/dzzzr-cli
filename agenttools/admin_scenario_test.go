package agenttools_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/gamesource"
)

type scenarioToolEngine struct{ *fakeEngine }

func (e *scenarioToolEngine) AdminGetGame(_ context.Context, id int) (*dzzzr.AdminGameInfo, error) {
	return &dzzzr.AdminGameInfo{ID: id, Params: dzzzr.GameParams{Name: "Test", Date: "18.08.2029", Time: "22:00"}}, nil
}
func (e *scenarioToolEngine) AdminListLevels(context.Context, int) ([]dzzzr.AdminLevel, error) {
	return []dzzzr.AdminLevel{}, nil
}

func TestScenarioToolsExportAndPolicy(t *testing.T) {
	e := &scenarioToolEngine{newFakeEngine()}
	e.admin = true
	root := t.TempDir()
	c := mustCatalog(t, e, agenttools.Options{IncludeAdmin: true, Policy: agenttools.PolicyReadonly, FilesRoot: root})
	tool := sourceTool(t, c, "admin_export_scenario")
	if tool.Mutating() || !tool.WritesLocalFiles() {
		t.Fatal("wrong export classification")
	}
	r := tool.Execute(t.Context(), map[string]any{"game_id": 42, "path": "scenario.json"})
	if r.IsError {
		t.Fatal(r.Content)
	}
	data, err := os.ReadFile(filepath.Join(root, "scenario.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gamesource.DecodeScenario(data); err != nil {
		t.Fatal(err)
	}
	r = sourceTool(t, c, "admin_validate_scenario").Execute(t.Context(), map[string]any{"path": "scenario.json"})
	if r.IsError {
		t.Fatal(r.Content)
	}
	r = tool.Execute(t.Context(), map[string]any{"game_id": 42, "path": "../outside.json"})
	if !r.IsError {
		t.Fatal("escaped file root")
	}
	r = tool.Execute(t.Context(), map[string]any{"game_id": 42, "path": "scenario.json"})
	if !r.IsError {
		t.Fatal("cached export or overwritten file")
	}
	for _, tool := range c.Tools() {
		if tool.Name() == "admin_import_scenario" {
			t.Fatal("readonly exposes import")
		}
	}
	r = sourceTool(t, c, "admin_import_scenario").Execute(t.Context(), map[string]any{"path": "missing.json"})
	if !r.IsError {
		t.Fatal("readonly import accepted")
	}
}
