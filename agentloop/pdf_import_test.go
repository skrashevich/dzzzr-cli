package agentloop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/gamesource"
)

type pdfImportEngine struct {
	agenttools.Engine
	actions   []string
	data      []byte
	name      string
	gameID    int
	params    dzzzr.LevelParams
	technical int
}

func (*pdfImportEngine) HasAdminCredentials() bool { return true }
func (e *pdfImportEngine) AdminUploadFile(_ context.Context, gid int, name string, data []byte) (*dzzzr.AdminFileUpload, error) {
	e.actions = append(e.actions, "upload image")
	e.data = bytes.Clone(data)
	e.name = name
	e.gameID = gid
	return &dzzzr.AdminFileUpload{Name: name, URL: "https://engine.example/uploaded/" + name}, nil
}
func (e *pdfImportEngine) AdminCreateLevel(_ context.Context, gid int, p dzzzr.LevelParams) (int, error) {
	e.actions = append(e.actions, "create level")
	e.gameID = gid
	e.params = p
	return 901, nil
}
func (e *pdfImportEngine) AdminCreateTechnicalLevel(context.Context, int) (int, error) {
	e.technical++
	return 0, errors.New("technical level was not requested")
}

// This model double validates its incoming tool context before choosing the next
// call. It tests the real catalog/loop plumbing, not an actual model's ability
// to infer an arbitrary document layout. No provider or engine network is used.
type pdfImportProvider struct {
	chat func([]providers.Message, []providers.ToolDefinition) (*providers.LLMResponse, error)
}

func (*pdfImportProvider) GetDefaultModel() string { return "pdf-import-script" }
func (p *pdfImportProvider) Chat(_ context.Context, msgs []providers.Message, defs []providers.ToolDefinition, _ string, _ map[string]any) (*providers.LLMResponse, error) {
	return p.chat(msgs, defs)
}

