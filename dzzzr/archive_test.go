package dzzzr_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// archiveClient serves one archive page and records the request that asked
// for it.
func archiveClient(t *testing.T, page []byte, seen **http.Request) *dzzzr.Client {
	t.Helper()
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = r
		w.Header().Set("Content-Type", "text/html; charset=windows-1251")
		_, _ = w.Write(page)
	}), dzzzr.WithSession("TOKEN"))
	return c
}

func TestGetArchiveStatReadsResultsTable(t *testing.T) {
	var seen *http.Request
	c := archiveClient(t, fixture(t, "archive_stat.html"), &seen)
	st, err := c.GetArchiveStat(context.Background(), 1563)
	if err != nil {
		t.Fatal(err)
	}
	q := seen.URL.Query()
	if seen.URL.Path != "/moscow/" || q.Get("section") != "arc" || q.Get("gmid") != "1563" || q.Get("what") != "stat" {
		t.Errorf("request = %s", seen.URL)
	}
	if q.Get("s") != "TOKEN" {
		t.Errorf("session token not sent: %s", seen.URL)
	}
	if st.Name != "«Битва Экстрасенсов» (альфа-лига)" || st.Date != "17 июля 2026 г." || st.GameID != 1563 {
		t.Errorf("heading: %+v", st)
	}

	wantColumns := []dzzzr.ArchiveStatColumn{
		{Title: "1.0. Кулак", Level: true},
		{Title: "1.1. Машинки", Level: true},
		{Title: "Сквозное 1 Творчество (бонусное сквозное)", Level: true},
		{Title: "Штраф"}, {Title: "Бонусы"},
		// The engine wraps this header over two lines with <br>.
		{Title: "Чистое Время"},
		{Title: "Общее время"}, {Title: "Место"},
		{Title: "Отставание от лидера"}, {Title: "Отставание от предудущей"},
	}
	if len(st.Columns) != len(wantColumns) {
		t.Fatalf("columns = %+v", st.Columns)
	}
	for i, want := range wantColumns {
		if st.Columns[i] != want {
			t.Errorf("column %d = %+v, want %+v", i, st.Columns[i], want)
		}
	}

	if len(st.Teams) != 2 {
		t.Fatalf("teams = %+v", st.Teams)
	}
	first := st.Teams[0]
	// The social widget inside a team cell is a nested table; its rows must not
	// be read as teams, and its script must not end up in the team's name.
	if first.ID != 1235 || first.Name != "Rising" {
		t.Errorf("first team = %+v", first)
	}
	if len(first.Cells) != len(wantColumns) {
		t.Fatalf("cells = %q", first.Cells)
	}
	if got := st.Cell(&first, "Место"); got != "1 / 1" {
		t.Errorf("place = %q", got)
	}
	if got := st.Cell(&first, "Общее время"); got != "03:57:06" {
		t.Errorf("total time = %q", got)
	}
	if got := st.Cell(&first, "1.0. Кулак"); got != "00:03:01" {
		t.Errorf("level 1 = %q", got)
	}
	if got := st.Cell(&first, "нет такой колонки"); got != "" {
		t.Errorf("unknown column = %q", got)
	}
	if !strings.Contains(first.Notes["Штраф"], "10 мин. за уровень 1.1. Машинки") {
		t.Errorf("penalty note = %q", first.Notes["Штраф"])
	}
	if !strings.Contains(first.Notes["Бонусы"], "бонусный код") {
		t.Errorf("bonus note = %q", first.Notes["Бонусы"])
	}
	if second := st.Teams[1]; second.ID != 12 || second.Name != "Все В Сад" || len(second.Notes) != 0 {
		t.Errorf("second team = %+v", second)
	}
}

