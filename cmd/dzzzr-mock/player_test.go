package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

const testPin = "1234"

// startMock brings up the mock behind an httptest server and returns both, so
// a test can reach the state directly when it needs to.
func startMock(t *testing.T, quirks ...quirk) (*httptest.Server, *server) {
	t.Helper()
	mock := newServer("moscow", log.New(io.Discard, "", 0), quirks...)
	srv := httptest.NewServer(mock.Handler())
	t.Cleanup(srv.Close)
	return srv, mock
}

// signIn creates a client for the given site login and signs it in.
func signIn(t *testing.T, srv *httptest.Server, login string) *dzzzr.Client {
	t.Helper()
	c := dzzzr.New("moscow",
		dzzzr.WithBaseURL(srv.URL+"/moscow/"),
		dzzzr.WithCredentials(dzzzr.Credentials{Captain: "demo", Pin: testPin}),
	)
	resp, err := c.Login(context.Background(), login, "secret")
	if err != nil {
		t.Fatalf("login %q: %v", login, err)
	}
	if !resp.OK() {
		t.Fatalf("login %q: code %d, error %q", login, resp.Code.Int(), resp.Error)
	}
	if len(resp.UserToken) != 32 {
		t.Fatalf("session token %q: want 32 characters", resp.UserToken)
	}
	return c
}

// send submits a code and fails the test when the transport breaks.
func send(t *testing.T, c *dzzzr.Client, code string) int {
	t.Helper()
	res, err := c.SendCode(context.Background(), code)
	if err != nil {
		t.Fatalf("send code %q: %v", code, err)
	}
	return res.Err
}

func TestWalkthroughFromFirstLevelToFinish(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	st, err := c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if st.GameID.Int() != 4242 || st.TeamName != "MockTeam" {
		t.Fatalf("game %d team %q: want 4242/MockTeam", st.GameID.Int(), st.TeamName)
	}
	if !st.GameStarted() {
		t.Fatal("game reports it has not started yet")
	}
	if st.Level == nil {
		t.Fatal("no current level in the game state")
	}
	if got := st.Level.LevelNumber.Int(); got != 1 {
		t.Fatalf("level number %d, want 1", got)
	}
	if st.Level.TotalCodes.Int() != 3 {
		t.Fatalf("total codes %d, want 3", st.Level.TotalCodes.Int())
	}
	if st.Level.Hint1 == "" {
		t.Fatal("first hint should be out an hour into the level")
	}
	if st.Level.TM.Int() <= 0 {
		t.Fatalf("tm %d, want the seconds left until the level ends", st.Level.TM.Int())
	}
	if len(st.BonusLevels) != 1 {
		t.Fatalf("bonus levels %d, want 1 (the trailing placeholder must be dropped)", len(st.BonusLevels))
	}
	if len(st.Messages) != 2 {
		t.Fatalf("messages %d, want the two the organizer left", len(st.Messages))
	}
	if !st.CanControl.Bool() {
		t.Fatal("the captain must be allowed to control the game")
	}

	if got := send(t, c, "1"); got != dzzzr.ErrCodeAcceptedPartial {
		t.Fatalf("first code: err %d, want %d", got, dzzzr.ErrCodeAcceptedPartial)
	}
	if got := send(t, c, "2"); got != dzzzr.ErrCodeAcceptedPartial {
		t.Fatalf("second code: err %d, want %d", got, dzzzr.ErrCodeAcceptedPartial)
	}
	if got := send(t, c, "3"); got != dzzzr.ErrCodeAcceptedNext {
		t.Fatalf("third code: err %d, want %d", got, dzzzr.ErrCodeAcceptedNext)
	}

	st, err = c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game after level 1: %v", err)
	}
	if st.Level == nil || st.Level.LevelNumber.Int() != 2 {
		t.Fatalf("after level 1 the team should be on level 2, got %+v", st.Level)
	}
	if st.Level.NeededCodes.Int() != 1 {
		t.Fatalf("needed codes %d, want 1 on level 2", st.Level.NeededCodes.Int())
	}

	if got := send(t, c, "4"); got != dzzzr.ErrCodeAcceptedNext {
		t.Fatalf("level 2 code: err %d, want %d", got, dzzzr.ErrCodeAcceptedNext)
	}
	st, err = c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game after level 2: %v", err)
	}
	if st.Level == nil || st.Level.LevelNumber.Int() != 3 {
		t.Fatalf("after level 2 the team should be on level 3, got %+v", st.Level)
	}

	if got := send(t, c, "нет такого кода"); got != dzzzr.ErrCodeRejected {
		t.Fatalf("wrong code: err %d, want %d", got, dzzzr.ErrCodeRejected)
	}
	if got := send(t, c, "6"); got != dzzzr.ErrCodeAcceptedNext {
		t.Fatalf("last code: err %d, want %d", got, dzzzr.ErrCodeAcceptedNext)
	}

	st, err = c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game after the last level: %v", err)
	}
	if !st.Finished.Bool() {
		t.Fatal("the game should be finished")
	}
	if st.Greeting == "" {
		t.Fatal("a finished game should greet the team")
	}
	if st.Level != nil {
		t.Fatalf("a finished game should have no level, got %+v", st.Level)
	}
	if got := send(t, c, "6"); got != dzzzr.ErrGameOver {
		t.Fatalf("code after the finish: err %d, want %d", got, dzzzr.ErrGameOver)
	}
}