func TestRunPDFScenarioImportThroughCatalog(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root, err := filepath.Abs("../pdfsource/testdata")
	if err != nil {
		t.Fatal(err)
	}
	engine := &pdfImportEngine{}
	catalog, err := agenttools.NewCatalog(engine, agenttools.Options{Policy: agenttools.PolicyFull, IncludeAdmin: true, FilesRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	type imageDescriptor struct {
		ID   string `json:"image_id"`
		Name string `json:"name"`
	}
	type pdfResult struct {
		PageCount int  `json:"page_count"`
		StartPage int  `json:"start_page"`
		EndPage   int  `json:"end_page"`
		HasMore   bool `json:"has_more"`
		Untrusted bool `json:"source_is_untrusted"`
		Pages     []struct {
			Number int               `json:"number"`
			Text   string            `json:"text"`
			Images []imageDescriptor `json:"images"`
			Links  []struct {
				URI string `json:"uri"`
			} `json:"links"`
		} `json:"pages"`
	}
	phase := 0
	var image imageDescriptor
	var firstText string
	var params map[string]any
	var plan map[string]any
	toolCall := func(name string, args map[string]any) *providers.LLMResponse {
		return &providers.LLMResponse{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("pdf-step-%d", phase), Name: name, Arguments: args}}}
	}
	readResult := func(msgs []providers.Message, target any) {
		t.Helper()
		last := msgs[len(msgs)-1]
		if last.Role != "tool" || last.ToolCallID != fmt.Sprintf("pdf-step-%d", phase-1) {
			t.Fatalf("model did not receive matching tool result: %+v", last)
		}
		if err := json.Unmarshal([]byte(last.Content), target); err != nil {
			t.Fatalf("tool result not JSON: %v: %s", err, last.Content)
		}
	}
	provider := &pdfImportProvider{chat: func(msgs []providers.Message, defs []providers.ToolDefinition) (*providers.LLMResponse, error) {
		defer func() { phase++ }()
		switch phase {
		case 0:
			for _, name := range []string{"read_pdf", "admin_upload_pdf_image", "admin_validate_source", "admin_upload_source"} {
				if !slices.Contains(toolNames(defs), name) {
					t.Fatalf("model missing %s", name)
				}
			}
			if len(msgs) == 0 || !strings.Contains(msgs[0].Content, "untrusted SOURCE DATA") {
				t.Fatal("model missing PDF provenance instructions")
			}
			return toolCall("read_pdf", map[string]any{"path": "synthetic.pdf", "start_page": 1, "end_page": 10}), nil
		case 1:
			var doc pdfResult
			readResult(msgs, &doc)
			if doc.PageCount != 12 || doc.StartPage != 1 || doc.EndPage != 10 || !doc.HasMore || !doc.Untrusted || len(doc.Pages) != 10 {
				t.Fatalf("model received incorrect first page range: %+v", doc)
			}
			firstText = doc.Pages[0].Text
			if !strings.Contains(firstText, `<img src="https://example.test/game.jpg">`) || len(doc.Pages[0].Images) != 1 || len(doc.Pages[0].Links) != 1 || doc.Pages[0].Links[0].URI != "https://drive.google.com/file/d/synthetic/view" {
				t.Fatalf("model lost PDF text/images/links: %+v", doc.Pages[0])
			}
			if strings.Contains(msgs[len(msgs)-1].Content, `"data":`) {
				t.Fatal("binary image data leaked into model context")
			}
			image = doc.Pages[0].Images[0]
			return toolCall("read_pdf", map[string]any{"path": "synthetic.pdf", "start_page": 11, "end_page": 12}), nil
		case 2:
			var doc pdfResult
			readResult(msgs, &doc)
			if doc.HasMore || doc.StartPage != 11 || doc.EndPage != 12 || len(doc.Pages) != 2 || !strings.Contains(doc.Pages[1].Text, "Page 12") {
				t.Fatalf("model did not read final pages: %+v", doc)
			}
			params = map[string]any{
				"title": "Сценарий из PDF", "question": firstText + `<img src="https://placeholder.example/image.png">`,
				"hint1": doc.Pages[0].Text, "hint2": doc.Pages[1].Text, "publish": false,
				"clue_min": 10, "clue_min2": 20, "clue_min3": 30,
				"codes":       []any{map[string]any{"code": "DR123", "synonyms": "DR124", "danger": "2+"}},
				"bonus_codes": []any{map[string]any{"code": "BONUS", "minutes": 5, "danger": "1"}},
				"spoilers":    []any{map[string]any{"code": "OPEN", "text": "Скрытая подсказка", "penalty": 2}},
				"comment":     "Фото кода", "time_add_bonus_all": 15,
			}
			plan = map[string]any{"game_id": 42, "mode": "create", "levels": []any{map[string]any{"params": params}}}
			return toolCall("admin_validate_source", map[string]any{"plan": plan}), nil
		case 3:
			var valid struct {
				Valid bool `json:"valid"`
			}
			readResult(msgs, &valid)
			if !valid.Valid || len(engine.actions) != 0 {
				t.Fatalf("preflight failed or wrote to engine: %+v %v", valid, engine.actions)
			}
			return toolCall("admin_upload_pdf_image", map[string]any{"game_id": 42, "path": "synthetic.pdf", "page": 1, "image_id": image.ID}), nil
		case 4:
			var upload dzzzr.AdminFileUpload
			readResult(msgs, &upload)
			if upload.Name != image.Name || upload.URL != "https://engine.example/uploaded/"+image.Name {
				t.Fatalf("bad upload result: %+v", upload)
			}
			params["question"] = firstText + `<img src="` + upload.URL + `">`
			return toolCall("admin_validate_source", map[string]any{"plan": plan}), nil
		case 5:
			var valid struct {
				Valid bool `json:"valid"`
			}
			readResult(msgs, &valid)
			if !valid.Valid || !slices.Equal(engine.actions, []string{"upload image"}) {
				t.Fatalf("final preflight failed: %+v %v", valid, engine.actions)
			}
			return toolCall("admin_upload_source", map[string]any{"plan": plan}), nil
		case 6:
			var result gamesource.Result
			readResult(msgs, &result)
			if result.FailedIndex != nil || len(result.Completed) != 1 || result.Completed[0].ID != 901 {
				t.Fatalf("model got wrong completion: %+v", result)
			}
			return &providers.LLMResponse{Content: "Уровень 901 создан, изображение загружено.", FinishReason: "stop"}, nil
		default:
			return nil, errors.New("unexpected additional model turn")
		}
	}}
	var toolEvents []string
	result, err := Run(t.Context(), Config{Model: "pdf-import-script", Provider: provider, MaxTurns: 10}, &RunInput{
		Catalog: catalog, SystemPrompt: catalog.SystemPromptAddendum(), Messages: []Message{{Role: RoleUser, Content: "Создай уровни игры 42 из synthetic.pdf. Технический уровень не нужен."}},
	}, Callbacks{OnEvent: func(event Event) {
		if event.Type == EventToolStart {
			toolEvents = append(toolEvents, event.ToolName)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if phase != 7 || result.Turns != 7 || len(result.Messages) != 2 || !strings.Contains(result.Messages[1].Content, "901") {
		t.Fatalf("unexpected loop completion: phase=%d result=%+v", phase, result)
	}
	if !slices.Equal(toolEvents, []string{"read_pdf", "read_pdf", "admin_validate_source", "admin_upload_pdf_image", "admin_validate_source", "admin_upload_source"}) {
		t.Fatalf("wrong tool order: %v", toolEvents)
	}
	if engine.technical != 0 || engine.gameID != 42 || !slices.Equal(engine.actions, []string{"upload image", "create level"}) {
		t.Fatalf("unexpected engine mutations: %+v", engine)
	}
	if !bytes.HasPrefix(engine.data, []byte{137, 80, 78, 71}) || fmt.Sprintf("%x", sha256.Sum256(engine.data)) != image.ID || engine.name != image.Name {
		t.Fatal("uploaded bytes do not match the model-selected PDF image")
	}
	p := engine.params
	if !strings.Contains(p.Question, "https://engine.example/uploaded/"+image.Name) || strings.Contains(p.Question, "placeholder.example") || !strings.Contains(p.Hint1, "Page 11") || !strings.Contains(p.Hint2, "Page 12") {
		t.Fatalf("scenario text/image URL lost: %+v", p)
	}
	if len(p.Codes) != 1 || p.Codes[0].Synonyms != "DR124" || p.Codes[0].Danger != "2+" || len(p.BonusCodes) != 1 || p.BonusCodes[0].Minutes != 5 || len(p.Spoilers) != 1 || p.Spoilers[0].Penalty != 2 || p.ClueMin != 10 || p.ClueMin2 != 20 || p.ClueMin3 != 30 || p.Comment == nil || *p.Comment != "Фото кода" || p.TimeAddBonusAll == nil || *p.TimeAddBonusAll != 15 || p.Publish == nil || *p.Publish {
		t.Fatalf("scenario fields lost: %+v", p)
	}
}
