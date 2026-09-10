package agenttools_test

import (
	"context"
	"errors"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExtendedAdminCatalogVisibility(t *testing.T) {
	names := []string{"admin_team",
		"admin_create_game",
		"admin_update_game",
		"admin_delete_game",
		"admin_copy_game",
		"admin_create_level",
		"admin_update_level",
		"admin_delete_level",
		"admin_copy_levels",
		"admin_move_level",
		"admin_set_application",
		"admin_accept_all",
		"admin_generate_pins",
		"admin_add_team",
		"admin_remove_application",
		"admin_create_team",
		"admin_accept_level",
		"admin_give_and_accept_level",
		"admin_clear_level_codes",
		"admin_remove_level",
		"admin_clear_team_progress",
		"admin_plan_level",
		"admin_unplan_level",
		"admin_delete_corrections",
		"admin_toggle_engine",
		"admin_finish_game",
		"admin_set_event_time",
		"admin_delete_message",
		"admin_delete_all_messages"}
	for _, policy := range []agenttools.Policy{agenttools.PolicyReadonly, agenttools.PolicyApprove, agenttools.PolicyFull} {
		for _, credentials := range []bool{false, true} {
			for _, include := range []bool{false, true} {
				e := newFakeEngine()
				e.admin = credentials
				c := mustCatalog(t, e, agenttools.Options{Policy: policy, IncludeAdmin: include, Confirmer: agenttools.ConfirmerFunc(func(_ context.Context, _ agenttools.ConfirmRequest) (bool, error) { return false, nil })})
				visible := map[string]bool{}
				for _, tool := range c.Tools() {
					visible[tool.Name()] = true
				}
				for _, name := range names {
					want := credentials && include && (policy != agenttools.PolicyReadonly || name == "admin_team")
					if visible[name] != want {
						t.Errorf("%s policy=%s credentials=%v include=%v: visible=%v want=%v", name, policy, credentials, include, visible[name], want)
					}
				}
			}
		}
	}
}

type adminRecordingEngine struct {
	*fakeEngine
	method string
	args   []any
	err    error
}

func (f *adminRecordingEngine) AdminGetTeam(_ context.Context, gameID int, teamID int) (*dzzzr.AdminTeamInfo, error) {
	f.method = "AdminGetTeam"
	f.args = []any{gameID, teamID}
	return &dzzzr.AdminTeamInfo{ID: 51, Name: "Команда"}, f.err
}
func (f *adminRecordingEngine) AdminCreateGame(_ context.Context, params dzzzr.GameParams) (int, error) {
	f.method = "AdminCreateGame"
	f.args = []any{params}
	return 777, f.err
}
func (f *adminRecordingEngine) AdminUpdateGame(_ context.Context, gameID int, params dzzzr.GameParams) error {
	f.method = "AdminUpdateGame"
	f.args = []any{gameID, params}
	return f.err
}
func (f *adminRecordingEngine) AdminDeleteGame(_ context.Context, gameID int) error {
	f.method = "AdminDeleteGame"
	f.args = []any{gameID}
	return f.err
}
func (f *adminRecordingEngine) AdminCopyGame(_ context.Context, sourceGameID int, withLevels bool) (int, error) {
	f.method = "AdminCopyGame"
	f.args = []any{sourceGameID, withLevels}
	return 777, f.err
}
func (f *adminRecordingEngine) AdminCreateLevel(_ context.Context, gameID int, params dzzzr.LevelParams) (int, error) {
	f.method = "AdminCreateLevel"
	f.args = []any{gameID, params}
	return 777, f.err
}
func (f *adminRecordingEngine) AdminUpdateLevel(_ context.Context, gameID int, levelID int, params dzzzr.LevelParams) error {
	f.method = "AdminUpdateLevel"
	f.args = []any{gameID, levelID, params}
	return f.err
}
func (f *adminRecordingEngine) AdminDeleteLevel(_ context.Context, gameID int, levelID int) error {
	f.method = "AdminDeleteLevel"
	f.args = []any{gameID, levelID}
	return f.err
}
func (f *adminRecordingEngine) AdminCopyLevels(_ context.Context, gameID int, sourceGameID int) error {
	f.method = "AdminCopyLevels"
	f.args = []any{gameID, sourceGameID}
	return f.err
}
func (f *adminRecordingEngine) AdminSetApplication(_ context.Context, gameID int, teamID int, params dzzzr.ApplicationParams) error {
	f.method = "AdminSetApplication"
	f.args = []any{gameID, teamID, params}
	return f.err
}
func (f *adminRecordingEngine) AdminAcceptAllApplications(_ context.Context, gameID int) error {
	f.method = "AdminAcceptAllApplications"
	f.args = []any{gameID}
	return f.err
}
func (f *adminRecordingEngine) AdminGeneratePins(_ context.Context, gameID int) error {
	f.method = "AdminGeneratePins"
	f.args = []any{gameID}
	return f.err
}
func (f *adminRecordingEngine) AdminAddTeamToGame(_ context.Context, gameID int, teamID int) error {
	f.method = "AdminAddTeamToGame"
	f.args = []any{gameID, teamID}
	return f.err
}
func (f *adminRecordingEngine) AdminRemoveApplication(_ context.Context, gameID int, teamID int, status int) error {
	f.method = "AdminRemoveApplication"
	f.args = []any{gameID, teamID, status}
	return f.err
}
func (f *adminRecordingEngine) AdminCreateTeam(_ context.Context, name string, captain string) (int, error) {
	f.method = "AdminCreateTeam"
	f.args = []any{name, captain}
	return 777, f.err
}
func (f *adminRecordingEngine) AdminAcceptLevel(_ context.Context, gameID int, teamID int, level int, time string) error {
	f.method = "AdminAcceptLevel"
	f.args = []any{gameID, teamID, level, time}
	return f.err
}
func (f *adminRecordingEngine) AdminGiveAndAcceptLevel(_ context.Context, gameID int, teamID int, level int, time string) error {
	f.method = "AdminGiveAndAcceptLevel"
	f.args = []any{gameID, teamID, level, time}
	return f.err
}
func (f *adminRecordingEngine) AdminClearLevelCodes(_ context.Context, gameID int, teamID int, level int) error {
	f.method = "AdminClearLevelCodes"
	f.args = []any{gameID, teamID, level}
	return f.err
}
func (f *adminRecordingEngine) AdminRemoveLevel(_ context.Context, gameID int, teamID int, level int) error {
	f.method = "AdminRemoveLevel"
	f.args = []any{gameID, teamID, level}
	return f.err
}
func (f *adminRecordingEngine) AdminClearTeamProgress(_ context.Context, gameID int, teamID int) error {
	f.method = "AdminClearTeamProgress"
	f.args = []any{gameID, teamID}
	return f.err
}
func (f *adminRecordingEngine) AdminPlanLevel(_ context.Context, gameID int, teamID int, level int) error {
	f.method = "AdminPlanLevel"
	f.args = []any{gameID, teamID, level}
	return f.err
}
func (f *adminRecordingEngine) AdminUnplanLevel(_ context.Context, gameID int, teamID int, order int) error {
	f.method = "AdminUnplanLevel"
	f.args = []any{gameID, teamID, order}
	return f.err
}
func (f *adminRecordingEngine) AdminDeleteCorrections(_ context.Context, gameID int, teamID int, kind string) error {
	f.method = "AdminDeleteCorrections"
	f.args = []any{gameID, teamID, kind}
	return f.err
}
func (f *adminRecordingEngine) AdminToggleEngine(_ context.Context, gameID int) error {
	f.method = "AdminToggleEngine"
	f.args = []any{gameID}
	return f.err
}
func (f *adminRecordingEngine) AdminFinishGame(_ context.Context, gameID int) error {
	f.method = "AdminFinishGame"
	f.args = []any{gameID}
	return f.err
}
func (f *adminRecordingEngine) AdminSetEventTime(_ context.Context, gameID int, teamID int, level int, event int, time string) error {
	f.method = "AdminSetEventTime"
	f.args = []any{gameID, teamID, level, event, time}
	return f.err
}
func (f *adminRecordingEngine) AdminDeleteMessage(_ context.Context, gameID int, timestamp string) error {
	f.method = "AdminDeleteMessage"
	f.args = []any{gameID, timestamp}
	return f.err
}
func (f *adminRecordingEngine) AdminDeleteAllMessages(_ context.Context, gameID int) error {
	f.method = "AdminDeleteAllMessages"
	f.args = []any{gameID}
	return f.err
}
func TestAdminManagementDispatch(t *testing.T) {
	cases := []struct {
		name, method string
		args         map[string]any
		want         []any
		required     []string
		mutating     bool
	}{
		{name: "admin_team", method: "AdminGetTeam", args: map[string]any{"game_id": 41, "team_id": 51}, want: []any{41, 51}, required: []string{"game_id", "team_id"}, mutating: false},
		{name: "admin_create_game", method: "AdminCreateGame", args: map[string]any{"params": map[string]any{"name": "Игра", "date": "10.09.2026", "time": "21:00", "publish": false}}, want: []any{dzzzr.GameParams{Name: "Игра", Date: "10.09.2026", Time: "21:00", Publish: new(false)}}, required: []string{"params"}, mutating: true},
		{name: "admin_update_game", method: "AdminUpdateGame", args: map[string]any{"game_id": 41, "params": map[string]any{"name": "Игра", "date": "10.09.2026", "time": "21:00", "publish": false}}, want: []any{41, dzzzr.GameParams{Name: "Игра", Date: "10.09.2026", Time: "21:00", Publish: new(false)}}, required: []string{"game_id", "params"}, mutating: true},
		{name: "admin_delete_game", method: "AdminDeleteGame", args: map[string]any{"game_id": 41}, want: []any{41}, required: []string{"game_id"}, mutating: true},
		{name: "admin_copy_game", method: "AdminCopyGame", args: map[string]any{"source_game_id": 42, "with_levels": true}, want: []any{42, true}, required: []string{"source_game_id"}, mutating: true},
		{name: "admin_create_level", method: "AdminCreateLevel", args: map[string]any{"game_id": 41, "params": map[string]any{"title": "Уровень", "question": "Текст\nвторая строка", "codes": []any{map[string]any{"code": "КОД", "danger": "1", "sector": 1}}, "bonus_codes": []any{}, "penalty": 0, "publish": false}}, want: []any{41, dzzzr.LevelParams{Title: "Уровень", Question: "Текст\nвторая строка", Codes: []dzzzr.LevelCode{{Code: "КОД", Danger: "1", Sector: 1}}, BonusCodes: []dzzzr.BonusCode{}, Penalty: new(0), Publish: new(false)}}, required: []string{"game_id", "params"}, mutating: true},
		{name: "admin_update_level", method: "AdminUpdateLevel", args: map[string]any{"game_id": 41, "level_id": 91, "params": map[string]any{"title": "Уровень", "question": "Текст\nвторая строка", "codes": []any{map[string]any{"code": "КОД", "danger": "1", "sector": 1}}, "bonus_codes": []any{}, "penalty": 0, "publish": false}}, want: []any{41, 91, dzzzr.LevelParams{Title: "Уровень", Question: "Текст\nвторая строка", Codes: []dzzzr.LevelCode{{Code: "КОД", Danger: "1", Sector: 1}}, BonusCodes: []dzzzr.BonusCode{}, Penalty: new(0), Publish: new(false)}}, required: []string{"game_id", "level_id", "params"}, mutating: true},
		{name: "admin_delete_level", method: "AdminDeleteLevel", args: map[string]any{"game_id": 41, "level_id": 91}, want: []any{41, 91}, required: []string{"game_id", "level_id"}, mutating: true},
		{name: "admin_copy_levels", method: "AdminCopyLevels", args: map[string]any{"game_id": 41, "source_game_id": 42}, want: []any{41, 42}, required: []string{"game_id", "source_game_id"}, mutating: true},
		{name: "admin_set_application", method: "AdminSetApplication", args: map[string]any{"game_id": 41, "team_id": 51, "params": map[string]any{"status": 0, "points": 0, "novice": false, "new_pin": true, "comment": "Причина"}}, want: []any{41, 51, dzzzr.ApplicationParams{Status: new(0), Points: new(0), Novice: new(false), NewPin: true, Comment: "Причина"}}, required: []string{"game_id", "team_id", "params"}, mutating: true},
		{name: "admin_accept_all", method: "AdminAcceptAllApplications", args: map[string]any{"game_id": 41}, want: []any{41}, required: []string{"game_id"}, mutating: true},
		{name: "admin_generate_pins", method: "AdminGeneratePins", args: map[string]any{"game_id": 41}, want: []any{41}, required: []string{"game_id"}, mutating: true},
		{name: "admin_add_team", method: "AdminAddTeamToGame", args: map[string]any{"game_id": 41, "team_id": 51}, want: []any{41, 51}, required: []string{"game_id", "team_id"}, mutating: true},
		{name: "admin_remove_application", method: "AdminRemoveApplication", args: map[string]any{"game_id": 41, "team_id": 51, "status": 0}, want: []any{41, 51, 0}, required: []string{"game_id", "team_id", "status"}, mutating: true},
		{name: "admin_create_team", method: "AdminCreateTeam", args: map[string]any{"name": "Команда", "captain": "captain_login"}, want: []any{"Команда", "captain_login"}, required: []string{"name", "captain"}, mutating: true},
		{name: "admin_accept_level", method: "AdminAcceptLevel", args: map[string]any{"game_id": 41, "team_id": 51, "level": 3, "time": "2026-09-09 22:01:02"}, want: []any{41, 51, 3, "2026-09-09 22:01:02"}, required: []string{"game_id", "team_id", "level"}, mutating: true},
		{name: "admin_give_and_accept_level", method: "AdminGiveAndAcceptLevel", args: map[string]any{"game_id": 41, "team_id": 51, "level": 3, "time": "2026-09-09 22:01:02"}, want: []any{41, 51, 3, "2026-09-09 22:01:02"}, required: []string{"game_id", "team_id", "level"}, mutating: true},
		{name: "admin_clear_level_codes", method: "AdminClearLevelCodes", args: map[string]any{"game_id": 41, "team_id": 51, "level": 3}, want: []any{41, 51, 3}, required: []string{"game_id", "team_id", "level"}, mutating: true},
		{name: "admin_remove_level", method: "AdminRemoveLevel", args: map[string]any{"game_id": 41, "team_id": 51, "level": 3}, want: []any{41, 51, 3}, required: []string{"game_id", "team_id", "level"}, mutating: true},
		{name: "admin_clear_team_progress", method: "AdminClearTeamProgress", args: map[string]any{"game_id": 41, "team_id": 51}, want: []any{41, 51}, required: []string{"game_id", "team_id"}, mutating: true},
		{name: "admin_plan_level", method: "AdminPlanLevel", args: map[string]any{"game_id": 41, "team_id": 51, "level": 3}, want: []any{41, 51, 3}, required: []string{"game_id", "team_id", "level"}, mutating: true},
		{name: "admin_unplan_level", method: "AdminUnplanLevel", args: map[string]any{"game_id": 41, "team_id": 51, "order": 2}, want: []any{41, 51, 2}, required: []string{"game_id", "team_id", "order"}, mutating: true},
		{name: "admin_delete_corrections", method: "AdminDeleteCorrections", args: map[string]any{"game_id": 41, "team_id": 51, "kind": "bonus"}, want: []any{41, 51, "bonus"}, required: []string{"game_id", "team_id", "kind"}, mutating: true},
		{name: "admin_toggle_engine", method: "AdminToggleEngine", args: map[string]any{"game_id": 41}, want: []any{41}, required: []string{"game_id"}, mutating: true},
		{name: "admin_finish_game", method: "AdminFinishGame", args: map[string]any{"game_id": 41}, want: []any{41}, required: []string{"game_id"}, mutating: true},
		{name: "admin_set_event_time", method: "AdminSetEventTime", args: map[string]any{"game_id": 41, "team_id": 51, "level": 3, "event": 2, "time": "2026-09-09 22:01:02"}, want: []any{41, 51, 3, 2, "2026-09-09 22:01:02"}, required: []string{"game_id", "team_id", "level", "event", "time"}, mutating: true},
		{name: "admin_delete_message", method: "AdminDeleteMessage", args: map[string]any{"game_id": 41, "timestamp": "2026-09-09 21:00:00"}, want: []any{41, "2026-09-09 21:00:00"}, required: []string{"game_id", "timestamp"}, mutating: true},
		{name: "admin_delete_all_messages", method: "AdminDeleteAllMessages", args: map[string]any{"game_id": 41}, want: []any{41}, required: []string{"game_id"}, mutating: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e := &adminRecordingEngine{fakeEngine: newFakeEngine()}
			c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true})
			tool, ok := c.Lookup(tt.name)
			if !ok {
				t.Fatal("missing tool")
			}
			result := tool.Execute(t.Context(), tt.args)
			if result.IsError {
				t.Fatal(result.Content)
			}
			if e.method != tt.method || !reflect.DeepEqual(e.args, tt.want) {
				t.Fatalf("got %s %#v; want %s %#v", e.method, e.args, tt.method, tt.want)
			}
			if tool.Mutating() != tt.mutating {
				t.Fatal("wrong mutation flag")
			}
			for _, key := range tt.required {
				e.method = ""
				args := maps.Clone(tt.args)
				delete(args, key)
				if got := tool.Execute(t.Context(), args); !got.IsError || e.method != "" {
					t.Fatalf("missing %s dispatched: %+v", key, got)
				}
			}
			e.err = errors.New("engine unavailable")
			if got := tool.Execute(t.Context(), tt.args); !got.IsError || !strings.Contains(got.Content, "engine unavailable") {
				t.Fatalf("lost error: %+v", got)
			}
			if !tt.mutating {
				return
			}
			e.err = nil
			for _, policy := range []agenttools.Policy{agenttools.PolicyReadonly, agenttools.PolicyApprove} {
				e.method = ""
				denied := mustCatalog(t, e, agenttools.Options{Policy: policy, IncludeAdmin: true, Confirmer: agenttools.ConfirmerFunc(func(context.Context, agenttools.ConfirmRequest) (bool, error) { return false, nil })})
				dt, _ := denied.Lookup(tt.name)
				if got := dt.Execute(t.Context(), tt.args); !got.IsError || e.method != "" {
					t.Fatalf("unauthorized call under %s: %+v", policy, got)
				}
			}
			confirmed := false
			approved := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyApprove, IncludeAdmin: true, ReadCacheTTL: time.Minute, Confirmer: agenttools.ConfirmerFunc(func(_ context.Context, req agenttools.ConfirmRequest) (bool, error) {
				confirmed = req.Tool == tt.name && reflect.DeepEqual(req.Args, tt.args)
				return true, nil
			})})
			read, _ := approved.Lookup("admin_games")
			read.Execute(t.Context(), nil)
			read.Execute(t.Context(), nil)
			before := e.calls["AdminListGames"]
			at, _ := approved.Lookup(tt.name)
			if got := at.Execute(t.Context(), tt.args); got.IsError || !confirmed {
				t.Fatalf("approval failed: %+v", got)
			}
			read.Execute(t.Context(), nil)
			if e.calls["AdminListGames"] != before+1 {
				t.Fatal("mutation did not invalidate cached reads")
			}
		})
	}
}

