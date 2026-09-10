package dzzrmobile_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/dzzzr-cli/mobile/dzzrmobile"
)

// newTestClient points a mobile client at a fake engine.
func newTestClient(t *testing.T, h http.Handler) *dzzrmobile.DzzrClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := dzzrmobile.NewClientWithOptions("moscow", srv.URL+"/moscow/", false, false, 5)
	c.SetRequestMinIntervalMillis(0)
	c.SetCredentials("demo", "1234")
	return c
}

func engineHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/moscow/API/login.php":
			if r.URL.Query().Get("password") == "fail" {
				_, _ = w.Write([]byte(`{"error":"Неверный пользователь или пароль","code":"4"}`))
				return
			}
			_, _ = w.Write([]byte(`{"userName":"demo","error":"ok","userToken":"TOKEN","code":"2"}`))
		case r.URL.Path == "/moscow/go/" && r.Method == http.MethodPost:
			http.Redirect(w, r, "/moscow/go/?err=9&api=true", http.StatusFound)
		case r.URL.Path == "/moscow/go/":
			_, _ = w.Write([]byte(`{"gameName":"Тест","gameId":"4242","teamName":"MockTeam","currentTime":"2026-09-08 22:00:00","finished":false,"canControl":"1","totallevels":"3","level":{"levelNumber":"2","codesFounded":"1","totalCodes":"3","question":"<b>Вопрос</b>","tm":"120","spoilers":[],"bonusCodesTimes":[]},"bonusLevels":[ { }],"messages":[],"errNo":0,"errText":""}`))
		case r.URL.Path == "/moscow/API/game.php":
			_, _ = w.Write([]byte(`{"lnames":["1"],"tds":["<td>00:10:00</td>"],"teamstatTime":"22:00","log":[],"number":"2","timer":120}`))
		case r.URL.Path == "/moscow/API/messages.php":
			_, _ = w.Write([]byte(`{ "messages" : [{"ava":"0","who":"организатор","time":"22:00","content":"Привет"}]}`))
		case r.URL.Path == "/moscow/API/postmessage.php":
			_, _ = w.Write([]byte(`{"message":"Ok"}`))
		case r.URL.Path == "/moscow/API/gamesList.php":
			_, _ = w.Write([]byte(`{ "games" : [{"id":4242,"name":"Тест","number":"112","date":"2026-09-08 21:00:00","isLinear":true,"teams":null}]}`))
		default:
			http.NotFound(w, r)
		}
	})
}

func TestLoginAndSession(t *testing.T) {
	c := newTestClient(t, engineHandler(t))
	raw, err := c.Login("demo", "secret")
	if err != nil {
		t.Fatal(err)
	}
	var login struct {
		Code      int    `json:"code"`
		UserToken string `json:"userToken"`
	}
	if err := json.Unmarshal([]byte(raw), &login); err != nil {
		t.Fatal(err)
	}
	if login.Code != 2 || c.Session() != "TOKEN" {
		t.Errorf("login = %+v session = %q", login, c.Session())
	}
	if dzzrmobile.LoginCodeText(2) != "Успешная авторизация" {
		t.Error("LoginCodeText")
	}
	if c.Captain() != "demo" || c.City() != "moscow" || !strings.HasSuffix(c.BaseURL(), "/moscow/") {
		t.Errorf("client identity: %q %q %q", c.Captain(), c.City(), c.BaseURL())
	}

	data, err := c.ExportSession()
	if err != nil {
		t.Fatal(err)
	}
	restored := dzzrmobile.NewClientWithOptions("moscow", c.BaseURL(), false, false, 5)
	if err := restored.ImportSession(data); err != nil {
		t.Fatal(err)
	}
	if restored.Session() != "TOKEN" || restored.Captain() != "demo" {
		t.Errorf("restored session = %q captain = %q", restored.Session(), restored.Captain())
	}
	c.Logout()
	if c.Session() != "" {
		t.Error("Logout must clear the token")
	}
}

func TestLoginFailureCode(t *testing.T) {
	c := newTestClient(t, engineHandler(t))
	raw, err := c.Login("demo", "fail")
	if err != nil {
		t.Fatal(err)
	}
	var login struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal([]byte(raw), &login); err != nil {
		t.Fatal(err)
	}
	if login.Code != 4 || c.Session() != "" {
		t.Errorf("code = %d session = %q", login.Code, c.Session())
	}
	if !strings.Contains(dzzrmobile.LoginCodeText(4), "Неверный") {
		t.Error("LoginCodeText(4)")
	}
}

func TestGetGameJSON(t *testing.T) {
	c := newTestClient(t, engineHandler(t))
	if _, err := c.Login("demo", "secret"); err != nil {
		t.Fatal(err)
	}
	raw, err := c.GetGame()
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		GameName string `json:"gameName"`
		GameID   int    `json:"gameId"`
		Level    struct {
			LevelNumber int    `json:"levelNumber"`
			Question    string `json:"question"`
			TM          int    `json:"tm"`
		} `json:"level"`
	}
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		t.Fatalf("GetGame is not decodable JSON: %v\n%s", err, raw)
	}
	if st.GameName != "Тест" || st.GameID != 4242 || st.Level.LevelNumber != 2 || st.Level.TM != 120 {
		t.Errorf("state = %+v", st)
	}
	if got := dzzrmobile.StripHTML(st.Level.Question); got != "Вопрос" {
		t.Errorf("StripHTML = %q", got)
	}
}

