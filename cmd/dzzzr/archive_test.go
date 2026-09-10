package main

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// archiveServer answers every request with one page and records the queries it
// was asked for.
func archiveServer(t *testing.T, body []byte) (*[]string, string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RequestURI())
		w.Header().Set("Content-Type", "text/html; charset=windows-1251")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return &seen, srv.URL + "/moscow/"
}

func archiveFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../dzzzr/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGameStatHidesLevelColumnsUntilAsked(t *testing.T) {
	isolate(t)
	seen, base := archiveServer(t, archiveFixture(t, "archive_stat.html"))

	code, out, errOut := runCLI(t, "-base-url", base, "game-stat", "1563")
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{
		"Игра 1563: «Битва Экстрасенсов» (альфа-лига)",
		"17 июля 2026 г.",
		"Rising", "Все В Сад",
		"Общее время", "Место",
		"03:57:06", "1 / 1",
		"Колонки заданий скрыты",
		// The breakdown the engine hides behind a tooltip.
		"10 мин. за уровень 1.1. Машинки",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод без %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "1.0. Кулак") {
		t.Errorf("колонки заданий показаны без -levels:\n%s", out)
	}
	if len(*seen) != 1 || !strings.Contains((*seen)[0], "what=stat") || !strings.Contains((*seen)[0], "gmid=1563") {
		t.Errorf("запросы = %q", *seen)
	}

	code, out, errOut = runCLI(t, "-base-url", base, "game-stat", "-levels", "1563")
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "1.0. Кулак") || !strings.Contains(out, "00:03:01") {
		t.Errorf("-levels не показал задания:\n%s", out)
	}
}

func TestGameStatJSON(t *testing.T) {
	isolate(t)
	_, base := archiveServer(t, archiveFixture(t, "archive_stat.html"))
	code, out, errOut := runCLI(t, "-base-url", base, "-json", "game-stat", "1563")
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	var st dzzzr.ArchiveStat
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatal(err)
	}
	if st.GameID != 1563 || len(st.Teams) != 2 || st.Teams[0].ID != 1235 {
		t.Fatalf("статистика = %+v", st)
	}
}

func TestGameDescPrintsScenario(t *testing.T) {
	isolate(t)
	seen, base := archiveServer(t, archiveFixture(t, "archive_description.html"))
	code, out, errOut := runCLI(t, "-base-url", base, "game-desc", "1563")
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{
		"Игра 1563: «Битва Экстрасенсов» (альфа-лига)",
		"Авторы", "Psiha, Skvo",
		"Легенда",
		"Задание 1: 1.0. Кулак",
		"Коды: ЯВСЕВИЖУ",
		"Сколько пальцев показывает наш герой?",
		"Примечания к заданию:",
		"Подсказка 1:", "Подсказка 2:",
		"Спойлер 1 (код РАБОЧИЙИКОЛХОЗНИЦА):",
		"Комментарий:",
		"Задание 2: 1.1. Машинки",
		"Коды: 5D57R, РИЧАРД ДА-И-НЕТ",
		"Код сложности: 1+,2",
		"картинка:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод без %q:\n%s", want, out)
		}
	}
	if len(*seen) != 1 || !strings.Contains((*seen)[0], "what=comment") {
		t.Errorf("запросы = %q", *seen)
	}
}

func TestGameLogPrintsEngineColumns(t *testing.T) {
	isolate(t)
	writeSession(t)
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"log":[{"time":"21:03:01","team":"Rising","level":1,"event":"уровень выдан"}]}`))
	}))
	defer srv.Close()

	code, out, errOut := runCLI(t, "-base-url", srv.URL+"/moscow/", "game-log", "1563")
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{"time", "team", "level", "event", "21:03:01", "Rising", "уровень выдан"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод без %q:\n%s", want, out)
		}
	}
	if !strings.Contains(seen, "gameLogJSON.php") || !strings.Contains(seen, "gmid=1563") {
		t.Errorf("запрос = %q", seen)
	}
}

func TestGameLogWithoutRightsExplainsWhy(t *testing.T) {
	isolate(t)
	writeSession(t)
	// The engine answers a visitor it will not show the log with nothing at
	// all, which must not read as an empty log.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	code, _, errOut := runCLI(t, "-base-url", srv.URL+"/moscow/", "game-log", "1563")
	if code == 0 || !strings.Contains(errOut, "капитан") {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
}

func TestArchiveCommandsRejectBadGameID(t *testing.T) {
	isolate(t)
	writeSession(t)
	for _, name := range []string{"game-stat", "game-desc", "game-log"} {
		t.Run(name, func(t *testing.T) {
			if code, _, _ := runCLI(t, "-base-url", "http://127.0.0.1:1/moscow/", name); code == 0 {
				t.Error("команда без номера игры принята")
			}
			if code, _, errOut := runCLI(t, "-base-url", "http://127.0.0.1:1/moscow/", name, "х"); code == 0 || !strings.Contains(errOut, "номер игры") {
				t.Errorf("код выхода = %d, stderr = %q", code, errOut)
			}
		})
	}
}

// The results are public, so they must be readable with no session on disk.
func TestArchiveViewsRunWithoutSession(t *testing.T) {
	isolate(t)
	_, base := archiveServer(t, archiveFixture(t, "archive_stat.html"))
	if code, _, errOut := runCLI(t, "-base-url", base, "game-stat", "1563"); code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
}

func TestGameDescExplainsWithheldTasks(t *testing.T) {
	isolate(t)
	page := `<html><body><!-- CONTENT --><div class=Date>17 июля 2026 г.</div><h1>&laquo;Битва&raquo;</h1>` +
		`<h2>Описание</h2><form></form><div class=grayBox><h2>Легенда</h2>Начинаем</div>` +
		`<p style='color:#ff9900'><strong>Для просмотра текста всех заданий игры ваша команда должна участвовать хотя бы в одной из игр за последние три месяца.</strong></p>` +
		`<!-- CONTENT END --></body></html>`
	_, base := archiveServer(t, []byte(page))
	code, out, errOut := runCLI(t, "-base-url", base, "game-desc", "1563")
	// The header is still worth printing, so this is not a failure.
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{"Легенда", "Начинаем", "Тексты заданий недоступны", "за последние три месяца"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод без %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Задания не опубликованы") {
		t.Errorf("отказ по правам выдан за неопубликованный сценарий:\n%s", out)
	}
}

func TestGameStatFoldsRepeatedBreakdownLines(t *testing.T) {
	isolate(t)
	note := strings.Repeat("0 мин. за бонусный код к заданию 2.1. Ширма;&#10;", 3)
	page := `<html><body><!-- CONTENT --><div class=Date>17 июля 2026 г.</div><h1>&laquo;Битва&raquo;</h1>` +
		`<table class=tbl id=suxx><tr id=shapka><th>КОМАНДЫ</th><th>Штраф</th></tr>` +
		`<tr><td><a href=?section=teams&teamID=12>Rising</a></td><td title='` + note + `'>0:00</td></tr></table>` +
		`<!-- CONTENT END --></body></html>`
	_, base := archiveServer(t, []byte(page))
	code, out, errOut := runCLI(t, "-base-url", base, "game-stat", "1563")
	if code != 0 {
		t.Fatalf("код выхода = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "(×3)") || strings.Count(out, "2.1. Ширма") != 1 {
		t.Errorf("повторы не свёрнуты:\n%s", out)
	}
}