func TestRepeatedAndNormalizedCodes(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")

	if got := send(t, c, "  1  "); got != dzzzr.ErrCodeAcceptedPartial {
		t.Fatalf("padded code: err %d, want %d", got, dzzzr.ErrCodeAcceptedPartial)
	}
	if got := send(t, c, "1"); got != dzzzr.ErrCodeRepeated {
		t.Fatalf("repeated code: err %d, want %d", got, dzzzr.ErrCodeRepeated)
	}
	if got := send(t, c, ""); got != dzzzr.ErrEmptyCode {
		t.Fatalf("empty code: err %d, want %d", got, dzzzr.ErrEmptyCode)
	}
}

func TestBonusCodeAcceptedOnceOnly(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")

	if got := send(t, c, "B1"); got != dzzzr.ErrBonusAccepted {
		t.Fatalf("bonus code: err %d, want %d", got, dzzzr.ErrBonusAccepted)
	}
	if got := send(t, c, "b1"); got != dzzzr.ErrBonusRepeated {
		t.Fatalf("repeated bonus code: err %d, want %d", got, dzzzr.ErrBonusRepeated)
	}

	st, err := c.GetGame(context.Background())
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if st.Level.BonusCodesFounded.Int() != 1 {
		t.Fatalf("bonus codes found %d, want 1", st.Level.BonusCodesFounded.Int())
	}
}

func TestFakeCodeCarriesPenalty(t *testing.T) {
	srv, mock := startMock(t)
	c := signIn(t, srv, "demo")

	if got := send(t, c, "FAKE"); got != dzzzr.ErrFakeCode {
		t.Fatalf("fake code: err %d, want %d", got, dzzzr.ErrFakeCode)
	}
	mock.mu.Lock()
	penalty := mock.state.PenaltyMin
	mock.mu.Unlock()
	if penalty != 10 {
		t.Fatalf("penalty %d minutes, want 10", penalty)
	}
}

func TestMasterCode(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")

	if got := send(t, c, "master"); got != dzzzr.ErrMasterCodePartial {
		t.Fatalf("universal code on level 1: err %d, want %d", got, dzzzr.ErrMasterCodePartial)
	}
	if got := send(t, c, "1"); got != dzzzr.ErrCodeAcceptedPartial {
		t.Fatalf("code after the universal one: err %d, want %d", got, dzzzr.ErrCodeAcceptedPartial)
	}
	if got := send(t, c, "2"); got != dzzzr.ErrCodeAcceptedNext {
		t.Fatalf("code that finishes level 1: err %d, want %d", got, dzzzr.ErrCodeAcceptedNext)
	}
	if got := send(t, c, "MASTER"); got != dzzzr.ErrMasterCodeForbidden {
		t.Fatalf("universal code on level 2: err %d, want %d", got, dzzzr.ErrMasterCodeForbidden)
	}
	if got := send(t, c, "4"); got != dzzzr.ErrCodeAcceptedNext {
		t.Fatalf("level 2 code: err %d, want %d", got, dzzzr.ErrCodeAcceptedNext)
	}
	if got := send(t, c, "MASTER"); got != dzzzr.ErrMasterCodeUsed {
		t.Fatalf("universal code used twice: err %d, want %d", got, dzzzr.ErrMasterCodeUsed)
	}
}