func TestActionsReturnResultJSON(t *testing.T) {
	c := newTestClient(t, engineHandler(t))
	if _, err := c.Login("demo", "secret"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		call func() (string, error)
	}{
		{"send code", func() (string, error) { return c.SendCode("КОД") }},
		{"send bonus", func() (string, error) { return c.SendBonusCode(7, "B1") }},
		{"spoiler", func() (string, error) { return c.SendSpoilerCode(2, "SP") }},
		{"hint", func() (string, error) { return c.TakeHint(2, 1) }},
		{"hint early", func() (string, error) { return c.TakeHintEarly(3, 1) }},
		{"abandon", func() (string, error) { return c.Abandon() }},
		{"break", func() (string, error) { return c.TakeBreak() }},
		{"break stop", func() (string, error) { return c.StopBreak() }},
		{"next level", func() (string, error) { return c.NextLevel(2) }},
		{"select level", func() (string, error) { return c.SelectLevel(2, 3) }},
		{"message", func() (string, error) { return c.SendMessageToOrg("привет") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := tc.call()
			if err != nil {
				t.Fatal(err)
			}
			var res struct {
				Err  int    `json:"err"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(raw), &res); err != nil {
				t.Fatalf("result is not JSON: %v\n%s", err, raw)
			}
			if res.Err != 9 || res.Text == "" {
				t.Errorf("result = %+v", res)
			}
			if !dzzrmobile.IsAcceptedCode(int64(res.Err)) {
				t.Error("code 9 must be accepted")
			}
			if dzzrmobile.ErrText(int64(res.Err)) != res.Text {
				t.Error("ErrText must match the result text")
			}
		})
	}
}

func TestReadsReturnJSON(t *testing.T) {
	c := newTestClient(t, engineHandler(t))
	if _, err := c.Login("demo", "secret"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		call func() (string, error)
	}{
		{"level info", c.GetLevelInfo},
		{"bonus level info", c.GetBonusLevelInfo},
		{"stat", c.GetStat},
		{"log", c.GetLog},
		{"messages", func() (string, error) { return c.GetMessages("") }},
		{"games", func() (string, error) { return c.GetGamesList(false, false) }},
		{"level media", c.GetLevelMedia},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := tc.call()
			if err != nil {
				t.Fatal(err)
			}
			var any1 any
			if err := json.Unmarshal([]byte(raw), &any1); err != nil {
				t.Fatalf("not JSON: %v\n%s", err, raw)
			}
		})
	}
	if err := c.PostMessage("привет"); err != nil {
		t.Fatal(err)
	}
}

func TestAuthErrorsAreClassified(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error" : "Ошибка авторизации. Неверный идентификатор сессии"}`))
	}))
	c.SetSession("STALE")
	_, err := c.GetGame()
	if !dzzrmobile.IsAuthError(err) || dzzrmobile.AuthErrorKind(err) != "session" {
		t.Fatalf("err = %v kind = %q", err, dzzrmobile.AuthErrorKind(err))
	}
	if dzzrmobile.AuthErrorKind(nil) != "" {
		t.Error("nil error has no kind")
	}
}

func TestUndecodableAcceptedIsFlagged(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>Технические работы</html>"))
	}))
	c.SetSession("TOKEN")
	_, err := c.SendCode("КОД")
	if !dzzrmobile.IsUndecodableAccepted(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestRequestPacing(t *testing.T) {
	c := newTestClient(t, engineHandler(t))
	c.SetSession("TOKEN")
	c.SetRequestMinIntervalMillis(60)
	start := time.Now()
	for range 3 {
		if _, err := c.GetGame(); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 120*time.Millisecond {
		t.Errorf("three paced requests took %s, want at least 120ms", elapsed)
	}
	c.SetRequestMinIntervalMillis(0)
	start = time.Now()
	for range 3 {
		if _, err := c.GetGame(); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("pacing disabled but three requests took %s", elapsed)
	}
}

func TestCodeSendTimeout(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		http.Redirect(w, r, "/moscow/go/?err=9", http.StatusFound)
	}))
	c.SetSession("TOKEN")
	c.SetCodeSendTimeoutSeconds(0) // default
	c.SetRequestMinIntervalMillis(0)
	done := make(chan error, 1)
	go func() {
		_, err := c.SendCode("КОД")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("default timeout must allow a 300ms reply: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendCode hung")
	}
}

func TestAdminWrappersNeedCredentials(t *testing.T) {
	c := newTestClient(t, engineHandler(t))
	if c.HasAdminCredentials() {
		t.Error("a fresh client has no organizer credentials")
	}
	if _, err := c.AdminListGames(); err == nil {
		t.Error("admin call without credentials must fail")
	}
	c.SetAdminCredentials("org", "secret")
	if !c.HasAdminCredentials() {
		t.Error("credentials were not stored")
	}
}
