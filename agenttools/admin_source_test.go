package agenttools_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

type sourceToolEngine struct {
	*fakeEngine
	uploads, technical, creates, replaces int
	filename                              string
	data                                  []byte
	params                                dzzzr.LevelParams
	failCreate                            int
}

func newSourceToolEngine() *sourceToolEngine { return &sourceToolEngine{fakeEngine: newFakeEngine()} }
func (e *sourceToolEngine) AdminUploadFile(_ context.Context, _ int, name string, data []byte) (*dzzzr.AdminFileUpload, error) {
	e.uploads++
	e.filename = name
	e.data = bytes.Clone(data)
	return &dzzzr.AdminFileUpload{Name: name, URL: "https://example.test/uploaded/" + name}, nil
}
func (e *sourceToolEngine) AdminCreateTechnicalLevel(context.Context, int) (int, error) {
	e.technical++
	return 77, nil
}
func (e *sourceToolEngine) AdminCreateLevel(_ context.Context, _ int, p dzzzr.LevelParams) (int, error) {
	e.creates++
	e.params = p
	if e.creates == e.failCreate {
		return 0, errors.New("write failed")
	}
	return 100 + e.creates, nil
}
func (e *sourceToolEngine) AdminReplaceLevel(_ context.Context, _, _ int, p dzzzr.LevelParams) error {
	e.replaces++
	e.params = p
	return nil
}
func (e *sourceToolEngine) AdminUpdateLevel(_ context.Context, _, _ int, p dzzzr.LevelParams) error {
	e.params = p
	return nil
}
func (e *sourceToolEngine) AdminGetLevel(_ context.Context, gid, lid int) (*dzzzr.AdminLevelInfo, error) {
	return &dzzzr.AdminLevelInfo{GameID: gid, ID: lid}, nil
}
func sourceTool(t *testing.T, c *agenttools.Catalog, name string) *agenttools.Tool {
	t.Helper()
	tool, ok := c.Lookup(name)
	if !ok {
		t.Fatalf("missing %s", name)
	}
	return tool
}
func sourceArgs(levels int) map[string]any {
	items := []any{}
	for range levels {
		items = append(items, map[string]any{"params": map[string]any{"title": "Level", "publish": false}})
	}
	return map[string]any{"plan": map[string]any{"game_id": 42, "mode": "create", "levels": items}}
}

func TestSourceToolVisibilityAndPolicy(t *testing.T) {
	names := []string{"admin_upload_file", "admin_create_technical_level", "admin_validate_source", "admin_upload_source"}
	for _, include := range []bool{false, true} {
		for _, hasAdmin := range []bool{false, true} {
			e := newSourceToolEngine()
			e.admin = hasAdmin
			c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly, IncludeAdmin: include})
			visible := map[string]bool{}
			for _, tool := range c.Tools() {
				visible[tool.Name()] = true
			}
			for _, name := range names {
				tool, ok := c.Lookup(name)
				if ok != (include && hasAdmin) {
					t.Errorf("%s inclusion=%v", name, ok)
				}
				if !ok {
					continue
				}
				wantMutation := name != "admin_validate_source"
				if tool.Mutating() != wantMutation || visible[name] == wantMutation {
					t.Errorf("policy metadata %s", name)
				}
				if wantMutation && !tool.Execute(t.Context(), nil).IsError {
					t.Errorf("readonly allowed %s", name)
				}
			}
		}
	}
	e := newSourceToolEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyApprove, IncludeAdmin: true, Confirmer: agenttools.ConfirmerFunc(func(context.Context, agenttools.ConfirmRequest) (bool, error) { return false, nil })})
	for _, name := range []string{"admin_upload_file", "admin_create_technical_level", "admin_upload_source"} {
		if !sourceTool(t, c, name).Execute(t.Context(), nil).IsError {
			t.Errorf("denied %s executed", name)
		}
	}
	if e.uploads+e.technical+e.creates != 0 {
		t.Fatal("denied writes reached engine")
	}
}

