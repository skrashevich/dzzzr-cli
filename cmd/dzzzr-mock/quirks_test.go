package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// rawGameState fetches go/?api=true exactly as it goes over the wire, before
// the client repairs anything.
func rawGameState(t *testing.T, base, token string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/moscow/go/?api=true&s="+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("moscow_demo", testPin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// The doubled comma only exists when a сквозное task sits in the main level
// slot, which the plain scenario can never reach: mainLine skips every bonus
// level. Without the quirk this reproduction would be unreachable code that
// looks like coverage.
func TestSkvozCurrentEmitsTheDoubledComma(t *testing.T) {
	srv, _ := startMock(t, quirkSkvozCurrent)
	c := signIn(t, srv, "demo")

	if raw := rawGameState(t, srv.URL, c.Session()); !strings.Contains(raw, "},,") {
		t.Fatalf("the reply is well-formed, so the repair is untested:\n%s", raw)
	}
	st, err := c.GetGame(context.Background())
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if st.Level == nil || !st.Level.Skvoz.Bool() {
		t.Fatalf("level = %+v", st.Level)
	}
	if st.Level.BonusLevelTime.Int() != 10 {
		t.Errorf("bonusLevelTime = %d, want the minutes the task is worth", st.Level.BonusLevelTime.Int())
	}
	// Live level 12 was both сквозное and бонусное while still current.
	if !st.Level.IsBonusLevel.Bool() {
		t.Errorf("isBonusLevel not reported: %+v", st.Level)
	}
}

// The demo town prints a leftover debug dump ahead of the JSON.
func TestNoiseBeforeTheGameState(t *testing.T) {
	srv, _ := startMock(t, quirkNoise)
	c := signIn(t, srv, "demo")

	if raw := rawGameState(t, srv.URL, c.Session()); !strings.HasPrefix(raw, "<!--") {
		t.Fatalf("no debug dump to skip:\n%.80s", raw)
	}
	if _, err := c.GetGame(context.Background()); err != nil {
		t.Fatalf("get game: %v", err)
	}
}

// go_nogame.tpl and its siblings answer with a numeric error and HTTP 200.
func TestNoGameIsReportedRatherThanDecodedAsEmpty(t *testing.T) {
	srv, _ := startMock(t, quirkNoGame)
	c := signIn(t, srv, "demo")

	st, err := c.GetGame(context.Background())
	if st != nil {
		t.Fatalf("state = %+v, want none", st)
	}
	if !dzzzr.IsGameAccessError(err) {
		t.Fatalf("err = %v, want GameAccessError", err)
	}
}

// API/game.php prints {"err":13} and exits while the engine is switched off.
// It carries no "error" key, so an envelope check that only looks for one
// reads it as an empty success.
func TestStoppedEngineIsReportedOnTheReadEndpoints(t *testing.T) {
	srv, _ := startMock(t, quirkStopped)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	for name, call := range map[string]func() error{
		"level info": func() error { _, err := c.GetLevelInfo(ctx); return err },
		"team stat":  func() error { _, err := c.GetStat(ctx); return err },
		"team log":   func() error { _, err := c.GetLog(ctx); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("a stopped engine was reported as an empty success")
			}
			var ee *dzzzr.EngineError
			if !errors.As(err, &ee) || ee.Code != dzzzr.ErrEngineStopped {
				t.Fatalf("err = %v, want engine error %d", err, dzzzr.ErrEngineStopped)
			}
		})
	}
}

// A 200 with no body at all must be named as such.
func TestEmptyLevelBodyIsNamed(t *testing.T) {
	srv, _ := startMock(t, quirkEmptyLevel)
	c := signIn(t, srv, "demo")

	_, err := c.GetLevelInfo(context.Background())
	if err == nil || !strings.Contains(err.Error(), "empty body") {
		t.Fatalf("err = %v, want an empty-body report", err)
	}
}

