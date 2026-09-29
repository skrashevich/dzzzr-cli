package main

import (
	"bytes"
	"encoding/json/v2"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/gamestats"
)

const statsCSV = "Время;Действие;Команда;Уровень;Данные\n2026-09-26 22:00:00;выдан уровень;A;1.0;\n2026-09-26 22:01:00;завершена игра;;;\n"

func statsRequest(t *testing.T, handler http.Handler, target, cfg, origin string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	f, err := w.CreateFormFile("file", "log.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(statsCSV)); err != nil {
		t.Fatal(err)
	}
	if cfg != "" {
		if err := w.WriteField("cfg", cfg); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, target, &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	return out
}

func TestLogStatisticsAPI(t *testing.T) {
	for _, handler := range []http.Handler{logStatHandler("", nil), newTestHub(t, nil).newMux()} {
		r := statsRequest(t, handler, "/api/v1/log-stat", "", "")
		if r.Code != http.StatusOK {
			t.Fatalf("%d %s", r.Code, r.Body)
		}
		var report gamestats.Report
		if err := json.Unmarshal(r.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.Result.Rows[0].Total != 960 {
			t.Fatalf("total: %v", report.Result.Rows[0].Total)
		}
		r = statsRequest(t, handler, "/api/v1/log-stat", `{"game":{},"levels":{},"teams":{"A":{"penalty":"2"}}}`, "")
		if err := json.Unmarshal(r.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.Result.Rows[0].Total != 1080 {
			t.Fatal("configuration not applied in backend")
		}
		if r = statsRequest(t, handler, "/api/v1/log-stat", "{", ""); r.Code != 400 {
			t.Fatal("invalid configuration accepted")
		}
		if r = statsRequest(t, handler, "/api/v1/log-stat", "", "https://other.example"); r.Code != 403 {
			t.Fatal("cross-origin upload accepted")
		}
		r = statsRequest(t, handler, "/api/v1/log-stat?format=xlsx", "", "")
		if r.Code != 200 || !bytes.HasPrefix(r.Body.Bytes(), []byte("PK")) {
			t.Fatal("XLSX export failed")
		}
	}
}

func TestLogStatisticsCommandAndAssets(t *testing.T) {
	cmd := findCommand("log-stat")
	if cmd == nil || cmd.Auth != authNone {
		t.Fatal("missing local command")
	}
	file := filepath.Join(t.TempDir(), "log.csv")
	if err := os.WriteFile(file, []byte(statsCSV), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := cmdLogStat(t.Context(), &config{jsonOut: true, stdout: &output}, nil, []string{file}); err != nil {
		t.Fatal(err)
	}
	var report gamestats.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Result.Rows) != 1 {
		t.Fatal("JSON calculation failed")
	}
	h := logStatHandler("log.csv", []byte(statsCSV))
	for _, path := range []string{"/stats.html", "/stats.js", "/stats.css", "/stats-input"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 200 {
			t.Fatalf("%s: %d", path, r.Code)
		}
		if path == "/stats.js" && strings.Contains(r.Body.String(), "function compute(") {
			t.Fatal("scoring leaked to browser")
		}
	}
}

func TestStatisticsHTMLExport(t *testing.T) {
	r := statsRequest(t, logStatHandler("", nil), "/api/v1/log-stat?format=html", `{"teams":{"A":{"penalty":"2"}}}`, "")
	if r.Code != 200 || !strings.HasPrefix(r.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("HTML export: %d", r.Code)
	}
	html := r.Body.String()
	for _, want := range []string{`id="stats-static-board"`, `00:18:00`, `id="stats-offline-wasm"`, `id="stats-offline-data"`, `"penalty":"2"`, "dzzzrOfflineCalculate", "Sergey"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, external := range []string{`src="stats.js"`, `href="stats.css"`} {
		if strings.Contains(html, external) {
			t.Errorf("external asset: %s", external)
		}
	}
	cfg := gamestats.Config{Teams: map[string]map[string]any{"</script><script>alert(1)</script>": {"penalty": "1"}}}
	out, err := exportStatsHTML("</script><script>alert(2)</script>.csv", []byte(statsCSV), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "<script>alert(") {
		t.Fatal("unescaped data in exported HTML")
	}
}
