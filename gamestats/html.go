// Statistics presentation adapted from dozor_stats.html.
// Original algorithm author: Sergey <sergey@luberg.me> Luberg.
package gamestats

import (
	"bytes"
	"cmp"
	"encoding/json/v2"
	"html/template"
	"slices"
	"strings"
)

// HTMLSnapshot renders already computed results. It needs neither scripts nor
// WebAssembly, and remains readable if an embedded viewer disables both.
func HTMLSnapshot(report *Report) (string, error) {
	cfg, err := json.Marshal(report.Config)
	if err != nil {
		return "", err
	}
	var levels []Level
	for _, l := range report.Result.R.Levels {
		if l.Kind != "hidden" {
			levels = append(levels, l)
		}
	}
	var out bytes.Buffer
	err = snapshotTemplate.Execute(&out, struct {
		Report *Report
		Levels []Level
		Config string
	}{report, levels, string(cfg)})
	return out.String(), err
}

func snapshotValue(c *Cell, l Level) string {
	if c == nil || !c.Show {
		return "—"
	}
	if l.Kind == "through" {
		if c.Configured {
			return FormatDuration(c.BonusSec, true)
		}
		return "бонус не задан"
	}
	if l.Kind == "bonus" {
		if strings.HasPrefix(c.St, "h") {
			return FormatDuration(l.DoneBonusSec, true)
		}
		return "—"
	}
	return hms(c.Sec)
}

func CellNotes(c *Cell) []string {
	var notes []string
	if c.Implicit {
		notes = append(notes, "Лимит истёк, но записи о таймауте нет. Проверьте лимит уровня.")
	}
	if c.VirtualIssue {
		notes = append(notes, "Выдача уровня восстановлена при завершении игры.")
	}
	if c.RejectedFix != nil {
		notes = append(notes, "Время исправлено по первому вводу верного ответа, отклонённого движком.")
	}
	if r := c.Rec; r != nil {
		if r.Rejected != nil {
			notes = append(notes, "Движок отклонял ответ «"+r.Rejected.Raw+"», который позднее был принят. Первый ввод: "+dateString(&r.Rejected.T)+".")
		}
		if r.Override != nil && r.Issued == nil && !c.VirtualIssue {
			notes = append(notes, "Зачёт без выдачи уровня — проверьте исходные события.")
		}
		if c.LastDone && r.TimeoutAt != nil {
			notes = append(notes, "Коды последнего уровня введены до таймаута; поздний таймаут не учитывается.")
		}
	}
	return notes
}

