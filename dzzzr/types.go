package dzzzr

import (
	"encoding/json"
	"html"
	"regexp"
	"slices"
	"strings"
)

// GameState is the reply of go/?api=true: the team's view of the current
// game. Field names follow templates/JSON/go.tpl of the engine.
type GameState struct {
	GameName          string     `json:"gameName"`
	Greeting          string     `json:"greeting"`
	LastOrgMessage    string     `json:"lastOrgMessage"`
	GameID            FlexInt    `json:"gameId"`
	TeamName          string     `json:"teamName"`
	GameStartTime     string     `json:"gameStartTime"`  // current server clock (HH:MM) while the game has not started
	GameStartOnDay    string     `json:"gameStartOnDay"` // "8 сентября 2026 г." while the game has not started
	GameStartOnTime   string     `json:"gameStartOnTime"`
	HoldTime          string     `json:"holdTime"` // when the organizer put the team on hold
	Finished          FlexBool   `json:"finished"`
	Countdown         FlexInt    `json:"countdown"` // seconds left on a break
	OnBreak           string     `json:"onBreak"`   // when the break ends
	CurrentTime       string     `json:"currentTime"`
	Legend            string     `json:"legend"`
	CanControl        FlexBool   `json:"canControl"` // captain, chief of staff or helper with canGame
	TotalLevels       FlexInt    `json:"totallevels"`
	TryLimitError     string     `json:"tryLimitError"`
	BlockedError      string     `json:"blockedError"`
	NextLevelSelector string     `json:"NextLevelSelector"`
	Level             *Level     `json:"level"`
	BonusLevels       []Level    `json:"bonusLevels"`
	Messages          []Message  `json:"messages"`
	BonusSkvozLevels  FlexString `json:"bonusSkvozLevels"`
	ErrNo             FlexInt    `json:"errNo"`
	ErrText           string     `json:"errText"`
}

// plainGameState is GameState without its custom unmarshaler.
type plainGameState GameState

// gameStateWire mirrors GameState but keeps level, bonusLevels and messages
// raw so the empty "{ }" placeholders the template emits can be dropped.
type gameStateWire struct {
	plainGameState
	Level       json.RawMessage `json:"level"`
	BonusLevels json.RawMessage `json:"bonusLevels"`
	Messages    json.RawMessage `json:"messages"`
}

