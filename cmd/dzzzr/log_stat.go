package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/gamestats"
)

func init() {
	register(command{
		Name: "log-stat", Usage: "log-stat [ФАЙЛ.xlsx|ФАЙЛ.json|ФАЙЛ.csv] [-json] [-web-addr АДРЕС]",
		Help: "Построить статистику игры по журналу в браузере, без входа в движок",
		Auth: authNone, Run: cmdLogStat,
	})
}

func cmdLogStat(ctx context.Context, cfg *config, _ *dzzzr.Client, args []string) error {
	if len(args) > 1 {
		return fatal("использование: log-stat [ФАЙЛ.xlsx|ФАЙЛ.json|ФАЙЛ.csv]")
	}
	var data []byte
	var name string
	if len(args) == 1 {
		f, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		const maxLogSize = 32 << 20
		data, err = io.ReadAll(io.LimitReader(f, maxLogSize+1))
		if err != nil {
			return err
		}
		if len(data) > maxLogSize {
			return fatal("журнал превышает 32 МБ")
		}
		name = filepath.Base(args[0])
	}
	page := "stats.html?standalone=1"
	if cfg.jsonOut {
		if name == "" {
			return fatal("для -json укажите файл журнала")
		}
		report, err := gamestats.Build(name, data, nil)
		if err != nil {
			return err
		}
		return json.MarshalWrite(cfg.stdout, report)
	}
	if name != "" {
		page += "&log=" + url.QueryEscape(name)
	}
	return serveWebHandler(ctx, cfg, logStatHandler(name, data), cfg.webAddr, page)
}

// Only the explicitly selected file is exposed, never its containing directory.
func logStatHandler(name string, data []byte) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/stats.html?standalone=1", http.StatusFound)
	})
	mux.HandleFunc("POST /api/v1/log-stat", httpLogStat)
	sub, err := fs.Sub(webUIFiles, "webui")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /stats-input", func(w http.ResponseWriter, r *http.Request) {
		if name == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	})
	return sameOriginWrites(mux)
}

// Each request is self-contained. Uploaded game logs are not persisted to disk
// or sent to the engine/LLM; parameter changes replay the log in Go.
func httpLogStat(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 34<<20)
	defer func() { _ = r.Body.Close() }()
	if err := r.ParseMultipartForm(34 << 20); err != nil {
		webError(w, http.StatusBadRequest, "не удалось прочитать загрузку (лимит 32 МБ): %v", err)
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	f, header, err := r.FormFile("file")
	if err != nil {
		webError(w, http.StatusBadRequest, "выберите файл журнала")
		return
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if err != nil || len(data) > 32<<20 {
		webError(w, http.StatusBadRequest, "не удалось прочитать журнал (лимит 32 МБ)")
		return
	}
	var cfg *gamestats.Config
	if raw := r.FormValue("cfg"); raw != "" {
		cfg = new(gamestats.Config)
		if len(raw) > 1<<20 || json.Unmarshal([]byte(raw), cfg) != nil {
			webError(w, http.StatusBadRequest, "неверные параметры расчёта")
			return
		}
	}
	report, err := gamestats.Build(header.Filename, data, cfg)
	if err != nil {
		webError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if r.URL.Query().Get("format") == "html" {
		out, err := exportStatsHTML(header.Filename, data, report.Config)
		if err != nil {
			webError(w, http.StatusInternalServerError, "экспорт HTML: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="dozor-statistics.html"`)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(out)
		return
	}
	if r.URL.Query().Get("format") == "xlsx" {
		out, err := gamestats.ExportXLSX(report)
		if err != nil {
			webError(w, http.StatusInternalServerError, "экспорт: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="dozor-statistics.xlsx"`)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(out)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	webWriteJSON(w, http.StatusOK, report)
}
