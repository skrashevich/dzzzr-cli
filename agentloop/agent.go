package agentloop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/tools"
	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// DefaultMaxTurns bounds one run. A game level is solved in a handful of
// calls; this many means the model is looping, and stopping is kinder than
// spending the team's money.
const DefaultMaxTurns = 200

// maxAttempts is how often one model request is repeated while the failure
// still looks temporary.
const maxAttempts = 3

// retryDelay is how long to wait before repeating a failed model request. It
// is a variable so a test can run the retry path without the wait.
var retryDelay = func(attempt int) time.Duration { return time.Duration(attempt) * 5 * time.Second }

// Config points the run at a provider.
type Config struct {
	// APIKey authenticates with the provider. A proxy on this machine may
	// need none.
	APIKey string
	// Model is the model identifier as the provider spells it.
	Model string
	// BaseURL is the OpenAI-compatible endpoint.
	BaseURL string
	// UserAgent identifies this client to the provider.
	UserAgent string
	// MaxTurns overrides DefaultMaxTurns.
	MaxTurns int
	// SourceContextBytes limits complete read_pdf/read_local_file results.
	// Zero uses DefaultSourceContextBytes. Exceeding it stops the run instead
	// of losing source pages and asking the model to read them again.
	SourceContextBytes int
	// RequestTimeout bounds each provider request, including streaming bodies.
	// Zero uses DefaultRequestTimeout.
	RequestTimeout time.Duration
	// Provider replaces the HTTP provider built from the fields above. Tests
	// and callers that already own a provider use it.
	Provider providers.LLMProvider
}

// Message is one turn of the conversation as it is kept between runs.
//
// Only the text is kept. PicoClaw owns the tool calls of a run and does not
// hand its expanded conversation back, so a later run continues from what was
// said, not from the tool results that led there.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Message roles.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

// RunInput is the conversation and the tools of one run.
type RunInput struct {
	// Catalog supplies the engine tools. Its policy has already decided which
	// of them the model may see.
	Catalog *agenttools.Catalog
	// Extra adds tools that do not come from the engine.
	Extra []Tool
	// Messages is the conversation so far, without the system message.
	Messages []Message
	// SystemPrompt opens the conversation.
	SystemPrompt string
}

// Result is what a finished run produced.
type Result struct {
	// Messages is the conversation with the model's answer appended.
	Messages []Message
	// Report is the same text as the EventReport event.
	Report string
	// Turns is how many times the model was asked.
	Turns int
}

// quietPicoClaw silences PicoClaw's own console logging once per process: it
// writes to the same terminal the CLI does, and in the TUI it would land on
// the alternate screen.
var quietPicoClaw sync.Once

// Run executes one agent run and returns when the model has answered.
func Run(ctx context.Context, cfg Config, in *RunInput, cb Callbacks) (Result, error) {
	if in == nil {
		return Result{}, errors.New("agentloop: RunInput is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return Result{}, errors.New("agentloop: a model is required")
	}

	list := collectTools(in.Catalog, in.Extra)
	st := &stats{}
	rt := &runtime{cb: cb, stats: st}
	registry, err := newRegistry(list, rt)
	if err != nil {
		return Result{Messages: in.Messages}, err
	}

	quietPicoClaw.Do(logger.DisableConsole)

	// The clock starts before the price lookup, so the reported total is the
	// time the user actually waited rather than the loop's share of it.
	started := time.Now()
	price := fetchPricing(ctx, cfg.BaseURL, cfg.APIKey, cfg.Model, func(format string, args ...any) {
		cb.status("debug", fmt.Sprintf(format, args...))
	})

	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}

	loop, err := tools.RunToolLoop(ctx, tools.ToolLoopConfig{
		Provider:      &observer{delegate: newProvider(cfg), cb: cb, stats: st, sourceContextBytes: cfg.SourceContextBytes, requestTimeout: cfg.RequestTimeout},
		Model:         cfg.Model,
		Tools:         registry,
		MaxIterations: maxTurns,
	}, picoMessages(in.SystemPrompt, in.Messages), "dzzzr", "agent")
	if err != nil {
		cb.emit(Event{Type: EventError, Err: err, Message: err.Error()})
		return Result{Messages: in.Messages}, err
	}

	out := Result{Messages: in.Messages, Turns: loop.Iterations}
	if answer := strings.TrimSpace(loop.Content); answer != "" {
		out.Messages = append(out.Messages, Message{Role: RoleAssistant, Content: answer})
		cb.emit(Event{Type: EventAssistantText, Text: answer})
	} else if loop.Iterations >= maxTurns {
		cb.emit(Event{
			Type:    EventWarning,
			Message: fmt.Sprintf("Агент исчерпал лимит в %d обращений к модели и остановлен без ответа.", maxTurns),
		})
	}

	out.Report = formatReport(cfg.Model, price, time.Since(started), st.snapshot())
	cb.emit(Event{Type: EventReport, Report: out.Report})
	cb.emit(Event{Type: EventDone})
	return out, nil
}

