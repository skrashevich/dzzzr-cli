package dzzzr_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func monitorServer(t *testing.T) *adminServer {
	t.Helper()
	return newAdminServer(t, map[string]string{
		"path:gmAdmin.php": "admin_gm.html",
		"path:lookLog.php": "admin_looklog.html",
		"path:gmcht.php":   "admin_gmcht.html",
	})
}

func TestAdminMonitor(t *testing.T) {
	s := monitorServer(t)
	st, err := s.client.AdminMonitor(context.Background(), 4242)
	if err != nil {
		t.Fatal(err)
	}
	if s.paths[0] != "/moscow/admin/gmAdmin.php" || s.gets[0].Get("finished") != "4242" {
		t.Errorf("request = %s %v", s.paths[0], s.gets[0])
	}
	if st.GameID != 4242 || st.GameName == "" {
		t.Errorf("state = %+v", st)
	}
	if len(st.Teams) != 2 || st.Teams[0].ID != 5150 || st.Teams[0].Name != "MockTeam" || st.Teams[1].ID != 5151 {
		t.Errorf("teams = %+v", st.Teams)
	}
	if len(st.Levels) != 3 || st.Levels[0].Order != 1 || st.Levels[2].Title != "3. Сквозной бонус" {
		t.Errorf("levels = %+v", st.Levels)
	}
	if st.EngineStopped {
		t.Error("engine must be running when the button offers to stop it")
	}
}

