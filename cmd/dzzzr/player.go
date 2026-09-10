package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func init() {
	register(
		command{Name: "login", Usage: "login", Auth: authNone, Run: cmdLogin,
			Help: "Войти на сайт и сохранить сессию вместе с логином капитана и PIN"},
		command{Name: "logout", Usage: "logout", Auth: authNone, Run: cmdLogout,
			Help: "Удалить сохранённую сессию города"},
		command{Name: "version", Usage: "version", Auth: authNone, Run: cmdVersion,
			Help: "Показать версию клиента"},
		command{Name: "games", Usage: "games [-new] [-archive]", Auth: authPlayer, Run: cmdGames,
			Help: "Список игр города"},
		command{Name: "status", Usage: "status", Auth: authPlayer, Run: cmdStatus,
			Help: "Состояние игры: уровень, коды, таймер, сообщения"},
		command{Name: "level", Usage: "level", Auth: authPlayer, Run: cmdLevel,
			Help: "Текст текущего задания без разметки"},
		command{Name: "hints", Usage: "hints", Auth: authPlayer, Run: cmdHints,
			Help: "Подсказки текущего уровня"},
		command{Name: "bonus-levels", Usage: "bonus-levels", Auth: authPlayer, Run: cmdBonusLevels,
			Help: "Сквозные бонусные задания"},
		command{Name: "stat", Usage: "stat", Auth: authPlayer, Run: cmdStat,
			Help: "Статистика команды по уровням"},
		command{Name: "log", Usage: "log", Auth: authPlayer, Run: cmdLog,
			Help: "Журнал действий команды"},
		command{Name: "messages", Usage: "messages [-after «ГГГГ-ММ-ДД ЧЧ:ММ:СС»]", Auth: authPlayer, Run: cmdMessages,
			Help: "Переписка с организатором"},
		command{Name: "send-message", Usage: "send-message ТЕКСТ...", Auth: authPlayer, Run: cmdSendMessage,
			Help: "Отправить сообщение организатору"},
		command{Name: "send-code", Usage: "send-code КОД", Auth: authPlayer, Run: cmdSendCode,
			Help: "Отправить код текущего уровня"},
		command{Name: "send-bonus", Usage: "send-bonus УРОВЕНЬ КОД", Auth: authPlayer, Run: cmdSendBonus,
			Help: "Отправить код сквозного бонусного задания"},
		command{Name: "spoiler", Usage: "spoiler УРОВЕНЬ КОД", Auth: authPlayer, Run: cmdSpoiler,
			Help: "Отправить код спойлера"},
		command{Name: "hint", Usage: "hint УРОВЕНЬ НОМЕР", Auth: authPlayer, Run: cmdHint,
			Help: "Запросить подсказку 1 или 2"},
		command{Name: "hint-early", Usage: "hint-early [НОМЕР] [УРОВЕНЬ]", Auth: authPlayer, Run: cmdHintEarly,
			Help: "Запросить следующую подсказку досрочно, со штрафом"},
		command{Name: "abandon", Usage: "abandon", Auth: authPlayer, Run: cmdAbandon,
			Help: "Отказаться от задания и получить следующее"},
		command{Name: "break", Usage: "break", Auth: authPlayer, Run: cmdBreak,
			Help: "Взять пятнадцатиминутный перерыв"},
		command{Name: "break-stop", Usage: "break-stop", Auth: authPlayer, Run: cmdBreakStop,
			Help: "Досрочно завершить перерыв"},
		command{Name: "next-level", Usage: "next-level [УРОВЕНЬ]", Auth: authPlayer, Run: cmdNextLevel,
			Help: "Перейти на следующий уровень, оставив бонусные коды"},
		command{Name: "select-level", Usage: "select-level [СЛЕДУЮЩИЙ]", Auth: authPlayer, Run: cmdSelectLevel,
			Help: "Выбрать следующий уровень в играх со свободным порядком; без аргумента показать список"},
	)
}

