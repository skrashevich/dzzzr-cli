package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// breakDuration is how long a scheduled break pauses the team, as in go2.php.
const breakDuration = 15 * time.Minute

// wrongTriesBeforeBlock is how many wrong codes a team may send on one level
// before the engine stops accepting data from it for wrongCodeBlock.
const (
	wrongTriesBeforeBlock = 4
	wrongCodeBlock        = 3 * time.Minute
)

// spoilerDef is a hidden part of a level task unlocked by its own code.
type spoilerDef struct {
	Order   int
	Text    string
	Code    string
	Penalty int
}

// CityTeamDef is one entry of the city's team directory.
type CityTeamDef struct {
	ID   int
	Name string
}

// playerDef is one member of the team's roster, as the organizer's team card
// lists it: the engine ticks the box for a player who counts as on the team.
type playerDef struct {
	Login  string
	OnTeam bool
}

// levelDef is one level of the demo game. Field names follow the columns of
// the engine's zadanie table.
type levelDef struct {
	// ID is the level's database id, stable across reordering: the engine
	// keeps it when a level moves, only the order changes.
	ID            int
	Order         int
	Title         string
	Question      string
	Codes         []string
	Dangers       []string
	Sectors       []string
	SectorNames   []string
	BonusCodes    []string
	BonusDangers  []string
	BonusTimes    []int
	FakeCodes     []string
	FakePenalties []int
	// CodeCount is how many main codes finish the level; 0 means all of them.
	CodeCount int
	Clue1     string
	Clue2     string
	ClueMin   int
	ClueMin2  int
	ClueMin3  int
	Spoilers  []spoilerDef
	Bonus     bool
	// ShowAsBonus makes the level report isBonusLevel without moving it off
	// the main line. The engine keeps the two apart — live level 12 was both
	// сквозное and бонусное while still being the current task — whereas
	// Bonus here also does the routing, so a quirk cannot set it without
	// dropping the level out of mainLine and breaking itself.
	ShowAsBonus  bool
	BonusTime    int
	Skvoz        bool
	SkvozMin     int
	NoMasterCode bool
	NoBreak      bool
	TryLimit     int

	LocationComment string
	Lat             string
	Lon             string
	Radius          string
}

// neededCodes returns how many main codes the level needs to be finished.
func (l *levelDef) neededCodes() int {
	if l.CodeCount > 0 {
		return l.CodeCount
	}
	return len(l.Codes)
}

// timeLimit is the whole span a team may spend on the level: the three hint
// intervals in a row.
func (l *levelDef) timeLimit() time.Duration {
	return time.Duration(l.ClueMin+l.ClueMin2+l.ClueMin3) * time.Minute
}

// levelProgress is what one team did on one issued level.
type levelProgress struct {
	Order        int
	Number       int // position in the main line as the team sees it
	IssuedAt     time.Time
	Codes        map[string]bool
	BonusCodes   map[string]bool
	FakeCodes    map[string]bool
	Spoilers     map[int]bool
	Hint1Early   time.Time
	Hint2Early   time.Time
	Hint1Taken   bool
	Hint2Taken   bool
	Wrong        int
	BlockedUntil time.Time
	NextRequest  bool
	MasterCode   bool
	CompletedAt  time.Time
}

func newLevelProgress(order, number int, at time.Time) *levelProgress {
	return &levelProgress{
		Order:      order,
		Number:     number,
		IssuedAt:   at,
		Codes:      map[string]bool{},
		BonusCodes: map[string]bool{},
		FakeCodes:  map[string]bool{},
		Spoilers:   map[int]bool{},
	}
}

// orgMessage is a message shown in the game interface. Team 0 goes to every
// team, a positive team to that team only, a negative team is what the team
// itself sent to the organizer.
type orgMessage struct {
	Time time.Time
	Team int
	Text string
	User string
	// Important marks the message the organizer flagged as urgent. The engine
	// stores it and never renders it, so it can be written but not read back.
	Important bool
	// ShowAfter delays a message; the admin chat prints it in its own column.
	ShowAfter time.Time
}

// chatMessage is one entry of the team chat (API/messages.php).
type chatMessage struct {
	Time    time.Time
	Who     string
	Content string
	Ava     string
}

// logEntry is one line of the team's game log (API/game.php?log=true).
type logEntry struct {
	Time  time.Time
	Level int
	Event string
	Data  string
	User  string
}

