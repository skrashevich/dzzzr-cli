package dzzzr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// AdminLevel is one row of a game's level list.
type AdminLevel struct {
	ID          int      `json:"id"`
	Order       int      `json:"order"` // order_p, the level number players see
	Title       string   `json:"title"`
	Kind        string   `json:"kind"` // "основной", "бонусный", "запасной"
	Skvoz       bool     `json:"skvoz"`
	Published   bool     `json:"published"`
	Codes       []string `json:"codes"`       // main codes as listed
	BonusCodes  []string `json:"bonus_codes"` // "CODE (N мин.)"
	FakeCodes   []string `json:"fake_codes"`
	CodeCount   string   `json:"code_count"` // "все" or a number
	TryLimit    string   `json:"try_limit"`
	HintTimings string   `json:"hint_timings"` // "30:30:30" when the level overrides the game's
	Selected    bool     `json:"selected,omitempty"`
}

// LevelCode is one code slot of a level.
type LevelCode struct {
	Code     string `json:"code"`
	Synonyms string `json:"synonyms,omitempty"` // alternative spellings, '#'-separated on the wire
	Danger   string `json:"danger"`             // difficulty: "1", "1+", "2", "2+", "3", "3+", "null" or ""
	Sector   int    `json:"sector,omitempty"`   // 1-based sector number, 0 = none
}

// BonusCode is a bonus code with the minutes it earns.
type BonusCode struct {
	Code     string `json:"code"`
	Synonyms string `json:"synonyms,omitempty"`
	Danger   string `json:"danger"`
	Minutes  int    `json:"minutes"`
}

// FakeCode is a decoy code with its penalty in minutes.
type FakeCode struct {
	Code     string `json:"code"`
	Synonyms string `json:"synonyms,omitempty"`
	Penalty  int    `json:"penalty"`
}

// SpoilerParams is a hidden part of the task unlocked by a code.
type SpoilerParams struct {
	Text     string `json:"text"`
	Code     string `json:"code"`
	Synonyms string `json:"synonyms,omitempty"`
	Penalty  int    `json:"penalty,omitempty"`
}

// LevelParams are the editable fields of a level. On update, zero values
// keep the current value except for the slices, which replace the current
// list when non-nil (an empty non-nil slice clears it). Bool pointers force
// a checkbox off.
type LevelParams struct {
	Title           string          `json:"title,omitempty"`
	Subtitle        string          `json:"subtitle,omitempty"`
	Question        string          `json:"question,omitempty"`
	Hint1           string          `json:"hint1,omitempty"`
	Hint2           string          `json:"hint2,omitempty"`
	Location        string          `json:"location,omitempty"`
	LocationComment string          `json:"location_comment,omitempty"`
	Comment         *string         `json:"comment,omitempty"`            // organizer's commentary and code photos; pointer permits clearing
	TimeAddBonusAll *int            `json:"time_add_bonus_all,omitempty"` // extra minutes for collecting every bonus code
	Codes           []LevelCode     `json:"codes,omitempty"`
	SectorNames     []string        `json:"sector_names,omitempty"`
	BonusCodes      []BonusCode     `json:"bonus_codes,omitempty"`
	FakeCodes       []FakeCode      `json:"fake_codes,omitempty"`
	Spoilers        []SpoilerParams `json:"spoilers,omitempty"`
	ClueMin         int             `json:"clue_min,omitempty"`
	ClueMin2        int             `json:"clue_min2,omitempty"`
	ClueMin3        int             `json:"clue_min3,omitempty"`
	Hint1OnRequest  *bool           `json:"hint1_on_request,omitempty"`
	Hint1Interval   int             `json:"hint1_interval,omitempty"`
	Hint1Penalty    int             `json:"hint1_penalty,omitempty"`
	Hint2OnRequest  *bool           `json:"hint2_on_request,omitempty"`
	Hint2Interval   int             `json:"hint2_interval,omitempty"`
	Hint2Penalty    int             `json:"hint2_penalty,omitempty"`
	CodeCount       int             `json:"code_count,omitempty"` // codes needed to pass, 0 = all
	TryLimit        int             `json:"try_limit,omitempty"`
	Penalty         *int            `json:"penalty,omitempty"` // minutes for an unfinished level; nil keeps the game's
	NoMasterCode    *bool           `json:"no_master_code,omitempty"`
	IsSabotage      *bool           `json:"is_sabotage,omitempty"`
	Bonus           *bool           `json:"bonus,omitempty"`
	BonusTime       int             `json:"bonus_time,omitempty"`
	Skvoz           *bool           `json:"skvoz,omitempty"`
	SkvozMin        int             `json:"skvoz_min,omitempty"`
	SkvozStart      string          `json:"skvoz_start,omitempty"`
	Lat             string          `json:"lat,omitempty"`
	Lon             string          `json:"lon,omitempty"`
	Radius          string          `json:"radius,omitempty"`
	ShowCoordHint2  *bool           `json:"show_coord_with_hint2,omitempty"`
	NoBreak         *bool           `json:"no_break,omitempty"`
	ClueBefore      *bool           `json:"clue_before,omitempty"`
	Reserve         *bool           `json:"reserve,omitempty"` // zapas
	Publish         *bool           `json:"publish,omitempty"`
	Greeting        string          `json:"greeting,omitempty"`
	// Clear names text parameters to blank explicitly, by the same names the
	// fields above carry in JSON; see GameParams.Clear for the precedence
	// rule. greeting is not among them: the level form has no such field.
	Clear []string `json:"clear,omitempty"`
}