// actionOutput is the -json form of an engine action result.
//
// Outcome exists because Accepted cannot tell the whole truth: the engine
// answers a control action it declined exactly as it answers one it
// performed. Accepted means the engine confirmed it; Outcome says when there
// was nothing to confirm.
type actionOutput struct {
	Err      int    `json:"err"`
	Text     string `json:"text"`
	Accepted bool   `json:"accepted"`
	Outcome  string `json:"outcome"` // accepted, rejected or unknown
	Penalty  int    `json:"penalty,omitzero"`
}

// printAction reports the outcome of an action and returns errRejected when
// the engine refused it, which makes the process exit with code 2.
//
// Code 0 means the engine had nothing to report. For a submitted code that is
// suspicious, so it counts as a refusal; for a control action (break, abandon,
// hint request) it is the normal answer on success, which is what quietOK
// says.
func printAction(cfg *config, res *dzzzr.ActionResult, quietOK bool) error {
	// The engine answers a control action it declined exactly as it answers
	// one it performed: the full state with errNo 0 and no wording. That is
	// not a success, so it is not reported as one — but it is not a refusal
	// either, so it does not fail the command.
	confirmed := res.Accepted()
	silent := quietOK && res.Err == 0 && res.Text == ""
	text := res.Text
	outcome := "rejected"
	switch {
	case confirmed:
		outcome = "accepted"
	case silent:
		outcome = "unknown"
		text = "движок не сообщил результат, проверьте состояние командой «dzzzr status»"
	case text == "":
		text = "движок не сообщил результат"
	}
	if cfg.jsonOut {
		out := actionOutput{Err: res.Err, Text: text, Accepted: confirmed, Outcome: outcome, Penalty: res.Penalty}
		if err := outputJSON(cfg, out); err != nil {
			return err
		}
	} else {
		prefix := map[string]string{"accepted": "Принято:", "rejected": "Отклонено:", "unknown": "Без ответа:"}[outcome]
		fmt.Fprintf(cfg.stdout, "%s [%d] %s\n", prefix, res.Err, text)
	}
	if outcome == "rejected" {
		return errRejected
	}
	return nil
}

// needArgs checks the argument count of a command.
func needArgs(args []string, n int, usage string) error {
	if len(args) < n {
		return fatal("недостаточно аргументов, использование: dzzzr %s", usage)
	}
	return nil
}

// parseLevel reads a level number.
func parseLevel(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0, fatal("неверный номер уровня %q", s)
	}
	return n, nil
}

// currentLevelNumber asks the engine which level the team is on.
func currentLevelNumber(ctx context.Context, c *dzzzr.Client) (int, error) {
	st, err := c.GetGame(ctx)
	if err != nil {
		return 0, err
	}
	if st.Level == nil || st.Level.LevelNumber.Int() <= 0 {
		return 0, fatal("текущий уровень неизвестен, укажите номер уровня явно")
	}
	return st.Level.LevelNumber.Int(), nil
}

// levelArg takes the level from the first argument, or from the engine when
// the argument is missing.
func levelArg(ctx context.Context, c *dzzzr.Client, args []string) (int, error) {
	if len(args) > 0 {
		return parseLevel(args[0])
	}
	return currentLevelNumber(ctx, c)
}

// promptLine reads one line from stdin after writing a prompt to stderr, so
// the prompts never pollute a piped stdout.
func promptLine(cfg *config, prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(cfg.stderr, prompt)
	}
	line, err := cfg.lineReader().ReadString('\n')
	if err != nil && line == "" {
		return "", fatal("не удалось прочитать ввод: %v", err)
	}
	return strings.TrimSpace(line), nil
}

