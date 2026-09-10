package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func init() {
	register(
		command{Name: "admin-games", Usage: "admin-games", Auth: authAdmin, Run: cmdAdminGames,
			Help: "Список игр организатора"},
		command{Name: "admin-game-info", Usage: "admin-game-info ИГРА", Auth: authAdmin, Run: cmdAdminGameInfo,
			Help: "Настройки игры в виде ключ=значение"},
		command{Name: "admin-create-game", Usage: "admin-create-game ключ=значение...", Auth: authAdmin, Run: cmdAdminCreateGame,
			Help: "Создать игру; обязательны name=, date= и time="},
		command{Name: "admin-update-game", Usage: "admin-update-game ИГРА ключ=значение...", Auth: authAdmin, Run: cmdAdminUpdateGame,
			Help: "Изменить настройки игры, остальные поля сохранятся"},
		command{Name: "admin-delete-game", Usage: "admin-delete-game ИГРА", Auth: authAdmin, Run: cmdAdminDeleteGame,
			Help: "Удалить игру; движок откажет, пока у неё есть задания"},
		command{Name: "admin-copy-game", Usage: "admin-copy-game ИГРА [-with-levels]", Auth: authAdmin, Run: cmdAdminCopyGame,
			Help: "Скопировать игру в новую, при -with-levels вместе с заданиями"},

		command{Name: "admin-levels", Usage: "admin-levels ИГРА", Auth: authAdmin, Run: cmdAdminLevels,
			Help: "Список заданий игры по порядку"},
		command{Name: "admin-level", Usage: "admin-level ИГРА ЗАДАНИЕ", Auth: authAdmin, Run: cmdAdminLevel,
			Help: "Полная карточка задания в виде ключ=значение"},
		command{Name: "admin-create-level", Usage: "admin-create-level ИГРА ключ=значение...", Auth: authAdmin, Run: cmdAdminCreateLevel,
			Help: "Добавить задание в игру; нужны title= или question= и codes="},
		command{Name: "admin-update-level", Usage: "admin-update-level ИГРА ЗАДАНИЕ ключ=значение...", Auth: authAdmin, Run: cmdAdminUpdateLevel,
			Help: "Изменить задание, остальные поля сохранятся"},
		command{Name: "admin-delete-level", Usage: "admin-delete-level ИГРА ЗАДАНИЕ", Auth: authAdmin, Run: cmdAdminDeleteLevel,
			Help: "Удалить задание игры"},
		command{Name: "admin-move-level", Usage: "admin-move-level ИГРА ЗАДАНИЕ up|down", Auth: authAdmin, Run: cmdAdminMoveLevel,
			Help: "Передвинуть задание на одну позицию вверх или вниз"},
		command{Name: "admin-copy-levels", Usage: "admin-copy-levels ИГРА ИГРА-ИСТОЧНИК", Auth: authAdmin, Run: cmdAdminCopyLevels,
			Help: "Скопировать все задания другой игры в эту"},

		command{Name: "admin-city-teams", Usage: "admin-city-teams ИГРА [ПОДСТРОКА]", Auth: authAdmin, Run: cmdAdminCityTeams,
			Help: "Все команды города из списка заявки на игру; подстрока фильтрует по названию"},
		command{Name: "admin-teams", Usage: "admin-teams ИГРА", Auth: authAdmin, Run: cmdAdminTeams,
			Help: "Заявки команд на игру"},
		command{Name: "admin-team", Usage: "admin-team ИГРА КОМАНДА", Auth: authAdmin, Run: cmdAdminTeam,
			Help: "Карточка команды в контексте игры"},
		command{Name: "admin-set-application", Usage: "admin-set-application ИГРА КОМАНДА ключ=значение...", Auth: authAdmin, Run: cmdAdminSetApplication,
			Help: "Изменить заявку: status=, new-pin=, points=, novice=, comment="},
		command{Name: "admin-accept-all", Usage: "admin-accept-all ИГРА", Auth: authAdmin, Run: cmdAdminAcceptAll,
			Help: "Принять все нерассмотренные заявки и выдать PIN"},
		command{Name: "admin-generate-pins", Usage: "admin-generate-pins ИГРА", Auth: authAdmin, Run: cmdAdminGeneratePins,
			Help: "Выдать новые PIN всем принятым командам"},
		command{Name: "admin-add-team", Usage: "admin-add-team ИГРА КОМАНДА", Auth: authAdmin, Run: cmdAdminAddTeam,
			Help: "Подать заявку команды на игру от имени организатора"},
		command{Name: "admin-remove-application", Usage: "admin-remove-application ИГРА КОМАНДА СТАТУС", Auth: authAdmin, Run: cmdAdminRemoveApplication,
			Help: "Удалить заявку команды вместе с её игровым логом"},
		command{Name: "admin-create-team", Usage: "admin-create-team НАЗВАНИЕ КАПИТАН", Auth: authAdmin, Run: cmdAdminCreateTeam,
			Help: "Зарегистрировать команду с указанным логином капитана"},

		command{Name: "admin-monitor", Usage: "admin-monitor ИГРА", Auth: authAdmin, Run: cmdAdminMonitor,
			Help: "Экран мониторинга: команды, уровни, состояние движка"},
		command{Name: "admin-log", Usage: "admin-log ИГРА [-since «ГГГГ-ММ-ДД ЧЧ:ММ:СС»]", Auth: authAdmin, Run: cmdAdminLog,
			Help: "События игры из журнала организатора"},
		command{Name: "admin-give-level", Usage: "admin-give-level ИГРА КОМАНДА УРОВЕНЬ [-at «ГГГГ-ММ-ДД ЧЧ:ММ:СС»]", Auth: authAdmin, Run: cmdAdminGiveLevel,
			Help: "Выдать команде уровень"},
		command{Name: "admin-accept-level", Usage: "admin-accept-level ИГРА КОМАНДА УРОВЕНЬ [-at «ГГГГ-ММ-ДД ЧЧ:ММ:СС»]", Auth: authAdmin, Run: cmdAdminAcceptLevel,
			Help: "Засчитать команде уровень целиком"},
		command{Name: "admin-accept-code", Usage: "admin-accept-code ИГРА КОМАНДА УРОВЕНЬ КОД [-at «ГГГГ-ММ-ДД ЧЧ:ММ:СС»]", Auth: authAdmin, Run: cmdAdminAcceptCode,
			Help: "Засчитать команде один код уровня"},
		command{Name: "admin-clear-codes", Usage: "admin-clear-codes ИГРА КОМАНДА УРОВЕНЬ", Auth: authAdmin, Run: cmdAdminClearCodes,
			Help: "Снять все коды, взятые командой на уровне"},
		command{Name: "admin-remove-level", Usage: "admin-remove-level ИГРА КОМАНДА УРОВЕНЬ", Auth: authAdmin, Run: cmdAdminRemoveLevel,
			Help: "Убрать уровень из игры команды, 0 вместо команды — у всех"},
		command{Name: "admin-clear-progress", Usage: "admin-clear-progress ИГРА КОМАНДА", Auth: authAdmin, Run: cmdAdminClearProgress,
			Help: "Стереть весь игровой лог команды"},
		command{Name: "admin-plan-level", Usage: "admin-plan-level ИГРА КОМАНДА УРОВЕНЬ", Auth: authAdmin, Run: cmdAdminPlanLevel,
			Help: "Поставить уровень в очередь команды"},
		command{Name: "admin-unplan-level", Usage: "admin-unplan-level ИГРА КОМАНДА ПОЗИЦИЯ", Auth: authAdmin, Run: cmdAdminUnplanLevel,
			Help: "Убрать уровень из очереди команды по позиции"},
		command{Name: "admin-add-correction", Usage: "admin-add-correction ИГРА КОМАНДА bonus|penalty МИНУТЫ [КОММЕНТАРИЙ...]", Auth: authAdmin, Run: cmdAdminAddCorrection,
			Help: "Начислить команде бонусные или штрафные минуты"},
		command{Name: "admin-delete-corrections", Usage: "admin-delete-corrections ИГРА КОМАНДА bonus|penalty", Auth: authAdmin, Run: cmdAdminDeleteCorrections,
			Help: "Снять все бонусы или все штрафы команды"},
		command{Name: "admin-block-team", Usage: "admin-block-team ИГРА КОМАНДА", Auth: authAdmin, Run: cmdAdminBlockTeam,
			Help: "Приостановить игру команды"},
		command{Name: "admin-unblock-team", Usage: "admin-unblock-team ИГРА КОМАНДА", Auth: authAdmin, Run: cmdAdminUnblockTeam,
			Help: "Возобновить игру команды"},
		command{Name: "admin-engine", Usage: "admin-engine ИГРА on|off|toggle", Auth: authAdmin, Run: cmdAdminEngine,
			Help: "Включить, остановить или переключить движок проекта"},
		command{Name: "admin-finish-game", Usage: "admin-finish-game ИГРА", Auth: authAdmin, Run: cmdAdminFinishGame,
			Help: "Завершить игру и очистить очереди уровней"},
		command{Name: "admin-set-time", Usage: "admin-set-time ИГРА КОМАНДА УРОВЕНЬ СОБЫТИЕ «ГГГГ-ММ-ДД ЧЧ:ММ:СС»", Auth: authAdmin, Run: cmdAdminSetTime,
			Help: "Передвинуть время события уровня: level, code или номер"},

		command{Name: "admin-messages", Usage: "admin-messages ИГРА [-team N]", Auth: authAdmin, Run: cmdAdminMessages,
			Help: "Переписка организатора с командами"},
		command{Name: "admin-send-message", Usage: "admin-send-message ИГРА ТЕКСТ... [-team N] [-show-after «ГГГГ-ММ-ДД ЧЧ:ММ:СС»] [-important]", Auth: authAdmin, Run: cmdAdminSendMessage,
			Help: "Отправить сообщение команде или всем"},
		command{Name: "admin-delete-message", Usage: "admin-delete-message ИГРА «ГГГГ-ММ-ДД ЧЧ:ММ:СС»", Auth: authAdmin, Run: cmdAdminDeleteMessage,
			Help: "Удалить одно сообщение по его метке времени"},
		command{Name: "admin-delete-all-messages", Usage: "admin-delete-all-messages ИГРА", Auth: authAdmin, Run: cmdAdminDeleteAllMessages,
			Help: "Очистить всю переписку игры"},
	)
}

