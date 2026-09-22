package main

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// The editor answers under /api/v1/admin/. It is the manual counterpart of the
// admin_* tools the agent calls: both go through the same dzzzr.Client, the
// same organizer session and the same parameter names, so an author can lay a
// level out by hand, hand the game to the agent, and take it back.

// registerAdminRoutes wires the editor onto the same mux as the chat.
func (h *webHub) registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/status", h.httpAdminStatus)
	mux.HandleFunc("POST /api/v1/admin/login", h.httpAdminLogin)
	mux.HandleFunc("POST /api/v1/admin/logout", h.httpAdminLogout)
	mux.HandleFunc("GET /api/v1/admin/schema", h.httpAdminSchema)
	mux.HandleFunc("POST /api/v1/admin/validate", h.httpAdminValidate)
	mux.HandleFunc("POST /api/v1/admin/handoff", h.httpAdminHandoff)

	mux.HandleFunc("GET /api/v1/admin/games", h.httpAdminListGames)
	mux.HandleFunc("POST /api/v1/admin/games", h.httpAdminCreateGame)
	mux.HandleFunc("GET /api/v1/admin/games/{gameID}", h.httpAdminGetGame)
	mux.HandleFunc("PATCH /api/v1/admin/games/{gameID}", h.httpAdminUpdateGame)
	mux.HandleFunc("DELETE /api/v1/admin/games/{gameID}", h.httpAdminDeleteGame)
	mux.HandleFunc("POST /api/v1/admin/games/{gameID}/copy", h.httpAdminCopyGame)

	mux.HandleFunc("GET /api/v1/admin/games/{gameID}/levels", h.httpAdminListLevels)
	mux.HandleFunc("POST /api/v1/admin/games/{gameID}/levels", h.httpAdminCreateLevel)
	mux.HandleFunc("GET /api/v1/admin/games/{gameID}/levels/{levelID}", h.httpAdminGetLevel)
	mux.HandleFunc("PATCH /api/v1/admin/games/{gameID}/levels/{levelID}", h.httpAdminUpdateLevel)
	mux.HandleFunc("DELETE /api/v1/admin/games/{gameID}/levels/{levelID}", h.httpAdminDeleteLevel)
	mux.HandleFunc("POST /api/v1/admin/games/{gameID}/levels/{levelID}/move", h.httpAdminMoveLevel)

	mux.HandleFunc("POST /api/v1/admin/games/{gameID}/files", h.httpAdminUploadFile)
	mux.HandleFunc("GET /api/v1/admin/games/{gameID}/scenario", h.httpAdminExportScenario)
	mux.HandleFunc("POST /api/v1/admin/scenario/validate", h.httpAdminValidateScenario)
	mux.HandleFunc("POST /api/v1/admin/scenario/import", h.httpAdminImportScenario)
}

// adminStatus is what the editor shows about the organizer account.
type adminStatus struct {
	City     string `json:"city"`
	Login    string `json:"login"`
	HasAdmin bool   `json:"has_admin"`
	// AdminURL is the address of the engine's own administration page. A
	// level's текст задания carries relative links — «../../uploaded/…» is how
	// the engine writes a picture — and they only resolve against that page.
	// The editor needs it to show those pictures instead of broken ones; what
	// it stores stays relative.
	AdminURL string `json:"admin_url"`
}

// adminStatusNow reports the organizer the client carries. The caller must
// hold clientMu.
func (h *webHub) adminStatusNow() adminStatus {
	return adminStatus{
		City:     h.cfg.city,
		Login:    h.client.AdminLogin(),
		HasAdmin: h.client.HasAdminCredentials(),
		AdminURL: h.client.BaseURL() + "admin/",
	}
}

func (h *webHub) httpAdminStatus(w http.ResponseWriter, r *http.Request) {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()
	webWriteJSON(w, http.StatusOK, h.adminStatusNow())
}

// adminLoginBody is the body of POST /api/v1/admin/login.
type adminLoginBody struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// httpAdminLogin stores the organizer credentials and proves they work before
// reporting success: the admin area answers 401 to a wrong password, and a
// silent failure here would only surface later as an unexplained empty game
// list.
func (h *webHub) httpAdminLogin(w http.ResponseWriter, r *http.Request) {
	var body adminLoginBody
	if !webReadJSON(w, r, &body) {
		return
	}
	status, code, err := h.loginOrganizer(r.Context(), body.Login, body.Password)
	if err != nil {
		webError(w, code, "%v", err)
		return
	}
	webWriteJSON(w, http.StatusOK, status)
}

