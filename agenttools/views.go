package agenttools

import (
	"strconv"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// The engine's payloads carry HTML and repeat data the model does not need.
// These projections keep a tool result small and readable: text instead of
// markup, counters instead of raw strings, and no field whose only use is
// rendering the site's own page.

type gameView struct {
	GameName    string      `json:"game_name"`
	GameID      int         `json:"game_id"`
	TeamName    string      `json:"team_name"`
	CurrentTime string      `json:"current_time"`
	Started     bool        `json:"started"`
	StartsOn    string      `json:"starts_on,omitempty"`
	Finished    bool        `json:"finished"`
	OnHoldSince string      `json:"on_hold_since,omitempty"`
	BreakUntil  string      `json:"break_until,omitempty"`
	CanControl  bool        `json:"can_control"`
	TotalLevels int         `json:"total_levels"`
	Level       *levelView  `json:"level,omitempty"`
	BonusLevels []levelView `json:"bonus_levels,omitempty"`
	Messages    []msgView   `json:"messages,omitempty"`
	LastResult  string      `json:"last_result,omitempty"`
	Notice      string      `json:"notice,omitempty"`
}

type levelView struct {
	Number            int           `json:"number"`
	Bonus             bool          `json:"bonus,omitempty"`
	Skvoz             bool          `json:"skvoz,omitempty"`
	BonusMinutes      int           `json:"bonus_minutes,omitempty"`
	Question          string        `json:"question"`
	LocationComment   string        `json:"location_comment,omitempty"`
	Difficulty        string        `json:"difficulty_line,omitempty"`
	CodesFound        int           `json:"codes_found"`
	CodesTotal        int           `json:"codes_total"`
	CodesNeeded       int           `json:"codes_needed,omitempty"`
	BonusCodesFound   int           `json:"bonus_codes_found,omitempty"`
	BonusCodesTotal   int           `json:"bonus_codes_total,omitempty"`
	Hint1             string        `json:"hint1,omitempty"`
	Hint2             string        `json:"hint2,omitempty"`
	SecondsToNext     int           `json:"seconds_to_next_event,omitempty"`
	TimeOnLevel       string        `json:"time_on_level,omitempty"`
	Sabotage          bool          `json:"sabotage,omitempty"`
	MasterCodeBlocked bool          `json:"master_code_blocked,omitempty"`
	TryLimit          int           `json:"try_limit,omitempty"`
	TryLimitUsed      int           `json:"try_limit_used,omitempty"`
	Spoilers          []spoilerView `json:"spoilers,omitempty"`
}

type spoilerView struct {
	Number  int    `json:"number"`
	Solved  bool   `json:"solved"`
	Penalty int    `json:"penalty_minutes,omitempty"`
	Text    string `json:"text,omitempty"`
}

type msgView struct {
	Time string `json:"time"`
	To   string `json:"to,omitempty"`
	Text string `json:"text"`
}

func newGameView(st *dzzzr.GameState) gameView {
	v := gameView{
		GameName: st.GameName, GameID: st.GameID.Int(), TeamName: st.TeamName,
		CurrentTime: st.CurrentTime, Started: st.GameStarted(), Finished: st.Finished.Bool(),
		OnHoldSince: st.HoldTime, BreakUntil: st.OnBreak, CanControl: st.CanControl.Bool(),
		TotalLevels: st.TotalLevels.Int(),
	}
	if !v.Started {
		v.StartsOn = strings.TrimSpace(st.GameStartOnDay + " " + st.GameStartOnTime)
	}
	if st.Level != nil {
		lv := newLevelView(*st.Level)
		v.Level = &lv
	}
	for _, bl := range st.BonusLevels {
		v.BonusLevels = append(v.BonusLevels, newLevelView(bl))
	}
	for _, m := range st.Messages {
		v.Messages = append(v.Messages, msgView{Time: m.Timestamp, To: m.Destination, Text: dzzzr.StripHTML(m.Text)})
	}
	if st.ErrNo != 0 {
		v.LastResult = dzzzr.ErrText(st.ErrNo.Int())
	}
	switch {
	case st.BlockedError != "":
		v.Notice = dzzzr.StripHTML(st.BlockedError)
	case st.TryLimitError != "":
		v.Notice = dzzzr.StripHTML(st.TryLimitError)
	}
	return v
}

func newLevelView(l dzzzr.Level) levelView {
	v := levelView{
		Number: l.LevelNumber.Int(), Bonus: l.IsBonusLevel.Bool(), Skvoz: l.Skvoz.Bool(),
		BonusMinutes: l.BonusLevelTime.Int(),
		Question:     dzzzr.StripHTML(l.Question), LocationComment: dzzzr.StripHTML(l.LocationComment),
		Difficulty: dzzzr.StripHTML(l.KOLine),
		CodesFound: l.CodesFounded.Int(), CodesTotal: l.TotalCodes.Int(), CodesNeeded: l.NeededCodes.Int(),
		BonusCodesFound: l.BonusCodesFounded.Int(), BonusCodesTotal: l.BonusCodesTotal.Int(),
		Hint1: dzzzr.StripHTML(l.Hint1), Hint2: dzzzr.StripHTML(l.Hint2),
		SecondsToNext: l.TM.Int(), TimeOnLevel: l.TimeOnLevel,
		Sabotage: l.IsSabotage.Bool(), MasterCodeBlocked: l.NoMasterCode.Bool(),
		TryLimit: l.TryLimit.Int(), TryLimitUsed: l.TryLimitUsed.Int(),
	}
	for _, s := range l.Spoilers {
		v.Spoilers = append(v.Spoilers, spoilerView{
			Number: s.Number.Int(), Solved: s.Solved.Bool(), Penalty: s.Penalty.Int(), Text: dzzzr.StripHTML(s.Text),
		})
	}
	return v
}

type codeView struct {
	Difficulty string `json:"difficulty"`
	Found      bool   `json:"found"`
	Sector     string `json:"sector,omitempty"`
}

type levelInfoView struct {
	Number       int        `json:"number"`
	Codes        []codeView `json:"codes,omitempty"`
	BonusCodes   []codeView `json:"bonus_codes,omitempty"`
	Sectors      []string   `json:"sectors,omitempty"`
	BonusMinutes []string   `json:"bonus_minutes,omitempty"`
	CodesNeeded  int        `json:"codes_needed,omitempty"`
	Hint1        string     `json:"hint1,omitempty"`
	Hint2        string     `json:"hint2,omitempty"`
	SecondsLeft  int        `json:"seconds_to_next_event,omitempty"`
	CanAbandon   bool       `json:"can_abandon"`
	CanBreak     bool       `json:"can_break"`
	OnHold       bool       `json:"on_hold,omitempty"`
	Sabotage     bool       `json:"sabotage,omitempty"`
	HasFakeCodes bool       `json:"has_fake_codes,omitempty"`
	EarlyHint1   bool       `json:"early_hint1_available,omitempty"`
	EarlyHint2   int        `json:"early_hint2_penalty_minutes,omitempty"`
}

func newLevelInfoView(info *dzzzr.LevelInfo) levelInfoView {
	v := levelInfoView{
		Number: info.Number.Int(), Sectors: info.Sectors, BonusMinutes: info.Bonus,
		CodesNeeded: info.CodeCount.Int(), Hint1: dzzzr.StripHTML(info.Clue1), Hint2: dzzzr.StripHTML(info.Clue2),
		SecondsLeft: info.Timer.Int(), CanAbandon: info.CanAbandon.Int() > 0, CanBreak: info.CanBreak.Int() > 0,
		OnHold: info.Hold.Bool(), Sabotage: info.IsSabotage.Bool(), HasFakeCodes: info.HasFakeCodes.Bool(),
		EarlyHint1: info.BeforeClue1.Int() > 0, EarlyHint2: info.BeforeClue2.Int(),
	}
	for _, d := range info.Dangers {
		v.Codes = append(v.Codes, codeView{Difficulty: d.Danger.String(), Found: d.Founded.Bool(), Sector: d.Sector.String()})
	}
	for _, d := range info.DangersB {
		v.BonusCodes = append(v.BonusCodes, codeView{Difficulty: d.Danger.String(), Found: d.Founded.Bool()})
	}
	return v
}

type statView struct {
	Time  string        `json:"time"`
	Rows  [][2]string   `json:"rows"`
	Extra []dzzzr.Level `json:"-"`
}

type logView struct {
	Time  string `json:"time"`
	Event string `json:"event"`
	Data  string `json:"data,omitempty"`
	User  string `json:"user,omitempty"`
}

func newLogViews(entries []dzzzr.LogEntry) []logView {
	out := make([]logView, 0, len(entries))
	for _, e := range entries {
		c := e.Clean()
		out = append(out, logView{Time: c.Time, Event: c.Event, Data: c.Data, User: c.User})
	}
	return out
}

type chatView struct {
	Time string `json:"time"`
	Who  string `json:"who"`
	Text string `json:"text"`
}

func newChatViews(msgs []dzzzr.ChatMessage) []chatView {
	out := make([]chatView, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, chatView{Time: m.Time, Who: m.Who, Text: dzzzr.StripHTML(m.Content)})
	}
	return out
}

