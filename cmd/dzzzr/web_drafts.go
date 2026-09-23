package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/gamesource"
)

// Drafts are local documents, never engine writes. All read/modify/write cycles
// (including publication) hold draftMu, so a revision identifies exact contents.
type editorDraft struct {
	ID        string                    `json:"id"`
	BaseURL   string                    `json:"base_url"`
	GameID    int                       `json:"game_id"`
	ChatID    string                    `json:"chat_id"`
	Active    string                    `json:"active"`
	Documents map[string]*draftDocument `json:"documents"`
}
type draftDocument struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	LevelID  int            `json:"level_id"`
	Revision int            `json:"revision"`
	Params   map[string]any `json:"params"`
	Base     map[string]any `json:"base"`
}

var errDraftConflict = errors.New("черновик уже изменён; получите свежую версию и объедините правки")

func (h *webHub) draftDir() string { return filepath.Join(h.store.dir, "drafts") }
func (h *webHub) readDraft(id string) (*editorDraft, error) {
	if !validChatID(id) {
		return nil, errors.New("неверный идентификатор черновика")
	}
	root, err := os.OpenRoot(h.draftDir())
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := root.ReadFile(id + ".json")
	if err != nil {
		return nil, err
	}
	var d editorDraft
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	if d.ID != id || d.BaseURL != h.client.BaseURL() || d.Documents == nil {
		return nil, errors.New("черновик относится к другому движку или повреждён")
	}
	return &d, nil
}
func (h *webHub) writeDraft(d *editorDraft) error {
	if err := os.MkdirAll(h.draftDir(), sessionDirPerm); err != nil {
		return err
	}
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return writeSecretFile(filepath.Join(h.draftDir(), d.ID+".json"), data)
}
func (h *webHub) registerDraftRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/admin/drafts/open", h.httpOpenDraft)
	mux.HandleFunc("GET /api/v1/admin/drafts/{draft}", h.httpGetDraft)
	mux.HandleFunc("PATCH /api/v1/admin/drafts/{draft}/documents/{document}", h.httpPatchDraft)
	mux.HandleFunc("POST /api/v1/admin/drafts/{draft}/documents/{document}/publish", h.httpPublishDraft)
	mux.HandleFunc("POST /api/v1/admin/drafts/{draft}/chat", h.httpDraftChat)
}
func draftError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, fs.ErrNotExist) {
		status = http.StatusNotFound
	}
	if errors.Is(err, errDraftConflict) {
		status = http.StatusConflict
	}
	webError(w, status, "%v", err)
}

type openDraftBody struct {
	DraftID    string         `json:"draft_id"`
	GameID     int            `json:"game_id"`
	Kind       string         `json:"kind"`
	LevelID    int            `json:"level_id"`
	DocumentID string         `json:"document_id"`
	Params     map[string]any `json:"params"`
}

func (h *webHub) httpOpenDraft(w http.ResponseWriter, r *http.Request) {
	var b openDraftBody
	if !webReadJSON(w, r, &b) {
		return
	}
	if (b.Kind != "game" && b.Kind != "level") || b.GameID < 0 || b.LevelID < 0 {
		webError(w, 400, "неверный вид черновика")
		return
	}
	h.draftMu.Lock()
	defer h.draftMu.Unlock()
	var d *editorDraft
	var err error
	if b.DraftID != "" {
		d, err = h.readDraft(b.DraftID)
		if err != nil {
			draftError(w, err)
			return
		}
		if b.GameID != d.GameID {
			webError(w, 400, "игра не соответствует черновику")
			return
		}
	} else if b.GameID > 0 {
		entries, e := os.ReadDir(h.draftDir())
		if e != nil && !errors.Is(e, fs.ErrNotExist) {
			draftError(w, e)
			return
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			candidate, e := h.readDraft(strings.TrimSuffix(entry.Name(), ".json"))
			if e == nil && candidate.GameID == b.GameID {
				d = candidate
				break
			}
		}
	}
	if d == nil {
		d = &editorDraft{ID: newChatID(), BaseURL: h.client.BaseURL(), GameID: b.GameID, Documents: map[string]*draftDocument{}}
	}
	var doc *draftDocument
	if b.DocumentID != "" {
		doc = d.Documents[b.DocumentID]
		if doc == nil {
			webError(w, 404, "документ не найден")
			return
		}
	}
	if doc == nil && (b.Kind == "game" || b.LevelID > 0) {
		for _, item := range d.Documents {
			if item.Kind == b.Kind && item.LevelID == b.LevelID {
				doc = item
				break
			}
		}
	}
	if doc == nil {
		if b.Params == nil {
			b.Params = map[string]any{}
		}
		doc = &draftDocument{ID: newChatID(), Kind: b.Kind, LevelID: b.LevelID, Revision: 1, Params: b.Params, Base: maps.Clone(b.Params)}
		d.Documents[doc.ID] = doc
	}
	d.Active = doc.ID
	if err := h.writeDraft(d); err != nil {
		draftError(w, err)
		return
	}
	webWriteJSON(w, 200, d)
}
func (h *webHub) httpGetDraft(w http.ResponseWriter, r *http.Request) {
	h.draftMu.Lock()
	defer h.draftMu.Unlock()
	d, err := h.readDraft(r.PathValue("draft"))
	if err != nil {
		draftError(w, err)
		return
	}
	webWriteJSON(w, 200, d)
}

