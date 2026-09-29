package gamestats

import (
	"archive/zip"
	"bytes"
	"encoding/json/v2"
	"os"
	"reflect"
	"strings"
	"testing"
)

func event(at, action, team, level, data string) Event {
	return Event{T: *ParseTime("2026-09-26 " + at), A: action, Team: team, Level: level, Data: data}
}

func TestScoring(t *testing.T) {
	events := []Event{
		event("22:00:00", "выдан уровень", "A", "1.0", ""),
		event("22:00:00", "выдан уровень", "B", "1.0", ""),
		event("22:00:00", "выдан сквозной уровень", "A", "Сквозное 1", ""),
		event("22:00:00", "принят код к приквелу", "A", "", ""),
		event("22:05:00", "принят код", "A", "Сквозное 1", "ONE"),
		event("22:06:00", "принят код", "A", "Сквозное 1", " one "),
		event("22:07:00", "запрос первой подсказки", "A", "Сквозное 1", ""),
		event("22:09:00", "принят бонусный код", "A", "1.0", "BON"),
		event("22:10:00", "принят код", "A", "1.0", "X"),
		event("22:10:00", "выдан уровень", "A", "2.0", ""),
		event("22:30:00", "вышло время", "B", "1.0", ""),
		event("22:30:00", "выдан уровень", "B", "2.0", ""),
		event("22:35:00", "отказ от уровня", "B", "2.0", ""),
		event("22:40:00", "завершена игра", "", "", ""),
	}
	g, err := Parse(events)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig(g)
	cfg.Game["prequelMin"] = "2"
	cfg.Levels["1.0"] = map[string]any{"h1": "10", "h2": "20", "perBonus": "3"}
	cfg.Levels["Сквозное 1"] = map[string]any{"done": "5", "hintPen": "1"}
	r := Compute(g, cfg)
	a, b := r.Rows[0], r.Rows[1]
	if a.Team != "A" || a.Clean != 2400 || a.Add != 300 || a.Thr != 240 || a.BonusOther != 300 || a.Total != 2160 {
		t.Fatalf("A: %+v", a)
	}
	if a.Cells["1.0"].St != "h1" || !a.Cells["2.0"].Implicit || len(g.Recs["A"]["Сквозное 1"].Codes) != 1 {
		t.Fatal("hint boundary, implicit timeout or duplicate code")
	}
	if b.Total != 4200 || b.Cells["2.0"].How != "refuse" || b.Place != 2 {
		t.Fatalf("B: %+v", b)
	}
}

func TestCorrectionsAndStopModes(t *testing.T) {
	events := []Event{event("22:00:00", "выдан уровень", "A", "1.0", ""), event("22:01:00", "введен неверный код к спойлеру 1", "A", "1.0", "ANSWER"), event("22:05:00", "введен правильный код к спойлеру 1", "A", "1.0", "ANSWER"), event("22:06:00", "засчитан уровень", "A", "1.0", "2026-09-26 22:05:00"), event("22:10:00", "выдан уровень", "A", "2.0", ""), event("22:12:00", "завершена игра", "", "", ""), event("22:15:00", "засчитано среднее время", "A", "3.0", "2026-09-26 22:14:00")}
	g, err := Parse(events)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig(g)
	r := Compute(g, cfg)
	cells := r.Rows[0].Cells
	if *cells["1.0"].Sec != 60 || cells["1.0"].RejectedFix == nil || *cells["2.0"].Sec != 120 || !cells["3.0"].VirtualIssue || *cells["3.0"].Sec != 120 {
		t.Fatalf("corrections: %+v", cells)
	}
	cfg.Game["rejectedMode"] = "ignore"
	cfg.Game["stopMode"] = "limit"
	r = Compute(g, cfg)
	if *r.Rows[0].Cells["1.0"].Sec != 300 || *r.Rows[0].Cells["2.0"].Sec != 5400 {
		t.Fatal("mode overrides ignored")
	}
}