// AdminLevelInfo is a level's full form.
type AdminLevelInfo struct {
	ID     int         `json:"id"`
	GameID int         `json:"game_id"`
	Order  int         `json:"order"`
	Params LevelParams `json:"params"`
	Fields url.Values  `json:"fields"`
}

var indexedRe = regexp.MustCompile(`^(\w+)\[(\d+)\]$`)

// AdminListLevels returns the levels of a game in order.
func (c *Client) AdminListLevels(ctx context.Context, gameID int) ([]AdminLevel, error) {
	doc, err := c.adminPage(ctx, "zadanie", url.Values{"categoryValue": {strconv.Itoa(gameID)}})
	if err != nil {
		return nil, err
	}
	selectedID, selectedOrder := 0, 0
	selectedPublished := true
	if f := doc.formWithAction("update_zadanie"); f != nil {
		selectedID, _ = strconv.Atoi(f.Fields.Get("id"))
		// The list prints "0." for the level open in the editor, but the
		// editor's own form states the real position, and AdminGetLevel
		// already reads a level's order from exactly this field.
		selectedOrder, _ = strconv.Atoi(f.Fields.Get("order_p"))
		if v, ok := f.Checkboxes["publish"]; ok {
			selectedPublished = v
		}
	}
	rows := doc.rows()
	idx, _ := headerIndex(rows)
	var out []AdminLevel
	for _, row := range rows {
		if l, ok := parseLevelRow(row, idx, selectedID); ok {
			if l.Selected {
				l.Published = selectedPublished
				if l.Order == 0 {
					l.Order = selectedOrder
				}
			}
			out = append(out, l)
		}
	}
	// A position the editor's form did not state is read off the neighbours
	// instead. Measured against classic.dzzzr.ru on 2026-09-09: the list
	// prints "0." for the level open in the editor, and before this the
	// level was dropped from the list altogether.
	for i := range out {
		if out[i].Order != 0 {
			continue
		}
		switch {
		case i > 0:
			out[i].Order = out[i-1].Order + 1
		case len(out) > 1 && out[1].Order > 1:
			out[i].Order = out[1].Order - 1
		default:
			out[i].Order = 1
		}
	}
	return out, nil
}

var hintTimingsRe = regexp.MustCompile(`^\d+:\d+:\d+$`)