// loginOrganizer stores the organizer credentials once the engine accepted
// them, and reports the HTTP status a refusal deserves or the organizer status
// read under the same lock as the save. The onboarding wizard
// shares it with httpAdminLogin.
func (h *webHub) loginOrganizer(ctx context.Context, login, password string) (adminStatus, int, error) {
	login = strings.TrimSpace(login)
	if login == "" || password == "" {
		return adminStatus{}, http.StatusBadRequest, errors.New("укажите логин и пароль организатора")
	}

	h.clientMu.Lock()
	prevLogin, prevPassword := h.cfg.adminLogin, h.cfg.adminPassword
	h.cfg.adminLogin, h.cfg.adminPassword = login, password
	h.client.SetAdminCredentials(login, password)
	h.clientMu.Unlock()

	// The proof is a request to the engine, and clientMu is the lock every
	// other authentication handler takes: held across the round-trip, one
	// engine that does not answer would stall even the status the browser
	// polls to find out what is happening. The client guards its own state, so
	// the request needs no lock; the lock only comes back to put the previous
	// organizer in place when these credentials are refused.
	if _, err := h.client.AdminListGames(ctx); err != nil {
		h.clientMu.Lock()
		// Put the previous organizer back only if these credentials are still
		// the ones in place. A logout landing while the engine was answering
		// has already dropped them on purpose, and restoring them here would
		// sign the user back in against their own instruction.
		if h.cfg.adminLogin == login {
			h.cfg.adminLogin, h.cfg.adminPassword = prevLogin, prevPassword
			h.client.SetAdminCredentials(prevLogin, prevPassword)
		}
		h.clientMu.Unlock()
		return adminStatus{}, adminStatusCode(err), err
	}

	h.clientMu.Lock()
	defer h.clientMu.Unlock()
	// The session file is what the CLI and the next run read, so an editor
	// login is worth as much as «dzzzr -admin-login … admin-games» was.
	if _, err := saveSession(h.cfg, h.client); err != nil {
		h.cfg.debugf("сессия не сохранена: %v", err)
	}
	return h.adminStatusNow(), http.StatusOK, nil
}

// httpAdminLogout forgets the organizer both in memory and in the session
// file: leaving the password on disk would sign the next run back in.
func (h *webHub) httpAdminLogout(w http.ResponseWriter, r *http.Request) {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()

	h.cfg.adminLogin, h.cfg.adminPassword = "", ""
	h.client.SetAdminCredentials("", "")
	if _, err := saveSession(h.cfg, h.client); err != nil {
		h.cfg.debugf("сессия не сохранена: %v", err)
	}
	webWriteJSON(w, http.StatusOK, h.adminStatusNow())
}

// adminStatusCode separates credentials the admin area refused from an engine
// that could not be reached, so the editor can say which of the two happened.
func adminStatusCode(err error) int {
	switch dzzzr.AuthErrorKindOf(err) {
	case dzzzr.AuthAdmin:
		return http.StatusUnauthorized
	case dzzzr.AuthAdminScope:
		return http.StatusForbidden
	}
	return http.StatusBadGateway
}

// requireAdmin reports whether the request may reach the administration area,
// answering the browser itself when it may not. The message names the two ways
// out — the editor's own login panel and the command-line flags — because a
// bare 403 leaves the author guessing.
func (h *webHub) requireAdmin(w http.ResponseWriter) bool {
	h.clientMu.Lock()
	ok := h.client.HasAdminCredentials()
	h.clientMu.Unlock()
	if !ok {
		webError(w, http.StatusForbidden,
			"нет учётных данных организатора: войдите в редакторе или запустите «dzzzr web -admin-login … -admin-password …»")
		return false
	}
	return true
}

// adminPathInt reads one positive identifier out of the path.
func adminPathInt(r *http.Request, name string) (int, error) {
	raw := r.PathValue(name)
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("неверный идентификатор %q", raw)
	}
	return n, nil
}

// adminGameID reads {gameID}, answering the browser itself when it is not a
// number.
func adminGameID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := adminPathInt(r, "gameID")
	if err != nil {
		webError(w, http.StatusBadRequest, "%v", err)
		return 0, false
	}
	return id, true
}

