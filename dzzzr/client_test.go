package dzzzr_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// newTestClient points a client at an httptest server under the "moscow" city.
func newTestClient(t *testing.T, h http.Handler, opts ...dzzzr.Option) *dzzzr.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	opts = append([]dzzzr.Option{dzzzr.WithBaseURL(srv.URL + "/moscow/"), dzzzr.WithAdminDelay(0)}, opts...)
	return dzzzr.New("moscow", opts...)
}

func wantBasic(t *testing.T, r *http.Request, user, pass string) {
	t.Helper()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	if got := r.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestNewDefaultBaseURL(t *testing.T) {
	if got := dzzzr.New("moscow").BaseURL(); got != "https://classic.dzzzr.ru/moscow/" {
		t.Errorf("BaseURL = %q", got)
	}
	if got := dzzzr.New("/spb/").BaseURL(); got != "https://classic.dzzzr.ru/spb/" {
		t.Errorf("trimmed BaseURL = %q", got)
	}
	if got := dzzzr.New("moscow", dzzzr.WithHTTP()).BaseURL(); got != "http://classic.dzzzr.ru/moscow/" {
		t.Errorf("WithHTTP BaseURL = %q", got)
	}
	if got := dzzzr.New("moscow", dzzzr.WithBaseURL("http://127.0.0.1:1/moscow")).BaseURL(); got != "http://127.0.0.1:1/moscow/" {
		t.Errorf("WithBaseURL BaseURL = %q", got)
	}
	if got := dzzzr.New("moscow", dzzzr.WithBaseURL("not a url")).BaseURL(); got != "https://classic.dzzzr.ru/moscow/" {
		t.Errorf("bad WithBaseURL must be ignored, got %q", got)
	}
	if dzzzr.New("moscow").City() != "moscow" {
		t.Error("City")
	}
}

func TestLoginSendsBasicAndQuery(t *testing.T) {
	var seen *http.Request
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		if r.URL.Path != "/moscow/API/login.php" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(`{"userName": "Вася", "error" : "Вы успешно авторизовались", "userToken" : "ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEF", "code" : "2"}`))
	}), dzzzr.WithCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "1234"}))

	resp, err := c.Login(context.Background(), "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if seen.Method != http.MethodGet {
		t.Errorf("method = %s", seen.Method)
	}
	q := seen.URL.Query()
	if q.Get("login") != "u" || q.Get("password") != "p" {
		t.Errorf("query = %v", q)
	}
	wantBasic(t, seen, "moscow_Cap", "1234")
	if !resp.OK() || resp.Code != 2 || resp.UserName != "Вася" {
		t.Errorf("resp = %+v", resp)
	}
	if c.Session() != "ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEF" {
		t.Errorf("Session = %q", c.Session())
	}
	if c.LoginName() != "u" {
		t.Errorf("LoginName = %q", c.LoginName())
	}
}

func TestLoginErrorCode(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error" : "Неверный пользователь или пароль. Проверьте личные данные.", "code" : "4"}`))
	}), dzzzr.WithCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "1"}))
	resp, err := c.Login(context.Background(), "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK() || resp.Code != 4 || c.Session() != "" {
		t.Errorf("resp = %+v session=%q", resp, c.Session())
	}
}