// parseLevelRow reads one row of the level list. Columns are found by their
// header ("Номер", "Название уровня", "Коды", "Нужно найти", "Попытки
// ввода"), because the engine pads rows with spacer cells and leaves values
// empty, so counting cells would drift.
func parseLevelRow(row htmlRow, idx map[string]int, selectedID int) (AdminLevel, bool) {
	if len(row.Cells) < 5 {
		return AdminLevel{}, false
	}
	var l AdminLevel
	if link, q, ok := row.linkWithParams("action", "zadanie", "edit", "1"); ok && q.Get("id") != "" {
		l.ID, _ = strconv.Atoi(q.Get("id"))
		l.Title = link.Text
	} else if row.selected() && selectedID != 0 {
		l.ID = selectedID
		l.Selected = true
	} else {
		return AdminLevel{}, false
	}
	if m := leadNumRe.FindStringSubmatch(row.cellByHeader(idx, "номер")); m != nil {
		l.Order, _ = strconv.Atoi(m[1])
	}
	if l.Order == 0 {
		for _, cell := range row.Cells {
			if m := leadNumRe.FindStringSubmatch(cell); m != nil {
				l.Order, _ = strconv.Atoi(m[1])
				break
			}
		}
	}
	// A level whose id is known stays in the list even without a number: the
	// engine prints "0." for the level currently open in the editor, and
	// dropping it silently hid one level of the game from every caller.
	// AdminListLevels fills the position in from the neighbours. A row with
	// neither id nor number is not a level row at all.
	if l.Order == 0 && l.ID == 0 {
		return AdminLevel{}, false
	}
	l.CodeCount = row.cellByHeader(idx, "нужно")
	l.TryLimit = row.cellByHeader(idx, "попытки", "попыток")
	if t := row.cellByHeader(idx, "интервалы"); hintTimingsRe.MatchString(t) {
		l.HintTimings = t
	}
	l.Codes, l.BonusCodes, l.FakeCodes = splitCodeCell(row.cellByHeader(idx, "коды"))
	for _, cell := range row.Cells {
		cell = strings.TrimSpace(cell)
		switch {
		case strings.HasPrefix(cell, "основной"):
			l.Kind = "основной"
		case strings.HasPrefix(cell, "бонусный"):
			l.Kind = "бонусный"
			l.Skvoz = strings.Contains(cell, "сквозной")
		case strings.HasPrefix(cell, "запасной"):
			l.Kind = "запасной"
		}
		if l.HintTimings == "" && hintTimingsRe.MatchString(cell) {
			l.HintTimings = cell
		}
	}
	if l.Title == "" {
		if t := row.cellByHeader(idx, "название"); t != "" {
			l.Title = firstLine(t)
		}
	}
	l.Published = !row.dimmed()
	return l, true
}

// splitCodeCell reads the codes column. The engine renders it as a <select>
// whose options are the main codes, then "---бонусные", "---ложные" and
// "---мастер-код" separators; the cell text keeps one option per line.
func splitCodeCell(cell string) (codes, bonus, fake []string) {
	if strings.TrimSpace(cell) == "" {
		return nil, nil, nil
	}
	section := "main"
	for _, line := range strings.Split(cell, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "---бонусные"):
			section = "bonus"
		case strings.HasPrefix(line, "---ложные"):
			section = "fake"
		case strings.HasPrefix(line, "---мастер"):
			section = "master"
		case strings.HasPrefix(line, "индивидуальные"):
			section = "skip"
		default:
			switch section {
			case "main":
				codes = append(codes, line)
			case "bonus":
				bonus = append(bonus, line)
			case "fake":
				fake = append(fake, line)
			}
		}
	}
	return codes, bonus, fake
}

// AdminGetLevel returns a level's full form.
func (c *Client) AdminGetLevel(ctx context.Context, gameID, levelID int) (*AdminLevelInfo, error) {
	doc, err := c.adminPage(ctx, "zadanie", url.Values{"categoryValue": {strconv.Itoa(gameID)}, "id": {strconv.Itoa(levelID)}})
	if err != nil {
		return nil, err
	}
	f := doc.formWithAction("update_zadanie")
	if f == nil {
		return nil, &UndecodableResponseError{StatusCode: http.StatusOK, Context: "admin level form", Err: stringError("no update_zadanie form; wrong level id or no access")}
	}
	info := &AdminLevelInfo{Fields: f.Fields}
	info.ID, _ = strconv.Atoi(f.Fields.Get("id"))
	info.GameID, _ = strconv.Atoi(f.Fields.Get("categoryValue"))
	info.Order, _ = strconv.Atoi(f.Fields.Get("order_p"))
	info.Params = levelParamsFromForm(f)
	return info, nil
}