// In games with a free order the engine sends the choices only as a rendered
// form inside the state.
func TestChooseLevelSelectorIsReadable(t *testing.T) {
	srv, _ := startMock(t, quirkChooseLevel)
	c := signIn(t, srv, "demo")

	st, err := c.GetGame(context.Background())
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	choices := st.NextLevelChoices()
	if len(choices) != st.TotalLevels.Int() {
		t.Fatalf("choices = %+v, want one per main level (%d)", choices, st.TotalLevels.Int())
	}
	if choices[0].Number != 1 || choices[0].Title == "" {
		t.Fatalf("choices = %+v", choices)
	}
}

// The engine answers a control action it declines exactly as it answers one
// it performed, so nothing in the reply says it refused.
func TestSilentRefusalCarriesNoResultCode(t *testing.T) {
	srv, _ := startMock(t, quirkSilentRefusal)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	before, err := c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	res, err := c.Abandon(ctx)
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if res.Err != 0 {
		t.Fatalf("err = %d, want the engine's silence", res.Err)
	}
	after, err := c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if after.Level.LevelNumber != before.Level.LevelNumber {
		t.Fatalf("the level changed after a refused abandon: %v -> %v",
			before.Level.LevelNumber, after.Level.LevelNumber)
	}
}

// quirkAdminNoAccess reproduces the "valid organizer credentials, wrong
// section" reply measured against classic.dzzzr.ru on 2026-09-09: teams,
// the live log and the organizer chat (read and write) all answer 200 with
// "Вы не имеете доступа к этому разделу" instead of their normal content.
func TestAdminNoAccessIsReportedNotSwallowed(t *testing.T) {
	srv, s := startMock(t, quirkAdminNoAccess)
	c := dzzzr.New("moscow",
		dzzzr.WithBaseURL(srv.URL+"/moscow/"),
		dzzzr.WithAdminCredentials(adminLogin, adminPassword),
		dzzzr.WithAdminDelay(0))
	ctx := context.Background()
	gameID := s.state.GameID

	if _, err := c.AdminListTeams(ctx, gameID); dzzzr.AuthErrorKindOf(err) != dzzzr.AuthAdminScope {
		t.Errorf("AdminListTeams err = %v, want AuthAdminScope", err)
	}
	if _, _, err := c.AdminGetLog(ctx, gameID, ""); dzzzr.AuthErrorKindOf(err) != dzzzr.AuthAdminScope {
		t.Errorf("AdminGetLog err = %v, want AuthAdminScope", err)
	}
	if _, err := c.AdminListMessages(ctx, gameID, 0); dzzzr.AuthErrorKindOf(err) != dzzzr.AuthAdminScope {
		t.Errorf("AdminListMessages err = %v, want AuthAdminScope (not a silent empty chat)", err)
	}
	if err := c.AdminSendMessage(ctx, gameID, 0, "hi", "", false); dzzzr.AuthErrorKindOf(err) != dzzzr.AuthAdminScope {
		t.Errorf("AdminSendMessage err = %v, want AuthAdminScope (not a silent send)", err)
	}
}

