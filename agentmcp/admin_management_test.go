package agentmcp_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

type adminEditEngine struct {
	stubEngine
	calls           int
	gameID, levelID int
	params          dzzzr.LevelParams
}

func (e *adminEditEngine) AdminUpdateLevel(_ context.Context, gameID, levelID int, p dzzzr.LevelParams) error {
	e.calls++
	e.gameID = gameID
	e.levelID = levelID
	e.params = p
	return nil
}
func TestAdminEditThroughMCP(t *testing.T) {
	e := &adminEditEngine{stubEngine: stubEngine{admin: true}}
	catalog, err := agenttools.NewCatalog(e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	session := connect(t, catalog, mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil))
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "admin_update_level", Arguments: map[string]any{
		"game_id": 41, "level_id": 91, "params": map[string]any{"publish": false, "penalty": 0, "codes": []any{}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal(contentText(result))
	}
	if e.calls != 1 || e.gameID != 41 || e.levelID != 91 || e.params.Publish == nil || *e.params.Publish || e.params.Penalty == nil || *e.params.Penalty != 0 || e.params.Codes == nil || len(e.params.Codes) != 0 {
		t.Fatalf("edit lost data: %+v", e)
	}
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "admin_update_level", Arguments: map[string]any{
		"game_id": 41, "level_id": 91, "params": map[string]any{"publsih": false},
	}})
	if err == nil && !result.IsError {
		t.Fatal("unknown field accepted")
	}
	if e.calls != 1 {
		t.Fatal("invalid edit reached engine")
	}
}