// idOutput is the -json form of a command that created something.
type idOutput struct {
	ID int `json:"id"`
}

// adminLogOutput is the -json form of admin-log: the events and the
// timestamp to pass to the next call as -since.
type adminLogOutput struct {
	Entries []dzzzr.AdminLogEntry `json:"entries"`
	Next    string                `json:"next"`
}

// ---------------------------------------------------------------------------
// Argument helpers

// needExactArgs checks that a command got exactly the arguments it takes.
func needExactArgs(args []string, n int, usage string) error {
	if len(args) != n {
		return fatal("неверное число аргументов, использование: dzzzr %s", usage)
	}
	return nil
}

// parseID reads a positive identifier, naming it in the error.
func parseID(name, s string) (int, error) {
	n, err := parseNumber(name, s)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, fatal("неверный %s %q: ожидалось положительное число", name, s)
	}
	return n, nil
}

// parseNumber reads a non-negative number argument.
func parseNumber(name, s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0, fatal("неверный %s %q: ожидалось неотрицательное число", name, s)
	}
	return n, nil
}

// gameAndTeam reads the two identifiers the live-screen commands start with.
func gameAndTeam(args []string) (gameID, teamID int, err error) {
	if gameID, err = parseID("номер игры", args[0]); err != nil {
		return 0, 0, err
	}
	if teamID, err = parseID("номер команды", args[1]); err != nil {
		return 0, 0, err
	}
	return gameID, teamID, nil
}

// reportDone prints the outcome of a command that only reports success.
func reportDone(cfg *config, format string, args ...any) error {
	if cfg.jsonOut {
		return outputJSON(cfg, okOutput{OK: true})
	}
	_, _ = fmt.Fprintf(cfg.stdout, format+"\n", args...)
	return nil
}

// yesNo renders a flag the way the tables read.
func yesNo(v bool) string {
	if v {
		return "да"
	}
	return "нет"
}

// parseCorrectionKind checks the bonus/penalty argument before the library
// does, so the error is in Russian.
func parseCorrectionKind(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "bonus", "бонус":
		return "bonus", nil
	case "penalty", "штраф":
		return "penalty", nil
	}
	return "", fatal("неверный вид поправки %q: укажите bonus или penalty", s)
}

// parseLogEvent reads the event of admin-set-time: a number or one of the two
// names the engine's own screen offers.
func parseLogEvent(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "level", "уровень":
		return dzzzr.LogEventLevelIssued, nil
	case "code", "код":
		return dzzzr.LogEventCodeAccepted, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0, fatal("неверное событие %q: укажите level, code или номер события", s)
	}
	return n, nil
}

// applicationStatusCodes maps the names the status arguments accept to the
// engine's numbers.
var applicationStatusCodes = map[string]int{
	"pending": dzzzr.ApplicationPending, "не рассмотрена": dzzzr.ApplicationPending,
	"accepted": dzzzr.ApplicationAccepted, "принята": dzzzr.ApplicationAccepted,
	"rejected": dzzzr.ApplicationRejected, "отклонена": dzzzr.ApplicationRejected,
	"out-of-contest": dzzzr.ApplicationOutOfContest, "вне зачета": dzzzr.ApplicationOutOfContest,
	"author": dzzzr.ApplicationAuthor, "автор": dzzzr.ApplicationAuthor,
	"disqualified": dzzzr.ApplicationDisqualified, "дисквалифицирована": dzzzr.ApplicationDisqualified,
}

// parseApplicationStatus reads an application status: a number 0–5 or a name.
func parseApplicationStatus(s string) (int, error) {
	key := strings.ToLower(strings.TrimSpace(s))
	if code, ok := applicationStatusCodes[key]; ok {
		return code, nil
	}
	n, err := strconv.Atoi(key)
	if err != nil || n < dzzzr.ApplicationPending || n > dzzzr.ApplicationDisqualified {
		return 0, fatal("неверный статус заявки %q: укажите число 0–5 или одно из: не рассмотрена, принята, отклонена, вне зачета, автор, дисквалифицирована", s)
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Games

func cmdAdminGames(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if len(args) > 0 {
		return fatal("команда admin-games не принимает аргументов")
	}
	games, err := c.AdminListGames(ctx)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, games)
	}
	if len(games) == 0 {
		_, _ = fmt.Fprintln(cfg.stdout, "Игр не найдено.")
		return nil
	}
	tw := newTable(cfg.stdout)
	_, _ = fmt.Fprintln(tw, "ID\tДата\tНомер\tНазвание\tСтатус\tСезон\tОпубликована")
	for _, g := range games {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			g.ID, dash(g.Date), dash(g.Number), dash(g.Name), dash(g.Status), dash(g.Season), yesNo(g.Published))
	}
	return tw.Flush()
}

func cmdAdminGameInfo(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-game-info ИГРА"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	info, err := c.AdminGetGame(ctx, gameID)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, info)
	}
	_, _ = fmt.Fprintf(cfg.stdout, "Игра %d\n", info.ID)
	printGameParams(cfg.stdout, info.Params)
	return nil
}

func cmdAdminCreateGame(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 1, "admin-create-game ключ=значение..."); err != nil {
		return err
	}
	p, err := gameParamsFromArgs(args)
	if err != nil {
		return err
	}
	id, err := c.AdminCreateGame(ctx, p)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, idOutput{ID: id})
	}
	_, _ = fmt.Fprintf(cfg.stdout, "Игра создана: %d\n", id)
	return nil
}

func cmdAdminUpdateGame(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 2, "admin-update-game ИГРА ключ=значение..."); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	p, err := gameParamsFromArgs(args[1:])
	if err != nil {
		return err
	}
	if err := c.AdminUpdateGame(ctx, gameID, p); err != nil {
		return err
	}
	return reportDone(cfg, "Игра %d обновлена.", gameID)
}