func TestSkvozBonusLevel(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	res, err := c.SendBonusCode(ctx, 4, "нет")
	if err != nil {
		t.Fatalf("send skvoz code: %v", err)
	}
	if res.Err != dzzzr.ErrSkvozCodeRejected {
		t.Fatalf("wrong skvoz code: err %d, want %d", res.Err, dzzzr.ErrSkvozCodeRejected)
	}

	res, err = c.SendBonusCode(ctx, 4, "SK1")
	if err != nil {
		t.Fatalf("send skvoz code: %v", err)
	}
	if res.Err != dzzzr.ErrSkvozCodeAccepted {
		t.Fatalf("skvoz code: err %d, want %d", res.Err, dzzzr.ErrSkvozCodeAccepted)
	}
	if !res.Accepted() {
		t.Fatal("the client should treat the skvoz code as accepted")
	}
}

func TestSpoilerCode(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	res, err := c.SendSpoilerCode(ctx, 1, "SP1")
	if err != nil {
		t.Fatalf("send spoiler code: %v", err)
	}
	if res.Err != dzzzr.ErrSpoilerAccepted {
		t.Fatalf("spoiler code: err %d, want %d", res.Err, dzzzr.ErrSpoilerAccepted)
	}

	res, err = c.SendSpoilerCode(ctx, 1, "нет")
	if err != nil {
		t.Fatalf("send spoiler code: %v", err)
	}
	if res.Err != dzzzr.ErrSpoilerRejected {
		t.Fatalf("wrong spoiler code: err %d, want %d", res.Err, dzzzr.ErrSpoilerRejected)
	}

	st, err := c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if len(st.Level.Spoilers) != 1 {
		t.Fatalf("spoilers %d, want 1", len(st.Level.Spoilers))
	}
	sp := st.Level.Spoilers[0]
	if !sp.Solved.Bool() || sp.Text != "Скрытая подсказка" {
		t.Fatalf("spoiler %+v: want it solved with its text revealed", sp)
	}
}

func TestTooManyWrongCodesBlockTheTeam(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")

	for i := 0; i < wrongTriesBeforeBlock; i++ {
		if got := send(t, c, "мимо"+string(rune('a'+i))); got != dzzzr.ErrCodeRejected {
			t.Fatalf("wrong code %d: err %d, want %d", i+1, got, dzzzr.ErrCodeRejected)
		}
	}
	if got := send(t, c, "1"); got != dzzzr.ErrTooManyAttempts {
		t.Fatalf("code after four wrong ones: err %d, want %d", got, dzzzr.ErrTooManyAttempts)
	}
}

func TestAbandonNeedsTheCaptain(t *testing.T) {
	srv, _ := startMock(t)
	helper := signIn(t, srv, "player")
	captain := signIn(t, srv, "demo")
	ctx := context.Background()

	res, err := helper.Abandon(ctx)
	if err != nil {
		t.Fatalf("abandon as a player: %v", err)
	}
	if res.Err != dzzzr.ErrNoPermission {
		t.Fatalf("abandon as a player: err %d, want %d", res.Err, dzzzr.ErrNoPermission)
	}

	res, err = captain.Abandon(ctx)
	if err != nil {
		t.Fatalf("abandon as the captain: %v", err)
	}
	if res.Err != 0 {
		t.Fatalf("abandon as the captain: err %d, want no result code", res.Err)
	}
	st, err := captain.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if st.Level == nil || st.Level.LevelNumber.Int() != 2 {
		t.Fatalf("abandoning level 1 should hand over level 2, got %+v", st.Level)
	}

	if res, err = captain.Abandon(ctx); err != nil {
		t.Fatalf("second abandon: %v", err)
	} else if res.Err != 0 {
		t.Fatalf("second abandon: err %d, want no result code", res.Err)
	}
	if res, err = captain.Abandon(ctx); err != nil {
		t.Fatalf("third abandon: %v", err)
	} else if res.Err != dzzzr.ErrAbandonsExhausted {
		t.Fatalf("third abandon: err %d, want %d", res.Err, dzzzr.ErrAbandonsExhausted)
	}
}

