package main

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// adminClient starts a mock and returns a client with organizer credentials,
// so the tests exercise the real scraper against the real pages.
func adminClient(t *testing.T) (*dzzzr.Client, *server) {
	t.Helper()
	s := newServer("moscow", log.New(io.Discard, "", 0))
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := dzzzr.New("moscow",
		dzzzr.WithBaseURL(srv.URL+"/moscow/"),
		dzzzr.WithAdminCredentials(adminLogin, adminPassword),
		dzzzr.WithAdminDelay(0))
	return c, s
}

// TestAdminMoveLevelReorders checks reordering against the mock's own state:
// the engine swaps a level with its neighbor and keeps both database ids, so
// only the order changes, and a move past either end is accepted silently.
func TestAdminMoveLevelReorders(t *testing.T) {
	c, s := adminClient(t)
	ctx := context.Background()
	gameID := s.state.GameID

	before, err := c.AdminListLevels(ctx, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) < 2 {
		t.Fatalf("нужно хотя бы два уровня, есть %d", len(before))
	}
	first, second := before[0].ID, before[1].ID

	if err := c.AdminMoveLevel(ctx, first, false); err != nil {
		t.Fatal(err)
	}
	after, err := c.AdminListLevels(ctx, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if after[0].ID != second || after[1].ID != first {
		t.Fatalf("порядок не изменился: %d,%d", after[0].ID, after[1].ID)
	}
	if after[0].Order != 1 || after[1].Order != 2 {
		t.Errorf("номера не пересчитаны: %d,%d", after[0].Order, after[1].Order)
	}

	// Moving the first level up is a no-op the engine accepts.
	if err := c.AdminMoveLevel(ctx, second, true); err != nil {
		t.Fatal(err)
	}
	again, err := c.AdminListLevels(ctx, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if again[0].ID != second || again[1].ID != first {
		t.Errorf("движение за край изменило порядок: %d,%d", again[0].ID, again[1].ID)
	}
}

// TestAdminTeamCardCarriesTheRoster checks the roster end to end against the
// mock: the engine's team card lists every member with a checkbox ticked for
// the ones it counts as being on the team.
func TestAdminTeamCardCarriesTheRoster(t *testing.T) {
	c, s := adminClient(t)
	info, err := c.AdminGetTeam(context.Background(), s.state.GameID, s.state.TeamID)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Roster) != len(s.state.Roster) {
		t.Fatalf("состав = %+v, в моке %d игроков", info.Roster, len(s.state.Roster))
	}
	want := map[string]bool{}
	for _, p := range s.state.Roster {
		want[p.Login] = p.OnTeam
	}
	for _, p := range info.Roster {
		on, ok := want[p.Login]
		if !ok {
			t.Errorf("неожиданный игрок %q", p.Login)
			continue
		}
		if p.OnTeam != on {
			t.Errorf("%s: в команде = %v, ожидалось %v", p.Login, p.OnTeam, on)
		}
	}
	if info.League == "" {
		t.Error("лига команды не прочитана")
	}
}

// TestAdminCityTeamsDirectory covers the city directory the engine offers in
// its add-a-team picker.
func TestAdminCityTeamsDirectory(t *testing.T) {
	c, s := adminClient(t)
	teams, err := c.AdminListCityTeams(context.Background(), s.state.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != len(s.state.CityTeams) {
		t.Fatalf("команд = %+v, в моке %d", teams, len(s.state.CityTeams))
	}
	for i, want := range s.state.CityTeams {
		if teams[i].ID != want.ID || teams[i].Name != want.Name {
			t.Errorf("teams[%d] = %+v, ожидалось %+v", i, teams[i], want)
		}
	}
}

// TestAdminCyrillicSurvivesTheRoundTrip guards the charset of the write
// path: the administration area is windows-1251 and converts nothing it
// receives, so a client posting UTF-8 stores mojibake in the organizer's own
// texts. The mock speaks the same charset, which is what makes this visible.
func TestAdminCyrillicSurvivesTheRoundTrip(t *testing.T) {
	c, s := adminClient(t)
	ctx := context.Background()
	gameID := s.state.GameID

	const name = "Ночной дозор «Ёжик»"
	if err := c.AdminUpdateGame(ctx, gameID, dzzzr.GameParams{Name: name}); err != nil {
		t.Fatal(err)
	}
	if s.state.GameName != name {
		t.Fatalf("the engine stored %q, want %q", s.state.GameName, name)
	}
	info, err := c.AdminGetGame(ctx, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Params.Name != name {
		t.Errorf("reading it back gave %q, want %q", info.Params.Name, name)
	}

	const question = "Найдите дом с «ёлкой» — код на стене"
	id, err := c.AdminCreateLevel(ctx, gameID, dzzzr.LevelParams{
		Title: "Кириллица", Question: question, Hint1: "П1", Hint2: "П2",
		Codes: []dzzzr.LevelCode{{Code: "КОД1", Danger: "1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	level, err := c.AdminGetLevel(ctx, gameID, id)
	if err != nil {
		t.Fatal(err)
	}
	if level.Params.Question != question {
		t.Errorf("question = %q, want %q", level.Params.Question, question)
	}
	if len(level.Params.Codes) != 1 || level.Params.Codes[0].Code != "КОД1" {
		t.Errorf("codes = %+v", level.Params.Codes)
	}

	const text = "Штаб: ждите на «Пушкинской»"
	if err := c.AdminSendMessage(ctx, gameID, 0, text, "", false); err != nil {
		t.Fatal(err)
	}
	msgs, err := c.AdminListMessages(ctx, gameID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 || msgs[0].Content != text {
		t.Errorf("message = %+v, want %q", msgs, text)
	}
}

func TestAdminUnauthorizedIsRejected(t *testing.T) {
	c, _ := adminClient(t)
	c.SetAdminCredentials("someone", "wrong")
	if _, err := c.AdminListGames(context.Background()); dzzzr.AuthErrorKindOf(err) != dzzzr.AuthAdmin {
		t.Fatalf("err = %v", err)
	}
}

func TestAdminGamesRoundTrip(t *testing.T) {
	c, s := adminClient(t)
	ctx := context.Background()
	games, err := c.AdminListGames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].ID != s.state.GameID || games[0].Name != "Тестовая игра" {
		t.Fatalf("games = %+v", games)
	}
	info, err := c.AdminGetGame(ctx, s.state.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Params.Name != "Тестовая игра" || info.Params.MasterCode != "MASTER" {
		t.Errorf("game form = %+v", info.Params)
	}
	if info.Params.ClueMin != 30 || info.Params.Publish == nil || !*info.Params.Publish {
		t.Errorf("game defaults = %+v", info.Params)
	}

	if err := c.AdminUpdateGame(ctx, s.state.GameID, dzzzr.GameParams{Name: "Переименованная"}); err != nil {
		t.Fatal(err)
	}
	if s.state.GameName != "Переименованная" {
		t.Errorf("mock game name = %q", s.state.GameName)
	}
	// The untouched fields survived the round trip through the form.
	if s.state.MasterCode != "MASTER" || s.state.Greeting == "" {
		t.Errorf("update lost fields: master %q greeting %q", s.state.MasterCode, s.state.Greeting)
	}
	if id, err := c.AdminCopyGame(ctx, s.state.GameID, true); err != nil || id == 0 {
		t.Errorf("copy game: %d %v", id, err)
	}
}

func TestAdminLevelsRoundTrip(t *testing.T) {
	c, s := adminClient(t)
	ctx := context.Background()
	gameID := s.state.GameID

	levels, err := c.AdminListLevels(ctx, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != len(s.state.Levels) {
		t.Fatalf("levels = %+v", levels)
	}
	first := levels[0]
	if first.Order != 1 || first.Title == "" || len(first.Codes) != 3 {
		t.Errorf("levels[0] = %+v", first)
	}
	if len(first.BonusCodes) != 1 || !strings.HasPrefix(first.BonusCodes[0], "B1") {
		t.Errorf("levels[0].BonusCodes = %v", first.BonusCodes)
	}

	info, err := c.AdminGetLevel(ctx, gameID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Params.Codes) != 3 || info.Params.Codes[0].Code != "1" || info.Params.Codes[0].Danger == "" {
		t.Errorf("level codes = %+v", info.Params.Codes)
	}
	if len(info.Params.Spoilers) != 1 || info.Params.Spoilers[0].Code != "SP1" {
		t.Errorf("level spoilers = %+v", info.Params.Spoilers)
	}
	if len(info.Params.BonusCodes) != 1 || info.Params.BonusCodes[0].Minutes != 10 {
		t.Errorf("level bonus codes = %+v", info.Params.BonusCodes)
	}

	newID, err := c.AdminCreateLevel(ctx, gameID, dzzzr.LevelParams{
		Title: "Новый уровень", Question: "Вопрос", Hint1: "П1", Hint2: "П2",
		Codes:      []dzzzr.LevelCode{{Code: "NEW1", Danger: "1", Sector: 1}},
		BonusCodes: []dzzzr.BonusCode{{Code: "NEWB", Danger: "1", Minutes: 7}},
		Spoilers:   []dzzzr.SpoilerParams{{Code: "NEWSP", Text: "Скрытое", Penalty: 3}},
		CodeCount:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	created := levelByID(s.state, newID)
	if created == nil {
		t.Fatalf("level %d was not created; levels: %d", newID, len(s.state.Levels))
	}
	if created.Title != "Новый уровень" || len(created.Codes) != 1 || created.Codes[0] != "NEW1" {
		t.Errorf("created level = %+v", created)
	}
	if len(created.BonusCodes) != 1 || created.BonusTimes[0] != 7 || len(created.Spoilers) != 1 {
		t.Errorf("created extras = %+v %+v", created.BonusCodes, created.Spoilers)
	}

	if err := c.AdminUpdateLevel(ctx, gameID, newID, dzzzr.LevelParams{
		Title: "Переименованный", Codes: []dzzzr.LevelCode{{Code: "OTHER", Danger: "2"}},
	}); err != nil {
		t.Fatal(err)
	}
	if created.Title != "Переименованный" || len(created.Codes) != 1 || created.Codes[0] != "OTHER" {
		t.Errorf("updated level = %+v", created)
	}
	if created.Question != "Вопрос" {
		t.Errorf("update lost the question: %q", created.Question)
	}

	before := len(s.state.Levels)
	if err := c.AdminDeleteLevel(ctx, gameID, newID); err != nil {
		t.Fatal(err)
	}
	if len(s.state.Levels) != before-1 {
		t.Errorf("level count = %d, want %d", len(s.state.Levels), before-1)
	}
}

func TestAdminTeamsRoundTrip(t *testing.T) {
	c, s := adminClient(t)
	ctx := context.Background()
	gameID := s.state.GameID

	teams, err := c.AdminListTeams(ctx, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 1 || teams[0].ID != s.state.TeamID || teams[0].Captain != s.state.Captain {
		t.Fatalf("teams = %+v", teams)
	}
	if teams[0].Pin != s.state.Pin {
		t.Errorf("pin = %q, want %q", teams[0].Pin, s.state.Pin)
	}

	info, err := c.AdminGetTeam(ctx, gameID, s.state.TeamID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != s.state.TeamName || info.Captain != s.state.Captain {
		t.Errorf("team form = %+v", info)
	}

	status := dzzzr.ApplicationOutOfContest
	points := 42
	if err := c.AdminSetApplication(ctx, gameID, s.state.TeamID, dzzzr.ApplicationParams{
		Status: &status, Points: &points, NewPin: true, Comment: "вне зачёта",
	}); err != nil {
		t.Fatal(err)
	}
	if s.state.Status != dzzzr.ApplicationOutOfContest || s.state.RatingPoints != 42 || s.state.Pin != "5678" {
		t.Errorf("application = status %d points %d pin %q", s.state.Status, s.state.RatingPoints, s.state.Pin)
	}
	if s.state.TeamName != "MockTeam" {
		t.Errorf("update lost the team name: %q", s.state.TeamName)
	}

	if err := c.AdminAcceptAllApplications(ctx, gameID); err != nil {
		t.Fatal(err)
	}
	if s.state.Status != dzzzr.ApplicationAccepted {
		t.Errorf("accept all left status %d", s.state.Status)
	}
}

func TestAdminMonitorAndInterventions(t *testing.T) {
	c, s := adminClient(t)
	ctx := context.Background()
	gameID := s.state.GameID

	st, err := c.AdminMonitor(ctx, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if st.GameID != gameID || len(st.Teams) != 1 || st.Teams[0].ID != s.state.TeamID {
		t.Fatalf("monitor = %+v", st)
	}
	if len(st.Levels) != len(s.state.Levels) || st.EngineStopped {
		t.Errorf("monitor levels = %+v stopped = %v", st.Levels, st.EngineStopped)
	}

	if err := c.AdminGiveLevel(ctx, gameID, s.state.TeamID, 2, ""); err != nil {
		t.Fatal(err)
	}
	if s.state.Current != 2 {
		t.Errorf("current level = %d after admin gave level 2", s.state.Current)
	}
	if err := c.AdminAcceptCode(ctx, gameID, s.state.TeamID, 2, "4", ""); err != nil {
		t.Fatal(err)
	}
	if p := s.state.Progress[2]; p == nil || !p.Codes["4"] {
		t.Errorf("code was not recorded: %+v", s.state.Progress[2])
	}
	if err := c.AdminAddCorrection(ctx, gameID, s.state.TeamID, "bonus", 10, "за красоту"); err != nil {
		t.Fatal(err)
	}
	if s.state.BonusMin != 10 {
		t.Errorf("bonus minutes = %d", s.state.BonusMin)
	}
	if err := c.AdminAddCorrection(ctx, gameID, s.state.TeamID, "penalty", 5, "опоздание"); err != nil {
		t.Fatal(err)
	}
	if s.state.PenaltyMin != 5 {
		t.Errorf("penalty minutes = %d", s.state.PenaltyMin)
	}
	if err := c.AdminBlockTeam(ctx, gameID, s.state.TeamID); err != nil {
		t.Fatal(err)
	}
	if !s.state.Blocked {
		t.Error("team was not blocked")
	}
	if err := c.AdminUnblockTeam(ctx, gameID, s.state.TeamID); err != nil {
		t.Fatal(err)
	}
	if s.state.Blocked {
		t.Error("team was not unblocked")
	}
	if err := c.AdminSetEngine(ctx, gameID, false); err != nil {
		t.Fatal(err)
	}
	if !s.state.EngineStopped {
		t.Error("engine was not stopped")
	}
	// Asking again for a state the engine is already in must not flip it back:
	// switchRobot is a toggle, so a blind second post would restart the game.
	if err := c.AdminSetEngine(ctx, gameID, false); err != nil {
		t.Fatal(err)
	}
	if !s.state.EngineStopped {
		t.Error("a repeated stop restarted the engine")
	}
	st, err = c.AdminMonitor(ctx, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if !st.EngineStopped {
		t.Error("monitor must report the stopped engine")
	}
	if err := c.AdminSetEngine(ctx, gameID, true); err != nil {
		t.Fatal(err)
	}
	if s.state.EngineStopped {
		t.Error("engine was not restarted")
	}
	if err := c.AdminToggleEngine(ctx, gameID); err != nil {
		t.Fatal(err)
	}
	if !s.state.EngineStopped {
		t.Error("the explicit toggle did not flip the engine")
	}
	if err := c.AdminToggleEngine(ctx, gameID); err != nil {
		t.Fatal(err)
	}
	if err := c.AdminFinishGame(ctx, gameID); err != nil {
		t.Fatal(err)
	}
	if !s.state.Finished {
		t.Error("game was not finished")
	}
}

func TestAdminLogPolling(t *testing.T) {
	c, s := adminClient(t)
	ctx := context.Background()
	gameID := s.state.GameID

	entries, since, err := c.AdminGetLog(ctx, gameID, "")
	if err != nil {
		t.Fatal(err)
	}
	if since == "" {
		t.Fatal("the first poll must return a timestamp for the next one")
	}
	if len(entries) == 0 {
		t.Fatal("the demo game has a log line from the start")
	}
	first := entries[0]
	if first.TeamID != s.state.TeamID || first.Event == 0 || first.Time == "" {
		t.Errorf("entries[0] = %+v", first)
	}

	// Polling again from the returned timestamp yields nothing new.
	entries, since2, err := c.AdminGetLog(ctx, gameID, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("second poll returned %+v", entries)
	}

	// An organizer action shows up in the next poll.
	if err := c.AdminGiveLevel(ctx, gameID, s.state.TeamID, 3, ""); err != nil {
		t.Fatal(err)
	}
	entries, _, err = c.AdminGetLog(ctx, gameID, since2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("giving a level must appear in the log")
	}
	if entries[len(entries)-1].Event != dzzzr.LogEventLevelIssued {
		t.Errorf("last event = %+v", entries[len(entries)-1])
	}
}

func TestAdminMessagesRoundTrip(t *testing.T) {
	c, s := adminClient(t)
	ctx := context.Background()
	gameID := s.state.GameID

	const showAfter = "2026-09-09 21:00:00"
	if err := c.AdminSendMessage(ctx, gameID, s.state.TeamID, "Ждите на месте", showAfter, true); err != nil {
		t.Fatal(err)
	}
	msgs, err := c.AdminListMessages(ctx, gameID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	newest := msgs[0]
	if newest.Content != "Ждите на месте" || newest.Timestamp == "" {
		t.Fatalf("newest message = %+v", newest)
	}
	// The engine stores the important flag without ever printing it, so only
	// the delay comes back.
	if newest.ShowAfter != showAfter {
		t.Errorf("show-after = %q, want %q", newest.ShowAfter, showAfter)
	}
	if !s.state.OrgMessages[len(s.state.OrgMessages)-1].Important {
		t.Error("the important flag did not reach the engine")
	}

	// The team sees the organizer's message in its own interface.
	player := dzzzr.New("moscow", dzzzr.WithBaseURL(c.BaseURL()), dzzzr.WithCredentials(dzzzr.Credentials{Captain: s.state.Captain, Pin: s.state.Pin}))
	if _, err := player.Login(ctx, s.state.Captain, "secret"); err != nil {
		t.Fatal(err)
	}
	game, err := player.GetGame(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range game.Messages {
		if strings.Contains(m.Text, "Ждите на месте") {
			found = true
		}
	}
	if !found {
		t.Errorf("the team does not see the message: %+v", game.Messages)
	}

	if err := c.AdminDeleteMessage(ctx, gameID, newest.Timestamp); err != nil {
		t.Fatal(err)
	}
	msgs, err = c.AdminListMessages(ctx, gameID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Timestamp == newest.Timestamp {
			t.Error("the message was not deleted")
		}
	}

	if err := c.AdminDeleteAllMessages(ctx, gameID); err != nil {
		t.Fatal(err)
	}
	msgs, err = c.AdminListMessages(ctx, gameID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("messages left after a full delete: %+v", msgs)
	}
}