var snapshotTemplate = template.Must(template.New("snapshot").Funcs(template.FuncMap{
	"duration": func(n float64) string { return FormatDuration(n, false) },
	"hms":      hms, "date": func(v any) string {
		switch n := v.(type) {
		case int64:
			return dateString(&n)
		case *int64:
			return dateString(n)
		}
		return "—"
	}, "value": snapshotValue, "notes": CellNotes,
	"sum": func(a, b float64) float64 { return a + b },
	"state": func(s string) string {
		return map[string]string{"h0": "без подсказок", "h1": "после первой подсказки", "h2": "после второй подсказки", "fail": "не выполнено", "none": "не получено", "stop": "не выполнено до стоп-игры"}[s]
	},
	"method": func(s string) string {
		return map[string]string{"code": "введены все коды", "credit": "засчитан организатором", "avg": "засчитано среднее время", "timeout": "истёк лимит", "refuse": "отказ от уровня", "stop": "стоп-игра", "none": "не получен"}[s]
	},
	"kind": func(s string) string {
		return map[string]string{"main": "зачётный", "bonus": "бонусный", "through": "сквозной"}[s]
	},
	"codes": func(m map[string]*Code) []*Code {
		a := make([]*Code, 0, len(m))
		for _, c := range m {
			a = append(a, c)
		}
		slices.SortFunc(a, func(a, b *Code) int { return cmp.Or(cmp.Compare(a.T, b.T), strings.Compare(a.Raw, b.Raw)) })
		return a
	},
}).Parse(`
<section id="stats-static" class="stats-snapshot">
<h1>Статистика уровней DozoR</h1>
<p id="stats-static-status">Сохранённый расчёт: таблица и подробности доступны без JavaScript. Для изменения параметров откройте файл в полноценном браузере с поддержкой JavaScript и WebAssembly.</p>
<p>{{.Report.Game.NRows}} строк журнала · {{len .Report.Result.Rows}} команд · стоп-игра {{date .Report.Result.R.StopAt | printf "%s"}}</p>
{{if .Report.Config.Preset}}<p>Параметры игры: {{.Report.Config.Preset}}</p>{{end}}
<div class="board-panel"><div class="board-scroll"><table class="board" id="stats-static-board">
<thead><tr><th>Команда</th>{{range $j,$l:=.Levels}}<th><a href="#stats-level-{{$j}}">{{$l.Name}}</a><br>{{kind $l.Kind}}</th>{{end}}<th>Штраф</th><th>Бонусы</th><th>Чистое время</th><th>Общее время</th><th>Место / по чистому</th><th>От лидера</th><th>От предыдущего</th></tr></thead>
<tbody>{{range $i,$row:=.Report.Result.Rows}}<tr><th><a href="#stats-team-{{$i}}">{{$row.Team}}</a></th>{{range $j,$l:=$.Levels}}{{$c:=index $row.Cells $l.Name}}<td class="st-{{$c.St}}{{if $c.Best}} best{{end}}"><a href="#stats-cell-{{$i}}-{{$j}}">{{value $c $l}}</a></td>{{end}}<td>{{duration (sum $row.Add $row.Penalty)}}</td><td>{{duration (sum $row.Thr $row.BonusOther)}}</td><td>{{duration $row.Clean}}</td><td>{{duration $row.Total}}</td><td>{{$row.Place}} / {{$row.PlaceClean}}</td><td>{{hms $row.GapLeader}}</td><td>{{hms $row.GapPrev}}</td></tr>{{end}}</tbody>
</table></div></div>
<p>Зелёный — без подсказок, синий — после первой, белый — после второй; красный — не выполнено. Выделены лучшие времена. Нажмите на время для перехода к событиям уровня.</p>
<h2>Параметры уровней</h2>
{{range $j,$l:=.Levels}}<div class="card details" id="stats-level-{{$j}}"><h3>{{$l.Name}} · {{kind $l.Kind}}</h3>
<p>Лимит {{$l.Dur}} мин · подсказки {{$l.H1}} / {{$l.H2}} мин · добавочное {{$l.Add}} мин. Источник: {{$l.DurSrcText}} ({{$l.DurSrc}}).</p>
<p>Бонус за выполнение {{$l.Done}} мин · за код {{$l.PerCode}} мин · за бонусный код {{$l.PerBonus}} мин · предел {{$l.Cap}} мин · штраф за запрос подсказки {{$l.HintPen}} мин. Особые коды: {{$l.CodeValsText}}</p>
{{if and (ne $l.Kind "through") (ne $l.DurSrc "manual") (ne $l.DurSrc "timeout")}}<p>Проверьте лимит: восстановлен косвенно, без таймаута.</p>{{end}}
</div>{{end}}
<h2>Подробности команд и замечания</h2>
{{range $i,$row:=.Report.Result.Rows}}<details open class="card details" id="stats-team-{{$i}}"><summary><b>{{$row.Team}} · {{duration $row.Total}}</b></summary>
<p>Чистое {{duration $row.Clean}} + добавочное {{duration $row.Add}} + ручной штраф {{duration $row.Penalty}} − сквозные {{duration $row.Thr}} − остальные бонусы {{duration $row.BonusOther}} = {{duration $row.Total}}.</p>
{{range $j,$l:=$.Levels}}{{$c:=index $row.Cells $l.Name}}<article id="stats-cell-{{$i}}-{{$j}}"><h3>{{$l.Name}} · {{state $c.St}}</h3>
<p>{{method $c.How}} · выдан {{date $c.Issued}} · закрыт {{date $c.End}} · длительность {{hms $c.Sec}} · в зачёт {{duration $c.CountSec}} · добавочное {{duration $c.AddSec}} · бонус {{duration $c.BonusSec}}.</p>
{{range notes $c}}<p class="snapshot-warning">{{.}}</p>{{end}}
{{with $c.Rec}}<p>Запросов подсказки: {{.HintReq}}. Отказ: {{date .RefusedAt}}. Таймаут: {{date .TimeoutAt}}. Спойлер: {{date .SpoilerAt}}.</p>
{{template "codes" (codes .Codes)}}{{if .Bonus}}<h4>Бонусные коды</h4>{{template "codes" (codes .Bonus)}}{{end}}{{if .Spoilers}}<h4>Принятые спойлеры</h4>{{template "codes" (codes .Spoilers)}}{{end}}{{if .SpoilerBad}}<h4>Отклонённые спойлеры</h4>{{template "codes" (codes .SpoilerBad)}}{{end}}
{{end}}<p><a href="#stats-static-board">К таблице</a></p></article>{{end}}
</details>{{end}}
<details class="card details"><summary>Сохранённые параметры расчёта</summary><pre>{{.Config}}</pre></details>
<p>Автор исходного алгоритма: Sergey &lt;sergey@luberg.me&gt; Luberg.</p>
</section>
{{define "codes"}}{{if .}}<div class="cfg-scroll"><table class="cfg"><thead><tr><th>Код</th><th>Время</th><th>От выдачи</th><th>Игрок</th></tr></thead><tbody>{{range .}}<tr><td>{{.Raw}}</td><td>{{date .T}}</td><td>{{hms .Elapsed}}</td><td>{{.Player}}</td></tr>{{end}}</tbody></table></div>{{end}}{{end}}
`))
