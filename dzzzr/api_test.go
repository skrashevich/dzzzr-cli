package dzzzr_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// apiServer serves fixtures for the API/*.php endpoints and records the last
// request.
func apiServer(t *testing.T) (*dzzzr.Client, *struct {
	req  *http.Request
	form url.Values
}) {
	t.Helper()
	seen := &struct {
		req  *http.Request
		form url.Values
	}{}
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.req = r
		_ = r.ParseForm()
		seen.form = r.PostForm
		q := r.URL.Query()
		switch r.URL.Path {
		case "/moscow/API/game.php":
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error" : "Ошибка авторизации. Не введено имя капитана"}`))
				return
			}
			if q.Get("s") != "TOKEN" {
				_, _ = w.Write([]byte(`{"error" : "Ошибка авторизации. Неверный идентификатор сессии"}`))
				return
			}
			switch {
			case q.Get("stat") == "true":
				_, _ = w.Write(fixture(t, "game_stat.json"))
			case q.Get("log") == "true":
				_, _ = w.Write(fixture(t, "game_log.json"))
			case q.Get("level") == "1", q.Get("bonuslevel") == "1":
				_, _ = w.Write(fixture(t, "game_level.json"))
			default:
				_, _ = w.Write([]byte(`{"skvoz":[]}`))
			}
		case "/moscow/API/messages.php":
			_, _ = w.Write(fixture(t, "messages.json"))
		case "/moscow/API/postmessage.php":
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			if strings.Contains(r.PostForm.Get("content"), "oops") {
				_, _ = w.Write([]byte(`{"message":"Oops"}`))
				return
			}
			_, _ = w.Write([]byte(`{"message":"Ok"}`))
		case "/moscow/API/gamesList.php":
			// gamesList.php sits behind the same wall as the rest of the API.
			if r.Header.Get("Authorization") == "" || q.Get("s") != "TOKEN" {
				_, _ = w.Write([]byte(`{"error" : "Ошибка авторизации. Неверный идентификатор сессии"}`))
				return
			}
			if q.Get("archive") == "1" {
				_, _ = w.Write([]byte(`{"games" : null}`))
				return
			}
			_, _ = w.Write(fixture(t, "games_list.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	return c, seen
}

func TestGetStat(t *testing.T) {
	c, seen := apiServer(t)
	st, err := c.GetStat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if seen.req.URL.Query().Get("stat") != "true" || seen.req.URL.Query().Get("s") != "TOKEN" {
		t.Errorf("query = %v", seen.req.URL.Query())
	}
	wantBasic(t, seen.req, "moscow_Cap", "1234")
	rows := st.Rows()
	if len(rows) != 4 || rows[0] != [2]string{"1", "00:12:40"} || rows[3] != [2]string{"Итого", "00:37:51"} || st.Time != "2026-09-08 22:10:00" {
		t.Errorf("rows = %v time = %q", rows, st.Time)
	}
}

func TestGetLog(t *testing.T) {
	c, seen := apiServer(t)
	log, err := c.GetLog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if seen.req.URL.Query().Get("log") != "true" {
		t.Errorf("query = %v", seen.req.URL.Query())
	}
	if len(log) != 3 {
		t.Fatalf("log = %+v", log)
	}
	e := log[1].Clean()
	if e.Time != "21:12:40" || e.Event != "принят код" || e.Data != "1" || e.User != "demo" {
		t.Errorf("cleaned = %+v", e)
	}
}

func TestGetLevelInfo(t *testing.T) {
	c, seen := apiServer(t)
	info, err := c.GetLevelInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if seen.req.URL.Query().Get("level") != "1" {
		t.Errorf("query = %v", seen.req.URL.Query())
	}
	if len(info.Dangers) != 4 || !info.Dangers[0].Founded || info.Dangers[0].Sector != "1" || info.Dangers[3].Danger != "3" {
		t.Errorf("dangers = %+v", info.Dangers)
	}
	if len(info.Sectors) != 2 || info.Sectors[0] != "Гаражи" {
		t.Errorf("sectors = %v", info.Sectors)
	}
	if len(info.DangersB) != 1 || len(info.Bonus) != 1 || info.Bonus[0] != "10" {
		t.Errorf("bonus = %+v %v", info.DangersB, info.Bonus)
	}
	if info.Timer != 125 || info.CanAbandon != 3 || info.CanBreak != 3 || info.BeforeClue2 != 10 || info.Number != 3 || info.CodeCount != 3 || !info.Spoiler {
		t.Errorf("scalars = %+v", info)
	}
	if info.Clue1 != "Подсказка 1" || info.Clue2 != "" {
		t.Errorf("clues = %q %q", info.Clue1, info.Clue2)
	}
	if len(info.Skvoz) != 1 || info.Skvoz[0].ID != 7 {
		t.Errorf("skvoz = %+v", info.Skvoz)
	}
	if _, err := c.GetBonusLevelInfo(context.Background()); err != nil || seen.req.URL.Query().Get("bonuslevel") != "1" {
		t.Errorf("bonus level: %v %v", err, seen.req.URL.Query())
	}
}

func TestGetLevelInfoBadSession(t *testing.T) {
	c, _ := apiServer(t)
	c.SetSession("WRONG")
	_, err := c.GetLevelInfo(context.Background())
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthSession {
		t.Fatalf("err = %v", err)
	}
}

func TestGetStatWithoutBasic(t *testing.T) {
	c, _ := apiServer(t)
	c.SetCredentials(dzzzr.Credentials{})
	_, err := c.GetStat(context.Background())
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthBasic {
		t.Fatalf("err = %v", err)
	}
}

func TestGetMessages(t *testing.T) {
	c, seen := apiServer(t)
	msgs, err := c.GetMessages(context.Background(), "2026-09-08 20:00:00")
	if err != nil {
		t.Fatal(err)
	}
	if seen.req.URL.Query().Get("after") != "2026-09-08 20:00:00" {
		t.Errorf("query = %v", seen.req.URL.Query())
	}
	if len(msgs) != 2 || msgs[0].Who != "организатор" || msgs[1].Ava != "1" || !strings.Contains(msgs[1].Content, "&quot;") {
		t.Errorf("messages = %+v", msgs)
	}
	if _, err := c.GetMessages(context.Background(), ""); err != nil || seen.req.URL.Query().Has("after") {
		t.Errorf("empty after must be omitted: %v %v", err, seen.req.URL)
	}
}

func TestPostMessage(t *testing.T) {
	c, seen := apiServer(t)
	if err := c.PostMessage(context.Background(), "привет штаб"); err != nil {
		t.Fatal(err)
	}
	if seen.req.Method != http.MethodPost || seen.form.Get("content") != "привет штаб" || seen.form.Get("login") != "Cap" {
		t.Errorf("form = %v", seen.form)
	}
	if seen.req.URL.Query().Get("s") != "TOKEN" {
		t.Errorf("query = %v", seen.req.URL.Query())
	}
	if err := c.PostMessage(context.Background(), "oops"); err == nil {
		t.Error("Oops reply must be an error")
	}
}

func TestGetGamesList(t *testing.T) {
	c, seen := apiServer(t)
	games, err := c.GetGamesList(context.Background(), dzzzr.GamesListOptions{NewOnly: true, After: "2026-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	q := seen.req.URL.Query()
	if q.Get("cityID") != "moscow" || q.Get("new") != "1" || q.Get("after") != "2026-01-01" || q.Has("archive") {
		t.Errorf("query = %v", q)
	}
	if len(games) != 2 {
		t.Fatalf("games = %+v", games)
	}
	g := games[0]
	if g.ID != 4242 || g.Name != "Ночной дозор: Осень" || g.Number != "112" || g.Image != nil || !g.IsLinear || len(g.Teams) != 2 {
		t.Errorf("game[0] = %+v", g)
	}
	if games[1].Image == nil || !strings.HasSuffix(*games[1].Image, "4243.jpg") || games[1].Teams != nil {
		t.Errorf("game[1] = %+v", games[1])
	}
	empty, err := c.GetGamesList(context.Background(), dzzzr.GamesListOptions{Archive: true})
	if err != nil || len(empty) != 0 {
		t.Errorf("archive: %v %v", empty, err)
	}
}

func TestGamesListWithoutCredentialsIsAuthError(t *testing.T) {
	c, seen := apiServer(t)
	c.SetSession("")
	_, err := c.GetGamesList(context.Background(), dzzzr.GamesListOptions{})
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthSession {
		t.Fatalf("err = %v", err)
	}
	c.SetSession("STALE")
	_, err = c.GetGamesList(context.Background(), dzzzr.GamesListOptions{})
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthSession {
		t.Fatalf("stale session err = %v", err)
	}
	_ = seen
}

// API/gamesList.php answers at most 50 games at a time and pages with limit
// and offset, so a city with a long archive was silently truncated.
func TestGamesListPagesThroughEverything(t *testing.T) {
	const total = 120
	var seen []url.Values
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		seen = append(seen, q)
		limit := 50
		if v := q.Get("limit"); v != "" {
			limit, _ = strconv.Atoi(v)
		}
		offset, _ := strconv.Atoi(q.Get("offset"))
		if offset >= total {
			_, _ = w.Write([]byte(`{"games" : null}`))
			return
		}
		var b strings.Builder
		b.WriteString(`{ "games" : [`)
		for i := offset; i < offset+limit && i < total; i++ {
			if i > offset {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"id" : %d,"name" : "Игра %d","teams" : null}`, 1000+i, i)
		}
		b.WriteString(`]}`)
		_, _ = w.Write([]byte(b.String()))
	}))

	games, err := c.GetGamesList(context.Background(), dzzzr.GamesListOptions{Archive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != total {
		t.Fatalf("got %d games, want %d", len(games), total)
	}
	if games[0].ID != 1000 || games[total-1].ID != 1000+total-1 {
		t.Errorf("first = %d last = %d", games[0].ID, games[total-1].ID)
	}
	if len(seen) != 3 {
		t.Errorf("pages fetched = %d, want 3", len(seen))
	}
	if seen[1].Get("offset") != "50" || seen[1].Get("archive") != "1" {
		t.Errorf("second page query = %v", seen[1])
	}

	// An explicit page is fetched once, exactly as asked.
	seen = nil
	if _, err := c.GetGamesList(context.Background(), dzzzr.GamesListOptions{Limit: 10, Offset: 20}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Get("limit") != "10" || seen[0].Get("offset") != "20" {
		t.Errorf("explicit page = %v", seen)
	}
}

// API/game.php answered 200 with nothing at all during the live playthrough.
// The report has to name that rather than talk about JSON syntax.
func TestGameAPIReportsAnEmptyBody(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	_, err := c.GetLevelInfo(context.Background())
	if !dzzzr.IsUndecodable(err) {
		t.Fatalf("err = %v, want undecodable", err)
	}
	if !strings.Contains(err.Error(), "empty body") {
		t.Errorf("err = %v, should say the body was empty", err)
	}
}
