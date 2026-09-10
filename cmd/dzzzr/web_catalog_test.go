package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// catalogHub builds a hub whose engine client points at srv and whose city is
// "moscow", the way «dzzzr web -city moscow» would.
func catalogHub(t *testing.T, srv *httptest.Server, opts ...dzzzr.Option) *webHub {
	t.Helper()
	hub := newTestHub(t, func(context.Context, *webHub, string) {})
	hub.cfg.city = "moscow"
	base := []dzzzr.Option{dzzzr.WithBaseURL(srv.URL + "/moscow/")}
	hub.client = dzzzr.New("moscow", append(base, opts...)...)
	return hub
}

// adminGamesFixture is the same page dzzzr's own admin tests parse.
func adminGamesFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "dzzzr", "testdata", "admin_games_list.html"))
	if err != nil {
		t.Skipf("admin games fixture unavailable: %v", err)
	}
	return b
}

// TestWebCatalogFallsBackToAdminList reproduces «dzzzr web -admin-login … »: no
// playing session, so gamesList.php answers with a session error, and the
// list still comes back from the administration area.
func TestWebCatalogFallsBackToAdminList(t *testing.T) {
	page := adminGamesFixture(t)
	var sawAdmin bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/moscow/API/gamesList.php":
			// The exact wording the user reported.
			_, _ = w.Write([]byte(`{"error":"Ошибка авторизации. Неверный идентификатор сессии"}`))
		case strings.HasPrefix(r.URL.Path, "/moscow/admin/"):
			if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("org:secret")) {
				w.Header().Set("WWW-Authenticate", `Basic realm="Admin area"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("action") != "games" {
				http.NotFound(w, r)
				return
			}
			sawAdmin = true
			w.Header().Set("Content-Type", "text/html; charset=windows-1251")
			_, _ = w.Write(page)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// A stale playing session, exactly as a leftover «dzzzr login» leaves one.
	hub := catalogHub(t, srv, dzzzr.WithSession("STALE"), dzzzr.WithAdminCredentials("org", "secret"))
	tsrv := httptest.NewServer(hub.newMux())
	defer tsrv.Close()

	var out struct {
		Games []webGame `json:"games"`
		Error string    `json:"error"`
	}
	if code := webDo(t, tsrv, http.MethodGet, "/api/v1/catalog/games", "", &out); code != http.StatusOK {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
	if !sawAdmin {
		t.Fatal("admin games page was never requested")
	}
	if len(out.Games) != 3 {
		t.Fatalf("games = %+v", out.Games)
	}
	// Sorted by ID ascending, names from the admin table.
	want := []webGame{
		{ID: 4100, Name: "Летняя игра"},
		{ID: 4242, Name: "Ночной дозор: Осень"},
		{ID: 4243, Name: "Будущая игра"},
	}
	for i, g := range want {
		if out.Games[i].ID != g.ID || out.Games[i].Name != g.Name {
			t.Errorf("games[%d] = %+v, want id %d name %q", i, out.Games[i], g.ID, g.Name)
		}
	}
}

// TestWebCatalogPlayerListOnly checks the ordinary path is untouched: a valid
// session lists games and no admin call is made.
func TestWebCatalogPlayerListOnly(t *testing.T) {
	var sawAdmin bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/moscow/API/gamesList.php":
			_, _ = w.Write([]byte(`{"games":[{"id":7,"name":"Седьмая игра","start":"2026-10-01 20:00"}]}`))
		case strings.HasPrefix(r.URL.Path, "/moscow/admin/"):
			sawAdmin = true
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	hub := catalogHub(t, srv,
		dzzzr.WithSession("TOK"),
		dzzzr.WithCredentials(dzzzr.Credentials{Captain: "cap", Pin: "0000"}),
	)
	tsrv := httptest.NewServer(hub.newMux())
	defer tsrv.Close()

	var out struct {
		Games []webGame `json:"games"`
	}
	if code := webDo(t, tsrv, http.MethodGet, "/api/v1/catalog/games", "", &out); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if sawAdmin {
		t.Error("admin page was queried though the player list answered")
	}
	if len(out.Games) != 1 || out.Games[0].ID != 7 || out.Games[0].Start != "2026-10-01 20:00" {
		t.Fatalf("games = %+v", out.Games)
	}
}

// TestWebCatalogReportsWhenNothingAnswers keeps the error visible when neither
// source produced a game.
func TestWebCatalogReportsWhenNothingAnswers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"Ошибка авторизации. Неверный идентификатор сессии"}`))
	}))
	defer srv.Close()

	hub := catalogHub(t, srv)
	tsrv := httptest.NewServer(hub.newMux())
	defer tsrv.Close()

	var out struct {
		Error string `json:"error"`
	}
	code := webDo(t, tsrv, http.MethodGet, "/api/v1/catalog/games", "", &out)
	if code != http.StatusBadGateway || out.Error == "" {
		t.Fatalf("status = %d, error = %q", code, out.Error)
	}
}