func TestAdminManagementRejectsInvalidArguments(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
	}{
		{"admin_delete_game", map[string]any{"game_id": 1.5}},
		{"admin_delete_game", map[string]any{"game_id": true}},
		{"admin_delete_game", map[string]any{"game_id": -1}},
		{"admin_delete_game", map[string]any{"game_id": "41.5"}},
		{"admin_copy_game", map[string]any{"source_game_id": 41, "with_levels": "maybe"}},
		{"admin_update_game", map[string]any{"game_id": 41, "params": map[string]any{"publsih": true}}},
		{"admin_update_game", map[string]any{"game_id": 41, "params": map[string]any{}}},
		{"admin_update_game", map[string]any{"game_id": 41, "params": map[string]any{"publish": "false"}}},
		{"admin_update_level", map[string]any{"game_id": 41, "level_id": 91, "params": map[string]any{"codes": []any{map[string]any{"cod": "x"}}}}},
		{"admin_set_application", map[string]any{"game_id": 41, "team_id": 51, "params": map[string]any{"status": 6}}},
		{"admin_set_event_time", map[string]any{"game_id": 41, "team_id": 51, "level": 3, "event": 9, "time": "2026-09-09 21:00:00"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e := &adminRecordingEngine{fakeEngine: newFakeEngine()}
			c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true})
			tool, ok := c.Lookup(tt.name)
			if !ok {
				t.Fatal("missing tool")
			}
			if got := tool.Execute(t.Context(), tt.args); !got.IsError || e.method != "" {
				t.Fatalf("invalid arguments reached engine: %+v, method=%s", got, e.method)
			}
		})
	}
}

