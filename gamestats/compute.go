// Statistics algorithm ported from dozor_stats.html.
// Original algorithm author: Sergey <sergey@luberg.me> Luberg.
package gamestats

import (
	"cmp"
	"crypto/sha256"
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// This game-specific preset comes from the supplied HTML, not from log events.
//
//go:embed preset1566.json
var preset1566 []byte

func DefaultConfig(g *Game) Config {
	c := Config{Game: map[string]any{}, Levels: map[string]map[string]any{}, Teams: map[string]map[string]any{}}
	if g.StartAt == *ParseTime("2026-09-26 22:30:00") && slices.Contains(g.Teams, "Rising") && slices.Contains(g.Teams, "MADS") && slices.Contains(g.Teams, "L_n_P") && slices.Contains(g.Teams, "Стальные Яйца") {
		_ = json.Unmarshal(preset1566, &c)
	}
	return c
}

func number(m map[string]any, key string, fallback float64) float64 {
	v, ok := m[key]
	if !ok || v == nil || v == "" {
		return fallback
	}
	s := strings.ReplaceAll(fmt.Sprint(v), ",", ".")
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return fallback
	}
	return n
}
func option(m map[string]any, key, def string) string {
	if s, ok := m[key].(string); ok && s != "" {
		return s
	}
	return def
}

type limit struct {
	trans, lazy  []float64
	maxOpen, dur float64
	src, text    string
}

func inferLimits(g *Game, defaultDur float64) map[string]*limit {
	out := map[string]*limit{}
	reliable := map[string][]float64{}
	for _, l := range g.Levels {
		if l.AutoKind == "through" {
			continue
		}
		o := &limit{}
		out[l.Name] = o
		for _, team := range g.Teams {
			r := g.Recs[team][l.Name]
			if r == nil || r.Issued == nil {
				continue
			}
			if r.TimeoutAt != nil {
				d := float64(*r.TimeoutAt - *r.Issued)
				if r.ClosedAt != nil && math.Abs(float64(*r.ClosedAt-*r.TimeoutAt)) <= 2000 {
					o.trans = append(o.trans, d)
				} else {
					o.lazy = append(o.lazy, d)
				}
			} else if r.ClosedAt != nil && r.RefusedAt == nil {
				o.maxOpen = max(o.maxOpen, float64(*r.ClosedAt-*r.Issued))
			}
		}
		if len(o.trans) > 0 {
			o.dur = math.Floor(slices.Min(o.trans) / 60000)
			o.src = "timeout"
			o.text = fmt.Sprintf("по таймаутам в журнале (%d ком.)", len(o.trans))
			reliable[l.Family] = append(reliable[l.Family], o.dur)
		}
	}
	for _, l := range g.Levels {
		if l.AutoKind == "through" {
			continue
		}
		o := out[l.Name]
		if o.src != "" {
			continue
		}
		fm := -1.0
		count := map[float64]int{}
		best := 0
		for _, n := range reliable[l.Family] {
			count[n]++
			if count[n] > best || (count[n] == best && n > fm) {
				best = count[n]
				fm = n
			}
		}
		if len(o.lazy) > 0 {
			obs := math.Floor(slices.Min(o.lazy) / 60000)
			if fm >= 0 && fm == obs {
				o.dur = fm
				o.src = "timeout"
				o.text = fmt.Sprintf("по таймауту последнего уровня команды (%d ком.)", len(o.lazy))
			} else if fm >= 0 && fm < obs {
				o.dur = fm
				o.src = "family"
				o.text = fmt.Sprintf("таймаут записан с опозданием (%g мин, последний уровень команды) — взят типовой лимит %g мин для уровней %s", obs, fm, l.Family)
			} else {
				o.dur = obs
				o.src = "lazy"
				o.text = fmt.Sprintf("оценка по запоздалой записи таймаута (%g мин) — проверьте", obs)
			}
		} else if fm >= 0 {
			o.dur = fm
			o.src = "family"
			o.text = "таймаутов не было — типовой лимит для уровней " + l.Family
		} else {
			o.dur = defaultDur
			o.src = "default"
			o.text = "таймаутов не было — лимит по умолчанию"
		}
		open := math.Ceil(o.maxOpen / 60000)
		if open > o.dur {
			o.dur = math.Ceil(open/5) * 5
			o.src += "+bound"
			o.text += fmt.Sprintf("; увеличен до %g мин: команда держала уровень %g мин без таймаута", o.dur, open)
		}
	}
	return out
}

