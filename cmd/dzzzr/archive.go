package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func init() {
	register(
		command{Name: "game-stat", Usage: "game-stat ИГРА [-levels]", Auth: authOptional, Run: cmdGameStat,
			Help: "Статистика завершённой игры по всем командам"},
		command{Name: "game-desc", Usage: "game-desc ИГРА", Auth: authOptional, Run: cmdGameDesc,
			Help: "Описание игры: легенда всем, а задания с кодами, подсказками и спойлерами — недавно игравшим командам"},
		command{Name: "game-log", Usage: "game-log ИГРА", Auth: authPlayer, Run: cmdGameLog,
			Help: "Полный лог игры; доступен капитану, замкапитана или начштаба недавно игравшей команды"},
	)
}

// gameIDArg reads the game number a command was called with.
func gameIDArg(args []string, usage string) (int, error) {
	if err := needArgs(args, 1, usage); err != nil {
		return 0, err
	}
	id, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || id <= 0 {
		return 0, fatal("неверный номер игры %q", args[0])
	}
	return id, nil
}

// oneLine folds a value onto one line: a tabwriter aligns columns by counting
// cells, and a newline inside one shifts every column after it.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// printArchiveTitle writes the heading both archive views share.
func printArchiveTitle(w io.Writer, name, date string, gameID int) {
	_, _ = fmt.Fprintf(w, "Игра %d: %s\n", gameID, dash(name))
	if date != "" {
		_, _ = fmt.Fprintf(w, "Дата:   %s\n", date)
	}
}

func cmdGameStat(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	id, err := gameIDArg(args, "game-stat ИГРА")
	if err != nil {
		return err
	}
	st, err := c.GetArchiveStat(ctx, id)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, st)
	}
	printArchiveTitle(cfg.stdout, st.Name, st.Date, st.GameID)
	if len(st.Teams) == 0 {
		_, _ = fmt.Fprintln(cfg.stdout, "\nРезультатов нет.")
		return nil
	}
	// The task columns are one per level and there can be thirty of them, so a
	// terminal gets the summary unless the player asks for the whole table.
	show := make([]int, 0, len(st.Columns))
	for i, col := range st.Columns {
		if cfg.withTasks || !col.Level {
			show = append(show, i)
		}
	}
	_, _ = fmt.Fprintln(cfg.stdout)
	tw := newTable(cfg.stdout)
	header := []string{"Команда"}
	for _, i := range show {
		header = append(header, st.Columns[i].Title)
	}
	_, _ = fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, t := range st.Teams {
		row := []string{oneLine(dash(t.Name))}
		for _, i := range show {
			cell := ""
			if i < len(t.Cells) {
				cell = t.Cells[i]
			}
			row = append(row, oneLine(dash(cell)))
		}
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if !cfg.withTasks && hasLevelColumns(st.Columns) {
		_, _ = fmt.Fprintln(cfg.stdout, "\nКолонки заданий скрыты; «-levels» показывает их.")
	}
	printStatNotes(cfg.stdout, st)
	return nil
}

// foldRepeats collapses runs of identical lines into one with a count. The
// engine writes a breakdown one bonus code at a time, so a team that found
// twenty of them on the same level gets twenty identical lines; the count says
// the same thing in one. The -json output keeps the engine's own text.
func foldRepeats(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		n := 1
		for i+n < len(lines) && lines[i+n] == lines[i] {
			n++
		}
		if n > 1 {
			out = append(out, fmt.Sprintf("%s (×%d)", lines[i], n))
		} else {
			out = append(out, lines[i])
		}
		i += n
	}
	return strings.Join(out, "\n")
}

func hasLevelColumns(columns []dzzzr.ArchiveStatColumn) bool {
	for _, c := range columns {
		if c.Level {
			return true
		}
	}
	return false
}

// printStatNotes writes the breakdowns the engine hides behind a tooltip: what
// each team's penalty and bonus minutes were charged and granted for.
func printStatNotes(w io.Writer, st *dzzzr.ArchiveStat) {
	printed := false
	for _, t := range st.Teams {
		for _, column := range st.Columns {
			note := t.Notes[column.Title]
			if strings.TrimSpace(note) == "" {
				continue
			}
			if !printed {
				_, _ = fmt.Fprintln(w, "\nРасшифровки:")
				printed = true
			}
			_, _ = fmt.Fprintf(w, "  %s, %s:\n%s\n", dash(t.Name), strings.ToLower(column.Title), indent(foldRepeats(note), "    "))
		}
	}
}

