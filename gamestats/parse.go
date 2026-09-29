// Game reconstruction ported from dozor_stats.html.
// Original algorithm author: Sergey <sergey@luberg.me> Luberg.
package gamestats

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

func action(raw string) string {
	a := strings.ToLower(norm(raw))
	exact := map[string]string{"выдан уровень": "ISSUE", "выдан сквозной уровень": "ISSUE_T", "принят код": "CODE", "принят бонусный код": "BONUS", "засчитан уровень": "CREDIT", "принят код к приквелу": "PREQUEL"}
	if k := exact[a]; k != "" {
		return k
	}
	for _, p := range [][2]string{{"вышло время", "TIMEOUT"}, {"отказ от уровня", "REFUSE"}, {"засчитано среднее", "AVG"}, {"введен правильный код к спойлеру", "SPOILER"}, {"введён правильный код к спойлеру", "SPOILER"}, {"введен неверный код к спойлеру", "SPOILER_BAD"}, {"введён неверный код к спойлеру", "SPOILER_BAD"}, {"запланировано задание", "PLAN"}, {"завершена игра", "END"}, {"удалена статистика", "RESET"}} {
		if strings.HasPrefix(a, p[0]) {
			return p[1]
		}
	}
	if strings.HasPrefix(a, "запрос ") && strings.Contains(a, "подсказк") {
		return "HINTREQ"
	}
	return ""
}

var levelNumber = regexp.MustCompile(`^(\d+(?:\.\d+)*)(?:\s+(.*))?$`)
var throughNumber = regexp.MustCompile(`(?i)сквозн\S*\s*(\d+)`)
var spoilerNumber = regexp.MustCompile(`(?i)спойлер\S*\s*(\d+)`)

func levelKey(l Level) []int {
	if m := levelNumber.FindStringSubmatch(l.Name); m != nil && !l.Through {
		key := []int{0}
		for part := range strings.SplitSeq(m[1], ".") {
			n, _ := strconv.Atoi(part)
			key = append(key, n)
		}
		return key
	}
	if m := throughNumber.FindStringSubmatch(l.Name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return []int{1, n}
	}
	return []int{2, 0}
}

func newRecord(level string) *Record {
	return &Record{Level: level, Codes: map[string]*Code{}, Bonus: map[string]*Code{}, Spoilers: map[string]*Code{}, SpoilerBad: map[string]*Code{}}
}

