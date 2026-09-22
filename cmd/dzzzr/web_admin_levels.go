package main

import (
	"context"
	"net/http"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// The level half of the editor. Every handler here is the manual counterpart
// of one admin_* level tool: same client, same parameter names, so a level laid
// out by hand and a level written by the agent are the same level.

// webAdminLevel is one row of the editor's level list. It mirrors
// dzzzr.AdminLevel in snake_case, so the browser sees a shape that does not
// move when the client's own struct tags do.
type webAdminLevel struct {
	ID          int      `json:"id"`
	Order       int      `json:"order"`
	Title       string   `json:"title"`
	Kind        string   `json:"kind"`
	Skvoz       bool     `json:"skvoz"`
	Published   bool     `json:"published"`
	Codes       []string `json:"codes"`
	BonusCodes  []string `json:"bonus_codes"`
	FakeCodes   []string `json:"fake_codes"`
	CodeCount   string   `json:"code_count"`
	TryLimit    string   `json:"try_limit"`
	HintTimings string   `json:"hint_timings"`
}

func (h *webHub) httpAdminListLevels(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	gameID, ok := adminGameID(w, r)
	if !ok {
		return
	}
	levels, err := h.client.AdminListLevels(r.Context(), gameID)
	if err != nil {
		webError(w, adminStatusCode(err), "не удалось получить список уровней игры %d: %v", gameID, err)
		return
	}
	items := make([]webAdminLevel, 0, len(levels))
	for _, l := range levels {
		items = append(items, webAdminLevel{
			ID:          l.ID,
			Order:       l.Order,
			Title:       l.Title,
			Kind:        l.Kind,
			Skvoz:       l.Skvoz,
			Published:   l.Published,
			Codes:       l.Codes,
			BonusCodes:  l.BonusCodes,
			FakeCodes:   l.FakeCodes,
			CodeCount:   l.CodeCount,
			TryLimit:    l.TryLimit,
			HintTimings: l.HintTimings,
		})
	}
	webWriteJSON(w, http.StatusOK, map[string]any{"levels": items})
}

// httpAdminGetLevel answers with the level's params under the very names PATCH
// takes back, so the editor can read a level, change one field and send the
// whole thing home again.
func (h *webHub) httpAdminGetLevel(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	gameID, ok := adminGameID(w, r)
	if !ok {
		return
	}
	levelID, ok := adminLevelID(w, r)
	if !ok {
		return
	}
	info, err := h.client.AdminGetLevel(r.Context(), gameID, levelID)
	if err != nil {
		webError(w, adminStatusCode(err), "не удалось прочитать уровень %d игры %d: %v", levelID, gameID, err)
		return
	}
	webWriteJSON(w, http.StatusOK, map[string]any{
		"id":      info.ID,
		"game_id": info.GameID,
		"order":   info.Order,
		"params":  info.Params,
	})
}

func (h *webHub) httpAdminCreateLevel(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	gameID, ok := adminGameID(w, r)
	if !ok {
		return
	}
	var body adminParamsBody
	if !webReadJSON(w, r, &body) {
		return
	}
	var params dzzzr.LevelParams
	if !decodeParams(w, body.Params, &params) {
		return
	}
	id, err := h.client.AdminCreateLevel(r.Context(), gameID, params)
	if err != nil {
		webError(w, adminStatusCode(err), "уровень в игре %d не создан: %v", gameID, err)
		return
	}
	webWriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (h *webHub) httpAdminUpdateLevel(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	gameID, ok := adminGameID(w, r)
	if !ok {
		return
	}
	levelID, ok := adminLevelID(w, r)
	if !ok {
		return
	}
	var body adminParamsBody
	if !webReadJSON(w, r, &body) {
		return
	}
	var params dzzzr.LevelParams
	if !decodeParams(w, body.Params, &params) {
		return
	}
	if err := h.client.AdminUpdateLevel(r.Context(), gameID, levelID, params); err != nil {
		webError(w, adminStatusCode(err), "уровень %d игры %d не сохранён: %v", levelID, gameID, err)
		return
	}
	webWriteJSON(w, http.StatusOK, map[string]any{"id": levelID, "status": "saved"})
}

func (h *webHub) httpAdminDeleteLevel(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	gameID, ok := adminGameID(w, r)
	if !ok {
		return
	}
	levelID, ok := adminLevelID(w, r)
	if !ok {
		return
	}
	if err := h.client.AdminDeleteLevel(r.Context(), gameID, levelID); err != nil {
		webError(w, adminStatusCode(err), "уровень %d игры %d не удалён: %v", levelID, gameID, err)
		return
	}
	webWriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

// adminMoveBody is the body of POST …/levels/{levelID}/move.
type adminMoveBody struct {
	Up bool `json:"up"`
}

// webAdminLevelOrder is the level's position in its game, or false when the
// game's list does not carry it at all.
func (h *webHub) webAdminLevelOrder(ctx context.Context, gameID, levelID int) (int, bool, error) {
	levels, err := h.client.AdminListLevels(ctx, gameID)
	if err != nil {
		return 0, false, err
	}
	for _, l := range levels {
		if l.ID == levelID {
			return l.Order, true, nil
		}
	}
	return 0, false, nil
}

// httpAdminMoveLevel moves a level one position and says what that did. The
// engine answers a refused move — the first level up, the last one down — with
// exactly the redirect it answers an accepted one, so the position is read on
// both sides, the same way the admin_move_level tool reads it.
func (h *webHub) httpAdminMoveLevel(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	gameID, ok := adminGameID(w, r)
	if !ok {
		return
	}
	levelID, ok := adminLevelID(w, r)
	if !ok {
		return
	}
	var body adminMoveBody
	if !webReadJSON(w, r, &body) {
		return
	}
	before, found, err := h.webAdminLevelOrder(r.Context(), gameID, levelID)
	if err != nil {
		webError(w, adminStatusCode(err), "не удалось получить список уровней игры %d: %v", gameID, err)
		return
	}
	if !found {
		webError(w, http.StatusNotFound, "уровень %d не найден в игре %d", levelID, gameID)
		return
	}
	if err := h.client.AdminMoveLevel(r.Context(), levelID, body.Up); err != nil {
		webError(w, adminStatusCode(err), "уровень %d не передвинут: %v", levelID, err)
		return
	}
	after, found, err := h.webAdminLevelOrder(r.Context(), gameID, levelID)
	if err != nil {
		webError(w, adminStatusCode(err), "не удалось получить список уровней игры %d: %v", gameID, err)
		return
	}
	if !found {
		webError(w, http.StatusNotFound, "уровень %d не найден в игре %d после перемещения", levelID, gameID)
		return
	}
	webWriteJSON(w, http.StatusOK, map[string]any{
		"order_before": before,
		"order_after":  after,
		"moved":        before != after,
	})
}