// indexedValues collects name[i] fields into an index-ordered slice.
func indexedValues(fields url.Values, name string) map[int]string {
	out := map[int]string{}
	for k, vs := range fields {
		m := indexedRe.FindStringSubmatch(k)
		if m == nil || m[1] != name || len(vs) == 0 {
			continue
		}
		i, _ := strconv.Atoi(m[2])
		out[i] = vs[0]
	}
	return out
}

func sortedKeys(m map[int]string) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func levelParamsFromForm(f *htmlForm) LevelParams {
	get := func(k string) string { return strings.TrimSpace(f.Fields.Get(k)) }
	atoi := func(k string) int { return parseLooseInt(get(k)) }
	boolp := func(k string) *bool {
		if _, ok := f.Checkboxes[k]; !ok {
			return nil
		}
		v := f.Checkboxes[k]
		return &v
	}
	p := LevelParams{
		Title: get("title"), Subtitle: get("subtitle"), Question: get("question"),
		Hint1: get("clue1"), Hint2: get("clue2"), Location: get("location"), LocationComment: get("locationComment"),
		ClueMin: atoi("ClueMin"), ClueMin2: atoi("ClueMin2"), ClueMin3: atoi("ClueMin3"),
		Hint1OnRequest: boolp("zapros1"), Hint1Interval: atoi("interval1"), Hint1Penalty: atoi("shtraf1"),
		Hint2OnRequest: boolp("zapros2"), Hint2Interval: atoi("interval2"), Hint2Penalty: atoi("shtraf2"),
		CodeCount: atoi("codeCount"), TryLimit: atoi("tryLimit"),
		NoMasterCode: boolp("noMasterCode"), IsSabotage: boolp("isSabotage"), Bonus: boolp("bonus"), BonusTime: atoi("bonusTime"),
		Skvoz: boolp("skvoz"), SkvozMin: atoi("skvozMin"), SkvozStart: get("skvozStart"),
		Lat: get("lat"), Lon: get("lon"), Radius: get("radius"), ShowCoordHint2: boolp("showCoordWith2ndClue"),
		NoBreak: boolp("nobreak"), ClueBefore: boolp("clueBefore"), Reserve: boolp("zapas"), Publish: boolp("publish"),
		Greeting: get("greeting"),
	}
	if v := get("penalty"); v != "" {
		n := parseLooseInt(v)
		p.Penalty = &n
	}
	if f.Fields.Has("comment") {
		p.Comment = new(get("comment"))
	}
	if f.Fields.Has("timeAddBonusAll") {
		p.TimeAddBonusAll = new(atoi("timeAddBonusAll"))
	}
	codes := indexedValues(f.Fields, "code")
	syn := indexedValues(f.Fields, "codeS")
	dangers := indexedValues(f.Fields, "danger")
	sectors := indexedValues(f.Fields, "sector")
	for _, i := range sortedKeys(codes) {
		if strings.TrimSpace(codes[i]) == "" {
			continue
		}
		p.Codes = append(p.Codes, LevelCode{Code: codes[i], Synonyms: syn[i], Danger: dangers[i], Sector: parseLooseInt(sectors[i])})
	}
	// The engine numbers sector names from one; a zero index would be a page
	// this parser has never seen, and must not take the process down.
	names := indexedValues(f.Fields, "secName")
	for _, i := range sortedKeys(names) {
		if i <= 0 || strings.TrimSpace(names[i]) == "" {
			continue
		}
		for len(p.SectorNames) < i {
			p.SectorNames = append(p.SectorNames, "")
		}
		p.SectorNames[i-1] = names[i]
	}
	bcodes := indexedValues(f.Fields, "codeB")
	bsyn := indexedValues(f.Fields, "codeBS")
	bdanger := indexedValues(f.Fields, "dangerB")
	btime := indexedValues(f.Fields, "timeB")
	for _, i := range sortedKeys(bcodes) {
		if strings.TrimSpace(bcodes[i]) == "" {
			continue
		}
		p.BonusCodes = append(p.BonusCodes, BonusCode{Code: bcodes[i], Synonyms: bsyn[i], Danger: bdanger[i], Minutes: parseLooseInt(btime[i])})
	}
	fcodes := indexedValues(f.Fields, "codeF")
	fsyn := indexedValues(f.Fields, "codeFS")
	fpen := indexedValues(f.Fields, "fakeShtraf")
	for _, i := range sortedKeys(fcodes) {
		if strings.TrimSpace(fcodes[i]) == "" {
			continue
		}
		p.FakeCodes = append(p.FakeCodes, FakeCode{Code: fcodes[i], Synonyms: fsyn[i], Penalty: parseLooseInt(fpen[i])})
	}
	sp := indexedValues(f.Fields, "spoiler")
	spc := indexedValues(f.Fields, "spoilerCode")
	sps := indexedValues(f.Fields, "spoilerSynonyms")
	spp := indexedValues(f.Fields, "spoilerPenalty")
	for _, i := range sortedKeys(sp) {
		if strings.TrimSpace(sp[i]) == "" && strings.TrimSpace(spc[i]) == "" {
			continue
		}
		p.Spoilers = append(p.Spoilers, SpoilerParams{Text: sp[i], Code: spc[i], Synonyms: sps[i], Penalty: parseLooseInt(spp[i])})
	}
	return p
}

