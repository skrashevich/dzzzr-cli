package agentloop

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// stats accumulates what a run cost. PicoClaw calls tools from several
// goroutines, so every field is written under the mutex.
type stats struct {
	mu               sync.Mutex
	llmDuration      time.Duration
	toolDuration     time.Duration
	turns            int
	toolCalls        int
	promptTokens     int
	completionTokens int
}

func (s *stats) addLLM(d time.Duration, usage *providers.UsageInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.llmDuration += d
	s.turns++
	if usage != nil {
		s.promptTokens += usage.PromptTokens
		s.completionTokens += usage.CompletionTokens
	}
}

func (s *stats) addTool(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolDuration += d
	s.toolCalls++
}

// snapshot is a consistent copy of the counters.
type snapshot struct {
	llmDuration      time.Duration
	toolDuration     time.Duration
	turns            int
	toolCalls        int
	promptTokens     int
	completionTokens int
}

func (s *stats) snapshot() snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshot{
		llmDuration:      s.llmDuration,
		toolDuration:     s.toolDuration,
		turns:            s.turns,
		toolCalls:        s.toolCalls,
		promptTokens:     s.promptTokens,
		completionTokens: s.completionTokens,
	}
}

// formatReport renders what the run spent. Overhead is whatever the total time
// is not accounted for by the model and the tools.
func formatReport(model string, p *pricing, total time.Duration, s snapshot) string {
	overhead := total - s.llmDuration - s.toolDuration
	if overhead < 0 {
		overhead = 0
	}

	var b strings.Builder
	b.WriteString("\n--- Отчёт о выполнении ---\n")
	fmt.Fprintf(&b, "Общее время:     %s\n", total.Round(time.Millisecond))
	fmt.Fprintf(&b, "Модель:          %s\n", model)
	fmt.Fprintf(&b, "  Модель:        %s\n", s.llmDuration.Round(time.Millisecond))
	fmt.Fprintf(&b, "  Инструменты:   %s\n", s.toolDuration.Round(time.Millisecond))
	fmt.Fprintf(&b, "  Накладные:     %s\n", overhead.Round(time.Millisecond))
	fmt.Fprintf(&b, "Запросов к LLM:  %d\n", s.turns)
	fmt.Fprintf(&b, "Вызовов тулзов:  %d\n", s.toolCalls)
	if s.promptTokens > 0 || s.completionTokens > 0 {
		fmt.Fprintf(&b, "Токены:          %d (вход: %d, выход: %d)\n",
			s.promptTokens+s.completionTokens, s.promptTokens, s.completionTokens)
		switch {
		case p == nil:
		case p.isLocal:
			b.WriteString("Стоимость:       $0 (локальный прокси)\n")
		default:
			fmt.Fprintf(&b, "Стоимость:       $%.4f (тарифы OpenRouter)\n",
				p.cost(s.promptTokens, s.completionTokens))
		}
	}
	return b.String()
}
