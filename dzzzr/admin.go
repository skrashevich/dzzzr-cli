package dzzzr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The administration area lives at {base}admin/ behind HTTP Basic with the
// organizer's own login and password (realm "Admin area"). Pages are
// selected with ?action=<section>; every write is a POST to admin/ with a
// hidden action=<verb> field and is answered by a redirect back to the page.
// The engine relies on register_globals-style variable names, so parameter
// names below are verbatim from the PHP sources of Dozor Classic.

const adminPath = "admin/"

// adminGet fetches an admin page and returns its HTML.
func (c *Client) adminGet(ctx context.Context, path string, query url.Values) (string, error) {
	req, err := c.newRequest(ctx, http.MethodGet, path, query, nil, requestOptions{admin: true, requireAuth: true})
	if err != nil {
		return "", err
	}
	status, _, body, err := c.do(req)
	if err != nil {
		return "", err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return "", &AuthError{Kind: AuthAdmin, Message: strings.TrimSpace(StripHTML(string(body)))}
	}
	if status != http.StatusOK {
		return "", &HTTPError{StatusCode: status, Context: "admin " + path}
	}
	page := decodeWindows1251IfNeeded(body)
	scope := path
	if a := query.Get("action"); a != "" {
		scope = a
	}
	if err := checkAdminScope(page, scope); err != nil {
		return "", err
	}
	return page, nil
}

// adminNoAccessPhrase is matched in full rather than by its first words:
// the organizer chat and a task's own text come back inside these same
// pages, so a looser match could turn a successful write into an
// authentication error.
const adminNoAccessPhrase = "Вы не имеете доступа к этому разделу"

// checkAdminScope reports the engine's refusal of a section to an account
// whose credentials it accepted. Measured against classic.dzzzr.ru on
// 2026-09-09: an organizer that is not the game's assigned organizer gets
// HTTP 200 carrying adminNoAccessPhrase — wrapped in the whole admin frame
// for admin/?action=teams, bare as "<login> … (0)" for admin/gmcht.php. The
// length of the reply is deliberately not part of the test: the teams page
// carries 896 bytes of chrome around the sentence, so a "short body" rule
// would miss it.
//
// It lives in adminGet/adminPost rather than in each caller, so a call site
// that adds no check of its own (as admin_messages.go had none) cannot read
// the refusal as an empty result or a successful write. section names the
// refused page or form verb for the message the caller shows.
func checkAdminScope(page, section string) error {
	if strings.Contains(page, adminNoAccessPhrase) {
		return &AuthError{Kind: AuthAdminScope, Message: section}
	}
	return nil
}

// adminPost submits an admin form. The engine answers with a redirect whose
// target names the page to show next; the target is returned so callers can
// read ids out of it. A 200 with a page is accepted as well, since some
// handlers render instead of redirecting.
func (c *Client) adminPost(ctx context.Context, path string, form url.Values) (location string, page string, err error) {
	if err := c.paceAdmin(ctx); err != nil {
		return "", "", err
	}
	req, err := c.newRequest(ctx, http.MethodPost, path, nil, form, requestOptions{admin: true, requireAuth: true, cp1251Form: true})
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Referer", c.resolve(path, nil))
	status, hdr, body, err := c.do(req)
	if err != nil {
		return "", "", err
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "", "", &AuthError{Kind: AuthAdmin, Message: strings.TrimSpace(StripHTML(string(body)))}
	case status >= 300 && status < 400:
		return hdr.Get("Location"), "", nil
	case status == http.StatusOK:
		page := decodeWindows1251IfNeeded(body)
		if err := checkAdminScope(page, form.Get("action")); err != nil {
			return "", "", err
		}
		return "", page, nil
	default:
		return "", "", &HTTPError{StatusCode: status, Context: "admin " + form.Get("action")}
	}
}

