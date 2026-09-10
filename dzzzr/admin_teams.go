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

// Application statuses of a team in a game.
const (
	ApplicationPending      = 0 // не рассмотрена
	ApplicationAccepted     = 1 // принята
	ApplicationRejected     = 2 // отклонена
	ApplicationOutOfContest = 3 // вне зачета
	ApplicationAuthor       = 4 // автор
	ApplicationDisqualified = 5 // дисквалифицирована
)

var applicationStatusText = map[int]string{
	0: "не рассмотрена", 1: "принята", 2: "отклонена", 3: "вне зачета", 4: "автор", 5: "дисквалифицирована",
}

// ApplicationStatusText names an application status.
func ApplicationStatusText(status int) string {
	if s, ok := applicationStatusText[status]; ok {
		return s
	}
	return "статус " + strconv.Itoa(status)
}

// AdminTeam is one row of the team list of a game.
type AdminTeam struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Captain    string `json:"captain"`
	Status     int    `json:"status"`
	StatusText string `json:"status_text"`
	Pin        string `json:"pin"`
	Points     int    `json:"points"`
	GamesCount int    `json:"games_count"`
	Blocked    bool   `json:"blocked"`
	Selected   bool   `json:"selected,omitempty"`
}

// ApplicationParams changes a team's application in a game.
type ApplicationParams struct {
	Status  *int   `json:"status,omitempty"`
	NewPin  bool   `json:"new_pin,omitempty"`
	Points  *int   `json:"points,omitempty"`
	Novice  *bool  `json:"novice,omitempty"`
	Comment string `json:"comment,omitempty"` // sent to the captain by e-mail
}

// TeamPlayer is one member of a team's roster as the card lists it.
type TeamPlayer struct {
	Login string `json:"login"`
	// OnTeam is the checkbox the engine ticks for a player who counts as
	// part of the team; it leaves the box clear for the rest.
	OnTeam bool `json:"on_team"`
}

// AdminTeamInfo is a team's form in the context of a game.
type AdminTeamInfo struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Captain string `json:"captain"`
	Shtab   string `json:"shtab"`
	Helper  string `json:"helper"`
	Status  int    `json:"status"`
	Pin     string `json:"pin"`
	Points  int    `json:"points"`
	Novice  bool   `json:"novice"`
	Blocked bool   `json:"blocked"`
	Website string `json:"website"`
	League  string `json:"league,omitempty"`
	Deviz   string `json:"deviz,omitempty"`
	// Roster is the team's full member list, sorted by login. The card's own
	// order is not preserved: the parsed form keeps checkboxes in a map, so
	// sorting is what makes the output, the JSON and the tests deterministic.
	Roster      []TeamPlayer `json:"roster,omitempty"`
	Fields      url.Values   `json:"fields"`
	Checkboxes  map[string]bool
	StatusNames map[string]string `json:"-"`
}

// AdminListTeams returns the teams that applied to a game.
func (c *Client) AdminListTeams(ctx context.Context, gameID int) ([]AdminTeam, error) {
	doc, err := c.adminPage(ctx, "teams", url.Values{"categoryValue": {strconv.Itoa(gameID)}})
	if err != nil {
		return nil, err
	}
	selectedID := 0
	selectedBlocked := false
	if f := doc.formWithAction("updateTeam"); f != nil {
		selectedID, _ = strconv.Atoi(f.Fields.Get("id"))
		selectedBlocked = f.Checkboxes["blocked"]
	}
	rows := doc.rows()
	idx, _ := headerIndex(rows)
	var out []AdminTeam
	for _, row := range rows {
		if t, ok := parseTeamRow(row, idx, selectedID); ok {
			if t.Selected {
				t.Blocked = selectedBlocked
			}
			out = append(out, t)
		}
	}
	return out, nil
}

// bracketedTeamRe matches the "[id] Название" the engine prints for a team
// in an application row.
var bracketedTeamRe = regexp.MustCompile(`^\s*\[(\d+)\]\s*(.*)$`)