// promptPassword reads a secret without echoing it when stdin is a terminal.
func promptPassword(cfg *config, prompt string) (string, error) {
	fmt.Fprint(cfg.stderr, prompt)
	if f, ok := cfg.stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(cfg.stderr)
		if err != nil {
			return "", fatal("не удалось прочитать пароль: %v", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return promptLine(cfg, "")
}

// loginOutput is the -json form of a successful sign-in.
type loginOutput struct {
	Code        int    `json:"code"`
	UserName    string `json:"userName"`
	SessionFile string `json:"sessionFile"`
}

func cmdLogin(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if len(args) > 0 {
		return fatal("команда login не принимает аргументов")
	}
	// Values already saved for this city are reused so that a repeated
	// login only has to ask for what is still missing.
	if _, _, err := loadSession(cfg, c); err != nil {
		return err
	}
	applyCredentialOverrides(cfg, c)

	login := cfg.login
	if login == "" {
		login = c.LoginName()
	}
	var err error
	if login == "" {
		if login, err = promptLine(cfg, "Логин сайта: "); err != nil {
			return err
		}
	}
	password := cfg.password
	if password == "" {
		if password, err = promptPassword(cfg, "Пароль: "); err != nil {
			return err
		}
	}
	if login == "" || password == "" {
		return fatal("логин и пароль обязательны")
	}
	// The captain login and PIN are optional: Classic teams have none, and the
	// engine's API wall only checks that a Basic header exists, so the site
	// login serves as the game identity. They are asked for only when the
	// organizer issued them, that is when -captain or -pin was given.
	if creds := c.Credentials(); creds.Captain != "" || creds.Pin != "" {
		c.SetCredentials(creds)
	}

	resp, err := c.Login(ctx, login, password)
	if err != nil {
		return err
	}
	if !resp.OK() {
		return fatal("вход не выполнен: %s", dzzzr.LoginCodeText(resp.Code.Int()))
	}
	path, err := saveSession(cfg, c)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, loginOutput{Code: resp.Code.Int(), UserName: resp.UserName, SessionFile: path})
	}
	fmt.Fprintf(cfg.stdout, "Вход выполнен: %s\n", dash(resp.UserName))
	fmt.Fprintf(cfg.stdout, "Сессия сохранена в %s\n", path)
	return nil
}

// logoutOutput is the -json form of logout.
type logoutOutput struct {
	Removed     bool   `json:"removed"`
	SessionFile string `json:"sessionFile"`
}

func cmdLogout(_ context.Context, cfg *config, _ *dzzzr.Client, args []string) error {
	if len(args) > 0 {
		return fatal("команда logout не принимает аргументов")
	}
	path, removed, err := removeSession(cfg)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, logoutOutput{Removed: removed, SessionFile: path})
	}
	if removed {
		fmt.Fprintf(cfg.stdout, "Сессия удалена: %s\n", path)
	} else {
		fmt.Fprintf(cfg.stdout, "Сохранённой сессии не было: %s\n", path)
	}
	return nil
}

// versionOutput is the -json form of version.
type versionOutput struct {
	Version string `json:"version"`
}

func cmdVersion(_ context.Context, cfg *config, _ *dzzzr.Client, _ []string) error {
	if cfg.jsonOut {
		return outputJSON(cfg, versionOutput{Version: version})
	}
	fmt.Fprintf(cfg.stdout, "dzzzr %s\n", version)
	return nil
}

func cmdGames(ctx context.Context, cfg *config, c *dzzzr.Client, _ []string) error {
	games, err := c.GetGamesList(ctx, dzzzr.GamesListOptions{NewOnly: cfg.newOnly, Archive: cfg.archive, After: cfg.after})
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, games)
	}
	if len(games) == 0 {
		fmt.Fprintln(cfg.stdout, "Игр не найдено.")
		return nil
	}
	tw := newTable(cfg.stdout)
	fmt.Fprintln(tw, "ID\tДата\tНомер\tНазвание\tАвторы\tКоманд")
	for _, g := range games {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%d\n",
			g.ID.Int(), dash(g.Date), dash(g.Number.String()), dash(g.Name), dash(g.Authors), len(g.Teams))
	}
	return tw.Flush()
}