// gameListView is an index entry. Announcements, start descriptions and team
// rosters can be very large and do not belong in a list sent to the model.
type gameListView struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Number string `json:"number"`
	Date   string `json:"date"`
	Linear bool   `json:"linear"`
}

func newGameListViews(games []dzzzr.GameInfo) []gameListView {
	out := make([]gameListView, 0, len(games))
	for _, g := range games {
		out = append(out, gameListView{
			ID: g.ID.Int(), Name: dzzzr.StripHTML(g.Name), Number: g.Number.String(), Date: g.Date,
			Linear: g.IsLinear.Bool(),
		})
	}
	return out
}

// teamArchiveResultView is an archive index entry for one team: the standing
// travels with the game because it is what the roster filter had to read
// anyway.
type teamArchiveResultView struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Number string `json:"number"`
	Date   string `json:"date"`
	Place  string `json:"place"`
	Gap    string `json:"gap"`
}

// archiveStatTeamView keys a team's results by column name, which a model can
// read without counting positions.
type archiveStatTeamView struct {
	ID      int               `json:"id"`
	Name    string            `json:"name"`
	Results map[string]string `json:"results"`
	// Notes explains the penalty and bonus minutes: which level each came from.
	Notes map[string]string `json:"notes,omitempty"`
}

