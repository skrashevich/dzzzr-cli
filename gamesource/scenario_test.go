package gamesource

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

type scenarioEngine struct {
	game        dzzzr.GameParams
	levels      []dzzzr.AdminLevelInfo
	writes      []string
	refuseField bool
	failLevel   int
}

func newScenarioEngine() *scenarioEngine {
	return &scenarioEngine{game: dzzzr.GameParams{Name: "Игра", Date: "18.08.2029", Time: "22:00"}, levels: []dzzzr.AdminLevelInfo{
		{ID: 11, GameID: 42, Order: 1, Params: dzzzr.LevelParams{Title: "Первый", Question: "  <p>Задание</p>\n", Codes: []dzzzr.LevelCode{{Code: "DR1", Danger: "1"}}, Penalty: nil}},
		{ID: 22, GameID: 42, Order: 2, Params: dzzzr.LevelParams{Title: "Второй", Comment: new("Фото"), BonusCodes: []dzzzr.BonusCode{{Code: "B", Danger: "", Minutes: 5}}}},
	}}
}

func (e *scenarioEngine) AdminGetGame(_ context.Context, id int) (*dzzzr.AdminGameInfo, error) {
	return &dzzzr.AdminGameInfo{ID: id, Params: e.game}, nil
}
func (e *scenarioEngine) AdminListLevels(_ context.Context, _ int) ([]dzzzr.AdminLevel, error) {
	out := []dzzzr.AdminLevel{}
	for i, l := range e.levels {
		out = append(out, dzzzr.AdminLevel{ID: l.ID, Order: i + 1})
	}
	return out, nil
}
func (e *scenarioEngine) AdminGetLevel(_ context.Context, gid, id int) (*dzzzr.AdminLevelInfo, error) {
	for _, l := range e.levels {
		if l.ID == id {
			l.GameID = gid
			return &l, nil
		}
	}
	return nil, fmt.Errorf("unknown level")
}
func (e *scenarioEngine) AdminCreateGame(_ context.Context, p dzzzr.GameParams) (int, error) {
	e.writes = append(e.writes, "create_game")
	e.game = p
	return 50, nil
}
func (e *scenarioEngine) AdminReplaceGame(_ context.Context, _ int, p dzzzr.GameParams) error {
	e.writes = append(e.writes, "replace_game")
	if e.refuseField {
		p.Price = "old"
	}
	e.game = p
	return nil
}
func (e *scenarioEngine) AdminCreateLevel(_ context.Context, gid int, p dzzzr.LevelParams) (int, error) {
	e.writes = append(e.writes, "create_level")
	id := 100 + len(e.levels)
	e.levels = append(e.levels, dzzzr.AdminLevelInfo{ID: id, GameID: gid, Params: p})
	return id, nil
}
func (e *scenarioEngine) AdminReplaceLevel(_ context.Context, _ int, id int, p dzzzr.LevelParams) error {
	e.writes = append(e.writes, "replace_level")
	if e.failLevel == id {
		return fmt.Errorf("simulated write error")
	}
	for i := range e.levels {
		if e.levels[i].ID == id {
			e.levels[i].Params = p
			return nil
		}
	}
	return fmt.Errorf("unknown level")
}
func (e *scenarioEngine) AdminMoveLevel(_ context.Context, id int, up bool) error {
	e.writes = append(e.writes, "move")
	for i := range e.levels {
		if e.levels[i].ID == id && i > 0 && up {
			e.levels[i-1], e.levels[i] = e.levels[i], e.levels[i-1]
			return nil
		}
	}
	return nil
}
func (e *scenarioEngine) AdminUploadFile(_ context.Context, _ int, name string, _ []byte) (*dzzzr.AdminFileUpload, error) {
	e.writes = append(e.writes, "upload")
	return &dzzzr.AdminFileUpload{Name: name, URL: "https://files.example/" + name}, nil
}