func cmdStatus(ctx context.Context, cfg *config, c *dzzzr.Client, _ []string) error {
	st, err := c.GetGame(ctx)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, st)
	}
	w := cfg.stdout
	fmt.Fprintf(w, "Игра:    %s (#%d)\n", dash(st.GameName), st.GameID.Int())
	fmt.Fprintf(w, "Команда: %s\n", dash(st.TeamName))
	if st.CurrentTime != "" {
		fmt.Fprintf(w, "Время:   %s\n", st.CurrentTime)
	}

	if !st.GameStarted() {
		fmt.Fprintf(w, "\nИгра ещё не началась. Старт: %s в %s", dash(st.GameStartOnDay), dash(st.GameStartOnTime))
		if st.GameStartTime != "" {
			fmt.Fprintf(w, " (на сервере %s)", st.GameStartTime)
		}
		fmt.Fprintln(w)
	}
	if st.Finished.Bool() {
		fmt.Fprintln(w, "\nИгра завершена.")
	}
	if st.HoldTime != "" {
		fmt.Fprintf(w, "Команда приостановлена организатором: %s\n", st.HoldTime)
	}
	if st.OnBreak != "" {
		fmt.Fprintf(w, "Перерыв до %s", st.OnBreak)
		if st.Countdown.Int() > 0 {
			fmt.Fprintf(w, " (осталось %s)", formatDuration(st.Countdown.Int()))
		}
		fmt.Fprintln(w)
	}
	for _, note := range []string{st.BlockedError, st.TryLimitError} {
		if s := dzzzr.StripHTML(note); s != "" {
			fmt.Fprintln(w, s)
		}
	}

	if st.Level != nil {
		fmt.Fprintln(w)
		printLevelSummary(w, st.Level, st.TotalLevels.Int())
	} else if st.GameStarted() {
		fmt.Fprintln(w, "\nЗадание не выдано.")
	}

	if len(st.BonusLevels) > 0 {
		fmt.Fprintln(w, "\nСквозные бонусные задания:")
		printBonusLevels(w, st.BonusLevels)
	}
	if len(st.Messages) > 0 {
		fmt.Fprintln(w, "\nСообщения:")
		for _, m := range st.Messages {
			fmt.Fprintf(w, "  %s [%s] %s\n", m.Timestamp, dash(m.Destination), dzzzr.StripHTML(m.Text))
		}
	}
	if st.ErrNo.Int() != 0 {
		fmt.Fprintf(w, "\nПоследнее действие: [%d] %s\n", st.ErrNo.Int(), dzzzr.StripHTML(st.ErrText))
	}
	return nil
}

