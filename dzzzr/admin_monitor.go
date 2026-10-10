package dzzzr

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// The live game screen is admin/gmstat1.php, which includes gmstatPost.php
// for organizer interventions. gmAdmin.php is a separate xajax planner and
// does not process these POST actions. Fresh events
// are polled from admin/lookLog.php, which returns them packed into the
// "opt" parameter of a meta-refresh URL as
// "team-level-event-comment-HH:MM:SS|..." records.

const (
	monitorPath = adminPath + "gmstat1.php"
	lookLogPath = adminPath + "lookLog.php"
)

var monitorLevelMenu = regexp.MustCompile(`(?i)\bshowMenu\s*\(\s*([0-9]+)\s*,\s*0\s*\)`)

// Game log event codes as written by the engine into gameLog.
const (
	LogEventLevelIssued  = 1  // уровень выдан
	LogEventCodeAccepted = 2  // принят код
	LogEventBonusIssued  = 3  // выдан бонусный (сквозной) уровень
	LogEventHint1        = 4  // запрошена первая подсказка
	LogEventHint2        = 5  // запрошена вторая подсказка
	LogEventWrongCode    = 6  // неверный код
	LogEventNextLevel    = 7  // переход на следующий уровень
	LogEventHint1Early   = 9  // первая подсказка досрочно
	LogEventHint2Early   = 10 // вторая подсказка досрочно
	LogEventSpoiler      = 11 // принят код спойлера
)

// MonitorTeam is one team's row on the live game screen.
type MonitorTeam struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status int    `json:"status"`
}

// MonitorLevel is one level column on the live game screen.
type MonitorLevel struct {
	Order int    `json:"order"`
	Title string `json:"title"`
}

// MonitorState is what the live game screen shows before any xajax update:
// which teams and levels the matrix has, and whether the engine is running.
type MonitorState struct {
	GameID        int            `json:"game_id"`
	GameName      string         `json:"game_name"`
	EngineStopped bool           `json:"engine_stopped"`
	Teams         []MonitorTeam  `json:"teams"`
	Levels        []MonitorLevel `json:"levels"`
}

// AdminLogEntry is one event polled from lookLog.php.
type AdminLogEntry struct {
	TeamID  int    `json:"team_id"`
	Level   int    `json:"level"`
	Event   int    `json:"event"`
	Comment string `json:"comment"`
	Time    string `json:"time"` // HH:MM:SS
}

// AdminMonitor loads the live game screen for a game.
func (c *Client) AdminMonitor(ctx context.Context, gameID int) (*MonitorState, error) {
	page, err := c.adminGet(ctx, monitorPath, url.Values{"finished": {strconv.Itoa(gameID)}, "Project": {""}})
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(page)
	if err != nil {
		return nil, &UndecodableResponseError{StatusCode: 200, Context: "admin monitor", Err: err}
	}
	st := &MonitorState{GameID: gameID}
	stateKnown := false
	walk(doc.root, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "input" || !strings.EqualFold(attr(n, "type"), "submit") {
			return true
		}
		switch strings.ToLower(strings.TrimSpace(attr(n, "value"))) {
		case "остановить робота", "остановить движок":
			stateKnown = true
		case "запустить робота", "включить робота", "включить движок":
			stateKnown, st.EngineStopped = true, true
		}
		return true
	})
	if !stateKnown {
		return nil, fmt.Errorf("dzzzr: admin monitor: engine control is missing or unrecognized")
	}
	teamsSeen, levelsSeen := map[int]bool{}, map[int]bool{}
	for _, f := range doc.forms() {
		for name, opts := range f.Selects {
			switch name {
			case "finished":
				for _, o := range opts {
					if o.Value == strconv.Itoa(gameID) {
						st.GameName = o.Text
					}
				}
			case "team":
				for _, o := range opts {
					id, err := strconv.Atoi(o.Value)
					if err != nil || id <= 0 || teamsSeen[id] {
						continue
					}
					teamsSeen[id] = true
					st.Teams = append(st.Teams, MonitorTeam{ID: id, Name: o.Text})
				}
			case "level", "MenuLevel":
				for _, o := range opts {
					n, err := strconv.Atoi(o.Value)
					if err != nil || n <= 0 || levelsSeen[n] {
						continue
					}
					levelsSeen[n] = true
					st.Levels = append(st.Levels, MonitorLevel{Order: n, Title: o.Text})
				}
			}
		}
	}
	if len(st.Levels) == 0 {
		// Classic renders level columns as clickable table headers and keeps
		// MenuLevel in a hidden input, rather than a select of level choices.
		seen := map[int]bool{}
		walk(doc.root, func(n *html.Node) bool {
			if n.Type != html.ElementNode || n.Data != "th" {
				return true
			}
			match := monitorLevelMenu.FindStringSubmatch(attr(n, "onclick"))
			if len(match) == 0 {
				return true
			}
			order, err := strconv.Atoi(match[1])
			if err == nil && order > 0 && !seen[order] {
				st.Levels = append(st.Levels, MonitorLevel{Order: order, Title: text(n)})
				seen[order] = true
			}
			return true
		})
	}
	if len(st.Teams) == 0 {
		// The matrix itself lists teams as rows with id="team<N>".
		for _, row := range doc.rows() {
			if id, ok := strings.CutPrefix(row.Attrs["id"], "team"); ok {
				n, err := strconv.Atoi(id)
				if err != nil || len(row.Cells) == 0 {
					continue
				}
				st.Teams = append(st.Teams, MonitorTeam{ID: n, Name: firstLine(row.Cells[0])})
			}
		}
	}
	return st, nil
}