// deleteIndexed removes every name[i] field.
func deleteIndexed(form url.Values, names ...string) {
	for k := range form {
		if m := indexedRe.FindStringSubmatch(k); m != nil {
			for _, n := range names {
				if m[1] == n {
					delete(form, k)
					break
				}
			}
		}
	}
}

// applyLevelParams writes p into the form values.
// levelTextFields maps a parameter name to the engine's own form field, so a
// caller can blank one without knowing the engine's spelling. greeting is
// absent on purpose: the level form has no such field at all.
var levelTextFields = map[string]string{
	"title": "title", "subtitle": "subtitle", "question": "question",
	"hint1": "clue1", "hint2": "clue2", "location": "location",
	"location_comment": "locationComment", "skvoz_start": "skvozStart",
	"lat": "lat", "lon": "lon", "radius": "radius", "comment": "comment",
}

func applyLevelParams(form url.Values, p LevelParams) {
	set := func(k, v string) {
		if v != "" {
			form.Set(k, v)
		}
	}
	seti := func(k string, v int) {
		if v != 0 {
			form.Set(k, strconv.Itoa(v))
		}
	}
	setb := func(k string, v *bool) {
		if v == nil {
			return
		}
		if *v {
			form.Set(k, "on")
		} else {
			form.Del(k)
		}
	}
	set("title", p.Title)
	set("subtitle", p.Subtitle)
	set("question", p.Question)
	set("clue1", p.Hint1)
	set("clue2", p.Hint2)
	set("location", p.Location)
	set("locationComment", p.LocationComment)
	if p.Comment != nil {
		form.Set("comment", *p.Comment)
	}
	if p.TimeAddBonusAll != nil {
		form.Set("timeAddBonusAll", strconv.Itoa(*p.TimeAddBonusAll))
	}
	seti("ClueMin", p.ClueMin)
	seti("ClueMin2", p.ClueMin2)
	seti("ClueMin3", p.ClueMin3)
	setb("zapros1", p.Hint1OnRequest)
	seti("interval1", p.Hint1Interval)
	seti("shtraf1", p.Hint1Penalty)
	setb("zapros2", p.Hint2OnRequest)
	seti("interval2", p.Hint2Interval)
	seti("shtraf2", p.Hint2Penalty)
	seti("codeCount", p.CodeCount)
	seti("tryLimit", p.TryLimit)
	if p.Penalty != nil {
		form.Set("penalty", strconv.Itoa(*p.Penalty))
	}
	setb("noMasterCode", p.NoMasterCode)
	setb("isSabotage", p.IsSabotage)
	setb("bonus", p.Bonus)
	seti("bonusTime", p.BonusTime)
	setb("skvoz", p.Skvoz)
	seti("skvozMin", p.SkvozMin)
	set("skvozStart", p.SkvozStart)
	set("lat", p.Lat)
	set("lon", p.Lon)
	set("radius", p.Radius)
	setb("showCoordWith2ndClue", p.ShowCoordHint2)
	setb("nobreak", p.NoBreak)
	setb("clueBefore", p.ClueBefore)
	setb("zapas", p.Reserve)
	setb("publish", p.Publish)
	set("greeting", p.Greeting)
	if p.Codes != nil {
		deleteIndexed(form, "code", "codeS", "danger", "sector")
		for i, cd := range p.Codes {
			idx := strconv.Itoa(i)
			form.Set("code["+idx+"]", cd.Code)
			form.Set("codeS["+idx+"]", cd.Synonyms)
			form.Set("danger["+idx+"]", cd.Danger)
			if cd.Sector > 0 {
				form.Set("sector["+idx+"]", strconv.Itoa(cd.Sector))
			} else {
				form.Set("sector["+idx+"]", "")
			}
		}
	}
	if p.SectorNames != nil {
		deleteIndexed(form, "secName")
		for i, n := range p.SectorNames {
			form.Set("secName["+strconv.Itoa(i+1)+"]", n)
		}
	}
	if p.BonusCodes != nil {
		deleteIndexed(form, "codeB", "codeBS", "dangerB", "timeB")
		for i, b := range p.BonusCodes {
			idx := strconv.Itoa(i)
			form.Set("codeB["+idx+"]", b.Code)
			form.Set("codeBS["+idx+"]", b.Synonyms)
			d := b.Danger
			if d == "" {
				d = "1"
			}
			form.Set("dangerB["+idx+"]", d)
			form.Set("timeB["+idx+"]", strconv.Itoa(b.Minutes))
		}
	}
	if p.FakeCodes != nil {
		deleteIndexed(form, "codeF", "codeFS", "fakeShtraf")
		for i, fc := range p.FakeCodes {
			idx := strconv.Itoa(i)
			form.Set("codeF["+idx+"]", fc.Code)
			form.Set("codeFS["+idx+"]", fc.Synonyms)
			form.Set("fakeShtraf["+idx+"]", strconv.Itoa(fc.Penalty))
		}
	}
	if p.Spoilers != nil {
		deleteIndexed(form, "spoiler", "spoilerCode", "spoilerSynonyms", "spoilerPenalty")
		for i, s := range p.Spoilers {
			idx := strconv.Itoa(i + 1)
			form.Set("spoiler["+idx+"]", s.Text)
			form.Set("spoilerCode["+idx+"]", s.Code)
			form.Set("spoilerSynonyms["+idx+"]", s.Synonyms)
			form.Set("spoilerPenalty["+idx+"]", strconv.Itoa(s.Penalty))
		}
	}
	clearFields(form, p.Clear, levelTextFields)
}