// printLevelSummary writes the counters of the level the team is playing.
func printLevelSummary(w io.Writer, l *dzzzr.Level, totalLevels int) {
	if totalLevels > 0 {
		fmt.Fprintf(w, "Уровень %d из %d\n", l.LevelNumber.Int(), totalLevels)
	} else {
		fmt.Fprintf(w, "Уровень %d\n", l.LevelNumber.Int())
	}
	needed := l.NeededCodes.Int()
	if needed <= 0 || needed > l.TotalCodes.Int() {
		needed = l.TotalCodes.Int()
	}
	fmt.Fprintf(w, "  Коды:           %d из %d (нужно %d)\n", l.CodesFounded.Int(), l.TotalCodes.Int(), needed)
	if l.BonusCodesTotal.Int() > 0 {
		fmt.Fprintf(w, "  Бонусные коды:  %d из %d\n", l.BonusCodesFounded.Int(), l.BonusCodesTotal.Int())
	}
	if kind := levelKind(l); kind != "" {
		fmt.Fprintf(w, "  Задание:        %s", kind)
		if m := l.BonusLevelTime.Int(); m > 0 {
			fmt.Fprintf(w, " (%d мин.)", m)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "  Подсказки:      %s\n", hintState(l))
	if l.TM.Int() > 0 {
		fmt.Fprintf(w, "  До события:     %s\n", formatDuration(l.TM.Int()))
	}
	if l.TimeOnLevel != "" {
		fmt.Fprintf(w, "  На уровне:      %s\n", l.TimeOnLevel)
	}
	if l.TryLimit.Int() > 0 {
		fmt.Fprintf(w, "  Попытки:        %d из %d\n", l.TryLimitUsed.Int(), l.TryLimit.Int())
	}
	if l.IsSabotage.Bool() {
		fmt.Fprintln(w, "  Задание-диверсия.")
	}
	if l.NoMasterCode.Bool() {
		fmt.Fprintln(w, "  Универсальный код запрещён.")
	}
	if n := len(l.Spoilers); n > 0 {
		fmt.Fprintf(w, "  Спойлеры:       %d\n", n)
	}
	if l.IsLevelFinished.Bool() {
		fmt.Fprintln(w, "  Уровень выполнен: можно добирать бонусные коды или перейти дальше («dzzzr next-level»).")
	}
	if l.TakeBreak.Bool() {
		fmt.Fprintln(w, "  После этого уровня запланирован перерыв 15 мин. («dzzzr break» отменяет).")
	}
}

// printMedia lists the pictures and links of a level. Tasks often hide the
// code in a picture, and StripHTML leaves nothing of it behind.
func printMedia(w io.Writer, c *dzzzr.Client, l *dzzzr.Level) {
	refs := c.LevelMedia(l)
	if len(refs) == 0 {
		return
	}
	fmt.Fprintln(w, "\nВложения:")
	for _, r := range refs {
		switch {
		case r.Inline:
			// The bytes are the picture. A terminal cannot show it, and
			// printing the URI would dump a blob into the scrollback, so
			// say what it is; a mobile client renders the same ref.
			fmt.Fprintf(w, "  картинка: встроена в задание (%s, %s)\n", r.MediaType, humanBytes(r.Bytes))
		case r.Kind == dzzzr.MediaImage:
			fmt.Fprintf(w, "  картинка: %s\n", r.URL)
		case r.Text != "":
			fmt.Fprintf(w, "  ссылка «%s»: %s\n", r.Text, r.URL)
		default:
			fmt.Fprintf(w, "  ссылка: %s\n", r.URL)
		}
	}
}

// humanBytes renders a size the way a person reads it.
func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f МБ", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f КБ", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d Б", n)
}

// levelKind names a level that is not an ordinary one. Сквозное and бонусное
// are separate flags in the engine: a bonus task need not run alongside the
// main line.
func levelKind(l *dzzzr.Level) string {
	switch {
	case l.Skvoz.Bool() && l.IsBonusLevel.Bool():
		return "сквозное бонусное"
	case l.Skvoz.Bool():
		return "сквозное"
	case l.IsBonusLevel.Bool():
		return "бонусное"
	}
	return ""
}

// hintState describes which hints the engine has already issued.
//
// They are independent: go_level.tpl guards hint1 and hint2 separately, and
// an early request names the one it wants, so the second can be out while the
// first is not.
func hintState(l *dzzzr.Level) string {
	switch {
	case l.Hint1 != "" && l.Hint2 != "":
		return "выданы обе"
	case l.Hint1 != "":
		return "выдана первая"
	case l.Hint2 != "":
		return "выдана вторая"
	default:
		return "ещё не выданы"
	}
}

// printBonusLevels renders the skvoz levels running alongside the main line.
func printBonusLevels(w io.Writer, levels []dzzzr.Level) {
	tw := newTable(w)
	fmt.Fprintln(tw, "  Уровень\tКоды\tБонус, мин\tОсталось\tЗадание")
	for i, l := range levels {
		number := l.LevelNumber.Int()
		label := strconv.Itoa(number)
		if number == 0 {
			label = "#" + strconv.Itoa(i+1)
		}
		left := "—"
		if l.TM.Int() > 0 {
			left = formatDuration(l.TM.Int())
		}
		question := dzzzr.StripHTML(l.Question)
		if idx := strings.IndexByte(question, '\n'); idx >= 0 {
			question = question[:idx]
		}
		fmt.Fprintf(tw, "  %s\t%d/%d\t%d\t%s\t%s\n",
			label, l.CodesFounded.Int(), l.TotalCodes.Int(), l.BonusLevelTime.Int(), left, dash(question))
	}
	_ = tw.Flush()
}

