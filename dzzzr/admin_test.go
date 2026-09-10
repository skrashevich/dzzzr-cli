package dzzzr_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// readCP1251Form reads a form body the way the engine's administration area
// does: the percent-decoded bytes are windows-1251 text, because the pages
// are served in that charset and PHP stores what it receives unchanged.
func readCP1251Form(r *http.Request) (url.Values, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	raw, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	out := url.Values{}
	for k, vs := range raw {
		key := dzzzr.DecodeWindows1251ForTest([]byte(k))
		for _, v := range vs {
			out.Add(key, dzzzr.DecodeWindows1251ForTest([]byte(v)))
		}
	}
	return out, nil
}

// adminFixtures maps "section" (the action query value) to the fixture file
// served for GET admin/?action=<section>... and "path:/x" for other paths.
type adminServer struct {
	t        *testing.T
	fixtures map[string]string
	posts    []url.Values
	gets     []url.Values
	paths    []string
	redirect func(form url.Values) string
	client   *dzzzr.Client
}

func newAdminServer(t *testing.T, fixtures map[string]string) *adminServer {
	t.Helper()
	s := &adminServer{t: t, fixtures: fixtures}
	s.redirect = func(form url.Values) string { return "/moscow/admin/?action=" + form.Get("action") + "&edit=1&id=77" }
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("org:secret")) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Admin area"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("<html><body>Unauthorized</body></html>"))
			return
		}
		s.paths = append(s.paths, r.URL.Path)
		if r.Method == http.MethodPost {
			form, err := readCP1251Form(r)
			if err != nil {
				t.Fatalf("reading the posted form: %v", err)
			}
			s.posts = append(s.posts, form)
			http.Redirect(w, r, s.redirect(form), http.StatusFound)
			return
		}
		s.gets = append(s.gets, r.URL.Query())
		key := r.URL.Query().Get("action")
		if r.URL.Path != "/moscow/admin/" {
			key = "path:" + strings.TrimPrefix(r.URL.Path, "/moscow/admin/")
		}
		name, ok := s.fixtures[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=windows-1251")
		_, _ = w.Write(fixture(t, name))
	})
	s.client = newTestClient(t, h, dzzzr.WithAdminCredentials("org", "secret"))
	return s
}

func (s *adminServer) lastPost() url.Values {
	if len(s.posts) == 0 {
		s.t.Fatal("no POST recorded")
	}
	return s.posts[len(s.posts)-1]
}

func TestAdminListGames(t *testing.T) {
	s := newAdminServer(t, map[string]string{"games": "admin_games_list.html"})
	games, err := s.client.AdminListGames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	q := s.gets[0]
	if q.Get("action") != "games" || q.Get("edit") != "1" {
		t.Errorf("query = %v", q)
	}
	if len(games) != 3 {
		t.Fatalf("games = %+v", games)
	}
	if g := games[0]; g.ID != 4243 || g.Name != "Будущая игра" || g.Date != "15.09.2026 21:00" || g.Number != "113" || g.Status != "прием заявок" || !g.Selected || g.Finished || !g.Published {
		t.Errorf("games[0] = %+v", g)
	}
	// The number column is empty when the game has no number, and the status
	// must not slide into its place.
	if g := games[1]; g.ID != 4242 || g.Name != "Ночной дозор: Осень" || g.Number != "" || g.Status != "анонс" || g.Season != "сезон 12 2026" {
		t.Errorf("games[1] = %+v", g)
	}
	// The engine writes bgcolor twice on a row that is both selected and
	// unpublished; an HTML parser keeps the first, so the state hides in the
	// second one.
	if g := games[2]; g.ID != 4100 || !g.Finished || g.Published || g.Name != "Летняя игра" {
		t.Errorf("games[2] = %+v", g)
	}
}

