package main

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/agenttools"
)

func TestDraftRoundTripConflictAndChat(t *testing.T) {
	_, base := newAdminEngine(t)
	hub := newAdminWebHub(t, base, true)
	srv := adminWebServer(t, hub)
	var draft editorDraft
	body := `{"game_id":4242,"kind":"level","level_id":901,"params":{"title":"Исходный","question":"Текст"}}`
	if code := webDo(t, srv, "POST", "/api/v1/admin/drafts/open", body, &draft); code != 200 {
		t.Fatalf("open %d", code)
	}
	id, docID := draft.ID, draft.Active
	endpoint := "/api/v1/admin/drafts/" + id + "/documents/" + docID
	if code := webDo(t, srv, "PATCH", endpoint, `{"revision":1,"params":{"title":"Из формы","question":""}}`, &draft); code != 200 {
		t.Fatalf("patch %d", code)
	}
	if code := webDo(t, srv, "PATCH", endpoint, `{"revision":1,"params":{"title":"Устаревший"}}`, nil); code != 409 {
		t.Fatalf("stale %d", code)
	}
	// A fresh hub reads the same on-disk revision, including empty text.
	fresh := newAdminWebHub(t, base, true)
	fresh.store.dir = hub.store.dir
	restored, err := fresh.readDraft(id)
	if err != nil {
		t.Fatal(err)
	}
	doc := restored.Documents[docID]
	if doc.Params["title"] != "Из формы" || doc.Params["question"] != "" || doc.Revision != 2 {
		t.Fatalf("restored %+v", doc)
	}
	var chat, again chatSnapshot
	route := "/api/v1/admin/drafts/" + id + "/chat"
	if code := webDo(t, srv, "POST", route, `{}`, &chat); code != 200 {
		t.Fatalf("chat %d", code)
	}
	webDo(t, srv, "POST", route, `{}`, &again)
	if chat.ID != again.ID || chat.DraftID != id {
		t.Fatalf("chat link %+v %+v", chat, again)
	}
	if err = fresh.store.loadFromDisk(quietf); err != nil {
		t.Fatal(err)
	}
	saved, _ := fresh.store.get(chat.ID)
	if saved.DraftID != id {
		t.Fatal("chat link lost on restart")
	}
	tool := &draftTool{hub: hub, draftID: id, operation: "update"}
	result := tool.Execute(t.Context(), map[string]any{"document_id": docID, "revision": 2, "params": map[string]any{"title": "Из агента"}})
	if result.IsError {
		t.Fatal(result.Content)
	}
	if code := webDo(t, srv, "GET", "/api/v1/admin/drafts/"+id, "", &draft); code != 200 {
		t.Fatalf("read %d", code)
	}
	if draft.Documents[docID].Params["title"] != "Из агента" {
		t.Fatal("agent edit missing")
	}
	// Opening a form again must not overwrite existing draft with engine data.
	webDo(t, srv, "POST", "/api/v1/admin/drafts/open", body, &draft)
	restored, err = hub.readDraft(id)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Documents[docID].Params["title"] != "Из агента" {
		t.Fatal("reopen replaced draft")
	}
}

func TestDraftToolsCannotMutateEngine(t *testing.T) {
	_, base := newAdminEngine(t)
	hub := newAdminWebHub(t, base, true)
	for _, policy := range []agenttools.Policy{agenttools.PolicyFull, agenttools.PolicyApprove, agenttools.PolicyReadonly} {
		catalog, err := hub.catalog("unused", policy)
		if err != nil {
			t.Fatal(err)
		}
		list := hub.draftTurnTools("unused", policy, catalog)
		hasUpdate := false
		for _, tool := range list {
			if engineTool, ok := tool.(*agenttools.Tool); ok && engineTool.Mutating() {
				t.Fatalf("exposed %s under %s", tool.Name(), policy)
			}
			hasUpdate = hasUpdate || tool.Name() == "draft_update"
		}
		if hasUpdate != (policy != agenttools.PolicyReadonly) {
			t.Fatalf("wrong draft write policy %s", policy)
		}
	}
}