func cmdLevel(ctx context.Context, cfg *config, c *dzzzr.Client, _ []string) error {
	st, err := c.GetGame(ctx)
	if err != nil {
		return err
	}
	if st.Level == nil {
		if cfg.jsonOut {
			return outputJSON(cfg, struct {
				Level *dzzzr.Level `json:"level"`
			}{nil})
		}
		return fatal("задание не выдано")
	}
	if cfg.jsonOut {
		return outputJSON(cfg, st.Level)
	}
	l := st.Level
	w := cfg.stdout
	fmt.Fprintf(w, "Уровень %d\n\n", l.LevelNumber.Int())
	if q := dzzzr.StripHTML(l.Question); q != "" {
		fmt.Fprintln(w, q)
	} else {
		fmt.Fprintln(w, "(текст задания пуст)")
	}
	printMedia(w, c, l)
	if s := dzzzr.StripHTML(l.LocationComment); s != "" {
		fmt.Fprintf(w, "\nКомментарий к локации: %s\n", s)
	}
	if s := dzzzr.StripHTML(l.KOLine); s != "" {
		fmt.Fprintf(w, "\nКоэффициенты сложности:\n%s\n", indent(s, "  "))
	}
	if len(l.Spoilers) > 0 {
		fmt.Fprintln(w, "\nСпойлеры:")
		for _, sp := range l.Spoilers {
			status := "закрыт"
			if sp.Solved.Bool() {
				status = "открыт"
			}
			fmt.Fprintf(w, "  #%d (%s, штраф %d)", sp.Number.Int(), status, sp.Penalty.Int())
			if text := dzzzr.StripHTML(sp.Text); text != "" {
				fmt.Fprintf(w, ": %s", text)
			}
			fmt.Fprintln(w)
		}
	}
	return nil
}

// hintsOutput is the -json form of the hints command.
type hintsOutput struct {
	Level int    `json:"level"`
	Hint1 string `json:"hint1"`
	Hint2 string `json:"hint2"`
}

func cmdHints(ctx context.Context, cfg *config, c *dzzzr.Client, _ []string) error {
	st, err := c.GetGame(ctx)
	if err != nil {
		return err
	}
	if st.Level == nil {
		return fatal("задание не выдано")
	}
	l := st.Level
	if cfg.jsonOut {
		return outputJSON(cfg, hintsOutput{Level: l.LevelNumber.Int(), Hint1: l.Hint1, Hint2: l.Hint2})
	}
	w := cfg.stdout
	fmt.Fprintf(w, "Уровень %d\n", l.LevelNumber.Int())
	for i, h := range []string{l.Hint1, l.Hint2} {
		text := dzzzr.StripHTML(h)
		if text == "" {
			fmt.Fprintf(w, "\nПодсказка %d: ещё не выдана\n", i+1)
			continue
		}
		fmt.Fprintf(w, "\nПодсказка %d:\n%s\n", i+1, indent(text, "  "))
	}
	if l.TM.Int() > 0 {
		fmt.Fprintf(w, "\nДо следующего события: %s\n", formatDuration(l.TM.Int()))
	}
	return nil
}

func cmdBonusLevels(ctx context.Context, cfg *config, c *dzzzr.Client, _ []string) error {
	st, err := c.GetGame(ctx)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, st.BonusLevels)
	}
	if len(st.BonusLevels) == 0 {
		fmt.Fprintln(cfg.stdout, "Сквозных бонусных заданий нет.")
		return nil
	}
	printBonusLevels(cfg.stdout, st.BonusLevels)
	for i, l := range st.BonusLevels {
		if q := dzzzr.StripHTML(l.Question); q != "" {
			fmt.Fprintf(cfg.stdout, "\n#%d:\n%s\n", i+1, indent(q, "  "))
		}
	}
	return nil
}

