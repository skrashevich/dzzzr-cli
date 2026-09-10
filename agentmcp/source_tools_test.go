package agentmcp_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func TestPDFToolsThroughMCPWithoutExternalPrograms(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root, err := filepath.Abs("../pdfsource/testdata")
	if err != nil {
		t.Fatal(err)
	}
	e := &sourceMCPEngine{stubEngine: stubEngine{admin: true}}
	c, err := agenttools.NewCatalog(e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true, FilesRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	session := connect(t, c, mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil))
	r, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "read_pdf", Arguments: map[string]any{"path": "synthetic.pdf", "start_page": 1, "end_page": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if r.IsError {
		t.Fatal(contentText(r))
	}
	var doc struct {
		Pages []struct {
			Text   string `json:"text"`
			Images []struct {
				ID string `json:"image_id"`
			} `json:"images"`
		} `json:"pages"`
	}
	if err := json.Unmarshal([]byte(contentText(r)), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 1 || len(doc.Pages[0].Images) != 1 || !strings.Contains(doc.Pages[0].Text, "Drive image chip") {
		t.Fatalf("PDF data lost through MCP: %s", contentText(r))
	}
	r, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "admin_upload_pdf_image", Arguments: map[string]any{"game_id": 42, "path": "synthetic.pdf", "page": 1, "image_id": doc.Pages[0].Images[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	if r.IsError || e.uploads != 1 || !bytes.HasPrefix(e.data, []byte{137, 80, 78, 71}) {
		t.Fatalf("PDF image upload failed: %s", contentText(r))
	}
}

type sourceMCPEngine struct {
	stubEngine
	technical, uploads, creates int
	data                        []byte
	params                      dzzzr.LevelParams
}

func (e *sourceMCPEngine) AdminUploadFile(_ context.Context, _ int, name string, data []byte) (*dzzzr.AdminFileUpload, error) {
	e.uploads++
	e.data = bytes.Clone(data)
	return &dzzzr.AdminFileUpload{Name: name, URL: "https://example.test/" + name}, nil
}
func (e *sourceMCPEngine) AdminCreateTechnicalLevel(context.Context, int) (int, error) {
	e.technical++
	return 99, nil
}
func (e *sourceMCPEngine) AdminCreateLevel(_ context.Context, _ int, p dzzzr.LevelParams) (int, error) {
	e.creates++
	e.params = p
	if e.creates == 2 {
		return 0, errors.New("stopped")
	}
	return 101, nil
}

func TestSourceToolsThroughMCP(t *testing.T) {
	root := t.TempDir()
	data := []byte("private-image\x00\xff")
	if err := os.WriteFile(filepath.Join(root, "image.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	e := &sourceMCPEngine{stubEngine: stubEngine{admin: true}}
	c, err := agenttools.NewCatalog(e, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true, FilesRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	session := connect(t, c, mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil))
	if instructions := session.InitializeResult().Instructions; !strings.Contains(instructions, "PDF SCENARIO IMPORT") || !strings.Contains(instructions, "untrusted SOURCE DATA") {
		t.Fatal("MCP initialization omitted PDF workflow instructions")
	}
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"admin_upload_file", map[string]any{"game_id": 42, "path": "image.png"}},
		{"admin_create_technical_level", map[string]any{"game_id": 42}},
	} {
		r, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if err != nil {
			t.Fatal(err)
		}
		if r.IsError {
			t.Fatal(contentText(r))
		}
		if strings.Contains(contentText(r), "private-image") {
			t.Fatal("binary content leaked")
		}
	}
	if e.uploads != 1 || !bytes.Equal(e.data, data) || e.technical != 1 {
		t.Fatalf("engine calls: %+v", e)
	}
	params := map[string]any{"title": "Source", "publish": false, "comment": "", "time_add_bonus_all": 0}
	args := map[string]any{"plan": map[string]any{"game_id": 42, "mode": "create", "levels": []any{map[string]any{"params": params}, map[string]any{"params": params}}}}
	r, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "admin_validate_source", Arguments: args})
	if err != nil || r.IsError {
		t.Fatalf("validate: %v %v", r, err)
	}
	if e.creates != 0 {
		t.Fatal("validation performed writes")
	}
	r, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "admin_upload_source", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsError || !strings.Contains(contentText(r), `"completed":[{"index":0,"id":101}]`) || !strings.Contains(contentText(r), `"failed_index":1`) {
		t.Fatalf("lost partial result: %s", contentText(r))
	}
	if e.creates != 2 || e.technical != 1 || e.params.Comment == nil || *e.params.Comment != "" || e.params.TimeAddBonusAll == nil || *e.params.TimeAddBonusAll != 0 || e.params.Publish == nil || *e.params.Publish {
		t.Fatalf("lost input fields: %+v", e.params)
	}
}