// AdminGetLog polls the organizer's live log. since is the timestamp
// returned by a previous call ("YYYY-MM-DD HH:MM:SS"); empty starts from
// now, which is what the engine's own screen does. The second result is the
// timestamp to pass to the next call.
func (c *Client) AdminGetLog(ctx context.Context, gameID int, since string) ([]AdminLogEntry, string, error) {
	q := url.Values{"gmid": {strconv.Itoa(gameID)}, "Project": {""}, "refresh": {"15"}}
	if since != "" {
		q.Set("lastTime", since)
	}
	page, err := c.adminGet(ctx, lookLogPath, q)
	if err != nil {
		return nil, since, err
	}
	next, opt := parseLookLogRefresh(page)
	if next == "" {
		next = since
	}
	var out []AdminLogEntry
	for _, rec := range strings.Split(opt, "|") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		parts := strings.Split(rec, "-")
		if len(parts) < 5 {
			continue
		}
		// The comment may itself contain dashes; the time is always last and
		// the first three fields are numeric.
		e := AdminLogEntry{
			TeamID:  parseLooseInt(parts[0]),
			Level:   parseLooseInt(parts[1]),
			Event:   parseLooseInt(parts[2]),
			Comment: strings.Join(parts[3:len(parts)-1], "-"),
			Time:    parts[len(parts)-1],
		}
		out = append(out, e)
	}
	return out, next, nil
}

// parseLookLogRefresh extracts lastTime and opt from the meta refresh URL
// lookLog.php renders.
func parseLookLogRefresh(page string) (lastTime, opt string) {
	i := strings.Index(page, "lookLog.php?")
	if i < 0 {
		return "", ""
	}
	rest := page[i+len("lookLog.php?"):]
	if j := strings.IndexAny(rest, "\"'>"); j >= 0 {
		rest = rest[:j]
	}
	// A page rendered through an HTML escaper writes &amp; between the
	// parameters; Go's query parser would choke on the semicolon.
	rest = strings.ReplaceAll(rest, "&amp;", "&")
	q, err := url.ParseQuery(rest)
	if err != nil && len(q) == 0 {
		return "", ""
	}
	return q.Get("lastTime"), q.Get("opt")
}

// monitorPost submits an intervention on the live game screen.
func (c *Client) monitorPost(ctx context.Context, gameID int, form url.Values) error {
	form.Set("Project", "")
	form.Set("finished", strconv.Itoa(gameID))
	// The native handler selects the game from GET before processing POST.
	// Sending finished only in the form silently leaves interventions unapplied.
	location, _, err := c.adminPostQuery(ctx, monitorPath, url.Values{"finished": {strconv.Itoa(gameID)}, "Project": {""}}, form)
	if err != nil {
		return err
	}
	if code := linkQuery(location).Get("err"); code != "" && code != "0" {
		message := monitorErrors[code]
		if message == "" {
			message = "движок отклонил операцию"
		}
		return fmt.Errorf("dzzzr: admin %s: %s (err=%s)", form.Get("action"), message, code)
	}
	return nil
}

