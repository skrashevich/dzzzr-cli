package dzzzr_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func authedClient(t *testing.T, h http.Handler, opts ...dzzzr.Option) *dzzzr.Client {
	t.Helper()
	opts = append(opts, dzzzr.WithSession("TOKEN"), dzzzr.WithCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "1234"}))
	c, _ := newTestClient(t, h, opts...)
	return c
}

func TestGetGameDecodesFixture(t *testing.T) {
	var seen *http.Request
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(fixture(t, "go_state.json"))
	}))
	st, err := c.GetGame(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if seen.URL.Path != "/moscow/go/" || seen.URL.Query().Get("api") != "true" || seen.URL.Query().Get("s") != "TOKEN" || seen.URL.Query().Get("pin") != "1234" {
		t.Errorf("request = %s", seen.URL)
	}
	wantBasic(t, seen, "moscow_Cap", "1234")
	if st.GameName != "Ночной дозор: Осень" || st.GameID != 4242 || !st.CanControl || st.TotalLevels != 12 || st.Finished {
		t.Errorf("header fields: %+v", st)
	}
	if !st.GameStarted() {
		t.Error("GameStarted should be true")
	}
	if st.Level == nil {
		t.Fatal("Level is nil")
	}
	l := st.Level
	if l.LevelNumber != 3 || l.TM != 125 || l.Skvoz || l.CodesFounded != 1 || l.TotalCodes != 4 || l.NeededCodes != 3 || l.Hint1 == "" || l.Hint2 != "" {
		t.Errorf("level: %+v", l)
	}
	if len(l.Spoilers) != 1 || l.Spoilers[0].Penalty != 5 || l.Spoilers[0].Solved {
		t.Errorf("spoilers: %+v", l.Spoilers)
	}
	if len(l.BonusCodesTimes) != 1 || l.BonusCodesTimes[0] != "10" {
		t.Errorf("bonusCodesTimes: %+v", l.BonusCodesTimes)
	}
	if len(st.BonusLevels) != 1 || !st.BonusLevels[0].Skvoz || st.BonusLevels[0].BonusLevelTime != 15 || !st.BonusLevels[0].NoMasterCode {
		t.Errorf("bonus levels: %+v", st.BonusLevels)
	}
	if len(st.Messages) != 2 || st.Messages[1].Destination != "команде" {
		t.Errorf("messages: %+v", st.Messages)
	}
	if st.ErrNo != 9 || !strings.Contains(st.ErrText, "Код принят") {
		t.Errorf("errNo/errText: %d %q", st.ErrNo, st.ErrText)
	}
	if got := dzzzr.StripHTML(l.Question); got != "Найдите двор с тремя гаражами.\nКоды на стенах." {
		t.Errorf("StripHTML(question) = %q", got)
	}
}

func TestGetGameNotStarted(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture(t, "go_state_not_started.json"))
	}))
	st, err := c.GetGame(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.GameStarted() || st.Level != nil || len(st.BonusLevels) != 0 || len(st.Messages) != 0 {
		t.Errorf("state: %+v", st)
	}
	if st.GameStartOnDay != "9 сентября 2026 г." || st.GameStartOnTime != "22:00" {
		t.Errorf("start fields: %+v", st)
	}
}

func TestGetGameRequiresSession(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request must not be sent without a session")
	}), dzzzr.WithCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "1"}))
	_, err := c.GetGame(context.Background())
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthSession {
		t.Fatalf("err = %v", err)
	}
}

func TestGetGameBadSessionEnvelope(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error" : "Ошибка авторизации. Неверный идентификатор сессии"}`))
	}))
	_, err := c.GetGame(context.Background())
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthSession {
		t.Fatalf("err = %v", err)
	}
}

