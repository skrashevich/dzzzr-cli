package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// stubEngine satisfies the catalog's engine interface without reaching an
// engine. Only the methods the tests actually exercise are defined; the
// embedded interface supplies the rest, and calling one would panic loudly
// rather than pass silently.
type stubEngine struct{ agenttools.Engine }

func (stubEngine) HasAdminCredentials() bool { return false }

// turn is one canned model reply.
type turn struct {
	content   string
	toolCalls []providers.ToolCall
	usage     *providers.UsageInfo
	err       error
}

// fakeProvider replays canned replies and records what it was asked.
type fakeProvider struct {
	turns []turn
	calls int
	tools []providers.ToolDefinition
}

func (p *fakeProvider) GetDefaultModel() string { return "fake" }

func (p *fakeProvider) Chat(
	_ context.Context,
	_ []providers.Message,
	toolDefs []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	p.tools = toolDefs
	if p.calls >= len(p.turns) {
		return nil, errors.New("fakeProvider: no reply left")
	}
	t := p.turns[p.calls]
	p.calls++
	if t.err != nil {
		return nil, t.err
	}
	finish := "stop"
	if len(t.toolCalls) > 0 {
		finish = "tool_calls"
	}
	return &providers.LLMResponse{
		Content:      t.content,
		ToolCalls:    t.toolCalls,
		FinishReason: finish,
		Usage:        t.usage,
	}, nil
}

// echoTool returns its argument, so a test can watch a tool call travel
// through the loop without an engine behind it.
type echoTool struct {
	calls int
	fail  bool
}

func (*echoTool) Name() string        { return "echo" }
func (*echoTool) Description() string { return "Возвращает переданный текст." }
func (*echoTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"text": map[string]any{"type": "string"}},
		"required":   []string{"text"},
	}
}

func (t *echoTool) Execute(_ context.Context, args map[string]any) agenttools.Result {
	t.calls++
	text, _ := args["text"].(string)
	if t.fail {
		return agenttools.Result{Content: "echo failed", IsError: true}
	}
	return agenttools.Result{Content: `{"echo":"` + text + `"}`}
}

func toolNames(defs []providers.ToolDefinition) []string {
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Function.Name)
	}
	return names
}

