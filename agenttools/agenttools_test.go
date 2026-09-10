package agenttools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// fakeEngine counts calls and returns canned state, so a test can prove what
// the catalog did rather than what it meant to do.
type fakeEngine struct {
	agenttools.Engine
	calls     map[string]int
	level     int
	admin     bool
	failNext  error
	lastCode  string
	lastLevel int
	lastHint  int
	lastGame  int
	penalty   int
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{calls: map[string]int{}, level: 3, admin: true}
}

func (f *fakeEngine) note(name string) { f.calls[name]++ }

func (f *fakeEngine) GetGame(context.Context) (*dzzzr.GameState, error) {
	f.note("GetGame")
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return nil, err
	}
	return &dzzzr.GameState{
		GameName: "Тест", GameID: 4242, TeamName: "MockTeam", CurrentTime: "2026-09-08 22:00:00",
		TotalLevels: 12, CanControl: true, ErrNo: 9,
		Level: &dzzzr.Level{
			LevelNumber: dzzzr.FlexInt(f.level), Question: "<b>Найдите</b> код", TotalCodes: 3, CodesFounded: 1,
			Hint1: "Подсказка", TM: 125,
			Spoilers: []dzzzr.Spoiler{{Number: 1, Penalty: 5}},
		},
		Messages: []dzzzr.Message{{Timestamp: "22:01:10", Destination: "всем", Text: "Привет &amp; удачи"}},
	}, nil
}

func (f *fakeEngine) GetLevelInfo(context.Context) (*dzzzr.LevelInfo, error) {
	f.note("GetLevelInfo")
	return &dzzzr.LevelInfo{
		Number: dzzzr.FlexInt(f.level), Timer: 125, CanAbandon: 3, CanBreak: 3, CodeCount: 2,
		Dangers: []dzzzr.Danger{{Danger: "1", Founded: true, Sector: "1"}, {Danger: "2", Sector: "2"}},
		Sectors: []string{"Гаражи", "Пустырь"},
		Clue1:   "Подсказка 1",
	}, nil
}

func (f *fakeEngine) GetBonusLevelInfo(context.Context) (*dzzzr.LevelInfo, error) {
	f.note("GetBonusLevelInfo")
	return &dzzzr.LevelInfo{Number: 7}, nil
}

func (f *fakeEngine) GetStat(context.Context) (*dzzzr.TeamStat, error) {
	f.note("GetStat")
	return &dzzzr.TeamStat{LevelNames: []string{"1"}, Cells: []string{"<td>00:12:40</td>"}, Time: "22:10"}, nil
}

func (f *fakeEngine) GetLog(context.Context) ([]dzzzr.LogEntry, error) {
	f.note("GetLog")
	return []dzzzr.LogEntry{{Time: "<td>21:00:05</td>", Event: "<td>выдан уровень</td>", Data: "1&nbsp;"}}, nil
}

func (f *fakeEngine) GetMessages(_ context.Context, after string) ([]dzzzr.ChatMessage, error) {
	f.note("GetMessages:" + after)
	return []dzzzr.ChatMessage{{Who: "организатор", Time: "22:00", Content: "Текст &quot;в кавычках&quot;"}}, nil
}

func (f *fakeEngine) GetGamesList(_ context.Context, opts dzzzr.GamesListOptions) ([]dzzzr.GameInfo, error) {
	f.note("GetGamesList")
	if opts.NewOnly {
		f.note("GetGamesList:new")
	}
	return []dzzzr.GameInfo{{ID: 4242, Name: "Тест", Number: "112", Date: "2026-09-08 21:00:00", IsLinear: true}}, nil
}

func (f *fakeEngine) SendCode(_ context.Context, code string) (*dzzzr.ActionResult, error) {
	f.note("SendCode")
	f.lastCode = code
	return &dzzzr.ActionResult{Err: 9, Text: dzzzr.ErrText(9)}, nil
}

func (f *fakeEngine) SendBonusCode(_ context.Context, level int, code string) (*dzzzr.ActionResult, error) {
	f.note("SendBonusCode")
	f.lastLevel, f.lastCode = level, code
	return &dzzzr.ActionResult{Err: 36, Text: dzzzr.ErrText(36)}, nil
}