func TestBreaksAreLimitedToTwo(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		res, err := c.TakeBreak(ctx)
		if err != nil {
			t.Fatalf("break %d: %v", i, err)
		}
		if res.Err != 0 {
			t.Fatalf("break %d: err %d, want no result code", i, res.Err)
		}
	}
	res, err := c.TakeBreak(ctx)
	if err != nil {
		t.Fatalf("third break: %v", err)
	}
	if res.Err != dzzzr.ErrBreaksExhausted {
		t.Fatalf("third break: err %d, want %d", res.Err, dzzzr.ErrBreaksExhausted)
	}

	if res, err = c.StopBreak(ctx); err != nil {
		t.Fatalf("stop break: %v", err)
	} else if res.Err != 0 {
		t.Fatalf("stop break: err %d, want no result code", res.Err)
	}
}

func TestBreakPausesTheGame(t *testing.T) {
	srv, mock := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	if res, err := c.TakeBreak(ctx); err != nil || res.Err != 0 {
		t.Fatalf("take break: %v, err %v", err, res)
	}
	// Finishing the level starts the break instead of handing over level 2.
	send(t, c, "1")
	send(t, c, "2")
	send(t, c, "3")

	st, err := c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if st.OnBreak == "" || st.Countdown.Int() <= 0 {
		t.Fatalf("the team should be on a break, got onBreak %q countdown %d", st.OnBreak, st.Countdown.Int())
	}
	if st.Level != nil {
		t.Fatalf("no level is issued during a break, got %+v", st.Level)
	}
	if got := send(t, c, "4"); got != dzzzr.ErrTeamPaused {
		t.Fatalf("code during a break: err %d, want %d", got, dzzzr.ErrTeamPaused)
	}

	if res, err := c.StopBreak(ctx); err != nil || res.Err != 0 {
		t.Fatalf("stop break: %v, %v", err, res)
	}
	st, err = c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game after the break: %v", err)
	}
	if st.Level == nil || st.Level.LevelNumber.Int() != 2 {
		t.Fatalf("level 2 should be issued after the break, got %+v", st.Level)
	}
	mock.mu.Lock()
	breaks := mock.state.Breaks
	mock.mu.Unlock()
	if breaks != 1 {
		t.Fatalf("breaks taken %d, want 1", breaks)
	}
}

func TestHints(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	res, err := c.TakeHint(ctx, 1, 1)
	if err != nil {
		t.Fatalf("take hint: %v", err)
	}
	if res.Err != 0 {
		t.Fatalf("take hint: err %d, want no result code", res.Err)
	}

	res, err = c.TakeHintEarly(ctx, 1, 2)
	if err != nil {
		t.Fatalf("take hint early: %v", err)
	}
	if res.Err != dzzzr.ErrEarlyHintPenalty {
		t.Fatalf("take hint early: err %d, want %d", res.Err, dzzzr.ErrEarlyHintPenalty)
	}
	// The engine reports the minutes it charged only in the redirect.
	if res.Penalty <= 0 {
		t.Fatalf("take hint early: penalty %d, want the charged minutes", res.Penalty)
	}
	if !strings.Contains(res.Text, strconv.Itoa(res.Penalty)) {
		t.Fatalf("take hint early: text %q does not name the penalty", res.Text)
	}

	st, err := c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if st.Level.Hint1 != "Подсказка 1 к уровню 1" {
		t.Fatalf("hint 1 %q", st.Level.Hint1)
	}
	if st.Level.Hint2 != "Подсказка 2 к уровню 1" {
		t.Fatalf("hint 2 %q", st.Level.Hint2)
	}
}

func TestSendMessageToOrganizer(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	res, err := c.SendMessageToOrg(ctx, "Мы застряли")
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if res.Err != dzzzr.ErrMessageSent {
		t.Fatalf("send message: err %d, want %d", res.Err, dzzzr.ErrMessageSent)
	}

	st, err := c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	last := st.Messages[len(st.Messages)-1]
	if last.Text != "Мы застряли" || last.Destination != "от команды" {
		t.Fatalf("last message %+v", last)
	}
}

