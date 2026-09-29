package agenttools_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/statstore"
)

type statsEngine struct{ *fakeEngine }

func (f *statsEngine) GetGameLog(context.Context, int) (*dzzzr.GameLog, error) {
	f.note("GetGameLog")
	return &dzzzr.GameLog{Columns: []string{"Время", "Действие", "Команда", "Уровень", "Данные", "Данные2"}, Entries: []map[string]string{
		{"Время": "2026-09-26 22:00:00", "Действие": "выдан уровень", "Команда": "A", "Уровень": "1.0"},
		{"Время": "2026-09-26 22:00:10", "Действие": "неверный код", "Команда": "A", "Уровень": "1.0", "Данные": "needle", "Данные2": "alice"},
		{"Время": "2026-09-26 22:01:00", "Действие": "завершена игра"},
	}}, nil
}
func TestStatisticsToolsSharedWorkflow(t *testing.T) {
	root := t.TempDir()
	store := &statstore.Store{Root: filepath.Join(root, "stats")}
	e := &statsEngine{newFakeEngine()}
	catalog := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly, FilesRoot: root, StatsStore: store})
	exec := func(name string, args map[string]any) map[string]any {
		t.Helper()
		r := call(t, catalog, name, args)
		if r.IsError {
			t.Fatal(r.Content)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(r.Content), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	loaded := exec("stats_load", map[string]any{"game_id": 1566})
	id := loaded["log_id"].(string)
	result := exec("stats_report", map[string]any{"log_id": id})
	if result["total"] != float64(1) {
		t.Fatal(result)
	}
	found := exec("stats_search", map[string]any{"log_id": id, "text": "NEEDLE", "player": "alice", "team": "A", "level": "1.0", "from": "2026-09-26 22:00:00", "to": "2026-09-26 22:00:30"})
	if found["total"] != float64(1) {
		t.Fatal(found)
	}
	page := exec("stats_search", map[string]any{"log_id": id, "limit": 1})
	if page["next_offset"] != float64(1) {
		t.Fatal(page)
	}
	cfg := result["cfg"].(map[string]any)
	cfg["teams"] = map[string]any{"A": map[string]any{"penalty": "2"}}
	exec("stats_configure", map[string]any{"log_id": id, "revision": 1, "cfg": cfg})
	if r := call(t, catalog, "stats_configure", map[string]any{"log_id": id, "revision": 1, "cfg": cfg}); !r.IsError {
		t.Fatal("stale edit accepted")
	}
	l, err := (&statstore.Store{Root: store.Root}).Load(id)
	if err != nil {
		t.Fatal(err)
	}
	r, err := l.Report()
	if err != nil || r.Result.Rows[0].Total != 1080 {
		t.Fatal("settings did not persist", err)
	}
	if e.calls["GetGameLog"] != 1 {
		t.Fatal("search refetched log")
	}
	if _, err = store.Load("../../anything"); err == nil {
		t.Fatal("invalid ID accepted")
	}
	external := filepath.Join(t.TempDir(), "log.csv")
	if err = os.WriteFile(external, []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := call(t, catalog, "stats_load", map[string]any{"path": external}); !r.IsError || !strings.Contains(r.Content, "inside") {
		t.Fatal("file root escaped", r)
	}
}