// movingEngine reports a level's order from a script, so a test can model the
// engine both moving a level and silently refusing to.
type movingEngine struct {
	*fakeEngine
	orders []int // order returned by successive AdminListLevels calls
	calls  int
	moved  bool
}

func (m *movingEngine) AdminListLevels(context.Context, int) ([]dzzzr.AdminLevel, error) {
	order := m.orders[min(m.calls, len(m.orders)-1)]
	m.calls++
	return []dzzzr.AdminLevel{{ID: 901, Order: order, Title: "Гаражи"}}, nil
}

func (m *movingEngine) AdminMoveLevel(context.Context, int, bool) error {
	m.moved = true
	return nil
}

// TestAdminMoveLevelToolReportsTheOutcome covers the tool's own check. The
// engine answers a refused move exactly like an accepted one, so a tool that
// just returned done:true could not tell an agent which had happened.
func TestAdminMoveLevelToolReportsTheOutcome(t *testing.T) {
	for _, tc := range []struct {
		name      string
		orders    []int
		wantMoved bool
	}{
		{"moved", []int{2, 1}, true},
		{"refused", []int{2, 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &movingEngine{fakeEngine: newFakeEngine(), orders: tc.orders}
			c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true})
			tool, ok := c.Lookup("admin_move_level")
			if !ok {
				t.Fatal("missing tool")
			}
			result := tool.Execute(t.Context(), map[string]any{"game_id": 41, "level_id": 901, "up": true})
			if result.IsError {
				t.Fatal(result.Content)
			}
			if !e.moved {
				t.Fatal("движок не получил запрос на перемещение")
			}
			if !strings.Contains(result.Content, `"moved":`+map[bool]string{true: "true", false: "false"}[tc.wantMoved]) {
				t.Fatalf("результат не сообщает исход: %s", result.Content)
			}
		})
	}
}
