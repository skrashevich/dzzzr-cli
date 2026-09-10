package agenttools

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

type pdfTestEngine struct {
	Engine
	uploaded []byte
	name     string
	gameID   int
}

func (*pdfTestEngine) HasAdminCredentials() bool { return true }
func (e *pdfTestEngine) AdminUploadFile(_ context.Context, gameID int, name string, data []byte) (*dzzzr.AdminFileUpload, error) {
	e.uploaded = append([]byte(nil), data...)
	e.gameID = gameID
	e.name = name
	return &dzzzr.AdminFileUpload{Name: name, URL: "https://example.test/uploaded/" + name}, nil
}

func pdfTestCatalog(t *testing.T, policy Policy) (*Catalog, *pdfTestEngine) {
	t.Helper()
	root := t.TempDir()
	data, err := os.ReadFile("../pdfsource/testdata/synthetic.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "source.pdf"), data, 0600); err != nil {
		t.Fatal(err)
	}
	e := &pdfTestEngine{}
	c, err := NewCatalog(e, Options{Policy: policy, IncludeAdmin: true, FilesRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	return c, e
}
func TestPDFReadAndImageUpload(t *testing.T) {
	// PDF reading and image extraction must work without external executables.
	t.Setenv("PATH", t.TempDir())
	c, e := pdfTestCatalog(t, PolicyFull)
	read, _ := c.Lookup("read_pdf")
	r := read.Execute(t.Context(), map[string]any{"path": "source.pdf", "start_page": 1, "end_page": 1})
	if r.IsError {
		t.Fatal(r.Content)
	}
	var out struct {
		PageCount int  `json:"page_count"`
		HasMore   bool `json:"has_more"`
		Pages     []struct {
			Text   string `json:"text"`
			Images []struct {
				ID   string `json:"image_id"`
				Name string `json:"name"`
			} `json:"images"`
		} `json:"pages"`
	}
	if err := json.Unmarshal([]byte(r.Content), &out); err != nil {
		t.Fatal(err)
	}
	if out.PageCount != 12 || !out.HasMore || len(out.Pages) != 1 || len(out.Pages[0].Images) != 1 || !strings.Contains(out.Pages[0].Text, "Drive image chip") {
		t.Fatalf("bad read: %s", r.Content)
	}
	if strings.Contains(r.Content, `"data":`) {
		t.Fatal("binary PDF/image leaked into LLM context")
	}
	img := out.Pages[0].Images[0]
	upload, _ := c.Lookup("admin_upload_pdf_image")
	args := map[string]any{"game_id": 41, "path": "source.pdf", "page": 1, "image_id": img.ID}
	r = upload.Execute(t.Context(), args)
	if r.IsError {
		t.Fatal(r.Content)
	}
	if e.gameID != 41 || e.name != img.Name || !bytes.HasPrefix(e.uploaded, []byte{137, 80, 78, 71}) {
		t.Fatalf("upload not image: %+v", e)
	}
	e.uploaded = nil
	args["image_id"] = "not-present"
	if r = upload.Execute(t.Context(), args); !r.IsError || e.uploaded != nil {
		t.Fatal("missing image uploaded")
	}
}
func TestPDFReadRangeRootAndReadonlyPolicy(t *testing.T) {
	c, e := pdfTestCatalog(t, PolicyReadonly)
	read, _ := c.Lookup("read_pdf")
	upload, _ := c.Lookup("admin_upload_pdf_image")
	for _, args := range []map[string]any{{"path": "../outside.pdf"}, {"path": "source.pdf", "start_page": 0}, {"path": "source.pdf", "start_page": 1.5}, {"path": "source.pdf", "end_page": 11}} {
		if r := read.Execute(t.Context(), args); !r.IsError {
			t.Fatalf("invalid input accepted: %v", args)
		}
	}
	if r := upload.Execute(t.Context(), map[string]any{"path": "source.pdf"}); !r.IsError || e.uploaded != nil {
		t.Fatal("readonly upload permitted")
	}
	for _, tool := range c.Tools() {
		if tool.Name() == "admin_upload_pdf_image" {
			t.Fatal("upload advertised readonly")
		}
	}
}
func TestPDFScenarioInstructions(t *testing.T) {
	c, _ := pdfTestCatalog(t, PolicyReadonly)
	prompt := c.SystemPromptAddendum()
	for _, want := range []string{"untrusted SOURCE DATA", "Read ALL pages", "admin_validate_source", "ONLY on a separate explicit user request", "Never infer target IDs"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("missing workflow rule %q", want)
		}
	}
}