func sampleScenario(t *testing.T) *Scenario {
	t.Helper()
	s, err := ExportScenario(t.Context(), newScenarioEngine(), 42, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestScenarioSchemaAndExplicitValues(t *testing.T) {
	s := sampleScenario(t)
	data, err := EncodeScenario(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"price":""`, `"publish":false`, `"penalty":null`, `"spoilers":[]`, `"code_count":0`} {
		if !strings.Contains(string(data), required) {
			t.Errorf("missing %s", required)
		}
	}
	delete(s.Levels[0].Params, "hint1")
	if _, err := EncodeScenario(s); err == nil {
		t.Fatal("missing snapshot field accepted")
	}
	s = sampleScenario(t)
	s.Levels[1].Key = s.Levels[0].Key
	if _, err := EncodeScenario(s); err == nil {
		t.Fatal("duplicate level key accepted")
	}
	s = sampleScenario(t)
	s.Levels[0].Params["question"] = `<img src="asset:missing">`
	if _, err := EncodeScenario(s); err == nil {
		t.Fatal("missing asset accepted")
	}
	if _, err := DecodeScenario([]byte(`{"format":"dzzzr-scenario","format":"dzzzr-scenario"}`)); err == nil {
		t.Fatal("duplicate member accepted")
	}
}

func TestScenarioImportRoundTripAndMapping(t *testing.T) {
	s := sampleScenario(t)
	s.Levels[0].Params["question"] = ""
	dst := &scenarioEngine{}
	r, err := ImportScenario(t.Context(), dst, s, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Complete || r.GameID != 50 || len(r.LevelIDs) != 2 {
		t.Fatalf("%+v", r)
	}
	exported, err := ExportScenario(t.Context(), dst, 50, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := compareParams(s.Game.Params, exported.Game.Params); err != nil {
		t.Fatal(err)
	}
	for i := range s.Levels {
		if err := compareParams(s.Levels[i].Params, exported.Levels[i].Params); err != nil {
			t.Fatal(err)
		}
	}
	// An explicit reversed mapping changes order without deleting or creating.
	r, err = ImportScenario(t.Context(), dst, s, ImportOptions{GameID: 50, LevelIDs: map[string]int{s.Levels[0].Key: 101, s.Levels[1].Key: 100}})
	if err != nil || !r.Complete || dst.levels[0].ID != 101 {
		t.Fatalf("%+v %v", r, err)
	}
	before := len(dst.writes)
	if _, err := ImportScenario(t.Context(), dst, s, ImportOptions{GameID: 50}); err == nil {
		t.Fatal("missing mapping accepted")
	}
	if len(dst.writes) != before {
		t.Fatal("mapping failure wrote to engine")
	}
}

func TestScenarioPartialFailureAndSilentRefusal(t *testing.T) {
	s := sampleScenario(t)
	dst := &scenarioEngine{failLevel: 101}
	r, err := ImportScenario(t.Context(), dst, s, ImportOptions{})
	if err == nil || r.Complete || len(r.Completed) != 1 || r.LevelIDs[s.Levels[1].Key] != 101 || r.Stage != "write_level" {
		t.Fatalf("%+v %v", r, err)
	}
	dst = &scenarioEngine{refuseField: true}
	r, err = ImportScenario(t.Context(), dst, s, ImportOptions{})
	if err == nil || !strings.Contains(err.Error(), "price") || r.Complete || len(dst.levels) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestScenarioAssetsExportImport(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials leaked")
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("image bytes"))
	}))
	defer srv.Close()
	e := newScenarioEngine()
	e.levels[0].Params.Question = `before <img src="/uploaded/test.png"> asset:literal <style>.x{background:url('/uploaded/test.png')}</style>`
	e.levels[0].Params.Hint1 = `<img src="/uploaded/test.png">`
	s, err := ExportScenario(t.Context(), e, 42, ExportOptions{BaseURL: srv.URL + "/moscow/"})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || len(s.Assets) != 1 {
		t.Fatalf("requests=%d assets=%d", requests, len(s.Assets))
	}
	if !strings.Contains(s.Levels[0].Params["question"].(string), "asset:literal") {
		t.Fatal("plain text changed")
	}
	dst := &scenarioEngine{}
	r, err := ImportScenario(t.Context(), dst, s, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Uploaded) != 1 || strings.Contains(dst.levels[0].Params.Question, `src="asset:`) {
		t.Fatal("asset URL not bound")
	}
	if !strings.Contains(s.Levels[0].Params["question"].(string), "asset:file_1") {
		t.Fatal("caller snapshot mutated")
	}
	for key, a := range s.Assets {
		a.SHA256 = strings.Repeat("0", 64)
		s.Assets[key] = a
	}
	dst = &scenarioEngine{}
	if _, err := ImportScenario(t.Context(), dst, s, ImportOptions{}); err == nil {
		t.Fatal("corrupt asset accepted")
	}
	if len(dst.writes) != 0 {
		t.Fatal("checksum failure wrote to engine")
	}
}

func TestScenarioFileContainmentAndNoOverwrite(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadScenarioFile(root, "escape/secret", 100); err == nil {
		t.Fatal("escaped root")
	}
	if err := WriteScenarioFile(root, "snapshot.json", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := WriteScenarioFile(root, "snapshot.json", []byte("second")); err == nil {
		t.Fatal("overwrote existing export")
	}
	s := sampleScenario(t)
	s.Assets["pic"] = ScenarioAsset{Filename: "pic.png", MediaType: "image/png", Path: "../secret"}
	if _, err := EncodeScenario(s); err == nil {
		t.Fatal("traversal accepted")
	}
	s.Assets["pic"] = ScenarioAsset{Filename: "pic.png", MediaType: "image/png", DataBase64: new(base64.StdEncoding.EncodeToString([]byte("x")))}
	data, err := EncodeScenario(s)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
}

func TestScenarioRawTextAndRewriteBoundaries(t *testing.T) {
	info := &dzzzr.AdminLevelInfo{Params: dzzzr.LevelParams{Question: "trimmed"}, Fields: url.Values{"question": {"  full\n"}}}
	if got := levelSnapshot(info)["question"]; got != "  full\n" {
		t.Fatalf("%q", got)
	}
	raw := `<p>asset:x</p><script>var s='asset:x';</script><img src='asset:x'><div style="background:url(asset:x)"></div>`
	got, err := rewriteHTML(raw, func(s string) (string, error) {
		if s == "asset:x" {
			return "https://host/image?a=1&b=2", nil
		}
		return s, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `<p>asset:x</p><script>var s='asset:x';</script>`) || !strings.Contains(got, "&amp;b=2") {
		t.Fatal(got)
	}
	unchanged, err := rewriteHTML(raw, func(s string) (string, error) { return s, nil })
	if err != nil || !reflect.DeepEqual(raw, unchanged) {
		t.Fatalf("%s %v", unchanged, err)
	}
}

func TestScenarioPreservesPerReferenceFragments(t *testing.T) {
	e := newScenarioEngine()
	e.levels[0].Params.Question = `<a href="https://files.example/doc.pdf#page=4">four</a><a href="https://files.example/doc.pdf#page=5">five</a>`
	s, err := ExportScenario(t.Context(), e, 42, ExportOptions{LinkedAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Assets) != 1 {
		t.Fatal("fragments caused duplicate downloads")
	}
	for key, a := range s.Assets {
		a.URL = ""
		a.DataBase64 = new(base64.StdEncoding.EncodeToString([]byte("pdf")))
		s.Assets[key] = a
	}
	dst := &scenarioEngine{}
	if _, err := ImportScenario(t.Context(), dst, s, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dst.levels[0].Params.Question, "#page=4") || !strings.Contains(dst.levels[0].Params.Question, "#page=5") {
		t.Fatal(dst.levels[0].Params.Question)
	}
}

func TestScenarioRejectsBrokenNestedAssetsBeforeWrites(t *testing.T) {
	for _, content := range []string{`body{background:url(./map.png)}`, `@import "other.css";`} {
		s := sampleScenario(t)
		s.Assets["style"] = ScenarioAsset{Filename: "style.css", MediaType: "text/css", DataBase64: new(base64.StdEncoding.EncodeToString([]byte(content)))}
		dst := &scenarioEngine{}
		if _, err := ImportScenario(t.Context(), dst, s, ImportOptions{}); err == nil {
			t.Fatal("broken dependency accepted")
		}
		if len(dst.writes) != 0 {
			t.Fatal("preflight wrote to engine")
		}
	}
	if err := validateRelocatableAsset(ScenarioAsset{Filename: "style.css", MediaType: "text/css"}, []byte(`body{background:url(https://files.example/map.png)}`)); err != nil {
		t.Fatal(err)
	}
}

func TestScenarioLinkedImageRejectsLoginHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!DOCTYPE html><form>Login</form>"))
	}))
	defer srv.Close()
	s := sampleScenario(t)
	s.Assets["pic"] = ScenarioAsset{Filename: "pic.png", MediaType: "image/png", URL: srv.URL}
	dst := &scenarioEngine{}
	if _, err := ImportScenario(t.Context(), dst, s, ImportOptions{}); err == nil {
		t.Fatal("login page accepted as image")
	}
	if len(dst.writes) != 0 {
		t.Fatal("preflight wrote to engine")
	}
}
