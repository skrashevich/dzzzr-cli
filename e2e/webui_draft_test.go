//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"testing"
)

// Exercise the public draft API through the real CLI and mock. Saving local
// fields and opening the chat must not touch the engine; publishing must.
func TestWebDraftPublishesOnlyExplicitly(t *testing.T) {
	h := startWeb(t)
	var draft struct {
		ID        string `json:"id"`
		Active    string `json:"active"`
		Documents map[string]struct {
			Revision int            `json:"revision"`
			LevelID  int            `json:"level_id"`
			Params   map[string]any `json:"params"`
		} `json:"documents"`
	}
	if code := h.do("POST", "/api/v1/admin/drafts/open", map[string]any{"game_id": 4242, "kind": "level", "level_id": 901, "params": map[string]any{"title": "Название из формы", "question": "Текст черновика"}}, &draft); code != 200 {
		t.Fatalf("open %d", code)
	}
	docID := draft.Active
	route := "/api/v1/admin/drafts/" + draft.ID
	var first, second struct {
		ID      string `json:"id"`
		DraftID string `json:"draft_id"`
	}
	if code := h.do("POST", route+"/chat", map[string]any{}, &first); code != 200 {
		t.Fatalf("chat %d", code)
	}
	h.do("POST", route+"/chat", map[string]any{}, &second)
	if first.ID != second.ID || first.DraftID != draft.ID {
		t.Fatal("chat not reused")
	}
	var live struct {
		Params map[string]any `json:"params"`
	}
	liveURL := "/api/v1/admin/games/4242/levels/901"
	h.do("GET", liveURL, nil, &live)
	if live.Params["title"] == "Название из формы" {
		t.Fatal("draft leaked to engine")
	}
	documentURL := route + "/documents/" + docID
	if code := h.do("PATCH", documentURL, map[string]any{"revision": 1, "params": map[string]any{"title": "Совместный черновик"}}, &draft); code != 200 {
		t.Fatalf("patch %d", code)
	}
	if code := h.do("POST", documentURL+"/publish", map[string]any{"revision": 1}, nil); code != http.StatusConflict {
		t.Fatalf("stale publish %d", code)
	}
	revision := draft.Documents[docID].Revision
	if code := h.do("POST", documentURL+"/publish", map[string]any{"revision": revision}, &draft); code != 200 {
		t.Fatalf("publish %d", code)
	}
	h.do("GET", liveURL, nil, &live)
	if live.Params["title"] != "Совместный черновик" || live.Params["question"] != "Текст черновика" {
		t.Fatalf("readback %s", fmt.Sprint(live.Params))
	}
}