func TestAdminUnauthorized(t *testing.T) {
	s := newAdminServer(t, map[string]string{"games": "admin_games_list.html"})
	s.client.SetAdminCredentials("org", "wrong")
	_, err := s.client.AdminListGames(context.Background())
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthAdmin {
		t.Fatalf("err = %v", err)
	}
	s.client.SetAdminCredentials("", "")
	_, err = s.client.AdminListGames(context.Background())
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthAdmin {
		t.Fatalf("no creds err = %v", err)
	}
}

// TestAdminSectionAccessDenied covers the engine's real response, measured
// against classic.dzzzr.ru on 2026-09-09: valid organizer credentials, HTTP
// 200, but a page body saying "Вы не имеете доступа к этому разделу"
// because the account is not this game's assigned organizer. That must not
// be reported the same way as a rejected login/password (AuthAdmin), or the
// user is sent to re-check credentials that are already correct.
func TestAdminSectionAccessDenied(t *testing.T) {
	s := newAdminServer(t, map[string]string{"teams": "admin_no_access.html"})
	_, err := s.client.AdminListTeams(context.Background(), 1383)
	// AuthAdmin is what this used to be, and it is what makes the CLI blame
	// the credentials; AuthAdminScope is the whole point of the fix.
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthAdminScope {
		t.Fatalf("err = %v, want AuthAdminScope", err)
	}
	if !strings.Contains(err.Error(), "teams") {
		t.Errorf("err = %v, want the section name preserved", err)
	}
}

// TestAdminUpdateGameClearsTextField covers blanking a text field. Measured
// against classic.dzzzr.ru on 2026-09-09 the engine does store an empty
// greeting, but a value type cannot tell "" from "not given", so the request
// used to be dropped while the command still reported success.
func TestAdminUpdateGameClearsTextField(t *testing.T) {
	s := newAdminServer(t, map[string]string{"games": "admin_games_list.html"})
	err := s.client.AdminUpdateGame(context.Background(), 4243, dzzzr.GameParams{
		Name:  "Оставить имя",
		Clear: []string{"greeting", "anons"},
	})
	if err != nil {
		t.Fatal(err)
	}
	f := s.lastPost()
	if f.Get("name") != "Оставить имя" {
		t.Errorf("name = %q", f.Get("name"))
	}
	for _, k := range []string{"greeting", "anons"} {
		if !f.Has(k) || f.Get(k) != "" {
			t.Errorf("поле %s не очищено: %q (есть: %v)", k, f.Get(k), f.Has(k))
		}
	}
}

func TestAdminGetGame(t *testing.T) {
	s := newAdminServer(t, map[string]string{"games": "admin_games_list.html"})
	info, err := s.client.AdminGetGame(context.Background(), 4243)
	if err != nil {
		t.Fatal(err)
	}
	if s.gets[0].Get("id") != "4243" {
		t.Errorf("query = %v", s.gets[0])
	}
	p := info.Params
	if info.ID != 4243 || p.Name != "Будущая игра" || p.Number != "113" || p.Date != "15.09.2026" || p.Time != "21:00" || p.Author != "Штаб" || p.League != 1 {
		t.Errorf("params = %+v", p)
	}
	if p.OtherLeague == nil || !*p.OtherLeague || p.Publish == nil || !*p.Publish || p.Finished == nil || *p.Finished {
		t.Errorf("checkboxes = %+v %+v %+v", p.OtherLeague, p.Publish, p.Finished)
	}
	if p.ClueMin != 30 || p.ClueMin2 != 20 || p.ClueMin3 != 10 || p.MasterCode != "MASTER" || p.MasterCodeShtraf != 15 || p.ClueBeforeShtraf != 10 || p.Penalty != 30 || p.Price != "1500" {
		t.Errorf("numbers = %+v", p)
	}
	if p.Legend != "Легенда <b>игры</b>" || p.BreafPlace != "Парковка у ТЦ" || p.Greeting != "Спасибо за игру!" || p.Zone != "0" {
		t.Errorf("texts = %+v", p)
	}
	if info.Fields.Get("season") != "12" {
		t.Errorf("season select = %q", info.Fields.Get("season"))
	}
}