func TestGetGameHTMLIsUndecodable(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>Введите PIN</body></html>"))
	}))
	_, err := c.GetGame(context.Background())
	if !dzzzr.IsUndecodableAccepted(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestSendCodePostsFormAndParsesRedirect(t *testing.T) {
	var seen *http.Request
	var form url.Values
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		form = r.PostForm
		http.Redirect(w, r, "/moscow/go/?nostat=&notext=&notags=&log=&legend=&err=9&bonus=&kladMap=&api=true", http.StatusFound)
	}))
	res, err := c.SendCode(context.Background(), "КОД 1")
	if err != nil {
		t.Fatal(err)
	}
	if seen.Method != http.MethodPost || seen.URL.Path != "/moscow/go/" || seen.URL.Query().Get("api") != "true" || seen.URL.Query().Get("s") != "TOKEN" {
		t.Errorf("request = %s %s", seen.Method, seen.URL)
	}
	if ct := seen.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", ct)
	}
	if ref := seen.Header.Get("Referer"); !strings.HasSuffix(ref, "/moscow/go/") {
		t.Errorf("Referer = %q", ref)
	}
	if form.Get("action") != "entcod" || form.Get("cod") != "КОД 1" || form.Has("skvoz") {
		t.Errorf("form = %v", form)
	}
	if res.Err != 9 || !res.Accepted() || !strings.Contains(res.Text, "Код принят") {
		t.Errorf("result = %+v", res)
	}
}

func TestActionRedirectVariants(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		location string
		want     int
	}{
		{"empty err", http.StatusFound, "/moscow/go/?nostat=&err=&bonus=", 0},
		{"no query", http.StatusFound, "/moscow/go/", 0},
		{"absolute", http.StatusMovedPermanently, "https://classic.dzzzr.ru/moscow/go/?err=57", 57},
		{"303", http.StatusSeeOther, "/moscow/go/?err=11", 11},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", tc.location)
				w.WriteHeader(tc.status)
			}))
			res, err := c.SendCode(context.Background(), "x")
			if err != nil {
				t.Fatal(err)
			}
			if res.Err != tc.want {
				t.Errorf("Err = %d, want %d", res.Err, tc.want)
			}
		})
	}
}