func cmdGameDesc(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	id, err := gameIDArg(args, "game-desc ИГРА")
	if err != nil {
		return err
	}
	d, err := c.GetArchiveDescription(ctx, id)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, d)
	}
	w := cfg.stdout
	printArchiveTitle(w, d.Name, d.Date, d.GameID)
	for _, s := range d.Sections {
		_, _ = fmt.Fprintf(w, "\n%s\n%s\n", s.Title, indent(s.Text, "  "))
	}
	printMediaRefs(w, "Афиша", d.Media)
	switch {
	case d.Restricted:
		// The engine explains the rule itself, so its wording is repeated
		// rather than paraphrased.
		_, _ = fmt.Fprintf(w, "\nТексты заданий недоступны.\n%s\n", indent(d.Notice, "  "))
		if len(d.Levels) == 0 {
			return nil
		}
	case len(d.Levels) == 0:
		_, _ = fmt.Fprintln(w, "\nЗадания не опубликованы.")
		return nil
	}
	for i, l := range d.Levels {
		_, _ = fmt.Fprintf(w, "\n\nЗадание %d: %s\n", i+1, dash(l.Title))
		if len(l.Codes) > 0 {
			_, _ = fmt.Fprintf(w, "  Коды: %s\n", strings.Join(l.Codes, ", "))
		}
		if l.Difficulty != "" {
			_, _ = fmt.Fprintf(w, "  Код сложности: %s\n", l.Difficulty)
		}
		if l.Task != "" {
			_, _ = fmt.Fprintf(w, "\n%s\n", indent(l.Task, "  "))
		}
		if l.Notes != "" {
			_, _ = fmt.Fprintf(w, "\n  Примечания к заданию:\n%s\n", indent(l.Notes, "    "))
		}
		for n, hint := range []string{l.Hint1, l.Hint2} {
			if hint == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "\n  Подсказка %d:\n%s\n", n+1, indent(hint, "    "))
		}
		for _, sp := range l.Spoilers {
			_, _ = fmt.Fprintf(w, "\n  Спойлер %d", sp.Number)
			if sp.Code != "" {
				_, _ = fmt.Fprintf(w, " (код %s)", sp.Code)
			}
			_, _ = fmt.Fprintf(w, ":\n%s\n", indent(sp.Text, "    "))
		}
		for _, s := range l.Sections {
			_, _ = fmt.Fprintf(w, "\n  %s:\n%s\n", s.Title, indent(s.Text, "    "))
		}
		printMediaRefs(w, "Вложения", l.Media)
	}
	return nil
}

// printMediaRefs lists the pictures and links of a block, mirroring what the
// level view does for a running game.
func printMediaRefs(w io.Writer, title string, refs []dzzzr.MediaRef) {
	if len(refs) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "\n  %s:\n", title)
	for _, r := range refs {
		switch {
		case r.Inline:
			_, _ = fmt.Fprintf(w, "    картинка: встроена (%s, %s)\n", r.MediaType, humanBytes(r.Bytes))
		case r.Kind == dzzzr.MediaImage:
			_, _ = fmt.Fprintf(w, "    картинка: %s\n", r.URL)
		case r.Text != "":
			_, _ = fmt.Fprintf(w, "    ссылка «%s»: %s\n", r.Text, r.URL)
		default:
			_, _ = fmt.Fprintf(w, "    ссылка: %s\n", r.URL)
		}
	}
}

func cmdGameLog(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	id, err := gameIDArg(args, "game-log ИГРА")
	if err != nil {
		return err
	}
	log, err := c.GetGameLog(ctx, id)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, log)
	}
	if len(log.Entries) == 0 {
		_, _ = fmt.Fprintln(cfg.stdout, "Лог пуст.")
		return nil
	}
	// The export names its own columns, so they are printed as the engine
	// wrote them rather than translated into names it never used.
	tw := newTable(cfg.stdout)
	_, _ = fmt.Fprintln(tw, strings.Join(log.Columns, "\t"))
	for _, e := range log.Entries {
		row := make([]string, 0, len(log.Columns))
		for _, col := range log.Columns {
			row = append(row, oneLine(dash(e[col])))
		}
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}