func (f *fakeEngine) SendSpoilerCode(_ context.Context, level int, code string) (*dzzzr.ActionResult, error) {
	f.note("SendSpoilerCode")
	f.lastLevel, f.lastCode = level, code
	return &dzzzr.ActionResult{Err: 55, Text: dzzzr.ErrText(55)}, nil
}

func (f *fakeEngine) TakeHint(_ context.Context, level, hint int) (*dzzzr.ActionResult, error) {
	f.note("TakeHint")
	f.lastLevel = level
	return &dzzzr.ActionResult{Err: 0}, nil
}

func (f *fakeEngine) TakeHintEarly(_ context.Context, level, hint int) (*dzzzr.ActionResult, error) {
	f.note("TakeHintEarly")
	f.lastLevel = level
	f.lastHint = hint
	return &dzzzr.ActionResult{Err: 42, Text: dzzzr.ErrText(42), Penalty: f.penalty}, nil
}

func (f *fakeEngine) Abandon(context.Context) (*dzzzr.ActionResult, error) {
	f.note("Abandon")
	return &dzzzr.ActionResult{Err: 0}, nil
}

func (f *fakeEngine) TakeBreak(context.Context) (*dzzzr.ActionResult, error) {
	f.note("TakeBreak")
	return &dzzzr.ActionResult{Err: 0}, nil
}

func (f *fakeEngine) StopBreak(context.Context) (*dzzzr.ActionResult, error) {
	f.note("StopBreak")
	return &dzzzr.ActionResult{Err: 0}, nil
}

func (f *fakeEngine) NextLevel(_ context.Context, level int) (*dzzzr.ActionResult, error) {
	f.note("NextLevel")
	f.lastLevel = level
	return &dzzzr.ActionResult{Err: 0}, nil
}

func (f *fakeEngine) SelectLevel(_ context.Context, current, next int) (*dzzzr.ActionResult, error) {
	f.note("SelectLevel")
	f.lastLevel = next
	return &dzzzr.ActionResult{Err: 0}, nil
}

func (f *fakeEngine) PostMessage(_ context.Context, text string) error {
	f.note("PostMessage")
	f.lastCode = text
	return nil
}

func (f *fakeEngine) SendMessageToOrg(_ context.Context, text string) (*dzzzr.ActionResult, error) {
	f.note("SendMessageToOrg")
	return &dzzzr.ActionResult{Err: 33, Text: dzzzr.ErrText(33)}, nil
}

func (f *fakeEngine) HasAdminCredentials() bool { return f.admin }

func (f *fakeEngine) AdminListGames(context.Context) ([]dzzzr.AdminGame, error) {
	f.note("AdminListGames")
	return []dzzzr.AdminGame{{ID: 4242, Name: "Тест"}}, nil
}

func (f *fakeEngine) AdminGetGame(_ context.Context, id int) (*dzzzr.AdminGameInfo, error) {
	f.note("AdminGetGame")
	return &dzzzr.AdminGameInfo{ID: id}, nil
}

func (f *fakeEngine) AdminListLevels(context.Context, int) ([]dzzzr.AdminLevel, error) {
	f.note("AdminListLevels")
	return []dzzzr.AdminLevel{{ID: 901, Order: 1, Title: "Гаражи"}}, nil
}

func (f *fakeEngine) AdminGetLevel(_ context.Context, gameID, levelID int) (*dzzzr.AdminLevelInfo, error) {
	f.note("AdminGetLevel")
	return &dzzzr.AdminLevelInfo{ID: levelID, GameID: gameID, Order: 1}, nil
}

func (f *fakeEngine) AdminListTeams(context.Context, int) ([]dzzzr.AdminTeam, error) {
	f.note("AdminListTeams")
	return []dzzzr.AdminTeam{{ID: 5150, Name: "MockTeam"}}, nil
}

func (f *fakeEngine) AdminMonitor(_ context.Context, id int) (*dzzzr.MonitorState, error) {
	f.note("AdminMonitor")
	return &dzzzr.MonitorState{GameID: id}, nil
}