func TestRunRegistersCatalogToolsUnderPolicy(t *testing.T) {
	catalog, err := agenttools.NewCatalog(stubEngine{}, agenttools.Options{Policy: agenttools.PolicyReadonly})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	provider := &fakeProvider{turns: []turn{{content: "готово"}}}

	if _, err := Run(t.Context(), Config{Model: "fake", Provider: provider}, &RunInput{
		Catalog:      catalog,
		Messages:     []Message{{Role: RoleUser, Content: "что со мной"}},
		SystemPrompt: "ты агент",
	}, Callbacks{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := toolNames(provider.tools)
	for _, want := range []string{"game_status", "level_info", "games_list"} {
		if !slices.Contains(got, want) {
			t.Errorf("инструмент %q не передан модели; передано: %v", want, got)
		}
	}
	// Under the read-only policy the catalog withholds mutating tools, and the
	// loop must not put them back.
	if slices.Contains(got, "send_code") {
		t.Errorf("мутирующий инструмент send_code виден модели в режиме readonly: %v", got)
	}
}

func TestRunExecutesToolAndReportsRun(t *testing.T) {
	echo := &echoTool{}
	provider := &fakeProvider{turns: []turn{
		{
			toolCalls: []providers.ToolCall{{
				ID:        "call-1",
				Name:      "echo",
				Arguments: map[string]any{"text": "привет"},
			}},
			usage: &providers.UsageInfo{PromptTokens: 10, CompletionTokens: 4},
		},
		{content: "Сказал привет.", usage: &providers.UsageInfo{PromptTokens: 20, CompletionTokens: 6}},
	}}

	var events []Event
	res, err := Run(t.Context(), Config{Model: "fake", Provider: provider}, &RunInput{
		Extra:    []Tool{echo},
		Messages: []Message{{Role: RoleUser, Content: "поздоровайся"}},
	}, Callbacks{OnEvent: func(ev Event) { events = append(events, ev) }})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if echo.calls != 1 {
		t.Errorf("инструмент вызван %d раз, ожидался 1", echo.calls)
	}
	if want := 2; res.Turns != want {
		t.Errorf("Turns = %d, ожидалось %d", res.Turns, want)
	}
	if len(res.Messages) != 2 || res.Messages[1].Role != RoleAssistant ||
		res.Messages[1].Content != "Сказал привет." {
		t.Errorf("ответ модели не добавлен в историю: %+v", res.Messages)
	}

	var kinds []string
	for _, ev := range events {
		kinds = append(kinds, ev.Type)
	}
	want := []string{EventToolStart, EventToolDone, EventAssistantText, EventReport, EventDone}
	if !slices.Equal(kinds, want) {
		t.Errorf("события %v, ожидались %v", kinds, want)
	}
	for _, ev := range events {
		if ev.Type == EventToolDone && !strings.Contains(ev.ToolResult, `"echo":"привет"`) {
			t.Errorf("результат инструмента не проброшен: %q", ev.ToolResult)
		}
	}

	for _, want := range []string{
		"Запросов к LLM:  2",
		"Вызовов тулзов:  1",
		"Токены:          40 (вход: 30, выход: 10)",
	} {
		if !strings.Contains(res.Report, want) {
			t.Errorf("в отчёте нет строки %q:\n%s", want, res.Report)
		}
	}
}

func TestRunReportsToolArgumentsAsJSON(t *testing.T) {
	echo := &echoTool{}
	provider := &fakeProvider{turns: []turn{
		{toolCalls: []providers.ToolCall{{
			ID:        "call-1",
			Name:      "echo",
			Arguments: map[string]any{"text": "привет"},
		}}},
		{content: "готово"},
	}}

	var args string
	if _, err := Run(t.Context(), Config{Model: "fake", Provider: provider}, &RunInput{
		Extra: []Tool{echo},
	}, Callbacks{OnEvent: func(ev Event) {
		if ev.Type == EventToolStart {
			args = ev.ToolArgs
		}
	}}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(args), &decoded); err != nil {
		t.Fatalf("аргументы не разбираются как JSON (%v): %s", err, args)
	}
	if decoded["text"] != "привет" {
		t.Errorf("аргументы вызова потеряны: %s", args)
	}
}

// PicoClaw validates a tool call against the declared schema before the tool
// runs, so a call the model got wrong comes back as an error result instead of
// reaching the engine.
func TestRunRejectsToolCallThatDoesNotMatchSchema(t *testing.T) {
	echo := &echoTool{}
	provider := &fakeProvider{turns: []turn{
		{toolCalls: []providers.ToolCall{{
			ID:        "call-1",
			Name:      "echo",
			Arguments: map[string]any{"wrong": "привет"},
		}}},
		{content: "готово"},
	}}

	if _, err := Run(t.Context(), Config{Model: "fake", Provider: provider}, &RunInput{
		Extra: []Tool{echo},
	}, Callbacks{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if echo.calls != 0 {
		t.Errorf("инструмент выполнен с аргументами, не отвечающими схеме (%d вызовов)", echo.calls)
	}
}

func TestRunRetriesTransientProviderFailure(t *testing.T) {
	prev := retryDelay
	retryDelay = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { retryDelay = prev })

	provider := &fakeProvider{turns: []turn{
		{err: errors.New("HTTP 503 service unavailable")},
		{content: "получилось"},
	}}

	res, err := Run(t.Context(), Config{Model: "fake", Provider: provider}, &RunInput{
		Extra: []Tool{&echoTool{}},
	}, Callbacks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if provider.calls != 2 {
		t.Errorf("провайдер вызван %d раз, ожидалось 2", provider.calls)
	}
	if len(res.Messages) != 1 || res.Messages[0].Content != "получилось" {
		t.Errorf("ответ после повтора не сохранён: %+v", res.Messages)
	}
	// One successful request; the failed attempt must not be counted as a turn.
	if !strings.Contains(res.Report, "Запросов к LLM:  1") {
		t.Errorf("неудачная попытка засчитана в отчёте:\n%s", res.Report)
	}
}

func TestRunDoesNotRetryPermanentProviderFailure(t *testing.T) {
	prev := retryDelay
	retryDelay = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { retryDelay = prev })

	provider := &fakeProvider{turns: []turn{
		{err: errors.New("HTTP 401 unauthorized")},
		{content: "не должно быть достигнуто"},
	}}

	var failure Event
	_, err := Run(t.Context(), Config{Model: "fake", Provider: provider}, &RunInput{
		Extra: []Tool{&echoTool{}},
	}, Callbacks{OnEvent: func(ev Event) {
		if ev.Type == EventError {
			failure = ev
		}
	}})
	if err == nil {
		t.Fatal("ожидалась ошибка, получен успех")
	}
	if provider.calls != 1 {
		t.Errorf("провайдер вызван %d раз, повтор был не нужен", provider.calls)
	}
	if failure.Type != EventError {
		t.Error("событие error не отправлено")
	}
}

func TestRunRequiresToolsAndModel(t *testing.T) {
	if _, err := Run(t.Context(), Config{Model: "fake"}, &RunInput{}, Callbacks{}); err == nil {
		t.Error("запуск без инструментов должен завершаться ошибкой")
	}
	if _, err := Run(t.Context(), Config{}, &RunInput{Extra: []Tool{&echoTool{}}}, Callbacks{}); err == nil {
		t.Error("запуск без модели должен завершаться ошибкой")
	}
	if _, err := Run(t.Context(), Config{Model: "fake"}, nil, Callbacks{}); err == nil {
		t.Error("запуск без RunInput должен завершаться ошибкой")
	}
}

func TestRetryableClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"rate limit", errors.New("HTTP 429 rate limit exceeded"), true},
		{"gateway", errors.New("HTTP 502 bad gateway"), true},
		{"timeout", errors.New("context deadline exceeded"), true},
		{"unauthorized", errors.New("HTTP 401 unauthorized"), false},
		{"unknown model", errors.New("invalid model: nope"), false},
		{"unrelated", errors.New("диск переполнен"), false},
		{"failover network", &providers.FailoverError{Reason: providers.FailoverNetwork}, true},
		{"failover overloaded", &providers.FailoverError{Reason: providers.FailoverOverloaded}, true},
		{"failover auth", &providers.FailoverError{Reason: providers.FailoverAuth}, false},
		{"failover format", &providers.FailoverError{Reason: providers.FailoverFormat}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryable(tc.err); got != tc.want {
				t.Errorf("retryable(%v) = %v, ожидалось %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestFormatReportCost(t *testing.T) {
	s := snapshot{
		llmDuration:      2 * time.Second,
		toolDuration:     time.Second,
		turns:            3,
		toolCalls:        2,
		promptTokens:     1000,
		completionTokens: 500,
	}

	paid := formatReport("m", &pricing{promptPerToken: 1e-6, completionPerToken: 2e-6}, 4*time.Second, s)
	if !strings.Contains(paid, "Стоимость:       $0.0020") {
		t.Errorf("стоимость посчитана неверно:\n%s", paid)
	}
	if !strings.Contains(paid, "Накладные:     1s") {
		t.Errorf("накладные расходы посчитаны неверно:\n%s", paid)
	}

	local := formatReport("m", &pricing{isLocal: true}, 4*time.Second, s)
	if !strings.Contains(local, "$0 (локальный прокси)") {
		t.Errorf("локальный прокси не отмечен:\n%s", local)
	}

	unknown := formatReport("m", nil, 4*time.Second, s)
	if strings.Contains(unknown, "Стоимость") {
		t.Errorf("без тарифов строки о стоимости быть не должно:\n%s", unknown)
	}
}

func TestFormatReportOmitsTokensWhenProviderReportsNone(t *testing.T) {
	report := formatReport("m", nil, time.Second, snapshot{turns: 1})
	if strings.Contains(report, "Токены") {
		t.Errorf("строка о токенах напечатана без данных:\n%s", report)
	}
}

func TestIsLocalBaseURL(t *testing.T) {
	for _, url := range []string{
		"http://localhost:8080/v1",
		"http://127.0.0.1:1234/v1",
		"http://[::1]:1234/v1",
	} {
		if !isLocalBaseURL(url) {
			t.Errorf("%q должен считаться локальным", url)
		}
	}
	if isLocalBaseURL("https://openrouter.ai/api/v1") {
		t.Error("openrouter.ai не локальный адрес")
	}
}

func TestFetchPricingSkipsUnknownProviders(t *testing.T) {
	quiet := func(string, ...any) {}
	if p := fetchPricing(t.Context(), "https://example.invalid/v1", "", "m", quiet); p != nil {
		t.Errorf("для неизвестного провайдера тарифы должны быть nil, получено %+v", p)
	}
	p := fetchPricing(t.Context(), "http://127.0.0.1:1234/v1", "", "m", quiet)
	if p == nil || !p.isLocal {
		t.Errorf("локальный прокси должен давать бесплатные тарифы, получено %+v", p)
	}
}

// A chat runs the loop once per message, so the price list must be looked up
// once per process rather than downloaded before every answer.
func TestFetchPricingIsCachedPerProcess(t *testing.T) {
	quiet := func(string, ...any) {}
	const url = "http://127.0.0.1:65001/v1"

	pricingMu.Lock()
	delete(pricingCache, url+"\x00м")
	pricingMu.Unlock()

	first := fetchPricing(t.Context(), url, "", "м", quiet)
	second := fetchPricing(t.Context(), url, "", "м", quiet)
	if first == nil || second == nil {
		t.Fatalf("локальный прокси должен давать тарифы: %v, %v", first, second)
	}
	if first != second {
		t.Error("второй вызов не взял тарифы из кэша")
	}

	pricingMu.Lock()
	_, cached := pricingCache[url+"\x00м"]
	pricingMu.Unlock()
	if !cached {
		t.Error("тарифы не сохранены в кэше")
	}
}

// A provider whose price list cannot be read must not be asked again on every
// message either.
func TestFetchPricingCachesTheAbsenceOfAPriceList(t *testing.T) {
	quiet := func(string, ...any) {}
	const url = "https://example.invalid/v1"

	pricingMu.Lock()
	delete(pricingCache, url+"\x00м")
	pricingMu.Unlock()

	if p := fetchPricing(t.Context(), url, "", "м", quiet); p != nil {
		t.Fatalf("для неизвестного провайдера ожидался nil, получено %+v", p)
	}
	pricingMu.Lock()
	value, cached := pricingCache[url+"\x00м"]
	pricingMu.Unlock()
	if !cached || value != nil {
		t.Errorf("отсутствие тарифов не закэшировано: cached=%v value=%+v", cached, value)
	}
}