func resolve(g *Game, c Config) Resolved {
	r := Resolved{Levels: []Level{}, StopAt: g.EndAt, DefaultDur: number(c.Game, "defaultDur", 90), PrequelMin: number(c.Game, "prequelMin", 0), NoneMode: option(c.Game, "noneMode", "limit"), StopMode: option(c.Game, "stopMode", "actual"), RejectedMode: option(c.Game, "rejectedMode", "first")}
	if t := ParseTime(option(c.Game, "stopAt", "")); t != nil {
		r.StopAt = *t
	}
	limits := inferLimits(g, r.DefaultDur)
	for _, base := range g.Levels {
		l := base
		o := c.Levels[l.Name]
		l.Kind = option(o, "kind", l.AutoKind)
		l.AutoDur = r.DefaultDur
		if lim := limits[l.Name]; lim != nil {
			l.AutoDur = lim.dur
			l.DurSrc = lim.src
			l.DurSrcText = lim.text
		}
		l.Dur = number(o, "dur", l.AutoDur)
		if v := o["dur"]; v != nil && v != "" {
			l.DurSrc = "manual"
		}
		l.AutoH1 = l.Dur / 3
		l.AutoH2 = l.Dur * 2 / 3
		l.AutoAdd = l.Dur / 6
		l.H1 = number(o, "h1", l.AutoH1)
		l.H2 = number(o, "h2", l.AutoH2)
		l.Add = number(o, "add", l.AutoAdd)
		l.Done = number(o, "done", 0)
		l.PerBonus = number(o, "perBonus", 0)
		l.PerCode = number(o, "perCode", 0)
		l.Cap = number(o, "cap", 0)
		l.HintPen = number(o, "hintPen", 0)
		l.DoneBonusSec = l.Done * 60
		l.CodeValsText = option(o, "codeVals", "")
		l.CodeVals = map[string]float64{}
		for _, p := range strings.FieldsFunc(l.CodeValsText, func(r rune) bool { return r == ';' || r == ',' || r == '\n' }) {
			k, v, ok := strings.Cut(p, "=")
			if !ok || strings.TrimSpace(k) == "" {
				continue
			}
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
				l.CodeVals[normCode(k)] = n
			}
		}
		r.Levels = append(r.Levels, l)
	}
	return r
}

func bonusCodes(r *Record, l Level) float64 {
	n := 0.0
	for code := range r.Bonus {
		v, ok := l.CodeVals[code]
		if !ok {
			v = l.PerBonus
		}
		n += v
	}
	return n
}