func (f *fakeEngine) AdminGetLog(_ context.Context, id int, since string) ([]dzzzr.AdminLogEntry, string, error) {
	f.note("AdminGetLog")
	return []dzzzr.AdminLogEntry{{TeamID: 5150, Level: 1, Event: 2, Time: "22:30:15"}}, "2026-09-08 22:31:00", nil
}

func (f *fakeEngine) AdminListMessages(context.Context, int, int) ([]dzzzr.AdminMessage, error) {
	f.note("AdminListMessages")
	return []dzzzr.AdminMessage{{Time: "22:03:00", Content: "Привет"}}, nil
}

func (f *fakeEngine) AdminGiveLevel(context.Context, int, int, int, string) error {
	f.note("AdminGiveLevel")
	return nil
}

func (f *fakeEngine) AdminAcceptCode(context.Context, int, int, int, string, string) error {
	f.note("AdminAcceptCode")
	return nil
}

func (f *fakeEngine) AdminAddCorrection(_ context.Context, _, _ int, kind string, _ int, _ string) error {
	f.note("AdminAddCorrection:" + kind)
	return nil
}

func (f *fakeEngine) AdminBlockTeam(context.Context, int, int) error {
	f.note("AdminBlockTeam")
	return nil
}

func (f *fakeEngine) AdminUnblockTeam(context.Context, int, int) error {
	f.note("AdminUnblockTeam")
	return nil
}

func (f *fakeEngine) AdminSetEngine(context.Context, int, bool) error {
	f.note("AdminSetEngine")
	return nil
}

func (f *fakeEngine) AdminSendMessage(context.Context, int, int, string, string, bool) error {
	f.note("AdminSendMessage")
	return nil
}

func (f *fakeEngine) GetArchiveStat(_ context.Context, gameID int) (*dzzzr.ArchiveStat, error) {
	f.note("GetArchiveStat")
	f.lastGame = gameID
	return &dzzzr.ArchiveStat{
		GameID: gameID, Name: "«Битва»", Date: "17 июля 2026 г.",
		Columns: []dzzzr.ArchiveStatColumn{
			{Title: "1.0. Кулак", Level: true},
			{Title: "1.0. Кулак", Level: true},
			{Title: "Место"},
		},
		Teams: []dzzzr.ArchiveStatTeam{
			{ID: 1235, Name: "Rising", Cells: []string{"00:03:01", "00:39:32", "1 / 2"},
				Notes: map[string]string{"Место": "—"}},
			{ID: 12, Name: "Все В Сад", Cells: []string{"00:25:00", "01:00:00", "2 / 2"}},
		},
	}, nil
}

func (f *fakeEngine) GetArchiveDescription(_ context.Context, gameID int) (*dzzzr.ArchiveDescription, error) {
	f.note("GetArchiveDescription")
	f.lastGame = gameID
	return &dzzzr.ArchiveDescription{
		GameID: gameID, Name: "«Битва»",
		Sections: []dzzzr.ArchiveSection{{Title: "Легенда", Text: "Начинаем"}},
		Levels: []dzzzr.ArchiveLevel{
			{Title: "1.0. Кулак", Codes: []string{"ЯВСЕВИЖУ"}, Task: "Первое"},
			{Title: "1.1. Машинки", Codes: []string{"5D57R"}, Task: "Второе"},
		},
	}, nil
}

func (f *fakeEngine) GetGameLog(_ context.Context, gameID int) (*dzzzr.GameLog, error) {
	f.note("GetGameLog")
	f.lastGame = gameID
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return nil, err
	}
	return &dzzzr.GameLog{
		GameID: gameID, Columns: []string{"time", "team", "event"},
		Entries: []map[string]string{{"time": "21:03:01", "team": "Rising", "event": "уровень выдан"}, {"time": "21:04:01", "team": "Rising", "event": "принят код"}},
	}, nil
}

