package main

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// adminEngine is a stand-in for the administration area: it serves the
// library's own HTML fixtures for the pages the commands read and records
// every form they post, answering with the redirect the real engine sends.
type adminEngine struct {
	t *testing.T

	mu    sync.Mutex
	posts []url.Values
	gets  []url.Values
	paths []string
	// deleted records levels removed through del_zadanie, so the level list
	// stops listing them. Without this the fake engine answered a refused
	// delete exactly like a successful one, which is what let a silently
	// ignored delete pass for working.
	deleted map[string]bool
	// refuseDelete makes the engine accept a del_zadanie and keep the level,
	// which is what the live engine does while the game's date is unset.
	refuseDelete bool
	// moves records accepted move_up/move_down requests so the level list
	// comes back reordered. Without it the list never changed and a command
	// that verifies its own effect could not tell a move from a refusal.
	moves []levelMove
	// refuseMove makes the engine accept a move and reorder nothing.
	refuseMove bool
}

// levelRowNumberRe finds a level row's position number, which the engine
// rewrites whenever the order changes.
var levelRowNumberRe = regexp.MustCompile(`(<span[^>]*>)\s*\d+\s*</span>`)

// levelMove is one accepted reorder request.
type levelMove struct {
	id string
	up bool
}

// adminFixtures maps a page to the fixture that answers it: the value of
// ?action= for admin/, and "path:<file>.php" for the separate scripts.
var adminFixtures = map[string]string{
	"games":            "admin_games_list.html",
	"zadanie":          "admin_levels_list.html",
	"teams":            "admin_teams_list.html",
	"path:gmAdmin.php": "admin_gm.html",
	"path:lookLog.php": "admin_looklog.html",
	"path:gmcht.php":   "admin_gmcht.html",
}

func newAdminEngine(t *testing.T) (*adminEngine, string) {
	t.Helper()
	e := &adminEngine{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/moscow/admin/", func(w http.ResponseWriter, r *http.Request) {
		if !e.authorized(w, r) {
			return
		}
		e.mu.Lock()
		e.paths = append(e.paths, r.URL.Path)
		e.mu.Unlock()
		if r.Method == http.MethodPost {
			form, err := readAdminForm(r)
			if err != nil {
				e.t.Errorf("admin: разбор формы: %v", err)
			}
			e.mu.Lock()
			switch form.Get("action") {
			case "del_zadanie":
				if !e.refuseDelete {
					if e.deleted == nil {
						e.deleted = map[string]bool{}
					}
					e.deleted[form.Get("id")] = true
				}
			case "move_up", "move_down":
				if !e.refuseMove {
					e.moves = append(e.moves, levelMove{id: form.Get("id"), up: form.Get("action") == "move_up"})
				}
			}
			e.posts = append(e.posts, form)
			e.mu.Unlock()
			// The engine answers a write with a redirect naming the page to
			// show next; the id of a freshly created row rides along.
			http.Redirect(w, r, "/moscow/admin/?action="+form.Get("action")+"&edit=1&id=777", http.StatusFound)
			return
		}
		e.mu.Lock()
		e.gets = append(e.gets, r.URL.Query())
		e.mu.Unlock()
		key := r.URL.Query().Get("action")
		if file, ok := strings.CutPrefix(r.URL.Path, "/moscow/admin/"); ok && file != "" {
			key = "path:" + file
		}
		name, ok := adminFixtures[key]
		if !ok || name == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=windows-1251")
		_, _ = w.Write(e.applyEdits(retargetForm(fixture(e.t, name), r.URL.Query())))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return e, srv.URL + "/moscow/"
}

// adminHiddenRow matches the hidden fields naming the row an edit form acts
// on. The values are bare and numeric in the engine's own markup.
var adminHiddenRow = regexp.MustCompile(`<input type=hidden name=(id|categoryValue|category) value=[0-9]+>`)

// retargetForm points a fixture's edit form at the row the query asked for.
// Each fixture holds one game and one level, while the real engine serves the
// form of whatever id it is given; a scenario export reads several levels in
// turn and refuses a page that answers with a different one. Every other test
// asks for the row the fixture already names, so this rewrites those values
// to themselves.
func retargetForm(page []byte, q url.Values) []byte {
	return adminHiddenRow.ReplaceAllFunc(page, func(field []byte) []byte {
		name := string(adminHiddenRow.FindSubmatch(field)[1])
		// The level form spells the game "category" too, but the page that
		// serves it is addressed by "categoryValue" alone.
		query := name
		if name == "category" {
			query = "categoryValue"
		}
		want := q.Get(query)
		if want == "" {
			return field
		}
		return []byte("<input type=hidden name=" + name + " value=" + want + ">")
	})
}

// applyEdits replays the writes the fake engine accepted onto the level list
// it serves: deleted levels disappear and moved ones change places. The
// engine writes one level per line and marks each with "<tr valign=top", so
// the rows can be reordered without parsing the page.
//
// Modeling this matters: before it, the list came back unchanged whatever
// was posted, so a refused delete and a successful one — and a move and a
// no-op — were indistinguishable to a test.
func (e *adminEngine) applyEdits(page []byte) []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.deleted) == 0 && len(e.moves) == 0 {
		return page
	}
	lines := strings.Split(string(page), "\n")
	var rows []int
	for i, line := range lines {
		if strings.HasPrefix(line, "<tr valign=top") {
			rows = append(rows, i)
		}
	}
	// The rows keep their places on the page; only their contents move.
	content := make([]string, len(rows))
	for i, at := range rows {
		content[i] = lines[at]
	}
	for _, mv := range e.moves {
		at := slices.IndexFunc(content, func(line string) bool { return strings.Contains(line, "id="+mv.id) })
		if at < 0 {
			continue
		}
		to := at + 1
		if mv.up {
			to = at - 1
		}
		if to >= 0 && to < len(content) {
			content[at], content[to] = content[to], content[at]
		}
	}
	// The engine renumbers after a move: the level takes the position's
	// number, so the number belongs to the row's place, not to its contents.
	for i, at := range rows {
		lines[at] = levelRowNumberRe.ReplaceAllString(content[i], "${1}"+strconv.Itoa(i+1)+"</span>")
	}
	kept := lines[:0]
	for _, line := range lines {
		drop := false
		for id := range e.deleted {
			if strings.Contains(line, "id="+id) {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}
	return []byte(strings.Join(kept, "\n"))
}

// authorized answers 401 unless the request carries the organizer's Basic
// credentials, the way the administration area does.
func (e *adminEngine) authorized(w http.ResponseWriter, r *http.Request) bool {
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("org:secret"))
	if r.Header.Get("Authorization") == want {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="Admin area"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte("<html><body>Unauthorized</body></html>"))
	return false
}

