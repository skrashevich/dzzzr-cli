package agenttools_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

type sourceFileEngine struct {
	*sourceToolEngine
	gameID int
}

func (e *sourceFileEngine) AdminCreateLevel(ctx context.Context, gameID int, params dzzzr.LevelParams) (int, error) {
	e.gameID = gameID
	return e.sourceToolEngine.AdminCreateLevel(ctx, gameID, params)
}

func writeSourceFile(t *testing.T, root, name, data string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSourceFilePreservesFieldsAndOverrides(t *testing.T) {
	root := t.TempDir()
	const question = "<p>Ёж — № 42 &amp; «Съешь ещё»</p>\n<img src=\"https://example.test/a.png\">"
	const data = `{"game_id":0,"mode":"","levels":[{"params":{"title":"Ёж и Щука","question":"<p>Ёж — № 42 &amp; «Съешь ещё»</p>\n<img src=\"https://example.test/a.png\">","hint1_interval":9007199254740993,"hint1":"Подсказка № 1","hint2":"Подсказка № 2","publish":false,"time_add_bonus_all":0,"comment":"","codes":[{"code":"ЁЖ42","danger":"1"}]}}]}`
	path := writeSourceFile(t, root, "source.json", data)
	e := &sourceFileEngine{sourceToolEngine: newSourceToolEngine()}
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true, FilesRoot: root})
	for _, name := range []string{"admin_validate_source", "admin_upload_source"} {
		r := sourceTool(t, c, name).Execute(t.Context(), map[string]any{"path": path, "game_id": 77, "mode": "create"})
		if r.IsError {
			t.Fatalf("%s: %s", name, r.Content)
		}
		if strings.Contains(r.Content, "Ёж") || strings.Contains(r.Content, "example.test") {
			t.Fatalf("file content exposed: %s", r.Content)
		}
	}
	if e.creates != 1 || e.gameID != 77 || e.params.Question != question || e.params.Title != "Ёж и Щука" || e.params.Hint1Interval != 9007199254740993 || e.params.Codes[0].Code != "ЁЖ42" || e.params.Comment == nil || *e.params.Comment != "" || e.params.TimeAddBonusAll == nil || *e.params.TimeAddBonusAll != 0 {
		t.Fatalf("source changed: game %d, params %+v", e.gameID, e.params)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != data {
		t.Fatalf("input file changed: %v", err)
	}
}

func TestSourceFileCapabilityAndLimit(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	const data = `{"game_id":42,"mode":"create","levels":[{"params":{"title":"Level"}}]}`
	path := writeSourceFile(t, root, "source.json", data)
	secret := writeSourceFile(t, outside, "secret.json", data)
	if err := os.Symlink(secret, filepath.Join(root, "escape.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("source.json", filepath.Join(root, "inside.json")); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(root, "large.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = f.Truncate((4 << 20) + 1)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("large file: %v %v", err, closeErr)
	}
	e := newSourceToolEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true, FilesRoot: root})
	for _, name := range []string{"admin_validate_source", "admin_upload_source"} {
		tool := sourceTool(t, c, name)
		for _, invalid := range []string{"../secret.json", secret, "escape.json", ".", "missing.json", "large.json"} {
			r := tool.Execute(t.Context(), map[string]any{"path": invalid})
			if !r.IsError {
				t.Errorf("%s accepted %s", name, invalid)
			}
		}
	}
	if e.creates != 0 {
		t.Fatal("invalid files reached engine")
	}
	for _, name := range []string{"admin_validate_source", "admin_upload_source"} {
		for _, valid := range []string{"source.json", path, "inside.json"} {
			if r := sourceTool(t, c, name).Execute(t.Context(), map[string]any{"path": valid}); r.IsError {
				t.Fatal(r.Content)
			}
		}
	}
	if e.creates != 3 {
		t.Fatalf("creates %d", e.creates)
	}
}

func TestSourceFileRejectsAmbiguityAndInvalidOverrides(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "source.json", `{"game_id":42,"mode":"create","levels":[{"params":{"title":"Level"}}]}`)
	e := newSourceToolEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true, FilesRoot: root})
	invalid := []map[string]any{
		{}, {"path": nil}, {"path": ""}, {"plan": nil, "path": "source.json"},
		{"plan": sourceArgs(1)["plan"], "game_id": 42}, {"plan": sourceArgs(1)["plan"], "mode": "create"},
	}
	for _, value := range []any{nil, false, 0, -1, 1.5, "42.5", ""} {
		invalid = append(invalid, map[string]any{"path": "source.json", "game_id": value})
	}
	for _, value := range []any{nil, false, 42, "", "CREATE", "create ", "invalid"} {
		invalid = append(invalid, map[string]any{"path": "source.json", "mode": value})
	}
	for _, name := range []string{"admin_validate_source", "admin_upload_source"} {
		for _, args := range invalid {
			if r := sourceTool(t, c, name).Execute(t.Context(), args); !r.IsError {
				t.Errorf("%s accepted %v", name, args)
			}
		}
	}
	if e.creates != 0 {
		t.Fatal("invalid request reached engine")
	}
}

func TestSourceFileValidatesEntirePlanBeforeWrite(t *testing.T) {
	root := t.TempDir()
	e := newSourceToolEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true, FilesRoot: root})
	for _, data := range []string{
		`{"game_id":42,"mode":"create","levels":[{"params":{"title":"Valid"}},{"params":{"title":"Invalid","hint1_interval":-1}}]}`,
		`{"game_id":42,"mode":"create","unexpected":true,"levels":[{"params":{"title":"Valid"}}]}`,
		`{"game_id":42,"mode":"create","levels":[{"params":{"title":"Valid","unexpected":true}}]}`,
		`{"game_id":42,"game_id":77,"mode":"create","levels":[{"params":{"title":"Valid"}}]}`,
		`{"game_id":42,"mode":"create","levels":[{"params":{"title":"Valid","title":"Duplicate"}}]}`,
		`{"game_id":42,"mode":"create","levels":[{"params":{"title":"Unencodable 😀"}}]}`,
		`null`, `[]`,
	} {
		writeSourceFile(t, root, "source.json", data)
		for _, name := range []string{"admin_validate_source", "admin_upload_source"} {
			if r := sourceTool(t, c, name).Execute(t.Context(), map[string]any{"path": "source.json", "game_id": 55}); !r.IsError {
				t.Errorf("%s accepted %s", name, data)
			}
		}
	}
	if e.creates != 0 {
		t.Fatal("invalid plan reached engine")
	}
}