func TestActionJSONBody(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"gameName":"x","errNo": 7, "errText": "повтор"}`))
	}))
	res, err := c.SendCode(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != 7 || res.Accepted() {
		t.Errorf("result = %+v", res)
	}
}

func TestActionHTMLBodyIsUndecodable(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>..."))
	}))
	_, err := c.SendCode(context.Background(), "x")
	if !dzzzr.IsUndecodableAccepted(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestAction401(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	_, err := c.SendCode(context.Background(), "x")
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthBasic {
		t.Fatalf("err = %v", err)
	}
}

func TestAction500(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	_, err := c.SendCode(context.Background(), "x")
	var he *dzzzr.HTTPError
	if !asErr(err, &he) || he.StatusCode != 502 {
		t.Fatalf("err = %v", err)
	}
}

func TestActionForms(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		call func(c *dzzzr.Client) (*dzzzr.ActionResult, error)
		want url.Values
	}{
		{"bonus", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) { return c.SendBonusCode(ctx, 7, "B1") },
			url.Values{"action": {"entcod"}, "cod": {"B1"}, "skvoz": {"1"}, "level": {"7"}}},
		{"abandon", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) { return c.Abandon(ctx) },
			url.Values{"action": {"abandon"}}},
		{"break", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) { return c.TakeBreak(ctx) },
			url.Values{"action": {"break"}}},
		{"breakStop", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) { return c.StopBreak(ctx) },
			url.Values{"action": {"breakStop"}, "bonus": {""}}},
		{"hint1", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) { return c.TakeHint(ctx, 3, 1) },
			url.Values{"action": {"takeClue"}, "event": {"4"}, "level": {"3"}}},
		{"hint2", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) { return c.TakeHint(ctx, 3, 2) },
			url.Values{"action": {"takeClue"}, "event": {"5"}, "level": {"3"}}},
		{"early", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) { return c.TakeHintEarly(ctx, 3, 1) },
			url.Values{"action": {"beforeClue"}, "clue": {"1"}, "level": {"3"}}},
		{"next", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) { return c.NextLevel(ctx, 3) },
			url.Values{"action": {"nextLevel"}, "level": {"3"}}},
		{"select", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) { return c.SelectLevel(ctx, 3, 5) },
			url.Values{"action": {"selectlevel"}, "llevel": {"3"}, "level": {"5"}}},
		{"spoiler", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) { return c.SendSpoilerCode(ctx, 3, "SP") },
			url.Values{"action": {"spoilerCode"}, "skvoz": {"3"}, "spoilerCode": {"SP"}}},
		{"message", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) { return c.SendMessageToOrg(ctx, "привет") },
			url.Values{"action": {"send_message"}, "content": {"привет"}}},
		{"options", func(c *dzzzr.Client) (*dzzzr.ActionResult, error) {
			return c.SaveOptions(ctx, map[string]string{"nostat": "1"})
		},
			url.Values{"action": {"saveOptions"}, "option[nostat]": {"1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var form url.Values
			c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				form = r.PostForm
				http.Redirect(w, r, "/moscow/go/?err=33", http.StatusFound)
			}))
			if _, err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			if form.Encode() != tc.want.Encode() {
				t.Errorf("form = %v, want %v", form, tc.want)
			}
		})
	}
}

func TestStripHTML(t *testing.T) {
	cases := map[string]string{
		"<b>Код принят.</b> Дальше": "Код принят. Дальше",
		"a<br>b<br/>c":                          "a\nb\nc",
		"&lt;x&gt; &amp; &nbsp;y":               "<x> & y",
		"<script>alert(1)</script>text":         "text",
		"  <p>раз</p>\n\n\n\n<p>два</p>  ":      "раз\n\nдва",
		"<td nowrap>22:01:10</td>":              "22:01:10",
		"<span style='color:red'>1</span>, 1; ": "1, 1;",
	}
	for in, want := range cases {
		if got := dzzzr.StripHTML(in); got != want {
			t.Errorf("StripHTML(%q) = %q, want %q", in, got, want)
		}
	}
}

// The live demo town prints a leftover debug dump ahead of the JSON, wrapped
// in an HTML comment. The payload behind it is ordinary game state and must
// still decode; a page that only looks like one must still be rejected.
func TestGetGameSkipsDebugCommentBeforeJSON(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<!--string(11) \"dzzzr_ndemo\"\n-->"))
		_, _ = w.Write(fixture(t, "go_state.json"))
	}))
	st, err := c.GetGame(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.GameID != 4242 || st.Level == nil || st.Level.LevelNumber != 3 {
		t.Errorf("state: %+v", st)
	}
}

func TestGetGameStillRejectsHTMLPages(t *testing.T) {
	for name, page := range map[string]string{
		"plain":            "<html><body>Ошибка</body></html>",
		"comment then tag": "<!-- generated -->\n<html><body>Ошибка</body></html>",
		"doctype":          "<!DOCTYPE html><html></html>",
	} {
		t.Run(name, func(t *testing.T) {
			c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(page))
			}))
			if _, err := c.GetGame(context.Background()); !dzzzr.IsUndecodable(err) {
				t.Errorf("err = %v, want undecodable", err)
			}
		})
	}
}

// The game page answers with a numeric "error" and an "errorText" when it has
// no game to show. Without recognising that shape the reply decodes as an
// empty game and the caller reports a success.
func TestGetGameReportsAccessStates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		state int
	}{
		{"no game", `{"error": 2, "errorText": "В настоящее время вы не заявлены ни в одной из ближайших игр."}`, dzzzr.GameStateNoGame},
		{"reserve", `{"error": 3, "errorText": "Вы должны быть переведены из резерва в активные игроки."}`, dzzzr.GameStateReserve},
		{"wrong team", `{"error": 4, "errorText": "Вы вошли в интерфейс чужой команды."}`, dzzzr.GameStateWrongTeam},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			st, err := c.GetGame(context.Background())
			if st != nil {
				t.Errorf("state = %+v, want none", st)
			}
			var ae *dzzzr.GameAccessError
			if !asErr(err, &ae) {
				t.Fatalf("err = %v, want GameAccessError", err)
			}
			if ae.State != tc.state || ae.Text == "" {
				t.Errorf("access error = %+v", ae)
			}
			if !dzzzr.IsGameAccessError(err) {
				t.Error("IsGameAccessError")
			}
		})
	}
}

// go_lite_noauth.tpl uses the same shape for an unauthenticated player, which
// is an authentication failure rather than a missing game.
func TestGetGameReportsUnauthenticatedState(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error": 1, "errorText": "Вы не авторизованы."}`))
	}))
	_, err := c.GetGame(context.Background())
	if !dzzzr.IsAuthError(err) {
		t.Fatalf("err = %v, want AuthError", err)
	}
}