func evalLevel(rec *Record, l Level, r Resolved) *Cell {
	durS, addS := l.Dur*60, l.Add*60
	c := &Cell{Kind: l.Kind, Rec: rec, St: "none", How: "none", Show: true}
	if rec != nil {
		c.Issued = rec.Issued
	}
	if rec != nil && c.Issued == nil && rec.Override != nil && rec.Override.At >= r.StopAt {
		c.Issued = new(r.StopAt)
		c.VirtualIssue = true
	}
	if rec == nil || c.Issued == nil || *c.Issued > r.StopAt {
		if r.NoneMode == "limit" {
			c.Sec = new(durS)
			c.CountSec = durS
			c.AddSec = addS
		} else {
			c.Show = false
			if r.NoneMode == "add" {
				c.AddSec = addS
			}
		}
		return c
	}
	issued := *c.Issued
	expires := issued + int64(math.Round(durS*1000))
	c.ExpiresAt = new(expires)
	if rec.SpoilerAt != nil {
		c.SpoilerElapsed = new(float64(*rec.SpoilerAt-issued) / 1000)
	}
	ov := rec.Override
	switch {
	case ov != nil && ov.At >= issued:
		c.How = ov.Kind
		c.End = new(ov.At)
	case rec.RefusedAt != nil:
		c.How = "refuse"
		c.End = rec.RefusedAt
	case rec.ClosedAt != nil:
		if rec.TimeoutAt != nil && *rec.TimeoutAt <= *rec.ClosedAt+2000 {
			c.How = "timeout"
			c.End = new(expires)
		} else {
			c.How = "code"
			c.End = rec.ClosedAt
		}
	case rec.DoneAt != nil && *rec.DoneAt <= expires:
		c.How = "code"
		c.End = rec.DoneAt
		c.LastDone = true
	case rec.TimeoutAt != nil:
		c.How = "timeout"
		c.End = new(expires)
	case expires <= r.StopAt:
		c.How = "timeout"
		c.Implicit = true
		c.End = new(expires)
	default:
		c.How = "stop"
	}
	if c.How == "code" && *c.End > r.StopAt {
		c.How = "stop"
		c.End = nil
	}
	if c.How == "credit" && r.RejectedMode == "first" && rec.Rejected != nil && rec.SpoilerAt != nil && math.Abs(float64(ov.At-*rec.SpoilerAt)) <= 2000 && rec.Rejected.T >= issued && rec.Rejected.T < *c.End {
		c.RejectedFix = &Correction{From: *c.End, To: rec.Rejected.T}
		c.End = new(rec.Rejected.T)
	}
	switch c.How {
	case "timeout", "refuse":
		c.St = "fail"
		c.Sec = new(durS)
		c.CountSec = durS
		c.AddSec = addS
	case "stop":
		c.St = "stop"
		sec := durS
		if r.StopMode != "limit" {
			sec = min(max(0, float64(r.StopAt-issued)/1000), durS)
		}
		c.Sec = new(sec)
		c.CountSec = sec
		c.AddSec = addS
	default:
		sec := float64(*c.End-issued) / 1000
		c.Sec = new(sec)
		c.CountSec = sec
		h := 2
		if sec < l.H1*60 {
			h = 0
		} else if sec < l.H2*60 {
			h = 1
		}
		h = max(h, min(2, rec.HintReq))
		c.St = fmt.Sprintf("h%d", h)
	}
	bon := bonusCodes(rec, l)
	if strings.HasPrefix(c.St, "h") {
		bon += l.Done
	}
	if l.Cap > 0 {
		bon = min(bon, l.Cap)
	}
	c.BonusSec = max(0, bon) * 60
	return c
}

func evalThrough(rec *Record, l Level) *Cell {
	c := &Cell{Kind: "through", Rec: rec, St: "none"}
	if rec == nil || rec.Issued == nil {
		return c
	}
	c.Show = true
	c.NC = len(rec.Codes)
	c.NB = len(rec.Bonus)
	c.Complete = l.ReqThrough > 0 && c.NC >= l.ReqThrough
	c.St = "empty"
	if c.NC > 0 || c.NB > 0 {
		c.St = "done"
	}
	n := float64(c.NC)*l.PerCode + bonusCodes(rec, l) - float64(rec.HintReq)*l.HintPen
	if c.Complete {
		n += l.Done
	}
	if l.Cap > 0 {
		n = min(n, l.Cap)
	}
	c.BonusSec = max(0, n) * 60
	c.Configured = l.Done > 0 || l.PerCode > 0 || l.PerBonus > 0 || len(l.CodeVals) > 0
	return c
}