// gameState is the whole in-memory world the mock serves: one city, one game,
// one team. It is deterministic: newGameState always builds the same game and
// only the clock moves.
type gameState struct {
	GameID   int
	GameName string
	Number   string
	Greeting string
	Legend   string
	Start    time.Time
	Season   int
	Authors  string

	Finished      bool
	EngineStopped bool
	Blocked       bool

	MasterCode        string
	MasterCodePenalty int
	ClueBeforePenalty int
	BonusAfter        int

	Levels []*levelDef

	TeamID   int
	TeamName string
	Captain  string
	Pin      string
	// Roster is the team's member list as the organizer's card shows it.
	Roster []playerDef
	// CityTeams is the directory the engine offers in its "add a team to
	// this game" picker: every team of the city, not just this game's.
	CityTeams []CityTeamDef

	// Issued lists the main levels in the order the team received them;
	// Current is the order number of the level being played, 0 when none.
	Issued   []int
	Current  int
	Progress map[int]*levelProgress

	// Status is the team's application status and RatingPoints its score;
	// both are what the organizer edits on the teams page.
	Status       int
	RatingPoints int

	Breaks       int
	Abandons     int
	BreakPending int // level after which a scheduled break starts, 0 when none
	BreakUntil   time.Time
	PendingNext  bool

	MasterUsed bool
	PenaltyMin int
	BonusMin   int

	OrgMessages []orgMessage
	Chat        []chatMessage
	Log         []logEntry
}

// newGameState builds the fixed demo game: three main levels, one bonus
// (skvoz) level running alongside them, and one team that received the first
// level when the game started an hour ago.
func newGameState(now time.Time) *gameState {
	start := now.Add(-time.Hour)
	g := &gameState{
		GameID:   4242,
		GameName: "Тестовая игра",
		Number:   "1",
		Greeting: "Поздравляем, вы прошли все уровни тестовой игры!",
		Legend:   "<p>Легенда тестовой игры.</p>",
		Start:    start,
		Season:   1,
		Authors:  "Штаб",

		MasterCode: "MASTER",
		// Bonus codes must be found before the last main one; the next level
		// is handed over as soon as the main codes are in.
		BonusAfter:        -1,
		MasterCodePenalty: 30,
		ClueBeforePenalty: 10,

		TeamID:   5150,
		TeamName: "MockTeam",
		Captain:  "demo",
		Pin:      "1234",
		CityTeams: []CityTeamDef{
			{ID: 5150, Name: "MockTeam"},
			{ID: 5151, Name: "Другая команда"},
			{ID: 5152, Name: "Третья команда"},
		},
		Roster: []playerDef{
			{Login: "demo", OnTeam: true},
			{Login: "sidekick", OnTeam: true},
			{Login: "former", OnTeam: false},
		},

		Progress: map[int]*levelProgress{},
	}

	g.Levels = []*levelDef{
		{
			Order: 1,
			Title: "Первый уровень",
			// A real task routinely puts the code in a picture and links out
			// of the game; a client that shows only the text shows a task it
			// cannot solve.
			Question: `<p>Найдите три кода во дворе</p>` +
				`<p><img src="../../uploaded/moscow/Night/dvor.jpg" alt="" /></p>` +
				`<p>Смотрите <a href="https://t.me/questDozoR">канал</a></p>`,
			Codes:         []string{"1", "2", "3"},
			Dangers:       []string{"1", "1", "2"},
			Sectors:       []string{"1", "1", "2"},
			SectorNames:   []string{"Гаражи", "Пустырь"},
			BonusCodes:    []string{"B1"},
			BonusDangers:  []string{"1"},
			BonusTimes:    []int{10},
			FakeCodes:     []string{"FAKE"},
			FakePenalties: []int{10},
			Clue1:         "Подсказка 1 к уровню 1",
			Clue2:         "Подсказка 2 к уровню 1",
			ClueMin:       30,
			ClueMin2:      30,
			ClueMin3:      30,
			Spoilers: []spoilerDef{
				{Order: 1, Text: "Скрытая подсказка", Code: "SP1", Penalty: 5},
			},
			LocationComment: "Тихий двор, не шумите",
		},
		{
			Order:       2,
			Title:       "Второй уровень",
			Question:    "Найдите любой из двух кодов",
			Codes:       []string{"4", "5"},
			Dangers:     []string{"1", "1"},
			Sectors:     []string{"1", "1"},
			SectorNames: []string{"Двор"},
			CodeCount:   1,
			Clue1:       "Подсказка 1 к уровню 2",
			Clue2:       "Подсказка 2 к уровню 2",
			ClueMin:     30,
			ClueMin2:    30,
			ClueMin3:    30,
			// The organizer forbade the universal code on this level.
			NoMasterCode: true,
		},
		{
			Order:       3,
			Title:       "Третий уровень",
			Question:    "Последний код игры",
			Codes:       []string{"6"},
			Dangers:     []string{"3"},
			Sectors:     []string{"1"},
			SectorNames: []string{"Финиш"},
			Clue1:       "Подсказка 1 к уровню 3",
			Clue2:       "Подсказка 2 к уровню 3",
			ClueMin:     30,
			ClueMin2:    30,
			ClueMin3:    30,
		},
		{
			Order:       4,
			Title:       "Сквозной бонус",
			Question:    "Сквозное бонусное задание: найдите памятник",
			Codes:       []string{"SK1"},
			Dangers:     []string{"1"},
			Sectors:     []string{""},
			SectorNames: nil,
			Clue1:       "Подсказка к сквозному заданию",
			ClueMin:     30,
			ClueMin2:    30,
			ClueMin3:    30,
			Bonus:       true,
			BonusTime:   15,
			Skvoz:       true,
			Lat:         "55.75",
			Lon:         "37.61",
			Radius:      "300",
		},
	}

	for _, l := range g.Levels {
		l.ID = levelIDOf(l.Order)
	}

	// The team received the first main level and the skvoz level at the start.
	g.issueLevel(1, start)
	g.Progress[4] = newLevelProgress(4, 0, start)

	g.OrgMessages = []orgMessage{
		{Time: start.Add(time.Minute), Team: 0, Text: "Всем командам: движок работает"},
		{Time: start.Add(5 * time.Minute), Team: g.TeamID, Text: "Вам начислен бонус 5 мин"},
	}
	g.Chat = []chatMessage{
		{Time: start.Add(time.Minute), Who: "организатор", Content: "Всем удачи!", Ava: "0"},
		{Time: start.Add(6 * time.Minute), Who: "организатор", Content: "Не забывайте про бонусы", Ava: "0"},
	}
	return g
}