func mustCatalog(t *testing.T, e agenttools.Engine, opts agenttools.Options) *agenttools.Catalog {
	t.Helper()
	c, err := agenttools.NewCatalog(e, opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func call(t *testing.T, c *agenttools.Catalog, name string, args map[string]any) agenttools.Result {
	t.Helper()
	tool, ok := c.Lookup(name)
	if !ok {
		t.Fatalf("tool %q not found", name)
	}
	return tool.Execute(context.Background(), args)
}

func TestCatalogRequiresEngineAndConfirmer(t *testing.T) {
	if _, err := agenttools.NewCatalog(nil, agenttools.Options{Policy: agenttools.PolicyFull}); err == nil {
		t.Error("nil engine must fail")
	}
	if _, err := agenttools.NewCatalog(newFakeEngine(), agenttools.Options{}); err == nil {
		t.Error("approve without a confirmer must fail")
	}
	if _, err := agenttools.NewCatalog(newFakeEngine(), agenttools.Options{Policy: "wat"}); err == nil {
		t.Error("unknown policy must fail")
	}
	if p, err := agenttools.ParsePolicy(""); err != nil || p != agenttools.DefaultPolicy {
		t.Errorf("ParsePolicy(\"\") = %v, %v", p, err)
	}
}

func TestReadonlyHidesMutatingTools(t *testing.T) {
	c := mustCatalog(t, newFakeEngine(), agenttools.Options{Policy: agenttools.PolicyReadonly, IncludeAdmin: true})
	for _, tool := range c.Tools() {
		if tool.Mutating() {
			t.Errorf("readonly catalog exposes %q", tool.Name())
		}
	}
	if len(c.All()) <= len(c.Tools()) {
		t.Error("All must include the hidden tools")
	}
	res := call(t, c, "send_code", map[string]any{"code": "1"})
	if !res.IsError || !strings.Contains(res.Content, "read-only") {
		t.Errorf("refusal = %+v", res)
	}
	if !strings.Contains(c.SystemPromptAddendum(), "read-only") {
		t.Error("prompt must mention the policy")
	}
}

func TestApprovePolicyAsksConfirmer(t *testing.T) {
	e := newFakeEngine()
	var seen agenttools.ConfirmRequest
	answer := true
	c := mustCatalog(t, e, agenttools.Options{
		Confirmer: agenttools.ConfirmerFunc(func(_ context.Context, req agenttools.ConfirmRequest) (bool, error) {
			seen = req
			return answer, nil
		}),
	})
	res := call(t, c, "send_code", map[string]any{"code": "КОД1"})
	if res.IsError {
		t.Fatalf("approved call failed: %+v", res)
	}
	if seen.Tool != "send_code" || seen.Args["code"] != "КОД1" {
		t.Errorf("confirm request = %+v", seen)
	}
	if e.lastCode != "КОД1" || e.calls["SendCode"] != 1 {
		t.Errorf("engine not called: %+v", e.calls)
	}
	var view struct {
		Err      int    `json:"err"`
		Text     string `json:"text"`
		Accepted bool   `json:"accepted"`
	}
	if err := json.Unmarshal([]byte(res.Content), &view); err != nil {
		t.Fatal(err)
	}
	if view.Err != 9 || !view.Accepted || view.Text == "" {
		t.Errorf("result = %+v", view)
	}

	answer = false
	res = call(t, c, "send_code", map[string]any{"code": "X"})
	if !res.IsError || !strings.Contains(res.Content, "declined") {
		t.Errorf("declined result = %+v", res)
	}
	if e.calls["SendCode"] != 1 {
		t.Error("declined call must not reach the engine")
	}
}

func TestConfirmerErrorRefuses(t *testing.T) {
	e := newFakeEngine()
	c := mustCatalog(t, e, agenttools.Options{
		Confirmer: agenttools.ConfirmerFunc(func(context.Context, agenttools.ConfirmRequest) (bool, error) {
			return false, errors.New("канал подтверждения закрыт")
		}),
	})
	res := call(t, c, "send_code", map[string]any{"code": "1"})
	if !res.IsError || !strings.Contains(res.Content, "not authorized") {
		t.Errorf("result = %+v", res)
	}
}

func TestReadCacheAndInvalidation(t *testing.T) {
	e := newFakeEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, ReadCacheTTL: time.Minute})
	call(t, c, "game_status", nil)
	call(t, c, "game_status", nil)
	if e.calls["GetGame"] != 1 {
		t.Errorf("second read should hit the cache, GetGame = %d", e.calls["GetGame"])
	}
	call(t, c, "messages", map[string]any{"after": "2026-09-08 20:00:00"})
	call(t, c, "messages", nil)
	if e.calls["GetMessages:2026-09-08 20:00:00"] != 1 || e.calls["GetMessages:"] != 1 {
		t.Errorf("arguments must be part of the cache key: %+v", e.calls)
	}
	call(t, c, "send_code", map[string]any{"code": "1"})
	call(t, c, "game_status", nil)
	if e.calls["GetGame"] != 2 {
		t.Errorf("a mutation must clear the cache, GetGame = %d", e.calls["GetGame"])
	}
	c.InvalidateCache()
	call(t, c, "game_status", nil)
	if e.calls["GetGame"] != 3 {
		t.Errorf("InvalidateCache must drop reads, GetGame = %d", e.calls["GetGame"])
	}
}

