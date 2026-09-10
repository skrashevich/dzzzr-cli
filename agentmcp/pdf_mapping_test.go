package agentmcp_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/pdfsource"
)

func TestPDFMappingThroughMCPReadonly(t *testing.T) {
	root := t.TempDir()
	data, err := os.ReadFile("../pdfsource/testdata/synthetic.pdf")
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("source.pdf", data)
	e := &sourceMCPEngine{stubEngine: stubEngine{admin: true}}
	catalog, err := agenttools.NewCatalog(e, agenttools.Options{Policy: agenttools.PolicyReadonly, IncludeAdmin: true, FilesRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	session := connect(t, catalog, mcp.NewClient(&mcp.Implementation{Name: "mapping-test", Version: "1"}, nil))
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "extract_pdf" && (tool.Annotations == nil || tool.Annotations.ReadOnlyHint) {
			t.Fatal("local file creation incorrectly advertised as read-only")
		}
	}
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	var ranges []pdfsource.Range
	var images []any
	imageURLs := map[string]string{}
	var texts []string
	var hash string
	var pageCount int
	for start := 1; ; {
		args := map[string]any{"path": "source.pdf", "start_page": start}
		result := call("index_pdf", args)
		if result.IsError {
			t.Fatal(contentText(result))
		}
		var indexed pdfsource.IndexedDocument
		if err := json.Unmarshal([]byte(contentText(result)), &indexed); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(contentText(result), `"data":`) {
			t.Fatal("index exposed image bytes")
		}
		if hash != "" && hash != indexed.SourceSHA256 {
			t.Fatal("source hash changed between chunks")
		}
		hash, pageCount = indexed.SourceSHA256, indexed.PageCount
		for _, page := range indexed.Pages {
			for _, img := range page.Images {
				images = append(images, map[string]any{"$source": pdfsource.Selector{Page: page.Number, ImageID: img.ImageID, Format: "image_html"}})
				imageURLs[img.ImageID] = "https://example.test/upload/" + img.ImageID + ".png"
			}
			for _, line := range page.Lines {
				if strings.TrimSpace(line.Text) == "" {
					continue
				}
				ranges = append(ranges, pdfsource.Range{Page: page.Number, StartLine: line.Number, EndLine: line.Number})
				texts = append(texts, line.Text)
			}
		}
		if !indexed.HasMore {
			break
		}
		if indexed.EndPage < start {
			t.Fatal("index failed to advance")
		}
		start = indexed.EndPage + 1
	}
	if pageCount != 12 || len(texts) == 0 {
		t.Fatalf("incomplete fixture index: pages=%d lines=%d", pageCount, len(texts))
	}
	selector := map[string]any{"$source": pdfsource.Selector{Ranges: ranges, Format: "text"}}
	mapping := map[string]any{"version": 1, "source_sha256": hash, "image_urls": imageURLs, "output": map[string]any{"levels": []any{map[string]any{"params": map[string]any{"question": selector, "comment": map[string]any{"$concat": images}}}}}}
	saveMapping := func(name string) {
		t.Helper()
		encoded, err := json.Marshal(mapping)
		if err != nil {
			t.Fatal(err)
		}
		write(name, encoded)
	}
	saveMapping("mapping.json")
	extractArgs := map[string]any{"path": "source.pdf", "mapping_path": "mapping.json", "output_path": "plan.json"}
	result := call("extract_pdf", extractArgs)
	if result.IsError {
		t.Fatal(contentText(result))
	}
	var report pdfsource.FileExtraction
	if err := json.Unmarshal([]byte(contentText(result)), &report); err != nil {
		t.Fatal(err)
	}
	if report.PageCount != 12 || report.ReferencedLines != len(texts) || report.SourceSHA256 != hash {
		t.Fatalf("invalid extraction report: %+v", report)
	}
	if strings.Contains(contentText(result), texts[0]) || strings.Contains(contentText(result), `"question"`) {
		t.Fatal("extraction returned document payload")
	}
	extracted, err := os.ReadFile(filepath.Join(root, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Levels []struct {
			Params struct {
				Question string `json:"question"`
				Comment  string `json:"comment"`
			} `json:"params"`
		} `json:"levels"`
	}
	if err := json.Unmarshal(extracted, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Levels) != 1 || plan.Levels[0].Params.Question != strings.Join(texts, "\n") {
		t.Fatalf("source text was changed: %s", extracted)
	}
	if len(images) == 0 || strings.Count(plan.Levels[0].Params.Comment, "<img ") != len(images) {
		t.Fatal("extraction lost image-only page references")
	}
	result = call("admin_validate_source", map[string]any{"path": "plan.json", "game_id": 42, "mode": "create"})
	if result.IsError {
		t.Fatal(contentText(result))
	}
	var validation struct {
		Valid  bool `json:"valid"`
		GameID int  `json:"game_id"`
		Levels int  `json:"levels"`
	}
	if err := json.Unmarshal([]byte(contentText(result)), &validation); err != nil {
		t.Fatal(err)
	}
	if !validation.Valid || validation.GameID != 42 || validation.Levels != 1 {
		t.Fatalf("invalid validation report: %+v", validation)
	}
	if strings.Contains(contentText(result), texts[0]) {
		t.Fatal("validation exposed source payload")
	}
	result = call("extract_pdf", extractArgs)
	if !result.IsError {
		t.Fatal("existing output was overwritten")
	}
	after, err := os.ReadFile(filepath.Join(root, "plan.json"))
	if err != nil || !bytes.Equal(after, extracted) {
		t.Fatalf("existing output changed: %v", err)
	}
	mapping["output"] = map[string]any{"question": "invented model text"}
	saveMapping("invalid.json")
	result = call("extract_pdf", map[string]any{"path": "source.pdf", "mapping_path": "invalid.json", "output_path": "invalid-output.json"})
	if !result.IsError {
		t.Fatal("literal mapping accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "invalid-output.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid mapping created output: %v", err)
	}
	if e.technical != 0 || e.uploads != 0 || e.creates != 0 {
		t.Fatalf("readonly workflow wrote to engine: %+v", e)
	}
}