func TestChatMessages(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	before, err := c.GetMessages(ctx, "")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("the mock should seed the chat with organizer messages")
	}

	// API/postmessage.php acknowledges and delivers nothing, so a client
	// that uses it loses the player's message.
	if err := c.PostMessage(ctx, "это никуда не дойдёт"); err != nil {
		t.Fatalf("post message: %v", err)
	}
	swallowed, err := c.GetMessages(ctx, "")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(swallowed) != len(before) {
		t.Fatalf("postmessage.php delivered a message: %d, want %d", len(swallowed), len(before))
	}

	res, err := c.SendMessageToOrg(ctx, "Мы на первом коде")
	if err != nil {
		t.Fatalf("send message to org: %v", err)
	}
	if res.Err != dzzzr.ErrMessageSent {
		t.Fatalf("send message to org: err %d, want %d", res.Err, dzzzr.ErrMessageSent)
	}
	after, err := c.GetMessages(ctx, "")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("messages %d, want %d", len(after), len(before)+1)
	}
	last := after[len(after)-1]
	if last.Content != "Мы на первом коде" {
		t.Fatalf("last chat message %+v", last)
	}

	filtered, err := c.GetMessages(ctx, last.Time)
	if err != nil {
		t.Fatalf("get messages after a timestamp: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("messages after the last one: %d, want 0", len(filtered))
	}
}

func TestPostMessageRefusesEmptyText(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	if err := c.PostMessage(context.Background(), "   "); err == nil {
		t.Fatal("posting blank text should fail")
	}
}

func TestGamesList(t *testing.T) {
	srv, _ := startMock(t)
	// gamesList.php sits behind APIconst.php like the rest of the API, so it
	// needs a session and the captain's PIN.
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	all, err := c.GetGamesList(ctx, dzzzr.GamesListOptions{})
	if err != nil {
		t.Fatalf("games list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("games %d, want 2", len(all))
	}
	if all[0].ID.Int() != 4242 || all[0].Name != "Тестовая игра" {
		t.Fatalf("first game %+v", all[0])
	}
	if all[0].Image != nil {
		t.Fatalf("first game poster %v, want null", *all[0].Image)
	}
	if all[1].Image == nil || !strings.HasSuffix(*all[1].Image, "4243.jpg") {
		t.Fatalf("second game poster %v", all[1].Image)
	}

	upcoming, err := c.GetGamesList(ctx, dzzzr.GamesListOptions{NewOnly: true})
	if err != nil {
		t.Fatalf("upcoming games: %v", err)
	}
	if len(upcoming) != 1 || upcoming[0].Name != "Будущая игра" {
		t.Fatalf("upcoming games %+v", upcoming)
	}

	archive, err := c.GetGamesList(ctx, dzzzr.GamesListOptions{Archive: true})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if len(archive) != 0 {
		t.Fatalf("archive %+v, want nothing", archive)
	}
}

func TestStatLogAndLevelInfo(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	send(t, c, "1")

	stat, err := c.GetStat(ctx)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if len(stat.LevelNames) != 2 || stat.LevelNames[1] != "Итого" {
		t.Fatalf("stat level names %+v", stat.LevelNames)
	}
	if len(stat.Cells) != len(stat.LevelNames) {
		t.Fatalf("stat cells %d, level names %d", len(stat.Cells), len(stat.LevelNames))
	}
	if stat.Time == "" {
		t.Fatal("stat has no timestamp")
	}
	if rows := stat.Rows(); len(rows) != 2 {
		t.Fatalf("stat rows %+v", rows)
	}

	entries, err := c.GetLog(ctx)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("log entries %d, want the issued level and the accepted code", len(entries))
	}
	first := entries[0].Clean()
	if first.Event != "выдан уровень 1" {
		t.Fatalf("first log entry %+v", first)
	}
	if !strings.Contains(entries[0].Event, "<td>") {
		t.Fatalf("log entries should keep the engine's table cells: %q", entries[0].Event)
	}

	info, err := c.GetLevelInfo(ctx)
	if err != nil {
		t.Fatalf("level info: %v", err)
	}
	if len(info.Dangers) != 3 {
		t.Fatalf("dangers %+v, want three code slots", info.Dangers)
	}
	if !info.Dangers[0].Founded.Bool() {
		t.Fatal("the first code slot should be marked as found")
	}
	if len(info.Sectors) != 2 || info.Sectors[0] != "Гаражи" {
		t.Fatalf("sectors %+v", info.Sectors)
	}
	if len(info.DangersB) != 1 {
		t.Fatalf("bonus code slots %+v", info.DangersB)
	}
	if len(info.Bonus) != 1 || info.Bonus[0] != "10" {
		t.Fatalf("bonus minutes %+v", info.Bonus)
	}
	if info.Number.Int() != 1 {
		t.Fatalf("level number %d, want 1", info.Number.Int())
	}
	if info.CanAbandon.Int() != 1 || info.CanBreak.Int() != 1 {
		t.Fatalf("canAbandon %d canBreake %d, want 1 for the captain", info.CanAbandon.Int(), info.CanBreak.Int())
	}
	if info.Timer.Int() <= 0 {
		t.Fatalf("timer %d", info.Timer.Int())
	}
	if info.Clue1 == "" {
		t.Fatal("the first hint should be part of the level info")
	}
	if len(info.Skvoz) != 1 {
		t.Fatalf("skvoz levels %+v, want one", info.Skvoz)
	}

	bonus, err := c.GetBonusLevelInfo(ctx)
	if err != nil {
		t.Fatalf("bonus level info: %v", err)
	}
	if len(bonus.Skvoz) != 1 || bonus.Skvoz[0].ID.Int() != 4 {
		t.Fatalf("bonus level info %+v", bonus.Skvoz)
	}
}