// UnmarshalJSON implements json.Unmarshaler. The template emits "{ }" for a
// missing level and a trailing "{ }" in bonusLevels; both become absent.
func (g *GameState) UnmarshalJSON(b []byte) error {
	var w gameStateWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*g = GameState(w.plainGameState)
	g.Level = nil
	g.BonusLevels = nil
	g.Messages = nil
	if len(w.Level) > 0 {
		var l Level
		if err := json.Unmarshal(w.Level, &l); err != nil {
			return err
		}
		if !l.empty() {
			g.Level = &l
		}
	}
	if len(w.BonusLevels) > 0 {
		var ls []Level
		if err := json.Unmarshal(w.BonusLevels, &ls); err != nil {
			return err
		}
		for _, l := range ls {
			if !l.empty() {
				g.BonusLevels = append(g.BonusLevels, l)
			}
		}
	}
	if len(w.Messages) > 0 {
		var ms []Message
		if err := json.Unmarshal(w.Messages, &ms); err != nil {
			return err
		}
		for _, m := range ms {
			if m.Text != "" || m.Timestamp != "" {
				g.Messages = append(g.Messages, m)
			}
		}
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (g GameState) MarshalJSON() ([]byte, error) {
	return json.Marshal(plainGameState(g))
}

// GameStarted reports whether the engine is issuing levels, i.e. the reply
// does not describe a countdown to the start.
func (g *GameState) GameStarted() bool {
	return g != nil && g.GameStartOnDay == "" && g.GameStartOnTime == ""
}

// Level is one issued level, main or bonus (skvoz). Field names follow
// templates/JSON/go_level.tpl.
type Level struct {
	IsLevelFinished   FlexBool  `json:"isLevelFinished"`
	LevelNumber       FlexInt   `json:"levelNumber"`
	IsBonusLevel      FlexBool  `json:"isBonusLevel"`
	BonusLevelTime    FlexInt   `json:"bonusLevelTime"` // bonus minutes for solving it
	CodesFounded      FlexInt   `json:"codesFounded"`
	TotalCodes        FlexInt   `json:"totalCodes"`
	NeededCodes       FlexInt   `json:"neededCodes"` // 0 means all
	BonusCodesFounded FlexInt   `json:"bonusCodesFounded"`
	BonusCodesTotal   FlexInt   `json:"bonusCodesTotal"`
	Skvoz             FlexBool  `json:"skvoz"` // level runs alongside the main line
	BonusCodesTimes   []string  `json:"bonusCodesTimes"`
	BonusAfter        FlexInt   `json:"bonusAfter"`
	NoMasterCode      FlexBool  `json:"noMasterCode"`
	IsSabotage        FlexBool  `json:"isSabotage"`
	TryLimit          FlexInt   `json:"tryLimit"`
	TryLimitUsed      FlexInt   `json:"tryLimitUsed"`
	KOLine            string    `json:"koline"` // difficulty coefficients as rendered by the engine
	Question          string    `json:"question"`
	LocationComment   string    `json:"locationComment"`
	Spoilers          []Spoiler `json:"spoilers"`
	KOInSpoiler       FlexBool  `json:"koinspoiler"`
	Hint1             string    `json:"hint1"`
	Hint2             string    `json:"hint2"`
	LocationLat       string    `json:"locationLat"`
	LocationLon       string    `json:"locationLon"`
	LocationRadius    string    `json:"locationRadius"`
	TimeOnLevel       string    `json:"timeOnLevel"`
	TM                FlexInt   `json:"tm"` // seconds until the next event (hint or level end)
	TakeBreak         FlexBool  `json:"takeBreak"`
}

func (l *Level) empty() bool {
	return l.LevelNumber == 0 && l.Question == "" && l.TotalCodes == 0 && l.Hint1 == "" && l.KOLine == ""
}

// Spoiler is a hidden part of a level task unlocked by its own code.
type Spoiler struct {
	Text    string   `json:"spoilerText"`
	Solved  FlexBool `json:"spoilerSolved"`
	Penalty FlexInt  `json:"spoilerPenalty"`
	Number  FlexInt  `json:"spoilerNumber"`
}

// Message is an organizer or staff message shown in the game interface.
type Message struct {
	Timestamp   string `json:"timestamp"`   // HH:MM:SS
	Destination string `json:"destination"` // "всем", "команде" or "от команды"
	Text        string `json:"text"`
}

// ActionResult is the engine's answer to an action: the numeric result code
// from the redirect and its wording.
type ActionResult struct {
	Err  int    `json:"err"`
	Text string `json:"text"`
	// Penalty is the minutes the engine charged for the action, taken from
	// the redirect's bcs parameter. It is set for code 42 (an early hint).
	Penalty  int    `json:"penalty,omitzero"`
	Location string `json:"location,omitempty"`
}

// Accepted reports whether the engine accepted the code or performed the
// action.
func (r *ActionResult) Accepted() bool { return r != nil && IsAcceptedCode(r.Err) }

// TeamStat is the team's per-level statistics table (API/game.php?stat=true).
// Cells are HTML fragments in the engine's own layout; Clean strips tags.
type TeamStat struct {
	LevelNames []string `json:"lnames"`
	Cells      []string `json:"tds"`
	Time       string   `json:"teamstatTime"`
}

// Rows pairs level names with their cells, stripping HTML.
func (s *TeamStat) Rows() [][2]string {
	if s == nil {
		return nil
	}
	n := len(s.LevelNames)
	if len(s.Cells) < n {
		n = len(s.Cells)
	}
	out := make([][2]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, [2]string{StripHTML(s.LevelNames[i]), StripHTML(s.Cells[i])})
	}
	return out
}

// LogEntry is one line of the team's game log (API/game.php?log=true). The
// engine wraps values in table cells; Clean returns them as text.
type LogEntry struct {
	Time  string `json:"time"`
	Event string `json:"event"`
	Data  string `json:"data"`
	User  string `json:"user"`
}

// Clean returns a copy with HTML removed from every field.
func (e LogEntry) Clean() LogEntry {
	return LogEntry{Time: StripHTML(e.Time), Event: StripHTML(e.Event), Data: StripHTML(e.Data), User: StripHTML(e.User)}
}

// Danger is one code slot of a level: its difficulty coefficient, whether
// the team already found it, and its sector.
type Danger struct {
	Danger  FlexString `json:"danger"`
	Founded FlexBool   `json:"founded"`
	Sector  FlexString `json:"sector"`
}

// LevelInfo is the bot-oriented view of the current level from
// API/game.php?level=1. Level and Title are HTML; the structured fields are
// what a client should rely on.
type LevelInfo struct {
	Level             string            `json:"level"`
	Title             string            `json:"title"`
	SpoilerText       map[string]string `json:"-"`
	Spoiler           FlexBool          `json:"spoiler"`
	RestartAfterBreak FlexBool          `json:"restartAfterBreake"`
	BeforeClue1       FlexInt           `json:"beforeClue1"`
	BeforeClue2       FlexInt           `json:"beforeClue2"`
	NextLevel         FlexInt           `json:"nextLevel"`
	CanBreak          FlexInt           `json:"canBreake"`
	CanAbandon        FlexInt           `json:"canAbandon"`
	TakeBreak         FlexBool          `json:"takeBreak"`
	Timer             FlexInt           `json:"timer"`
	SelectLevel       string            `json:"selectLevel"`
	Dangers           []Danger          `json:"-"`
	Sectors           []string          `json:"-"`
	DangersB          []Danger          `json:"-"`
	Bonus             []string          `json:"-"`
	HasFakeCodes      FlexBool          `json:"has_fake_codes"`
	IsSabotage        FlexBool          `json:"isSabotage"`
	Number            FlexInt           `json:"number"`
	NoMasterCode      FlexBool          `json:"noMasterCode"`
	NoBreak           FlexBool          `json:"noBreak"`
	Hold              FlexBool          `json:"hold"`
	Clue1             string            `json:"clue1"`
	Clue2             string            `json:"clue2"`
	CodeCount         FlexInt           `json:"codeCount"`
	Legend            string            `json:"legend"`
	BreakStop         FlexBool          `json:"breakStop"`
	Skvoz             []SkvozLevel      `json:"skvoz"`
}

// SkvozLevel is a bonus level entry inside LevelInfo.
type SkvozLevel struct {
	ID      FlexInt  `json:"id"`
	Title   string   `json:"title"`
	Level   string   `json:"level"`
	Spoiler FlexBool `json:"spoiler"`
}

// UnmarshalJSON implements json.Unmarshaler. PHP emits sparse arrays as JSON
// objects keyed by index, so the list-like fields are decoded through a
// helper that accepts both forms.
func (l *LevelInfo) UnmarshalJSON(b []byte) error {
	type plain LevelInfo
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var raw struct {
		SpoilerText json.RawMessage `json:"spoilerText"`
		Dangers     json.RawMessage `json:"dangers"`
		Sectors     json.RawMessage `json:"sectors"`
		DangersB    json.RawMessage `json:"dangersB"`
		Bonus       json.RawMessage `json:"bonus"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*l = LevelInfo(p)
	var err error
	if l.Dangers, err = decodeSparseList[Danger](raw.Dangers); err != nil {
		return err
	}
	if l.DangersB, err = decodeSparseList[Danger](raw.DangersB); err != nil {
		return err
	}
	if l.Sectors, err = decodeSparseStrings(raw.Sectors); err != nil {
		return err
	}
	if l.Bonus, err = decodeSparseStrings(raw.Bonus); err != nil {
		return err
	}
	if len(raw.SpoilerText) > 0 && raw.SpoilerText[0] == '{' {
		_ = json.Unmarshal(raw.SpoilerText, &l.SpoilerText)
	}
	return nil
}

// decodeSparseList reads either a JSON array or a JSON object whose keys are
// numeric indexes (what PHP's json_encode does for arrays with holes).
func decodeSparseList[T any](raw json.RawMessage) ([]T, error) {
	raw = trimRaw(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` || string(raw) == "false" {
		return nil, nil
	}
	if raw[0] == '[' {
		var out []T
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	if raw[0] == '{' {
		var m map[string]T
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sortNumeric(keys)
		out := make([]T, 0, len(m))
		for _, k := range keys {
			out = append(out, m[k])
		}
		return out, nil
	}
	return nil, nil
}

func decodeSparseStrings(raw json.RawMessage) ([]string, error) {
	items, err := decodeSparseList[FlexString](raw)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, s := range items {
		out = append(out, string(s))
	}
	return out, nil
}

func trimRaw(raw json.RawMessage) json.RawMessage {
	return json.RawMessage(strings.TrimSpace(string(raw)))
}

func sortNumeric(keys []string) {
	slices.SortFunc(keys, func(a, b string) int { return parseLooseInt(a) - parseLooseInt(b) })
}

// ChatMessage is one entry of API/messages.php: organizer messages, staff
// notes and the team's own posts, oldest first.
type ChatMessage struct {
	Ava     FlexString `json:"ava"`
	Who     string     `json:"who"`
	Time    string     `json:"time"`
	Content string     `json:"content"`
}

// GameInfo describes a game announced in the city (API/gamesList.php).
type GameInfo struct {
	ID                 FlexInt    `json:"id"`
	Project            string     `json:"project"`
	Number             FlexString `json:"number"`
	Name               string     `json:"name"`
	League             FlexInt    `json:"league"`
	Date               string     `json:"date"`
	Authors            string     `json:"authors"`
	Territory          string     `json:"territory"`
	AdditionalTools    string     `json:"additionalTools"`
	Legend             string     `json:"legend"`
	Information        string     `json:"information"`
	Image              *string    `json:"image"`
	Season             FlexInt    `json:"season"`
	Start              string     `json:"start"`
	IsPrequelAvailable FlexBool   `json:"isPrequelAvailable"`
	IsLeague           FlexBool   `json:"isLeague"`
	IsLinear           FlexBool   `json:"isLinear"`
	Teams              []string   `json:"teams"`
}

// GamesListOptions filters GetGamesList.
type GamesListOptions struct {
	NewOnly bool   // games whose date is in the future
	Archive bool   // finished games instead of upcoming ones
	After   string // only games dated after this "YYYY-MM-DD HH:MM:SS"
	// Limit is the page size; the engine defaults to 50 and silently
	// truncates, so a city with a long archive needs paging. Offset skips
	// that many games. GetGamesList pages through everything by itself when
	// both are zero.
	Limit  int
	Offset int
}

var (
	tagRe   = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>|<[^>]*>`)
	brRe    = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</tr>`)
	spaceRe = regexp.MustCompile(`[ \t]+`)
	nlRe    = regexp.MustCompile(`\n{3,}`)
)

// StripHTML converts an HTML fragment to plain text: line breaks are kept,
// tags removed, entities decoded, whitespace collapsed.
//
// Control characters go too. Every fragment it is given — a task, a hint, a
// spoiler, an organizer's message — is written by someone else and printed to
// a terminal, which would act on an escape sequence rather than show it.
func StripHTML(s string) string {
	if s == "" {
		return ""
	}
	s = brRe.ReplaceAllString(s, "\n")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = stripControls(s, "\n\t")
	s = strings.ReplaceAll(s, " ", " ")
	s = spaceRe.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	s = strings.Join(lines, "\n")
	s = nlRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// stripControls removes the C0 and C1 control characters from text that came
// off the wire. A level's HTML is written by the organizer, and its text and
// links are printed straight to a terminal, which would act on an escape
// sequence rather than show it. keep names the ones to leave alone: StripHTML
// builds its layout out of newlines and tabs, while a URL must lose even
// those — a tab inside "java\tscript:" would otherwise slip past the scheme
// check.
func stripControls(s, keep string) string {
	drop := func(r rune) bool { return isControl(r) && !strings.ContainsRune(keep, r) }
	if strings.IndexFunc(s, drop) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		if drop(r) {
			return -1
		}
		return r
	}, s)
}

// isControl reports whether a rune is a C0 or C1 control character.
func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}
