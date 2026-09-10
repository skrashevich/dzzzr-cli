package agentmcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skrashevich/dzzzr-cli/agentmcp"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// stubEngine is the smallest Engine that lets a catalog be built.
type stubEngine struct {
	agenttools.Engine
	sent  string
	admin bool
}

func (s *stubEngine) GetGame(context.Context) (*dzzzr.GameState, error) {
	return &dzzzr.GameState{GameName: "Тест", GameID: 4242, Level: &dzzzr.Level{LevelNumber: 3, Question: "Вопрос", TotalCodes: 1}}, nil
}
func (s *stubEngine) GetLevelInfo(context.Context) (*dzzzr.LevelInfo, error) {
	return &dzzzr.LevelInfo{Number: 3}, nil
}
func (s *stubEngine) GetBonusLevelInfo(context.Context) (*dzzzr.LevelInfo, error) {
	return &dzzzr.LevelInfo{}, nil
}
func (s *stubEngine) GetStat(context.Context) (*dzzzr.TeamStat, error) { return &dzzzr.TeamStat{}, nil }
func (s *stubEngine) GetLog(context.Context) ([]dzzzr.LogEntry, error) { return nil, nil }
func (s *stubEngine) GetMessages(context.Context, string) ([]dzzzr.ChatMessage, error) {
	return nil, nil
}
func (s *stubEngine) GetGamesList(context.Context, dzzzr.GamesListOptions) ([]dzzzr.GameInfo, error) {
	return nil, nil
}
func (s *stubEngine) SendCode(_ context.Context, code string) (*dzzzr.ActionResult, error) {
	s.sent = code
	return &dzzzr.ActionResult{Err: 9, Text: dzzzr.ErrText(9)}, nil
}
func (s *stubEngine) SendBonusCode(context.Context, int, string) (*dzzzr.ActionResult, error) {
	return &dzzzr.ActionResult{}, nil
}
func (s *stubEngine) SendSpoilerCode(context.Context, int, string) (*dzzzr.ActionResult, error) {
	return &dzzzr.ActionResult{}, nil
}
func (s *stubEngine) TakeHint(context.Context, int, int) (*dzzzr.ActionResult, error) {
	return &dzzzr.ActionResult{}, nil
}
func (s *stubEngine) TakeHintEarly(context.Context, int, int) (*dzzzr.ActionResult, error) {
	return &dzzzr.ActionResult{}, nil
}
func (s *stubEngine) Abandon(context.Context) (*dzzzr.ActionResult, error) {
	return &dzzzr.ActionResult{}, nil
}
func (s *stubEngine) TakeBreak(context.Context) (*dzzzr.ActionResult, error) {
	return &dzzzr.ActionResult{}, nil
}
func (s *stubEngine) StopBreak(context.Context) (*dzzzr.ActionResult, error) {
	return &dzzzr.ActionResult{}, nil
}
func (s *stubEngine) NextLevel(context.Context, int) (*dzzzr.ActionResult, error) {
	return &dzzzr.ActionResult{}, nil
}
func (s *stubEngine) SelectLevel(context.Context, int, int) (*dzzzr.ActionResult, error) {
	return &dzzzr.ActionResult{}, nil
}
func (s *stubEngine) PostMessage(context.Context, string) error { return nil }
func (s *stubEngine) SendMessageToOrg(context.Context, string) (*dzzzr.ActionResult, error) {
	return &dzzzr.ActionResult{}, nil
}
func (s *stubEngine) HasAdminCredentials() bool { return s.admin }
func (s *stubEngine) AdminListGames(context.Context) ([]dzzzr.AdminGame, error) {
	return []dzzzr.AdminGame{{ID: 4242}}, nil
}
func (s *stubEngine) AdminGetGame(context.Context, int) (*dzzzr.AdminGameInfo, error) {
	return &dzzzr.AdminGameInfo{}, nil
}
func (s *stubEngine) AdminListLevels(context.Context, int) ([]dzzzr.AdminLevel, error) {
	return nil, nil
}
func (s *stubEngine) AdminGetLevel(context.Context, int, int) (*dzzzr.AdminLevelInfo, error) {
	return &dzzzr.AdminLevelInfo{}, nil
}
func (s *stubEngine) AdminListTeams(context.Context, int) ([]dzzzr.AdminTeam, error) { return nil, nil }
func (s *stubEngine) AdminMonitor(context.Context, int) (*dzzzr.MonitorState, error) {
	return &dzzzr.MonitorState{}, nil
}
func (s *stubEngine) AdminGetLog(context.Context, int, string) ([]dzzzr.AdminLogEntry, string, error) {
	return nil, "", nil
}
func (s *stubEngine) AdminListMessages(context.Context, int, int) ([]dzzzr.AdminMessage, error) {
	return nil, nil
}
func (s *stubEngine) AdminGiveLevel(context.Context, int, int, int, string) error { return nil }
func (s *stubEngine) AdminAcceptCode(context.Context, int, int, int, string, string) error {
	return nil
}
func (s *stubEngine) AdminAddCorrection(context.Context, int, int, string, int, string) error {
	return nil
}
func (s *stubEngine) AdminBlockTeam(context.Context, int, int) error   { return nil }
func (s *stubEngine) AdminUnblockTeam(context.Context, int, int) error { return nil }
func (s *stubEngine) AdminSetEngine(context.Context, int, bool) error  { return nil }
func (s *stubEngine) AdminSendMessage(context.Context, int, int, string, string, bool) error {
	return nil
}

