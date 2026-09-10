package main

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/dzzzr-cli/agentloop"
)

// newTestHub builds a hub over a private HOME, with the turn runner the test
// asks for. A nil runner leaves the real one in place.
func newTestHub(t *testing.T, run webRunTurnFn) *webHub {
	t.Helper()
	home := isolate(t)
	cfg := &config{
		city:     "moscow",
		security: "approve",
		stdin:    strings.NewReader(""),
		stdout:   io.Discard,
		stderr:   io.Discard,
	}
	return &webHub{
		cfg:    cfg,
		client: newClient(cfg),
		store:  newChatStore(filepath.Join(home, ".config", "dzzzr", "web", "chats")),
		sse:    newSSEHub(),
		run:    run,
	}
}

// webDo runs one request against a hub's mux and decodes the JSON answer.
func webDo(t *testing.T, srv *httptest.Server, method, path, body string, out any) int {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s %s: не прочитан ответ: %v", method, path, err)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s %s: ответ не разобран (%s): %v", method, path, data, err)
		}
	}
	return res.StatusCode
}

func TestWebChatCRUD(t *testing.T) {
	hub := newTestHub(t, func(context.Context, *webHub, string) {})
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()

	var created chatSnapshot
	if code := webDo(t, srv, http.MethodPost, "/api/v1/chats", `{}`, &created); code != http.StatusCreated {
		t.Fatalf("создание чата вернуло %d", code)
	}
	if created.ID == "" || created.City != "moscow" || string(created.Policy) != "approve" {
		t.Fatalf("неожиданный чат: %+v", created)
	}

	var list struct {
		Chats []webChatListItem `json:"chats"`
	}
	if code := webDo(t, srv, http.MethodGet, "/api/v1/chats", "", &list); code != http.StatusOK {
		t.Fatalf("список чатов вернул %d", code)
	}
	if len(list.Chats) != 1 || list.Chats[0].ID != created.ID || list.Chats[0].City != "moscow" {
		t.Fatalf("список чатов: %+v", list.Chats)
	}

	var got chatSnapshot
	if code := webDo(t, srv, http.MethodGet, "/api/v1/chats/"+created.ID, "", &got); code != http.StatusOK {
		t.Fatalf("чтение чата вернуло %d", code)
	}
	if got.ID != created.ID {
		t.Fatalf("прочитан другой чат: %+v", got)
	}

	var patched chatSnapshot
	code := webDo(t, srv, http.MethodPatch, "/api/v1/chats/"+created.ID,
		`{"title":"разбор уровня","policy":"readonly"}`, &patched)
	if code != http.StatusOK {
		t.Fatalf("правка чата вернула %d", code)
	}
	if patched.Title != "разбор уровня" || string(patched.Policy) != "readonly" {
		t.Fatalf("правка не применена: %+v", patched)
	}

	if code := webDo(t, srv, http.MethodPatch, "/api/v1/chats/"+created.ID, `{"policy":"что-то"}`, nil); code != http.StatusBadRequest {
		t.Errorf("неизвестные права приняты с кодом %d", code)
	}

	if code := webDo(t, srv, http.MethodDelete, "/api/v1/chats/"+created.ID, "", nil); code != http.StatusOK {
		t.Fatalf("удаление чата вернуло %d", code)
	}
	if code := webDo(t, srv, http.MethodGet, "/api/v1/chats/"+created.ID, "", nil); code != http.StatusNotFound {
		t.Errorf("удалённый чат ещё читается, код %d", code)
	}
}

func TestWebChatSearchFiltersByTranscript(t *testing.T) {
	hub := newTestHub(t, func(context.Context, *webHub, string) {})
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()

	keep := hub.store.create("moscow", "approve")
	drop := hub.store.create("moscow", "approve")
	hub.store.appendLine(keep.ID, chatRoleAssistant, "код принят на седьмом уровне", "")
	hub.store.appendLine(drop.ID, chatRoleAssistant, "ничего интересного", "")

	var list struct {
		Chats []webChatListItem `json:"chats"`
	}
	if code := webDo(t, srv, http.MethodGet, "/api/v1/chats?q=%D1%81%D0%B5%D0%B4%D1%8C%D0%BC%D0%BE%D0%BC", "", &list); code != http.StatusOK {
		t.Fatalf("поиск вернул %d", code)
	}
	if len(list.Chats) != 1 || list.Chats[0].ID != keep.ID {
		t.Fatalf("поиск по переписке нашёл %+v", list.Chats)
	}
}