func TestLevelInfoHidesControlsFromNonCaptain(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "player")

	info, err := c.GetLevelInfo(context.Background())
	if err != nil {
		t.Fatalf("level info: %v", err)
	}
	if info.CanAbandon.Int() != 0 || info.CanBreak.Int() != 0 {
		t.Fatalf("canAbandon %d canBreake %d, want both empty for a plain player", info.CanAbandon.Int(), info.CanBreak.Int())
	}
}

func TestGameAPIActionProxy(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")

	body := url.Values{"action": {"entcod"}, "cod": {"1"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/moscow/API/game.php?s="+c.Session(), strings.NewReader(body.Encode()))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("moscow_demo", testPin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post action: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Err int `json:"err"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Err != dzzzr.ErrCodeAcceptedPartial {
		t.Fatalf("proxied action: err %d, want %d", out.Err, dzzzr.ErrCodeAcceptedPartial)
	}
}

func TestNetworkOutageCode(t *testing.T) {
	oldDuration, oldSleep := networkDownDuration, networkDownSleep
	networkDownDuration, networkDownSleep = 300*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { networkDownDuration, networkDownSleep = oldDuration, oldSleep })

	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	start := time.Now()
	if _, err := c.SendCode(ctx, "PZDC"); err == nil {
		t.Fatal("PZDC should break the request")
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("PZDC held the connection for %s, want at least the outage", elapsed)
	}

	// The session stays dead for the rest of the outage, but by now it is
	// over, so a fresh request has to work again.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := c.GetGame(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the session never recovered from the outage")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestNetworkOutageRejectsRequestsWhileItLasts(t *testing.T) {
	oldDuration, oldSleep := networkDownDuration, networkDownSleep
	networkDownDuration, networkDownSleep = 2*time.Second, 20*time.Millisecond
	t.Cleanup(func() { networkDownDuration, networkDownSleep = oldDuration, oldSleep })

	srv, mock := startMock(t)
	c := signIn(t, srv, "demo")

	mock.mu.Lock()
	for _, sess := range mock.sessions {
		sess.NetDown = time.Now().Add(networkDownDuration)
	}
	mock.mu.Unlock()

	start := time.Now()
	_, err := c.GetGame(context.Background())
	if err == nil {
		t.Fatal("a request inside the outage should fail")
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("the mock answered in %s, it should stall first", elapsed)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the mock stalled for %s, it should give up after the short sleep", elapsed)
	}
}