func TestGetArchiveStatRejectsPageWithoutTable(t *testing.T) {
	var seen *http.Request
	c := archiveClient(t, []byte("<html><body><!-- CONTENT --><h1>Архив игр</h1><!-- CONTENT END --></body></html>"), &seen)
	_, err := c.GetArchiveStat(context.Background(), 1563)
	if !dzzzr.IsUndecodable(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestArchiveRefusalsAreReported(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		check      func(error) bool
	}{
		{"нужна авторизация", "Требуется авторизация.", func(err error) bool {
			return dzzzr.AuthErrorKindOf(err) == dzzzr.AuthSession
		}},
		{"нет прав", "Недостаточно полномочий.", func(err error) bool {
			return dzzzr.IsEngineError(err)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen *http.Request
			page := "<html><body><!-- CONTENT -->\n\t\t" + tc.body + "\t\t<!-- CONTENT END --></body></html>"
			c := archiveClient(t, []byte(page), &seen)
			_, err := c.GetArchiveStat(context.Background(), 1563)
			if err == nil || !tc.check(err) {
				t.Fatalf("err = %v", err)
			}
			_, err = c.GetArchiveDescription(context.Background(), 1563)
			if err == nil || !tc.check(err) {
				t.Fatalf("description err = %v", err)
			}
		})
	}
}

func TestArchiveRejectsBadGameID(t *testing.T) {
	c := dzzzr.New("moscow")
	if _, err := c.GetArchiveStat(context.Background(), 0); err == nil {
		t.Error("stat accepted game 0")
	}
	if _, err := c.GetArchiveDescription(context.Background(), -1); err == nil {
		t.Error("description accepted game -1")
	}
	if _, err := c.GetGameLog(context.Background(), 0); err == nil {
		t.Error("log accepted game 0")
	}
}

func TestGetArchiveDescriptionReadsScenario(t *testing.T) {
	var seen *http.Request
	c := archiveClient(t, fixture(t, "archive_description.html"), &seen)
	d, err := c.GetArchiveDescription(context.Background(), 1563)
	if err != nil {
		t.Fatal(err)
	}
	if q := seen.URL.Query(); q.Get("what") != "comment" || q.Get("gmid") != "1563" {
		t.Errorf("request = %s", seen.URL)
	}
	if d.Name != "«Битва Экстрасенсов» (альфа-лига)" || d.Date != "17 июля 2026 г." {
		t.Errorf("heading: %+v", d)
	}

	// The page's own «Описание» heading titles the checkbox form, not a
	// section of the scenario.
	wantSections := []dzzzr.ArchiveSection{
		{Title: "Авторы", Text: "Psiha, Skvo"},
		{Title: "Легенда", Text: "Вас приветствует Новая Битва Экстрасенсов!\n\nБитва начинается."},
		{Title: "Зона покрытия", Text: "Москва"},
	}
	if len(d.Sections) != len(wantSections) {
		t.Fatalf("sections = %+v", d.Sections)
	}
	for i, want := range wantSections {
		if d.Sections[i] != want {
			t.Errorf("section %d = %+v, want %+v", i, d.Sections[i], want)
		}
	}
	// Only the poster: the buttons above it lead to the other views of the
	// same game and are navigation, not content.
	if len(d.Media) != 1 || !strings.HasSuffix(d.Media[0].URL, "/moscow/uploaded/moscow/Night/afisha/1563.jpg") {
		t.Errorf("poster = %+v", d.Media)
	}

	if len(d.Levels) != 2 {
		t.Fatalf("levels = %d", len(d.Levels))
	}
	first := d.Levels[0]
	if first.Title != "1.0. Кулак" {
		t.Errorf("title = %q", first.Title)
	}
	if len(first.Codes) != 1 || first.Codes[0] != "ЯВСЕВИЖУ" {
		t.Errorf("codes = %q", first.Codes)
	}
	// The commented-out <fb:like> block in the heading table is markup the
	// engine never renders; stripping tags without dropping comments first
	// leaves its attributes in front of the task.
	if first.Task != "Сколько пальцев показывает наш герой?" {
		t.Errorf("task = %q", first.Task)
	}
	if !strings.HasPrefix(first.Notes, "Тайминг уровня: 7-7-30") {
		t.Errorf("notes = %q", first.Notes)
	}
	// «null» is what the engine prints for a task whose codes have no
	// difficulty; it is not a difficulty.
	if first.Difficulty != "" {
		t.Errorf("difficulty = %q", first.Difficulty)
	}
	if first.Hint1 != "Кулак = 0." || first.Hint2 != "Это МКАД и 3 транспортное." {
		t.Errorf("hints = %q / %q", first.Hint1, first.Hint2)
	}
	if len(first.Spoilers) != 1 {
		t.Fatalf("spoilers = %+v", first.Spoilers)
	}
	if sp := first.Spoilers[0]; sp.Number != 1 || sp.Code != "РАБОЧИЙИКОЛХОЗНИЦА" || sp.Text != "Поздравляем, вы справились!" {
		t.Errorf("spoiler = %+v", sp)
	}
	if len(first.Sections) != 1 || first.Sections[0].Title != "Комментарий" || first.Sections[0].Text != "Кольца символизируют МКАД." {
		t.Errorf("sections = %+v", first.Sections)
	}
	if len(first.Media) == 0 || !strings.HasSuffix(first.Media[0].URL, "/uploaded/moscow/Night/games/1563/spoyler.jpg") {
		t.Errorf("media = %+v", first.Media)
	}

	second := d.Levels[1]
	// Codes are separated by non-breaking spaces and may contain ordinary
	// ones, so splitting after the entities are resolved would tear one apart.
	if len(second.Codes) != 2 || second.Codes[0] != "5D57R" || second.Codes[1] != "РИЧАРД ДА-И-НЕТ" {
		t.Errorf("codes = %q", second.Codes)
	}
	if second.Difficulty != "1+,2" {
		t.Errorf("difficulty = %q", second.Difficulty)
	}
	if len(second.Spoilers) != 0 {
		t.Errorf("spoilers = %+v", second.Spoilers)
	}
}

func TestGetArchiveDescriptionReportsWithheldTasks(t *testing.T) {
	// What the engine sends a visitor whose team may not read scenarios: the
	// header, then its own explanation and a few games offered as samples.
	page := `<html><body><!-- CONTENT --><h1>Архив игр</h1>` +
		`<div class=Date>17 июля 2026 г.</div><h1>&laquo;Битва&raquo; (альфа-лига)</h1>` +
		`<h2>Описание</h2><form></form>` +
		`<div class=grayBox><h2>Авторы</h2>Psiha</div><br>` +
		`<div class=grayBox><h2>Зона покрытия</h2><p>Москва</p></div>` +
		`<p style='color:#ff9900'><strong>Для просмотра текста всех заданий игры ваша команда должна участвовать хотя бы в одной из игр за последние три месяца.</strong></p>` +
		`<p><strong>Полностью примеры заданий можно просмотреть в следующих играх</strong>:</p>` +
		`<p>2 ноября 2008 г. <a href=?section=arc&gmid=137&what=comment>Make love, not war</a></p>` +
		`<!-- CONTENT END --></body></html>`
	var seen *http.Request
	c := archiveClient(t, []byte(page), &seen)
	d, err := c.GetArchiveDescription(context.Background(), 1563)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Restricted || len(d.Levels) != 0 {
		t.Fatalf("restricted=%v levels=%+v", d.Restricted, d.Levels)
	}
	// The header still comes through, and the notice must not be swallowed by
	// the section above it.
	if len(d.Sections) != 2 || d.Sections[1].Text != "Москва" {
		t.Fatalf("sections = %+v", d.Sections)
	}
	if !strings.HasPrefix(d.Notice, "Для просмотра текста всех заданий") {
		t.Fatalf("notice = %q", d.Notice)
	}
	// Everything after the notice belongs to it, samples included.
	if !strings.Contains(d.Notice, "Make love, not war") {
		t.Errorf("notice without the samples: %q", d.Notice)
	}
}

func TestArchiveDescriptionKeepsTasksWhenNoticeIsNotLast(t *testing.T) {
	// Guard for the tail rule: a notice with headings after it must cost only
	// itself, never the scenario that follows.
	page := `<html><body><!-- CONTENT --><h1>Архив игр</h1>` +
		`<p><strong>Для просмотра текста всех заданий игры нужно играть чаще.</strong></p>` +
		`<h2>Задание: 1.0. Кулак<br><span id=Code>Код: ЯВСЕВИЖУ &nbsp;</span></h2>` +
		`<div class=boxOrang id=left><p>Текст</p></div>` +
		`<!-- CONTENT END --></body></html>`
	var seen *http.Request
	c := archiveClient(t, []byte(page), &seen)
	d, err := c.GetArchiveDescription(context.Background(), 1563)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Restricted || d.Notice == "" {
		t.Fatalf("restricted=%v notice=%q", d.Restricted, d.Notice)
	}
	if len(d.Levels) != 1 || d.Levels[0].Task != "Текст" || d.Levels[0].Codes[0] != "ЯВСЕВИЖУ" {
		t.Fatalf("levels = %+v", d.Levels)
	}
}
