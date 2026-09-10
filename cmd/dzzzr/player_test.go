package main

import (
	"encoding/base64"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// fixture reads one of the library's engine fixtures; the CLI tests speak to
// a server that replays exactly what dzzzr's own tests decode.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "dzzzr", "testdata", name))
	if err != nil {
		t.Fatalf("фикстура %s: %v", name, err)
	}
	return data
}

// engine is a minimal stand-in for the Dozor Classic endpoints the player
// commands use. Every POST to go/ is recorded and answered with the redirect
// the real engine sends.
type engine struct {
	t *testing.T

	mu            sync.Mutex
	forms         []url.Values
	auths         []string
	actionCode    string
	actionBody    string
	state         string
	unauthorized  bool
	rejectSession bool
	loginCount    int
}

// newEngine starts the test server and returns it with the base URL the CLI
// must be pointed at.
func newEngine(t *testing.T) (*engine, string) {
	t.Helper()
	e := &engine{t: t, actionCode: "9", state: "go_state.json"}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /moscow/go/", func(w http.ResponseWriter, r *http.Request) {
		if e.rejectAuth(w) {
			return
		}
		if r.URL.Query().Get("api") != "true" {
			e.t.Errorf("go/: ожидался api=true, получено %q", r.URL.RawQuery)
		}
		if e.takeSessionRejection() {
			writeJSON(w, []byte(`{"error" : "Ошибка авторизации. Сессия не найдена"}`))
			return
		}
		writeJSON(w, fixture(e.t, e.currentState()))
	})
	mux.HandleFunc("POST /moscow/go/", func(w http.ResponseWriter, r *http.Request) {
		if e.rejectAuth(w) {
			return
		}
		if err := r.ParseForm(); err != nil {
			e.t.Errorf("go/: разбор формы: %v", err)
		}
		e.mu.Lock()
		e.forms = append(e.forms, r.PostForm)
		e.auths = append(e.auths, r.Header.Get("Authorization"))
		code := e.actionCode
		body := e.actionBody
		e.mu.Unlock()
		if body != "" {
			// Some deployments answer an action with the state itself rather
			// than a redirect.
			writeJSON(w, []byte(body))
			return
		}
		w.Header().Set("Location", "/moscow/go/?nostat=&err="+code+"&api=true")
		w.WriteHeader(http.StatusFound)
	})

	mux.HandleFunc("GET /moscow/API/login.php", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("login") == "" {
			e.t.Error("login.php: пустой логин")
		}
		e.mu.Lock()
		e.loginCount++
		e.mu.Unlock()
		writeJSON(w, []byte(`{"userName":"Капитан","error":"ok","userToken":"TOK","code":"2"}`))
	})
	mux.HandleFunc("GET /moscow/API/game.php", func(w http.ResponseWriter, r *http.Request) {
		if e.rejectAuth(w) {
			return
		}
		q := r.URL.Query()
		switch {
		case q.Get("stat") == "true":
			writeJSON(w, fixture(e.t, "game_stat.json"))
		case q.Get("log") == "true":
			writeJSON(w, fixture(e.t, "game_log.json"))
		default:
			writeJSON(w, fixture(e.t, "game_level.json"))
		}
	})
	mux.HandleFunc("GET /moscow/API/messages.php", func(w http.ResponseWriter, r *http.Request) {
		if e.rejectAuth(w) {
			return
		}
		e.mu.Lock()
		e.forms = append(e.forms, r.URL.Query())
		e.mu.Unlock()
		writeJSON(w, fixture(e.t, "messages.json"))
	})
	mux.HandleFunc("POST /moscow/API/postmessage.php", func(w http.ResponseWriter, r *http.Request) {
		if e.rejectAuth(w) {
			return
		}
		if err := r.ParseForm(); err != nil {
			e.t.Errorf("postmessage.php: разбор формы: %v", err)
		}
		e.mu.Lock()
		e.forms = append(e.forms, r.PostForm)
		e.mu.Unlock()
		writeJSON(w, []byte(`{"message":"Ok"}`))
	})
	mux.HandleFunc("GET /moscow/API/gamesList.php", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.forms = append(e.forms, r.URL.Query())
		e.mu.Unlock()
		writeJSON(w, fixture(e.t, "games_list.json"))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return e, srv.URL + "/moscow/"
}