func cmdAdminDeleteGame(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-delete-game ИГРА"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	if err := c.AdminDeleteGame(ctx, gameID); err != nil {
		return err
	}
	return reportDone(cfg, "Игра %d удалена.", gameID)
}

func cmdAdminCopyGame(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-copy-game ИГРА [-with-levels]"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	newID, err := c.AdminCopyGame(ctx, gameID, cfg.withLevels)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, idOutput{ID: newID})
	}
	_, _ = fmt.Fprintf(cfg.stdout, "Игра %d скопирована в %d\n", gameID, newID)
	return nil
}

// ---------------------------------------------------------------------------
// Levels

func cmdAdminLevels(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-levels ИГРА"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	levels, err := c.AdminListLevels(ctx, gameID)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, levels)
	}
	if len(levels) == 0 {
		_, _ = fmt.Fprintln(cfg.stdout, "Заданий нет.")
		return nil
	}
	tw := newTable(cfg.stdout)
	_, _ = fmt.Fprintln(tw, "№\tID\tНазвание\tТип\tКоды\tНужно\tПопытки\tПодсказки\tОпубликовано")
	for _, l := range levels {
		kind := dash(l.Kind)
		if l.Skvoz {
			kind += ", сквозной"
		}
		_, _ = fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			l.Order, l.ID, dash(l.Title), kind, dash(strings.Join(l.Codes, " ")),
			dash(l.CodeCount), dash(l.TryLimit), dash(l.HintTimings), yesNo(l.Published))
	}
	return tw.Flush()
}

func cmdAdminLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 2, "admin-level ИГРА ЗАДАНИЕ"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	levelID, err := parseID("номер задания", args[1])
	if err != nil {
		return err
	}
	info, err := c.AdminGetLevel(ctx, gameID, levelID)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, info)
	}
	_, _ = fmt.Fprintf(cfg.stdout, "Задание %d игры %d, уровень %d\n", info.ID, info.GameID, info.Order)
	printLevelParams(cfg.stdout, info.Params)
	return nil
}

func cmdAdminCreateLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 2, "admin-create-level ИГРА ключ=значение..."); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	p, err := levelParamsFromArgs(args[1:])
	if err != nil {
		return err
	}
	id, err := c.AdminCreateLevel(ctx, gameID, p)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, idOutput{ID: id})
	}
	_, _ = fmt.Fprintf(cfg.stdout, "Задание создано: %d\n", id)
	return nil
}

func cmdAdminUpdateLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 3, "admin-update-level ИГРА ЗАДАНИЕ ключ=значение..."); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	levelID, err := parseID("номер задания", args[1])
	if err != nil {
		return err
	}
	p, err := levelParamsFromArgs(args[2:])
	if err != nil {
		return err
	}
	if err := c.AdminUpdateLevel(ctx, gameID, levelID, p); err != nil {
		return err
	}
	return reportDone(cfg, "Задание %d обновлено.", levelID)
}

func cmdAdminDeleteLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 2, "admin-delete-level ИГРА ЗАДАНИЕ"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	levelID, err := parseID("номер задания", args[1])
	if err != nil {
		return err
	}
	if err := c.AdminDeleteLevel(ctx, gameID, levelID); err != nil {
		return err
	}
	// The engine answers a refused delete exactly like a performed one, so
	// the only way to tell them apart is to look. See silentRefusal.
	levels, err := c.AdminListLevels(ctx, gameID)
	if err != nil {
		return err
	}
	for _, l := range levels {
		if l.ID == levelID {
			return silentRefusal("задание %d на месте", levelID)
		}
	}
	return reportDone(cfg, "Задание %d удалено.", levelID)
}

func cmdAdminMoveLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 3, "admin-move-level ИГРА ЗАДАНИЕ up|down"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	levelID, err := parseID("номер задания", args[1])
	if err != nil {
		return err
	}
	var up bool
	switch strings.ToLower(strings.TrimSpace(args[2])) {
	case "up", "вверх":
		up = true
	case "down", "вниз":
		up = false
	default:
		return fatal("укажите up или down")
	}
	// The order is read before and after, because the engine reports nothing
	// either way: it accepts a move it silently refuses exactly like one it
	// performs.
	before, atEdge, err := levelOrder(ctx, c, gameID, levelID, up)
	if err != nil {
		return err
	}
	if err := c.AdminMoveLevel(ctx, levelID, up); err != nil {
		return err
	}
	after, _, err := levelOrder(ctx, c, gameID, levelID, up)
	if err != nil {
		return err
	}
	if before != after {
		return reportDone(cfg, "Задание %d перемещено с позиции %d на %d.", levelID, before, after)
	}
	// Standing still is only expected at the edge the move points at. Any
	// other stalled move is the same silent refusal admin-delete-level runs
	// into, so it must not be reported as "nowhere left to go".
	if atEdge {
		return reportDone(cfg, "Задание %d осталось на позиции %d: двигать дальше некуда.", levelID, after)
	}
	return silentRefusal("задание %d на позиции %d", levelID, before)
}

// silentRefusal explains a write the engine accepted and then did not
// perform. Measured against classic.dzzzr.ru on 2026-09-09: while a game's
// date is 00.00.0000 the engine keeps every level and stops rendering the
// delete and move controls, yet answers both requests with the same clean
// redirect as a success, carrying no err= at all.
func silentRefusal(what string, args ...any) error {
	return fatal("движок оставил "+what+". Он молча отказывает, пока у игры не заполнены дата и время (проверьте date= и time= в admin-game-info)", args...)
}

// levelOrder returns a level's position and whether it is already at the end
// the move points at. The edge is read from the neighbors rather than from
// the number of levels, because the engine's positions are not necessarily
// consecutive: measured on 2026-09-09, moving the last level down leaves it
// last but bumps its number, so a list of 51 levels can end at 52.
func levelOrder(ctx context.Context, c *dzzzr.Client, gameID, levelID int, up bool) (order int, atEdge bool, err error) {
	levels, err := c.AdminListLevels(ctx, gameID)
	if err != nil {
		return 0, false, err
	}
	found := false
	for _, l := range levels {
		if l.ID == levelID {
			order, found = l.Order, true
			break
		}
	}
	if !found {
		return 0, false, fatal("задание %d не найдено в игре %d", levelID, gameID)
	}
	atEdge = true
	for _, l := range levels {
		if l.ID == levelID {
			continue
		}
		if (up && l.Order < order) || (!up && l.Order > order) {
			atEdge = false
			break
		}
	}
	return order, atEdge, nil
}

func cmdAdminCopyLevels(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 2, "admin-copy-levels ИГРА ИГРА-ИСТОЧНИК"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	sourceID, err := parseID("номер игры-источника", args[1])
	if err != nil {
		return err
	}
	if err := c.AdminCopyLevels(ctx, gameID, sourceID); err != nil {
		return err
	}
	return reportDone(cfg, "Задания игры %d скопированы в игру %d.", sourceID, gameID)
}

// ---------------------------------------------------------------------------
// Teams

func cmdAdminTeams(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-teams ИГРА"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	teams, err := c.AdminListTeams(ctx, gameID)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, teams)
	}
	if len(teams) == 0 {
		_, _ = fmt.Fprintln(cfg.stdout, "Заявок нет.")
		return nil
	}
	tw := newTable(cfg.stdout)
	_, _ = fmt.Fprintln(tw, "ID\tНазвание\tКапитан\tСтатус\tPIN\tОчки\tИгр\tЗаблокирована")
	for _, t := range teams {
		status := t.StatusText
		if status == "" && t.Status >= 0 {
			status = dzzzr.ApplicationStatusText(t.Status)
		}
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			t.ID, dash(t.Name), dash(t.Captain), dash(status), dash(t.Pin), t.Points, t.GamesCount, yesNo(t.Blocked))
	}
	return tw.Flush()
}

