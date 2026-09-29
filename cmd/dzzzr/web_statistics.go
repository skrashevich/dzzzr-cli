package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"github.com/skrashevich/dzzzr-cli/gamestats"
	"github.com/skrashevich/dzzzr-cli/statstore"
)

func (h *webHub) statisticsStore() (*statstore.Store, error) {
	h.statsOnce.Do(func() {
		dir, err := sessionDir()
		h.statsErr = err
		if err == nil {
			h.statsStore = &statstore.Store{Root: filepath.Join(dir, "web", "statistics")}
		}
	})
	return h.statsStore, h.statsErr
}
func (h *webHub) httpImportStatistics(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 34<<20)
	if err := r.ParseMultipartForm(34 << 20); err != nil {
		webError(w, 400, "%v", err)
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, head, err := r.FormFile("file")
	if err != nil {
		webError(w, 400, "выберите журнал")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if err != nil {
		webError(w, 400, "%v", err)
		return
	}
	s, err := h.statisticsStore()
	if err != nil {
		webError(w, 500, "%v", err)
		return
	}
	l, err := s.Create(head.Filename, data, nil)
	if err != nil {
		webError(w, 400, "%v", err)
		return
	}
	writeStatistics(w, l)
}
func writeStatistics(w http.ResponseWriter, l *statstore.Log) {
	report, err := l.Report()
	if err != nil {
		webError(w, 400, "%v", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	webWriteJSON(w, 200, map[string]any{"log_id": l.ID, "name": l.Name, "revision": l.Revision, "report": report, "defaults": gamestats.DefaultConfig(report.Game)})
}
func (h *webHub) loadStatistics(w http.ResponseWriter, r *http.Request) *statstore.Log {
	s, err := h.statisticsStore()
	if err != nil {
		webError(w, 500, "%v", err)
		return nil
	}
	l, err := s.Load(r.PathValue("log"))
	if err != nil {
		webError(w, 404, "журнал не найден: %v", err)
		return nil
	}
	return l
}
func (h *webHub) httpStatistics(w http.ResponseWriter, r *http.Request) {
	if l := h.loadStatistics(w, r); l != nil {
		writeStatistics(w, l)
	}
}
func (h *webHub) httpUpdateStatistics(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Revision int              `json:"revision"`
		Config   gamestats.Config `json:"cfg"`
	}
	if !webReadJSON(w, r, &body) {
		return
	}
	s, err := h.statisticsStore()
	if err != nil {
		webError(w, 500, "%v", err)
		return
	}
	l, err := s.Update(r.PathValue("log"), body.Revision, body.Config)
	if err != nil {
		webError(w, 409, "%v", err)
		return
	}
	writeStatistics(w, l)
}
func (h *webHub) httpStatisticsSource(w http.ResponseWriter, r *http.Request) {
	l := h.loadStatistics(w, r)
	if l == nil {
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(l.Data)
}
func (h *webHub) httpStatisticsChat(w http.ResponseWriter, r *http.Request) {
	l := h.loadStatistics(w, r)
	if l == nil {
		return
	}
	snap := h.store.create(h.cfg.city, h.defaultPolicy())
	prompt := fmt.Sprintf("Проанализируй журнал игры %q (log_id=%s, версия параметров %d). Он уже загружен в общий раздел статистики. Используй stats_report и stats_search: объясни результаты и найди аномалии со ссылками на конкретные события. Параметры без моей просьбы не меняй. Ссылка на расчёт: /?stats=%s#/stats", l.Name, l.ID, l.Revision, l.ID)
	_, _ = h.store.appendUser(snap.ID, prompt)
	_ = h.store.persist(snap.ID)
	run := h.run
	if run == nil {
		run = runWebChatTurn
	}
	go run(context.Background(), h, snap.ID)
	webWriteJSON(w, 201, map[string]string{"chat_id": snap.ID})
}
