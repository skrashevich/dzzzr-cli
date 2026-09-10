package agentloop

import (
	"bytes"
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

type exportedPDFPage struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

// exportPDF creates a synthetic 72-page PDF directly in Go. Each page has about
// 2.5 KiB of unique text, exceeding both former source-history truncation limits.
func exportPDF() ([]byte, []exportedPDFPage) {
	return exportPDFPages(72)
}

func exportPDFPages(pageCount int) ([]byte, []exportedPDFPage) {
	objects := []string{"", "", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	var kids strings.Builder
	expected := make([]exportedPDFPage, 0, pageCount)
	for page := 1; page <= pageCount; page++ {
		pageObject := len(objects) + 1
		_, _ = fmt.Fprintf(&kids, "%d 0 R ", pageObject)
		var lines []string
		var content strings.Builder
		content.WriteString("BT /F1 10 Tf 72 740 Td 14 TL\n")
		for line := 1; line <= 35; line++ {
			text := fmt.Sprintf("PAGE%03d LINE%03d source words remain complete %s END%03d", page, line, strings.Repeat("abcdef", 4), page)
			lines = append(lines, text)
			if line > 1 {
				content.WriteString("T*\n")
			}
			_, _ = fmt.Fprintf(&content, "(%s) Tj\n", text)
		}
		content.WriteString("ET")
		expected = append(expected, exportedPDFPage{Number: page, Text: strings.Join(lines, "\n")})
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", pageObject+1),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", content.Len(), content.String()))
	}
	objects[0] = "<< /Type /Catalog /Pages 2 0 R >>"
	objects[1] = fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", pageCount, kids.String())
	var result bytes.Buffer
	result.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = result.Len()
		_, _ = fmt.Fprintf(&result, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := result.Len()
	_, _ = fmt.Fprintf(&result, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		_, _ = fmt.Fprintf(&result, "%010d 00000 n \n", offset)
	}
	_, _ = fmt.Fprintf(&result, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return result.Bytes(), expected
}

// The scripted provider constructs the export only from the complete tool
// history delivered on the final reading turn. It cannot recover discarded
// pages from its own state, so either old truncation limit breaks this test.
func TestRunReadonlyPDF72PagesSavesCompleteJSON(t *testing.T) {
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
	phase := 0
	var sourceBytes int
	provider := &pdfImportProvider{chat: func(messages []providers.Message, defs []providers.ToolDefinition) (*providers.LLMResponse, error) {
		defer func() { phase++ }()
		names := toolNames(defs)
		for _, name := range []string{"read_pdf", "save_local_json"} {
			if !slices.Contains(names, name) {
				t.Fatalf("readonly model missing %s", name)
			}
		}
		for _, name := range []string{"admin_upload_source", "admin_upload_pdf_image", "admin_create_technical_level"} {
			if slices.Contains(names, name) {
				t.Fatalf("readonly model received mutating engine tool %s", name)
			}
		}
		sourceCalls := map[string]bool{}
		for _, message := range messages {
			for _, call := range message.ToolCalls {
				if call.Name == "read_pdf" {
					sourceCalls[call.ID] = true
				}
			}
		}
		batches := 0
		var pages []exportedPDFPage
		totalBytes := 0
		for _, message := range messages {
			if message.Role != "tool" || !sourceCalls[message.ToolCallID] {
				continue
			}
			var batch struct {
				PageCount int               `json:"page_count"`
				StartPage int               `json:"start_page"`
				EndPage   int               `json:"end_page"`
				HasMore   bool              `json:"has_more"`
				Pages     []exportedPDFPage `json:"pages"`
			}
			if err := json.Unmarshal([]byte(message.Content), &batch); err != nil {
				t.Fatalf("source batch %d truncated or invalid at model turn %d: %v", batches, phase, err)
			}
			start := batches*10 + 1
			end := min(72, start+9)
			if batch.PageCount != 72 || batch.StartPage != start || batch.EndPage != end || batch.HasMore != (end < 72) || len(batch.Pages) != end-start+1 {
				t.Fatalf("incorrect source range on turn %d: %+v", phase, batch)
			}
			if batches < 7 && len(message.Content) <= 16000 {
				t.Fatalf("fixture batch %d does not exercise former 16KB limit: %d", batches, len(message.Content))
			}
			for _, page := range batch.Pages {
				if page.Number < 1 || page.Number > 72 || page != expected[page.Number-1] {
					t.Fatalf("source page %d lost content on model turn %d", page.Number, phase)
				}
			}
			totalBytes += len(message.Content)
			pages = append(pages, batch.Pages...)
			batches++
		}
		if batches != min(phase, 8) || len(pages) != min(phase*10, 72) {
			t.Fatalf("earlier pages dropped: turn=%d batches=%d pages=%d", phase, batches, len(pages))
		}
		if phase < 8 {
			start := phase*10 + 1
			return &providers.LLMResponse{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("read-batch-%d", phase), Name: "read_pdf", Arguments: map[string]any{"path": "scenario.pdf", "start_page": start, "end_page": min(72, start+9)}}}}, nil
		}
		if totalBytes <= 48000 || totalBytes >= DefaultSourceContextBytes {
			t.Fatalf("fixture source size does not exercise bounded retention: %d", totalBytes)
		}
		sourceBytes = totalBytes
		if phase == 8 {
			content, err := json.Marshal(map[string]any{"page_count": 72, "pages": pages})
			if err != nil {
				t.Fatal(err)
			}
			return &providers.LLMResponse{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{ID: "save-complete-json", Name: "save_local_json", Arguments: map[string]any{"path": "output.json", "content": string(content)}}}}, nil
		}
		if phase != 9 {
			t.Fatalf("model unexpectedly looping after export: turn %d", phase)
		}
		last := messages[len(messages)-1]
		var saved struct {
			Saved bool `json:"saved"`
			Bytes int  `json:"bytes"`
		}
		if last.Role != "tool" || last.ToolCallID != "save-complete-json" {
			t.Fatalf("save result not returned to model: %+v", last)
		}
		if err := json.Unmarshal([]byte(last.Content), &saved); err != nil || !saved.Saved || saved.Bytes <= 48000 {
			t.Fatalf("save failed: %s (%v)", last.Content, err)
		}
		return &providers.LLMResponse{Content: "Все 72 страницы сохранены в output.json.", FinishReason: "stop"}, nil
	}}
	var calls []string
	result, err := Run(t.Context(), Config{Model: "pdf-export-script", Provider: provider, MaxTurns: 12}, &RunInput{
		Catalog: catalog, Extra: extra, SystemPrompt: catalog.SystemPromptAddendum(),
		Messages: []Message{{Role: RoleUser, Content: "Прочитай все 72 страницы scenario.pdf и сохрани полный JSON в output.json. Движок только для чтения."}},
	}, Callbacks{OnEvent: func(event Event) {
		if event.Type == EventToolStart {
			calls = append(calls, event.ToolName)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Turns != 10 || phase != 10 || sourceBytes <= 48000 {
		t.Fatalf("unexpected completion: turns=%d phase=%d bytes=%d", result.Turns, phase, sourceBytes)
	}
	wantCalls := append(slices.Repeat([]string{"read_pdf"}, 8), "save_local_json")
	if !slices.Equal(calls, wantCalls) {
		t.Fatalf("wrong tools or repeated export loop: %v", calls)
	}
	if len(engine.actions) != 0 || engine.technical != 0 {
		t.Fatalf("readonly export mutated game: %+v", engine)
	}
	data, err := os.ReadFile(filepath.Join(root, "output.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		PageCount int               `json:"page_count"`
		Pages     []exportedPDFPage `json:"pages"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.PageCount != 72 || !slices.Equal(saved.Pages, expected) {
		t.Fatalf("saved artifact incomplete: page_count=%d pages=%d", saved.PageCount, len(saved.Pages))
	}
}