func writeJSON(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(body)
}

// rejectAuth answers 401 when the test asked the engine to refuse
// credentials.
func (e *engine) rejectAuth(w http.ResponseWriter) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.unauthorized {
		return false
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="dzzzr"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte("Unauthorized"))
	return true
}

func (e *engine) currentState() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

// rejectSessionOnce makes the next game-state read answer with the engine's
// session-error envelope, the way it does when a stored token has expired.
func (e *engine) rejectSessionOnce() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rejectSession = true
}

// takeSessionRejection reports and clears a pending session rejection.
func (e *engine) takeSessionRejection() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.rejectSession {
		return false
	}
	e.rejectSession = false
	return true
}

// logins counts the sign-ins the engine served.
func (e *engine) logins() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.loginCount
}

func (e *engine) setState(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state = name
}

// setActionBody makes an action answer with a body instead of a redirect,
// the way a deployment that does not redirect would.
func (e *engine) setActionBody(body string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.actionBody = body
}

func (e *engine) setActionCode(code string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.actionCode = code
}

func (e *engine) setUnauthorized() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.unauthorized = true
}

// lastBasicAuth returns the captain login and PIN of the most recent action.
func (e *engine) lastBasicAuth(t *testing.T) (string, string) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.auths) == 0 {
		t.Fatal("движок не получил заголовок Authorization")
	}
	header := e.auths[len(e.auths)-1]
	encoded, ok := strings.CutPrefix(header, "Basic ")
	if !ok {
		t.Fatalf("Authorization = %q", header)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("Authorization не base64: %v", err)
	}
	user, pass, _ := strings.Cut(string(raw), ":")
	return user, pass
}

// lastForm returns the most recent recorded request values.
func (e *engine) lastForm() url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.forms) == 0 {
		e.t.Fatal("движок не получил ни одного запроса с параметрами")
	}
	return e.forms[len(e.forms)-1]
}

// writeSession puts a ready-to-use session file into the isolated HOME.
func writeSession(t *testing.T, city string) string {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), ".config", "dzzzr")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, city+".json")
	body := `{"token":"TOK","login":"demo","captain":"demo","pin":"1234"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// playing sets up an isolated HOME with a session and a running engine.
func playing(t *testing.T) (*engine, []string) {
	t.Helper()
	isolate(t)
	writeSession(t, "moscow")
	e, base := newEngine(t)
	return e, []string{"-base-url", base}
}

func with(prefix []string, args ...string) []string {
	return append(append([]string{}, prefix...), args...)
}

func TestStatusEndToEnd(t *testing.T) {
	_, base := playing(t)
	code, out, errOut := runCLI(t, with(base, "status")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{
		"Ночной дозор: Осень",
		"MockTeam",
		"Уровень 3 из 12",
		"Коды:           1 из 4 (нужно 3)",
		"2м05с",
		"выдана первая",
		"Сквозные бонусные задания",
		"Всем командам: движок работает",
		"Последнее действие: [9]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод не содержит %q:\n%s", want, out)
		}
	}
	// The empty placeholder object of the fixture must not become a level.
	if strings.Contains(out, "Уровень 0") {
		t.Errorf("пустой бонусный уровень попал в вывод:\n%s", out)
	}
}

func TestStatusJSONMode(t *testing.T) {
	_, base := playing(t)
	code, out, errOut := runCLI(t, with(base, "status", "-json")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	var got struct {
		GameName string `json:"gameName"`
		Level    struct {
			LevelNumber int `json:"levelNumber"`
		} `json:"level"`
		Messages []struct {
			Text string `json:"text"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("вывод не JSON (%q): %v", out, err)
	}
	if got.GameName != "Ночной дозор: Осень" {
		t.Errorf("gameName = %q", got.GameName)
	}
	if got.Level.LevelNumber != 3 {
		t.Errorf("levelNumber = %d", got.Level.LevelNumber)
	}
	if len(got.Messages) != 2 {
		t.Errorf("сообщений = %d, ожидалось 2", len(got.Messages))
	}
}

