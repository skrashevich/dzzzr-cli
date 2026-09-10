package main

import (
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *server) registerArchive(mux *http.ServeMux, prefix string) {
	mux.HandleFunc("GET "+prefix+"{$}", s.handleArchive)
	mux.HandleFunc("GET "+prefix+"gameLogJSON.php", s.handleArchiveLog)
}

// Site pages use the API token in a cookie, not in the s query parameter.
// The mock captain can read the scenario and log; other accounts cannot.
func (s *server) archiveAccess(r *http.Request) bool {
	cookie, err := r.Cookie("dozorSiteSession")
	return err == nil && s.state.isCaptain(s.sessions[cookie.Value])
}

func (s *server) handleArchive(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.state
	q := r.URL.Query()
	if q.Get("section") != "arc" {
		http.NotFound(w, r)
		return
	}
	if q.Get("gmid") == "" {
		writeHTML(w, page(fmt.Sprintf(`<h1>Архив игр</h1><h2>%s</h2><a href="?section=arc&gmid=%d&what=stat">Статистика</a> <a href="?section=arc&gmid=%d&what=comment">Описание</a>`, html.EscapeString(g.GameName), g.GameID, g.GameID)))
		return
	}
	if q.Get("gmid") != strconv.Itoa(g.GameID) {
		http.NotFound(w, r)
		return
	}
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, `<!-- CONTENT --><h1>Архив игр</h1><div class=Date>%s</div><h1>%s</h1>`, g.Start.Format("02.01.2006"), html.EscapeString(g.GameName))
	switch q.Get("what") {
	case "stat":
		g.archiveStat(&b)
	case "comment":
		g.archiveDescription(&b, s.archiveAccess(r))
	default:
		http.NotFound(w, r)
		return
	}
	b.WriteString("<!-- CONTENT END -->")
	writeHTML(w, page(b.String()))
}

func (g *gameState) archiveDescription(b *strings.Builder, allowed bool) {
	_, _ = fmt.Fprintf(b, `<h2>Описание</h2><div class=grayBox><h2>Авторы</h2>%s</div><div class=grayBox><h2>Легенда</h2>%s</div>`, html.EscapeString(g.Authors), g.Legend)
	if !allowed {
		b.WriteString(`<p><strong>Для просмотра текста всех заданий игры ваша команда должна участвовать хотя бы в одной из игр за последние три месяца.</strong></p>`)
		return
	}
	for _, l := range g.Levels {
		_, _ = fmt.Fprintf(b, `<h2>Задание: %s<br><span id=Code>Код: `, html.EscapeString(l.Title))
		for _, code := range l.Codes {
			_, _ = fmt.Fprintf(b, "%s &nbsp;", html.EscapeString(code))
		}
		_, _ = fmt.Fprintf(b, `</span></h2><div class=boxOrang id=left>%s<strong>Примечания к заданию</strong>: <p>%s</p><strong>Код сложности:</strong> %s<h3>Подсказка 1</h3>%s<h3>Подсказка 2</h3>%s</div>`, l.Question, html.EscapeString(l.LocationComment), html.EscapeString(strings.Join(l.Dangers, ",")), l.Clue1, l.Clue2)
		for _, sp := range l.Spoilers {
			_, _ = fmt.Fprintf(b, `<div class=grayBox><h2>Спойлер %d</h2>%s<br>Код спойлера %d: %s</div>`, sp.Order, sp.Text, sp.Order, html.EscapeString(sp.Code))
		}
	}
}

func (g *gameState) archiveStat(b *strings.Builder) {
	b.WriteString(`<h2>Статистика</h2><table class=tbl id=suxx><tr id=shapka><th>КОМАНДЫ</th><!--lev-->`)
	for _, l := range g.Levels {
		_, _ = fmt.Fprintf(b, "<th>%s</th>", html.EscapeString(l.Title))
	}
	b.WriteString(`<!--levend--><th>Штраф</th><th>Бонусы</th><th>Чистое время</th><th>Общее время</th><th>Место</th></tr>`)
	_, _ = fmt.Fprintf(b, `<tr><td><a href="?section=teams&teamID=%d">%s</a></td><!--lev-->`, g.TeamID, html.EscapeString(g.TeamName))
	var total time.Duration
	for _, l := range g.Levels {
		value := ""
		if p := g.Progress[l.Order]; p != nil && !p.CompletedAt.IsZero() {
			d := g.levelDuration(p, time.Now())
			value = formatClock(d)
			if !l.Bonus {
				total += d
			}
		}
		_, _ = fmt.Fprintf(b, "<td>%s</td>", value)
	}
	place := "—"
	if g.Finished {
		place = "1 / 1"
	}
	_, _ = fmt.Fprintf(b, `<!--levend--><td>%d</td><td>%d</td><td>%s</td><td>%s</td><td>%s</td></tr></table>`, g.PenaltyMin, g.BonusMin, formatClock(total), formatClock(total+time.Duration(g.PenaltyMin-g.BonusMin)*time.Minute), place)
}

func (s *server) handleArchiveLog(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.URL.Query().Get("gmid") != strconv.Itoa(s.state.GameID) {
		http.NotFound(w, r)
		return
	}
	if !s.archiveAccess(r) {
		w.Header().Set("Content-Type", "text/html; charset=windows-1251")
		return
	}
	g := s.state
	rows := map[string][]any{"1": {"Время", "Действие", "Команда", "Уровень", "Данные", "Данные"}}
	for i, e := range g.Log {
		title := ""
		for _, l := range g.Levels {
			if l.Order == e.Level {
				title = l.Title
				break
			}
		}
		rows[strconv.Itoa(i+2)] = []any{formatTime(e.Time), e.Event, g.TeamName, title, e.Data, e.User}
	}
	writeJSON(w, rows)
}