func TestLoginUsesTheSiteLoginAsIdentity(t *testing.T) {
	// The API wall only checks that a Basic header is present, and Classic
	// teams have no PIN, so a player signs in with the site login alone.
	var seen *http.Request
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="DozoR API"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error" : "Ошибка авторизации. Не введено имя капитана"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":"2","userToken":"T","userName":"svk"}`))
	}))
	resp, err := c.Login(context.Background(), "svk", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK() {
		t.Fatalf("resp = %+v", resp)
	}
	wantBasic(t, seen, "moscow_svk", "")

	// An issued captain and PIN take precedence over that fallback.
	c.SetCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "1234"})
	if _, err := c.Login(context.Background(), "svk", "secret"); err != nil {
		t.Fatal(err)
	}
	wantBasic(t, seen, "moscow_Cap", "1234")
}

func TestRequestWithoutAnyIdentity(t *testing.T) {
	// With neither credentials nor a remembered login there is nothing to put
	// in the header, and the call fails before it reaches the network.
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should be sent without an identity")
	}), dzzzr.WithSession("TOKEN"))
	_, err := c.GetStat(context.Background())
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthBasic {
		t.Fatalf("err = %v", err)
	}
}

func TestRememberedLoginIsUsedAsIdentity(t *testing.T) {
	var seen *http.Request
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		switch r.URL.Path {
		case "/moscow/API/login.php":
			_, _ = w.Write([]byte(`{"code":"2","userToken":"TOKEN"}`))
		default:
			_, _ = w.Write([]byte(`{"lnames":[],"tds":[],"teamstatTime":""}`))
		}
	}))
	if _, err := c.Login(context.Background(), "svk", "secret"); err != nil {
		t.Fatal(err)
	}
	// A later call has no login argument, so it uses the remembered one.
	if _, err := c.GetStat(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantBasic(t, seen, "moscow_svk", "")
}

func TestLoginAuthEnvelopeWithoutCode(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error" : "Ошибка авторизации. Неверный идентификатор сессии"}`))
	}), dzzzr.WithCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "1"}))
	_, err := c.Login(context.Background(), "u", "p")
	if !dzzzr.IsAuthError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginHTMLIsUndecodable(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>maintenance</body></html>`))
	}), dzzzr.WithCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "1"}))
	_, err := c.Login(context.Background(), "u", "p")
	if !dzzzr.IsUndecodableAccepted(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestExportImportSessionRoundTrip(t *testing.T) {
	c := dzzzr.New("moscow", dzzzr.WithSession("TOK"), dzzzr.WithCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "9"}), dzzzr.WithAdminCredentials("org", "secret"))
	data, err := c.ExportSession()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["token"] != "TOK" || m["captain"] != "Cap" || m["pin"] != "9" || m["admin_login"] != "org" || m["admin_password"] != "secret" {
		t.Errorf("exported = %s", data)
	}
	d := dzzzr.New("moscow")
	if err := d.ImportSession(data); err != nil {
		t.Fatal(err)
	}
	if d.Session() != "TOK" || d.Credentials() != (dzzzr.Credentials{Captain: "Cap", Pin: "9"}) || !d.HasAdminCredentials() || d.AdminLogin() != "org" {
		t.Errorf("imported client differs: %q %+v", d.Session(), d.Credentials())
	}
	if err := d.ImportSession([]byte("{garbage")); err == nil {
		t.Error("garbage must fail")
	}
	d.Logout()
	if d.Session() != "" || d.Credentials().Pin != "9" {
		t.Error("Logout must clear the token only")
	}
}

func TestNoRedirectFollow(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moscow/API/login.php" {
			http.Redirect(w, r, "/moscow/elsewhere?err=9", http.StatusFound)
			return
		}
		t.Errorf("redirect was followed to %s", r.URL)
	}), dzzzr.WithCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "1"}))
	_, err := c.Login(context.Background(), "u", "p")
	var he *dzzzr.HTTPError
	if !asErr(err, &he) || he.StatusCode != http.StatusFound {
		t.Fatalf("err = %v", err)
	}
}

func TestDebugLoggerRedactsSecrets(t *testing.T) {
	var lines []string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"2","userToken":"T"}`))
	}), dzzzr.WithCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "1234"}), dzzzr.WithDebugLogger(func(f string, a ...any) {
		lines = append(lines, sprintf(f, a...))
	}))
	if _, err := c.Login(context.Background(), "u", "secretpass"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "secretpass") || strings.Contains(joined, "1234") {
		t.Errorf("debug output leaks secrets:\n%s", joined)
	}
	if !strings.Contains(joined, "login.php") {
		t.Errorf("debug output missing request line:\n%s", joined)
	}
}

func TestHARRecording(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"2","userToken":"T"}`))
	}), dzzzr.WithCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "1234"}), dzzzr.WithHARRecording(true))
	if _, err := c.Login(context.Background(), "u", "secretpass"); err != nil {
		t.Fatal(err)
	}
	har, err := c.ExportHAR()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Log struct {
			Entries []struct {
				Request struct {
					URL     string
					Headers []struct{ Name, Value string }
				}
				Response struct{ Status int }
			}
		}
	}
	if err := json.Unmarshal(har, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Log.Entries) != 1 || doc.Log.Entries[0].Response.Status != 200 {
		t.Fatalf("entries = %+v", doc.Log.Entries)
	}
	s := string(har)
	if strings.Contains(s, "secretpass") || strings.Contains(s, "1234") {
		t.Error("HAR leaks secrets")
	}
	empty, _ := dzzzr.New("moscow").ExportHAR()
	if !strings.Contains(string(empty), `"entries": []`) {
		t.Errorf("empty HAR = %s", empty)
	}
}

func TestTimeoutOption(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}), dzzzr.WithTimeout(50*time.Millisecond), dzzzr.WithCredentials(dzzzr.Credentials{Captain: "C", Pin: "1"}))
	if _, err := c.Login(context.Background(), "u", "p"); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestHARRedactsTheSessionToken(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"userName":"svk","error":"ok","userToken":"SUPERSECRETTOKEN12345678901234","code":"2"}`))
	}), dzzzr.WithHARRecording(true))
	if _, err := c.Login(context.Background(), "svk", "secret"); err != nil {
		t.Fatal(err)
	}
	har, err := c.ExportHAR()
	if err != nil {
		t.Fatal(err)
	}
	s := string(har)
	// The token in the reply body is what lets somebody play as the team.
	if strings.Contains(s, "SUPERSECRETTOKEN12345678901234") {
		t.Error("the HAR keeps the session token from the sign-in reply")
	}
	if strings.Contains(s, "secret") || strings.Contains(s, "login=svk") {
		t.Error("the HAR keeps the site account credentials from the request")
	}
	// The capture is still useful for protocol work: the exchange and the
	// shape of the reply are there, only the value is gone.
	if !strings.Contains(s, "login.php") || !strings.Contains(s, "userToken") {
		t.Errorf("the HAR should still describe the exchange:\n%s", s)
	}
}
