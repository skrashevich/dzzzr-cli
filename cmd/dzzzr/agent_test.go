package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/dzzzr-cli/agentloop"
	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// scriptedProvider replays canned model replies instead of calling one.
type scriptedProvider struct {
	replies []*providers.LLMResponse
	failure error
	calls   int
	tools   []providers.ToolDefinition
}

func (p *scriptedProvider) GetDefaultModel() string { return "scripted" }

func (p *scriptedProvider) Chat(
	_ context.Context,
	_ []providers.Message,
	toolDefs []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	p.tools = toolDefs
	if p.failure != nil {
		return nil, p.failure
	}
	if p.calls >= len(p.replies) {
		return nil, errors.New("scriptedProvider: реплики закончились")
	}
	r := p.replies[p.calls]
	p.calls++
	return r, nil
}

// useProvider points the agent commands at a scripted provider for one test.
func useProvider(t *testing.T, p providers.LLMProvider) {
	t.Helper()
	agentConfigHook = func(c *agentloop.Config) { c.Provider = p }
	t.Cleanup(func() { agentConfigHook = nil })
}

func answer(text string) *providers.LLMResponse {
	return &providers.LLMResponse{Content: text, FinishReason: "stop"}
}

func TestAgentAnswersAndReports(t *testing.T) {
	isolate(t)
	t.Setenv("DZZZR_LLM_API_KEY", "тест")
	provider := &scriptedProvider{replies: []*providers.LLMResponse{answer("Уровень взят.")}}
	useProvider(t, provider)

	code, out, errOut := runCLI(t, "agent", "что с уровнем")
	if code != 0 {
		t.Fatalf("код выхода %d, stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "Уровень взят.") {
		t.Errorf("ответ модели не напечатан в stdout: %q", out)
	}
	if strings.Contains(out, "Отчёт о выполнении") {
		t.Errorf("отчёт попал в stdout вместе с ответом: %q", out)
	}
	if !strings.Contains(errOut, "Отчёт о выполнении") {
		t.Errorf("отчёт не напечатан в stderr: %q", errOut)
	}
	if provider.calls != 1 {
		t.Errorf("провайдер вызван %d раз, ожидался 1", provider.calls)
	}
}

func TestAgentPassesEngineToolsToModel(t *testing.T) {
	isolate(t)
	t.Setenv("DZZZR_LLM_API_KEY", "тест")
	provider := &scriptedProvider{replies: []*providers.LLMResponse{answer("готово")}}
	useProvider(t, provider)

	if code, _, errOut := runCLI(t, "agent", "-security", "readonly", "покажи статус"); code != 0 {
		t.Fatalf("код выхода %d, stderr:\n%s", code, errOut)
	}

	var names []string
	for _, d := range provider.tools {
		names = append(names, d.Function.Name)
	}
	for _, want := range []string{"game_status", "level_info", "read_local_file", "save_local_json", "assemble_local_json"} {
		if !strings.Contains(strings.Join(names, " "), want) {
			t.Errorf("инструмент %q не передан модели; передано: %v", want, names)
		}
	}
	if strings.Contains(strings.Join(names, " "), "send_code") {
		t.Errorf("в режиме readonly модель видит send_code: %v", names)
	}
}

func TestAgentJSONOutput(t *testing.T) {
	isolate(t)
	t.Setenv("DZZZR_LLM_API_KEY", "тест")
	useProvider(t, &scriptedProvider{replies: []*providers.LLMResponse{answer("Всё готово.")}})

	code, out, errOut := runCLI(t, "-json", "agent", "статус")
	if code != 0 {
		t.Fatalf("код выхода %d, stderr:\n%s", code, errOut)
	}
	var got agentOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout не JSON (%v): %s", err, out)
	}
	if got.Answer != "Всё готово." {
		t.Errorf("answer = %q", got.Answer)
	}
	if got.Turns != 1 {
		t.Errorf("turns = %d, ожидался 1", got.Turns)
	}
}

func TestAgentWithoutAPIKeyFails(t *testing.T) {
	isolate(t)
	code, _, errOut := runCLI(t, "agent", "статус")
	if code != 1 {
		t.Errorf("код выхода %d, ожидался 1", code)
	}
	if !strings.Contains(errOut, "DZZZR_LLM_API_KEY") {
		t.Errorf("сообщение не называет нужную переменную: %q", errOut)
	}
}