// level returns the definition of the level with the given order number.
func (g *gameState) level(order int) *levelDef {
	for _, l := range g.Levels {
		if l.Order == order {
			return l
		}
	}
	return nil
}

// mainLine returns the ordinary levels in play order.
func (g *gameState) mainLine() []*levelDef {
	out := make([]*levelDef, 0, len(g.Levels))
	for _, l := range g.Levels {
		if !l.Bonus {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

// skvozLevels returns the bonus levels that run alongside the main line.
func (g *gameState) skvozLevels() []*levelDef {
	out := make([]*levelDef, 0, 1)
	for _, l := range g.Levels {
		if l.Bonus {
			out = append(out, l)
		}
	}
	return out
}

// issueLevel hands the level to the team and writes the log line the engine
// writes (event 1).
func (g *gameState) issueLevel(order int, at time.Time) {
	g.Issued = append(g.Issued, order)
	g.Progress[order] = newLevelProgress(order, len(g.Issued), at)
	g.Current = order
	g.addLog(at, fmt.Sprintf("выдан уровень %d", order), strconv.Itoa(order), "")
}

// addLog appends one line to the team's game log.
func (g *gameState) addLog(at time.Time, event, data, user string) {
	// Events belong to the level being played, which is what the organizer's
	// live log shows next to them. The time is truncated to a second because
	// the engine stores it in a MySQL DATETIME column, and the organizer's log
	// poll compares against exactly that resolution.
	g.Log = append(g.Log, logEntry{Time: at.Truncate(time.Second), Level: g.Current, Event: event, Data: data, User: user})
}

// current returns the level and progress the team is working on, or nil when
// the team has no level (finished, on a break, or waiting for the organizer).
func (g *gameState) current() (*levelDef, *levelProgress) {
	if g.Current == 0 {
		return nil, nil
	}
	return g.level(g.Current), g.Progress[g.Current]
}

// skvoz returns the bonus level running alongside the main line.
func (g *gameState) skvoz() (*levelDef, *levelProgress) {
	for _, l := range g.skvozLevels() {
		if p := g.Progress[l.Order]; p != nil {
			return l, p
		}
	}
	return nil, nil
}

// tick advances time-driven state: a finished break releases the pending
// level, and the next level is issued once the team may play again.
func (g *gameState) tick(now time.Time) {
	if g.Finished {
		return
	}
	if !g.BreakUntil.IsZero() && !now.Before(g.BreakUntil) {
		g.BreakUntil = time.Time{}
	}
	if g.PendingNext && g.BreakUntil.IsZero() {
		g.PendingNext = false
		g.advance(now)
	}
}

// onBreak reports whether the team's game is paused right now.
func (g *gameState) onBreak(now time.Time) bool {
	return !g.BreakUntil.IsZero() && now.Before(g.BreakUntil)
}

// nextMainOrder returns the order number of the level that follows the last
// issued one, or 0 when the main line is over.
func (g *gameState) nextMainOrder() int {
	issued := map[int]bool{}
	for _, o := range g.Issued {
		issued[o] = true
	}
	for _, l := range g.mainLine() {
		if !issued[l.Order] {
			return l.Order
		}
	}
	return 0
}

// advance issues the next main level, or finishes the game when the line is
// over. A scheduled break delays the hand-over.
func (g *gameState) advance(now time.Time) {
	next := g.nextMainOrder()
	if next == 0 {
		g.Current = 0
		g.Finished = true
		g.addLog(now, "игра завершена", "", "")
		return
	}
	if g.BreakPending != 0 && g.BreakPending == g.Current {
		g.BreakPending = 0
		g.BreakUntil = now.Add(breakDuration)
		g.PendingNext = true
		g.Current = 0
		g.addLog(now, "начался перерыв", "", "")
		return
	}
	if g.onBreak(now) {
		g.PendingNext = true
		g.Current = 0
		return
	}
	g.issueLevel(next, now)
}

// codesFound counts the accepted main codes of a level.
func (p *levelProgress) codesFound() int {
	n := len(p.Codes)
	if p.MasterCode {
		n++
	}
	return n
}

// mainDone reports whether the team found enough main codes to finish the
// level.
func (g *gameState) mainDone(l *levelDef, p *levelProgress) bool {
	return p.codesFound() >= l.neededCodes()
}

// bonusPending reports whether the level is held open because bonus codes are
// still missing (the engine's isLevelFinished() == -1).
func (g *gameState) bonusPending(l *levelDef, p *levelProgress) bool {
	if !g.mainDone(l, p) || len(l.BonusCodes) == 0 {
		return false
	}
	if len(p.BonusCodes) >= len(l.BonusCodes) {
		return false
	}
	if g.BonusAfter == -1 {
		return false
	}
	return !p.NextRequest
}

// expired reports whether the whole time allowed for the level has run out.
func (g *gameState) expired(l *levelDef, p *levelProgress, now time.Time) bool {
	if l.Skvoz {
		return false
	}
	limit := p.IssuedAt.Add(l.timeLimit())
	if !p.Hint1Early.IsZero() {
		limit = p.Hint1Early.Add(time.Duration(l.ClueMin2+l.ClueMin3) * time.Minute)
	}
	if !p.Hint2Early.IsZero() {
		limit = p.Hint2Early.Add(time.Duration(l.ClueMin3) * time.Minute)
	}
	return now.After(limit)
}

// hints returns the hint texts the team may see right now.
func (g *gameState) hints(l *levelDef, p *levelProgress, now time.Time) (string, string) {
	elapsed := now.Sub(p.IssuedAt)
	var h1, h2 string
	if p.Hint1Taken || !p.Hint1Early.IsZero() || elapsed >= time.Duration(l.ClueMin)*time.Minute {
		h1 = l.Clue1
	}
	second := time.Duration(l.ClueMin+l.ClueMin2) * time.Minute
	if !p.Hint1Early.IsZero() {
		second = p.Hint1Early.Add(time.Duration(l.ClueMin2) * time.Minute).Sub(p.IssuedAt)
	}
	if p.Hint2Taken || !p.Hint2Early.IsZero() || elapsed >= second {
		h2 = l.Clue2
	}
	if h1 == "" {
		h2 = ""
	}
	return h1, h2
}

// timerSeconds returns the seconds left until the next hint or, once both
// hints are out, until the level ends.
func (g *gameState) timerSeconds(l *levelDef, p *levelProgress, now time.Time) int {
	elapsed := int(now.Sub(p.IssuedAt) / time.Second)
	marks := []int{l.ClueMin * 60, (l.ClueMin + l.ClueMin2) * 60, (l.ClueMin + l.ClueMin2 + l.ClueMin3) * 60}
	for _, m := range marks {
		if m-elapsed > 0 {
			return m - elapsed
		}
	}
	return 0
}

// koline renders the difficulty coefficients the way the engine prints them,
// marking the codes the team already found.
func (g *gameState) koline(l *levelDef, p *levelProgress) string {
	type group struct {
		sector string
		parts  []string
	}
	var groups []*group
	index := map[string]*group{}
	for i, code := range l.Codes {
		sector := ""
		if i < len(l.Sectors) {
			sector = l.Sectors[i]
		}
		danger := ""
		if i < len(l.Dangers) {
			danger = l.Dangers[i]
		}
		gr, ok := index[sector]
		if !ok {
			gr = &group{sector: sector}
			index[sector] = gr
			groups = append(groups, gr)
		}
		if p.Codes[normalizeCode(code)] {
			danger = "<span style='color:red'>" + danger + "</span>"
		}
		gr.parts = append(gr.parts, danger)
	}
	var b strings.Builder
	for _, gr := range groups {
		if gr.sector != "" {
			name := gr.sector
			if n, err := strconv.Atoi(gr.sector); err == nil && n >= 1 && n <= len(l.SectorNames) {
				name = l.SectorNames[n-1]
			}
			b.WriteString("Сектор " + name + ": ")
		}
		b.WriteString("основные коды: " + strings.Join(gr.parts, ", ") + "; ")
	}
	if len(l.BonusCodes) > 0 {
		var parts []string
		for i, code := range l.BonusCodes {
			danger := ""
			if i < len(l.BonusDangers) {
				danger = l.BonusDangers[i]
			}
			if p.BonusCodes[normalizeCode(code)] {
				danger = "<span style='color:red'>" + danger + "</span>"
			}
			parts = append(parts, danger)
		}
		b.WriteString("бонусные коды: " + strings.Join(parts, ", ") + "; ")
	}
	return strings.TrimSuffix(b.String(), " ") + "<br>"
}

// htmlTag matches the markup strip_tags removes from a submitted code.
var htmlTag = regexp.MustCompile(`<[^>]*>`)

// normalizeCode brings a code to the form the engine compares: no surrounding
// space, no markup, upper case, no inner spaces, ё folded to е.
func normalizeCode(s string) string {
	s = strings.TrimSpace(s)
	s = htmlTag.ReplaceAllString(s, "")
	s = strings.NewReplacer("<", "", ">", "", "=", "", "'", "", `"`, "", "#", "", "|", "").Replace(s)
	s = strings.ToUpper(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "Ё", "Е")
	return s
}

// matchCode returns the primary form of the matching entry of codes, or an
// empty string. Entries may list synonyms separated by "#", as in the engine.
func matchCode(entered string, codes []string) string {
	e := normalizeCode(entered)
	if e == "" {
		return ""
	}
	for _, c := range codes {
		variants := strings.Split(c, "#")
		for _, v := range variants {
			if normalizeCode(v) == e {
				return normalizeCode(variants[0])
			}
		}
	}
	return ""
}

// bonusMinutesFor returns the bonus minutes attached to a bonus code.
func (l *levelDef) bonusMinutesFor(primary string) int {
	for i, c := range l.BonusCodes {
		if normalizeCode(strings.Split(c, "#")[0]) == primary && i < len(l.BonusTimes) {
			return l.BonusTimes[i]
		}
	}
	return 0
}

// fakePenaltyFor returns the penalty minutes attached to a fake code.
func (l *levelDef) fakePenaltyFor(primary string) int {
	for i, c := range l.FakeCodes {
		if normalizeCode(strings.Split(c, "#")[0]) == primary && i < len(l.FakePenalties) {
			return l.FakePenalties[i]
		}
	}
	return 0
}

// levelDuration is how long the team spent on a level, or is spending now.
func (g *gameState) levelDuration(p *levelProgress, now time.Time) time.Duration {
	end := now
	if !p.CompletedAt.IsZero() {
		end = p.CompletedAt
	}
	d := end.Sub(p.IssuedAt)
	if d < 0 {
		return 0
	}
	return d
}

// formatClock renders a duration as the engine's HH:MM:SS.
func formatClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, (total/60)%60, total%60)
}

// formatTime renders a moment as the engine's "YYYY-MM-DD HH:MM:SS".
func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

var russianMonths = [...]string{
	"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

// formatDay renders a date the way the engine's printDate does.
func formatDay(t time.Time) string {
	return fmt.Sprintf("%d %s %d г.", t.Day(), russianMonths[int(t.Month())-1], t.Year())
}