func parseTeamRow(row htmlRow, idx map[string]int, selectedID int) (AdminTeam, bool) {
	if len(row.Cells) < 6 {
		return AdminTeam{}, false
	}
	var t AdminTeam
	// The captain's profile link only supplies the login; it does not decide
	// whether this is an application row, because the "[id] Название" marker
	// below identifies one on its own.
	if capLink, capQ, ok := row.linkWithParams("action", "users", "edit", "1"); ok {
		t.Captain = capQ.Get("login")
		if t.Captain == "" {
			t.Captain = capLink.Text
		}
	}
	// Every application row names the team as "[id] Название" in a nested
	// table, which identifies it even when no link does. The cell it lands in
	// varies with the spacer columns the engine pads rows with.
	for _, cell := range row.Cells {
		if m := bracketedTeamRe.FindStringSubmatch(cell); m != nil {
			t.ID, _ = strconv.Atoi(m[1])
			t.Name = firstLine(strings.TrimSpace(m[2]))
			break
		}
	}
	switch {
	case t.ID != 0:
		t.Selected = t.ID == selectedID
	default:
		if link, q, ok := row.linkWithParams("action", "teams"); ok && q.Get("id") != "" {
			t.ID, _ = strconv.Atoi(q.Get("id"))
			t.Name = strings.TrimSpace(link.Text)
		} else if row.selected() && selectedID != 0 {
			t.ID = selectedID
			t.Selected = true
		} else {
			return AdminTeam{}, false
		}
	}
	t.Status = -1
	statusIdx := -1
	for i, cell := range row.Cells {
		cell = strings.TrimSpace(cell)
		for code, name := range applicationStatusText {
			if cell == name {
				t.Status = code
				t.StatusText = name
				statusIdx = i
			}
		}
	}
	if t.Name == "" {
		for i, cell := range row.Cells {
			cell = strings.TrimSpace(cell)
			if cell == "" || cell == t.Captain || i == statusIdx {
				continue
			}
			if strings.ContainsAny(cell, " ") || len([]rune(cell)) > 2 {
				t.Name = firstLine(cell)
				break
			}
		}
	}
	// Remaining columns are read by header name, because the engine pads rows
	// with spacer cells and omits values, so counting cells is unreliable.
	t.GamesCount = parseLooseInt(row.cellByHeader(idx, "сыграно"))
	t.Pin = row.cellByHeader(idx, "pin")
	t.Points = parseLooseInt(row.cellByHeader(idx, "рейтинговые"))
	t.Blocked = row.dimmed()
	return t, true
}

// CityTeam is one entry of the city's team directory.
type CityTeam struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// AdminListCityTeams returns every team registered in the city, as the
// engine's "add a team to this game" picker lists them. The picker lives on
// a game's teams page, so a game id is needed to reach it; the list itself
// is the city's, not the game's.
//
// Measured against classic.dzzzr.ru on 2026-09-10: 1043 teams, the same list
// on every game. The picker's first entry is an empty placeholder and is
// dropped. Only an account that may open the teams section sees any of it.
func (c *Client) AdminListCityTeams(ctx context.Context, gameID int) ([]CityTeam, error) {
	doc, err := c.adminPage(ctx, "teams", url.Values{"categoryValue": {strconv.Itoa(gameID)}})
	if err != nil {
		return nil, err
	}
	f := doc.formWithAction("addTeamToGame")
	if f == nil {
		return nil, &UndecodableResponseError{StatusCode: http.StatusOK, Context: "admin city teams", Err: stringError("no addTeamToGame form; the teams section is closed for this account")}
	}
	var out []CityTeam
	for _, o := range f.Selects["team"] {
		id, err := strconv.Atoi(o.Value)
		name := strings.TrimSpace(o.Text)
		if err != nil || id == 0 || name == "" {
			continue
		}
		out = append(out, CityTeam{ID: id, Name: name})
	}
	return out, nil
}

// AdminGetTeam returns a team's form for the given game.
func (c *Client) AdminGetTeam(ctx context.Context, gameID, teamID int) (*AdminTeamInfo, error) {
	doc, err := c.adminPage(ctx, "teams", url.Values{"categoryValue": {strconv.Itoa(gameID)}, "id": {strconv.Itoa(teamID)}})
	if err != nil {
		return nil, err
	}
	f := doc.formWithAction("updateTeam")
	if f == nil {
		return nil, &UndecodableResponseError{StatusCode: http.StatusOK, Context: "admin team form", Err: stringError("no updateTeam form; wrong team id or no access")}
	}
	info := &AdminTeamInfo{Fields: f.Fields, Checkboxes: f.Checkboxes, StatusNames: map[string]string{}}
	info.ID, _ = strconv.Atoi(f.Fields.Get("id"))
	info.Name = strings.TrimSpace(f.Fields.Get("name"))
	info.Captain = strings.TrimSpace(f.Fields.Get("newCap"))
	if info.Captain == "" {
		info.Captain = strings.TrimSpace(f.Fields.Get("captain"))
	}
	info.Shtab = f.Fields.Get("shtab")
	info.Helper = f.Fields.Get("captainHelper")
	info.Status = parseLooseInt(f.Fields.Get("status"))
	info.Pin = strings.TrimSpace(f.Fields.Get("pinlogin"))
	info.Points = parseLooseInt(f.Fields.Get("points"))
	info.Novice = f.Checkboxes["novice"]
	info.Blocked = f.Checkboxes["blocked"]
	info.Website = f.Fields.Get("website")
	info.Deviz = f.Fields.Get("deviz")
	info.League = selectedOptionText(f, "league")
	info.Roster = rosterFromForm(f)
	for _, o := range f.Selects["status"] {
		info.StatusNames[o.Value] = o.Text
	}
	return info, nil
}