// Parse replays administrative corrections in log order and reconstructs
// transitions independently for each team. Equal timestamps retain export order.
func Parse(input []Event) (*Game, error) {
	rows := slices.Clone(input)
	asc, desc := 0, 0
	for i := 1; i < len(rows); i++ {
		if rows[i].T > rows[i-1].T {
			asc++
		} else if rows[i].T < rows[i-1].T {
			desc++
		}
	}
	if desc > asc {
		slices.Reverse(rows)
	}
	slices.SortStableFunc(rows, func(a, b Event) int { return cmp.Compare(a.T, b.T) })
	lastIssue := int64(0)
	found := false
	for i := range rows {
		rows[i].Kind = action(rows[i].A)
		if rows[i].Kind == "ISSUE" && rows[i].Team != "" {
			lastIssue = rows[i].T
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("в файле нет событий «выдан уровень»: нужна выгрузка журнала игры DozoR")
	}
	g := &Game{Teams: []string{}, Levels: []Level{}, Recs: map[string]map[string]*Record{}, Plan: map[string][]string{}, PlannedTeams: []string{}, Prequel: []string{}, NRows: len(rows)}
	for _, e := range rows {
		if e.Kind == "RESET" && e.T < lastIssue {
			g.CutAt = new(e.T)
		}
	}
	var play []Event
	for _, e := range rows {
		if e.Kind == "" {
			continue
		}
		if g.CutAt != nil && e.T < *g.CutAt && e.Kind != "PLAN" && e.Kind != "PREQUEL" && e.Kind != "RESET" {
			continue
		}
		play = append(play, e)
		if e.Kind == "ISSUE" && e.Team != "" && !slices.Contains(g.Teams, e.Team) {
			g.Teams = append(g.Teams, e.Team)
		}
		if e.Kind == "PLAN" && e.Team != "" && !slices.Contains(g.PlannedTeams, e.Team) {
			g.PlannedTeams = append(g.PlannedTeams, e.Team)
		}
	}
	g.NEvents = len(play)
	for _, e := range play {
		if e.Kind == "ISSUE" && e.Team != "" {
			g.StartAt = e.T
			break
		}
	}
	levels := map[string]*Level{}
	order := map[string]int{}
	current := map[string]string{}
	touch := func(name string, through bool) {
		if name == "" {
			return
		}
		if levels[name] == nil {
			levels[name] = &Level{Name: name}
		}
		if through {
			levels[name].Through = true
		}
	}
	record := func(team, level string) *Record {
		if g.Recs[team] == nil {
			g.Recs[team] = map[string]*Record{}
		}
		if g.Recs[team][level] == nil {
			g.Recs[team][level] = newRecord(level)
		}
		return g.Recs[team][level]
	}
	ended := false
	for _, e := range play {
		if e.Kind == "END" {
			g.EndAt = e.T
			ended = true
			continue
		}
		if e.Kind == "PREQUEL" {
			if e.Team != "" && !slices.Contains(g.Prequel, e.Team) {
				g.Prequel = append(g.Prequel, e.Team)
			}
			continue
		}
		if e.Kind == "RESET" || e.Team == "" || e.Level == "" {
			continue
		}
		if e.Kind == "PLAN" {
			touch(e.Level, false)
			if !slices.Contains(g.Plan[e.Team], e.Level) {
				g.Plan[e.Team] = append(g.Plan[e.Team], e.Level)
			}
			continue
		}
		if !slices.Contains(g.Teams, e.Team) {
			continue
		}
		touch(e.Level, e.Kind == "ISSUE_T")
		r := record(e.Team, e.Level)
		switch e.Kind {
		case "ISSUE":
			if r.Issued != nil && r.ClosedAt != nil {
				break
			}
			if r.Issued == nil {
				r.Issued = new(e.T)
				order[e.Team]++
				r.Order = order[e.Team]
			}
			if prev := current[e.Team]; prev != "" && prev != e.Level {
				pr := record(e.Team, prev)
				if pr.ClosedAt == nil {
					pr.ClosedAt = new(e.T)
				}
			}
			current[e.Team] = e.Level
		case "ISSUE_T":
			r.Through = true
			if r.Issued == nil {
				r.Issued = ParseTime(e.Data)
				if r.Issued == nil {
					r.Issued = new(e.T)
				}
			}
		case "CODE", "BONUS":
			codes := r.Codes
			if e.Kind == "BONUS" {
				codes = r.Bonus
			}
			key := normCode(e.Data)
			if codes[key] == nil {
				codes[key] = &Code{T: e.T, Raw: e.Data, Player: e.Player}
			}
			if e.Kind == "CODE" {
				r.LastCodeAt = new(e.T)
			}
		case "TIMEOUT":
			if r.TimeoutAt == nil {
				r.TimeoutAt = new(e.T)
			}
		case "REFUSE":
			if r.RefusedAt == nil {
				r.RefusedAt = new(e.T)
			}
		case "SPOILER", "SPOILER_BAD":
			if e.Kind == "SPOILER" && r.SpoilerAt == nil {
				r.SpoilerAt = new(e.T)
			}
			codes := r.Spoilers
			if e.Kind == "SPOILER_BAD" {
				codes = r.SpoilerBad
			}
			key := normCode(e.Data)
			if c := codes[key]; c != nil {
				c.Count++
			} else {
				n := ""
				if m := spoilerNumber.FindStringSubmatch(e.A); m != nil {
					n = m[1]
				}
				codes[key] = &Code{T: e.T, Raw: e.Data, Player: e.Player, N: n, Count: 1}
			}
		case "HINTREQ":
			h := 1
			if strings.Contains(strings.ToLower(e.A), "втор") {
				h = 2
			}
			r.HintReq = max(r.HintReq, h)
		case "CREDIT", "AVG":
			at := ParseTime(e.Data)
			if at == nil {
				at = new(e.T)
			}
			kind := "credit"
			if e.Kind == "AVG" {
				kind = "avg"
			}
			r.Override = &Override{Kind: kind, At: *at, Logged: e.T}
		}
	}
	for _, l := range levels {
		for _, team := range g.Teams {
			r := g.Recs[team][l.Name]
			if r == nil {
				continue
			}
			l.ReqThrough = max(l.ReqThrough, len(r.Codes))
			if r.ClosedAt == nil || (r.TimeoutAt != nil && *r.TimeoutAt <= *r.ClosedAt+2000) || r.RefusedAt != nil {
				continue
			}
			if n := len(r.Codes); n > 0 && (l.ReqCodes == 0 || n < l.ReqCodes) {
				l.ReqCodes = n
			}
		}
		ok := map[string]bool{}
		for _, team := range g.Teams {
			if r := g.Recs[team][l.Name]; r != nil {
				for code := range r.Spoilers {
					ok[code] = true
				}
			}
		}
		for _, team := range g.Teams {
			r := g.Recs[team][l.Name]
			if r == nil {
				continue
			}
			if l.ReqCodes > 0 && len(r.Codes) >= l.ReqCodes {
				var times []int64
				for _, c := range r.Codes {
					times = append(times, c.T)
				}
				slices.Sort(times)
				r.DoneAt = new(times[l.ReqCodes-1])
			}
			for key, c := range r.SpoilerBad {
				if !ok[key] || (r.SpoilerAt != nil && c.T >= *r.SpoilerAt) {
					continue
				}
				if r.Rejected == nil || c.T < r.Rejected.T {
					r.Rejected = c
				}
			}
		}
		l.Title = l.Name
		l.Family = "other"
		if m := levelNumber.FindStringSubmatch(l.Name); m != nil && !l.Through {
			l.Num = m[1]
			l.Title = m[2]
			parts := strings.Split(m[1], ".")
			l.Family = "x.y.z"
			if len(parts) == 2 {
				l.Family = "x.y"
				if parts[1] == "0" {
					l.Family = "x.0"
				}
			}
		}
		if l.Through {
			l.Family = "through"
		}
		n := strings.ToLower(l.Name)
		l.AutoKind = "main"
		if l.Through || strings.HasPrefix(n, "сквозн") || strings.HasPrefix(n, "технич") {
			l.AutoKind = "through"
		} else if strings.Contains(n, "заглушк") || strings.Contains(n, "бонусн") {
			l.AutoKind = "bonus"
		}
		g.Levels = append(g.Levels, *l)
	}
	collator := collate.New(language.Russian)
	slices.SortFunc(g.Levels, func(a, b Level) int {
		if c := slices.Compare(levelKey(a), levelKey(b)); c != 0 {
			return c
		}
		return collator.CompareString(a.Name, b.Name)
	})
	g.EndSrc = "событие «завершена игра»"
	if !ended {
		g.EndSrc = "последнее игровое событие журнала"
		for _, e := range play {
			if slices.Contains([]string{"ISSUE", "CODE", "TIMEOUT", "BONUS"}, e.Kind) {
				g.EndAt = max(g.EndAt, e.T)
			}
		}
	}
	for _, records := range g.Recs {
		for _, rec := range records {
			if rec.Issued == nil {
				continue
			}
			for _, codes := range []map[string]*Code{rec.Codes, rec.Bonus, rec.Spoilers, rec.SpoilerBad} {
				for _, code := range codes {
					code.Elapsed = new(float64(code.T-*rec.Issued) / 1000)
				}
			}
		}
	}
	return g, nil
}