type archiveStatView struct {
	GameID       int                   `json:"game_id"`
	Name         string                `json:"name"`
	Date         string                `json:"date"`
	LevelColumns []string              `json:"level_columns,omitempty"`
	TotalColumns []string              `json:"total_columns,omitempty"`
	Teams        []archiveStatTeamView `json:"teams"`
}

func newArchiveStatView(st *dzzzr.ArchiveStat) archiveStatView {
	v := archiveStatView{GameID: st.GameID, Name: st.Name, Date: st.Date}
	// Two levels of a game may carry the same name, and a map would keep only
	// one of them, so the titles are made unique before they become keys.
	titles := make([]string, len(st.Columns))
	used := map[string]int{}
	for i, c := range st.Columns {
		title := c.Title
		if n := used[c.Title]; n > 0 {
			title = c.Title + " (" + strconv.Itoa(n+1) + ")"
		}
		used[c.Title]++
		titles[i] = title
		if c.Level {
			v.LevelColumns = append(v.LevelColumns, title)
		} else {
			v.TotalColumns = append(v.TotalColumns, title)
		}
	}
	for _, t := range st.Teams {
		team := archiveStatTeamView{ID: t.ID, Name: t.Name, Results: map[string]string{}}
		for i, cell := range t.Cells {
			if i < len(titles) {
				team.Results[titles[i]] = cell
			}
		}
		if len(t.Notes) > 0 {
			team.Notes = t.Notes
		}
		v.Teams = append(v.Teams, team)
	}
	return v
}

type actionView struct {
	Err      int    `json:"err"`
	Text     string `json:"text"`
	Accepted bool   `json:"accepted"`
	// PenaltyMinutes is what the engine charged, which it reports only in the
	// redirect beside the code.
	PenaltyMinutes int `json:"penalty_minutes,omitzero"`
}

func newActionView(r *dzzzr.ActionResult) actionView {
	return actionView{Err: r.Err, Text: r.Text, Accepted: r.Accepted(), PenaltyMinutes: r.Penalty}
}