func TestNoCacheWhenDisabled(t *testing.T) {
	e := newFakeEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull})
	call(t, c, "game_status", nil)
	call(t, c, "game_status", nil)
	if e.calls["GetGame"] != 2 {
		t.Errorf("without a TTL every read must hit the engine, GetGame = %d", e.calls["GetGame"])
	}
}

func TestPlayerToolsStripHTML(t *testing.T) {
	e := newFakeEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull})
	res := call(t, c, "game_status", nil)
	if res.IsError {
		t.Fatal(res.Content)
	}
	var view struct {
		Level    struct{ Question string } `json:"level"`
		Messages []struct{ Text string }   `json:"messages"`
	}
	if err := json.Unmarshal([]byte(res.Content), &view); err != nil {
		t.Fatal(err)
	}
	if view.Level.Question != "Найдите код" {
		t.Errorf("question = %q, HTML must be stripped", view.Level.Question)
	}
	if len(view.Messages) != 1 || view.Messages[0].Text != "Привет & удачи" {
		t.Errorf("messages = %+v, entities must be decoded", view.Messages)
	}
	res = call(t, c, "team_log", nil)
	if strings.Contains(res.Content, "<td>") || !strings.Contains(res.Content, "выдан уровень") {
		t.Errorf("log = %s", res.Content)
	}
	res = call(t, c, "team_stat", nil)
	if strings.Contains(res.Content, "<td>") || !strings.Contains(res.Content, "00:12:40") {
		t.Errorf("stat = %s", res.Content)
	}
	res = call(t, c, "level_info", nil)
	if !strings.Contains(res.Content, "Гаражи") || !strings.Contains(res.Content, "\"can_abandon\":true") {
		t.Errorf("level info = %s", res.Content)
	}
}

func TestLevelDefaultsToCurrent(t *testing.T) {
	e := newFakeEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull})
	// take_hint_early falls back to the current level and to the hint the
	// engine has not issued; naming neither makes the engine pick one itself.
	call(t, c, "take_hint_early", nil)
	if e.lastLevel != 3 || e.lastHint != 2 {
		t.Errorf("early hint used level %d hint %d, want 3 and 2", e.lastLevel, e.lastHint)
	}
	call(t, c, "take_hint_early", map[string]any{"level": 5, "hint": 1})
	if e.lastLevel != 5 || e.lastHint != 1 {
		t.Errorf("explicit early hint ignored: level %d hint %d", e.lastLevel, e.lastHint)
	}
	call(t, c, "send_spoiler_code", map[string]any{"code": "SP1"})
	if e.lastLevel != 3 || e.lastCode != "SP1" {
		t.Errorf("spoiler used level %d code %q", e.lastLevel, e.lastCode)
	}
}