// A string "error" still means what it did: the engine sends that shape for a
// bad session and for an unreachable town database.
func TestGetGameStillReportsStringErrorEnvelope(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error" : "Can't connect to DB"}`))
	}))
	if _, err := c.GetGame(context.Background()); !dzzzr.IsEngineError(err) {
		t.Fatalf("err = %v, want EngineError", err)
	}
}

// API/game.php prints {"err":13} and exits while the organizer has the engine
// switched off. It carries no "error" key, so a read that only looks for one
// decodes it into an empty payload and reports a success.
func TestReadEndpointsReportAStoppedEngine(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"err":13}`))
	}))
	_, err := c.GetStat(context.Background())
	var ee *dzzzr.EngineError
	if !asErr(err, &ee) || ee.Code != dzzzr.ErrEngineStopped {
		t.Fatalf("err = %v, want engine error 13", err)
	}
}

// A payload that happens to carry err beside real data is not an envelope.
func TestReadEndpointsKeepPayloadsThatCarryErr(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"err":0,"teamstatTime":"01:02:03","tds":[]}`))
	}))
	st, err := c.GetStat(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if st.Time != "01:02:03" {
		t.Errorf("stat = %+v", st)
	}
}

// The engine sends its own wording for an action beside the code; the local
// table cannot produce the parts it interpolates.
func TestActionPrefersTheEnginesOwnText(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errNo":13,"errText":"Движок остановлен организатором. Свяжитесь с ним по телефону +7 000"}`))
	}))
	res, err := c.SendCode(context.Background(), "1DR11")
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != 13 || !strings.Contains(res.Text, "+7 000") {
		t.Errorf("result = %+v", res)
	}
}

// The penalty belongs to code 42 alone; errors.php interpolates bcs nowhere
// else.
func TestPenaltyIsOnlyAppendedToTheEarlyHintText(t *testing.T) {
	for _, tc := range []struct {
		code, penalty int
		wantMinutes   bool
	}{
		{dzzzr.ErrEarlyHintPenalty, 21, true},
		{dzzzr.ErrCodeAccepted, 21, false},
	} {
		c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", sprintf("/moscow/go/?err=%d&bcs=%d", tc.code, tc.penalty))
			w.WriteHeader(http.StatusFound)
		}))
		res, err := c.SendCode(context.Background(), "X")
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(res.Text, "21 мин."); got != tc.wantMinutes {
			t.Errorf("code %d: text = %q", tc.code, res.Text)
		}
		if res.Penalty != tc.penalty {
			t.Errorf("code %d: penalty = %d", tc.code, res.Penalty)
		}
	}
}

// go.tpl puts errText on every state reply, not only on an answer to an
// action. Standing text beside errNo 0 must not turn a control action the
// engine performed into one it refused.
func TestZeroCodeIgnoresStandingEngineText(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errNo":0,"errText":"Приветствуем участников команды!"}`))
	}))
	res, err := c.TakeBreak(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != 0 {
		t.Fatalf("err = %d", res.Err)
	}
	if res.Text != "" {
		t.Errorf("text = %q, want none so the caller can tell silence apart", res.Text)
	}
}