func TestAdminGetLog(t *testing.T) {
	s := monitorServer(t)
	entries, next, err := s.client.AdminGetLog(context.Background(), 4242, "2026-09-08 22:30:00")
	if err != nil {
		t.Fatal(err)
	}
	if s.gets[0].Get("gmid") != "4242" || s.gets[0].Get("lastTime") != "2026-09-08 22:30:00" {
		t.Errorf("query = %v", s.gets[0])
	}
	if next != "2026-09-08 22:31:00" {
		t.Errorf("next = %q", next)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	if e := entries[0]; e.TeamID != 5150 || e.Level != 1 || e.Event != dzzzr.LogEventCodeAccepted || e.Comment != "КОД-1" || e.Time != "22:30:15" {
		t.Errorf("entries[0] = %+v", e)
	}
	if e := entries[1]; e.TeamID != 5150 || e.Level != 2 || e.Event != dzzzr.LogEventLevelIssued || e.Comment != "" || e.Time != "22:30:16" {
		t.Errorf("entries[1] = %+v", e)
	}
	if e := entries[2]; e.TeamID != 5151 || e.Event != dzzzr.LogEventWrongCode || e.Comment != "XYZ" {
		t.Errorf("entries[2] = %+v", e)
	}
}

// TestAdminGetLogSectionAccessDenied mirrors TestAdminSectionAccessDenied for
// the log endpoint: valid credentials, HTTP 200, but this account is not the
// game's organizer, so lookLog.php answers with "Вы не имеете доступа". That
// must surface as AuthAdminScope, not AuthAdmin, so the caller is not told
// to re-check an already-correct login/password.
func TestAdminGetLogSectionAccessDenied(t *testing.T) {
	s := newAdminServer(t, map[string]string{"path:lookLog.php": "admin_looklog_no_access.html"})
	_, _, err := s.client.AdminGetLog(context.Background(), 1383, "")
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthAdminScope {
		t.Fatalf("err = %v, want AuthAdminScope", err)
	}
}

func TestAdminMonitorActions(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		call func(c *dzzzr.Client) error
		want map[string]string
	}{
		{"give level", func(c *dzzzr.Client) error { return c.AdminGiveLevel(ctx, 4242, 5150, 2, "2026-09-08 22:40:00") },
			map[string]string{"action": "newlevel", "team": "5150", "level": "2", "time": "2026-09-08 22:40:00", "finished": "4242"}},
		{"accept level", func(c *dzzzr.Client) error { return c.AdminAcceptLevel(ctx, 4242, 5150, 2, "") },
			map[string]string{"action": "acceptLevel", "team": "5150", "level": "2"}},
		{"give and accept", func(c *dzzzr.Client) error { return c.AdminGiveAndAcceptLevel(ctx, 4242, 5150, 3, "") },
			map[string]string{"action": "newLevelAndAccept", "level": "3"}},
		{"accept code", func(c *dzzzr.Client) error { return c.AdminAcceptCode(ctx, 4242, 5150, 1, "КОД1", "") },
			map[string]string{"action": "code", "code": "КОД1", "level": "1"}},
		{"clear codes", func(c *dzzzr.Client) error { return c.AdminClearLevelCodes(ctx, 4242, 5150, 1) },
			map[string]string{"action": "delLevel", "team": "5150", "level": "1"}},
		{"remove level", func(c *dzzzr.Client) error { return c.AdminRemoveLevel(ctx, 4242, 0, 3) },
			map[string]string{"action": "removeLevel", "team": "0", "level": "3"}},
		{"clear progress", func(c *dzzzr.Client) error { return c.AdminClearTeamProgress(ctx, 4242, 5151) },
			map[string]string{"action": "delstat", "team": "5151"}},
		{"plan level", func(c *dzzzr.Client) error { return c.AdminPlanLevel(ctx, 4242, 5150, 2) },
			map[string]string{"action": "line", "level": "2"}},
		{"unplan level", func(c *dzzzr.Client) error { return c.AdminUnplanLevel(ctx, 4242, 5150, 1) },
			map[string]string{"action": "del_Line", "op": "1"}},
		{"bonus", func(c *dzzzr.Client) error {
			return c.AdminAddCorrection(ctx, 4242, 5150, "bonus", 10, "за красоту")
		},
			map[string]string{"action": "bonus", "type": "1", "mins": "10", "comment": "за красоту", "game": "4242"}},
		{"penalty", func(c *dzzzr.Client) error {
			return c.AdminAddCorrection(ctx, 4242, 5150, "penalty", -15, "опоздание")
		},
			map[string]string{"action": "bonus", "type": "-1", "mins": "15"}},
		{"delete corrections", func(c *dzzzr.Client) error { return c.AdminDeleteCorrections(ctx, 4242, 5150, "bonus") },
			map[string]string{"action": "delBonus", "type": "1"}},
		{"block", func(c *dzzzr.Client) error { return c.AdminBlockTeam(ctx, 4242, 5150) },
			map[string]string{"action": "stop", "team": "5150"}},
		{"unblock", func(c *dzzzr.Client) error { return c.AdminUnblockTeam(ctx, 4242, 5150) },
			map[string]string{"action": "start", "team": "5150"}},
		{"engine toggle", func(c *dzzzr.Client) error { return c.AdminToggleEngine(ctx, 4242) },
			map[string]string{"action": "switchRobot", "finished": "4242"}},
		{"finish", func(c *dzzzr.Client) error { return c.AdminFinishGame(ctx, 4242) },
			map[string]string{"action": "finishGame", "game": "4242"}},
		{"set time", func(c *dzzzr.Client) error {
			return c.AdminSetEventTime(ctx, 4242, 5150, 1, dzzzr.LogEventLevelIssued, "2026-09-08 21:00:00")
		},
			map[string]string{"action": "chtime", "event": "1", "time": "2026-09-08 21:00:00"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := monitorServer(t)
			if err := tc.call(s.client); err != nil {
				t.Fatal(err)
			}
			if s.paths[0] != "/moscow/admin/gmAdmin.php" {
				t.Errorf("path = %s", s.paths[0])
			}
			f := s.lastPost()
			for k, want := range tc.want {
				if got := f.Get(k); got != want {
					t.Errorf("form[%q] = %q, want %q", k, got, want)
				}
			}
		})
	}
	s := monitorServer(t)
	if err := s.client.AdminAddCorrection(ctx, 4242, 5150, "нечто", 5, ""); err == nil {
		t.Error("unknown correction kind must fail")
	}
	if err := s.client.AdminDeleteCorrections(ctx, 4242, 5150, "нечто"); err == nil {
		t.Error("unknown correction kind must fail")
	}
	// The engine has no way to set the state, only to flip it, so
	// AdminSetEngine reads the live screen first and acts only when the state
	// differs. The monitor fixture shows a running engine.
	s = monitorServer(t)
	if err := s.client.AdminSetEngine(ctx, 4242, true); err != nil {
		t.Fatal(err)
	}
	if len(s.posts) != 0 {
		t.Errorf("asking for the state it is already in must post nothing, got %v", s.posts)
	}
	if err := s.client.AdminSetEngine(ctx, 4242, false); err != nil {
		t.Fatal(err)
	}
	f := s.lastPost()
	if f.Get("action") != "switchRobot" || f.Has("robot") {
		t.Errorf("stopping the engine = %v; the engine reads no parameter here", f)
	}
}