func TestArgumentCoercionAndValidation(t *testing.T) {
	e := newFakeEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull})
	// Providers send integers as floats, strings or json.Number.
	if res := call(t, c, "send_bonus_code", map[string]any{"level": 7.0, "code": "B1"}); res.IsError {
		t.Errorf("float level rejected: %s", res.Content)
	}
	if e.lastLevel != 7 {
		t.Errorf("level = %d", e.lastLevel)
	}
	if res := call(t, c, "send_bonus_code", map[string]any{"level": "8", "code": "B2"}); res.IsError {
		t.Errorf("string level rejected: %s", res.Content)
	}
	if e.lastLevel != 8 {
		t.Errorf("level = %d", e.lastLevel)
	}
	if res := call(t, c, "send_code", map[string]any{}); !res.IsError || !strings.Contains(res.Content, "code") {
		t.Errorf("missing code must fail: %+v", res)
	}
	if res := call(t, c, "take_hint", map[string]any{"hint": 3}); !res.IsError {
		t.Error("hint 3 must fail")
	}
	if res := call(t, c, "games_list", map[string]any{"new_only": "true"}); res.IsError {
		t.Errorf("string bool rejected: %s", res.Content)
	}
	if e.calls["GetGamesList:new"] != 1 {
		t.Errorf("new_only was not forwarded: %+v", e.calls)
	}
}

func TestEngineErrorBecomesToolError(t *testing.T) {
	e := newFakeEngine()
	e.failNext = errors.New("сеть недоступна")
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull})
	res := call(t, c, "game_status", nil)
	if !res.IsError || !strings.Contains(res.Content, "сеть недоступна") {
		t.Errorf("result = %+v", res)
	}
}

func TestAdminToolsGating(t *testing.T) {
	e := newFakeEngine()
	without := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull})
	if _, ok := without.Lookup("admin_games"); ok {
		t.Error("admin tools must be opt-in")
	}
	e.admin = false
	noCreds := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true})
	if _, ok := noCreds.Lookup("admin_games"); ok {
		t.Error("admin tools need organizer credentials")
	}
	e.admin = true
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true})
	if res := call(t, c, "admin_games", nil); res.IsError {
		t.Fatal(res.Content)
	}
	if res := call(t, c, "admin_level", map[string]any{"game_id": 4242, "level_id": 901}); res.IsError {
		t.Fatal(res.Content)
	}
	if res := call(t, c, "admin_add_correction", map[string]any{"game_id": 4242, "team_id": 5150, "kind": "bonus", "minutes": 10}); res.IsError {
		t.Fatal(res.Content)
	}
	if e.calls["AdminAddCorrection:bonus"] != 1 {
		t.Errorf("calls = %+v", e.calls)
	}
	if res := call(t, c, "admin_block_team", map[string]any{"game_id": 4242, "team_id": 5150, "blocked": true}); res.IsError {
		t.Fatal(res.Content)
	}
	if res := call(t, c, "admin_block_team", map[string]any{"game_id": 4242, "team_id": 5150, "blocked": false}); res.IsError {
		t.Fatal(res.Content)
	}
	if e.calls["AdminBlockTeam"] != 1 || e.calls["AdminUnblockTeam"] != 1 {
		t.Errorf("calls = %+v", e.calls)
	}
	if res := call(t, c, "admin_block_team", map[string]any{"game_id": 4242, "team_id": 5150}); !res.IsError {
		t.Error("missing blocked must fail")
	}
	if res := call(t, c, "admin_log", map[string]any{"game_id": 4242}); res.IsError || !strings.Contains(res.Content, "next_since") {
		t.Errorf("admin_log = %+v", res)
	}
}

func TestToolSchemasAreValidAndIsolated(t *testing.T) {
	c := mustCatalog(t, newFakeEngine(), agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true})
	names := map[string]bool{}
	for _, tool := range c.All() {
		if tool.Name() == "" || tool.Description() == "" {
			t.Errorf("tool %q lacks a name or description", tool.Name())
		}
		if names[tool.Name()] {
			t.Errorf("duplicate tool name %q", tool.Name())
		}
		names[tool.Name()] = true
		s := tool.Parameters()
		if s["type"] != "object" {
			t.Errorf("%s: schema type = %v", tool.Name(), s["type"])
		}
		if _, err := json.Marshal(s); err != nil {
			t.Errorf("%s: schema does not marshal: %v", tool.Name(), err)
		}
		props, _ := s["properties"].(map[string]any)
		for _, req := range toStrings(s["required"]) {
			if _, ok := props[req]; !ok {
				t.Errorf("%s: required %q is not among the properties", tool.Name(), req)
			}
		}
		// Mutating the returned schema must not affect the catalog.
		s["type"] = "corrupted"
		if tool.Parameters()["type"] != "object" {
			t.Errorf("%s: schema is not defensively copied", tool.Name())
		}
	}
	for _, want := range []string{"game_status", "level_info", "send_code", "admin_games", "admin_send_message"} {
		if !names[want] {
			t.Errorf("catalog is missing %q", want)
		}
	}
}