// rosterFromForm reads the card's "Полный состав команды" block: the engine
// prints one checkbox per member, named pl[<login>], ticked for a player who
// counts as part of the team. Measured against classic.dzzzr.ru on
// 2026-09-10: team 31 of game 1383 carries 184 of them.
//
// The engine also prints one nameless pl[] box at the head of the block, for
// adding a player; it is not a member and is skipped.
func rosterFromForm(f *htmlForm) []TeamPlayer {
	logins := make([]string, 0, len(f.Checkboxes))
	for name := range f.Checkboxes {
		if login, ok := strings.CutPrefix(name, "pl["); ok {
			if login, ok := strings.CutSuffix(login, "]"); ok && login != "" {
				logins = append(logins, login)
			}
		}
	}
	if len(logins) == 0 {
		return nil
	}
	slices.Sort(logins)
	out := make([]TeamPlayer, 0, len(logins))
	for _, login := range logins {
		out = append(out, TeamPlayer{Login: login, OnTeam: f.Checkboxes["pl["+login+"]"]})
	}
	return out
}

// selectedOptionText names the chosen option of a select, for the fields the
// engine stores as a code but shows as a word.
func selectedOptionText(f *htmlForm, name string) string {
	value := f.Fields.Get(name)
	for _, o := range f.Selects[name] {
		if o.Value == value {
			return o.Text
		}
	}
	return ""
}

// AdminSetApplication updates a team's application: status, PIN, rating
// points, novice flag. It reads the form first so the rest of the team's
// settings are preserved.
func (c *Client) AdminSetApplication(ctx context.Context, gameID, teamID int, p ApplicationParams) error {
	info, err := c.AdminGetTeam(ctx, gameID, teamID)
	if err != nil {
		return err
	}
	form := cloneValues(info.Fields)
	form.Set("action", "updateTeam")
	form.Set("id", strconv.Itoa(teamID))
	form.Set("categoryValue", strconv.Itoa(gameID))
	form.Del("pinlogin")
	if p.Status != nil {
		form.Set("status", strconv.Itoa(*p.Status))
	}
	if p.NewPin {
		form.Set("newPIN", "on")
	} else {
		form.Del("newPIN")
	}
	if p.Points != nil {
		form.Set("points", strconv.Itoa(*p.Points))
	}
	if p.Novice != nil {
		if *p.Novice {
			form.Set("novice", "on")
		} else {
			form.Del("novice")
		}
	}
	form.Set("comment", p.Comment)
	_, _, err = c.adminPost(ctx, adminPath, form)
	return err
}

// AdminAcceptAllApplications accepts every pending application of a game
// and issues PINs.
func (c *Client) AdminAcceptAllApplications(ctx context.Context, gameID int) error {
	form := url.Values{"action": {"acceptAllApplications"}, "Project": {""}, "ofset": {"0"}, "id": {""}, "categoryValue": {strconv.Itoa(gameID)}}
	_, _, err := c.adminPost(ctx, adminPath, form)
	return err
}

// AdminGeneratePins issues fresh PINs to every accepted team of a game.
func (c *Client) AdminGeneratePins(ctx context.Context, gameID int) error {
	form := url.Values{"action": {"GeneratePinCodes"}, "Project": {""}, "ofset": {"0"}, "id": {""}, "categoryValue": {strconv.Itoa(gameID)}}
	_, _, err := c.adminPost(ctx, adminPath, form)
	return err
}

// AdminAddTeamToGame files an application for a team on the organizer's
// behalf (status pending).
func (c *Client) AdminAddTeamToGame(ctx context.Context, gameID, teamID int) error {
	form := url.Values{"action": {"addTeamToGame"}, "Project": {""}, "categoryValue": {strconv.Itoa(gameID)}, "team": {strconv.Itoa(teamID)}}
	_, _, err := c.adminPost(ctx, adminPath, form)
	return err
}

// AdminRemoveApplication deletes a team's application (and its game log,
// planned levels and individual codes) from a game. status must match the
// application's current status.
func (c *Client) AdminRemoveApplication(ctx context.Context, gameID, teamID, status int) error {
	form := url.Values{"action": {"del_application"}, "Project": {""}, "ofset": {"0"}, "categoryValue": {strconv.Itoa(gameID)}, "id": {strconv.Itoa(teamID)}, "status": {strconv.Itoa(status)}}
	_, _, err := c.adminPost(ctx, adminPath, form)
	return err
}

// AdminCreateTeam registers a new team with the given captain login and
// returns its id when the engine reports it.
func (c *Client) AdminCreateTeam(ctx context.Context, name, captain string) (int, error) {
	form := url.Values{"action": {"newTeam"}, "Project": {""}, "ofset": {"0"}, "categoryValue": {""}, "name": {name}, "captain": {captain}}
	loc, _, err := c.adminPost(ctx, adminPath, form)
	if err != nil {
		return 0, err
	}
	if q := linkQuery(loc); q.Get("err") != "" && q.Get("err") != "0" {
		// Measured against classic.dzzzr.ru on 2026-09-09: a duplicate
		// name/captain gives err=3, but err=4 was also observed for an
		// account that lacks the rights to create teams at all (a fresh,
		// unique name/captain still failed with err=4), so the code alone
		// does not tell the two apart.
		return 0, &EngineError{Code: parseLooseInt(q.Get("err")), Text: fmt.Sprintf("engine refused to create the team (error %s: seen both for a duplicate name/captain and for an organizer account without rights to create teams)", q.Get("err"))}
	}
	id, _ := idFromLocation(loc)
	return id, nil
}