// AdminCreateLevel adds a level to a game and returns its id. The engine
// publishes a level only when title, question, both hints and at least one
// code with a difficulty are present; otherwise it is saved unpublished.
func (c *Client) AdminCreateLevel(ctx context.Context, gameID int, p LevelParams) (int, error) {
	if p.Title == "" && p.Question == "" {
		return 0, fmt.Errorf("dzzzr: admin create level: title or question is required")
	}
	gid := strconv.Itoa(gameID)
	form := url.Values{
		"action": {"add_zadanie"}, "Project": {""}, "ofset": {"0"}, "id": {""},
		"categoryValue": {gid}, "category": {gid}, "goto": {"1"}, "gZone": {"0"},
	}
	for _, cd := range p.Codes {
		if cd.Danger == "" {
			return 0, fmt.Errorf("dzzzr: admin create level: code %q needs a difficulty (danger), e.g. \"1\"", cd.Code)
		}
	}
	applyLevelParams(form, p)
	if p.Publish == nil {
		form.Set("publish", "on")
	}
	loc, page, err := c.adminPost(ctx, adminPath, form)
	if err != nil {
		return 0, err
	}
	if id, ok := idFromLocation(loc); ok {
		return id, nil
	}
	if page != "" {
		if doc, err := parseHTML(page); err == nil {
			if f := doc.formWithAction("update_zadanie"); f != nil {
				if id, err := strconv.Atoi(f.Fields.Get("id")); err == nil {
					return id, nil
				}
			}
		}
	}
	// add_zadanie redirects to "addnew=1" (no id) when goto=0; with goto=1 it
	// lands on the list where the new level is selected.
	if strings.Contains(loc, "action=zadanie") {
		levels, err := c.AdminListLevels(ctx, gameID)
		if err == nil && len(levels) > 0 {
			return levels[len(levels)-1].ID, nil
		}
	}
	return 0, &UndecodableResponseError{StatusCode: http.StatusFound, Context: "admin create level", Err: stringError("no level id in redirect"), Body: loc}
}