// newProvider builds the provider the run talks to.
func newProvider(cfg Config) providers.LLMProvider {
	if cfg.Provider != nil {
		return cfg.Provider
	}
	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = "dzzzr-cli"
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	p := providers.NewHTTPProviderWithMaxTokensFieldAndRequestTimeout(
		cfg.APIKey,
		strings.TrimRight(cfg.BaseURL, "/"),
		"", "",
		userAgent,
		int((timeout+time.Second-1)/time.Second),
		nil, nil,
	)
	// OpenRouter needs to be recognized by name for its own request fields.
	if strings.Contains(strings.ToLower(cfg.BaseURL), "openrouter.ai") {
		p.SetProviderName("openrouter")
	}
	return p
}

// picoMessages converts the stored conversation into PicoClaw's form.
func picoMessages(systemPrompt string, messages []Message) []providers.Message {
	out := make([]providers.Message, 0, len(messages)+1)
	if strings.TrimSpace(systemPrompt) != "" {
		out = append(out, providers.Message{Role: RoleSystem, Content: systemPrompt})
	}
	for _, m := range messages {
		out = append(out, providers.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

// observer wraps the provider to count what a run costs, to report progress
// while the model thinks, and to repeat a request that failed for a reason
// that may pass.
type observer struct {
	delegate           providers.LLMProvider
	cb                 Callbacks
	stats              *stats
	sourceContextBytes int
	requestTimeout     time.Duration
	statusMu           sync.Mutex
}

func (o *observer) status(phase, message string) {
	o.statusMu.Lock()
	defer o.statusMu.Unlock()
	o.cb.status(phase, message)
}

func (o *observer) GetDefaultModel() string { return o.delegate.GetDefaultModel() }

func (o *observer) Chat(
	ctx context.Context,
	messages []providers.Message,
	toolDefs []providers.ToolDefinition,
	model string,
	options map[string]any,
) (*providers.LLMResponse, error) {
	var err error
	messages, err = boundToolHistory(messages, o.sourceContextBytes)
	if err != nil {
		return nil, err
	}
	turn := o.stats.snapshot().turns + 1
	o.status("llm", fmt.Sprintf("Шаг %d: ожидание ответа модели…", turn))

	started := time.Now()
	var (
		response *providers.LLMResponse
		lastErr  error
	)
	// A truncated response is never executed. With structural extraction tools
	// available, give the model one bounded chance to emit a small saved part.
	canSplitMapping := false
	for _, def := range toolDefs {
		if def.Function.Name == "extract_pdf" {
			canSplitMapping = true
		}
	}
	hasPDFSource := false
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			name := call.Name
			if name == "" && call.Function != nil {
				name = call.Function.Name
			}
			if name == "index_pdf" || name == "read_pdf" {
				hasPDFSource = true
			}
		}
	}
	canSplitMapping = canSplitMapping && hasPDFSource
	for attempt := range maxAttempts {
		if attempt > 0 {
			delay := retryDelay(attempt)
			o.status("retry", fmt.Sprintf("Повтор через %s…", delay))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		response, lastErr = o.chatWithProgress(ctx, messages, toolDefs, model, options)
		if lastErr == nil && response != nil && response.FinishReason == "error" {
			// Even apparently complete tool calls in an aborted response are
			// untrusted. Retry generation with the same successful tool history.
			response = nil
			lastErr = errProviderResponse
		}
		if lastErr == nil && response != nil && truncatedResponse(response) && canSplitMapping && attempt+1 < maxAttempts {
			canSplitMapping = false
			o.status("retry", "Ответ обрезан провайдером; неполные вызовы отброшены. Повтор с запросом одной небольшой части схемы.")
			messages = append(append([]providers.Message(nil), messages...), providers.Message{Role: "user", Content: "The provider truncated the previous response; NONE of its tool calls were executed. Continue with ONE SMALL structural mapping part for one level/section via save_local_json. Do not emit the full mapping or scenario text. Reuse previously saved parts. Eventually save a version:2 manifest and call extract_pdf. If you cannot proceed, report the incomplete work explicitly."})
			continue
		}
		if lastErr == nil && (response == nil || (len(response.ToolCalls) == 0 && strings.TrimSpace(response.Content) == "" && (response.FinishReason == "stop" || response.FinishReason == ""))) {
			lastErr = errEmptyModelResponse
		}
		if lastErr == nil {
			break
		}
		// Repeating an exhausted generation can turn a ten-minute wait into
		// half an hour. Let the user choose whether to try again.
		if errors.Is(lastErr, context.DeadlineExceeded) || errors.Is(lastErr, context.Canceled) {
			return nil, lastErr
		}
		if !retryable(lastErr) {
			return nil, lastErr
		}
		o.status("retry", fmt.Sprintf("Ошибка модели (%d/%d): %v", attempt+1, maxAttempts, lastErr))
	}
	if lastErr != nil {
		return nil, fmt.Errorf("модель не ответила за %d попытки: %w", maxAttempts, lastErr)
	}
	if response == nil {
		return nil, errors.New("модель вернула пустой ответ")
	}
	o.status("debug", fmt.Sprintf("Ответ модели: finish_reason=%q, вызовов инструментов=%d, текст=%d байт", response.FinishReason, len(response.ToolCalls), len(response.Content)))
	if truncatedResponse(response) {
		return nil, errors.New("модель достигла лимита длины ответа; неполные вызовы инструментов не выполнены. Разбейте экспорт на меньшие части")
	}
	switch response.FinishReason {
	case "content_filter", "safety", "canceled":
		return nil, fmt.Errorf("провайдер прервал ответ (finish_reason=%s); вызовы инструментов не выполнены", response.FinishReason)
	}

	o.stats.addLLM(time.Since(started), response.Usage)
	return response, nil
}

func truncatedResponse(response *providers.LLMResponse) bool {
	return response.FinishReason == "length" || response.FinishReason == "truncated" || response.FinishReason == "max_tokens"
}

// chatWithProgress reports how long the wait has lasted, because a model that
// is thinking looks exactly like one that has hung.
func (o *observer) chatWithProgress(
	ctx context.Context,
	messages []providers.Message,
	toolDefs []providers.ToolDefinition,
	model string,
	options map[string]any,
) (*providers.LLMResponse, error) {
	timeout := o.requestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if o.cb.OnStatus != nil {
		requestCtx = traceModelRequest(requestCtx, Callbacks{OnStatus: o.status})
	}
	var size int
	for _, message := range messages {
		size += len(message.Content)
	}
	o.status("debug", fmt.Sprintf("Запрос модели: сообщений=%d, текст сообщений=%d байт, инструментов=%d, таймаут=%s", len(messages), size, len(toolDefs), timeout))
	call := func() (*providers.LLMResponse, error) {
		result, err := o.delegate.Chat(requestCtx, messages, toolDefs, model, options)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if requestCtx.Err() != nil {
			return nil, fmt.Errorf("модель не завершила ответ за %s; DZZZR_LLM_REQUEST_TIMEOUT_SECONDS задаёт таймаут: %w", timeout, requestCtx.Err())
		}
		return result, err
	}
	if o.cb.OnStatus == nil {
		return call()
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	defer func() { close(done); <-stopped }()
	go func() {
		defer close(stopped)
		started := time.Now()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				o.status("llm_wait", fmt.Sprintf("Ожидание завершения ответа модели… %s (таймаут %s)",
					time.Since(started).Round(time.Second), timeout))
			case <-done:
				return
			}
		}
	}()
	return call()
}