func toStrings(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// An out-of-range hint number is a mistake, not a request to pick one: taking
// the other hint instead charges the team a penalty nobody asked for.
func TestTakeHintEarlyRejectsAnInvalidHintNumber(t *testing.T) {
	e := newFakeEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull})
	res := call(t, c, "take_hint_early", map[string]any{"hint": 3})
	if !res.IsError {
		t.Fatalf("hint 3 was accepted: %s", res.Content)
	}
	if e.lastHint != 0 {
		t.Errorf("the engine was called with hint %d", e.lastHint)
	}
}

// The penalty the engine charged must reach an agent as data, not only inside
// the free-text result.
func TestActionViewCarriesThePenalty(t *testing.T) {
	e := newFakeEngine()
	e.penalty = 21
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyFull})
	res := call(t, c, "take_hint_early", map[string]any{"hint": 1, "level": 3})
	if !strings.Contains(res.Content, `"penalty_minutes":21`) {
		t.Errorf("result = %s", res.Content)
	}
}

func TestArchiveToolsAreReadableWithoutAdmin(t *testing.T) {
	e := newFakeEngine()
	// The archive is public, so its tools belong to the player set: a
	// read-only catalog without organizer credentials must still offer them.
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly})
	for _, name := range []string{"game_stat", "game_description", "game_log"} {
		tool, ok := c.Lookup(name)
		if !ok {
			t.Fatalf("tool %q not found", name)
		}
		if tool.Mutating() {
			t.Errorf("%s must not be mutating", name)
		}
	}
}

func TestGameStatToolKeysResultsByColumn(t *testing.T) {
	e := newFakeEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly})
	res := call(t, c, "game_stat", map[string]any{"game_id": 1563})
	if res.IsError {
		t.Fatalf("result = %+v", res)
	}
	var got struct {
		GameID       int      `json:"game_id"`
		LevelColumns []string `json:"level_columns"`
		TotalColumns []string `json:"total_columns"`
		Teams        []struct {
			ID      int               `json:"id"`
			Name    string            `json:"name"`
			Results map[string]string `json:"results"`
			Notes   map[string]string `json:"notes"`
		} `json:"teams"`
	}
	if err := json.Unmarshal([]byte(res.Content), &got); err != nil {
		t.Fatal(err)
	}
	if got.GameID != 1563 || e.lastGame != 1563 {
		t.Errorf("game = %d / %d", got.GameID, e.lastGame)
	}
	// Two levels of a game may share a name; keying by it must not lose one.
	if len(got.LevelColumns) != 2 || got.LevelColumns[1] != "1.0. Кулак (2)" {
		t.Fatalf("level columns = %q", got.LevelColumns)
	}
	if len(got.TotalColumns) != 1 || got.TotalColumns[0] != "Место" {
		t.Errorf("total columns = %q", got.TotalColumns)
	}
	if len(got.Teams) != 2 {
		t.Fatalf("teams = %+v", got.Teams)
	}
	first := got.Teams[0]
	if first.Results["1.0. Кулак"] != "00:03:01" || first.Results["1.0. Кулак (2)"] != "00:39:32" || first.Results["Место"] != "1 / 2" {
		t.Errorf("results = %v", first.Results)
	}
	if first.Notes["Место"] != "—" || got.Teams[1].Notes != nil {
		t.Errorf("notes = %v / %v", first.Notes, got.Teams[1].Notes)
	}
	if res := call(t, c, "game_stat", nil); !res.IsError {
		t.Error("game_stat without game_id must fail")
	}
}