func (e *adminEngine) lastPost() url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.posts) == 0 {
		e.t.Fatal("админка не получила ни одной формы")
	}
	return e.posts[len(e.posts)-1]
}

func (e *adminEngine) lastGet() url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.gets) == 0 {
		e.t.Fatal("админка не получила ни одного запроса страницы")
	}
	return e.gets[len(e.gets)-1]
}

func (e *adminEngine) lastPath() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.paths) == 0 {
		e.t.Fatal("админка не получила ни одного запроса")
	}
	return e.paths[len(e.paths)-1]
}

// administering starts the administration area and returns the flags that
// point the CLI at it with the organizer's credentials.
func administering(t *testing.T) (*adminEngine, []string) {
	t.Helper()
	isolate(t)
	e, base := newAdminEngine(t)
	return e, []string{"-base-url", base, "-admin-login", "org", "-admin-password", "secret"}
}

// wantFields checks the recorded form against the expected values; an empty
// expectation means the field must be absent.
func wantFields(t *testing.T, form url.Values, want url.Values) {
	t.Helper()
	for k, v := range want {
		if got := form.Get(k); got != v[0] {
			t.Errorf("поле %s = %q, ожидалось %q (форма %v)", k, got, v[0], form)
		}
	}
}

func TestAdminGamesTableAndJSON(t *testing.T) {
	e, base := administering(t)

	code, out, errOut := runCLI(t, with(base, "admin-games")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{"Будущая игра", "Ночной дозор: Осень", "Летняя игра", "прием заявок", "сезон 12 2026"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод не содержит %q:\n%s", want, out)
		}
	}
	if q := e.lastGet(); q.Get("action") != "games" || q.Get("edit") != "1" {
		t.Errorf("запрос = %v", q)
	}

	code, out, errOut = runCLI(t, with(base, "-json", "admin-games")...)
	if code != 0 {
		t.Fatalf("json: код выхода = %d, stderr = %q", code, errOut)
	}
	var games []struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &games); err != nil {
		t.Fatalf("вывод не JSON (%q): %v", out, err)
	}
	if len(games) != 3 || games[0].ID != 4243 || games[0].Name != "Будущая игра" {
		t.Fatalf("игры = %+v", games)
	}
}