func cmdAdminTeam(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 2, "admin-team ИГРА КОМАНДА"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	info, err := c.AdminGetTeam(ctx, gameID, teamID)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, info)
	}
	w := cfg.stdout
	_, _ = fmt.Fprintf(w, "Команда %d в игре %d\n", info.ID, gameID)
	_, _ = fmt.Fprintf(w, "  Название:      %s\n", dash(info.Name))
	_, _ = fmt.Fprintf(w, "  Капитан:       %s\n", dash(info.Captain))
	_, _ = fmt.Fprintf(w, "  Штаб:          %s\n", dash(info.Shtab))
	_, _ = fmt.Fprintf(w, "  Помощник:      %s\n", dash(info.Helper))
	_, _ = fmt.Fprintf(w, "  Статус заявки: %s (%d)\n", dzzzr.ApplicationStatusText(info.Status), info.Status)
	_, _ = fmt.Fprintf(w, "  PIN:           %s\n", dash(info.Pin))
	_, _ = fmt.Fprintf(w, "  Очки:          %d\n", info.Points)
	_, _ = fmt.Fprintf(w, "  Новичок:       %s\n", yesNo(info.Novice))
	_, _ = fmt.Fprintf(w, "  Заблокирована: %s\n", yesNo(info.Blocked))
	_, _ = fmt.Fprintf(w, "  Сайт:          %s\n", dash(info.Website))
	if info.League != "" {
		_, _ = fmt.Fprintf(w, "  Лига:          %s\n", info.League)
	}
	if info.Deviz != "" {
		_, _ = fmt.Fprintf(w, "  Девиз:         %s\n", firstLineOf(info.Deviz))
	}
	if len(info.Roster) > 0 {
		onTeam := 0
		for _, p := range info.Roster {
			if p.OnTeam {
				onTeam++
			}
		}
		_, _ = fmt.Fprintf(w, "\nСостав: %d в команде из %d\n", onTeam, len(info.Roster))
		for _, p := range info.Roster {
			mark := " "
			if p.OnTeam {
				mark = "+"
			}
			_, _ = fmt.Fprintf(w, "  %s %s\n", mark, p.Login)
		}
	}
	return nil
}

func cmdAdminCityTeams(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 1, "admin-city-teams ИГРА [ПОДСТРОКА]"); err != nil {
		return err
	}
	if len(args) > 2 {
		return fatal("admin-city-teams принимает игру и необязательную подстроку")
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	teams, err := c.AdminListCityTeams(ctx, gameID)
	if err != nil {
		return err
	}
	if len(args) == 2 {
		needle := strings.ToLower(args[1])
		kept := teams[:0]
		for _, t := range teams {
			if strings.Contains(strings.ToLower(t.Name), needle) {
				kept = append(kept, t)
			}
		}
		teams = kept
	}
	if cfg.jsonOut {
		return outputJSON(cfg, teams)
	}
	if len(teams) == 0 {
		_, _ = fmt.Fprintln(cfg.stdout, "Команд не найдено.")
		return nil
	}
	tw := newTable(cfg.stdout)
	_, _ = fmt.Fprintln(tw, "ID\tНазвание")
	for _, t := range teams {
		_, _ = fmt.Fprintf(tw, "%d\t%s\n", t.ID, t.Name)
	}
	return tw.Flush()
}

func cmdAdminSetApplication(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 3, "admin-set-application ИГРА КОМАНДА ключ=значение..."); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	kv, err := parseKV(args[2:])
	if err != nil {
		return err
	}
	p, err := applicationParamsFromKV(kv)
	if err != nil {
		return err
	}
	if err := c.AdminSetApplication(ctx, gameID, teamID, p); err != nil {
		return err
	}
	return reportDone(cfg, "Заявка команды %d обновлена.", teamID)
}

func cmdAdminAcceptAll(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-accept-all ИГРА"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	if err := c.AdminAcceptAllApplications(ctx, gameID); err != nil {
		return err
	}
	return reportDone(cfg, "Заявки игры %d приняты.", gameID)
}

func cmdAdminGeneratePins(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-generate-pins ИГРА"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	if err := c.AdminGeneratePins(ctx, gameID); err != nil {
		return err
	}
	return reportDone(cfg, "PIN-коды игры %d перевыпущены.", gameID)
}

func cmdAdminAddTeam(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 2, "admin-add-team ИГРА КОМАНДА"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	if err := c.AdminAddTeamToGame(ctx, gameID, teamID); err != nil {
		return err
	}
	return reportDone(cfg, "Команда %d добавлена в игру %d.", teamID, gameID)
}

func cmdAdminRemoveApplication(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 3, "admin-remove-application ИГРА КОМАНДА СТАТУС"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	status, err := parseApplicationStatus(args[2])
	if err != nil {
		return err
	}
	if err := c.AdminRemoveApplication(ctx, gameID, teamID, status); err != nil {
		return err
	}
	return reportDone(cfg, "Заявка команды %d удалена.", teamID)
}

func cmdAdminCreateTeam(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 2, "admin-create-team НАЗВАНИЕ КАПИТАН"); err != nil {
		return err
	}
	name := strings.TrimSpace(args[0])
	captain := strings.TrimSpace(args[1])
	if name == "" || captain == "" {
		return fatal("название команды и логин капитана обязательны")
	}
	id, err := c.AdminCreateTeam(ctx, name, captain)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, idOutput{ID: id})
	}
	if id == 0 {
		_, _ = fmt.Fprintf(cfg.stdout, "Команда %s создана, движок не сообщил её номер.\n", name)
		return nil
	}
	_, _ = fmt.Fprintf(cfg.stdout, "Команда создана: %d\n", id)
	return nil
}

// ---------------------------------------------------------------------------
// Live game screen

func cmdAdminMonitor(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-monitor ИГРА"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	st, err := c.AdminMonitor(ctx, gameID)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, st)
	}
	w := cfg.stdout
	_, _ = fmt.Fprintf(w, "Игра %d: %s\n", st.GameID, dash(st.GameName))
	if st.EngineStopped {
		_, _ = fmt.Fprintln(w, "Движок: остановлен")
	} else {
		_, _ = fmt.Fprintln(w, "Движок: работает")
	}
	if len(st.Teams) > 0 {
		_, _ = fmt.Fprintln(w, "\nКоманды:")
		tw := newTable(w)
		_, _ = fmt.Fprintln(tw, "  ID\tНазвание")
		for _, t := range st.Teams {
			_, _ = fmt.Fprintf(tw, "  %d\t%s\n", t.ID, dash(t.Name))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if len(st.Levels) > 0 {
		_, _ = fmt.Fprintln(w, "\nУровни:")
		tw := newTable(w)
		_, _ = fmt.Fprintln(tw, "  №\tНазвание")
		for _, l := range st.Levels {
			_, _ = fmt.Fprintf(tw, "  %d\t%s\n", l.Order, dash(l.Title))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	return nil
}

// logEventText names a game log event code.
func logEventText(event int) string {
	switch event {
	case dzzzr.LogEventLevelIssued:
		return "выдан уровень"
	case dzzzr.LogEventCodeAccepted:
		return "принят код"
	case dzzzr.LogEventBonusIssued:
		return "выдан бонусный уровень"
	case dzzzr.LogEventHint1:
		return "подсказка 1"
	case dzzzr.LogEventHint2:
		return "подсказка 2"
	case dzzzr.LogEventWrongCode:
		return "неверный код"
	case dzzzr.LogEventNextLevel:
		return "переход на следующий уровень"
	case dzzzr.LogEventHint1Early:
		return "подсказка 1 досрочно"
	case dzzzr.LogEventHint2Early:
		return "подсказка 2 досрочно"
	case dzzzr.LogEventSpoiler:
		return "принят код спойлера"
	}
	return "событие " + strconv.Itoa(event)
}

func cmdAdminLog(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-log ИГРА [-since «ГГГГ-ММ-ДД ЧЧ:ММ:СС»]"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	entries, next, err := c.AdminGetLog(ctx, gameID, cfg.since)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, adminLogOutput{Entries: entries, Next: next})
	}
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(cfg.stdout, "Новых событий нет.")
	} else {
		tw := newTable(cfg.stdout)
		_, _ = fmt.Fprintln(tw, "Время\tКоманда\tУровень\tСобытие\tКомментарий")
		for _, e := range entries {
			_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\n", dash(e.Time), e.TeamID, e.Level, logEventText(e.Event), dash(e.Comment))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if next != "" {
		_, _ = fmt.Fprintf(cfg.stdout, "\nСледующий запрос: -since «%s»\n", next)
	}
	return nil
}

func cmdAdminGiveLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 3, "admin-give-level ИГРА КОМАНДА УРОВЕНЬ [-at «ГГГГ-ММ-ДД ЧЧ:ММ:СС»]"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	level, err := parseLevel(args[2])
	if err != nil {
		return err
	}
	if err := c.AdminGiveLevel(ctx, gameID, teamID, level, cfg.at); err != nil {
		return err
	}
	return reportDone(cfg, "Команде %d выдан уровень %d.", teamID, level)
}

func cmdAdminAcceptLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 3, "admin-accept-level ИГРА КОМАНДА УРОВЕНЬ [-at «ГГГГ-ММ-ДД ЧЧ:ММ:СС»]"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	level, err := parseLevel(args[2])
	if err != nil {
		return err
	}
	if err := c.AdminAcceptLevel(ctx, gameID, teamID, level, cfg.at); err != nil {
		return err
	}
	return reportDone(cfg, "Команде %d засчитан уровень %d.", teamID, level)
}