func TestAdminCreateGame(t *testing.T) {
	s := newAdminServer(t, map[string]string{"games": "admin_games_list.html"})
	s.redirect = func(form url.Values) string { return "/moscow/admin/?action=games&edit=1&id=4300&ofset=0&Project=" }
	pub := true
	id, err := s.client.AdminCreateGame(context.Background(), dzzzr.GameParams{Name: "Новая", Date: "10.09.2026", Time: "21:00", League: 2, Publish: &pub, ClueMin2: 25})
	if err != nil {
		t.Fatal(err)
	}
	if id != 4300 {
		t.Errorf("id = %d", id)
	}
	f := s.lastPost()
	if f.Get("action") != "add_game" || f.Get("name") != "Новая" || f.Get("date") != "10.09.2026" || f.Get("time") != "21:00" || f.Get("league") != "2" || f.Get("publish") != "on" || f.Get("clueMin") != "30" || f.Get("clueMin2") != "25" {
		t.Errorf("form = %v", f)
	}
	if _, err := s.client.AdminCreateGame(context.Background(), dzzzr.GameParams{Name: "x"}); err == nil {
		t.Error("missing date must fail before any request")
	}
}

func TestAdminUpdateGame(t *testing.T) {
	s := newAdminServer(t, map[string]string{"games": "admin_games_list.html"})
	fin := true
	other := false
	err := s.client.AdminUpdateGame(context.Background(), 4243, dzzzr.GameParams{Name: "Переименована", Finished: &fin, OtherLeague: &other})
	if err != nil {
		t.Fatal(err)
	}
	f := s.lastPost()
	if f.Get("action") != "update_game" || f.Get("id") != "4243" || f.Get("name") != "Переименована" {
		t.Errorf("form = %v", f)
	}
	// Untouched fields are carried over from the form.
	if f.Get("author") != "Штаб" || f.Get("clueMin2") != "20" || f.Get("publish") != "on" || f.Get("season") != "12" || f.Get("zone") != "0" {
		t.Errorf("carried fields = %v", f)
	}
	if f.Get("finished") != "on" || f.Has("otherLeague") {
		t.Errorf("checkbox overrides = finished %q otherLeague %v", f.Get("finished"), f.Has("otherLeague"))
	}
}

func TestAdminDeleteAndCopyGame(t *testing.T) {
	s := newAdminServer(t, map[string]string{"games": "admin_games_list.html"})
	if err := s.client.AdminDeleteGame(context.Background(), 4100); err != nil {
		t.Fatal(err)
	}
	if f := s.lastPost(); f.Get("action") != "del_games" || f.Get("id") != "4100" {
		t.Errorf("form = %v", f)
	}
	s.redirect = func(form url.Values) string { return "/moscow/admin/?action=games&id=4301&edit=1&Project=" }
	id, err := s.client.AdminCopyGame(context.Background(), 4242, true)
	if err != nil {
		t.Fatal(err)
	}
	if id != 4301 {
		t.Errorf("id = %d", id)
	}
	if f := s.lastPost(); f.Get("action") != "copy_game" || f.Get("game") != "4242" || f.Get("zadan") != "on" {
		t.Errorf("form = %v", f)
	}
}

func TestAdminWindows1251Page(t *testing.T) {
	page := fixture(t, "admin_games_list.html")
	enc := dzzzr.EncodeWindows1251ForTest(string(page))
	s := newAdminServer(t, map[string]string{})
	_ = s
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=windows-1251")
		_, _ = w.Write(enc)
	}), dzzzr.WithAdminCredentials("org", "secret"))
	games, err := c.AdminListGames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 3 || games[1].Name != "Ночной дозор: Осень" {
		t.Errorf("games = %+v", games)
	}
}