// connect runs a server and a client over an in-memory transport.
func connect(t *testing.T, catalog *agenttools.Catalog, client *mcp.Client) *mcp.ClientSession {
	t.Helper()
	server, err := agentmcp.NewServer(catalog, "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// contentText joins the text blocks of a tool result for readable failures.
func contentText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestNewServerRequiresCatalog(t *testing.T) {
	if _, err := agentmcp.NewServer(nil, "1"); err == nil {
		t.Fatal("nil catalog must fail")
	}
}

func TestServerListsToolsWithReadOnlyHints(t *testing.T) {
	catalog, err := agenttools.NewCatalog(&stubEngine{}, agenttools.Options{Policy: agenttools.PolicyFull})
	if err != nil {
		t.Fatal(err)
	}
	session := connect(t, catalog, mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil))
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	status, ok := byName["game_status"]
	if !ok {
		t.Fatalf("game_status missing; got %d tools", len(res.Tools))
	}
	if status.Annotations == nil || !status.Annotations.ReadOnlyHint {
		t.Error("a read tool must be annotated read-only")
	}
	if status.Description == "" || status.InputSchema == nil {
		t.Error("tool definition is incomplete")
	}
	send, ok := byName["send_code"]
	if !ok {
		t.Fatal("send_code missing")
	}
	if send.Annotations != nil && send.Annotations.ReadOnlyHint {
		t.Error("a mutating tool must not be annotated read-only")
	}
}

func TestReadonlyCatalogPublishesNoMutatingTools(t *testing.T) {
	catalog, err := agenttools.NewCatalog(&stubEngine{}, agenttools.Options{Policy: agenttools.PolicyReadonly})
	if err != nil {
		t.Fatal(err)
	}
	session := connect(t, catalog, mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil))
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "send_code" {
			t.Error("read-only server must not publish send_code")
		}
	}
}

func TestCallToolReturnsJSON(t *testing.T) {
	catalog, err := agenttools.NewCatalog(&stubEngine{}, agenttools.Options{Policy: agenttools.PolicyFull})
	if err != nil {
		t.Fatal(err)
	}
	session := connect(t, catalog, mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil))
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "game_status"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || len(res.Content) == 0 {
		t.Fatalf("result = %+v", res)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content = %T", res.Content[0])
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(text.Text), &view); err != nil {
		t.Fatalf("tool result is not JSON: %v", err)
	}
	if view["game_name"] != "Тест" {
		t.Errorf("view = %v", view)
	}
}

func TestElicitationApprovesAndDeclines(t *testing.T) {
	engine := &stubEngine{}
	catalog, err := agenttools.NewCatalog(engine, agenttools.Options{
		Policy: agenttools.PolicyApprove, Confirmer: agentmcp.NewElicitConfirmer(),
	})
	if err != nil {
		t.Fatal(err)
	}
	answer := true
	var prompt string
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			prompt = req.Params.Message
			if !answer {
				return &mcp.ElicitResult{Action: "decline"}, nil
			}
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
		},
	})
	session := connect(t, catalog, client)
	ctx := context.Background()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "send_code", Arguments: map[string]any{"code": "КОД1"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("approved call failed: %s", contentText(res))
	}
	if engine.sent != "КОД1" {
		t.Errorf("engine received %q", engine.sent)
	}
	if !strings.Contains(prompt, "send_code") || !strings.Contains(prompt, "КОД1") {
		t.Errorf("prompt = %q", prompt)
	}

	answer = false
	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "send_code", Arguments: map[string]any{"code": "ДРУГОЙ"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("declined call must be an error result")
	}
	if engine.sent != "КОД1" {
		t.Errorf("declined call reached the engine: %q", engine.sent)
	}
}