// AdminUpdateLevel reads the level's form, overlays p and submits it.
func (c *Client) AdminUpdateLevel(ctx context.Context, gameID, levelID int, p LevelParams) error {
	info, err := c.AdminGetLevel(ctx, gameID, levelID)
	if err != nil {
		return err
	}
	form := cloneValues(info.Fields)
	form.Set("action", "update_zadanie")
	form.Set("id", strconv.Itoa(levelID))
	form.Set("categoryValue", strconv.Itoa(gameID))
	form.Set("category", strconv.Itoa(gameID))
	applyLevelParams(form, p)
	_, _, err = c.adminPost(ctx, adminPath, form)
	return err
}

// AdminDeleteLevel removes a level. The engine only allows this before the
// game starts and when no level chains exist.
func (c *Client) AdminDeleteLevel(ctx context.Context, gameID, levelID int) error {
	form := url.Values{"action": {"del_zadanie"}, "Project": {""}, "ofset": {"0"}, "id": {strconv.Itoa(levelID)}, "categoryValue": {strconv.Itoa(gameID)}}
	_, _, err := c.adminPost(ctx, adminPath, form)
	return err
}

// AdminMoveLevel moves a level one position up or down in its game's order.
//
// The engine renders one arrow form per level and drives them through its
// generic row mover: table=zadanie names the table and parentName=game the
// relation, while parent is left empty and no game id is sent at all — the
// level id alone identifies the row. The fields below are what the live
// admin page submits, measured against classic.dzzzr.ru on 2026-09-09.
//
// Moving the first level up, or the last one down, is a no-op the engine
// accepts silently, so callers that need to know whether anything changed
// have to compare AdminListLevels before and after.
func (c *Client) AdminMoveLevel(ctx context.Context, levelID int, up bool) error {
	action := "move_down"
	if up {
		action = "move_up"
	}
	form := url.Values{
		"action": {action}, "Project": {""}, "ofset": {"0"},
		"table": {"zadanie"}, "parentName": {"game"}, "parent": {""},
		"id": {strconv.Itoa(levelID)},
	}
	_, _, err := c.adminPost(ctx, adminPath, form)
	return err
}

// AdminCopyLevels copies all levels of sourceGameID into gameID.
func (c *Client) AdminCopyLevels(ctx context.Context, gameID, sourceGameID int) error {
	form := url.Values{"action": {"copy_zadanie"}, "Project": {""}, "ProjectValue": {""}, "categoryValue": {strconv.Itoa(gameID)}, "game": {strconv.Itoa(sourceGameID)}}
	_, _, err := c.adminPost(ctx, adminPath, form)
	return err
}
