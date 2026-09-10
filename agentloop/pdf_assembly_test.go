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
)

func TestRunPDFInventoryAssemblyRefusesPartialThenCompletes(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	pdf, expected := exportPDF()
	if err := os.WriteFile(filepath.Join(root, "scenario.pdf"), pdf, 0600); err != nil {
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
	keys := []string{"first", "middle", "last"}
	titles := []string{"First section", "Middle section", "Last section"}
	phase := 0
	var pages []exportedPDFPage
	assertSaved := func(messages []providers.Message, levelCount int) {
		t.Helper()
		last := messages[len(messages)-1]
		var result struct {
			Saved      bool `json:"saved"`
			LevelCount int  `json:"level_count"`
		}
		if last.Role != "tool" {
			t.Fatalf("missing save/assembly response: %+v", last)
		}
		if err := json.Unmarshal([]byte(last.Content), &result); err != nil || !result.Saved || result.LevelCount != levelCount {
			t.Fatalf("save/assembly failed: %s, %v", last.Content, err)
		}
	}
	call := func(name string, args map[string]any) *providers.LLMResponse {
		return &providers.LLMResponse{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("assembly-step-%d", phase), Name: name, Arguments: args}}}
	}
	savePart := func(index int) *providers.LLMResponse {
		// Structured data is deliberately passed as an object, without escaped JSON
		// strings. Each part preserves a disjoint 24-page section of the source.
		return call("save_local_json", map[string]any{
			"path": keys[index] + ".json",
			"data": map[string]any{"key": keys[index], "title": titles[index], "pages": pages[index*24 : (index+1)*24], "notes": nil},
		})
	}
	provider := &pdfImportProvider{chat: func(messages []providers.Message, defs []providers.ToolDefinition) (*providers.LLMResponse, error) {
		defer func() { phase++ }()
		names := toolNames(defs)
		for _, name := range []string{"read_pdf", "save_local_json", "assemble_local_json"} {
			if !slices.Contains(names, name) {
				t.Fatalf("readonly model missing %s", name)
			}
		}
		if slices.Contains(names, "admin_upload_source") {
			t.Fatal("readonly model received game upload")
		}
		if phase >= 1 && phase <= 8 {
			last := messages[len(messages)-1]
			var batch struct {
				PageCount int               `json:"page_count"`
				HasMore   bool              `json:"has_more"`
				Pages     []exportedPDFPage `json:"pages"`
			}
			if last.Role != "tool" || last.ToolCallID != fmt.Sprintf("assembly-step-%d", phase-1) {
				t.Fatal("source response missing")
			}
			if err := json.Unmarshal([]byte(last.Content), &batch); err != nil {
				t.Fatal(err)
			}
			if batch.PageCount != 72 || batch.HasMore != (phase < 8) {
				t.Fatalf("incorrect source pagination: %+v", batch)
			}
			pages = append(pages, batch.Pages...)
		}
		if phase < 8 {
			return call("read_pdf", map[string]any{"path": "scenario.pdf", "start_page": phase*10 + 1, "end_page": min(72, phase*10+10)}), nil
		}
		switch phase {
		case 8:
			if !slices.Equal(pages, expected) {
				t.Fatal("inventory started before reading all 72 pages intact")
			}
			inventory := make([]any, len(keys))
			for i, key := range keys {
				inventory[i] = map[string]any{"key": key, "title": titles[i], "first_page": i*24 + 1, "last_page": (i + 1) * 24}
			}
			return call("save_local_json", map[string]any{"path": "manifest.json", "data": map[string]any{
				"source":   map[string]any{"path": "scenario.pdf", "page_count": 72},
				"metadata": map[string]any{"expected_level_count": 3, "uncertain_fields": nil},
				"levels":   inventory,
			}}), nil
		case 9:
			assertSaved(messages, 0)
			return savePart(2), nil
		case 10:
			assertSaved(messages, 0)
			return savePart(0), nil
		case 11:
			assertSaved(messages, 0)
			return call("assemble_local_json", map[string]any{"path": "scenario.json", "manifest": "manifest.json", "parts": []any{"last.json", "first.json"}}), nil
		case 12:
			last := messages[len(messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, "middle") || !strings.Contains(last.Content, "неполна") {
				t.Fatalf("partial coverage was not explicitly rejected: %+v", last)
			}
			if _, err := os.Stat(filepath.Join(root, "scenario.json")); !os.IsNotExist(err) {
				t.Fatalf("partial assembly produced an artifact: %v", err)
			}
			return savePart(1), nil
		case 13:
			assertSaved(messages, 0)
			return call("assemble_local_json", map[string]any{"path": "scenario.json", "manifest": "manifest.json", "parts": []any{"last.json", "middle.json", "first.json"}}), nil
		case 14:
			assertSaved(messages, 3)
			return &providers.LLMResponse{Content: "Все три раздела из 72 страниц собраны в scenario.json.", FinishReason: "stop"}, nil
		default:
			t.Fatalf("unexpected export loop at turn %d", phase)
			return nil, nil
		}
	}}
	var calls []string
	result, err := Run(t.Context(), Config{Model: "pdf-assembly-script", Provider: provider, MaxTurns: 20}, &RunInput{
		Catalog: catalog, Extra: extra, SystemPrompt: catalog.SystemPromptAddendum(),
		Messages: []Message{{Role: RoleUser, Content: "Прочитай scenario.pdf полностью, сохрани инвентарь трёх разделов и отдельные JSON-части, затем собери полный scenario.json. Движок не изменяй."}},
	}, Callbacks{OnEvent: func(event Event) {
		if event.Type == EventToolStart {
			calls = append(calls, event.ToolName)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	expectedCalls := append(slices.Repeat([]string{"read_pdf"}, 8), "save_local_json", "save_local_json", "save_local_json", "assemble_local_json", "save_local_json", "assemble_local_json")
	if !slices.Equal(calls, expectedCalls) || result.Turns != 15 || phase != 15 {
		t.Fatalf("wrong assembly sequence: calls=%v turns=%d phase=%d", calls, result.Turns, phase)
	}
	if len(engine.actions) != 0 || engine.technical != 0 {
		t.Fatalf("local assembly mutated engine: %+v", engine)
	}
	data, err := os.ReadFile(filepath.Join(root, "scenario.json"))
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Source struct {
			Path      string `json:"path"`
			PageCount int    `json:"page_count"`
		} `json:"source"`
		Metadata struct {
			ExpectedLevelCount int `json:"expected_level_count"`
		} `json:"metadata"`
		Levels []struct {
			Key   string            `json:"key"`
			Title string            `json:"title"`
			Pages []exportedPDFPage `json:"pages"`
			Notes *string           `json:"notes"`
		} `json:"levels"`
	}
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if output.Source.Path != "scenario.pdf" || output.Source.PageCount != 72 || output.Metadata.ExpectedLevelCount != 3 || len(output.Levels) != 3 {
		t.Fatalf("manifest metadata or complete inventory lost: %+v", output)
	}
	var assembled []exportedPDFPage
	for i, level := range output.Levels {
		if level.Key != keys[i] || level.Title != titles[i] || level.Notes != nil || len(level.Pages) != 24 {
			t.Fatalf("manifest order/part data lost at %d: %+v", i, level)
		}
		assembled = append(assembled, level.Pages...)
	}
	if !slices.Equal(assembled, expected) {
		t.Fatal("final assembled JSON lost or duplicated source pages")
	}
}