func cmdAdminAcceptCode(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 4, "admin-accept-code ИГРА КОМАНДА УРОВЕНЬ КОД [-at «ГГГГ-ММ-ДД ЧЧ:ММ:СС»]"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	level, err := parseLevel(args[2])
	if err != nil {
		return err
	}
	code := strings.TrimSpace(args[3])
	if code == "" {
		return fatal("код пуст")
	}
	if err := c.AdminAcceptCode(ctx, gameID, teamID, level, code, cfg.at); err != nil {
		return err
	}
	return reportDone(cfg, "Команде %d засчитан код %s на уровне %d.", teamID, code, level)
}

func cmdAdminClearCodes(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 3, "admin-clear-codes ИГРА КОМАНДА УРОВЕНЬ"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	level, err := parseLevel(args[2])
	if err != nil {
		return err
	}
	if err := c.AdminClearLevelCodes(ctx, gameID, teamID, level); err != nil {
		return err
	}
	return reportDone(cfg, "Коды команды %d на уровне %d сняты.", teamID, level)
}

func cmdAdminRemoveLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 3, "admin-remove-level ИГРА КОМАНДА УРОВЕНЬ"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	// Team 0 removes the level from every team of the game.
	teamID, err := parseNumber("номер команды", args[1])
	if err != nil {
		return err
	}
	level, err := parseLevel(args[2])
	if err != nil {
		return err
	}
	if err := c.AdminRemoveLevel(ctx, gameID, teamID, level); err != nil {
		return err
	}
	if teamID == 0 {
		return reportDone(cfg, "Уровень %d убран у всех команд.", level)
	}
	return reportDone(cfg, "Уровень %d убран у команды %d.", level, teamID)
}

func cmdAdminClearProgress(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 2, "admin-clear-progress ИГРА КОМАНДА"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	if err := c.AdminClearTeamProgress(ctx, gameID, teamID); err != nil {
		return err
	}
	return reportDone(cfg, "Игровой лог команды %d очищен.", teamID)
}

func cmdAdminPlanLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 3, "admin-plan-level ИГРА КОМАНДА УРОВЕНЬ"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	level, err := parseLevel(args[2])
	if err != nil {
		return err
	}
	if err := c.AdminPlanLevel(ctx, gameID, teamID, level); err != nil {
		return err
	}
	return reportDone(cfg, "Уровень %d поставлен в очередь команды %d.", level, teamID)
}

func cmdAdminUnplanLevel(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 3, "admin-unplan-level ИГРА КОМАНДА ПОЗИЦИЯ"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	order, err := parseID("номер позиции", args[2])
	if err != nil {
		return err
	}
	if err := c.AdminUnplanLevel(ctx, gameID, teamID, order); err != nil {
		return err
	}
	return reportDone(cfg, "Позиция %d очереди команды %d убрана.", order, teamID)
}

func cmdAdminAddCorrection(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 4, "admin-add-correction ИГРА КОМАНДА bonus|penalty МИНУТЫ [КОММЕНТАРИЙ...]"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	kind, err := parseCorrectionKind(args[2])
	if err != nil {
		return err
	}
	minutes, err := parseNumber("число минут", args[3])
	if err != nil {
		return err
	}
	comment := strings.TrimSpace(strings.Join(args[4:], " "))
	if err := c.AdminAddCorrection(ctx, gameID, teamID, kind, minutes, comment); err != nil {
		return err
	}
	if kind == "bonus" {
		return reportDone(cfg, "Команде %d начислено %d бонусных минут.", teamID, minutes)
	}
	return reportDone(cfg, "Команде %d начислено %d штрафных минут.", teamID, minutes)
}

func cmdAdminDeleteCorrections(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 3, "admin-delete-corrections ИГРА КОМАНДА bonus|penalty"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	kind, err := parseCorrectionKind(args[2])
	if err != nil {
		return err
	}
	if err := c.AdminDeleteCorrections(ctx, gameID, teamID, kind); err != nil {
		return err
	}
	if kind == "bonus" {
		return reportDone(cfg, "Бонусы команды %d сняты.", teamID)
	}
	return reportDone(cfg, "Штрафы команды %d сняты.", teamID)
}

func cmdAdminBlockTeam(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 2, "admin-block-team ИГРА КОМАНДА"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	if err := c.AdminBlockTeam(ctx, gameID, teamID); err != nil {
		return err
	}
	return reportDone(cfg, "Игра команды %d приостановлена.", teamID)
}

func cmdAdminUnblockTeam(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 2, "admin-unblock-team ИГРА КОМАНДА"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	if err := c.AdminUnblockTeam(ctx, gameID, teamID); err != nil {
		return err
	}
	return reportDone(cfg, "Игра команды %d возобновлена.", teamID)
}

// cmdAdminEngine switches the project's engine. The engine itself only has a
// toggle, so on and off read the current state first and act when it differs;
// "toggle" presses the switch unconditionally, which is what the organizer's
// own button does.
func cmdAdminEngine(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 2, "admin-engine ИГРА on|off|toggle"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(args[1])) {
	case "on", "1", "вкл":
		if err := c.AdminSetEngine(ctx, gameID, true); err != nil {
			return err
		}
		return reportDone(cfg, "Движок работает.")
	case "off", "0", "выкл":
		if err := c.AdminSetEngine(ctx, gameID, false); err != nil {
			return err
		}
		return reportDone(cfg, "Движок остановлен.")
	case "toggle", "переключить":
		if err := c.AdminToggleEngine(ctx, gameID); err != nil {
			return err
		}
		return reportDone(cfg, "Состояние движка переключено.")
	default:
		return fatal("неверное состояние движка %q: укажите on, off или toggle", args[1])
	}
}

func cmdAdminFinishGame(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-finish-game ИГРА"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	if err := c.AdminFinishGame(ctx, gameID); err != nil {
		return err
	}
	return reportDone(cfg, "Игра %d завершена.", gameID)
}

func cmdAdminSetTime(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 5, "admin-set-time ИГРА КОМАНДА УРОВЕНЬ СОБЫТИЕ «ГГГГ-ММ-ДД ЧЧ:ММ:СС»"); err != nil {
		return err
	}
	gameID, teamID, err := gameAndTeam(args)
	if err != nil {
		return err
	}
	level, err := parseLevel(args[2])
	if err != nil {
		return err
	}
	event, err := parseLogEvent(args[3])
	if err != nil {
		return err
	}
	at := strings.TrimSpace(args[4])
	if at == "" {
		return fatal("время события пусто")
	}
	if err := c.AdminSetEventTime(ctx, gameID, teamID, level, event, at); err != nil {
		return err
	}
	return reportDone(cfg, "Время события «%s» команды %d на уровне %d изменено на %s.", logEventText(event), teamID, level, at)
}

// ---------------------------------------------------------------------------
// Organizer chat