func TestAgentAllowsLocalEndpointWithoutAPIKey(t *testing.T) {
	isolate(t)
	t.Setenv("DZZZR_LLM_BASE_URL", "http://127.0.0.1:11434/v1")
	useProvider(t, &scriptedProvider{replies: []*providers.LLMResponse{answer("локально")}})

	if code, out, errOut := runCLI(t, "agent", "статус"); code != 0 {
		t.Fatalf("локальный прокси без ключа отклонён: код %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestAgentWithoutPromptFails(t *testing.T) {
	isolate(t)
	t.Setenv("DZZZR_LLM_API_KEY", "тест")
	code, _, errOut := runCLI(t, "agent")
	if code != 1 {
		t.Errorf("код выхода %d, ожидался 1", code)
	}
	if !strings.Contains(errOut, "укажите запрос") {
		t.Errorf("сообщение не объясняет, чего не хватает: %q", errOut)
	}
}

func TestAgentRejectsUnknownSecurity(t *testing.T) {
	isolate(t)
	t.Setenv("DZZZR_LLM_API_KEY", "тест")
	useProvider(t, &scriptedProvider{replies: []*providers.LLMResponse{answer("готово")}})

	code, _, errOut := runCLI(t, "agent", "-security", "маленько", "статус")
	if code != 1 {
		t.Errorf("код выхода %d, ожидался 1", code)
	}
	if !strings.Contains(errOut, "readonly") {
		t.Errorf("сообщение не перечисляет допустимые значения: %q", errOut)
	}
}

func TestAgentReportsProviderFailure(t *testing.T) {
	isolate(t)
	t.Setenv("DZZZR_LLM_API_KEY", "тест")
	useProvider(t, &scriptedProvider{failure: errors.New("HTTP 401 unauthorized")})

	code, _, errOut := runCLI(t, "agent", "статус")
	if code != 1 {
		t.Errorf("код выхода %d, ожидался 1", code)
	}
	if !strings.Contains(errOut, "401") {
		t.Errorf("причина отказа не показана: %q", errOut)
	}
}

func TestTerminalConfirmer(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"да\n", true},
		{"n\n", false},
		{"\n", false},
		{"что?\n", false},
	}
	for _, tc := range tests {
		t.Run(strings.TrimSpace(tc.input), func(t *testing.T) {
			var errOut bytes.Buffer
			cfg := &config{stderr: &errOut, stdinReader: bufio.NewReader(strings.NewReader(tc.input))}
			got, err := (&terminalConfirmer{cfg: cfg}).ConfirmToolCall(t.Context(),
				agenttools.ConfirmRequest{Tool: "send_code", Args: map[string]any{"code": "ЛУНА"}})
			if err != nil {
				t.Fatalf("ConfirmToolCall: %v", err)
			}
			if got != tc.want {
				t.Errorf("ответ %q дал %v, ожидалось %v", tc.input, got, tc.want)
			}
			if !strings.Contains(errOut.String(), "send_code(code=ЛУНА)") {
				t.Errorf("запрос не показывает вызов: %q", errOut.String())
			}
		})
	}
}

func TestTerminalConfirmerRefusesOnClosedInput(t *testing.T) {
	var errOut bytes.Buffer
	cfg := &config{stderr: &errOut, stdinReader: bufio.NewReader(strings.NewReader(""))}
	got, err := (&terminalConfirmer{cfg: cfg}).ConfirmToolCall(t.Context(),
		agenttools.ConfirmRequest{Tool: "send_code"})
	if got {
		t.Error("закрытый ввод принят за согласие")
	}
	if err == nil {
		t.Error("закрытый ввод должен возвращать ошибку")
	}
}

func TestEncodeConfirmArgsIsStable(t *testing.T) {
	args := map[string]any{"level": 3, "code": "ЛУНА", "hint": 1}
	first := encodeConfirmArgs(args)
	for range 20 {
		if got := encodeConfirmArgs(args); got != first {
			t.Fatalf("порядок аргументов нестабилен: %q против %q", got, first)
		}
	}
	if first != "(code=ЛУНА, hint=1, level=3)" {
		t.Errorf("аргументы отрисованы как %q", first)
	}
	if encodeConfirmArgs(nil) != "" {
		t.Error("вызов без аргументов должен давать пустую строку")
	}
}

func TestFormatMarkdownForTerminalAlignsTables(t *testing.T) {
	in := "Итоги:\n\n| Уровень | Код |\n|---|:--:|\n| 1 | ЛУНА |\n| 12 | СОЛНЦЕ |\n\nВсё."
	got := formatMarkdownForTerminal(in)
	for _, want := range []string{
		"  Уровень  Код",
		"  -------  ------",
		"  1        ЛУНА",
		"  12       СОЛНЦЕ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("в выводе нет строки %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "|") {
		t.Errorf("разметка таблицы осталась в выводе:\n%s", got)
	}
	if !strings.Contains(got, "Итоги:") || !strings.Contains(got, "Всё.") {
		t.Errorf("текст вокруг таблицы потерян:\n%s", got)
	}
}

func TestFormatMarkdownForTerminalLeavesProseAlone(t *testing.T) {
	in := "# Заголовок\n\n- пункт\n- ещё пункт\n\n```\nкод\n```"
	if got := formatMarkdownForTerminal(in); got != in {
		t.Errorf("текст без таблиц изменён:\n%s", got)
	}
}