func Compute(g *Game, cfg Config) Result {
	r := resolve(g, cfg)
	rows := []*Row{}
	for _, team := range g.Teams {
		tc := cfg.Teams[team]
		if tc["hidden"] == true {
			continue
		}
		row := &Row{Team: team, Cells: map[string]*Cell{}, Penalty: number(tc, "penalty", 0) * 60, ManualBonus: number(tc, "bonus", 0) * 60}
		if slices.Contains(g.Prequel, team) {
			row.Prequel = r.PrequelMin * 60
		}
		for _, l := range r.Levels {
			if l.Kind == "hidden" {
				continue
			}
			rec := g.Recs[team][l.Name]
			if l.Kind == "through" {
				cell := evalThrough(rec, l)
				row.Cells[l.Name] = cell
				row.Thr += cell.BonusSec
			} else {
				cell := evalLevel(rec, l, r)
				row.Cells[l.Name] = cell
				row.LvlBonus += cell.BonusSec
				if l.Kind == "main" {
					row.Clean += cell.CountSec
					row.Add += cell.AddSec
				}
			}
		}
		row.BonusOther = row.LvlBonus + row.Prequel + row.ManualBonus
		row.Total = row.Clean + row.Add + row.Penalty - row.Thr - row.BonusOther
		rows = append(rows, row)
	}
	for i := range r.Levels {
		l := &r.Levels[i]
		if l.Kind != "main" && l.Kind != "bonus" {
			continue
		}
		best := math.Inf(1)
		sum := 0.0
		for _, row := range rows {
			c := row.Cells[l.Name]
			if c != nil && strings.HasPrefix(c.St, "h") {
				best = min(best, math.Round(*c.Sec))
				if c.How != "avg" {
					l.Completed++
					sum += *c.Sec
				}
			}
		}
		if l.Completed > 0 {
			l.Average = new(sum / float64(l.Completed))
		}
		for _, row := range rows {
			c := row.Cells[l.Name]
			if c != nil && strings.HasPrefix(c.St, "h") && math.Round(*c.Sec) == best {
				c.Best = true
			}
		}
	}
	col := collate.New(language.Russian)
	slices.SortStableFunc(rows, func(a, b *Row) int {
		return cmp.Or(cmp.Compare(a.Total, b.Total), cmp.Compare(a.Clean, b.Clean), col.CompareString(a.Team, b.Team))
	})
	for i, row := range rows {
		row.Place = i + 1
		// JavaScript Math.round rounds negative halves toward +infinity too.
		if i > 0 && math.Floor(row.Total+0.5) == math.Floor(rows[i-1].Total+0.5) {
			row.Place = rows[i-1].Place
		}
	}
	byClean := slices.Clone(rows)
	slices.SortStableFunc(byClean, func(a, b *Row) int { return cmp.Compare(a.Clean, b.Clean) })
	for i, row := range byClean {
		row.PlaceClean = i + 1
		if i > 0 && math.Round(row.Clean) == math.Round(byClean[i-1].Clean) {
			row.PlaceClean = byClean[i-1].PlaceClean
		}
	}
	for i, row := range rows {
		if i > 0 {
			row.GapLeader = new(row.Total - rows[0].Total)
			row.GapPrev = new(row.Total - rows[i-1].Total)
		}
	}
	return Result{R: r, Rows: rows}
}

func Build(name string, data []byte, cfg *Config) (*Report, error) {
	events, err := Read(name, data)
	if err != nil {
		return nil, err
	}
	game, err := Parse(events)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = new(DefaultConfig(game))
	}
	if cfg.Game == nil {
		cfg.Game = map[string]any{}
	}
	if cfg.Levels == nil {
		cfg.Levels = map[string]map[string]any{}
	}
	if cfg.Teams == nil {
		cfg.Teams = map[string]map[string]any{}
	}
	return &Report{Game: game, Config: *cfg, Result: Compute(game, *cfg), Key: fmt.Sprintf("dzzzr-stats-v2:%x", sha256.Sum256(data))}, nil
}