func TestWebPostMessageStartsRunAndRefusesSecond(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	hub := newTestHub(t, func(ctx context.Context, h *webHub, chatID string) {
		if _, ok := h.store.beginRun(chatID, func() {}); !ok {
			return
		}
		started <- chatID
		<-release
		h.store.finishRun(chatID, nil)
	})
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()
	defer close(release)

	var created chatSnapshot
	webDo(t, srv, http.MethodPost, "/api/v1/chats", `{}`, &created)

	var accepted chatSnapshot
	code := webDo(t, srv, http.MethodPost, "/api/v1/chats/"+created.ID+"/messages", `{"content":"что с уровнем"}`, &accepted)
	if code != http.StatusAccepted {
		t.Fatalf("отправка сообщения вернула %d", code)
	}
	if len(accepted.Lines) != 1 || accepted.Lines[0].Role != chatRoleUser {
		t.Fatalf("сообщение не попало в переписку: %+v", accepted.Lines)
	}

	select {
	case id := <-started:
		if id != created.ID {
			t.Fatalf("запущен не тот чат: %s", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ход агента не начался")
	}

	code = webDo(t, srv, http.MethodPost, "/api/v1/chats/"+created.ID+"/messages", `{"content":"ещё"}`, nil)
	if code != http.StatusConflict {
		t.Errorf("второе сообщение во время хода вернуло %d, ожидался 409", code)
	}

	if code := webDo(t, srv, http.MethodPost, "/api/v1/chats/"+created.ID+"/cancel", "", nil); code != http.StatusOK {
		t.Errorf("отмена вернула %d", code)
	}

	if code := webDo(t, srv, http.MethodPost, "/api/v1/chats/"+created.ID+"/messages", `{}`, nil); code != http.StatusBadRequest {
		t.Errorf("пустое сообщение принято с кодом %d", code)
	}
}

func TestWebRunTurnAnswersThroughScriptedProvider(t *testing.T) {
	hub := newTestHub(t, runWebChatTurn)
	t.Setenv("DZZZR_LLM_API_KEY", "тест")
	useProvider(t, &scriptedProvider{replies: []*providers.LLMResponse{answer("Уровень взят.")}})

	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()

	var created chatSnapshot
	webDo(t, srv, http.MethodPost, "/api/v1/chats", `{"policy":"readonly"}`, &created)
	if code := webDo(t, srv, http.MethodPost, "/api/v1/chats/"+created.ID+"/messages", `{"content":"что с уровнем"}`, nil); code != http.StatusAccepted {
		t.Fatal("сообщение не принято")
	}

	deadline := time.Now().Add(15 * time.Second)
	var snap chatSnapshot
	for time.Now().Before(deadline) {
		var ok bool
		if snap, ok = hub.store.get(created.ID); ok && !snap.Running && len(snap.Lines) > 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if snap.Running {
		t.Fatal("ход агента не завершился")
	}
	var answered bool
	for _, line := range snap.Lines {
		if line.Role == chatRoleAssistant && strings.Contains(line.Content, "Уровень взят.") {
			answered = true
		}
	}
	if !answered {
		t.Fatalf("ответ модели не попал в переписку: %+v", snap.Lines)
	}
}

func TestWebExportFormats(t *testing.T) {
	hub := newTestHub(t, func(context.Context, *webHub, string) {})
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()

	snap := hub.store.create("moscow", "approve")
	hub.store.appendLine(snap.ID, chatRoleAssistant, "готово", "")

	for _, tc := range []struct {
		format string
		want   string
	}{
		{"", "text/markdown"},
		{"markdown", "text/markdown"},
		{"json", "application/json"},
	} {
		res, err := srv.Client().Get(srv.URL + "/api/v1/chats/" + snap.ID + "/export?format=" + tc.format)
		if err != nil {
			t.Fatalf("экспорт %q: %v", tc.format, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("экспорт %q вернул %d", tc.format, res.StatusCode)
		}
		if !strings.HasPrefix(res.Header.Get("Content-Type"), tc.want) {
			t.Errorf("экспорт %q: тип %q, ожидался %q", tc.format, res.Header.Get("Content-Type"), tc.want)
		}
		if !strings.Contains(string(body), "готово") {
			t.Errorf("экспорт %q не содержит переписку: %s", tc.format, body)
		}
	}

	if code := webDo(t, srv, http.MethodGet, "/api/v1/chats/"+snap.ID+"/export?format=pdf", "", nil); code != http.StatusBadRequest {
		t.Errorf("неизвестный формат принят с кодом %d", code)
	}
}

func TestWebAuthStatusShape(t *testing.T) {
	hub := newTestHub(t, nil)
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()

	var st authStatus
	if code := webDo(t, srv, http.MethodGet, "/api/v1/auth/status", "", &st); code != http.StatusOK {
		t.Fatalf("статус авторизации вернул %d", code)
	}
	if st.City != "moscow" || st.HasSession {
		t.Fatalf("неожиданный статус: %+v", st)
	}

	if code := webDo(t, srv, http.MethodPost, "/api/v1/auth/login", `{"login":"","password":""}`, nil); code != http.StatusBadRequest {
		t.Errorf("пустой вход принят с кодом %d", code)
	}

	// Logging out without a session must still leave a usable status rather
	// than failing on the missing file.
	if code := webDo(t, srv, http.MethodPost, "/api/v1/auth/logout", `{}`, &st); code != http.StatusOK {
		t.Fatalf("выход вернул %d", code)
	}
}

func TestWebAgentConfigReportsMissingModel(t *testing.T) {
	hub := newTestHub(t, nil)
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()

	type agentConfigBody struct {
		Model        string `json:"model"`
		FilesEnabled bool   `json:"files_enabled"`
		Error        string `json:"error"`
	}

	// A missing key is answered with 200 and an explanation, because the rest
	// of the interface still works without a model.
	var missing agentConfigBody
	if code := webDo(t, srv, http.MethodGet, "/api/v1/agent/config", "", &missing); code != http.StatusOK {
		t.Fatalf("настройки агента вернули %d", code)
	}
	if missing.Model != "" || missing.Error == "" {
		t.Fatalf("без ключа модели ожидалась ошибка в теле: %+v", missing)
	}

	t.Setenv("DZZZR_LLM_API_KEY", "тест")
	t.Setenv("DZZZR_LLM_MODEL", "тестовая-модель")
	t.Setenv("DZZZR_FILES_ROOT", t.TempDir())
	var ready agentConfigBody
	if code := webDo(t, srv, http.MethodGet, "/api/v1/agent/config", "", &ready); code != http.StatusOK {
		t.Fatalf("настройки агента вернули %d", code)
	}
	if ready.Model != "тестовая-модель" || !ready.FilesEnabled || ready.Error != "" {
		t.Fatalf("настройки агента: %+v", ready)
	}
}

func TestWebServesEmbeddedInterface(t *testing.T) {
	hub := newTestHub(t, nil)
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()

	res, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("корень вернул %d", res.StatusCode)
	}
	if !strings.Contains(string(body), "<title>dzzzr") {
		t.Fatalf("отдан не тот документ: %.120s", body)
	}
}

func TestFormatSSEFramesOneEvent(t *testing.T) {
	frame := string(formatSSE(agentloop.EventToolStart, map[string]any{"name": "read_level"}))
	if !strings.HasPrefix(frame, "event: tool_start\ndata: ") {
		t.Fatalf("заголовок кадра: %q", frame)
	}
	if !strings.HasSuffix(frame, "\n\n") {
		t.Fatalf("кадр не закрыт пустой строкой: %q", frame)
	}
	if !strings.Contains(frame, `"name":"read_level"`) {
		t.Fatalf("полезная нагрузка потеряна: %q", frame)
	}
	if strings.Count(frame, "\n") != 3 {
		t.Fatalf("в кадре лишние переводы строки: %q", frame)
	}
}

func TestSSEHubDeliversAndClosesOnRemove(t *testing.T) {
	hub := newSSEHub()
	ch := hub.room("abc").subscribe(4)
	hub.room("abc").broadcast([]byte("привет"))
	select {
	case frame := <-ch:
		if string(frame) != "привет" {
			t.Fatalf("получено %q", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("кадр не доставлен")
	}
	hub.removeChat("abc")
	if _, open := <-ch; open {
		t.Fatal("канал не закрыт вместе с чатом")
	}
}

// The web command has to be reachable the same way every other one is.
func TestWebCommandIsRegistered(t *testing.T) {
	c := findCommand("web")
	if c == nil {
		t.Fatal("команда web не зарегистрирована")
	}
	if c.Auth != authNone {
		t.Errorf("web требует уровень доступа %d, ожидался authNone", c.Auth)
	}
	code, out, _ := runCLI(t, "web", "-h")
	if code != 0 || !strings.Contains(out, "web-addr") {
		t.Errorf("справка web не упоминает -web-addr: %s", out)
	}
}
