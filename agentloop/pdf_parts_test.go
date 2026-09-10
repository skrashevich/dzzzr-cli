package agentloop

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/dzzzr-cli/agentfiles"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/pdfsource"
)

func TestRun119PagePDFWithIncrementalMappingParts(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	data, expected := exportPDFPages(119)
	if err := os.WriteFile(filepath.Join(root, "source.pdf"), data, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := agenttools.NewCatalog(&pdfImportEngine{}, agenttools.Options{Policy: agenttools.PolicyReadonly, FilesRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	files, err := agentfiles.Tools(root)
	if err != nil {
		t.Fatal(err)
	}
	extra := make([]Tool, len(files))
	for i, tool := range files {
		extra[i] = tool
	}
	phase := 0
	var parts []string
	call := func(name string, args map[string]any) *providers.LLMResponse {
		raw, err := json.Marshal(args)
		if err != nil || len(raw) > 8000 || strings.Contains(string(raw), "source words remain complete") {
			t.Fatalf("model response oversized or transcribes source: %d bytes %v", len(raw), err)
		}
		return &providers.LLMResponse{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{ID: fmt.Sprint(phase), Name: name, Arguments: args}}}
	}
	provider := &pdfImportProvider{chat: func(messages []providers.Message, _ []providers.ToolDefinition) (*providers.LLMResponse, error) {
		defer func() { phase++ }()
		if phase < 24 {
			batch := phase / 2
			if phase%2 == 0 {
				if batch > 0 {
					if _, err := os.Stat(filepath.Join(root, parts[batch-1])); err != nil {
						t.Fatal("next pages read before saving preceding mapping")
					}
				}
				return call("index_pdf", map[string]any{"path": "source.pdf", "start_page": batch*10 + 1, "end_page": min(119, batch*10+10)}), nil
			}
			var index pdfsource.IndexedDocument
			if err := json.Unmarshal([]byte(messages[len(messages)-1].Content), &index); err != nil {
				t.Fatal(err)
			}
			var levels []any
			for _, page := range index.Pages {
				levels = append(levels, map[string]any{"question": map[string]any{"$source": pdfsource.Selector{Ranges: []pdfsource.Range{{Page: page.Number, StartLine: 1, EndLine: len(page.Lines)}}}}})
			}
			name := fmt.Sprintf("part-%03d.json", batch+1)
			parts = append(parts, name)
			return call("save_local_json", map[string]any{"path": name, "data": map[string]any{"version": 1, "source_sha256": index.SourceSHA256, "output": map[string]any{"levels": levels}}}), nil
		}
		switch phase {
		case 24:
			return call("save_local_json", map[string]any{"path": "manifest.json", "data": pdfsource.MappingManifest{Version: 2, SourceSHA256: pdfsource.SourceHash(data), Parts: parts}}), nil
		case 25:
			return call("extract_pdf", map[string]any{"path": "source.pdf", "mapping_path": "manifest.json", "output_path": "result.json"}), nil
		case 26:
			var report pdfsource.FileExtraction
			if err := json.Unmarshal([]byte(messages[len(messages)-1].Content), &report); err != nil || report.PageCount != 119 {
				t.Fatalf("extraction failed: %+v %v", report, err)
			}
			return &providers.LLMResponse{FinishReason: "stop", Content: "Saved result.json"}, nil
		default:
			t.Fatalf("unexpected phase %d", phase)
			return nil, nil
		}
	}}
	_, err = Run(t.Context(), Config{Model: "script", Provider: provider, MaxTurns: 30}, &RunInput{Catalog: catalog, Extra: extra, Messages: []Message{{Role: RoleUser, Content: "Export source.pdf"}}}, Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Levels []struct {
			Question string `json:"question"`
		} `json:"levels"`
	}
	if err := json.Unmarshal(raw, &got); err != nil || len(got.Levels) != len(expected) {
		t.Fatalf("incomplete export: %d levels %v", len(got.Levels), err)
	}
	for i, page := range expected {
		if got.Levels[i].Question != page.Text {
			t.Fatalf("source changed on page %d", i+1)
		}
	}
}