func TestDraftPublishRevisionAndNewLevel(t *testing.T) {
	_, base := newAdminEngine(t)
	hub := newAdminWebHub(t, base, true)
	srv := adminWebServer(t, hub)
	var d editorDraft
	code := webDo(t, srv, "POST", "/api/v1/admin/drafts/open", `{"game_id":4242,"kind":"level","params":{"title":"Новый","question":"Текст"}}`, &d)
	if code != 200 {
		t.Fatalf("open %d", code)
	}
	docID := d.Active
	route := "/api/v1/admin/drafts/" + d.ID + "/documents/" + docID + "/publish"
	if code = webDo(t, srv, "POST", route, `{"revision":0}`, nil); code != http.StatusConflict {
		t.Fatalf("stale publish %d", code)
	}
	if code = webDo(t, srv, "POST", route, `{"revision":1}`, &d); code != 200 {
		t.Fatalf("publish %d", code)
	}
	doc := d.Documents[docID]
	if doc.LevelID == 0 || doc.Revision != 2 || doc.Base["title"] != "Новый" {
		t.Fatalf("published %+v", doc)
	}
}

func TestDraftScopeAndToolConflict(t *testing.T) {
	_, base := newAdminEngine(t)
	hub := newAdminWebHub(t, base, true)
	srv := adminWebServer(t, hub)
	var d editorDraft
	webDo(t, srv, "POST", "/api/v1/admin/drafts/open", `{"game_id":0,"kind":"game","params":{"name":"Новая"}}`, &d)
	add := &draftTool{hub: hub, draftID: d.ID, operation: "add_level"}
	result := add.Execute(context.Background(), map[string]any{"params": map[string]any{"title": "Первое"}})
	if result.IsError {
		t.Fatal(result.Content)
	}
	if err := json.Unmarshal([]byte(result.Content), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Documents) != 2 {
		t.Fatalf("documents %d", len(d.Documents))
	}
	result = (&draftTool{hub: hub, draftID: d.ID, operation: "update"}).Execute(t.Context(), map[string]any{"document_id": d.Active, "revision": 0, "params": map[string]any{"name": "stale"}})
	if !result.IsError || !strings.Contains(result.Content, "изменён") {
		t.Fatalf("stale result %+v", result)
	}
	other := newTestHub(t, nil)
	other.store.dir = hub.store.dir
	if _, err := other.readDraft(d.ID); err == nil {
		t.Fatal("read draft for another engine")
	}
}

func TestDraftOpenLevelPreservesEdits(t *testing.T) {
	_, base := newAdminEngine(t)
	hub := newAdminWebHub(t, base, true)
	srv := adminWebServer(t, hub)
	var d editorDraft
	webDo(t, srv, "POST", "/api/v1/admin/drafts/open", `{"game_id":4242,"kind":"game","params":{}}`, &d)
	tool := &draftTool{hub: hub, draftID: d.ID, operation: "open_level", readonly: true}
	result := tool.Execute(t.Context(), map[string]any{"level_id": 901})
	if result.IsError {
		t.Fatal(result.Content)
	}
	if err := json.Unmarshal([]byte(result.Content), &d); err != nil {
		t.Fatal(err)
	}
	var docID string
	for _, doc := range d.Documents {
		if doc.Kind == "level" {
			docID = doc.ID
		}
	}
	if docID == "" {
		t.Fatal("level not loaded")
	}
	hub.draftMu.Lock()
	_, err := hub.patchDraft(d.ID, docID, patchDraftBody{Revision: 1, Params: map[string]any{"title": "Ручная правка"}})
	hub.draftMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	result = tool.Execute(t.Context(), map[string]any{"level_id": 901})
	if result.IsError || !strings.Contains(result.Content, "Ручная правка") {
		t.Fatal(result.Content)
	}
}