func TestElicitationUnsupportedClientRefuses(t *testing.T) {
	engine := &stubEngine{}
	catalog, err := agenttools.NewCatalog(engine, agenttools.Options{
		Policy: agenttools.PolicyApprove, Confirmer: agentmcp.NewElicitConfirmer(),
	})
	if err != nil {
		t.Fatal(err)
	}
	session := connect(t, catalog, mcp.NewClient(&mcp.Implementation{Name: "plain-client", Version: "1"}, nil))
	// A client that cannot ask the user fails the round trip; what matters is
	// that the mutation never reaches the engine.
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "send_code", Arguments: map[string]any{"code": "X"}})
	switch {
	case err != nil:
		if !strings.Contains(err.Error(), "elicitation") {
			t.Errorf("err = %v", err)
		}
	case !res.IsError:
		t.Error("a client without elicitation must not be able to mutate")
	}
	if engine.sent != "" {
		t.Errorf("engine was called anyway: %q", engine.sent)
	}
}

func TestAdminToolsPublishedWithCredentials(t *testing.T) {
	catalog, err := agenttools.NewCatalog(&stubEngine{admin: true}, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	session := connect(t, catalog, mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil))
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range res.Tools {
		if tool.Name == "admin_games" {
			found = true
		}
	}
	if !found {
		t.Error("admin_games must be published when credentials are present")
	}
}

// The user approves one call, and the client retries with the answer in a
// separate request. Nothing in the protocol stops it from retrying with
// different arguments, so the answer carries a fingerprint of the arguments
// it was given for.
func TestApprovalIsBoundToTheArguments(t *testing.T) {
	engine := &stubEngine{}
	catalog, err := agenttools.NewCatalog(engine, agenttools.Options{
		Policy: agenttools.PolicyApprove, Confirmer: agentmcp.NewElicitConfirmer(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var answer func(schema any) map[string]any
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "accept", Content: answer(req.Params.RequestedSchema)}, nil
		},
	})
	session := connect(t, catalog, client)
	ctx := context.Background()

	// An honest client echoes the identifier the server asked for.
	answer = func(schema any) map[string]any {
		return map[string]any{"confirm": true, "call": callIdentifier(t, schema)}
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "send_code", Arguments: map[string]any{"code": "ПРАВИЛЬНЫЙ"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("an honest confirmation failed: %s", contentText(res))
	}
	if engine.sent != "ПРАВИЛЬНЫЙ" {
		t.Errorf("engine received %q", engine.sent)
	}

	// A client that answers for a different call must not get through.
	answer = func(any) map[string]any {
		return map[string]any{"confirm": true, "call": "0000000000000000"}
	}
	// The SDK validates the answer against the schema, so a mismatch is
	// usually refused before the server sees it; the server checks too, for a
	// client that does not validate. Either way the call must not go through.
	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "send_code", Arguments: map[string]any{"code": "ПОДМЕНА"}})
	switch {
	case err != nil:
		if !strings.Contains(err.Error(), "const") && !strings.Contains(err.Error(), "schema") {
			t.Errorf("err = %v", err)
		}
	case !res.IsError:
		t.Error("an answer for another call must be refused")
	}
	if engine.sent != "ПРАВИЛЬНЫЙ" {
		t.Errorf("the substituted call reached the engine: %q", engine.sent)
	}
}

// callIdentifier reads the value the server wants echoed back.
func callIdentifier(t *testing.T, schema any) string {
	t.Helper()
	obj, ok := schema.(map[string]any)
	if !ok {
		t.Fatalf("schema is not an object: %T", schema)
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties: %v", obj)
	}
	call, ok := props["call"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no call property: %v", props)
	}
	v, ok := call["const"].(string)
	if !ok || v == "" {
		t.Fatalf("call property has no identifier: %v", call)
	}
	return v
}