func TestAdminGameInfoPrintsKeyValues(t *testing.T) {
	e, base := administering(t)
	code, out, errOut := runCLI(t, with(base, "admin-game-info", "4243")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	if q := e.lastGet(); q.Get("id") != "4243" {
		t.Errorf("запрос = %v", q)
	}
	for _, want := range []string{"Игра 4243", "name=Будущая игра", "number=113", "date=15.09.2026", "time=21:00", "author=Штаб"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод не содержит %q:\n%s", want, out)
		}
	}
}

func TestAdminCreateGameFromKeyValues(t *testing.T) {
	e, base := administering(t)
	code, out, errOut := runCLI(t, with(base, "admin-create-game",
		"name=Новая игра", "date=01.10.2026", "time=20:00", "league=2", "duration=360", "publish=нет", "other_league=да")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "Игра создана: 777") {
		t.Fatalf("вывод = %q", out)
	}
	form := e.lastPost()
	wantFields(t, form, url.Values{
		"action": {"add_game"}, "name": {"Новая игра"}, "date": {"01.10.2026"}, "time": {"20:00"},
		"league": {"2"}, "duration": {"360"}, "otherLeague": {"on"}, "publish": {""},
	})
}

func TestAdminUpdateGameKeepsOtherFields(t *testing.T) {
	e, base := administering(t)
	code, _, errOut := runCLI(t, with(base, "admin-update-game", "4243", "name=Переименованная", "finished=true")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	form := e.lastPost()
	wantFields(t, form, url.Values{
		"action": {"update_game"}, "id": {"4243"}, "name": {"Переименованная"}, "finished": {"on"},
		// Untouched fields come back from the form the command read first.
		"author": {"Штаб"}, "number": {"113"},
	})
}

func TestAdminDeleteAndCopyGame(t *testing.T) {
	e, base := administering(t)

	if code, _, errOut := runCLI(t, with(base, "admin-delete-game", "4100")...); code != 0 {
		t.Fatalf("delete: код=%d stderr=%q", code, errOut)
	}
	wantFields(t, e.lastPost(), url.Values{"action": {"del_games"}, "id": {"4100"}})

	code, out, errOut := runCLI(t, with(base, "admin-copy-game", "4242")...)
	if code != 0 {
		t.Fatalf("copy: код=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "скопирована в 777") {
		t.Errorf("вывод = %q", out)
	}
	wantFields(t, e.lastPost(), url.Values{"action": {"copy_game"}, "game": {"4242"}, "zadan": {""}})

	if code, _, errOut := runCLI(t, with(base, "admin-copy-game", "4242", "-with-levels")...); code != 0 {
		t.Fatalf("copy -with-levels: код=%d stderr=%q", code, errOut)
	}
	wantFields(t, e.lastPost(), url.Values{"action": {"copy_game"}, "zadan": {"on"}})
}

func TestAdminLevelsAndLevelInfo(t *testing.T) {
	e, base := administering(t)

	code, out, errOut := runCLI(t, with(base, "admin-levels", "4242")...)
	if code != 0 {
		t.Fatalf("admin-levels: код=%d stderr=%q", code, errOut)
	}
	if q := e.lastGet(); q.Get("action") != "zadanie" || q.Get("categoryValue") != "4242" {
		t.Errorf("запрос = %v", q)
	}
	for _, want := range []string{"Гаражи", "основной", "30:30:30"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод admin-levels не содержит %q:\n%s", want, out)
		}
	}

	code, out, errOut = runCLI(t, with(base, "admin-level", "4242", "901")...)
	if code != 0 {
		t.Fatalf("admin-level: код=%d stderr=%q", code, errOut)
	}
	if q := e.lastGet(); q.Get("id") != "901" {
		t.Errorf("запрос = %v", q)
	}
	for _, want := range []string{"Задание 901", "title=Гаражи", "codes=", "question:", "Найдите"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод admin-level не содержит %q:\n%s", want, out)
		}
	}
	// The dump is plain text, not the HTML the form carries.
	if strings.Contains(out, "<b>") {
		t.Errorf("разметка не удалена:\n%s", out)
	}
}

