package main

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/gamesource"
)

// The whole-scenario half of the editor: one file in, one file out, plus the
// seam that hands what the author is looking at over to the agent. Everything
// here answers with the same documents the command line reads and writes, so
// «dzzzr admin-import-scenario» accepts a file the browser saved and the
// browser accepts a file the command line exported.

// httpAdminUploadFile stores one file in a game's directory through the
// engine's file manager, the same way «dzzzr admin-upload-file» does.
//
// The engine never overwrites a file that is already there: already_exists
// true means nothing was written, and the author has to either pick another
// name or satisfy themselves that the stored content is the one they meant.
// The URL is returned in both cases, because it is the same URL either way.
func (h *webHub) httpAdminUploadFile(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	gameID, ok := adminGameID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, dzzzr.MaxAdminFileBytes)
	if err := r.ParseMultipartForm(dzzzr.MaxAdminFileBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			webError(w, http.StatusRequestEntityTooLarge, "файл больше %d байт: движок принимает не больше", dzzzr.MaxAdminFileBytes)
			return
		}
		webError(w, http.StatusBadRequest, "форма повреждена: %v", err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		webError(w, http.StatusBadRequest, "нужно поле формы «file»")
		return
	}
	defer func() { _ = file.Close() }()

	// One byte over the limit is read deliberately: a body that only became
	// too large after the multipart header must be refused with the same
	// message as one that was too large from the start.
	data, err := io.ReadAll(io.LimitReader(file, dzzzr.MaxAdminFileBytes+1))
	if err != nil {
		webError(w, http.StatusBadRequest, "файл не прочитан: %v", err)
		return
	}
	if len(data) > dzzzr.MaxAdminFileBytes {
		webError(w, http.StatusRequestEntityTooLarge, "файл больше %d байт: движок принимает не больше", dzzzr.MaxAdminFileBytes)
		return
	}

	// The name the file gets in the game's directory. The browser may name it
	// itself; both that name and the uploaded one are client-supplied, so both
	// go through the same sanitizer the chat attachments use.
	name := sanitizeUploadFilename(header.Filename)
	if chosen := strings.TrimSpace(r.FormValue("name")); chosen != "" {
		name = sanitizeUploadFilename(chosen)
	}

	result, err := h.client.AdminUploadFile(r.Context(), gameID, name, data)
	if err != nil {
		// The file manager's own refusal is an answer, not a breakdown: it
		// carries the engine's status token and message key, and repeating
		// them is worth more to the author than «внутренняя ошибка».
		var refused *dzzzr.AdminFileError
		if errors.As(err, &refused) {
			webError(w, http.StatusBadGateway, "движок отклонил загрузку %s: %v", name, refused)
			return
		}
		webError(w, adminStatusCode(err), "файл %s не загружен в игру %d: %v", name, gameID, err)
		return
	}
	webWriteJSON(w, http.StatusCreated, result)
}

