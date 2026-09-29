package browserapp

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/agentloop"
	"github.com/skrashevich/dzzzr-cli/gamesource"
)

func editorCall(t *testing.T, w *Workspace, method, path string, body map[string]any) any {
	t.Helper()
	v, err := w.API(method, path, body)
	if err != nil {
		t.Fatal(path, err)
	}
	return v
}
func createGame(t *testing.T, w *Workspace) *Draft {
	t.Helper()
	d := editorCall(t, w, "POST", "/admin/drafts/open", map[string]any{"kind": "game", "params": map[string]any{"name": "Локальная игра"}}).(*Draft)
	doc := d.Documents[d.Active]
	return editorCall(t, w, "POST", fmt.Sprintf("/admin/drafts/%s/documents/%s/publish", d.ID, doc.ID), map[string]any{"revision": doc.Revision}).(*Draft)
}
func TestEditorExportRestoreAndConflict(t *testing.T) {
	var w Workspace
	d := createGame(t, &w)
	d = editorCall(t, &w, "POST", "/admin/drafts/open", map[string]any{"draft_id": d.ID, "game_id": d.GameID, "kind": "level", "params": map[string]any{"title": "Первый", "question": "  <p>Исходный HTML</p>\n", "codes": []any{map[string]any{"code": "012345", "synonyms": "01#02", "sector": 0, "danger": "1"}}}}).(*Draft)
	doc := d.Documents[d.Active]
	patchPath := fmt.Sprintf("/admin/drafts/%s/documents/%s", d.ID, doc.ID)
	d = editorCall(t, &w, "PATCH", patchPath, map[string]any{"revision": doc.Revision, "params": map[string]any{"hint1": "Сохранённый черновик"}}).(*Draft)
	if _, err := w.API("PATCH", patchPath, map[string]any{"revision": doc.Revision, "params": map[string]any{"title": "Затереть"}}); err == nil {
		t.Fatal("stale revision accepted")
	} else {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 409 {
			t.Fatal(err)
		}
	}
	if _, err := w.API("PATCH", patchPath, map[string]any{"revision": d.Documents[d.Active].Revision, "params": map[string]any{"typo": 1}}); err == nil {
		t.Fatal("unknown field accepted")
	}
	editorCall(t, &w, "POST", patchPath+"/publish", map[string]any{"revision": d.Documents[d.Active].Revision})
	state := w.Snapshot()
	var restored Workspace
	if err := restored.Restore(state); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(editorCall(t, &restored, "GET", fmt.Sprintf("/admin/games/%d/scenario", d.GameID), nil))
	s, err := gamesource.DecodeScenario(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Levels) != 1 || s.Levels[0].Params["question"] != "  <p>Исходный HTML</p>\n" || s.Levels[0].Params["hint1"] != "Сохранённый черновик" {
		t.Fatal(string(raw))
	}
	state.Drafts[d.ID].Documents[d.Active].Params["title"] = "external mutation"
	latest := editorCall(t, &restored, "GET", "/admin/drafts/"+d.ID, nil).(*Draft)
	if latest.Documents[d.Active].Params["title"] != "Первый" {
		t.Fatal("snapshot aliases state")
	}
}
func TestEditorEmbeddedAssetsAndProvenance(t *testing.T) {
	var w Workspace
	d := createGame(t, &w)
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0}
	upload := editorCall(t, &w, "POST", fmt.Sprintf("/admin/games/%d/files", d.GameID), map[string]any{"name": "pixel.png", "data": base64.StdEncoding.EncodeToString(png)}).(map[string]any)
	doc := d.Documents[d.Active]
	editorCall(t, &w, "PATCH", fmt.Sprintf("/admin/drafts/%s/documents/%s", d.ID, doc.ID), map[string]any{"revision": doc.Revision, "params": map[string]any{"legend": "<img src=\"" + upload["url"].(string) + "\">"}})
	raw, _ := json.Marshal(editorCall(t, &w, "GET", fmt.Sprintf("/admin/games/%d/scenario", d.GameID), nil))
	s, err := gamesource.DecodeScenario(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Assets) != 1 {
		t.Fatal("asset lost")
	}
	s.Source = &gamesource.ScenarioSource{Engine: "classic.dzzzr", BaseURL: "https://example.com/", Project: "test", GameID: 1566}
	s.Extensions = map[string]any{"test.unknown": []any{1, "retained"}}
	s.Game.Extensions = map[string]any{"test.layout": "retained"}
	imported := editorCall(t, &w, "POST", "/admin/scenario/import", map[string]any{"scenario": s}).(map[string]any)
	gid := integer(imported, "game_id", 0)
	v := editorCall(t, &w, "GET", fmt.Sprintf("/admin/games/%d/scenario", gid), nil)
	raw, _ = json.Marshal(v)
	got, err := gamesource.DecodeScenario(raw)
	if err != nil || got.Source.GameID != 1566 || got.Game.Extensions["test.layout"] != "retained" || len(got.Assets) != 1 {
		t.Fatal(string(raw), err)
	}
}
func TestBrowserAgentEditsSharedDraftAndReports(t *testing.T) {
	var a App
	d := createGame(t, &a.Editor)
	doc := d.Documents[d.Active]
	calls := 0
	events := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.UnmarshalRead(r.Body, &body)
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"read","type":"function","function":{"name":"draft_read","arguments":"{}"}}]}}]}`))
		case 2:
			raw, _ := json.Marshal(body)
			if !strings.Contains(string(raw), "Локальная игра") {
				t.Error("model did not receive shared draft")
			}
			args, _ := json.Marshal(map[string]any{"document_id": doc.ID, "revision": doc.Revision, "params": map[string]any{"name": "Правка агента"}})
			reply := map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "write", "type": "function", "function": map[string]any{"name": "draft_update", "arguments": string(args)}}}}}}}
			_ = json.MarshalWrite(w, reply)
		default:
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"Обновлено."}}],"usage":{"prompt_tokens":30,"completion_tokens":5,"total_tokens":35}}`))
		}
	}))
	defer srv.Close()
	a.Approve = func(context.Context, string, map[string]any) error { events++; return nil }
	r, err := a.ChatWithEvents(t.Context(), ChatConfig{Endpoint: srv.URL, Model: "test", DraftID: d.ID, Policy: "confirm"}, []ChatMessage{{Role: "user", Content: "Переименуй"}}, agentloop.Callbacks{})
	if err != nil || r.Turns != 3 || r.Report == "" || events != 1 {
		t.Fatal(r, err, events)
	}
	latest := editorCall(t, &a.Editor, "GET", "/admin/drafts/"+d.ID, nil).(*Draft)
	if latest.Documents[doc.ID].Params["name"] != "Правка агента" {
		t.Fatal(latest)
	}
}