// gmstat1.php exposes intervention failures through the redirect query.
// Some codes describe partial application, so callers must not retry blindly.
var monitorErrors = map[string]string{
	"1":  "указано время ранее начала игры",
	"2":  "указано время ранее завершения предыдущего задания",
	"3":  "указано время позднее выполнения этого задания",
	"4":  "указано время позднее начала следующего задания",
	"5":  "этот уровень уже выдан команде",
	"6":  "нельзя засчитать невыданный уровень",
	"7":  "получается отрицательный интервал времени",
	"8":  "время больше отведённого на выполнение задания",
	"9":  "неверный формат даты",
	"10": "уровень выдан не всем командам: уже получившим его сохранено прежнее время выдачи",
	"11": "время позже окончания игры командой",
	"12": "нельзя снять текущий уровень: сначала выдайте следующий",
	"13": "нельзя выдавать и планировать часть цепочки заданий",
	"14": "нельзя использовать линейную выдачу при наличии цепочек",
	"15": "нельзя удалить из линейки часть цепочки заданий",
	"17": "при засчитывании уровня произошли ошибки: уровень засчитан не всем командам",
	"18": "нельзя скопировать линейку, если команда уже начала игру",
}

// AdminGiveLevel issues a level to a team. at is an optional
// "YYYY-MM-DD HH:MM:SS" timestamp; empty means now.
func (c *Client) AdminGiveLevel(ctx context.Context, gameID, teamID, level int, at string) error {
	return c.monitorPost(ctx, gameID, url.Values{
		"action": {"newlevel"}, "team": {strconv.Itoa(teamID)}, "level": {strconv.Itoa(level)}, "time": {at},
	})
}

// AdminAcceptLevel marks a level as fully solved by a team, filling in all
// its codes.
func (c *Client) AdminAcceptLevel(ctx context.Context, gameID, teamID, level int, at string) error {
	return c.monitorPost(ctx, gameID, url.Values{
		"action": {"acceptLevel"}, "team": {strconv.Itoa(teamID)}, "level": {strconv.Itoa(level)}, "time": {at},
	})
}

// AdminGiveAndAcceptLevel issues a level and immediately marks it solved.
func (c *Client) AdminGiveAndAcceptLevel(ctx context.Context, gameID, teamID, level int, at string) error {
	return c.monitorPost(ctx, gameID, url.Values{
		"action": {"newLevelAndAccept"}, "team": {strconv.Itoa(teamID)}, "level": {strconv.Itoa(level)}, "time": {at},
	})
}

// AdminAcceptCode records a single code for a team on a level, as if the
// team had entered it.
func (c *Client) AdminAcceptCode(ctx context.Context, gameID, teamID, level int, code, at string) error {
	return c.monitorPost(ctx, gameID, url.Values{
		"action": {"code"}, "team": {strconv.Itoa(teamID)}, "level": {strconv.Itoa(level)}, "code": {code}, "time": {at},
	})
}

// AdminClearLevelCodes removes every code a team entered on a level.
func (c *Client) AdminClearLevelCodes(ctx context.Context, gameID, teamID, level int) error {
	return c.monitorPost(ctx, gameID, url.Values{
		"action": {"delLevel"}, "team": {strconv.Itoa(teamID)}, "level": {strconv.Itoa(level)},
	})
}

// AdminRemoveLevel removes a level from a team's game entirely (or from
// every team when teamID is 0).
func (c *Client) AdminRemoveLevel(ctx context.Context, gameID, teamID, level int) error {
	return c.monitorPost(ctx, gameID, url.Values{
		"action": {"removeLevel"}, "team": {strconv.Itoa(teamID)}, "level": {strconv.Itoa(level)},
	})
}

// AdminClearTeamProgress deletes a team's whole game log.
func (c *Client) AdminClearTeamProgress(ctx context.Context, gameID, teamID int) error {
	return c.monitorPost(ctx, gameID, url.Values{"action": {"delstat"}, "team": {strconv.Itoa(teamID)}})
}

// AdminPlanLevel appends a level to a team's planned queue.
func (c *Client) AdminPlanLevel(ctx context.Context, gameID, teamID, level int) error {
	return c.monitorPost(ctx, gameID, url.Values{
		"action": {"line"}, "team": {strconv.Itoa(teamID)}, "level": {strconv.Itoa(level)},
	})
}

// AdminUnplanLevel removes the queued level at position order from a team's
// planned queue.
func (c *Client) AdminUnplanLevel(ctx context.Context, gameID, teamID, order int) error {
	return c.monitorPost(ctx, gameID, url.Values{
		"action": {"del_Line"}, "team": {strconv.Itoa(teamID)}, "op": {strconv.Itoa(order)},
	})
}