func TestAdminCreateLevelCompoundValues(t *testing.T) {
	e, base := administering(t)
	code, out, errOut := runCLI(t, with(base, "admin-create-level", "4242",
		"title=Пустырь",
		"question=Найдите табличку",
		"codes=A#АА:1:1,B:2",
		"sector_names=Гаражи,Пустырь",
		"bonus_codes=BB:1:10",
		"fake_codes=FF:10",
		"spoilers=SP1:5:Текст: с двоеточием",
		"code_count=1",
		"penalty=0",
		"skvoz=нет")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "Задание создано: 777") {
		t.Fatalf("вывод = %q", out)
	}
	wantFields(t, e.lastPost(), url.Values{
		"action": {"add_zadanie"}, "categoryValue": {"4242"}, "title": {"Пустырь"}, "question": {"Найдите табличку"},
		"code[0]": {"A"}, "codeS[0]": {"АА"}, "danger[0]": {"1"}, "sector[0]": {"1"},
		"code[1]": {"B"}, "codeS[1]": {""}, "danger[1]": {"2"}, "sector[1]": {""},
		"secName[1]": {"Гаражи"}, "secName[2]": {"Пустырь"},
		"codeB[0]": {"BB"}, "dangerB[0]": {"1"}, "timeB[0]": {"10"},
		"codeF[0]": {"FF"}, "fakeShtraf[0]": {"10"},
		"spoiler[1]": {"Текст: с двоеточием"}, "spoilerCode[1]": {"SP1"}, "spoilerPenalty[1]": {"5"},
		"codeCount": {"1"}, "penalty": {"0"},
	})
}

func TestAdminUpdateAndDeleteLevel(t *testing.T) {
	e, base := administering(t)

	if code, _, errOut := runCLI(t, with(base, "admin-update-level", "4242", "901", "title=Новое имя", "try_limit=5")...); code != 0 {
		t.Fatalf("update: код=%d stderr=%q", code, errOut)
	}
	wantFields(t, e.lastPost(), url.Values{
		"action": {"update_zadanie"}, "id": {"901"}, "categoryValue": {"4242"},
		"title": {"Новое имя"}, "tryLimit": {"5"},
	})

	if code, _, errOut := runCLI(t, with(base, "admin-delete-level", "4242", "903")...); code != 0 {
		t.Fatalf("delete: код=%d stderr=%q", code, errOut)
	}
	wantFields(t, e.lastPost(), url.Values{"action": {"del_zadanie"}, "id": {"903"}, "categoryValue": {"4242"}})

	if code, _, errOut := runCLI(t, with(base, "admin-copy-levels", "4243", "4242")...); code != 0 {
		t.Fatalf("copy-levels: код=%d stderr=%q", code, errOut)
	}
	wantFields(t, e.lastPost(), url.Values{"action": {"copy_zadanie"}, "categoryValue": {"4243"}, "game": {"4242"}})
}