func cmdStat(ctx context.Context, cfg *config, c *dzzzr.Client, _ []string) error {
	stat, err := c.GetStat(ctx)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, stat)
	}
	rows := stat.Rows()
	if len(rows) == 0 {
		fmt.Fprintln(cfg.stdout, "Статистика пуста.")
		return nil
	}
	if stat.Time != "" {
		fmt.Fprintf(cfg.stdout, "Статистика на %s\n\n", stat.Time)
	}
	tw := newTable(cfg.stdout)
	fmt.Fprintln(tw, "Уровень\tРезультат")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", dash(r[0]), dash(r[1]))
	}
	return tw.Flush()
}

func cmdLog(ctx context.Context, cfg *config, c *dzzzr.Client, _ []string) error {
	entries, err := c.GetLog(ctx)
	if err != nil {
		return err
	}
	clean := make([]dzzzr.LogEntry, 0, len(entries))
	for _, e := range entries {
		clean = append(clean, e.Clean())
	}
	if cfg.jsonOut {
		return outputJSON(cfg, clean)
	}
	if len(clean) == 0 {
		fmt.Fprintln(cfg.stdout, "Журнал пуст.")
		return nil
	}
	tw := newTable(cfg.stdout)
	fmt.Fprintln(tw, "Время\tСобытие\tДанные\tПользователь")
	for _, e := range clean {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", dash(e.Time), dash(e.Event), dash(e.Data), dash(e.User))
	}
	return tw.Flush()
}

func cmdMessages(ctx context.Context, cfg *config, c *dzzzr.Client, _ []string) error {
	msgs, err := c.GetMessages(ctx, cfg.after)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, msgs)
	}
	if len(msgs) == 0 {
		fmt.Fprintln(cfg.stdout, "Сообщений нет.")
		return nil
	}
	for _, m := range msgs {
		fmt.Fprintf(cfg.stdout, "%s %s:\n%s\n\n", m.Time, dash(m.Who), indent(dzzzr.StripHTML(m.Content), "  "))
	}
	return nil
}

// okOutput is the -json form of a command that only reports success.
type okOutput struct {
	OK bool `json:"ok"`
}

func cmdSendMessage(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 1, "send-message ТЕКСТ..."); err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		return fatal("текст сообщения пуст")
	}
	res, err := c.SendMessageToOrg(ctx, text)
	if err != nil {
		return err
	}
	return printAction(cfg, res, false)
}

func cmdSendCode(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 1, "send-code КОД"); err != nil {
		return err
	}
	res, err := c.SendCode(ctx, args[0])
	if err != nil {
		return err
	}
	return printAction(cfg, res, false)
}

func cmdSendBonus(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 2, "send-bonus УРОВЕНЬ КОД"); err != nil {
		return err
	}
	level, err := parseLevel(args[0])
	if err != nil {
		return err
	}
	res, err := c.SendBonusCode(ctx, level, args[1])
	if err != nil {
		return err
	}
	return printAction(cfg, res, false)
}

func cmdSpoiler(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 2, "spoiler УРОВЕНЬ КОД"); err != nil {
		return err
	}
	level, err := parseLevel(args[0])
	if err != nil {
		return err
	}
	res, err := c.SendSpoilerCode(ctx, level, args[1])
	if err != nil {
		return err
	}
	return printAction(cfg, res, false)
}

func cmdHint(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 2, "hint УРОВЕНЬ НОМЕР"); err != nil {
		return err
	}
	level, err := parseLevel(args[0])
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(strings.TrimSpace(args[1]))
	if err != nil || (n != 1 && n != 2) {
		return fatal("номер подсказки должен быть 1 или 2, получено %q", args[1])
	}
	res, err := c.TakeHint(ctx, level, n)
	if err != nil {
		return err
	}
	return printAction(cfg, res, true)
}

