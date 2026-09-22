//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The editor's end-to-end lane. It drives the real browser API of «dzzzr web»
// against the real mock server, over HTTP, with nothing stubbed on either
// side.
//
// The mock is frozen — it is the engine here, not a fixture to bend — and it
// models less of the administration area than the live engine does. What it
// does support is the level table: add_zadanie builds a level out of the
// indexed code/danger/sector/secName/codeB/timeB/codeF/fakeShtraf/spoiler
// fields, update_zadanie rewrites the level's text, codeCount and main codes,
// del_zadanie removes a level and move_up/move_down reorder the list. That is
// the whole path an author takes when laying a game out by hand, so that is
// what this test walks.
//
// What the mock deliberately does NOT model, and what this test therefore does
// not assert (see cmd/dzzzr-mock/README.md and its admin.go):
//   - add_game / del_games / copy_game only fabricate a plausible redirect;
//     the mock hosts one game and creates none, so game creation is covered by
//     the handler tests in cmd/dzzzr instead;
//   - update_zadanie rewrites only title, question, clue1, clue2, codeCount
//     and the main codes — bonus codes, fake codes and spoilers are settable
//     at creation only, so this test reads them back after create, not after
//     update;
//   - the TinyMCE file manager is not mounted at all, so file upload has no
//     route here.
//
// None of these is worked around by touching the mock.

// webHarness is the mock server with «dzzzr web» in front of it.
type webHarness struct {
	t    *testing.T
	base string // http://127.0.0.1:PORT
	http *http.Client
}

// startWeb builds both binaries, runs the mock, and serves «dzzzr web» over it
// with the organizer credentials the mock expects.
func startWeb(t *testing.T) *webHarness {
	t.Helper()
	dir := t.TempDir()
	cli := build(t, dir, "./cmd/dzzzr", "dzzzr")
	mock := build(t, dir, "./cmd/dzzzr-mock", "dzzzr-mock")

	mockAddr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	ctx, cancel := context.WithCancel(context.Background())
	server := exec.CommandContext(ctx, mock, "-addr", mockAddr)
	server.Stderr = os.Stderr
	if err := server.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = server.Wait()
	})
	waitForPort(t, mockAddr)

	home := t.TempDir()
	webAddr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	webCtx, webCancel := context.WithCancel(context.Background())
	web := exec.CommandContext(webCtx, cli,
		"-base-url", "http://"+mockAddr+"/moscow/",
		"-admin-login", "admin", "-admin-password", "admin",
		"web", "-web-addr", webAddr,
	)
	web.Env = append(os.Environ(),
		"HOME="+home,
		"DZZZR_CONFIG_DIR="+filepath.Join(home, ".config", "dzzzr"),
		"DZZZR_CITY=moscow",
		// «dzzzr web» opens the system browser on start; shadowing the opener
		// with a no-op keeps a local run from spawning a window. On CI neither
		// program exists and the call fails harmlessly anyway.
		"PATH="+noopBrowserDir(t)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	web.Stderr = os.Stderr
	if err := web.Start(); err != nil {
		webCancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		webCancel()
		_ = web.Wait()
	})
	waitForPort(t, webAddr)

	return &webHarness{t: t, base: "http://" + webAddr, http: &http.Client{Timeout: 20 * time.Second}}
}