// TestAdminDeleteLevelReportsASilentRefusal covers the engine's real
// behavior, measured against classic.dzzzr.ru on 2026-09-09: with the game's
// date left at 00.00.0000 it answers del_zadanie with the same clean redirect
// as a success — no err= anywhere — and keeps the level. The command used to
// print "Задание удалено." on top of that.
func TestAdminDeleteLevelReportsASilentRefusal(t *testing.T) {
	e, base := administering(t)
	e.refuseDelete = true

	code, _, errOut := runCLI(t, with(base, "admin-delete-level", "4242", "903")...)
	if code == 0 {
		t.Fatalf("молчаливый отказ принят за успех: код=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "оставил задание") {
		t.Fatalf("stderr не объясняет причину: %q", errOut)
	}
}

// TestAdminMoveLevelCommand covers reordering, which the engine offers as the
// up/down arrows next to every level and which the client had no command for
// at all until 2026-09-09. The command verifies its own effect, because the
// engine reports nothing either way.
func TestAdminMoveLevelCommand(t *testing.T) {
	e, base := administering(t)

	code, out, errOut := runCLI(t, with(base, "admin-move-level", "4242", "902", "up")...)
	if code != 0 {
		t.Fatalf("move up: код=%d stderr=%q", code, errOut)
	}
	wantFields(t, e.lastPost(), url.Values{
		"action": {"move_up"}, "id": {"902"}, "table": {"zadanie"}, "parentName": {"game"},
	})
	if !strings.Contains(out, "с позиции 2 на 1") {
		t.Errorf("вывод не сообщает о перемещении: %q", out)
	}

	if code, _, errOut := runCLI(t, with(base, "admin-move-level", "4242", "902", "вниз")...); code != 0 {
		t.Fatalf("move down: код=%d stderr=%q", code, errOut)
	}
	wantFields(t, e.lastPost(), url.Values{"action": {"move_down"}, "id": {"902"}})

	code, _, errOut = runCLI(t, with(base, "admin-move-level", "4242", "902", "вбок")...)
	if code == 0 || !strings.Contains(errOut, "up или down") {
		t.Fatalf("неверное направление принято: код=%d stderr=%q", code, errOut)
	}
}

// TestAdminMoveLevelAtTheEdge separates the two ways a move can change
// nothing. Standing still at the end of the list is expected and reported as
// such; standing still anywhere else is the same silent refusal
// admin-delete-level runs into, and must not be dressed up as "nowhere left
// to go".
func TestAdminMoveLevelAtTheEdge(t *testing.T) {
	_, base := administering(t)
	code, out, errOut := runCLI(t, with(base, "admin-move-level", "4242", "903", "down")...)
	if code != 0 {
		t.Fatalf("край списка: код=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "двигать дальше некуда") {
		t.Errorf("вывод = %q", out)
	}
}

func TestAdminMoveLevelReportsASilentRefusal(t *testing.T) {
	e, base := administering(t)
	e.refuseMove = true

	code, out, errOut := runCLI(t, with(base, "admin-move-level", "4242", "902", "up")...)
	if code == 0 {
		t.Fatalf("молчаливый отказ принят за успех: код=%d stdout=%q", code, out)
	}
	if !strings.Contains(errOut, "оставил задание") {
		t.Fatalf("stderr не объясняет причину: %q", errOut)
	}
}

// TestAdminTeamPrintsRoster covers the roster the organizer's team card
// carries, which the client had no way to read until 2026-09-10.
func TestAdminTeamPrintsRoster(t *testing.T) {
	_, base := administering(t)
	original := adminFixtures["teams"]
	adminFixtures["teams"] = "admin_teams_organizer.html"
	t.Cleanup(func() { adminFixtures["teams"] = original })

	code, out, errOut := runCLI(t, with(base, "admin-team", "1383", "31")...)
	if code != 0 {
		t.Fatalf("код=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"Стальные Яйца", "Kamrad_anin", "высшая лига", "Состав: 2 в команде из 3", "+ 4r2na", "  zoya_O"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод не содержит %q:\n%s", want, out)
		}
	}

	code, out, errOut = runCLI(t, with(base, "-json", "admin-team", "1383", "31")...)
	if code != 0 {
		t.Fatalf("json: код=%d stderr=%q", code, errOut)
	}
	var info struct {
		Roster []struct {
			Login  string `json:"login"`
			OnTeam bool   `json:"on_team"`
		} `json:"roster"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("вывод не JSON: %v", err)
	}
	if len(info.Roster) != 3 || info.Roster[0].Login != "4r2na" || !info.Roster[0].OnTeam {
		t.Fatalf("состав в JSON = %+v", info.Roster)
	}
}

// TestAdminCityTeamsCommand covers the city directory the организатор needs
// to find a team's id before заявка.
func TestAdminCityTeamsCommand(t *testing.T) {
	_, base := administering(t)
	original := adminFixtures["teams"]
	adminFixtures["teams"] = "admin_teams_organizer.html"
	t.Cleanup(func() { adminFixtures["teams"] = original })

	code, out, errOut := runCLI(t, with(base, "admin-city-teams", "1383")...)
	if code != 0 {
		t.Fatalf("код=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"979", "альфа x3m & A.", "1349", "КИПИШ"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод не содержит %q:\n%s", want, out)
		}
	}

	code, out, errOut = runCLI(t, with(base, "admin-city-teams", "1383", "кипиш")...)
	if code != 0 {
		t.Fatalf("фильтр: код=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "КИПИШ") || strings.Contains(out, "альфа") {
		t.Errorf("фильтр не сработал:\n%s", out)
	}
}

func TestAdminArgumentErrors(t *testing.T) {
	_, base := administering(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"too few", []string{"admin-level", "4242"}, "admin-level ИГРА ЗАДАНИЕ"},
		{"too many", []string{"admin-games", "4242"}, "не принимает аргументов"},
		{"bad id", []string{"admin-levels", "abc"}, "неверный номер игры"},
		{"bad level", []string{"admin-give-level", "4242", "5150", "0"}, "неверный номер уровня"},
		{"bad correction", []string{"admin-add-correction", "4242", "5150", "премия", "5"}, "укажите bonus или penalty"},
		{"bad engine state", []string{"admin-engine", "4242", "maybe"}, "укажите on, off или toggle"},
		{"bad event", []string{"admin-set-time", "4242", "5150", "3", "нечто", "2026-09-08 22:30:00"}, "укажите level, code или номер"},
		{"bad status", []string{"admin-remove-application", "4242", "5150", "неизвестно"}, "неверный статус заявки"},
		{"not a pair", []string{"admin-create-game", "имя"}, "не является парой ключ=значение"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, errOut := runCLI(t, with(base, tc.args...)...)
			if code != 1 {
				t.Fatalf("код выхода = %d, ожидался 1", code)
			}
			if !strings.Contains(errOut, tc.want) {
				t.Fatalf("stderr = %q, ожидалось упоминание %q", errOut, tc.want)
			}
		})
	}
}

func TestParseKV(t *testing.T) {
	kv, err := parseKV([]string{"Name=Игра", "new-pin=да", "comment=a=b"})
	if err != nil {
		t.Fatal(err)
	}
	if kv["name"] != "Игра" || kv["new_pin"] != "да" || kv["comment"] != "a=b" {
		t.Fatalf("пары = %v", kv)
	}
	if _, err := parseKV([]string{"без-знака-равенства"}); err == nil {
		t.Error("аргумент без '=' должен быть ошибкой")
	}
	if _, err := parseKV([]string{"=значение"}); err == nil {
		t.Error("пустой ключ должен быть ошибкой")
	}
}

func TestGameParamsFromKV(t *testing.T) {
	p, err := gameParamsFromKV(map[string]string{
		"name": "Игра", "date": "01.10.2026", "time": "20:00", "league": "2",
		"publish": "нет", "invitation": "1", "duration": "360",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Игра" || p.Date != "01.10.2026" || p.Time != "20:00" || p.League != 2 || p.Duration != 360 {
		t.Fatalf("параметры = %+v", p)
	}
	if p.Publish == nil || *p.Publish || p.Invitation == nil || !*p.Invitation {
		t.Fatalf("флаги = %v %v", p.Publish, p.Invitation)
	}

	err = mustFail(t, func() error { _, e := gameParamsFromKV(map[string]string{"нетакого": "1"}); return e })
	if !strings.Contains(err.Error(), "неизвестный ключ") || !strings.Contains(err.Error(), "master_code_shtraf") {
		t.Fatalf("ошибка = %v", err)
	}
	err = mustFail(t, func() error { _, e := gameParamsFromKV(map[string]string{"duration": "долго"}); return e })
	if !strings.Contains(err.Error(), "ожидалось целое число") {
		t.Fatalf("ошибка = %v", err)
	}
	err = mustFail(t, func() error { _, e := gameParamsFromKV(map[string]string{"publish": "может быть"}); return e })
	if !strings.Contains(err.Error(), "true/false/1/0/да/нет") {
		t.Fatalf("ошибка = %v", err)
	}
}

func TestLevelParamsFromKV(t *testing.T) {
	p, err := levelParamsFromKV(map[string]string{
		"title":        "Пустырь",
		"codes":        "A#АА:1:1, B:2 , C:3+:2",
		"sector_names": "Гаражи,Пустырь",
		"bonus_codes":  "BB#ББ:1:10",
		"fake_codes":   "FF:10",
		"spoilers":     "SP1:5:Текст: с двоеточием",
		"penalty":      "0",
		"try_limit":    "3",
		"reserve":      "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []dzzzr.LevelCode{
		{Code: "A", Synonyms: "АА", Danger: "1", Sector: 1},
		{Code: "B", Danger: "2"},
		{Code: "C", Danger: "3+", Sector: 2},
	}
	if len(p.Codes) != len(want) {
		t.Fatalf("коды = %+v", p.Codes)
	}
	for i := range want {
		if p.Codes[i] != want[i] {
			t.Errorf("код %d = %+v, ожидался %+v", i, p.Codes[i], want[i])
		}
	}
	if len(p.SectorNames) != 2 || p.SectorNames[1] != "Пустырь" {
		t.Errorf("секторы = %v", p.SectorNames)
	}
	if len(p.BonusCodes) != 1 || p.BonusCodes[0] != (dzzzr.BonusCode{Code: "BB", Synonyms: "ББ", Danger: "1", Minutes: 10}) {
		t.Errorf("бонусные коды = %+v", p.BonusCodes)
	}
	if len(p.FakeCodes) != 1 || p.FakeCodes[0] != (dzzzr.FakeCode{Code: "FF", Penalty: 10}) {
		t.Errorf("ложные коды = %+v", p.FakeCodes)
	}
	if len(p.Spoilers) != 1 || p.Spoilers[0] != (dzzzr.SpoilerParams{Code: "SP1", Penalty: 5, Text: "Текст: с двоеточием"}) {
		t.Errorf("спойлеры = %+v", p.Spoilers)
	}
	if p.Penalty == nil || *p.Penalty != 0 {
		t.Errorf("штраф = %v, ожидался явный ноль", p.Penalty)
	}
	if p.TryLimit != 3 || p.Reserve == nil || !*p.Reserve {
		t.Errorf("параметры = %+v", p)
	}

	// An empty compound value clears the list instead of leaving it alone.
	empty, err := levelParamsFromKV(map[string]string{"codes": "", "sector_names": ""})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Codes == nil || len(empty.Codes) != 0 || empty.SectorNames == nil || len(empty.SectorNames) != 0 {
		t.Errorf("пустые списки = %+v / %+v", empty.Codes, empty.SectorNames)
	}

	for _, tc := range []struct{ key, value, want string }{
		{"codes", "A", "КОД:СЛОЖНОСТЬ"},
		{"codes", "A:1:x", "ожидалось целое число"},
		{"codes", "#АА:1", "пустой код"},
		{"bonus_codes", "BB:10", "КОД:СЛОЖНОСТЬ:МИНУТЫ"},
		{"fake_codes", "FF", "КОД:ШТРАФ"},
		{"spoilers", "SP1", "КОД:ШТРАФ"},
	} {
		err := mustFail(t, func() error { _, e := levelParamsFromKV(map[string]string{tc.key: tc.value}); return e })
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s=%q: ошибка = %v, ожидалось упоминание %q", tc.key, tc.value, err, tc.want)
		}
	}
}

func TestApplicationParamsFromKV(t *testing.T) {
	p, err := applicationParamsFromKV(map[string]string{
		"status": "принята", "new_pin": "1", "points": "0", "novice": "нет", "comment": "ок",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status == nil || *p.Status != dzzzr.ApplicationAccepted || !p.NewPin {
		t.Fatalf("заявка = %+v", p)
	}
	if p.Points == nil || *p.Points != 0 || p.Novice == nil || *p.Novice || p.Comment != "ок" {
		t.Fatalf("заявка = %+v", p)
	}
	err = mustFail(t, func() error { _, e := applicationParamsFromKV(map[string]string{"pin": "1"}); return e })
	if !strings.Contains(err.Error(), "неизвестный ключ") || !strings.Contains(err.Error(), "new_pin") {
		t.Fatalf("ошибка = %v", err)
	}
}

// TestKVKeyListsMatchTheParsers guards the lists printed in the "unknown key"
// message against drifting away from the switches that accept the keys.
func TestKVKeyListsMatchTheParsers(t *testing.T) {
	for _, k := range gameKeys {
		if _, err := gameParamsFromKV(map[string]string{k: "1"}); err != nil && strings.Contains(err.Error(), "неизвестный ключ") {
			t.Errorf("ключ игры %q объявлен, но не разбирается", k)
		}
	}
	for _, k := range levelKeys {
		if _, err := levelParamsFromKV(map[string]string{k: "1"}); err != nil && strings.Contains(err.Error(), "неизвестный ключ") {
			t.Errorf("ключ задания %q объявлен, но не разбирается", k)
		}
	}
	for _, k := range applicationKeys {
		if _, err := applicationParamsFromKV(map[string]string{k: "1"}); err != nil && strings.Contains(err.Error(), "неизвестный ключ") {
			t.Errorf("ключ заявки %q объявлен, но не разбирается", k)
		}
	}
}

func TestFormatCompoundValues(t *testing.T) {
	codes := []dzzzr.LevelCode{{Code: "A", Synonyms: "АА", Danger: "1", Sector: 1}, {Code: "B", Danger: "2"}}
	if got := formatCodes(codes); got != "A#АА:1:1,B:2" {
		t.Errorf("formatCodes = %q", got)
	}
	// What the dump prints must parse back into what it came from.
	back, err := kvCodes("codes", formatCodes(codes))
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 || back[0] != codes[0] || back[1] != codes[1] {
		t.Errorf("обратный разбор = %+v", back)
	}
	if got := formatBonusCodes([]dzzzr.BonusCode{{Code: "BB", Danger: "1", Minutes: 10}}); got != "BB:1:10" {
		t.Errorf("formatBonusCodes = %q", got)
	}
	if got := formatFakeCodes([]dzzzr.FakeCode{{Code: "FF", Penalty: 10}}); got != "FF:10" {
		t.Errorf("formatFakeCodes = %q", got)
	}
	if got := formatSpoilers([]dzzzr.SpoilerParams{{Code: "SP1", Penalty: 5, Text: "<b>Текст</b>"}}); got != "SP1:5:Текст" {
		t.Errorf("formatSpoilers = %q", got)
	}
}

func TestAdminCommandsAreRegistered(t *testing.T) {
	want := []string{
		"admin-games", "admin-game-info", "admin-create-game", "admin-update-game", "admin-delete-game", "admin-copy-game",
		"admin-levels", "admin-level", "admin-create-level", "admin-update-level", "admin-delete-level", "admin-move-level", "admin-copy-levels",
		"admin-teams", "admin-team", "admin-city-teams", "admin-set-application", "admin-accept-all", "admin-generate-pins", "admin-add-team",
		"admin-remove-application", "admin-create-team",
		"admin-monitor", "admin-log", "admin-give-level", "admin-accept-level", "admin-accept-code", "admin-clear-codes",
		"admin-remove-level", "admin-clear-progress", "admin-plan-level", "admin-unplan-level", "admin-add-correction",
		"admin-delete-corrections", "admin-block-team", "admin-unblock-team", "admin-engine", "admin-finish-game",
		"admin-set-time",
		"admin-messages", "admin-send-message", "admin-delete-message", "admin-delete-all-messages",
	}
	for _, name := range want {
		c := findCommand(name)
		if c == nil {
			t.Errorf("команда %q не зарегистрирована", name)
			continue
		}
		if c.Auth != authAdmin {
			t.Errorf("%s: уровень доступа = %d, ожидался authAdmin", name, c.Auth)
		}
		if c.Help == "" || !strings.HasPrefix(c.Usage, name) {
			t.Errorf("%s: справка %q, использование %q", name, c.Help, c.Usage)
		}
	}
	// Nothing else may carry the organizer prefix, and nothing outside the
	// prefix may demand organizer credentials.
	for _, c := range commands {
		if c.Name == "admin-validate-source" || c.Name == "admin-validate-scenario" {
			if c.Auth != authNone {
				t.Errorf("offline validator requires authentication")
			}
			continue
		}
		if isAdminCommand(c.Name) != (c.Auth == authAdmin) {
			t.Errorf("%s: имя и уровень доступа не согласованы (%d)", c.Name, c.Auth)
		}
	}
}

func TestMCPCommandRejectsUnknownSecurity(t *testing.T) {
	isolate(t)
	c := findCommand("mcp")
	if c == nil || c.Auth != authNone || c.Help == "" {
		t.Fatalf("команда mcp = %+v", c)
	}
	code, _, errOut := runCLI(t, "mcp", "-security", "нечто")
	if code != 1 {
		t.Fatalf("код выхода = %d, ожидался 1", code)
	}
	if !strings.Contains(errOut, "readonly") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestMCPEndedByClient(t *testing.T) {
	// The SDK's own wording for a client that closed stdin, plus the two
	// errors the transport reports directly.
	for _, err := range []error{
		errors.New("server is closing: EOF"),
		io.EOF,
		fmt.Errorf("обёртка: %w", context.Canceled),
	} {
		if !endedByClient(err) {
			t.Errorf("%v: ожидалось завершение по инициативе клиента", err)
		}
	}
	if endedByClient(errors.New("не удалось записать ответ")) {
		t.Error("настоящая ошибка принята за отключение клиента")
	}
}

// mustFail runs f, failing the test when it returns no error.
func mustFail(t *testing.T, f func() error) error {
	t.Helper()
	err := f()
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	return err
}
