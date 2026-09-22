package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// The editor's tests reuse the administration-area double the CLI's own admin
// tests already drive (adminEngine in admin_test.go): it serves the library's
// HTML fixtures, retargets each form at the row the request asked for and
// replays deletes and moves, which is exactly the behavior these handlers
// have to survive. A second double would only be a second thing to keep true.

// newAdminWebHub builds a hub whose client talks to the double at base,
// already carrying the organizer credentials unless withAdmin is false.
func newAdminWebHub(t *testing.T, base string, withAdmin bool) *webHub {
	t.Helper()
	hub := newTestHub(t, func(context.Context, *webHub, string) {})
	hub.cfg.city = "moscow"
	opts := []dzzzr.Option{dzzzr.WithBaseURL(base), dzzzr.WithAdminDelay(0)}
	if withAdmin {
		hub.cfg.adminLogin, hub.cfg.adminPassword = "org", "secret"
		opts = append(opts, dzzzr.WithAdminCredentials("org", "secret"))
	}
	hub.client = dzzzr.New("moscow", opts...)
	return hub
}

// adminWebServer starts the editor's own HTTP surface over the hub.
func adminWebServer(t *testing.T, hub *webHub) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(hub.newMux())
	t.Cleanup(srv.Close)
	return srv
}

// editing is the pair every editor test starts from: the fake engine and the
// editor served over it.
func editing(t *testing.T, withAdmin bool) (*adminEngine, *httptest.Server) {
	t.Helper()
	e, base := newAdminEngine(t)
	return e, adminWebServer(t, newAdminWebHub(t, base, withAdmin))
}

func TestWebAdminStatusAndLogin(t *testing.T) {
	_, srv := editing(t, false)

	var status adminStatus
	if code := webDo(t, srv, http.MethodGet, "/api/v1/admin/status", "", &status); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if status.HasAdmin {
		t.Fatal("организатор найден до входа")
	}

	if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/login", `{"login":"org","password":"secret"}`, &status); code != http.StatusOK {
		t.Fatalf("вход = %d", code)
	}
	if !status.HasAdmin || status.Login != "org" {
		t.Fatalf("после входа status = %+v", status)
	}

	// The session file is what the next dzzzr command reads, so an editor
	// login has to be worth as much as the -admin-login flags were.
	path, err := sessionPath("moscow")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл сессии не записан: %v", err)
	}
	if !strings.Contains(string(saved), "org") {
		t.Fatalf("в файле сессии нет организатора: %s", saved)
	}

	if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/logout", "{}", &status); code != http.StatusOK {
		t.Fatalf("выход = %d", code)
	}
	if status.HasAdmin {
		t.Fatalf("после выхода status = %+v", status)
	}
}

func TestWebAdminLoginRejectsWrongPassword(t *testing.T) {
	_, srv := editing(t, false)

	var out struct {
		Error string `json:"error"`
	}
	code := webDo(t, srv, http.MethodPost, "/api/v1/admin/login", `{"login":"org","password":"nope"}`, &out)
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
	if out.Error == "" {
		t.Fatal("отказ без объяснения")
	}

	// A refused login must not leave the credentials behind.
	var status adminStatus
	webDo(t, srv, http.MethodGet, "/api/v1/admin/status", "", &status)
	if status.HasAdmin {
		t.Fatal("отклонённые креды сохранились")
	}
}