func TestEngineStoppedAndBlockedTeam(t *testing.T) {
	srv, mock := startMock(t)
	c := signIn(t, srv, "demo")

	mock.mu.Lock()
	mock.state.EngineStopped = true
	mock.mu.Unlock()
	if got := send(t, c, "1"); got != dzzzr.ErrEngineStopped {
		t.Fatalf("stopped engine: err %d, want %d", got, dzzzr.ErrEngineStopped)
	}

	mock.mu.Lock()
	mock.state.EngineStopped = false
	mock.state.Blocked = true
	mock.mu.Unlock()
	if got := send(t, c, "1"); got != dzzzr.ErrTeamPaused {
		t.Fatalf("blocked team: err %d, want %d", got, dzzzr.ErrTeamPaused)
	}
}

func TestSelectLevelIgnoresStaleChoice(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	// llevel names a level the team is not on, so the engine drops the choice.
	if res, err := c.SelectLevel(ctx, 2, 3); err != nil {
		t.Fatalf("select level: %v", err)
	} else if res.Err != 0 {
		t.Fatalf("stale select level: err %d, want no result code", res.Err)
	}
	st, err := c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if st.Level.LevelNumber.Int() != 1 {
		t.Fatalf("the team should still be on level 1, got %d", st.Level.LevelNumber.Int())
	}

	if res, err := c.SelectLevel(ctx, 1, 3); err != nil {
		t.Fatalf("select level: %v", err)
	} else if res.Err != 0 {
		t.Fatalf("select level: err %d, want no result code", res.Err)
	}
	st, err = c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if st.Level == nil || st.Level.Question != "Последний код игры" {
		t.Fatalf("the team should be on the chosen level, got %+v", st.Level)
	}
}

func TestNextLevelHandsOverTheLine(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")
	ctx := context.Background()

	send(t, c, "1")
	send(t, c, "2")
	res, err := c.NextLevel(ctx, 1)
	if err != nil {
		t.Fatalf("next level: %v", err)
	}
	if res.Err != 0 {
		t.Fatalf("next level: err %d, want no result code", res.Err)
	}
	st, err := c.GetGame(ctx)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	// Two of three main codes are not enough, so the level stays.
	if st.Level.LevelNumber.Int() != 1 {
		t.Fatalf("level %d, want 1", st.Level.LevelNumber.Int())
	}
}

func TestLoginFailures(t *testing.T) {
	srv, _ := startMock(t)
	ctx := context.Background()

	// Wrong site account.
	c := dzzzr.New("moscow",
		dzzzr.WithBaseURL(srv.URL+"/moscow/"),
		dzzzr.WithCredentials(dzzzr.Credentials{Captain: "demo", Pin: testPin}),
	)
	resp, err := c.Login(ctx, "fail", "fail")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.Code.Int() != dzzzr.LoginWrongPassword {
		t.Fatalf("login code %d, want %d", resp.Code.Int(), dzzzr.LoginWrongPassword)
	}
	if c.Session() != "" {
		t.Fatalf("a failed login left a session %q", c.Session())
	}

	// No captain credentials at all: the site login is the identity, which is
	// how a Classic player signs in, since their team has no PIN.
	bare := dzzzr.New("moscow", dzzzr.WithBaseURL(srv.URL+"/moscow/"))
	resp, err = bare.Login(ctx, "demo", "secret")
	if err != nil {
		t.Fatalf("login without a captain: %v", err)
	}
	if !resp.OK() || bare.Session() == "" {
		t.Fatalf("login without a captain = %+v", resp)
	}

	// The engine does not validate the PIN, so a wrong one still signs in;
	// only a missing header is refused.
	badPin := dzzzr.New("moscow",
		dzzzr.WithBaseURL(srv.URL+"/moscow/"),
		dzzzr.WithCredentials(dzzzr.Credentials{Captain: "demo", Pin: "0000"}),
	)
	if _, err := badPin.Login(ctx, "demo", "secret"); err != nil {
		t.Fatalf("login with an unchecked PIN: %v", err)
	}

	// A request with no identity at all is refused by the wall.
	req0, err := http.NewRequest(http.MethodGet, srv.URL+"/moscow/API/login.php?login=demo&password=secret", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp0, err := http.DefaultClient.Do(req0)
	if err != nil {
		t.Fatalf("login without any header: %v", err)
	}
	defer func() { _ = resp0.Body.Close() }()
	if resp0.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login without any header: status %d, want 401", resp0.StatusCode)
	}

	// Missing parameters.
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/moscow/API/login.php?login=demo", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.SetBasicAuth("moscow_demo", testPin)
	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login without a password: %v", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	body, _ := io.ReadAll(httpResp.Body)
	if !strings.Contains(string(body), `"code" : "6"`) {
		t.Fatalf("login without a password: %s", body)
	}
}

