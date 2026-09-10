package agentloop

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/dzzzr-cli/agentfiles"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/pdfsource"
)

func TestRunPDFMappingCopiesSourceWithoutModelTranscription(t *testing.T) {
	root := t.TempDir()
	data, err := os.ReadFile("../pdfsource/testdata/synthetic.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.pdf"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	engine := &pdfImportEngine{}
	catalog, err := agenttools.NewCatalog(engine, agenttools.Options{Policy: agenttools.PolicyReadonly, IncludeAdmin: true, FilesRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	files, err := agentfiles.Tools(root)
	if err != nil {
		t.Fatal(err)
	}
	extra := make([]Tool, len(files))
	for i, file := range files {
		extra[i] = file
	}
	phase := 0
	var hash string
	var ranges []pdfsource.Range
	var images []any
	imageURLs := map[string]string{}
	var sourceLines []string
	var modelResponses []string
	call := func(name string, args map[string]any) *providers.LLMResponse {
		response := &providers.LLMResponse{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("mapping-%d", phase), Name: name, Arguments: args}}}
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		modelResponses = append(modelResponses, string(encoded))
		return response
	}
	provider := &pdfImportProvider{chat: func(messages []providers.Message, defs []providers.ToolDefinition) (*providers.LLMResponse, error) {
		defer func() { phase++ }()
		names := toolNames(defs)
		for _, name := range []string{"index_pdf", "save_local_json", "extract_pdf", "admin_validate_source"} {
			if !slices.Contains(names, name) {
				t.Fatalf("readonly model missing %s", name)
			}
		}
		if slices.Contains(names, "admin_upload_source") {
			t.Fatal("readonly model received upload tool")
		}
		if phase > 0 {
			last := messages[len(messages)-1]
			if last.Role != "tool" || last.ToolCallID != fmt.Sprintf("mapping-%d", phase-1) {
				t.Fatalf("missing tool response: %+v", last)
			}
			switch phase {
			case 1, 2:
				var indexed pdfsource.IndexedDocument
				if err := json.Unmarshal([]byte(last.Content), &indexed); err != nil {
					t.Fatal(err)
				}
				if indexed.PageCount != 12 || indexed.HasMore != (phase == 1) || indexed.StartPage != (phase-1)*10+1 {
					t.Fatalf("wrong page chunk: %+v", indexed)
				}
				if phase == 1 {
					hash = indexed.SourceSHA256
				} else if indexed.SourceSHA256 != hash {
					t.Fatal("source hash changed")
				}
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
						sourceLines = append(sourceLines, line.Text)
					}
				}
			case 3:
				var saved struct {
					Saved bool `json:"saved"`
				}
				if err := json.Unmarshal([]byte(last.Content), &saved); err != nil || !saved.Saved {
					t.Fatalf("mapping save failed: %s %v", last.Content, err)
				}
			case 4:
				var report pdfsource.FileExtraction
				if err := json.Unmarshal([]byte(last.Content), &report); err != nil {
					t.Fatal(err)
				}
				if report.PageCount != 12 || report.ReferencedLines != len(sourceLines) || report.SourceSHA256 != hash {
					t.Fatalf("extraction report incorrect: %+v", report)
				}
				if strings.Contains(last.Content, sourceLines[0]) || strings.Contains(last.Content, `"question"`) {
					t.Fatal("extraction payload returned through model")
				}
			case 5:
				var validated struct {
					Valid  bool `json:"valid"`
					GameID int  `json:"game_id"`
					Levels int  `json:"levels"`
				}
				if err := json.Unmarshal([]byte(last.Content), &validated); err != nil || !validated.Valid || validated.GameID != 42 || validated.Levels != 1 {
					t.Fatalf("file validation failed: %s %v", last.Content, err)
				}
			}
		}
		switch phase {
		case 0, 1:
			return call("index_pdf", map[string]any{"path": "source.pdf", "start_page": phase*10 + 1, "end_page": min(12, phase*10+10)}), nil
		case 2:
			mapping := map[string]any{"version": 1, "source_sha256": hash, "image_urls": imageURLs, "output": map[string]any{"levels": []any{map[string]any{"params": map[string]any{"question": map[string]any{"$source": pdfsource.Selector{Ranges: ranges, Format: "text"}}, "comment": map[string]any{"$concat": images}}}}}}
			return call("save_local_json", map[string]any{"path": "mapping.json", "data": mapping}), nil
		case 3:
			return call("extract_pdf", map[string]any{"path": "source.pdf", "mapping_path": "mapping.json", "output_path": "plan.json"}), nil
		case 4:
			return call("admin_validate_source", map[string]any{"path": "plan.json", "game_id": 42, "mode": "create"}), nil
		case 5:
			return &providers.LLMResponse{Content: "Готово: plan.json.", FinishReason: "stop"}, nil
		default:
			t.Fatalf("unexpected model turn %d", phase)
			return nil, nil
		}
	}}
	var calls []string
	result, err := Run(t.Context(), Config{Model: "pdf-mapping-script", Provider: provider, MaxTurns: 8}, &RunInput{
		Catalog: catalog, Extra: extra, SystemPrompt: catalog.SystemPromptAddendum(), Messages: []Message{{Role: RoleUser, Content: "Извлеки source.pdf в JSON через схему ссылок на строки и проверь файл для игры 42. Движок не изменяй."}},
	}, Callbacks{OnEvent: func(event Event) {
		if event.Type == EventToolStart {
			calls = append(calls, event.ToolName)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(calls, []string{"index_pdf", "index_pdf", "save_local_json", "extract_pdf", "admin_validate_source"}) || result.Turns != 6 || phase != 6 {
		t.Fatalf("unexpected sequence: %v turns=%d phase=%d", calls, result.Turns, phase)
	}
	if len(engine.actions) != 0 || engine.technical != 0 {
		t.Fatalf("model wrote to engine: %+v", engine)
	}
	extracted, err := os.ReadFile(filepath.Join(root, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Levels []struct {
			Params struct {
				Question string `json:"question"`
				Comment  string `json:"comment"`
			} `json:"params"`
		} `json:"levels"`
	}
	if err := json.Unmarshal(extracted, &output); err != nil {
		t.Fatal(err)
	}
	expected := strings.Join(sourceLines, "\n")
	if len(sourceLines) == 0 || len(output.Levels) != 1 || output.Levels[0].Params.Question != expected {
		t.Fatalf("source text changed: %s", extracted)
	}
	if len(images) == 0 || strings.Count(output.Levels[0].Params.Comment, "<img ") != len(images) {
		t.Fatal("extraction lost image-only page references")
	}
	for _, response := range modelResponses {
		for _, line := range sourceLines {
			encoded, _ := json.Marshal(line)
			if strings.Contains(response, string(encoded)) {
				t.Fatalf("model transcribed source line %q", line)
			}
		}
	}
}