// adminLevelID reads {levelID} the same way.
func adminLevelID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := adminPathInt(r, "levelID")
	if err != nil {
		webError(w, http.StatusBadRequest, "%v", err)
		return 0, false
	}
	return id, true
}

// decodeParams turns the editor's params object into the typed structure the
// client takes. Unknown members are refused rather than dropped, the way
// decodeAdminParams refuses them for the agent: a misspelled field that is
// silently ignored looks exactly like a field the engine did not save.
func decodeParams[T any](w http.ResponseWriter, raw jsontext.Value, dst *T) bool {
	if len(raw) == 0 {
		webError(w, http.StatusBadRequest, "нужен объект params")
		return false
	}
	if err := json.Unmarshal(raw, dst, json.RejectUnknownMembers(true)); err != nil {
		webError(w, http.StatusBadRequest, "params: %v", err)
		return false
	}
	return true
}

// adminParamsBody is the body every write of the editor sends.
type adminParamsBody struct {
	Params jsontext.Value `json:"params"`
}

// webAdminGame is one row of the editor's game list.
type webAdminGame struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Date      string `json:"date"`
	Number    string `json:"number"`
	Status    string `json:"status"`
	Season    string `json:"season"`
	Finished  bool   `json:"finished"`
	Published bool   `json:"published"`
}

func (h *webHub) httpAdminListGames(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	games, err := h.client.AdminListGames(r.Context())
	if err != nil {
		webError(w, adminStatusCode(err), "не удалось получить список игр: %v", err)
		return
	}
	items := make([]webAdminGame, 0, len(games))
	for _, g := range games {
		items = append(items, webAdminGame{
			ID:        g.ID,
			Name:      g.Name,
			Date:      g.Date,
			Number:    g.Number,
			Status:    g.Status,
			Season:    g.Season,
			Finished:  g.Finished,
			Published: g.Published,
		})
	}
	webWriteJSON(w, http.StatusOK, map[string]any{"games": items})
}

func (h *webHub) httpAdminGetGame(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	id, ok := adminGameID(w, r)
	if !ok {
		return
	}
	info, err := h.client.AdminGetGame(r.Context(), id)
	if err != nil {
		webError(w, adminStatusCode(err), "не удалось прочитать игру %d: %v", id, err)
		return
	}
	webWriteJSON(w, http.StatusOK, map[string]any{"id": info.ID, "params": info.Params})
}

func (h *webHub) httpAdminCreateGame(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	var body adminParamsBody
	if !webReadJSON(w, r, &body) {
		return
	}
	var params dzzzr.GameParams
	if !decodeParams(w, body.Params, &params) {
		return
	}
	id, err := h.client.AdminCreateGame(r.Context(), params)
	if err != nil {
		webError(w, adminStatusCode(err), "игра не создана: %v", err)
		return
	}
	webWriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (h *webHub) httpAdminUpdateGame(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	id, ok := adminGameID(w, r)
	if !ok {
		return
	}
	var body adminParamsBody
	if !webReadJSON(w, r, &body) {
		return
	}
	var params dzzzr.GameParams
	if !decodeParams(w, body.Params, &params) {
		return
	}
	if err := h.client.AdminUpdateGame(r.Context(), id, params); err != nil {
		webError(w, adminStatusCode(err), "игра %d не сохранена: %v", id, err)
		return
	}
	webWriteJSON(w, http.StatusOK, map[string]any{"id": id, "status": "saved"})
}

func (h *webHub) httpAdminDeleteGame(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	id, ok := adminGameID(w, r)
	if !ok {
		return
	}
	if err := h.client.AdminDeleteGame(r.Context(), id); err != nil {
		webError(w, adminStatusCode(err), "игра %d не удалена: %v", id, err)
		return
	}
	webWriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

// adminCopyBody is the body of POST /api/v1/admin/games/{gameID}/copy.
type adminCopyBody struct {
	WithLevels bool `json:"with_levels"`
}

func (h *webHub) httpAdminCopyGame(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	id, ok := adminGameID(w, r)
	if !ok {
		return
	}
	var body adminCopyBody
	if !webReadJSON(w, r, &body) {
		return
	}
	newID, err := h.client.AdminCopyGame(r.Context(), id, body.WithLevels)
	if err != nil {
		webError(w, adminStatusCode(err), "игра %d не скопирована: %v", id, err)
		return
	}
	webWriteJSON(w, http.StatusCreated, map[string]any{"id": newID})
}
