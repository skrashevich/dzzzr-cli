package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func TestAgentAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name        string
		stored      bool
		credentials bool
		rejectLogin bool
		rejectFresh bool
		wantError   bool
		wantLogins  int
		wantReads   int
	}{
		{name: "flags without session", credentials: true, wantLogins: 1, wantReads: 1},
		{name: "expired session", stored: true, credentials: true, wantLogins: 1, wantReads: 2},
		{name: "rejected fresh session stops retry", stored: true, credentials: true, rejectFresh: true, wantError: true, wantLogins: 1, wantReads: 2},
		{name: "wrong password at startup", credentials: true, rejectLogin: true, wantError: true, wantLogins: 1},
		{name: "wrong password during refresh", stored: true, credentials: true, rejectLogin: true, wantError: true, wantLogins: 1, wantReads: 1},
		{name: "no credentials", stored: true, wantError: true, wantReads: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			logins, reads := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/moscow/API/login.php":
					logins++
					if r.URL.Query().Get("login") != "player" || r.URL.Query().Get("password") != "secret" {
						t.Error("credentials not forwarded")
					}
					if tc.rejectLogin {
						_, _ = fmt.Fprint(w, `{"code":4}`)
						return
					}
					_, _ = fmt.Fprint(w, `{"code":2,"userToken":"FRESH","userName":"Player"}`)
				case "/moscow/API/gamesList.php":
					reads++
					if r.URL.Query().Get("s") != "FRESH" || tc.rejectFresh {
						_, _ = fmt.Fprint(w, `{"error":"Ошибка авторизации. Неверный идентификатор сессии"}`)
						return
					}
					_, _ = fmt.Fprint(w, `{"games":[]}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			args := []string{"-city", "moscow", "-security", "readonly"}
			if tc.credentials {
				args = append(args, "-login", "player", "-password", "secret")
			}
			cfg, _ := parseFor(t, args)
			cfg.stderr = io.Discard
			c := dzzzr.New("moscow", dzzzr.WithBaseURL(srv.URL+"/moscow/"))
			if tc.stored {
				if err := c.ImportSession([]byte(`{"token":"STALE","login":"player"}`)); err != nil {
					t.Fatal(err)
				}
				if _, err := saveSession(cfg, c); err != nil {
					t.Fatal(err)
				}
				c.Logout()
			}
			err := agentAuthorize(t.Context(), cfg, c)
			gotError := err != nil
			if err == nil {
				cat, err := newAgentCatalog(cfg, c, nil)
				if err != nil {
					t.Fatal(err)
				}
				tool, ok := cat.Lookup("games_list")
				if !ok {
					t.Fatal("no games_list")
				}
				result := tool.Execute(t.Context(), nil)
				gotError = result.IsError
				if strings.Contains(result.Content, "secret") {
					t.Fatal("password leaked to model")
				}
				if tc.rejectLogin && !strings.Contains(result.Content, "пароль") {
					t.Errorf("missing login failure: %s", result.Content)
				}
			}
			if gotError != tc.wantError || logins != tc.wantLogins || reads != tc.wantReads {
				t.Fatalf("error=%v logins=%d reads=%d; want %v/%d/%d (startup: %v)", gotError, logins, reads, tc.wantError, tc.wantLogins, tc.wantReads, err)
			}
			if !tc.wantError {
				restored := dzzzr.New("moscow")
				if _, _, err := loadSession(cfg, restored); err != nil {
					t.Fatal(err)
				}
				if restored.Session() != "FRESH" {
					t.Fatal("fresh session was not saved")
				}
			}
		})
	}
}

// The organizer-only run is allowed to start without a playing session, and
// that permission is keyed to the credentials the run itself was given. The
// session file carries the organizer too — saveSession writes it, and a login
// in the editor's browser panel saves one — so keying it to the client would
// mean that one browser login turns a wrong player password into a warning for
// every later «dzzzr chat» in that city.
func TestAgentAuthorizeOrganizerRunOnlyFromFlags(t *testing.T) {
	for _, tc := range []struct {
		name      string
		flags     bool
		wantError bool
	}{
		{name: "организатор только из файла сессии", wantError: true},
		{name: "организатор задан в командной строке", flags: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/moscow/API/login.php" {
					http.NotFound(w, r)
					return
				}
				_, _ = fmt.Fprint(w, `{"code":4}`)
			}))
			defer srv.Close()

			args := []string{"-city", "moscow", "-security", "readonly", "-login", "player", "-password", "wrong"}
			if tc.flags {
				args = append(args, "-admin-login", "org", "-admin-password", "secret")
			}
			cfg, _ := parseFor(t, args)
			var stderr strings.Builder
			cfg.stderr = &stderr

			// What an organizer login in the browser leaves on disk: the
			// organizer and no playing token.
			stored := dzzzr.New("moscow")
			stored.SetAdminCredentials("org", "secret")
			if _, err := saveSession(cfg, stored); err != nil {
				t.Fatal(err)
			}

			c := dzzzr.New("moscow", dzzzr.WithBaseURL(srv.URL+"/moscow/"))
			err := agentAuthorize(t.Context(), cfg, c)
			if (err != nil) != tc.wantError {
				t.Fatalf("agentAuthorize = %v, ожидалась ошибка: %v", err, tc.wantError)
			}
			if tc.wantError {
				if stderr.Len() != 0 {
					t.Errorf("неверный пароль игрока понижен до предупреждения: %q", stderr.String())
				}
				return
			}
			if !strings.Contains(stderr.String(), "Игрок не авторизован") {
				t.Errorf("stderr = %q, в нём нет объяснения", stderr.String())
			}
		})
	}
}