func TestStatusNotStartedGame(t *testing.T) {
	e, base := playing(t)
	e.mu.Lock()
	e.state = "go_state_not_started.json"
	e.mu.Unlock()
	code, out, errOut := runCLI(t, with(base, "status")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "Игра ещё не началась") || !strings.Contains(out, "9 сентября 2026 г.") {
		t.Fatalf("вывод = %q", out)
	}
}

func TestSendCodeAcceptedAndRejected(t *testing.T) {
	e, base := playing(t)

	code, out, errOut := runCLI(t, with(base, "send-code", "1")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "Принято: [9]") || !strings.Contains(out, "Выполняйте следующее задание") {
		t.Fatalf("вывод = %q", out)
	}
	form := e.lastForm()
	if form.Get("action") != "entcod" || form.Get("cod") != "1" {
		t.Fatalf("форма = %v", form)
	}

	e.setActionCode("11")
	code, out, _ = runCLI(t, with(base, "send-code", "X")...)
	if code != 2 {
		t.Fatalf("код выхода отклонённого кода = %d, ожидался 2", code)
	}
	if !strings.Contains(out, "Отклонено: [11]") {
		t.Fatalf("вывод = %q", out)
	}
}

func TestSendCodeJSONMode(t *testing.T) {
	_, base := playing(t)
	code, out, errOut := runCLI(t, with(base, "-json", "send-code", "1")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	var got actionOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("вывод не JSON (%q): %v", out, err)
	}
	if got.Err != 9 || !got.Accepted || got.Text == "" {
		t.Fatalf("результат = %+v", got)
	}
}