func cmdAdminMessages(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-messages ИГРА [-team N]"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	msgs, err := c.AdminListMessages(ctx, gameID, cfg.team)
	if err != nil {
		return err
	}
	if cfg.jsonOut {
		return outputJSON(cfg, msgs)
	}
	if len(msgs) == 0 {
		_, _ = fmt.Fprintln(cfg.stdout, "Сообщений нет.")
		return nil
	}
	tw := newTable(cfg.stdout)
	_, _ = fmt.Fprintln(tw, "Метка времени\tОтправитель\tАдресат\tПоказать после\tТекст")
	for _, m := range msgs {
		from := "организатор"
		if m.FromTeam {
			from = "команда"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			dash(m.Timestamp), from, dash(m.Addressee), dash(m.ShowAfter), dash(dzzzr.StripHTML(m.Content)))
	}
	return tw.Flush()
}

func cmdAdminSendMessage(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needArgs(args, 2, "admin-send-message ИГРА ТЕКСТ..."); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(args[1:], " "))
	if text == "" {
		return fatal("текст сообщения пуст")
	}
	if err := c.AdminSendMessage(ctx, gameID, cfg.team, text, cfg.showAfter, cfg.important); err != nil {
		return err
	}
	if cfg.team == 0 {
		return reportDone(cfg, "Сообщение отправлено всем командам.")
	}
	return reportDone(cfg, "Сообщение отправлено команде %d.", cfg.team)
}

func cmdAdminDeleteMessage(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 2, "admin-delete-message ИГРА «ГГГГ-ММ-ДД ЧЧ:ММ:СС»"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	timestamp := strings.TrimSpace(args[1])
	if timestamp == "" {
		return fatal("метка времени сообщения пуста")
	}
	if err := c.AdminDeleteMessage(ctx, gameID, timestamp); err != nil {
		return err
	}
	return reportDone(cfg, "Сообщение %s удалено.", timestamp)
}

func cmdAdminDeleteAllMessages(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if err := needExactArgs(args, 1, "admin-delete-all-messages ИГРА"); err != nil {
		return err
	}
	gameID, err := parseID("номер игры", args[0])
	if err != nil {
		return err
	}
	if err := c.AdminDeleteAllMessages(ctx, gameID); err != nil {
		return err
	}
	return reportDone(cfg, "Переписка игры %d очищена.", gameID)
}

// ---------------------------------------------------------------------------
// key=value arguments

// parseKV reads the "ключ=значение" arguments of the create, update and
// application commands. Keys are lower-cased and their dashes turned into
// underscores, so "new-pin" and "new_pin" name the same field; values are
// kept verbatim, because a legend or a greeting may contain anything.
func parseKV(args []string) (map[string]string, error) {
	kv := make(map[string]string, len(args))
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			return nil, fatal("аргумент %q не является парой ключ=значение", arg)
		}
		key = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(key)), "-", "_")
		if key == "" {
			return nil, fatal("аргумент %q не содержит имени ключа", arg)
		}
		kv[key] = value
	}
	return kv, nil
}

// sortedKV returns the keys in a fixed order, so the same arguments always
// produce the same error.
func sortedKV(kv map[string]string) []string { return slices.Sorted(maps.Keys(kv)) }

// unknownKey reports a key the command does not have, listing the ones it has.
func unknownKey(key string, valid []string) error {
	return fatal("неизвестный ключ %q. Допустимые ключи: %s", key, strings.Join(valid, ", "))
}

// kvInt reads a value that has to be a whole number.
func kvInt(key, value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fatal("ключ %s: ожидалось целое число, получено %q", key, value)
	}
	return n, nil
}

// kvIntPtr reads a number into a pointer, so a zero can be sent on purpose.
func kvIntPtr(key, value string) (*int, error) {
	n, err := kvInt(key, value)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// kvBool reads true/false/1/0/да/нет into a pointer, so a checkbox can be
// switched off on purpose.
func kvBool(key, value string) (*bool, error) {
	var b bool
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "да":
		b = true
	case "false", "0", "нет":
		b = false
	default:
		return nil, fatal("ключ %s: ожидалось true/false/1/0/да/нет, получено %q", key, value)
	}
	return &b, nil
}