// cmdHintEarly asks for a named hint of a named level, because the engine
// picks one of its own when the request carries neither.
func cmdHintEarly(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if len(args) > 2 {
		return fatal("использование: dzzzr hint-early [НОМЕР] [УРОВЕНЬ]")
	}
	hint := 0
	if len(args) > 0 {
		n, err := strconv.Atoi(strings.TrimSpace(args[0]))
		if err != nil || (n != 1 && n != 2) {
			return fatal("номер подсказки должен быть 1 или 2, получено %q", args[0])
		}
		hint = n
	}
	level := 0
	if len(args) > 1 {
		n, err := parseLevel(args[1])
		if err != nil {
			return err
		}
		level = n
	}
	if hint == 0 || level == 0 {
		st, err := c.GetGame(ctx)
		if err != nil {
			return err
		}
		if st.Level == nil || st.Level.LevelNumber.Int() <= 0 {
			return fatal("задание не выдано, укажите номер подсказки и уровень явно")
		}
		if level == 0 {
			level = st.Level.LevelNumber.Int()
		}
		if hint == 0 {
			hint = nextHint(st.Level)
			if hint == 0 {
				return fatal("обе подсказки этого уровня уже выданы")
			}
		}
	}
	res, err := c.TakeHintEarly(ctx, level, hint)
	if err != nil {
		return err
	}
	return printAction(cfg, res, true)
}

// nextHint is the hint the engine has not issued yet, or 0 when both are out.
func nextHint(l *dzzzr.Level) int {
	switch {
	case l.Hint1 == "":
		return 1
	case l.Hint2 == "":
		return 2
	}
	return 0
}

// The engine's own forms post these three with no level at all, and a team on
// a break has no level to name, so taking one here would make break-stop
// impossible exactly when it is needed.
func cmdAbandon(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if len(args) > 0 {
		return fatal("команда abandon не принимает аргументов")
	}
	res, err := c.Abandon(ctx)
	if err != nil {
		return err
	}
	return printAction(cfg, res, true)
}

func cmdBreak(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if len(args) > 0 {
		return fatal("команда break не принимает аргументов")
	}
	res, err := c.TakeBreak(ctx)
	if err != nil {
		return err
	}
	return printAction(cfg, res, true)
}

func cmdBreakStop(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if len(args) > 0 {
		return fatal("команда break-stop не принимает аргументов")
	}
	res, err := c.StopBreak(ctx)
	if err != nil {
		return err
	}
	return printAction(cfg, res, true)
}

func cmdNextLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	level, err := levelArg(ctx, c, args)
	if err != nil {
		return err
	}
	res, err := c.NextLevel(ctx, level)
	if err != nil {
		return err
	}
	return printAction(cfg, res, true)
}

// cmdSelectLevel picks the next level, or lists the choices when called
// without one: the engine sends them only as a rendered form.
func cmdSelectLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	st, err := c.GetGame(ctx)
	if err != nil {
		return err
	}
	choices := st.NextLevelChoices()
	if len(args) == 0 {
		if len(choices) == 0 {
			return fatal("движок не предлагает выбор уровня")
		}
		if cfg.jsonOut {
			return outputJSON(cfg, choices)
		}
		fmt.Fprintln(cfg.stdout, "Доступные уровни:")
		for _, ch := range choices {
			fmt.Fprintf(cfg.stdout, "  %d  %s\n", ch.Number, ch.Title)
		}
		return nil
	}
	next, err := parseLevel(args[0])
	if err != nil {
		return err
	}
	if st.Level == nil || st.Level.LevelNumber.Int() <= 0 {
		return fatal("текущий уровень неизвестен")
	}
	res, err := c.SelectLevel(ctx, st.Level.LevelNumber.Int(), next)
	if err != nil {
		return err
	}
	return printAction(cfg, res, true)
}