func TestSourceUploadFileCapability(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	data := []byte("private-binary-payload\x00\xff")
	if err := os.WriteFile(filepath.Join(root, "image.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(root, "escape.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("image.png", filepath.Join(root, "inside.png")); err != nil {
		t.Fatal(err)
	}
	large, err := os.Create(filepath.Join(root, "large.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(dzzzr.MaxAdminFileBytes + 1); err != nil {
		_ = large.Close()
		t.Fatal(err)
	}
	if err := large.Close(); err != nil {
		t.Fatal(err)
	}
	e := newSourceToolEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true, FilesRoot: root})
	tool := sourceTool(t, c, "admin_upload_file")
	for _, path := range []string{"../secret.png", filepath.Join(outside, "secret.png"), "escape.png", ".", "missing.png", "large.png"} {
		r := tool.Execute(t.Context(), map[string]any{"game_id": 42, "path": path})
		if !r.IsError {
			t.Errorf("accepted outside/nonfile %q", path)
		}
	}
	if e.uploads != 0 {
		t.Fatal("invalid files reached engine")
	}
	for _, path := range []string{"image.png", filepath.Join(root, "image.png"), "inside.png"} {
		r := tool.Execute(t.Context(), map[string]any{"game_id": 42, "path": path, "name": "map.final.png"})
		if r.IsError {
			t.Fatal(r.Content)
		}
		if !bytes.Equal(e.data, data) || e.filename != "map.final.png" {
			t.Fatal("file data changed")
		}
		if strings.Contains(r.Content, "private-binary-payload") || strings.Contains(r.Content, base64.StdEncoding.EncodeToString(data)) {
			t.Fatal("output exposes file content")
		}
	}
	for _, args := range []map[string]any{{"game_id": 0, "path": "image.png"}, {"game_id": 42, "path": "image.png", "name": ""}, {"game_id": 42, "path": "image.png", "name": false}} {
		if !tool.Execute(t.Context(), args).IsError {
			t.Errorf("accepted %v", args)
		}
	}
	if e.uploads != 3 {
		t.Fatalf("unexpected uploads %d", e.uploads)
	}
}

func TestSourceToolNewFieldsAndSeparateTechnical(t *testing.T) {
	e := newSourceToolEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true})
	params := map[string]any{"title": "Source", "comment": "", "time_add_bonus_all": 0, "publish": false}
	args := map[string]any{"plan": map[string]any{"game_id": 42, "mode": "update", "levels": []any{map[string]any{"id": 7, "params": params}}}}
	for _, name := range []string{"admin_validate_source", "admin_upload_source"} {
		r := sourceTool(t, c, name).Execute(t.Context(), args)
		if r.IsError {
			t.Fatalf("%s: %s", name, r.Content)
		}
	}
	if e.replaces != 1 || e.params.Comment == nil || *e.params.Comment != "" || e.params.TimeAddBonusAll == nil || *e.params.TimeAddBonusAll != 0 || e.params.Publish == nil || *e.params.Publish {
		t.Fatalf("lost explicit values: %+v", e.params)
	}
	if e.technical != 0 {
		t.Fatal("source implicitly created technical level")
	}
	r := sourceTool(t, c, "admin_create_technical_level").Execute(t.Context(), map[string]any{"game_id": 42})
	if r.IsError || e.technical != 1 {
		t.Fatalf("technical: %+v", r)
	}
	schema := sourceTool(t, c, "admin_update_level").Parameters()["properties"].(map[string]any)["params"].(map[string]any)["properties"].(map[string]any)
	for key, want := range map[string]string{"comment": "string", "time_add_bonus_all": "integer"} {
		if schema[key].(map[string]any)["type"] != want {
			t.Errorf("schema %s", key)
		}
	}
	params["time_add_bonus_all"] = "0"
	if !sourceTool(t, c, "admin_upload_source").Execute(t.Context(), args).IsError || e.replaces != 1 {
		t.Fatal("wrong field type reached engine")
	}
}

func TestSourcePartialFailureInvalidatesCache(t *testing.T) {
	e := newSourceToolEngine()
	e.failCreate = 2
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true, ReadCacheTTL: time.Hour})
	read := sourceTool(t, c, "admin_games")
	for range 2 {
		if r := read.Execute(t.Context(), nil); r.IsError {
			t.Fatal(r.Content)
		}
	}
	if e.calls["AdminListGames"] != 1 {
		t.Fatal("read not cached")
	}
	r := sourceTool(t, c, "admin_upload_source").Execute(t.Context(), sourceArgs(3))
	if !r.IsError {
		t.Fatal("partial error marked successful")
	}
	var response struct {
		Result struct {
			Completed []struct {
				Index int `json:"index"`
				ID    int `json:"id"`
			} `json:"completed"`
			FailedIndex *int `json:"failed_index"`
		} `json:"result"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.Content), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Result.Completed) != 1 || response.Result.Completed[0].ID != 101 || response.Result.FailedIndex == nil || *response.Result.FailedIndex != 1 || response.Error == "" || e.creates != 2 {
		t.Fatalf("lost partial result: %s", r.Content)
	}
	if result := read.Execute(t.Context(), nil); result.IsError {
		t.Fatal(result.Content)
	}
	if e.calls["AdminListGames"] != 2 {
		t.Fatal("failed mutation retained stale cache")
	}
}