// splitList splits a comma-separated compound value, dropping empty items.
func splitList(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// splitCode separates a code from the synonyms written after '#'.
func splitCode(s string) (code, synonyms string) {
	code, synonyms, _ = strings.Cut(s, "#")
	return strings.TrimSpace(code), strings.TrimSpace(synonyms)
}

// kvCodes reads codes="КОД[#СИНОНИМЫ]:СЛОЖНОСТЬ[:СЕКТОР],...". An empty
// value yields an empty list, which clears the level's codes.
func kvCodes(key, value string) ([]dzzzr.LevelCode, error) {
	out := []dzzzr.LevelCode{}
	for _, item := range splitList(value) {
		parts := strings.Split(item, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return nil, fatal("ключ %s: %q должно быть вида КОД:СЛОЖНОСТЬ[:СЕКТОР]", key, item)
		}
		code, synonyms := splitCode(parts[0])
		if code == "" {
			return nil, fatal("ключ %s: пустой код в %q", key, item)
		}
		cd := dzzzr.LevelCode{Code: code, Synonyms: synonyms, Danger: strings.TrimSpace(parts[1])}
		if len(parts) == 3 && strings.TrimSpace(parts[2]) != "" {
			sector, err := kvInt(key, parts[2])
			if err != nil {
				return nil, err
			}
			cd.Sector = sector
		}
		out = append(out, cd)
	}
	return out, nil
}

// kvBonusCodes reads bonus_codes="КОД[#СИНОНИМЫ]:СЛОЖНОСТЬ:МИНУТЫ,...".
func kvBonusCodes(key, value string) ([]dzzzr.BonusCode, error) {
	out := []dzzzr.BonusCode{}
	for _, item := range splitList(value) {
		parts := strings.Split(item, ":")
		if len(parts) != 3 {
			return nil, fatal("ключ %s: %q должно быть вида КОД:СЛОЖНОСТЬ:МИНУТЫ", key, item)
		}
		code, synonyms := splitCode(parts[0])
		if code == "" {
			return nil, fatal("ключ %s: пустой код в %q", key, item)
		}
		minutes, err := kvInt(key, parts[2])
		if err != nil {
			return nil, err
		}
		out = append(out, dzzzr.BonusCode{Code: code, Synonyms: synonyms, Danger: strings.TrimSpace(parts[1]), Minutes: minutes})
	}
	return out, nil
}

// kvFakeCodes reads fake_codes="КОД[#СИНОНИМЫ]:ШТРАФ,...".
func kvFakeCodes(key, value string) ([]dzzzr.FakeCode, error) {
	out := []dzzzr.FakeCode{}
	for _, item := range splitList(value) {
		parts := strings.Split(item, ":")
		if len(parts) != 2 {
			return nil, fatal("ключ %s: %q должно быть вида КОД:ШТРАФ", key, item)
		}
		code, synonyms := splitCode(parts[0])
		if code == "" {
			return nil, fatal("ключ %s: пустой код в %q", key, item)
		}
		penalty, err := kvInt(key, parts[1])
		if err != nil {
			return nil, err
		}
		out = append(out, dzzzr.FakeCode{Code: code, Synonyms: synonyms, Penalty: penalty})
	}
	return out, nil
}

// kvSpoilers reads spoilers="КОД[#СИНОНИМЫ]:ШТРАФ[:ТЕКСТ],...". The text is
// whatever follows the second colon, so it may contain colons itself.
func kvSpoilers(key, value string) ([]dzzzr.SpoilerParams, error) {
	out := []dzzzr.SpoilerParams{}
	for _, item := range splitList(value) {
		parts := strings.SplitN(item, ":", 3)
		if len(parts) < 2 {
			return nil, fatal("ключ %s: %q должно быть вида КОД:ШТРАФ[:ТЕКСТ]", key, item)
		}
		code, synonyms := splitCode(parts[0])
		if code == "" {
			return nil, fatal("ключ %s: пустой код в %q", key, item)
		}
		penalty, err := kvInt(key, parts[1])
		if err != nil {
			return nil, err
		}
		sp := dzzzr.SpoilerParams{Code: code, Synonyms: synonyms, Penalty: penalty}
		if len(parts) == 3 {
			sp.Text = parts[2]
		}
		out = append(out, sp)
	}
	return out, nil
}

// gameKeys are the ключ=значение names of admin-create-game and
// admin-update-game.
var gameKeys = []string{
	"name", "number", "date", "time", "author", "league", "other_league", "zone",
	"legend", "legend_comment", "anons", "clue_min", "clue_min2", "clue_min3",
	"master_code", "master_code_shtraf", "clue_before_shtraf", "penalty", "price",
	"breaf_place", "greeting", "duration", "bonus_after", "publish", "finished", "invitation",
}

// gameParamsFromArgs parses the ключ=значение tail of a game command.
func gameParamsFromArgs(args []string) (dzzzr.GameParams, error) {
	kv, err := parseKV(args)
	if err != nil {
		return dzzzr.GameParams{}, err
	}
	return gameParamsFromKV(kv)
}

// gameParamsFromKV turns parsed pairs into the library's game settings.
//
// An explicitly empty text value (greeting=) means "blank this field", which
// the engine does honor, so it goes into Clear rather than being dropped as
// an unset value. A numeric zero is not treated that way: the engine ignores
// a zero for these fields, measured against classic.dzzzr.ru on 2026-09-09.
func gameParamsFromKV(kv map[string]string) (dzzzr.GameParams, error) {
	var p dzzzr.GameParams
	for _, k := range sortedKV(kv) {
		v := kv[k]
		if v == "" && dzzzr.GameTextParam(k) {
			p.Clear = append(p.Clear, k)
			continue
		}
		var err error
		switch k {
		case "name":
			p.Name = v
		case "number":
			p.Number = v
		case "date":
			p.Date = v
		case "time":
			p.Time = v
		case "author":
			p.Author = v
		case "league":
			p.League, err = kvInt(k, v)
		case "other_league":
			p.OtherLeague, err = kvBool(k, v)
		case "zone":
			p.Zone = v
		case "legend":
			p.Legend = v
		case "legend_comment":
			p.LegendComment = v
		case "anons":
			p.Anons = v
		case "clue_min":
			p.ClueMin, err = kvInt(k, v)
		case "clue_min2":
			p.ClueMin2, err = kvInt(k, v)
		case "clue_min3":
			p.ClueMin3, err = kvInt(k, v)
		case "master_code":
			p.MasterCode = v
		case "master_code_shtraf":
			p.MasterCodeShtraf, err = kvInt(k, v)
		case "clue_before_shtraf":
			p.ClueBeforeShtraf, err = kvInt(k, v)
		case "penalty":
			p.Penalty, err = kvInt(k, v)
		case "price":
			p.Price = v
		case "breaf_place":
			p.BreafPlace = v
		case "greeting":
			p.Greeting = v
		case "duration":
			p.Duration, err = kvInt(k, v)
		case "bonus_after":
			p.BonusAfter, err = kvInt(k, v)
		case "publish":
			p.Publish, err = kvBool(k, v)
		case "finished":
			p.Finished, err = kvBool(k, v)
		case "invitation":
			p.Invitation, err = kvBool(k, v)
		default:
			return p, unknownKey(k, gameKeys)
		}
		if err != nil {
			return p, err
		}
	}
	return p, nil
}

// levelKeys are the ключ=значение names of admin-create-level and
// admin-update-level.
var levelKeys = []string{
	"title", "subtitle", "question", "hint1", "hint2", "location", "location_comment",
	"clue_min", "clue_min2", "clue_min3", "hint1_on_request", "hint1_interval", "hint1_penalty",
	"hint2_on_request", "hint2_interval", "hint2_penalty", "code_count", "try_limit", "penalty",
	"no_master_code", "is_sabotage", "bonus", "bonus_time", "skvoz", "skvoz_min", "skvoz_start",
	"lat", "lon", "radius", "show_coord_with_hint2", "no_break", "clue_before", "reserve",
	"publish", "greeting", "codes", "sector_names", "bonus_codes", "fake_codes", "spoilers", "comment", "time_add_bonus_all",
}

// levelParamsFromArgs parses the ключ=значение tail of a level command.
func levelParamsFromArgs(args []string) (dzzzr.LevelParams, error) {
	kv, err := parseKV(args)
	if err != nil {
		return dzzzr.LevelParams{}, err
	}
	return levelParamsFromKV(kv)
}

// levelParamsFromKV turns parsed pairs into the library's level settings.
func levelParamsFromKV(kv map[string]string) (dzzzr.LevelParams, error) {
	var p dzzzr.LevelParams
	for _, k := range sortedKV(kv) {
		v := kv[k]
		if v == "" && dzzzr.LevelTextParam(k) {
			p.Clear = append(p.Clear, k)
			continue
		}
		var err error
		switch k {
		case "title":
			p.Title = v
		case "subtitle":
			p.Subtitle = v
		case "question":
			p.Question = v
		case "hint1":
			p.Hint1 = v
		case "hint2":
			p.Hint2 = v
		case "location":
			p.Location = v
		case "location_comment":
			p.LocationComment = v
		case "comment":
			p.Comment = new(v)
		case "time_add_bonus_all":
			p.TimeAddBonusAll, err = kvIntPtr(k, v)
		case "clue_min":
			p.ClueMin, err = kvInt(k, v)
		case "clue_min2":
			p.ClueMin2, err = kvInt(k, v)
		case "clue_min3":
			p.ClueMin3, err = kvInt(k, v)
		case "hint1_on_request":
			p.Hint1OnRequest, err = kvBool(k, v)
		case "hint1_interval":
			p.Hint1Interval, err = kvInt(k, v)
		case "hint1_penalty":
			p.Hint1Penalty, err = kvInt(k, v)
		case "hint2_on_request":
			p.Hint2OnRequest, err = kvBool(k, v)
		case "hint2_interval":
			p.Hint2Interval, err = kvInt(k, v)
		case "hint2_penalty":
			p.Hint2Penalty, err = kvInt(k, v)
		case "code_count":
			p.CodeCount, err = kvInt(k, v)
		case "try_limit":
			p.TryLimit, err = kvInt(k, v)
		case "penalty":
			p.Penalty, err = kvIntPtr(k, v)
		case "no_master_code":
			p.NoMasterCode, err = kvBool(k, v)
		case "is_sabotage":
			p.IsSabotage, err = kvBool(k, v)
		case "bonus":
			p.Bonus, err = kvBool(k, v)
		case "bonus_time":
			p.BonusTime, err = kvInt(k, v)
		case "skvoz":
			p.Skvoz, err = kvBool(k, v)
		case "skvoz_min":
			p.SkvozMin, err = kvInt(k, v)
		case "skvoz_start":
			p.SkvozStart = v
		case "lat":
			p.Lat = v
		case "lon":
			p.Lon = v
		case "radius":
			p.Radius = v
		case "show_coord_with_hint2":
			p.ShowCoordHint2, err = kvBool(k, v)
		case "no_break":
			p.NoBreak, err = kvBool(k, v)
		case "clue_before":
			p.ClueBefore, err = kvBool(k, v)
		case "reserve":
			p.Reserve, err = kvBool(k, v)
		case "publish":
			p.Publish, err = kvBool(k, v)
		case "greeting":
			// Measured against classic.dzzzr.ru on 2026-09-09: the level form
			// carries no greeting field at all, so the engine drops this
			// silently. Refusing beats reporting a change that never happens;
			// greeting belongs to the game (admin-update-game).
			err = fatal("ключ greeting: у задания нет такого поля, движок его не сохранит. Приветствие задаётся игре: admin-update-game")
		case "codes":
			p.Codes, err = kvCodes(k, v)
		case "sector_names":
			p.SectorNames = splitList(v)
			if p.SectorNames == nil {
				p.SectorNames = []string{}
			}
		case "bonus_codes":
			p.BonusCodes, err = kvBonusCodes(k, v)
		case "fake_codes":
			p.FakeCodes, err = kvFakeCodes(k, v)
		case "spoilers":
			p.Spoilers, err = kvSpoilers(k, v)
		default:
			return p, unknownKey(k, levelKeys)
		}
		if err != nil {
			return p, err
		}
	}
	return p, nil
}

// applicationKeys are the ключ=значение names of admin-set-application.
var applicationKeys = []string{"status", "new_pin", "points", "novice", "comment"}

// applicationParamsFromKV turns parsed pairs into an application change.
func applicationParamsFromKV(kv map[string]string) (dzzzr.ApplicationParams, error) {
	var p dzzzr.ApplicationParams
	for _, k := range sortedKV(kv) {
		v := kv[k]
		var err error
		switch k {
		case "status":
			var status int
			if status, err = parseApplicationStatus(v); err == nil {
				p.Status = &status
			}
		case "new_pin":
			var b *bool
			if b, err = kvBool(k, v); err == nil {
				p.NewPin = *b
			}
		case "points":
			p.Points, err = kvIntPtr(k, v)
		case "novice":
			p.Novice, err = kvBool(k, v)
		case "comment":
			p.Comment = v
		default:
			return p, unknownKey(k, applicationKeys)
		}
		if err != nil {
			return p, err
		}
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Settings output

// kvLines collects the "ключ=значение" lines of a settings dump. Empty and
// zero values are skipped, so what is printed can be pasted back into an
// admin-update-* command.
type kvLines []string

func (l *kvLines) str(key, value string) {
	if strings.TrimSpace(value) != "" {
		*l = append(*l, key+"="+value)
	}
}

func (l *kvLines) num(key string, value int) {
	if value != 0 {
		*l = append(*l, key+"="+strconv.Itoa(value))
	}
}

func (l *kvLines) numPtr(key string, value *int) {
	if value != nil {
		*l = append(*l, key+"="+strconv.Itoa(*value))
	}
}

func (l *kvLines) flag(key string, value *bool) {
	if value != nil {
		*l = append(*l, key+"="+strconv.FormatBool(*value))
	}
}

// write prints the collected lines indented under a heading.
func (l kvLines) write(w io.Writer) {
	for _, line := range l {
		_, _ = fmt.Fprintln(w, "  "+line)
	}
}

// printTextBlock writes a long HTML field as plain indented text.
func printTextBlock(w io.Writer, title, text string) {
	text = strings.TrimSpace(dzzzr.StripHTML(text))
	if text == "" {
		return
	}
	_, _ = fmt.Fprintf(w, "\n%s:\n%s\n", title, indent(text, "  "))
}

// printGameParams writes a game's settings as the same pairs
// admin-update-game accepts, with the long texts in blocks below.
func printGameParams(w io.Writer, p dzzzr.GameParams) {
	var l kvLines
	l.str("name", p.Name)
	l.str("number", p.Number)
	l.str("date", p.Date)
	l.str("time", p.Time)
	l.str("author", p.Author)
	l.num("league", p.League)
	l.flag("other_league", p.OtherLeague)
	l.str("zone", p.Zone)
	l.num("clue_min", p.ClueMin)
	l.num("clue_min2", p.ClueMin2)
	l.num("clue_min3", p.ClueMin3)
	l.str("master_code", p.MasterCode)
	l.num("master_code_shtraf", p.MasterCodeShtraf)
	l.num("clue_before_shtraf", p.ClueBeforeShtraf)
	l.num("penalty", p.Penalty)
	l.str("price", p.Price)
	l.str("breaf_place", p.BreafPlace)
	l.num("duration", p.Duration)
	l.num("bonus_after", p.BonusAfter)
	l.flag("publish", p.Publish)
	l.flag("finished", p.Finished)
	l.flag("invitation", p.Invitation)
	l.write(w)
	printTextBlock(w, "legend", p.Legend)
	printTextBlock(w, "legend_comment", p.LegendComment)
	printTextBlock(w, "anons", p.Anons)
	printTextBlock(w, "greeting", p.Greeting)
}

// printLevelParams writes a level the same way, compound values included.
func printLevelParams(w io.Writer, p dzzzr.LevelParams) {
	var l kvLines
	l.str("title", p.Title)
	l.str("subtitle", p.Subtitle)
	l.str("location", p.Location)
	l.str("location_comment", p.LocationComment)
	l.numPtr("time_add_bonus_all", p.TimeAddBonusAll)
	l.num("clue_min", p.ClueMin)
	l.num("clue_min2", p.ClueMin2)
	l.num("clue_min3", p.ClueMin3)
	l.flag("hint1_on_request", p.Hint1OnRequest)
	l.num("hint1_interval", p.Hint1Interval)
	l.num("hint1_penalty", p.Hint1Penalty)
	l.flag("hint2_on_request", p.Hint2OnRequest)
	l.num("hint2_interval", p.Hint2Interval)
	l.num("hint2_penalty", p.Hint2Penalty)
	l.num("code_count", p.CodeCount)
	l.num("try_limit", p.TryLimit)
	l.numPtr("penalty", p.Penalty)
	l.flag("no_master_code", p.NoMasterCode)
	l.flag("is_sabotage", p.IsSabotage)
	l.flag("bonus", p.Bonus)
	l.num("bonus_time", p.BonusTime)
	l.flag("skvoz", p.Skvoz)
	l.num("skvoz_min", p.SkvozMin)
	l.str("skvoz_start", p.SkvozStart)
	l.str("lat", p.Lat)
	l.str("lon", p.Lon)
	l.str("radius", p.Radius)
	l.flag("show_coord_with_hint2", p.ShowCoordHint2)
	l.flag("no_break", p.NoBreak)
	l.flag("clue_before", p.ClueBefore)
	l.flag("reserve", p.Reserve)
	l.flag("publish", p.Publish)
	l.str("codes", formatCodes(p.Codes))
	l.str("sector_names", strings.Join(p.SectorNames, ","))
	l.str("bonus_codes", formatBonusCodes(p.BonusCodes))
	l.str("fake_codes", formatFakeCodes(p.FakeCodes))
	l.str("spoilers", formatSpoilers(p.Spoilers))
	l.write(w)
	if p.Comment != nil {
		printTextBlock(w, "comment", *p.Comment)
	}
	printTextBlock(w, "question", p.Question)
	printTextBlock(w, "hint1", p.Hint1)
	printTextBlock(w, "hint2", p.Hint2)
	printTextBlock(w, "greeting", p.Greeting)
}

// formatCode joins a code with its synonyms the way the arguments spell it.
func formatCode(code, synonyms string) string {
	if synonyms == "" {
		return code
	}
	return code + "#" + synonyms
}

// formatCodes renders codes= as admin-update-level would take it.
func formatCodes(codes []dzzzr.LevelCode) string {
	items := make([]string, 0, len(codes))
	for _, c := range codes {
		item := formatCode(c.Code, c.Synonyms) + ":" + c.Danger
		if c.Sector > 0 {
			item += ":" + strconv.Itoa(c.Sector)
		}
		items = append(items, item)
	}
	return strings.Join(items, ",")
}

// formatBonusCodes renders bonus_codes=.
func formatBonusCodes(codes []dzzzr.BonusCode) string {
	items := make([]string, 0, len(codes))
	for _, c := range codes {
		items = append(items, fmt.Sprintf("%s:%s:%d", formatCode(c.Code, c.Synonyms), c.Danger, c.Minutes))
	}
	return strings.Join(items, ",")
}

// formatFakeCodes renders fake_codes=.
func formatFakeCodes(codes []dzzzr.FakeCode) string {
	items := make([]string, 0, len(codes))
	for _, c := range codes {
		items = append(items, fmt.Sprintf("%s:%d", formatCode(c.Code, c.Synonyms), c.Penalty))
	}
	return strings.Join(items, ",")
}

// formatSpoilers renders spoilers=.
func formatSpoilers(spoilers []dzzzr.SpoilerParams) string {
	items := make([]string, 0, len(spoilers))
	for _, s := range spoilers {
		item := fmt.Sprintf("%s:%d", formatCode(s.Code, s.Synonyms), s.Penalty)
		if text := strings.TrimSpace(dzzzr.StripHTML(s.Text)); text != "" {
			item += ":" + firstLineOf(text)
		}
		items = append(items, item)
	}
	return strings.Join(items, ",")
}

// firstLineOf keeps a spoiler text on the one line the dump gives it.
func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + "…"
	}
	return s
}
