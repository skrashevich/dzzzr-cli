package dzzzr_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func TestAdminListTeams(t *testing.T) {
	s := newAdminServer(t, map[string]string{"teams": "admin_teams_list.html"})
	teams, err := s.client.AdminListTeams(context.Background(), 4242)
	if err != nil {
		t.Fatal(err)
	}
	if q := s.gets[0]; q.Get("action") != "teams" || q.Get("categoryValue") != "4242" {
		t.Errorf("query = %v", q)
	}
	if len(teams) != 3 {
		t.Fatalf("teams = %+v", teams)
	}
	t0 := teams[0]
	if t0.ID != 5150 || t0.Name != "MockTeam" || t0.Captain != "demo" || t0.Status != dzzzr.ApplicationAccepted || t0.StatusText != "принята" {
		t.Errorf("teams[0] = %+v", t0)
	}
	if t0.Pin != "1234" || t0.Points != 12 || t0.GamesCount != 7 || t0.Blocked || !t0.Selected {
		t.Errorf("teams[0] numbers = %+v", t0)
	}
	t1 := teams[1]
	if t1.ID != 5151 || t1.Name != "Ночные совы" || t1.Captain != "alice" || t1.Status != dzzzr.ApplicationPending || t1.Pin != "" || t1.Points != 0 || t1.GamesCount != 3 {
		t.Errorf("teams[1] = %+v", t1)
	}
	t2 := teams[2]
	if t2.ID != 5152 || t2.Status != dzzzr.ApplicationRejected || !t2.Blocked {
		t.Errorf("teams[2] = %+v", t2)
	}
}

func TestAdminGetTeam(t *testing.T) {
	s := newAdminServer(t, map[string]string{"teams": "admin_teams_list.html"})
	info, err := s.client.AdminGetTeam(context.Background(), 4242, 5150)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != 5150 || info.Name != "MockTeam" || info.Captain != "demo" || info.Shtab != "alice" {
		t.Errorf("info = %+v", info)
	}
	if info.Status != dzzzr.ApplicationAccepted || info.Pin != "1234" || info.Points != 12 || !info.Novice || info.Blocked {
		t.Errorf("info fields = %+v", info)
	}
	if info.StatusNames["3"] != "вне зачета" {
		t.Errorf("status names = %v", info.StatusNames)
	}
}

func TestAdminSetApplication(t *testing.T) {
	s := newAdminServer(t, map[string]string{"teams": "admin_teams_list.html"})
	status := dzzzr.ApplicationOutOfContest
	points := 5
	novice := false
	err := s.client.AdminSetApplication(context.Background(), 4242, 5150, dzzzr.ApplicationParams{
		Status: &status, NewPin: true, Points: &points, Novice: &novice, Comment: "заявка принята вне зачёта",
	})
	if err != nil {
		t.Fatal(err)
	}
	f := s.lastPost()
	if f.Get("action") != "updateTeam" || f.Get("id") != "5150" || f.Get("categoryValue") != "4242" {
		t.Errorf("form = %v", f)
	}
	if f.Get("status") != "3" || f.Get("newPIN") != "on" || f.Get("points") != "5" || f.Has("novice") {
		t.Errorf("changed fields = %v", f)
	}
	if f.Get("comment") != "заявка принята вне зачёта" {
		t.Errorf("comment = %q", f.Get("comment"))
	}
	if f.Get("name") != "MockTeam" || f.Get("newCap") != "demo" || f.Get("shtab") != "alice" || f.Get("website") != "mock.example" || f.Get("autoApplication") != "on" {
		t.Errorf("carried fields = %v", f)
	}
	if f.Has("pinlogin") {
		t.Error("disabled pinlogin must not be submitted")
	}
}

func TestAdminBulkApplicationActions(t *testing.T) {
	s := newAdminServer(t, map[string]string{"teams": "admin_teams_list.html"})
	ctx := context.Background()
	if err := s.client.AdminAcceptAllApplications(ctx, 4242); err != nil {
		t.Fatal(err)
	}
	if f := s.lastPost(); f.Get("action") != "acceptAllApplications" || f.Get("categoryValue") != "4242" {
		t.Errorf("form = %v", f)
	}
	if err := s.client.AdminGeneratePins(ctx, 4242); err != nil {
		t.Fatal(err)
	}
	if f := s.lastPost(); f.Get("action") != "GeneratePinCodes" {
		t.Errorf("form = %v", f)
	}
	if err := s.client.AdminAddTeamToGame(ctx, 4242, 5153); err != nil {
		t.Fatal(err)
	}
	if f := s.lastPost(); f.Get("action") != "addTeamToGame" || f.Get("team") != "5153" {
		t.Errorf("form = %v", f)
	}
	if err := s.client.AdminRemoveApplication(ctx, 4242, 5151, dzzzr.ApplicationPending); err != nil {
		t.Fatal(err)
	}
	if f := s.lastPost(); f.Get("action") != "del_application" || f.Get("id") != "5151" || f.Get("status") != "0" {
		t.Errorf("form = %v", f)
	}
}

