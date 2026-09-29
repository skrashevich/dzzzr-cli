package gamestats

import (
	"strings"
	"testing"
)

func TestHTMLSnapshot(t *testing.T) {
	report, err := Build("log.csv", []byte("Время;Действие;Команда;Уровень;Данные\n2026-09-26 22:00:00;выдан уровень;A;1.0;\n2026-09-26 22:01:00;завершена игра;;;\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	report.Result.Rows[0].Team = `<img src=x onerror=alert(1)>`
	out, err := HTMLSnapshot(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="stats-static-board"`, `href="#stats-cell-0-0"`, `id="stats-cell-0-0"`, `00:16:00`, `26.09 22:00:00`, `&lt;img`, `без JavaScript`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, bad := range []string{`<script`, `<img`, `onclick=`} {
		if strings.Contains(out, bad) {
			t.Errorf("unsafe HTML: %s", bad)
		}
	}
	report.Result.Rows[0].Total += 600
	out, err = HTMLSnapshot(report)
	if err != nil || !strings.Contains(out, "00:26:00") {
		t.Fatal("snapshot did not preserve modified total", err)
	}
}