// The four routes that deliberately answer without organizer credentials are
// not in this table and must not be added to it: GET /admin/schema and POST
// /admin/validate describe and check the document the browser already holds,
// POST /admin/scenario/validate reads the file an author has not uploaded yet,
// and POST /admin/handoff only seeds a chat. None of them reaches the engine.
func TestWebAdminRequiresCredentials(t *testing.T) {
	_, srv := editing(t, false)

	requiresAdmin := func(name string, code int, message string) {
		t.Helper()
		if code != http.StatusForbidden {
			t.Errorf("%s: status = %d, ожидался 403", name, code)
			return
		}
		if !strings.Contains(message, "организатора") {
			t.Errorf("%s: сообщение = %q, в нём нет подсказки про организатора", name, message)
		}
	}

	for _, probe := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/admin/games", ""},
		{http.MethodPost, "/api/v1/admin/games", `{"params":{"name":"x"}}`},
		{http.MethodGet, "/api/v1/admin/games/4242", ""},
		{http.MethodPatch, "/api/v1/admin/games/4242", `{"params":{"name":"x"}}`},
		{http.MethodDelete, "/api/v1/admin/games/4242", ""},
		{http.MethodPost, "/api/v1/admin/games/4242/copy", `{"with_levels":true}`},
		{http.MethodGet, "/api/v1/admin/games/4242/levels", ""},
		{http.MethodPost, "/api/v1/admin/games/4242/levels", `{"params":{"title":"x"}}`},
		{http.MethodGet, "/api/v1/admin/games/4242/levels/901", ""},
		{http.MethodPatch, "/api/v1/admin/games/4242/levels/901", `{"params":{"title":"x"}}`},
		{http.MethodDelete, "/api/v1/admin/games/4242/levels/901", ""},
		{http.MethodPost, "/api/v1/admin/games/4242/levels/901/move", `{"up":true}`},
		{http.MethodGet, "/api/v1/admin/games/4242/scenario", ""},
		{http.MethodPost, "/api/v1/admin/scenario/import", `{"scenario":{},"game_id":0}`},
	} {
		var out struct {
			Error string `json:"error"`
		}
		code := webDo(t, srv, probe.method, probe.path, probe.body, &out)
		requiresAdmin(probe.method+" "+probe.path, code, out.Error)
	}

	// The upload takes a multipart body, so it cannot ride the table above.
	code, raw := webDoMultipart(t, srv, []byte("привет"), nil)
	requiresAdmin("POST /api/v1/admin/games/{gameID}/files", code, string(raw))
}