// AdminAddCorrection grants bonus (kind "bonus") or penalty (kind
// "penalty") minutes to a team with a comment shown in the results.
func (c *Client) AdminAddCorrection(ctx context.Context, gameID, teamID int, kind string, minutes int, comment string) error {
	// gmstatPost.php stores mins = -1 * type * abs(mins): type 1 gives a bonus
	// (negative minutes reduce the result), type -1 a penalty.
	var typ string
	switch strings.ToLower(kind) {
	case "bonus", "бонус":
		typ = "1"
	case "penalty", "штраф":
		typ = "-1"
	default:
		return fmt.Errorf("dzzzr: admin correction: kind must be \"bonus\" or \"penalty\", got %q", kind)
	}
	if minutes < 0 {
		minutes = -minutes
	}
	return c.monitorPost(ctx, gameID, url.Values{
		"action": {"bonus"}, "team[]": {strconv.Itoa(teamID)}, "game": {strconv.Itoa(gameID)},
		"type": {typ}, "mins": {strconv.Itoa(minutes)}, "comment": {comment},
	})
}

// AdminDeleteCorrections removes a team's bonus (kind "bonus") or penalty
// (kind "penalty") corrections in a game.
func (c *Client) AdminDeleteCorrections(ctx context.Context, gameID, teamID int, kind string) error {
	var typ string
	switch strings.ToLower(kind) {
	case "bonus", "бонус":
		typ = "1"
	case "penalty", "штраф":
		typ = "-1"
	default:
		return fmt.Errorf("dzzzr: admin delete corrections: kind must be \"bonus\" or \"penalty\", got %q", kind)
	}
	return c.monitorPost(ctx, gameID, url.Values{
		"action": {"delBonus"}, "team": {strconv.Itoa(teamID)}, "game": {strconv.Itoa(gameID)}, "type": {typ},
	})
}

// AdminBlockTeam stops a team's game (the engine answers its requests with
// "игра приостановлена").
func (c *Client) AdminBlockTeam(ctx context.Context, gameID, teamID int) error {
	return c.monitorPost(ctx, gameID, url.Values{"action": {"stop"}, "team[]": {strconv.Itoa(teamID)}})
}

// AdminUnblockTeam resumes a blocked team.
func (c *Client) AdminUnblockTeam(ctx context.Context, gameID, teamID int) error {
	return c.monitorPost(ctx, gameID, url.Values{"action": {"start"}, "team": {strconv.Itoa(teamID)}})
}

// AdminToggleEngine flips the engine between running and stopped for every
// team of the project.
//
// The engine offers no way to set the state: admin/gmstatPost.php's switchRobot
// case reads no parameter at all, it deletes the botStoped option when it is
// set and inserts it otherwise. Callers that know which state they want
// should use AdminSetEngine, which reads the current one first.
func (c *Client) AdminToggleEngine(ctx context.Context, gameID int) error {
	return c.monitorPost(ctx, gameID, url.Values{"action": {"switchRobot"}})
}

// AdminSetEngine brings the engine to the requested state, doing nothing when
// it is already there.
//
// Because the engine only offers a toggle, this reads the live game screen
// first. Two organizers pressing the switch at the same time can still race,
// and the state is per project rather than per game, so the caller is
// changing it for every game the project is running.
func (c *Client) AdminSetEngine(ctx context.Context, gameID int, running bool) error {
	st, err := c.AdminMonitor(ctx, gameID)
	if err != nil {
		return err
	}
	if st.EngineStopped == !running {
		return nil
	}
	return c.AdminToggleEngine(ctx, gameID)
}

// AdminFinishGame closes the game: clears planned levels and marks it
// finished so results can be published.
func (c *Client) AdminFinishGame(ctx context.Context, gameID int) error {
	return c.monitorPost(ctx, gameID, url.Values{"action": {"finishGame"}, "game": {strconv.Itoa(gameID)}})
}

// AdminSetEventTime moves the recorded time of a level event: event
// LogEventLevelIssued for when the level was issued, LogEventCodeAccepted
// for the last accepted code. at is "YYYY-MM-DD HH:MM:SS".
func (c *Client) AdminSetEventTime(ctx context.Context, gameID, teamID, level, event int, at string) error {
	return c.monitorPost(ctx, gameID, url.Values{
		"action": {"chtime"}, "team": {strconv.Itoa(teamID)}, "level": {strconv.Itoa(level)},
		"event": {strconv.Itoa(event)}, "time": {at},
	})
}