func TestAdminCreateTeam(t *testing.T) {
	s := newAdminServer(t, map[string]string{"teams": "admin_teams_list.html"})
	s.redirect = func(form url.Values) string { return "/moscow/admin/?action=teams&edit=1&id=5160&err=" }
	id, err := s.client.AdminCreateTeam(context.Background(), "Новая команда", "carol")
	if err != nil {
		t.Fatal(err)
	}
	if id != 5160 {
		t.Errorf("id = %d", id)
	}
	if f := s.lastPost(); f.Get("action") != "newTeam" || f.Get("name") != "Новая команда" || f.Get("captain") != "carol" {
		t.Errorf("form = %v", f)
	}
	s.redirect = func(form url.Values) string { return "/moscow/admin/?action=teams&edit=1&err=3" }
	if _, err := s.client.AdminCreateTeam(context.Background(), "Дубликат", "carol"); !dzzzr.IsEngineError(err) {
		t.Errorf("err = %v", err)
	}
}

// TestAdminTeamsOrganizerPage covers the teams page as classic.dzzzr.ru
// renders it for an account that may see it, measured 2026-09-10. Three
// things about that page broke the client at once:
//
//   - the team card's <form> spans table structure, so an HTML parser moves
//     its controls onto the create-team form printed earlier and the card
//     parses as empty — admin-team answered "no updateTeam form";
//   - the list identifies the selected application row through that same
//     card, so with the card gone selectedID stayed 0 and the row — which
//     carries no ?action=teams&id= link of its own — could not be matched:
//     a game with one accepted team reported no applications at all;
//   - the row names the team as "[31] Стальные Яйца", brackets and all.
func TestAdminTeamsOrganizerPage(t *testing.T) {
	s := newAdminServer(t, map[string]string{"teams": "admin_teams_organizer.html"})
	ctx := context.Background()

	teams, err := s.client.AdminListTeams(ctx, 1383)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 1 {
		t.Fatalf("заявок = %d, ожидалась 1: %+v", len(teams), teams)
	}
	if got := teams[0]; got.ID != 31 || got.Name != "Стальные Яйца" || got.Status != dzzzr.ApplicationAccepted || got.Pin != "877548" || got.GamesCount != 344 {
		t.Errorf("заявка = %+v", got)
	}

	info, err := s.client.AdminGetTeam(ctx, 1383, 31)
	if err != nil {
		t.Fatalf("карточка команды: %v", err)
	}
	if info.ID != 31 || info.Name != "Стальные Яйца" || info.Captain != "Kamrad_anin" || info.Shtab != "BuMa6ka" || info.Helper != "Ballbot2" {
		t.Errorf("карточка = %+v", info)
	}
	if info.Pin != "877548" || info.Status != dzzzr.ApplicationAccepted || info.Website != "steelballs.ru" || info.League != "высшая лига" {
		t.Errorf("поля карточки = %+v", info)
	}
	// The roster: the nameless pl[] box the engine puts at the head of the
	// block is not a member.
	if len(info.Roster) != 3 {
		t.Fatalf("состав = %+v", info.Roster)
	}
	want := map[string]bool{"4r2na": true, "Ballbot2": true, "zoya_O": false}
	for _, p := range info.Roster {
		on, known := want[p.Login]
		if !known {
			t.Errorf("в составе неожиданный %q", p.Login)
			continue
		}
		if p.OnTeam != on {
			t.Errorf("%s: в команде = %v, ожидалось %v", p.Login, p.OnTeam, on)
		}
	}
}

// TestAdminListCityTeams covers the city's team directory, which only an
// account that may open the teams section can see. The engine offers it as
// the picker of its "add a team to this game" form; the placeholder option
// it puts first is not a team.
func TestAdminListCityTeams(t *testing.T) {
	s := newAdminServer(t, map[string]string{"teams": "admin_teams_organizer.html"})
	teams, err := s.client.AdminListCityTeams(context.Background(), 1383)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 2 {
		t.Fatalf("команд = %+v", teams)
	}
	if teams[0] != (dzzzr.CityTeam{ID: 979, Name: "альфа x3m & A."}) || teams[1] != (dzzzr.CityTeam{ID: 1349, Name: "КИПИШ"}) {
		t.Errorf("команды = %+v", teams)
	}
}

func TestApplicationStatusText(t *testing.T) {
	if dzzzr.ApplicationStatusText(1) != "принята" || dzzzr.ApplicationStatusText(99) != "статус 99" {
		t.Error("ApplicationStatusText")
	}
}