func TestAdminMessages(t *testing.T) {
	s := monitorServer(t)
	msgs, err := s.client.AdminListMessages(context.Background(), 4242, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.paths[0] != "/moscow/admin/gmcht.php" || s.gets[0].Get("gm") != "4242" || s.gets[0].Has("team") {
		t.Errorf("request = %s %v", s.paths[0], s.gets[0])
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %+v", msgs)
	}
	m0 := msgs[0]
	if m0.Time != "22:05:10" || m0.Addressee != "MockTeam" || m0.Content != "Мы застряли на уровне 2" || !m0.FromTeam || m0.Timestamp != "2026-09-08 22:05:10" {
		t.Errorf("messages[0] = %+v", m0)
	}
	m1 := msgs[1]
	if m1.Addressee != "всем" || m1.FromTeam || m1.Timestamp != "2026-09-08 22:03:00" {
		t.Errorf("messages[1] = %+v", m1)
	}
}

func TestAdminSendAndDeleteMessage(t *testing.T) {
	ctx := context.Background()
	s := monitorServer(t)
	if err := s.client.AdminSendMessage(ctx, 4242, 5150, "Ждите", "2026-09-08 23:00:00", true); err != nil {
		t.Fatal(err)
	}
	f := s.lastPost()
	if f.Get("action") != "addMessage" || f.Get("gm") != "4242" || f.Get("team") != "5150" || f.Get("content") != "Ждите" || f.Get("showafter") != "2026-09-08 23:00:00" || f.Get("important") != "on" {
		t.Errorf("form = %v", f)
	}
	if err := s.client.AdminSendMessage(ctx, 4242, 0, "Всем", "", false); err != nil {
		t.Fatal(err)
	}
	if f := s.lastPost(); f.Get("team") != "0" || f.Has("important") || f.Get("showafter") != "" {
		t.Errorf("form = %v", f)
	}
	if err := s.client.AdminDeleteMessage(ctx, 4242, "2026-09-08 22:03:00"); err != nil {
		t.Fatal(err)
	}
	if f := s.lastPost(); f.Get("action") != "delMessage" || f.Get("tm") != "2026-09-08 22:03:00" {
		t.Errorf("form = %v", f)
	}
	if err := s.client.AdminDeleteAllMessages(ctx, 4242); err != nil {
		t.Fatal(err)
	}
	if f := s.lastPost(); f.Get("action") != "delAllMessages" {
		t.Errorf("form = %v", f)
	}
}

// TestAdminMessagesSectionAccessDenied covers the engine's real reply,
// measured against classic.dzzzr.ru on 2026-09-09: an organizer account
// that is not this game's assigned organizer gets a 200 from gmcht.php
// whose entire body is "<login> Вы не имеете доступа к этому разделу. (0)"
// for both the GET (message list) and the POST (send). Before the shared
// checkAdminScope check, AdminListMessages silently parsed that denial text
// as zero rows (indistinguishable from an empty chat) and AdminSendMessage
// returned nil, reporting a message as sent when the engine had refused it.
func TestAdminMessagesSectionAccessDenied(t *testing.T) {
	denied := fixture(t, "admin_gmcht_no_access.html")
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantBasic(t, r, "org", "secret")
		w.Header().Set("Content-Type", "text/html; charset=windows-1251")
		_, _ = w.Write(denied)
	})
	client := newTestClient(t, h, dzzzr.WithAdminCredentials("org", "secret"))

	if _, err := client.AdminListMessages(context.Background(), 1383, 0); dzzzr.AuthErrorKindOf(err) != dzzzr.AuthAdminScope {
		t.Fatalf("AdminListMessages err = %v, want AuthAdminScope (not a silent empty list)", err)
	}
	if err := client.AdminSendMessage(context.Background(), 1383, 0, "hi", "", false); dzzzr.AuthErrorKindOf(err) != dzzzr.AuthAdminScope {
		t.Fatalf("AdminSendMessage err = %v, want AuthAdminScope (not a silent success)", err)
	}
}

func TestAdminListMessagesFilterByTeam(t *testing.T) {
	s := monitorServer(t)
	if _, err := s.client.AdminListMessages(context.Background(), 4242, 5150); err != nil {
		t.Fatal(err)
	}
	if s.gets[0].Get("team") != "5150" {
		t.Errorf("query = %v", s.gets[0])
	}
}