// noopBrowserDir returns a directory holding do-nothing open/xdg-open
// programs, to be put in front of PATH.
func noopBrowserDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		// The Windows opener is rundll32, which is resolved from the system
		// directory rather than from PATH; there is nothing to shadow.
		return t.TempDir()
	}
	dir := t.TempDir()
	for _, name := range []string{"open", "xdg-open"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func waitForPort(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("никто не ответил на %s", addr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// do runs one request against the editor API and decodes the JSON answer.
func (h *webHarness) do(method, path string, body any, out any) int {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, h.base+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := h.http.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			h.t.Fatalf("%s %s: ответ не разобран (%s): %v", method, path, data, err)
		}
	}
	return res.StatusCode
}

type e2eGame struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type e2eLevel struct {
	ID    int    `json:"id"`
	Order int    `json:"order"`
	Title string `json:"title"`
}

// TestWebEditorLevelLifecycle walks one level from creation to deletion the
// way the browser does, against the mock engine.
func TestWebEditorLevelLifecycle(t *testing.T) {
	h := startWeb(t)

	// The organizer credentials came in on the command line, so the editor is
	// already signed in — the browser's own login form is the other way in and
	// is covered by the handler tests.
	var status struct {
		City     string `json:"city"`
		Login    string `json:"login"`
		HasAdmin bool   `json:"has_admin"`
	}
	if code := h.do(http.MethodGet, "/api/v1/admin/status", nil, &status); code != http.StatusOK {
		t.Fatalf("статус организатора: код %d", code)
	}
	if !status.HasAdmin || status.Login != "admin" {
		t.Fatalf("статус = %+v", status)
	}

	// The field description the browser draws its form from must be servable
	// before anything else: an empty schema is an empty editor.
	var schema map[string]struct {
		Form struct {
			Groups []struct {
				Title  string `json:"title"`
				Fields []struct {
					Name string `json:"name"`
				} `json:"fields"`
			} `json:"groups"`
		} `json:"form"`
	}
	if code := h.do(http.MethodGet, "/api/v1/admin/schema", nil, &schema); code != http.StatusOK {
		t.Fatalf("схема полей: код %d", code)
	}
	for _, kind := range []string{"game", "level"} {
		if len(schema[kind].Form.Groups) == 0 {
			t.Fatalf("схема %q пришла без групп полей", kind)
		}
	}

	var games struct {
		Games []e2eGame `json:"games"`
	}
	if code := h.do(http.MethodGet, "/api/v1/admin/games", nil, &games); code != http.StatusOK {
		t.Fatalf("список игр: код %d", code)
	}
	if len(games.Games) == 0 {
		t.Fatal("движок не показал ни одной игры")
	}
	gameID := games.Games[0].ID

	var before struct {
		Levels []e2eLevel `json:"levels"`
	}
	if code := h.do(http.MethodGet, fmt.Sprintf("/api/v1/admin/games/%d/levels", gameID), nil, &before); code != http.StatusOK {
		t.Fatalf("список уровней: код %d", code)
	}
	if len(before.Levels) == 0 {
		t.Fatal("движок не показал ни одного уровня")
	}

	// Creating a level is the whole point of the screen: main codes with
	// difficulty and sector, a sector name, a bonus code with its minutes, a
	// fake code with its penalty and a spoiler all go out in one form.
	create := map[string]any{"params": map[string]any{
		"title":    "Уровень из редактора",
		"question": "Текст задания, набранный руками",
		"hint1":    "Первая подсказка",
		"hint2":    "Вторая подсказка",
		"codes": []map[string]any{
			{"code": "РЕДАКТОР1", "danger": "2", "sector": 1},
			{"code": "РЕДАКТОР2", "synonyms": "РЕД2", "danger": "3", "sector": 2},
		},
		"sector_names": []string{"Первый сектор", "Второй сектор"},
		"bonus_codes":  []map[string]any{{"code": "БОНУС1", "danger": "1", "minutes": 7}},
		"fake_codes":   []map[string]any{{"code": "ЛОЖНЫЙ1", "penalty": 5}},
		"spoilers":     []map[string]any{{"code": "СПОЙЛЕР1", "penalty": 3, "text": "Что видно за спойлером"}},
		"code_count":   2,
	}}
	var created struct {
		ID    int    `json:"id"`
		Error string `json:"error"`
	}
	if code := h.do(http.MethodPost, fmt.Sprintf("/api/v1/admin/games/%d/levels", gameID), create, &created); code != http.StatusCreated {
		t.Fatalf("создание уровня: код %d, ошибка %q", code, created.Error)
	}
	if created.ID <= 0 {
		t.Fatalf("движок не назвал id нового уровня: %+v", created)
	}

	var after struct {
		Levels []e2eLevel `json:"levels"`
	}
	if code := h.do(http.MethodGet, fmt.Sprintf("/api/v1/admin/games/%d/levels", gameID), nil, &after); code != http.StatusOK {
		t.Fatalf("список уровней после создания: код %d", code)
	}
	if len(after.Levels) != len(before.Levels)+1 {
		t.Fatalf("уровней было %d, стало %d", len(before.Levels), len(after.Levels))
	}

	// Reading the level back is what proves the codes reached the engine
	// rather than just the wire.
	var got struct {
		ID     int `json:"id"`
		Params struct {
			Title       string `json:"title"`
			Question    string `json:"question"`
			SectorNames []string
			Codes       []struct {
				Code   string `json:"code"`
				Danger string `json:"danger"`
				Sector int    `json:"sector"`
			} `json:"codes"`
			BonusCodes []struct {
				Code    string `json:"code"`
				Minutes int    `json:"minutes"`
			} `json:"bonus_codes"`
			FakeCodes []struct {
				Code    string `json:"code"`
				Penalty int    `json:"penalty"`
			} `json:"fake_codes"`
			Spoilers []struct {
				Code string `json:"code"`
				Text string `json:"text"`
			} `json:"spoilers"`
		} `json:"params"`
		Error string `json:"error"`
	}
	path := fmt.Sprintf("/api/v1/admin/games/%d/levels/%d", gameID, created.ID)
	if code := h.do(http.MethodGet, path, nil, &got); code != http.StatusOK {
		t.Fatalf("чтение уровня: код %d, ошибка %q", code, got.Error)
	}
	if got.Params.Title != "Уровень из редактора" {
		t.Errorf("название = %q", got.Params.Title)
	}
	if !strings.Contains(got.Params.Question, "набранный руками") {
		t.Errorf("задание = %q", got.Params.Question)
	}
	wantCodes := map[string]bool{"РЕДАКТОР1": false, "РЕДАКТОР2": false}
	for _, c := range got.Params.Codes {
		if _, ok := wantCodes[c.Code]; ok {
			wantCodes[c.Code] = true
		}
	}
	for code, seen := range wantCodes {
		if !seen {
			t.Errorf("основной код %q не вернулся из движка: %+v", code, got.Params.Codes)
		}
	}
	if len(got.Params.BonusCodes) == 0 || got.Params.BonusCodes[0].Code != "БОНУС1" {
		t.Errorf("бонусные коды = %+v", got.Params.BonusCodes)
	}
	if len(got.Params.FakeCodes) == 0 || got.Params.FakeCodes[0].Code != "ЛОЖНЫЙ1" {
		t.Errorf("ложные коды = %+v", got.Params.FakeCodes)
	}
	if len(got.Params.Spoilers) == 0 || got.Params.Spoilers[0].Code != "СПОЙЛЕР1" {
		t.Errorf("спойлеры = %+v", got.Params.Spoilers)
	}

	// Updating rewrites what the mock's update_zadanie models: the text and
	// the main codes.
	update := map[string]any{"params": map[string]any{
		"title":    "Уровень, переименованный в редакторе",
		"question": "Переписанное задание",
		"codes":    []map[string]any{{"code": "НОВЫЙКОД", "danger": "1", "sector": 1}},
	}}
	var saved struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if code := h.do(http.MethodPatch, path, update, &saved); code != http.StatusOK {
		t.Fatalf("сохранение уровня: код %d, ошибка %q", code, saved.Error)
	}
	got.Params.Title = ""
	got.Params.Codes = nil
	if code := h.do(http.MethodGet, path, nil, &got); code != http.StatusOK {
		t.Fatalf("повторное чтение уровня: код %d", code)
	}
	if got.Params.Title != "Уровень, переименованный в редакторе" {
		t.Errorf("название после правки = %q", got.Params.Title)
	}
	foundNew := false
	for _, c := range got.Params.Codes {
		if c.Code == "НОВЫЙКОД" {
			foundNew = true
		}
	}
	if !foundNew {
		t.Errorf("новый код не сохранился: %+v", got.Params.Codes)
	}

	// Moving reports the position before and after, because the engine's own
	// answer to a refused move looks exactly like an accepted one.
	var moved struct {
		OrderBefore int    `json:"order_before"`
		OrderAfter  int    `json:"order_after"`
		Moved       bool   `json:"moved"`
		Error       string `json:"error"`
	}
	if code := h.do(http.MethodPost, path+"/move", map[string]any{"up": true}, &moved); code != http.StatusOK {
		t.Fatalf("перемещение уровня: код %d, ошибка %q", code, moved.Error)
	}
	if !moved.Moved || moved.OrderAfter != moved.OrderBefore-1 {
		t.Fatalf("перемещение вверх: %+v", moved)
	}

	// Deleting takes the level back out, and the list is what proves it.
	var deleted struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if code := h.do(http.MethodDelete, path, nil, &deleted); code != http.StatusOK {
		t.Fatalf("удаление уровня: код %d, ошибка %q", code, deleted.Error)
	}
	var final struct {
		Levels []e2eLevel `json:"levels"`
	}
	if code := h.do(http.MethodGet, fmt.Sprintf("/api/v1/admin/games/%d/levels", gameID), nil, &final); code != http.StatusOK {
		t.Fatalf("список уровней после удаления: код %d", code)
	}
	if len(final.Levels) != len(before.Levels) {
		t.Fatalf("после удаления уровней %d, было до создания %d", len(final.Levels), len(before.Levels))
	}
	for _, l := range final.Levels {
		if l.ID == created.ID {
			t.Fatalf("удалённый уровень %d всё ещё в списке", created.ID)
		}
	}
}

// TestWebEditorValidatesBeforeTheEngine proves the local check answers without
// the engine having to refuse anything: the author sees the problem while the
// level is still only in the browser.
func TestWebEditorValidatesBeforeTheEngine(t *testing.T) {
	h := startWeb(t)

	var bad struct {
		OK     bool     `json:"ok"`
		Errors []string `json:"errors"`
	}
	body := map[string]any{"kind": "level", "params": map[string]any{
		"title":    "Уровень с дублем",
		"question": "Задание",
		"codes": []map[string]any{
			{"code": "ОДИН", "danger": "1"},
			{"code": "ОДИН", "danger": "1"},
		},
	}}
	if code := h.do(http.MethodPost, "/api/v1/admin/validate", body, &bad); code != http.StatusOK {
		t.Fatalf("проверка: код %d", code)
	}
	if bad.OK || len(bad.Errors) == 0 {
		t.Fatalf("повторяющийся код принят: %+v", bad)
	}

	var good struct {
		OK     bool     `json:"ok"`
		Errors []string `json:"errors"`
	}
	body = map[string]any{"kind": "level", "params": map[string]any{
		"title":    "Нормальный уровень",
		"question": "Задание",
		"codes":    []map[string]any{{"code": "ОДИН", "danger": "1"}},
	}}
	if code := h.do(http.MethodPost, "/api/v1/admin/validate", body, &good); code != http.StatusOK {
		t.Fatalf("проверка: код %d", code)
	}
	if !good.OK {
		t.Fatalf("корректный уровень отвергнут: %+v", good.Errors)
	}
}

// TestWebEditorHandsOffToAgent covers the seam between the two views: the
// editor asks the server for a chat carrying the game's context, and the chat
// that comes back is a real one the browser can open.
func TestWebEditorHandsOffToAgent(t *testing.T) {
	h := startWeb(t)

	var games struct {
		Games []e2eGame `json:"games"`
	}
	if code := h.do(http.MethodGet, "/api/v1/admin/games", nil, &games); code != http.StatusOK || len(games.Games) == 0 {
		t.Fatalf("список игр: код %d, игр %d", code, len(games.Games))
	}
	gameID := games.Games[0].ID

	var chat struct {
		ID    string `json:"id"`
		Lines []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"lines"`
		Error string `json:"error"`
	}
	body := map[string]any{"game_id": gameID, "note": "Проверь коды первого уровня"}
	if code := h.do(http.MethodPost, "/api/v1/admin/handoff", body, &chat); code != http.StatusCreated {
		t.Fatalf("передача агенту: код %d, ошибка %q", code, chat.Error)
	}
	if chat.ID == "" {
		t.Fatal("чат создан без идентификатора")
	}
	if len(chat.Lines) == 0 {
		t.Fatal("в чате нет первого сообщения с контекстом")
	}
	first := chat.Lines[0].Content
	if !strings.Contains(first, fmt.Sprint(gameID)) {
		t.Errorf("в контексте нет номера игры: %q", first)
	}
	if !strings.Contains(first, "Проверь коды первого уровня") {
		t.Errorf("в контексте нет заметки автора: %q", first)
	}

	// The chat really exists on the server, not just in the answer.
	var fetched struct {
		ID string `json:"id"`
	}
	if code := h.do(http.MethodGet, "/api/v1/chats/"+chat.ID, nil, &fetched); code != http.StatusOK {
		t.Fatalf("чтение созданного чата: код %d", code)
	}
	if fetched.ID != chat.ID {
		t.Fatalf("прочитан чат %q вместо %q", fetched.ID, chat.ID)
	}
}

// TestWebEditorServesItsOwnInterface keeps the browser files inside the
// binary: a file that is not embedded is a file the author never sees.
func TestWebEditorServesItsOwnInterface(t *testing.T) {
	h := startWeb(t)
	for _, name := range []string{"/", "/editor.js", "/editor.css", "/app.js", "/style.css"} {
		res, err := h.http.Get(h.base + name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: код %d", name, res.StatusCode)
			continue
		}
		if len(body) == 0 {
			t.Errorf("%s: пустой файл", name)
		}
	}
}