func TestWebAdminListGames(t *testing.T) {
	_, srv := editing(t, true)

	var out struct {
		Games []webAdminGame `json:"games"`
		Error string         `json:"error"`
	}
	if code := webDo(t, srv, http.MethodGet, "/api/v1/admin/games", "", &out); code != http.StatusOK {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
	if len(out.Games) == 0 {
		t.Fatal("список игр пуст")
	}
	for _, g := range out.Games {
		if g.ID <= 0 || g.Name == "" {
			t.Fatalf("игра без идентификатора или названия: %+v", g)
		}
	}
}

func TestWebAdminGameRoundTrip(t *testing.T) {
	engine, srv := editing(t, true)

	var got struct {
		ID     int              `json:"id"`
		Params dzzzr.GameParams `json:"params"`
		Error  string           `json:"error"`
	}
	if code := webDo(t, srv, http.MethodGet, "/api/v1/admin/games/4242", "", &got); code != http.StatusOK {
		t.Fatalf("чтение игры = %d, error = %q", code, got.Error)
	}
	if got.ID != 4242 {
		t.Fatalf("id = %d, ожидалось 4242", got.ID)
	}

	body := `{"params":{"name":"Переименованная","number":"12"}}`
	var saved struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if code := webDo(t, srv, http.MethodPatch, "/api/v1/admin/games/4242", body, &saved); code != http.StatusOK {
		t.Fatalf("сохранение = %d, error = %q", code, saved.Error)
	}
	form := engine.lastPost()
	if form.Get("action") != "update_game" {
		t.Fatalf("action = %q", form.Get("action"))
	}
	if form.Get("name") != "Переименованная" {
		t.Fatalf("name = %q", form.Get("name"))
	}
	if form.Get("number") != "12" {
		t.Fatalf("number = %q", form.Get("number"))
	}
}

func TestWebAdminRejectsUnknownParam(t *testing.T) {
	_, srv := editing(t, true)

	var out struct {
		Error string `json:"error"`
	}
	code := webDo(t, srv, http.MethodPost, "/api/v1/admin/games", `{"params":{"naem":"опечатка"}}`, &out)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
}

func TestWebAdminRejectsBadGameID(t *testing.T) {
	_, srv := editing(t, true)

	var out struct {
		Error string `json:"error"`
	}
	if code := webDo(t, srv, http.MethodGet, "/api/v1/admin/games/0", "", &out); code != http.StatusBadRequest {
		t.Fatalf("id 0: status = %d", code)
	}
	if code := webDo(t, srv, http.MethodGet, "/api/v1/admin/games/abc", "", &out); code != http.StatusBadRequest {
		t.Fatalf("id abc: status = %d", code)
	}
}

// TestWebAdminLoginOpensAgentTools is the point of putting the editor and the
// chat behind one server: signing in as an organizer on one side hands the
// other side its admin_* tools, because the tool catalog is built per turn
// from the same client. An author can lay a level out by hand and then ask the
// agent about it without signing in twice.
func TestWebAdminLoginOpensAgentTools(t *testing.T) {
	_, base := newAdminEngine(t)
	hub := newAdminWebHub(t, base, false)
	srv := adminWebServer(t, hub)

	catalog, err := hub.catalog("chat", agenttools.PolicyReadonly)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.Lookup("admin_levels"); ok {
		t.Fatal("админские инструменты доступны без организатора")
	}

	var status adminStatus
	if code := webDo(t, srv, http.MethodPost, "/api/v1/admin/login", `{"login":"org","password":"secret"}`, &status); code != http.StatusOK {
		t.Fatalf("вход = %d", code)
	}

	catalog, err = hub.catalog("chat", agenttools.PolicyReadonly)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.Lookup("admin_levels"); !ok {
		t.Fatal("после входа в редакторе агент так и не получил admin_levels")
	}
}

// TestWebAuthorizeStartsWithoutPlayerSession covers the organizer-only run:
// «dzzzr -admin-login … -admin-password … web» has no playing session and
// needs none, and the browser can sign a player in by itself. Refusing to
// start would hide the one screen that fixes the problem.
func TestWebAuthorizeStartsWithoutPlayerSession(t *testing.T) {
	_, base := newAdminEngine(t)

	for _, tc := range []struct {
		name          string
		admin         bool
		leaveSession  bool
		wantInMessage string
	}{
		{name: "без всего", wantInMessage: "войдите в браузере"},
		{name: "только организатор", admin: true, wantInMessage: "организаторской части она не нужна"},
		{name: "файл сессии без токена", admin: true, leaveSession: true, wantInMessage: "организаторской части она не нужна"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hub := newAdminWebHub(t, base, tc.admin)
			var stderr strings.Builder
			hub.cfg.stderr = &stderr

			if tc.leaveSession {
				// A session file left by an earlier organizer login carries no
				// playing token. That is the shape that used to refuse to start.
				if _, err := saveSession(hub.cfg, hub.client); err != nil {
					t.Fatal(err)
				}
			}

			if err := webAuthorize(context.Background(), hub.cfg, hub.client); err != nil {
				t.Fatalf("web отказался стартовать: %v", err)
			}
			if !strings.Contains(stderr.String(), tc.wantInMessage) {
				t.Errorf("stderr = %q, в нём нет %q", stderr.String(), tc.wantInMessage)
			}
			if tc.admin && !hub.client.HasAdminCredentials() {
				t.Error("организаторские данные потерялись при запуске")
			}
		})
	}
}

// TestWebServesEditorAssets keeps the editor's own files inside the embedded
// interface: the binary is the whole distribution, so a file that is not
// embedded is a file the author never sees.
func TestWebServesEditorAssets(t *testing.T) {
	hub := newTestHub(t, func(context.Context, *webHub, string) {})
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()

	for _, name := range []string{"/editor.js", "/editor.css", "/index.html"} {
		res, err := srv.Client().Get(srv.URL + name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d", name, res.StatusCode)
		}
	}
}