// paceAdmin waits so consecutive admin POSTs are at least adminDelay apart.
// The slot is reserved under the lock and the wait happens outside it, and
// the wait ends early when the caller gives up.
func (c *Client) paceAdmin(ctx context.Context) error {
	delay := c.AdminDelay()
	if delay <= 0 {
		return nil
	}
	c.adminMu.Lock()
	wait := time.Until(c.lastAdminPost.Add(delay))
	if wait > 0 {
		c.lastAdminPost = c.lastAdminPost.Add(delay)
	} else {
		c.lastAdminPost = time.Now()
	}
	c.adminMu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// adminPage fetches admin/?action=<section>&edit=1&... and parses it.
func (c *Client) adminPage(ctx context.Context, section string, extra url.Values) (*htmlDoc, error) {
	q := url.Values{"action": {section}, "edit": {"1"}, "Project": {""}}
	for k, v := range extra {
		q[k] = v
	}
	page, err := c.adminGet(ctx, adminPath, q)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(page, "action=") && !strings.Contains(page, "<form") {
		return nil, &UndecodableResponseError{StatusCode: http.StatusOK, Context: "admin " + section, Err: errNotAdminPage, Body: truncate([]byte(page))}
	}
	doc, err := parseHTML(page)
	if err != nil {
		return nil, &UndecodableResponseError{StatusCode: http.StatusOK, Context: "admin " + section, Err: err}
	}
	return doc, nil
}

const errNotAdminPage = stringError("reply is not an administration page")

// decodeWindows1251IfNeeded converts a legacy page to UTF-8. The admin area
// is served in windows-1251; API replies are already UTF-8.
func decodeWindows1251IfNeeded(body []byte) string {
	if isUTF8(body) {
		return string(body)
	}
	return decodeWindows1251(body)
}

func isUTF8(b []byte) bool {
	for i := 0; i < len(b); {
		if b[i] < 0x80 {
			i++
			continue
		}
		size := 0
		switch {
		case b[i]&0xE0 == 0xC0:
			size = 2
		case b[i]&0xF0 == 0xE0:
			size = 3
		case b[i]&0xF8 == 0xF0:
			size = 4
		default:
			return false
		}
		if i+size > len(b) {
			return false
		}
		for j := 1; j < size; j++ {
			if b[i+j]&0xC0 != 0x80 {
				return false
			}
		}
		i += size
	}
	return true
}

// idFromLocation extracts the id=N parameter of an admin redirect.
func idFromLocation(loc string) (int, bool) {
	q := linkQuery(loc)
	v := q.Get("id")
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ---------------------------------------------------------------------------
// Games

// AdminGame is one row of the organizer's game list.
type AdminGame struct {
	ID        int    `json:"id"`
	Date      string `json:"date"` // "DD.MM.YYYY HH:MM"
	Number    string `json:"number"`
	Name      string `json:"name"`
	Status    string `json:"status"` // "анонс", "прием заявок", "завершена"
	Season    string `json:"season"`
	Finished  bool   `json:"finished"`
	Published bool   `json:"published"`
	Selected  bool   `json:"selected,omitempty"`
}

// GameParams are the editable settings of a game. Zero values mean "leave
// the engine default" on create and "keep the current value" on update;
// use the *Set fields to force a false/zero value on update.
type GameParams struct {
	Name             string `json:"name,omitempty"`
	Number           string `json:"number,omitempty"`
	Date             string `json:"date,omitempty"` // DD.MM.YYYY
	Time             string `json:"time,omitempty"` // HH:MM
	Author           string `json:"author,omitempty"`
	League           int    `json:"league,omitempty"`
	OtherLeague      *bool  `json:"other_league,omitempty"`
	Zone             string `json:"zone,omitempty"`
	Legend           string `json:"legend,omitempty"`
	LegendComment    string `json:"legend_comment,omitempty"`
	Anons            string `json:"anons,omitempty"`
	ClueMin          int    `json:"clue_min,omitempty"`
	ClueMin2         int    `json:"clue_min2,omitempty"`
	ClueMin3         int    `json:"clue_min3,omitempty"`
	MasterCode       string `json:"master_code,omitempty"`
	MasterCodeShtraf int    `json:"master_code_shtraf,omitempty"`
	ClueBeforeShtraf int    `json:"clue_before_shtraf,omitempty"`
	Penalty          int    `json:"penalty,omitempty"`
	Price            string `json:"price,omitempty"`
	BreafPlace       string `json:"breaf_place,omitempty"`
	Greeting         string `json:"greeting,omitempty"`
	Duration         int    `json:"duration,omitempty"`
	BonusAfter       int    `json:"bonus_after,omitempty"`
	Publish          *bool  `json:"publish,omitempty"`
	Finished         *bool  `json:"finished,omitempty"`
	Invitation       *bool  `json:"invitation,omitempty"`
	// Clear names text parameters to blank explicitly, by the same names the
	// fields above carry in JSON. A value type cannot tell an empty string
	// from an absent one, and the engine does store an empty text field.
	// Clearing is applied after every other field, so naming a parameter here
	// and setting it in the same call blanks it: the explicit intent wins
	// over an ambiguous value. A name that is not a text parameter is
	// ignored.
	Clear []string `json:"clear,omitempty"`
}

// AdminGameInfo is the full game form.
type AdminGameInfo struct {
	ID     int        `json:"id"`
	Params GameParams `json:"params"`
	Fields url.Values `json:"fields"` // every form field as the engine renders it
}

var (
	dateRe     = regexp.MustCompile(`^\d{2}\.\d{2}\.\d{4}( \d{2}:\d{2})?$`)
	leadNumRe  = regexp.MustCompile(`^\s*(\d+)\s*\.`)
	gameStatus = []string{"завершена", "прием заявок", "анонс"}
)

// AdminListGames returns the organizer's games, newest first.
func (c *Client) AdminListGames(ctx context.Context) ([]AdminGame, error) {
	doc, err := c.adminPage(ctx, "games", nil)
	if err != nil {
		return nil, err
	}
	selectedID := 0
	selectedPublished := true
	if f := doc.formWithAction("update_game"); f != nil {
		selectedID, _ = strconv.Atoi(f.Fields.Get("id"))
		if v, ok := f.Checkboxes["publish"]; ok {
			selectedPublished = v
		}
	}
	rows := doc.rows()
	idx := headerIndex(rows)
	var out []AdminGame
	for _, row := range rows {
		g, ok := parseGameRow(row, idx, selectedID)
		if ok {
			if g.Selected {
				g.Published = selectedPublished
			}
			out = append(out, g)
		}
	}
	return out, nil
}

func parseGameRow(row htmlRow, idx map[string]int, selectedID int) (AdminGame, bool) {
	if len(row.Cells) < 4 {
		return AdminGame{}, false
	}
	var g AdminGame
	if l, q, ok := row.linkWithParams("action", "games", "edit", "1"); ok {
		g.ID, _ = strconv.Atoi(q.Get("id"))
		g.Name = l.Text
	} else if row.selected() && selectedID != 0 {
		g.ID = selectedID
		g.Selected = true
	} else {
		return AdminGame{}, false
	}
	if d := row.cellByHeader(idx, "дата"); dateRe.MatchString(d) {
		g.Date = d
	}
	if n := row.cellByHeader(idx, "n", "номер"); n != "" && n != g.Name {
		g.Number = n
	}
	dateSeen := g.Date != ""
	for _, cell := range row.Cells {
		cell = strings.TrimSpace(cell)
		switch {
		case cell == "":
			continue
		case !dateSeen && dateRe.MatchString(cell):
			g.Date = cell
			dateSeen = true
		}
		for _, s := range gameStatus {
			if strings.HasPrefix(cell, s) {
				g.Status = s
				g.Finished = s == "завершена"
			}
		}
		if strings.HasPrefix(cell, "сезон ") || strings.HasPrefix(cell, "вне зачета") || strings.HasPrefix(cell, "тестовая") || strings.HasPrefix(cell, "презентационная") {
			g.Season = cell
		}
	}
	if g.Name == "" {
		// Selected row: the name is the bold cell, which contains no link.
		for _, cell := range row.Cells {
			cell = strings.TrimSpace(cell)
			if cell == "" || dateRe.MatchString(cell) || cell == g.Number || cell == g.Status || cell == g.Season {
				continue
			}
			if strings.ContainsAny(cell, " ") || len(cell) > 3 {
				g.Name = firstLine(cell)
				break
			}
		}
	}
	if g.Date == "" && g.ID == 0 {
		return AdminGame{}, false
	}
	g.Published = !row.dimmed()
	return g, true
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// AdminGetGame returns the settings form of a game.
func (c *Client) AdminGetGame(ctx context.Context, gameID int) (*AdminGameInfo, error) {
	doc, err := c.adminPage(ctx, "games", url.Values{"id": {strconv.Itoa(gameID)}})
	if err != nil {
		return nil, err
	}
	f := doc.formWithAction("update_game")
	if f == nil {
		return nil, &UndecodableResponseError{StatusCode: http.StatusOK, Context: "admin game form", Err: stringError("no update_game form; wrong game id or no access")}
	}
	info := &AdminGameInfo{Fields: f.Fields}
	info.ID, _ = strconv.Atoi(f.Fields.Get("id"))
	info.Params = gameParamsFromForm(f)
	return info, nil
}

func gameParamsFromForm(f *htmlForm) GameParams {
	get := func(k string) string { return strings.TrimSpace(f.Fields.Get(k)) }
	atoi := func(k string) int { return parseLooseInt(get(k)) }
	boolp := func(k string) *bool {
		if _, ok := f.Checkboxes[k]; !ok {
			return nil
		}
		v := f.Checkboxes[k]
		return &v
	}
	return GameParams{
		Name: get("name"), Number: get("number"), Date: get("date"), Time: get("time"), Author: get("author"),
		League: atoi("league"), OtherLeague: boolp("otherLeague"), Zone: get("zone"),
		Legend: get("legend"), LegendComment: get("legendComment"), Anons: get("anons"),
		ClueMin: atoi("clueMin"), ClueMin2: atoi("clueMin2"), ClueMin3: atoi("clueMin3"),
		MasterCode: get("masterCode"), MasterCodeShtraf: atoi("masterCodeShtraf"), ClueBeforeShtraf: atoi("clueBeforeShtraf"),
		Penalty: atoi("penalty"), Price: get("price"), BreafPlace: get("breafPlace"), Greeting: get("greeting"),
		Duration: atoi("duration"), BonusAfter: atoi("bonusAfter"),
		Publish: boolp("publish"), Finished: boolp("finished"), Invitation: boolp("invitation"),
	}
}

// applyGameParams writes non-zero params into the form values.
// gameTextFields maps a parameter name, as the CLI and the JSON layers spell
// it, to the engine's own form field. Clearing goes through it so a caller
// never has to know the engine's spelling.
var gameTextFields = map[string]string{
	"name": "name", "number": "number", "date": "date", "time": "time",
	"author": "author", "zone": "zone", "legend": "legend",
	"legend_comment": "legendComment", "anons": "anons",
	"master_code": "masterCode", "price": "price",
	"breaf_place": "breafPlace", "greeting": "greeting",
}

func applyGameParams(form url.Values, p GameParams) {
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
	set("name", p.Name)
	set("number", p.Number)
	set("date", p.Date)
	set("time", p.Time)
	set("author", p.Author)
	seti("league", p.League)
	setb("otherLeague", p.OtherLeague)
	set("zone", p.Zone)
	set("legend", p.Legend)
	set("legendComment", p.LegendComment)
	set("anons", p.Anons)
	seti("clueMin", p.ClueMin)
	seti("clueMin2", p.ClueMin2)
	seti("clueMin3", p.ClueMin3)
	set("masterCode", p.MasterCode)
	seti("masterCodeShtraf", p.MasterCodeShtraf)
	seti("clueBeforeShtraf", p.ClueBeforeShtraf)
	seti("penalty", p.Penalty)
	set("price", p.Price)
	set("breafPlace", p.BreafPlace)
	set("greeting", p.Greeting)
	seti("duration", p.Duration)
	seti("bonusAfter", p.BonusAfter)
	setb("publish", p.Publish)
	setb("finished", p.Finished)
	setb("invitation", p.Invitation)
	clearFields(form, p.Clear, gameTextFields)
}

// clearFields blanks the form fields named in clear. A value type cannot tell
// "" apart from "not given", so a caller that means to empty a text field
// says so here. Measured against classic.dzzzr.ru on 2026-09-09: the engine
// stores an empty text field as empty, so clearing is a real operation —
// unlike a numeric zero, which the engine itself ignores.
func clearFields(form url.Values, clear []string, fields map[string]string) {
	for _, name := range clear {
		if key, ok := fields[name]; ok {
			form.Set(key, "")
		}
	}
}

// AdminCreateGame creates a game and returns its id. Name, Date and Time are
// required; the engine fills defaults for the rest (hint intervals 30/30/30,
// penalty and price from city settings).
func (c *Client) AdminCreateGame(ctx context.Context, p GameParams) (int, error) {
	if p.Name == "" || p.Date == "" || p.Time == "" {
		return 0, fmt.Errorf("dzzzr: admin create game: name, date (DD.MM.YYYY) and time (HH:MM) are required")
	}
	// Clear is applied after this guard, so it could otherwise blank the very
	// fields the guard insists on.
	for _, name := range p.Clear {
		switch name {
		case "name", "date", "time":
			return 0, fmt.Errorf("dzzzr: admin create game: %s is required and cannot be cleared", name)
		}
	}
	form := url.Values{
		"action": {"add_game"}, "Project": {""}, "ofset": {"0"}, "id": {""},
		"clueMin": {"30"}, "clueMin2": {"30"}, "clueMin3": {"30"}, "oldClueMin": {"30"},
		"league": {"1"}, "season": {"0"}, "dateAutoStop": {"00.00.0000"}, "timeAutoStop": {"00:00"},
	}
	applyGameParams(form, p)
	loc, page, err := c.adminPost(ctx, adminPath, form)
	if err != nil {
		return 0, err
	}
	if id, ok := idFromLocation(loc); ok {
		return id, nil
	}
	if page != "" {
		if doc, err := parseHTML(page); err == nil {
			if f := doc.formWithAction("update_game"); f != nil {
				if id, err := strconv.Atoi(f.Fields.Get("id")); err == nil {
					return id, nil
				}
			}
		}
	}
	return 0, &UndecodableResponseError{StatusCode: http.StatusFound, Context: "admin create game", Err: stringError("no game id in redirect"), Body: loc}
}

// AdminUpdateGame reads the game's form, overlays p and submits it, so
// fields not mentioned in p keep their values.
func (c *Client) AdminUpdateGame(ctx context.Context, gameID int, p GameParams) error {
	info, err := c.AdminGetGame(ctx, gameID)
	if err != nil {
		return err
	}
	form := cloneValues(info.Fields)
	form.Set("action", "update_game")
	form.Set("id", strconv.Itoa(gameID))
	applyGameParams(form, p)
	_, _, err = c.adminPost(ctx, adminPath, form)
	return err
}

// AdminDeleteGame deletes a game. The engine refuses games that still have
// levels; delete them first.
func (c *Client) AdminDeleteGame(ctx context.Context, gameID int) error {
	form := url.Values{"action": {"del_games"}, "Project": {""}, "ofset": {"0"}, "id": {strconv.Itoa(gameID)}}
	_, _, err := c.adminPost(ctx, adminPath, form)
	return err
}

// AdminCopyGame copies a game (settings and, when withLevels, all levels)
// into a new one and returns the new id.
func (c *Client) AdminCopyGame(ctx context.Context, sourceGameID int, withLevels bool) (int, error) {
	form := url.Values{"action": {"copy_game"}, "Project": {""}, "game": {strconv.Itoa(sourceGameID)}}
	if withLevels {
		form.Set("zadan", "on")
	}
	loc, _, err := c.adminPost(ctx, adminPath, form)
	if err != nil {
		return 0, err
	}
	if id, ok := idFromLocation(loc); ok {
		return id, nil
	}
	return 0, &UndecodableResponseError{StatusCode: http.StatusFound, Context: "admin copy game", Err: stringError("no game id in redirect"), Body: loc}
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// GameTextParam reports whether name is a text parameter of a game, i.e. one
// that GameParams.Clear can blank.
func GameTextParam(name string) bool { _, ok := gameTextFields[name]; return ok }

// LevelTextParam reports whether name is a text parameter of a level.
func LevelTextParam(name string) bool { _, ok := levelTextFields[name]; return ok }