func TestSendCodeMissingArgument(t *testing.T) {
	_, base := playing(t)
	code, _, errOut := runCLI(t, with(base, "send-code")...)
	if code != 1 {
		t.Fatalf("код выхода = %d", code)
	}
	if !strings.Contains(errOut, "send-code КОД") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestActionCommandForms(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want url.Values
	}{
		{"send-bonus", []string{"send-bonus", "4", "B1"}, url.Values{"action": {"entcod"}, "cod": {"B1"}, "skvoz": {"1"}, "level": {"4"}}},
		{"spoiler", []string{"spoiler", "3", "SP1"}, url.Values{"action": {"spoilerCode"}, "skvoz": {"3"}, "spoilerCode": {"SP1"}}},
		{"hint", []string{"hint", "3", "2"}, url.Values{"action": {"takeClue"}, "event": {"5"}, "level": {"3"}}},
		{"hint-early explicit", []string{"hint-early", "2", "7"}, url.Values{"action": {"beforeClue"}, "clue": {"2"}, "level": {"7"}}},
		{"hint-early default", []string{"hint-early"}, url.Values{"action": {"beforeClue"}, "clue": {"2"}, "level": {"3"}}},
		// The engine's own forms post these three with no level, and a team
		// on a break has none to name.
		{"abandon", []string{"abandon"}, url.Values{"action": {"abandon"}}},
		{"break", []string{"break"}, url.Values{"action": {"break"}}},
		{"break-stop", []string{"break-stop"}, url.Values{"action": {"breakStop"}}},
		{"next-level", []string{"next-level"}, url.Values{"action": {"nextLevel"}, "level": {"3"}}},
		{"select-level", []string{"select-level", "5"}, url.Values{"action": {"selectlevel"}, "llevel": {"3"}, "level": {"5"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, base := playing(t)
			e.setActionCode("16")
			if code, out, errOut := runCLI(t, with(base, tc.args...)...); code != 0 {
				t.Fatalf("код выхода = %d, stdout=%q stderr=%q", code, out, errOut)
			}
			form := e.lastForm()
			for k, want := range tc.want {
				if got := form.Get(k); got != want[0] {
					t.Errorf("%s = %q, ожидалось %q (форма %v)", k, got, want[0], form)
				}
			}
			if form.Has("level") && !tc.want.Has("level") {
				t.Errorf("уровень отправлен там, где движок его не шлёт: %v", form)
			}
		})
	}
}

func TestHintRejectsBadNumber(t *testing.T) {
	_, base := playing(t)
	code, _, errOut := runCLI(t, with(base, "hint", "3", "9")...)
	if code != 1 || !strings.Contains(errOut, "1 или 2") {
		t.Fatalf("код=%d stderr=%q", code, errOut)
	}
}

func TestLevelAndHintsCommands(t *testing.T) {
	_, base := playing(t)

	code, out, errOut := runCLI(t, with(base, "level")...)
	if code != 0 {
		t.Fatalf("level: код=%d stderr=%q", code, errOut)
	}
	if strings.Contains(out, "<b>") || strings.Contains(out, "<p>") {
		t.Errorf("разметка не удалена: %q", out)
	}
	for _, want := range []string{"Найдите двор с тремя гаражами", "Тихий район", "Коэффициенты сложности", "Спойлеры"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод level не содержит %q:\n%s", want, out)
		}
	}

	code, out, errOut = runCLI(t, with(base, "hints")...)
	if code != 0 {
		t.Fatalf("hints: код=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "Подсказка 1:") || !strings.Contains(out, "смотрите выше") {
		t.Errorf("вывод hints = %q", out)
	}
	if !strings.Contains(out, "Подсказка 2: ещё не выдана") {
		t.Errorf("вторая подсказка описана неверно: %q", out)
	}
}

func TestBonusLevelsCommand(t *testing.T) {
	_, base := playing(t)
	code, out, errOut := runCLI(t, with(base, "bonus-levels")...)
	if code != 0 {
		t.Fatalf("код=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "Сквозной бонус: найдите памятник") {
		t.Fatalf("вывод = %q", out)
	}
}

func TestStatLogAndMessages(t *testing.T) {
	_, base := playing(t)

	code, out, errOut := runCLI(t, with(base, "stat")...)
	if code != 0 {
		t.Fatalf("stat: код=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "00:37:51") || strings.Contains(out, "<td") {
		t.Errorf("вывод stat = %q", out)
	}

	code, out, errOut = runCLI(t, with(base, "log")...)
	if code != 0 {
		t.Fatalf("log: код=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "выдан уровень 1") || strings.Contains(out, "<td") {
		t.Errorf("вывод log = %q", out)
	}

	code, out, errOut = runCLI(t, with(base, "messages")...)
	if code != 0 {
		t.Fatalf("messages: код=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "Всем удачи!") || !strings.Contains(out, `Мы на первом "коде"`) {
		t.Errorf("вывод messages = %q", out)
	}
}

func TestMessagesAfterFlag(t *testing.T) {
	e, base := playing(t)
	if code, _, errOut := runCLI(t, with(base, "messages", "-after", "2026-09-08 21:01:00")...); code != 0 {
		t.Fatalf("код=%d stderr=%q", code, errOut)
	}
	if got := e.lastForm().Get("after"); got != "2026-09-08 21:01:00" {
		t.Fatalf("after = %q", got)
	}
}

// send-message must go through action=send_message. API/postmessage.php
// answers Ok and delivers nothing, so a message sent that way is lost.
func TestSendMessage(t *testing.T) {
	e, base := playing(t)
	e.setActionCode(strconv.Itoa(dzzzr.ErrMessageSent))
	code, out, errOut := runCLI(t, with(base, "send-message", "нужен", "совет")...)
	if code != 0 {
		t.Fatalf("код=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "сообщение отправлено организатору") {
		t.Fatalf("вывод = %q", out)
	}
	form := e.lastForm()
	if form.Get("action") != "send_message" {
		t.Fatalf("action = %q (форма %v)", form.Get("action"), form)
	}
	if form.Get("content") != "нужен совет" {
		t.Fatalf("content = %q", form.Get("content"))
	}
}

func TestGamesCommand(t *testing.T) {
	// gamesList.php sits behind the same wall as the rest of the API, so the
	// command needs a session like any other.
	e, base := playing(t)

	code, out, errOut := runCLI(t, with(base, "games", "-new")...)
	if code != 0 {
		t.Fatalf("код=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"ID", "Ночной дозор: Осень", "Штаб", "4243"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод games не содержит %q:\n%s", want, out)
		}
	}
	if got := e.lastForm().Get("new"); got != "1" {
		t.Fatalf("флаг -new не дошёл до движка: %q", got)
	}

	code, out, errOut = runCLI(t, with(base, "-json", "games")...)
	if code != 0 {
		t.Fatalf("json games: код=%d stderr=%q", code, errOut)
	}
	var games []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &games); err != nil {
		t.Fatalf("вывод не JSON (%q): %v", out, err)
	}
	if len(games) != 2 || games[0].ID != 4242 {
		t.Fatalf("игры = %+v", games)
	}
}

func TestLoginStoresSession(t *testing.T) {
	isolate(t)
	_, base := newEngine(t)
	code, out, errOut := runCLI(t, "-base-url", base,
		"login", "-login", "demo", "-password", "secret", "-captain", "demo", "-pin", "1234")
	if code != 0 {
		t.Fatalf("код=%d stderr=%q", code, errOut)
	}
	path := filepath.Join(os.Getenv("HOME"), ".config", "dzzzr", "moscow.json")
	if !strings.Contains(out, "Сессия сохранена в "+path) {
		t.Fatalf("вывод = %q", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Token   string `json:"token"`
		Captain string `json:"captain"`
		Pin     string `json:"pin"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Token != "TOK" || saved.Captain != "demo" || saved.Pin != "1234" {
		t.Fatalf("сохранено %+v", saved)
	}

	// The stored session is enough for the next command.
	if code, _, errOut := runCLI(t, "-base-url", base, "status"); code != 0 {
		t.Fatalf("status после login: код=%d stderr=%q", code, errOut)
	}
}

func TestLoginFromFlagsWhenSessionMissing(t *testing.T) {
	isolate(t)
	_, base := newEngine(t)
	code, out, errOut := runCLI(t, "-base-url", base,
		"-login", "demo", "-password", "secret", "-captain", "demo", "-pin", "1234", "status")
	if code != 0 {
		t.Fatalf("код=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "MockTeam") {
		t.Fatalf("вывод = %q", out)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "dzzzr", "moscow.json")); err != nil {
		t.Fatalf("сессия не сохранена: %v", err)
	}
}

func TestAuthErrorNamesTheCredential(t *testing.T) {
	e, base := playing(t)
	e.setUnauthorized()
	code, _, errOut := runCLI(t, with(base, "status")...)
	if code != 1 {
		t.Fatalf("код=%d", code)
	}
	if !strings.Contains(errOut, "-captain/-pin") {
		t.Fatalf("подсказка не называет учётные данные: %q", errOut)
	}
}

func TestAuthErrorInJSONMode(t *testing.T) {
	e, base := playing(t)
	e.setUnauthorized()
	code, out, _ := runCLI(t, with(base, "-json", "status")...)
	if code != 1 {
		t.Fatalf("код=%d", code)
	}
	var got errorOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("вывод не JSON (%q): %v", out, err)
	}
	if !strings.Contains(got.Error, "-captain/-pin") {
		t.Fatalf("сообщение = %q", got.Error)
	}
}

func TestCaptainFlagOverridesSession(t *testing.T) {
	e, base := playing(t)
	if code, _, errOut := runCLI(t, with(base, "-captain", "other", "-pin", "9999", "send-code", "1")...); code != 0 {
		t.Fatalf("код=%d stderr=%q", code, errOut)
	}
	if user, pass := e.lastBasicAuth(t); user != "moscow_other" || pass != "9999" {
		t.Fatalf("движок получил %q:%q, ожидалось moscow_other:9999", user, pass)
	}
	// The session file itself is untouched by the override.
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config", "dzzzr", "moscow.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"captain":"demo"`) {
		t.Fatalf("файл сессии изменён: %s", data)
	}
}

func TestHARIsWrittenOnExit(t *testing.T) {
	_, base := playing(t)
	harPath := filepath.Join(t.TempDir(), "traffic.har")
	if code, _, errOut := runCLI(t, with(base, "-har-out", harPath, "status")...); code != 0 {
		t.Fatalf("код=%d stderr=%q", code, errOut)
	}
	data, err := os.ReadFile(harPath)
	if err != nil {
		t.Fatalf("HAR не записан: %v", err)
	}
	var har struct {
		Log struct {
			Entries []struct {
				Request struct {
					Method string `json:"method"`
				} `json:"request"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(data, &har); err != nil {
		t.Fatalf("HAR не JSON: %v", err)
	}
	if len(har.Log.Entries) == 0 {
		t.Fatal("в HAR нет записей")
	}
}

// The live engine sends this shape whenever the current level is a сквозное
// task: a doubled comma after the level object, only the second hint issued,
// a finished level with a break queued behind it, and the code inside a
// picture. Every one of those used to be lost or fatal.
func TestStatusAndLevelOnASkvozLevel(t *testing.T) {
	e, base := playing(t)
	e.setState("go_state_skvoz_level.json")

	code, out, errOut := runCLI(t, with(base, "status")...)
	if code != 0 {
		t.Fatalf("status: код выхода = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{
		"Уровень 12 из 13",
		"сквозное бонусное (10 мин.)",
		"выдана вторая",
		"Уровень выполнен",
		"запланирован перерыв",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status не содержит %q:\n%s", want, out)
		}
	}

	code, out, errOut = runCLI(t, with(base, "level")...)
	if code != 0 {
		t.Fatalf("level: код выхода = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{
		"Вложения:",
		// ../../ from /moscow/go/ lands at the host root, where the engine
		// really keeps uploads.
		"картинка: " + strings.TrimSuffix(base[1], "/moscow/") + "/uploaded/demo/Night/x_68417492.jpg",
		"ссылка «телеграм»: http://t.me/questDozoR",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("level не содержит %q:\n%s", want, out)
		}
	}
}

// In games with a free level order the engine sends the choices only as a
// rendered form, so select-level without an argument has to list them.
func TestSelectLevelListsTheChoices(t *testing.T) {
	e, base := playing(t)
	e.setState("go_state_level_choice.json")
	code, out, errOut := runCLI(t, with(base, "select-level")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{"4  Гаражи", "7  Пустырь"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод не содержит %q:\n%s", want, out)
		}
	}
	// Listing is a read: it must not post an action.
	if strings.Contains(out, "Принято") || strings.Contains(out, "Отклонено") {
		t.Errorf("список уровней отправил действие движку:\n%s", out)
	}
}

// The engine answers a control action it declined exactly as it answers one
// it performed: the whole state with errNo 0. Live, a refused abandon was
// reported as "выполнено".
func TestSilentControlActionDoesNotClaimSuccess(t *testing.T) {
	e, base := playing(t)
	e.setActionCode("")
	code, out, errOut := runCLI(t, with(base, "abandon")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	if strings.Contains(out, "выполнено") {
		t.Errorf("молчание движка выдано за успех: %q", out)
	}
	if !strings.Contains(out, "не сообщил результат") {
		t.Errorf("вывод = %q", out)
	}
}

// A stored session outlives the game it was made for. When the engine rejects
// it and the run carries -login/-password, the command must sign in again
// instead of sending the player to «dzzzr login».
func TestStaleSessionIsRefreshedAndTheCommandRetried(t *testing.T) {
	e, base := playing(t)
	e.rejectSessionOnce()
	code, out, errOut := runCLI(t, with(base, "status", "-login", "demo", "-password", "secret")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stdout=%q stderr=%q", code, out, errOut)
	}
	if !strings.Contains(out, "Ночной дозор: Осень") {
		t.Fatalf("состояние не получено после повторного входа:\n%s", out)
	}
	if e.logins() != 1 {
		t.Errorf("повторных входов %d, ожидался один", e.logins())
	}
}

// Without credentials there is nothing to sign in with, so the engine's
// verdict has to reach the user unchanged.
func TestStaleSessionWithoutCredentialsIsReported(t *testing.T) {
	e, base := playing(t)
	e.rejectSessionOnce()
	code, _, errOut := runCLI(t, with(base, "status")...)
	if code == 0 {
		t.Fatalf("ожидалась ошибка, stderr=%q", errOut)
	}
	if e.logins() != 0 {
		t.Errorf("вход выполнен без учётных данных: %d", e.logins())
	}
}

// The structured output must not tell a script that a refused action was
// accepted, and must not fail the command when the engine simply said
// nothing.
func TestSilentActionIsReportedAsUnknownInJSON(t *testing.T) {
	e, base := playing(t)
	e.setActionCode("")
	code, out, errOut := runCLI(t, with(base, "abandon", "-json")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	var got struct {
		Accepted bool   `json:"accepted"`
		Outcome  string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("разбор %q: %v", out, err)
	}
	if got.Accepted || got.Outcome != "unknown" {
		t.Fatalf("вывод = %q", out)
	}
}

// A bonus task that does not run alongside the main line is not сквозное.
func TestLevelKindNamesBonusAndSkvozSeparately(t *testing.T) {
	for _, tc := range []struct{ skvoz, bonus, want string }{
		{"true", "1", "сквозное бонусное"},
		{"true", "0", "сквозное"},
		{"false", "1", "бонусное"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			e, base := playing(t)
			e.setState(writeLevelKindFixture(t, tc.skvoz, tc.bonus))
			_, out, _ := runCLI(t, with(base, "status")...)
			if !strings.Contains(out, "Задание:        "+tc.want) {
				t.Fatalf("вывод не называет задание %q:\n%s", tc.want, out)
			}
		})
	}
}

// writeLevelKindFixture copies the сквозное fixture with the two kind flags
// set as asked and returns its name.
func writeLevelKindFixture(t *testing.T, skvoz, bonus string) string {
	t.Helper()
	src := fixture(t, "go_state_skvoz_level.json")
	body := strings.Replace(string(src), `"skvoz": true`, `"skvoz": `+skvoz, 1)
	body = strings.Replace(body, `"isBonusLevel": "1"`, `"isBonusLevel": "`+bonus+`"`, 1)
	name := "go_state_kind_" + skvoz + "_" + bonus + ".json"
	path := filepath.Join("..", "..", "dzzzr", "testdata", name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return name
}

// An embedded picture is the task itself, so it must not be lost — but a
// terminal cannot show it and its URI would dump a blob into the scrollback,
// so the CLI names it instead. A mobile client gets the same ref with the
// data: URI intact.
func TestLevelDescribesAnInlinePictureInsteadOfPrintingIt(t *testing.T) {
	e, base := playing(t)
	e.setState("go_state_inline_image.json")
	code, out, errOut := runCLI(t, with(base, "level")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	if strings.Contains(out, "base64") || strings.Contains(out, "R0lGOD") {
		t.Fatalf("в вывод попали байты картинки:\n%s", out)
	}
	if !strings.Contains(out, "картинка: встроена в задание (image/gif, 42 Б)") {
		t.Fatalf("вывод не описывает встроенную картинку:\n%s", out)
	}
}

// A deployment that answers an action with the state carries errText on every
// reply, not only on an answer to an action. Standing text beside errNo 0
// must not fail the command: before this was guarded, a break the engine
// performed exited 2 and printed «Отклонено».
func TestStandingEngineTextDoesNotFailAControlAction(t *testing.T) {
	e, base := playing(t)
	e.setActionBody(`{"errNo":0,"errText":"Приветствуем участников команды!"}`)
	code, out, errOut := runCLI(t, with(base, "break", "-json")...)
	if code != 0 {
		t.Fatalf("код выхода = %d, stdout=%q stderr=%q", code, out, errOut)
	}
	var got struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("разбор %q: %v", out, err)
	}
	if got.Outcome != "unknown" {
		t.Fatalf("outcome = %q, ожидалось unknown: %s", got.Outcome, out)
	}
}