func TestResetMissingLevelsTiesAndHidden(t *testing.T) {
	events := []Event{event("20:00:00", "выдан уровень", "Test", "Old", ""), event("21:00:00", "удалена статистика", "", "", ""), event("21:30:00", "запланировано задание", "A", "2.0", ""), event("22:00:00", "выдан уровень", "A", "1.0", ""), event("22:00:00", "выдан уровень", "B", "1.0", ""), event("22:01:00", "завершена игра", "", "", "")}
	g, err := Parse(events)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Teams) != 2 || g.CutAt == nil {
		t.Fatal("reset did not remove test run")
	}
	cfg := DefaultConfig(g)
	r := Compute(g, cfg)
	if r.Rows[0].Place != 1 || r.Rows[1].Place != 1 || r.Rows[0].Cells["2.0"].CountSec != 5400 {
		t.Fatal("tie or missing level")
	}
	cfg.Game["noneMode"] = "add"
	r = Compute(g, cfg)
	if r.Rows[0].Cells["2.0"].Show || r.Rows[0].Cells["2.0"].AddSec != 900 {
		t.Fatal("add-only mode")
	}
	cfg.Game["noneMode"] = "ignore"
	cfg.Teams["B"] = map[string]any{"hidden": true}
	r = Compute(g, cfg)
	if len(r.Rows) != 1 || r.Rows[0].Cells["2.0"].AddSec != 0 {
		t.Fatal("ignore or hidden")
	}
}

