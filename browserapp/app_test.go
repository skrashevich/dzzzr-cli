package browserapp

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBrowserCapabilities(t *testing.T) {
	var a App
	data := []byte("Время;Действие;Команда;Уровень;Данные\n2026-09-26 22:00:00;выдан уровень;A;1.0;\n2026-09-26 22:01:00;завершена игра;;;\n")
	if _, err := a.Dispatch(t.Context(), Request{Action: "upload", Name: "log.csv", Data: data}); err != nil {
		t.Fatal(err)
	}
	out, err := a.Dispatch(t.Context(), Request{Action: "tool", Name: "stats_load", Args: map[string]any{"path": "log.csv"}})
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["log_id"].(string)
	out, err = a.Dispatch(t.Context(), Request{Action: "tool", Name: "stats_report", Args: map[string]any{"log_id": id}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"total":960`) {
		t.Fatal(string(raw))
	}
	pdf, err := os.ReadFile("../pdfsource/testdata/synthetic.pdf")
	if err != nil {
		t.Fatal(err)
	}
	{
		a.put("test.pdf", pdf)
		if _, err = a.Dispatch(t.Context(), Request{Action: "tool", Name: "index_pdf", Args: map[string]any{"path": "test.pdf"}}); err != nil {
			t.Fatal(err)
		}
	}
	a.put("invalid.json", []byte(`{"game_id":0}`))
	if _, err = a.Dispatch(t.Context(), Request{Action: "tool", Name: "validate_scenario", Args: map[string]any{"path": "invalid.json"}}); err == nil {
		t.Fatal("invalid scenario accepted")
	}
}
func TestBrowserAgentToolLoop(t *testing.T) {
	var a App
	a.put("source.txt", []byte("grounded source"))
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing authorization")
		}
		var body map[string]any
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"files","type":"function","function":{"name":"list_files","arguments":"{}"}}]}}]}`))
			return
		}
		raw, _ := json.Marshal(body)
		if !strings.Contains(string(raw), "source.txt") {
			t.Error("tool output missing")
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Файл найден."}}]}`))
	}))
	defer server.Close()
	messages, err := a.Chat(t.Context(), ChatConfig{Endpoint: server.URL, Key: "test-key", Model: "test"}, []ChatMessage{{Role: "user", Content: "Какие файлы?"}}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || messages[len(messages)-1].Content != "Файл найден." {
		t.Fatal(messages)
	}
}
