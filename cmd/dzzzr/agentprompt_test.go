package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// pinClock fixes the clock carried on user messages for one test.
func pinClock(t *testing.T, at time.Time) {
	t.Helper()
	messageStampNow = func() time.Time { return at }
	t.Cleanup(func() { messageStampNow = time.Now })
}

func TestStampedUserMessageCarriesTheSendTime(t *testing.T) {
	at := time.Date(2026, 9, 22, 14, 30, 0, 0, time.FixedZone("MSK", 3*60*60))
	pinClock(t, at)

	got := stampedUserMessage("возьми третий уровень")
	want := "[sent at 2026-09-22T14:30:00+03:00 / 2026-09-22T11:30:00Z UTC]\nвозьми третий уровень"
	if got != want {
		t.Errorf("stampedUserMessage =\n%q\nожидалось\n%q", got, want)
	}
}

// The system prompt runs ahead of the rules and the whole tool catalog. A value
// that changes every turn in that position costs the prompt cache everything
// behind it, so the prompt must not move with the clock.
func TestSystemPromptDoesNotMoveWithTheClock(t *testing.T) {
	isolate(t)
	cfg, _ := parseFor(t, []string{"-city", "moscow"})
	cfg.stderr = io.Discard
	c := dzzzr.New("moscow")
	catalog, err := newAgentCatalog(cfg, c, nil)
	if err != nil {
		t.Fatal(err)
	}

	pinClock(t, time.Date(2026, 9, 22, 14, 30, 0, 0, time.UTC))
	first := agentSystemPrompt(cfg, catalog, true)
	pinClock(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	second := agentSystemPrompt(cfg, catalog, true)

	if first != second {
		t.Error("системный промпт изменился вместе с часами: префикс перестаёт кешироваться")
	}
	if strings.Contains(first, "The current date and time is") {
		t.Error("часы вернулись в системный промпт")
	}
	if !strings.Contains(first, "every user message opens with the moment it was sent") {
		t.Error("правило TIME не объясняет модели, где теперь искать время")
	}
}

func TestChatStoreStampsOnlyTheModelCopy(t *testing.T) {
	pinClock(t, time.Date(2026, 9, 22, 14, 30, 0, 0, time.UTC))
	s := newChatStore(t.TempDir())
	snap := s.create("moscow", agenttools.PolicyReadonly)

	if _, ok := s.appendUser(snap.ID, "где штаб"); !ok {
		t.Fatal("appendUser отказал")
	}

	got, _ := s.get(snap.ID)
	if len(got.Lines) != 1 || got.Lines[0].Content != "где штаб" {
		t.Errorf("в ленте показано %+v, ожидался текст оператора без метки", got.Lines)
	}

	history, started := s.beginRun(snap.ID, func() {})
	if !started {
		t.Fatal("beginRun отказал")
	}
	if len(history) != 1 {
		t.Fatalf("истории %d сообщений, ожидалось 1", len(history))
	}
	want := "[sent at 2026-09-22T14:30:00Z / 2026-09-22T14:30:00Z UTC]\nгде штаб"
	if history[0].Content != want {
		t.Errorf("модель получила %q, ожидалось %q", history[0].Content, want)
	}
}