func TestEditorCopyMoveDelete(t *testing.T) {
	var w Workspace
	d := createGame(t, &w)
	for _, title := range []string{"Первый", "Второй"} {
		d = editorCall(t, &w, "POST", "/admin/drafts/open", map[string]any{"draft_id": d.ID, "game_id": d.GameID, "kind": "level", "params": map[string]any{"title": title, "question": "Текст задания"}}).(*Draft)
		doc := d.Documents[d.Active]
		d = editorCall(t, &w, "POST", fmt.Sprintf("/admin/drafts/%s/documents/%s/publish", d.ID, doc.ID), map[string]any{"revision": doc.Revision}).(*Draft)
	}
	s := w.Snapshot()
	g := s.Games[d.GameID]
	id := g.LevelIDs[1]
	editorCall(t, &w, "POST", fmt.Sprintf("/admin/games/%d/levels/%d/move", d.GameID, id), map[string]any{"up": true})
	s = w.Snapshot()
	if s.Games[d.GameID].Scenario.Levels[0].Params["title"] != "Второй" {
		t.Fatal("level order not changed")
	}
	copied := editorCall(t, &w, "POST", fmt.Sprintf("/admin/games/%d/copy", d.GameID), map[string]any{"with_levels": true}).(map[string]any)
	copyID := integer(copied, "id", 0)
	s = w.Snapshot()
	if len(s.Games[copyID].LevelIDs) != 2 || s.Games[copyID].LevelIDs[0] == id {
		t.Fatal("copy reuses local IDs")
	}
	editorCall(t, &w, "DELETE", fmt.Sprintf("/admin/games/%d/levels/%d", d.GameID, id), nil)
	editorCall(t, &w, "DELETE", fmt.Sprintf("/admin/games/%d", copyID), nil)
	var restored Workspace
	if err := restored.Restore(w.Snapshot()); err != nil {
		t.Fatal(err)
	}
	s = restored.Snapshot()
	if len(s.Games) != 1 || len(s.Games[d.GameID].LevelIDs) != 1 {
		t.Fatal("deletion not retained")
	}
}

func TestEditorExportsUnpublishedLevelsInCreationOrder(t *testing.T) {
	var w Workspace
	d := createGame(t, &w)
	for i := range 12 {
		editorCall(t, &w, "POST", "/admin/drafts/open", map[string]any{"draft_id": d.ID, "game_id": d.GameID, "kind": "level", "params": map[string]any{"title": fmt.Sprintf("Уровень %d", i+1), "question": "Текст"}})
	}
	raw, _ := json.Marshal(editorCall(t, &w, "GET", fmt.Sprintf("/admin/games/%d/scenario", d.GameID), nil))
	s, err := gamesource.DecodeScenario(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Levels) != 12 {
		t.Fatal("unpublished levels lost")
	}
	for i, l := range s.Levels {
		if l.Params["title"] != fmt.Sprintf("Уровень %d", i+1) {
			t.Fatalf("wrong draft order: %v", l.Params["title"])
		}
	}
}