type patchDraftBody struct {
	Revision int            `json:"revision"`
	Params   map[string]any `json:"params"`
}

func (h *webHub) patchDraft(id, document string, b patchDraftBody) (*editorDraft, error) {
	d, err := h.readDraft(id)
	if err != nil {
		return nil, err
	}
	doc := d.Documents[document]
	if doc == nil {
		return nil, errors.New("документ не найден")
	}
	if doc.Revision != b.Revision {
		return nil, errDraftConflict
	}
	props := draftProperties(doc.Kind)
	for name, value := range b.Params {
		// JSON keys are field names; reject unknown fields to surface model typos.
		if _, ok := props[name]; !ok {
			return nil, fmt.Errorf("неизвестное поле %s", name)
		}
		if value == nil {
			doc.Params[name] = ""
		} else {
			doc.Params[name] = value
		}
	}
	doc.Revision++
	if err := h.writeDraft(d); err != nil {
		return nil, err
	}
	return d, nil
}
func (h *webHub) httpPatchDraft(w http.ResponseWriter, r *http.Request) {
	var b patchDraftBody
	if !webReadJSON(w, r, &b) {
		return
	}
	h.draftMu.Lock()
	defer h.draftMu.Unlock()
	d, err := h.patchDraft(r.PathValue("draft"), r.PathValue("document"), b)
	if err != nil {
		draftError(w, err)
		return
	}
	webWriteJSON(w, 200, d)
}

// engineDraftParams converts empty form values to the library's explicit clear
// semantics. Draft storage itself also accepts incomplete/temporarily invalid fields.
func engineDraftParams(doc *draftDocument) map[string]any {
	out := maps.Clone(doc.Params)
	clear := []string{}
	if raw, ok := out["clear"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				clear = append(clear, s)
			}
		}
	}
	for name, value := range out {
		if value == "" {
			delete(out, name)
			textParam := dzzzr.GameTextParam
			if doc.Kind == "level" {
				textParam = dzzzr.LevelTextParam
			}
			if base, ok := doc.Base[name]; ok && base != "" && base != nil && textParam(name) {
				clear = append(clear, name)
			}
		}
	}
	if len(clear) > 0 {
		slices.Sort(clear)
		out["clear"] = slices.Compact(clear)
	} else {
		delete(out, "clear")
	}
	return out
}
func (h *webHub) httpPublishDraft(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	var b patchDraftBody
	if !webReadJSON(w, r, &b) {
		return
	}
	h.draftMu.Lock()
	defer h.draftMu.Unlock()
	d, err := h.readDraft(r.PathValue("draft"))
	if err != nil {
		draftError(w, err)
		return
	}
	doc := d.Documents[r.PathValue("document")]
	if doc == nil {
		webError(w, 404, "документ не найден")
		return
	}
	if doc.Revision != b.Revision {
		draftError(w, errDraftConflict)
		return
	}
	params := engineDraftParams(doc)
	raw, err := json.Marshal(params)
	if err != nil {
		draftError(w, err)
		return
	}
	if doc.Kind == "game" {
		var p dzzzr.GameParams
		if err = json.Unmarshal(raw, &p); err == nil {
			if problems := webValidateGameParams(p); len(problems) > 0 {
				err = errors.New(strings.Join(problems, "\n"))
			} else if d.GameID == 0 {
				d.GameID, err = h.client.AdminCreateGame(r.Context(), p)
			} else {
				err = h.client.AdminUpdateGame(r.Context(), d.GameID, p)
			}
		}
	} else {
		var p dzzzr.LevelParams
		if d.GameID == 0 {
			webError(w, 400, "сначала сохраните игру")
			return
		}
		if err = json.Unmarshal(raw, &p); err == nil {
			plan := gamesource.Plan{GameID: d.GameID, Mode: "create", Levels: []gamesource.Level{{Params: p}}}
			if err = plan.Validate(); err == nil {
				if doc.LevelID == 0 {
					doc.LevelID, err = h.client.AdminCreateLevel(r.Context(), d.GameID, p)
				} else {
					err = h.client.AdminUpdateLevel(r.Context(), d.GameID, doc.LevelID, p)
				}
			}
		}
	}
	if err != nil {
		draftError(w, err)
		return
	}
	if fields, ok := params["clear"].([]string); ok {
		for _, name := range fields {
			doc.Params[name] = ""
		}
	}
	doc.Params["clear"] = []any{}
	doc.Base = maps.Clone(doc.Params)
	doc.Revision++
	if err = h.writeDraft(d); err != nil {
		webError(w, 500, "движок принял изменения, но черновик не сохранён: %v; проверьте игру перед повтором", err)
		return
	}
	webWriteJSON(w, 200, d)
}