// quirkSelectedLevelZero makes the mock print "0." for the level open in the
// editor, the way the engine does. The list must still carry every level with
// its real position, which the level's own order_p supplies.
func TestSelectedLevelZeroKeepsEveryLevel(t *testing.T) {
	srv, s := startMock(t, quirkSelectedLevelZero)
	c := dzzzr.New("moscow",
		dzzzr.WithBaseURL(srv.URL+"/moscow/"),
		dzzzr.WithAdminCredentials(adminLogin, adminPassword),
		dzzzr.WithAdminDelay(0))

	levels, err := c.AdminListLevels(context.Background(), s.state.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != len(s.state.Levels) {
		t.Fatalf("уровней = %d, в игре %d", len(levels), len(s.state.Levels))
	}
	for i, l := range levels {
		if l.Order != i+1 {
			t.Errorf("levels[%d].Order = %d, ожидалось %d", i, l.Order, i+1)
		}
	}
}

// quirkUnclosedForm reproduces the engine's unclosed create-team form, which
// makes the team card that follows unreachable through the parse tree. The
// client must still read the card — and the application list, which finds its
// selected row through that card.
func TestUnclosedFormStillYieldsTheTeamCard(t *testing.T) {
	srv, s := startMock(t, quirkUnclosedForm)
	c := dzzzr.New("moscow",
		dzzzr.WithBaseURL(srv.URL+"/moscow/"),
		dzzzr.WithAdminCredentials(adminLogin, adminPassword),
		dzzzr.WithAdminDelay(0))
	ctx := context.Background()

	info, err := c.AdminGetTeam(ctx, s.state.GameID, s.state.TeamID)
	if err != nil {
		t.Fatalf("карточка команды: %v", err)
	}
	if info.ID != s.state.TeamID || info.Name != s.state.TeamName {
		t.Errorf("карточка = %+v", info)
	}
	if len(info.Roster) != len(s.state.Roster) {
		t.Errorf("состав = %+v", info.Roster)
	}
	teams, err := c.AdminListTeams(ctx, s.state.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) == 0 {
		t.Fatal("список заявок пуст, хотя команда заявлена")
	}
}

// The plain scenario stays well-behaved, so tests that are not about a quirk
// are not paying for one.
func TestPlainMockServesValidJSON(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	raw := rawGameState(t, srv.URL, c.Session())
	if strings.Contains(raw, "},,") || strings.HasPrefix(raw, "<!--") {
		t.Fatalf("the plain mock is not plain:\n%.120s", raw)
	}
}

// A misspelled quirk must fail loudly: silently serving a well-behaved engine
// would make a test pass for the wrong reason.
func TestParseQuirksRejectsATypo(t *testing.T) {
	got, err := parseQuirks("noise, nogame")
	if err != nil {
		t.Fatalf("known names rejected: %v", err)
	}
	if !slices.Equal(got, []quirk{quirkNoise, quirkNoGame}) {
		t.Fatalf("quirks = %v", got)
	}
	if _, err := parseQuirks("nosie"); err == nil || !strings.Contains(err.Error(), "unknown quirk nosie") {
		t.Fatalf("err = %v", err)
	}
}

// isLevelFinished "1" is what tells a team holding every main code that the
// level is done and next-level is theirs to take. With the default
// bonusAfter of -1 the engine hands the next level over immediately, so that
// state never appears in the slot a player's summary renders.
func TestLevelFinishedIsReachableInTheMainSlot(t *testing.T) {
	srv, _ := startMock(t, quirkLevelFinished)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	st, err := c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if st.Level.IsLevelFinished.Bool() {
		t.Fatalf("the level is finished before a single code: %+v", st.Level)
	}
	var last *dzzzr.ActionResult
	for _, code := range []string{"1", "2", "3"} {
		res, err := c.SendCode(ctx, code)
		if err != nil {
			t.Fatalf("send %q: %v", code, err)
		}
		last = res
	}
	// The live engine answered the last main code of a level with bonus
	// codes left with 41, not 9. That code was unreachable here before.
	if last.Err != dzzzr.ErrAllMainCodesFound {
		t.Fatalf("last main code -> %d, want %d", last.Err, dzzzr.ErrAllMainCodesFound)
	}
	if raw := rawGameState(t, srv.URL, c.Session()); !strings.Contains(raw, `"isLevelFinished": "1"`) {
		t.Fatalf("the engine never says the level is finished:\n%s", raw)
	}
	st, err = c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if !st.Level.IsLevelFinished.Bool() {
		t.Fatalf("level = %+v", st.Level)
	}
	// The level stays current: bonus codes are still out there.
	if st.Level.LevelNumber.Int() != 1 {
		t.Fatalf("the level advanced anyway: %d", st.Level.LevelNumber.Int())
	}
}