func TestReadAndExport(t *testing.T) {
	csv := "Время;Действие;Команда;Уровень;Данные;Данные\n2026-09-26 22:00:00;выдан уровень;\"A;B\";8;;\n2026-09-26 22:01:00;завершена игра;;;;\n"
	report, err := Build("log.csv", []byte(csv), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Game.Teams[0] != "A;B" || report.Game.Levels[0].Name != "8.0" {
		t.Fatal("CSV parsing")
	}
	out, err := ExportXLSX(report)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := readXLSX(out)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "Команда" || rows[1][0] != "A;B" || !strings.Contains(strings.Join(rows[0], ","), "Общее время") {
		t.Fatal("export table")
	}
	z, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 6 {
		t.Fatalf("xlsx parts: %d", len(z.File))
	}
	if _, err := Read("invalid.xlsx", []byte("bad")); err == nil {
		t.Fatal("invalid ZIP accepted")
	}
	if _, err := Build("empty.csv", []byte("Время;Действие\n"), nil); err == nil {
		t.Fatal("empty log accepted")
	}
}

func TestNegativeTotalTieMatchesReference(t *testing.T) {
	g, err := Parse([]Event{
		event("22:00:00", "выдан уровень", "A", "1.0", ""),
		event("22:00:00", "выдан уровень", "B", "1.0", ""),
		event("22:01:00", "завершена игра", "", "", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig(g)
	cfg.Game["defaultDur"] = "1"
	cfg.Teams["A"] = map[string]any{"bonus": "1.175"}
	cfg.Teams["B"] = map[string]any{"bonus": "1.17"}
	r := Compute(g, cfg)
	if r.Rows[0].Total != -0.5 || r.Rows[0].Place != 1 || r.Rows[1].Place != 1 {
		t.Fatalf("negative totals must share first place: %+v %+v", r.Rows[0], r.Rows[1])
	}
}

func TestJSONRows(t *testing.T) {
	data := []byte(`{"10":["2026-09-26 22:00:00","выдан уровень",42,2,null,null],"1":["Время","Действие","Команда","Уровень","Данные","Данные"],"11":["2026-09-26 22:01:00","завершена игра",null,null,null,null],"2":["2026-09-26 22:00:00","выдан уровень",42,1,null,null]}`)
	report, err := Build("log.JSON", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, second := report.Game.Recs["42"]["1.0"], report.Game.Recs["42"]["2.0"]
	if first.Order != 1 || second.Order != 2 || first.ClosedAt == nil || second.ClosedAt != nil {
		t.Fatal("numbered JSON rows lost event order")
	}
	table, err := readJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	array, err := json.Marshal(table)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Build("log.json", append([]byte("\xef\xbb\xbf"), array...), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Result, other.Result) {
		t.Fatal("object and array JSON results differ")
	}
	for _, bad := range []string{`{`, `{}`, `null`, `{"wrong":[]}`, `{"1":"row"}`, `[["Время","Действие"],[{},true]]`, `{"01":[]}`} {
		if _, err := Build("bad.json", []byte(bad), nil); err == nil {
			t.Errorf("accepted invalid JSON log %s", bad)
		}
	}
}

func TestRealJSONMatchesXLSX(t *testing.T) {
	jsonFile, xlsxFile := os.Getenv("DZZZR_STATS_TEST_JSON"), os.Getenv("DZZZR_STATS_TEST_LOG")
	if jsonFile == "" || xlsxFile == "" {
		t.Skip("set DZZZR_STATS_TEST_JSON and DZZZR_STATS_TEST_LOG")
	}
	read := func(file string) *Report {
		t.Helper()
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		report, err := Build(file, data, nil)
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	j, x := read(jsonFile), read(xlsxFile)
	if j.Game.NRows != 17848 || !reflect.DeepEqual(j.Result, x.Result) {
		t.Fatal("real JSON and XLSX results differ")
	}
}

// Optional local regression: user-provided game logs are never checked in.
func TestRealLog1566(t *testing.T) {
	file := os.Getenv("DZZZR_STATS_TEST_LOG")
	if file == "" {
		t.Skip("set DZZZR_STATS_TEST_LOG to log-1566.xlsx")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Build(file, data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Game.NRows != 17848 || len(report.Game.Teams) != 9 || len(report.Game.Levels) != 24 {
		t.Fatalf("shape: %d / %d / %d", report.Game.NRows, len(report.Game.Teams), len(report.Game.Levels))
	}
	want := []struct{ team, total string }{{"Rising", "02:13:04"}, {"DaIGR", "02:41:58"}, {"MADS", "02:43:16"}, {"Стальные Яйца", "02:53:09"}, {"42", "03:08:40"}, {"Кошмар", "03:26:24"}, {"Ночная Смена", "04:10:45"}, {"L_n_P", "06:41:48"}, {"Все В Сад", "08:33:43"}}
	for i, w := range want {
		r := report.Result.Rows[i]
		if r.Team != w.team || FormatDuration(r.Total, false) != w.total {
			t.Errorf("row %d: %s %s, want %s %s", i, r.Team, FormatDuration(r.Total, false), w.team, w.total)
		}
	}
	if oracle := os.Getenv("DZZZR_STATS_TEST_ORACLE"); oracle != "" {
		b, err := os.ReadFile(oracle)
		if err != nil {
			t.Fatal(err)
		}
		var rows []*Row
		if err := json.Unmarshal(b, &rows); err != nil {
			t.Fatal(err)
		}
		for i, w := range rows {
			got := report.Result.Rows[i]
			if got.Clean != w.Clean || got.Add != w.Add || got.Thr != w.Thr || got.BonusOther != w.BonusOther || got.Place != w.Place || got.PlaceClean != w.PlaceClean {
				t.Errorf("totals differ for %s", w.Team)
			}
			for level, c := range w.Cells {
				actual := *got.Cells[level]
				actual.Rec = nil
				actual.SpoilerElapsed = nil
				actual.ExpiresAt = nil
				left, _ := json.Marshal(actual)
				right, _ := json.Marshal(c)
				if !bytes.Equal(left, right) {
					t.Errorf("%s / %s\ngot %s\nwant %s", w.Team, level, left, right)
				}
			}
		}
	}
}