func TestGameDescriptionToolCanReturnOneLevel(t *testing.T) {
	e := newFakeEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly})
	var all dzzzr.ArchiveDescription
	res := call(t, c, "game_description", map[string]any{"game_id": 1563})
	if res.IsError {
		t.Fatalf("result = %+v", res)
	}
	if err := json.Unmarshal([]byte(res.Content), &all); err != nil {
		t.Fatal(err)
	}
	if len(all.Levels) != 2 || len(all.Sections) != 1 {
		t.Fatalf("description = %+v", all)
	}

	var one dzzzr.ArchiveDescription
	res = call(t, c, "game_description", map[string]any{"game_id": 1563, "level": 2})
	if res.IsError {
		t.Fatalf("result = %+v", res)
	}
	if err := json.Unmarshal([]byte(res.Content), &one); err != nil {
		t.Fatal(err)
	}
	if len(one.Levels) != 1 || one.Levels[0].Title != "1.1. Машинки" {
		t.Fatalf("level = %+v", one.Levels)
	}
	// The whole scenario must survive the narrowing: slicing it must not
	// truncate the copy the next caller gets.
	res = call(t, c, "game_description", map[string]any{"game_id": 1563})
	if err := json.Unmarshal([]byte(res.Content), &all); err != nil {
		t.Fatal(err)
	}
	if len(all.Levels) != 2 {
		t.Errorf("levels after narrowing = %+v", all.Levels)
	}
	if res := call(t, c, "game_description", map[string]any{"game_id": 1563, "level": 9}); !res.IsError {
		t.Error("level out of range must fail")
	}
}

func TestGameLogToolReportsMissingRights(t *testing.T) {
	e := newFakeEngine()
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly})
	res := call(t, c, "game_log", map[string]any{"game_id": 1563})
	if res.IsError || !strings.Contains(res.Content, "уровень выдан") {
		t.Fatalf("result = %+v", res)
	}
	e.failNext = &dzzzr.EngineError{Code: dzzzr.ErrNoPermission, Text: "смотреть его может капитан, замкапитана или начштаба"}
	res = call(t, c, "game_log", map[string]any{"game_id": 1564})
	if !res.IsError || !strings.Contains(res.Content, "начштаба") {
		t.Fatalf("refusal = %+v", res)
	}
}

func TestGameLogPagination(t *testing.T) {
	c := mustCatalog(t, newFakeEngine(), agenttools.Options{Policy: agenttools.PolicyReadonly})
	result := call(t, c, "game_log", map[string]any{"game_id": 1563, "limit": 1})
	if result.IsError {
		t.Fatal(result.Content)
	}
	var page struct {
		Total   int                 `json:"total"`
		Next    int                 `json:"next_offset"`
		Entries []map[string]string `json:"entries"`
	}
	if err := json.Unmarshal([]byte(result.Content), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.Next != 1 || len(page.Entries) != 1 {
		t.Fatalf("page: %+v", page)
	}
	result = call(t, c, "game_log", map[string]any{"game_id": 1563, "offset": 1, "limit": 1})
	if result.IsError || strings.Contains(result.Content, "next_offset") || !strings.Contains(result.Content, "принят код") {
		t.Fatalf("last page: %+v", result)
	}
	for _, args := range []map[string]any{{"game_id": 1563, "offset": -1}, {"game_id": 1563, "limit": 501}, {"game_id": 1563, "limit": 0}} {
		if r := call(t, c, "game_log", args); !r.IsError {
			t.Fatalf("accepted invalid page: %v", args)
		}
	}
}

func TestNoArgumentToolSchemaUsesObjectProperties(t *testing.T) {
	catalog, err := agenttools.NewCatalog(newFakeEngine(), agenttools.Options{Policy: agenttools.PolicyReadonly})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range catalog.Tools() {
		data, err := json.Marshal(tool.Parameters())
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatal(err)
		}
		if _, ok := schema["properties"].(map[string]any); !ok {
			t.Errorf("%s: properties must be an object: %s", tool.Name(), data)
		}
	}
}
