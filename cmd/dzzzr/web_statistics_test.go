package main

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/dzzzr-cli/agenttools"
)

func TestWebStatisticsAgentSharedState(t *testing.T) {
	runs := make(chan string, 1)
	h := newTestHub(t, func(_ context.Context, _ *webHub, id string) { runs <- id })
	mux := h.newMux()
	upload := statsRequest(t, mux, "/api/v1/statistics", "", "")
	if upload.Code != 200 {
		t.Fatal(upload.Body)
	}
	var loaded struct {
		LogID    string `json:"log_id"`
		Revision int    `json:"revision"`
	}
	if err := json.Unmarshal(upload.Body.Bytes(), &loaded); err != nil {
		t.Fatal(err)
	}
	agent, err := h.catalog("test-chat", agenttools.PolicyReadonly)
	if err != nil {
		t.Fatal(err)
	}
	report, _ := agent.Lookup("stats_report")
	r := report.Execute(t.Context(), map[string]any{"log_id": loaded.LogID})
	if r.IsError || !strings.Contains(r.Content, "960") {
		t.Fatal(r)
	}
	update, _ := agent.Lookup("stats_configure")
	r = update.Execute(t.Context(), map[string]any{"log_id": loaded.LogID, "revision": 1, "cfg": map[string]any{"teams": map[string]any{"A": map[string]any{"penalty": "2"}}}})
	if r.IsError {
		t.Fatal(r)
	}
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest("GET", "/api/v1/statistics/"+loaded.LogID, nil))
	if get.Code != 200 || !strings.Contains(get.Body.String(), `"total":1080`) {
		t.Fatal(get.Body)
	}
	stale := httptest.NewRecorder()
	mux.ServeHTTP(stale, httptest.NewRequest("PUT", "/api/v1/statistics/"+loaded.LogID, strings.NewReader(`{"revision":1,"cfg":{}}`)))
	if stale.Code != 409 {
		t.Fatal("stale UI edit accepted", stale.Code)
	}
	chat := httptest.NewRecorder()
	mux.ServeHTTP(chat, httptest.NewRequest("POST", "/api/v1/statistics/"+loaded.LogID+"/chat", nil))
	if chat.Code != http.StatusCreated {
		t.Fatal(chat.Body)
	}
	select {
	case id := <-runs:
		snap, _ := h.store.get(id)
		raw, _ := json.Marshal(snap)
		if !strings.Contains(string(raw), loaded.LogID) {
			t.Fatal("chat context lost")
		}
	case <-time.After(time.Second):
		t.Fatal("agent not started")
	}
}

func TestWebStatisticsAgentTurn(t *testing.T) {
	h := newTestHub(t, runWebChatTurn)
	t.Setenv("DZZZR_LLM_API_KEY", "test")
	s, err := h.statisticsStore()
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.Create("log.csv", []byte(statsCSV), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &scriptedProvider{replies: []*providers.LLMResponse{
		{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{ID: "report", Name: "stats_report", Arguments: map[string]any{"log_id": l.ID}}}},
		{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{ID: "search", Name: "stats_search", Arguments: map[string]any{"log_id": l.ID, "team": "A"}}}},
		answer("Расчёт и события получены."),
	}}
	useProvider(t, p)
	chat := h.store.create("moscow", agenttools.PolicyReadonly)
	_, _ = h.store.appendUser(chat.ID, "Найди аномалии в журнале "+l.ID)
	runWebChatTurn(t.Context(), h, chat.ID)
	snap, _ := h.store.get(chat.ID)
	if p.calls != 3 || snap.Running {
		t.Fatal("agent did not finish", p.calls)
	}
	raw, _ := json.Marshal(snap)
	if !strings.Contains(string(raw), "Расчёт и события получены.") || strings.Contains(string(raw), "failed:") {
		t.Fatalf("unexpected transcript: %s", raw)
	}
}