// httpAdminExportScenario answers with the scenario document itself rather
// than with a JSON envelope around it: what the browser saves has to be the
// file «dzzzr admin-import-scenario» takes unchanged.
//
// ?linked=1 keeps assets as URLs instead of downloading and embedding them,
// exactly as the trailing «linked» argument of the command does.
func (h *webHub) httpAdminExportScenario(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	gameID, ok := adminGameID(w, r)
	if !ok {
		return
	}
	linked := r.URL.Query().Get("linked") == "1"
	s, err := gamesource.ExportScenario(r.Context(), h.client, gameID, gamesource.ExportOptions{
		BaseURL:      h.client.BaseURL(),
		LinkedAssets: linked,
	})
	if err != nil {
		webError(w, adminStatusCode(err), "сценарий игры %d не выгружен: %v", gameID, err)
		return
	}
	// EncodeScenario decodes what it produced, so a document that reaches the
	// browser is one the importer can read back.
	data, err := gamesource.EncodeScenario(s)
	if err != nil {
		webError(w, http.StatusInternalServerError, "сценарий игры %d не закодирован: %v", gameID, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="scenario-%d.json"`, gameID))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// adminScenarioBody is the body both scenario endpoints take. Scenario is the
// document; the destination fields are read by the import alone.
type adminScenarioBody struct {
	Scenario jsontext.Value `json:"scenario"`
	GameID   int            `json:"game_id"`
	LevelIDs map[string]int `json:"level_ids"`
}

// readScenarioDocument reads the request body and returns the scenario object
// out of it alongside the rest of the body.
//
// Two shapes are accepted: {"scenario":{…}} and the bare scenario object. The
// wrapped one wins whenever it is there, because it is the only one that can
// also carry a destination, and a file dropped straight into the request is
// then still understood — that file is exactly what the export produced.
//
// The body is read here rather than through webReadJSON: that helper stops at
// one megabyte, which is ample for every other request of the editor and far
// too small for a scenario carrying embedded files.
func readScenarioDocument(w http.ResponseWriter, r *http.Request) ([]byte, adminScenarioBody, bool) {
	defer func() { _ = r.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(r.Body, gamesource.MaxScenarioBytes+1))
	if err != nil {
		webError(w, http.StatusBadRequest, "тело запроса не прочитано")
		return nil, adminScenarioBody{}, false
	}
	if len(raw) > gamesource.MaxScenarioBytes {
		webError(w, http.StatusRequestEntityTooLarge, "сценарий больше %d байт", gamesource.MaxScenarioBytes)
		return nil, adminScenarioBody{}, false
	}
	if len(raw) == 0 {
		webError(w, http.StatusBadRequest, "пустое тело запроса")
		return nil, adminScenarioBody{}, false
	}
	var body adminScenarioBody
	if err := json.Unmarshal(raw, &body); err != nil {
		webError(w, http.StatusBadRequest, "неверный JSON: %v", err)
		return nil, adminScenarioBody{}, false
	}
	doc := []byte(body.Scenario)
	if len(doc) == 0 {
		doc = raw
	}
	return doc, body, true
}

// scenarioErrorLines turns one error into the list the editor shows. The
// validators wrap the reason they found into a single error, and a decoder
// error may run over several lines; handing the browser one joined blob makes
// it choose between showing a wall of text and hiding the part that matters.
func scenarioErrorLines(err error) []string {
	out := []string{}
	for line := range strings.SplitSeq(err.Error(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		out = append(out, "сценарий не прошёл проверку")
	}
	return out
}

// adminScenarioCheck is what a validation answers with. It is a verdict, not
// an outcome: a scenario the checks refused still produced an answer, so the
// status stays 200 and ok carries the verdict.
type adminScenarioCheck struct {
	OK     bool     `json:"ok"`
	Errors []string `json:"errors"`
	Levels int      `json:"levels"`
	Assets int      `json:"assets"`
}

// httpAdminValidateScenario checks a scenario without touching the engine.
// Validation is local — the document says everything the checks read — so this
// is the one editor endpoint that asks for no organizer credentials: an author
// may check a file before signing in, and refusing to would only teach them to
// sign in before finding out the file is broken.
//
// Payloads behind an asset URL are not fetched here; they are read and checked
// by the import, the same split «dzzzr admin-validate-scenario» makes.
func (h *webHub) httpAdminValidateScenario(w http.ResponseWriter, r *http.Request) {
	doc, _, ok := readScenarioDocument(w, r)
	if !ok {
		return
	}
	s, err := gamesource.DecodeScenario(doc)
	if err != nil {
		webWriteJSON(w, http.StatusOK, adminScenarioCheck{Errors: scenarioErrorLines(fmt.Errorf("сценарий не разобран: %w", err))})
		return
	}
	if err := gamesource.ValidateScenario(s); err != nil {
		webWriteJSON(w, http.StatusOK, adminScenarioCheck{
			Errors: scenarioErrorLines(fmt.Errorf("сценарий не прошёл проверку: %w", err)),
			Levels: len(s.Levels),
			Assets: len(s.Assets),
		})
		return
	}
	webWriteJSON(w, http.StatusOK, adminScenarioCheck{OK: true, Errors: []string{}, Levels: len(s.Levels), Assets: len(s.Assets)})
}

// adminImportFailure is a failed import: the error and everything that was
// nevertheless written, in the same shape the success answer carries.
type adminImportFailure struct {
	*gamesource.ScenarioImportResult
	Error string `json:"error"`
}

// httpAdminImportScenario writes a whole scenario into a new game (game_id 0
// or absent) or into an explicit one. A nonempty destination needs a complete
// level_ids map: the importer never guesses which level is which.
//
// An import that fails partway is not a no-op and there is nothing to roll it
// back with. The error alone would tell the author that something went wrong
// and nothing about what is now in their game, so the result is answered
// alongside it — game_id, created_game, level_ids, the levels that completed,
// the assets that uploaded, and the stage and key it stopped at — and the
// status is non-2xx so the browser still treats it as a failure.
//
// Assets must arrive embedded (data_base64) or reachable by URL: a scenario
// posted over HTTP has no directory beside it, so an asset named only by a
// relative path cannot be resolved here the way the command line resolves it.
func (h *webHub) httpAdminImportScenario(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w) {
		return
	}
	doc, body, ok := readScenarioDocument(w, r)
	if !ok {
		return
	}
	s, err := gamesource.DecodeScenario(doc)
	if err != nil {
		webError(w, http.StatusBadRequest, "сценарий не разобран: %v", err)
		return
	}
	if err := gamesource.ValidateScenario(s); err != nil {
		webError(w, http.StatusBadRequest, "сценарий не прошёл проверку: %v", err)
		return
	}
	result, importErr := gamesource.ImportScenario(r.Context(), h.client, s, gamesource.ImportOptions{
		GameID:   body.GameID,
		LevelIDs: body.LevelIDs,
		Files:    gamesource.ScenarioFiles{},
	})
	if importErr != nil {
		webWriteJSON(w, adminStatusCode(importErr), adminImportFailure{
			ScenarioImportResult: result,
			Error:                fmt.Sprintf("импорт остановлен: %v", importErr),
		})
		return
	}
	webWriteJSON(w, http.StatusOK, result)
}

// adminHandoffBody is the body of POST /api/v1/admin/handoff. Only game_id is
// required: an author may hand over a whole game as readily as one level.
type adminHandoffBody struct {
	GameID  int    `json:"game_id"`
	LevelID int    `json:"level_id"`
	Note    string `json:"note"`
}

// httpAdminHandoff opens a chat about what the editor is showing and seeds it
// with the context, so the author can carry on with the agent instead of
// re-describing the game to it.
//
// The run is deliberately not started: the seeded line is the author's, and
// they send it when they are ready, from the chat they were switched to.
//
// Organizer credentials are not required. With them the names of the game and
// the level are read and put in the message; without them the identifiers go
// in alone — an author who has not signed in yet still gets their chat.
func (h *webHub) httpAdminHandoff(w http.ResponseWriter, r *http.Request) {
	var body adminHandoffBody
	if !webReadJSON(w, r, &body) {
		return
	}
	if body.GameID <= 0 {
		webError(w, http.StatusBadRequest, "укажите номер игры")
		return
	}
	if body.LevelID < 0 {
		webError(w, http.StatusBadRequest, "неверный номер задания")
		return
	}

	snap := h.store.create(h.cfg.city, h.defaultPolicy())
	busy, ok := h.store.appendUser(snap.ID, h.handoffMessage(r.Context(), body))
	if busy || !ok {
		webError(w, http.StatusInternalServerError, "чат создан, но сообщение не записано")
		return
	}
	_ = h.store.persist(snap.ID)
	snap, _ = h.store.get(snap.ID)
	webWriteJSON(w, http.StatusCreated, snap)
}

// handoffMessage writes the context line on the server rather than in the
// browser: the editor and the agent must not be able to disagree about which
// game is meant, and only one of them can be made to say what the engine says.
//
// A name that cannot be read is left out rather than guessed at, and a read
// that fails does not fail the handoff: the identifiers alone still name the
// game unambiguously.
func (h *webHub) handoffMessage(ctx context.Context, body adminHandoffBody) string {
	h.clientMu.Lock()
	hasAdmin := h.client.HasAdminCredentials()
	h.clientMu.Unlock()

	gameName, levelTitle := "", ""
	if hasAdmin {
		if info, err := h.client.AdminGetGame(ctx, body.GameID); err != nil {
			h.cfg.debugf("передача из редактора: игра %d не прочитана: %v", body.GameID, err)
		} else if info != nil {
			gameName = strings.TrimSpace(info.Params.Name)
		}
		if body.LevelID > 0 {
			if info, err := h.client.AdminGetLevel(ctx, body.GameID, body.LevelID); err != nil {
				h.cfg.debugf("передача из редактора: задание %d не прочитано: %v", body.LevelID, err)
			} else if info != nil {
				levelTitle = strings.TrimSpace(info.Params.Title)
			}
		}
	}

	var b strings.Builder
	// The first line also becomes the chat's title, so it names the game.
	_, _ = fmt.Fprintf(&b, "Передача из редактора: игра %d%s\n", body.GameID, quotedName(gameName))
	_, _ = fmt.Fprintf(&b, "Город: %s\n", h.cfg.city)
	if body.LevelID > 0 {
		_, _ = fmt.Fprintf(&b, "Задание: %d%s\n", body.LevelID, quotedName(levelTitle))
	}
	if note := strings.TrimSpace(body.Note); note != "" {
		_, _ = fmt.Fprintf(&b, "Заметка автора: %s\n", note)
	}
	return b.String()
}

// quotedName renders a name in Russian quotes, or nothing when there is none.
func quotedName(name string) string {
	if name == "" {
		return ""
	}
	return " «" + name + "»"
}