func TestUnknownSessionIsRefused(t *testing.T) {
	srv, _ := startMock(t)
	c := dzzzr.New("moscow",
		dzzzr.WithBaseURL(srv.URL+"/moscow/"),
		dzzzr.WithCredentials(dzzzr.Credentials{Captain: "demo", Pin: testPin}),
		dzzzr.WithSession("NOSUCHSESSIONTOKENATALLXXXXXXXXX"),
	)
	_, err := c.GetGame(context.Background())
	if !dzzzr.IsAuthError(err) {
		t.Fatalf("get game with an unknown session: %v, want an auth error", err)
	}
}

func TestPostWithoutAPIOrRefererIsIgnored(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")

	body := url.Values{"action": {"entcod"}, "cod": {"1"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/moscow/go/?s="+c.Session(), strings.NewReader(body.Encode()))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("moscow_demo", testPin)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Query().Get("err") != "" {
		t.Fatalf("Location %q: the engine should ignore the POST", resp.Header.Get("Location"))
	}

	st, err := c.GetGame(context.Background())
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if st.Level.CodesFounded.Int() != 0 {
		t.Fatalf("codes found %d, want 0", st.Level.CodesFounded.Int())
	}
}

func TestGoStateCarriesTheErrorFromTheRedirect(t *testing.T) {
	srv, _ := startMock(t)
	c := signIn(t, srv, "demo")

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/moscow/go/?api=true&err=9&s="+c.Session(), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.SetBasicAuth("moscow_demo", testPin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	var st dzzzr.GameState
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("decode game state: %v\n%s", err, body)
	}
	if st.ErrNo.Int() != dzzzr.ErrCodeAcceptedNext {
		t.Fatalf("errNo %d, want %d", st.ErrNo.Int(), dzzzr.ErrCodeAcceptedNext)
	}
	if st.ErrText != dzzzr.ErrText(dzzzr.ErrCodeAcceptedNext) {
		t.Fatalf("errText %q", st.ErrText)
	}
}

func TestResetRebuildsTheGame(t *testing.T) {
	srv, mock := startMock(t)
	c := signIn(t, srv, "demo")
	send(t, c, "1")

	mock.reset()

	if _, err := c.GetGame(context.Background()); !dzzzr.IsAuthError(err) {
		t.Fatalf("get game after a reset: %v, want the session to be gone", err)
	}
	fresh := signIn(t, srv, "demo")
	st, err := fresh.GetGame(context.Background())
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if st.Level.CodesFounded.Int() != 0 {
		t.Fatalf("codes found %d after a reset, want 0", st.Level.CodesFounded.Int())
	}
}

func TestNormalizeCode(t *testing.T) {
	cases := map[string]string{
		"  код  ":  "КОД",
		"ёлка":     "ЕЛКА",
		"a b c":    "ABC",
		`<b>x</b>`: "X",
		"":         "",
	}
	for in, want := range cases {
		if got := normalizeCode(in); got != want {
			t.Errorf("normalizeCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatchCodeUsesSynonyms(t *testing.T) {
	codes := []string{"АЛЬФА#ALPHA", "БЕТА"}
	if got := matchCode("alpha", codes); got != "АЛЬФА" {
		t.Fatalf("matchCode(alpha) = %q, want АЛЬФА", got)
	}
	if got := matchCode("гамма", codes); got != "" {
		t.Fatalf("matchCode(гамма) = %q, want no match", got)
	}
}
