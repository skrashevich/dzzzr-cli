package dzzzr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// The archive is the one part of the engine that has no JSON at all: it is
// rendered at {base}?section=arc&gmid=<game>&what=<view> as windows-1251 HTML
// meant for a browser. Two views carry data worth reading — "stat", the
// finished game's results for every team, and "comment", the scenario the
// organizer published afterwards with its codes, hints and spoilers.
//
// Both are parsed rather than decoded. The results table is regular and is
// read from the DOM; the scenario is not — its markup has unclosed <p> and a
// </noindex> outside the <div> it opened inside, so an HTML tree of it is the
// parser's guess rather than the engine's intent. It is therefore split on the
// headings the engine writes, which are exact.

const (
	// archiveSection is the site section the archive lives in.
	archiveSection = "arc"
	// statTableID is the id the engine gives the results table.
	statTableID = "suxx"
)

// sitePage fetches one page of the public site and returns it as UTF-8 text.
//
// The site requires dozorSiteSession with the API token. A query-only s token
// leaves the visitor anonymous and hides restricted scenario content.
func (c *Client) sitePage(ctx context.Context, ctxName string, q url.Values) (string, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "", q, nil, requestOptions{siteCookie: true, session: true})
	if err != nil {
		return "", err
	}
	status, _, body, err := c.do(req)
	if err != nil {
		return "", err
	}
	if err := classifyStatus(status, body, ctxName); err != nil {
		return "", err
	}
	return decodeWindows1251IfNeeded(body), nil
}

// siteContent cuts a site page down to the region between the engine's own
// content markers, so the menu, the calendar and the social widgets around it
// cannot be mistaken for data.
func siteContent(page string) string {
	i := strings.Index(page, "<!-- CONTENT -->")
	if i < 0 {
		return page
	}
	rest := page[i+len("<!-- CONTENT -->"):]
	if j := strings.Index(rest, "<!-- CONTENT END -->"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// siteAccessError recognizes the two refusals the site renders in place of a
// page: one for a visitor it does not know and one for a visitor it knows but
// will not show this to.
//
// Only the opening of the content region is examined, because a refusal is the
// whole of it. A scenario written by an organizer may well contain the same
// words somewhere in a task, and matching those would hide a page the caller
// is entitled to.
func siteAccessError(content string) error {
	head := content
	if len(head) > 512 {
		head = head[:512]
	}
	switch t := strings.TrimSpace(StripHTML(head)); {
	case strings.HasPrefix(t, "Требуется авторизация"):
		return &AuthError{Kind: AuthSession, Message: "движок требует авторизации на сайте"}
	case strings.HasPrefix(t, "Недостаточно полномочий"):
		return &EngineError{Code: ErrNoPermission, Text: "недостаточно полномочий для просмотра этой страницы"}
	}
	return nil
}

// archiveHeadRe matches the date and title the archive prints above a game.
var archiveHeadRe = regexp.MustCompile(`(?is)<div class=Date>(.*?)</div>\s*<h1>(.*?)</h1>`)

// archiveHeading reads the game's date and name off an archive page.
func archiveHeading(content string) (date, name string) {
	m := archiveHeadRe.FindStringSubmatch(content)
	if m == nil {
		return "", ""
	}
	return StripHTML(m[1]), StripHTML(m[2])
}

// ---------------------------------------------------------------------------
// Results

// ArchiveStatColumn is one column of a finished game's results table. Level
// marks the columns that stand for a task, as opposed to the summary ones
// (penalty, bonuses, total time, place, gap).
type ArchiveStatColumn struct {
	Title string `json:"title"`
	Level bool   `json:"level"`
}

// ArchiveStatTeam is one team's row. Cells follows Columns.
type ArchiveStatTeam struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Cells []string `json:"cells"`
	// Notes carries the breakdowns the engine hides in a cell's title
	// attribute, keyed by column title: which levels a penalty came from and
	// what each bonus was granted for.
	Notes map[string]string `json:"notes,omitempty"`
}

// ArchiveStat is the results table of a finished game.
type ArchiveStat struct {
	GameID  int                 `json:"game_id"`
	Name    string              `json:"name"`
	Date    string              `json:"date"`
	Columns []ArchiveStatColumn `json:"columns"`
	Teams   []ArchiveStatTeam   `json:"teams"`
}

// Cell returns a team's value in the named column, or "" when the table has no
// such column.
func (s *ArchiveStat) Cell(team *ArchiveStatTeam, column string) string {
	for i, col := range s.Columns {
		if strings.EqualFold(col.Title, column) && i < len(team.Cells) {
			return team.Cells[i]
		}
	}
	return ""
}

// GetArchiveStat returns the results of a finished game: every team's time on
// every task plus the penalties, bonuses, total and place the engine computed.
//
// The page is public; a signed-out client gets the same table.
func (c *Client) GetArchiveStat(ctx context.Context, gameID int) (*ArchiveStat, error) {
	if gameID <= 0 {
		return nil, fmt.Errorf("dzzzr: archive stat: game id must be positive, got %d", gameID)
	}
	page, err := c.sitePage(ctx, "archive stat", url.Values{
		"section": {archiveSection},
		"gmid":    {strconv.Itoa(gameID)},
		"what":    {"stat"},
	})
	if err != nil {
		return nil, err
	}
	return parseArchiveStat(gameID, page)
}

const (
	errNoStatTable   = stringError("страница архива без таблицы результатов; проверьте номер игры")
	errNoDescription = stringError("страница архива без описания игры; проверьте номер игры")
)

func parseArchiveStat(gameID int, page string) (*ArchiveStat, error) {
	content := siteContent(page)
	if err := siteAccessError(content); err != nil {
		return nil, err
	}
	doc, err := parseHTML(content)
	if err != nil {
		return nil, &UndecodableResponseError{StatusCode: http.StatusOK, Context: "archive stat", Err: err}
	}
	table := nodeByID(doc.root, statTableID)
	if table == nil {
		return nil, &UndecodableResponseError{
			StatusCode: http.StatusOK, Context: "archive stat",
			Err: errNoStatTable, Body: truncate([]byte(StripHTML(content))),
		}
	}
	st := &ArchiveStat{GameID: gameID}
	st.Date, st.Name = archiveHeading(content)
	for _, tr := range tableRows(table) {
		cells := statRowCells(tr)
		if len(cells) < 2 {
			continue
		}
		if st.Columns == nil {
			// The engine marks the header row with id=shapka and writes it with
			// <th>; either signal is enough to tell it from a team.
			if strings.EqualFold(attr(tr, "id"), "shapka") || cells[0].header {
				for _, c := range cells[1:] {
					// A header is wrapped over two lines with <br> ("Чистое
					// Время"); as a column name it is one.
					title := strings.Join(strings.Fields(c.text), " ")
					st.Columns = append(st.Columns, ArchiveStatColumn{Title: title, Level: c.level})
				}
				continue
			}
		}
		id, name := teamOfCell(cells[0].node)
		if id == 0 && name == "" {
			continue
		}
		team := ArchiveStatTeam{ID: id, Name: name}
		for i, c := range cells[1:] {
			team.Cells = append(team.Cells, c.text)
			if c.note == "" {
				continue
			}
			if team.Notes == nil {
				team.Notes = map[string]string{}
			}
			team.Notes[statColumnTitle(st.Columns, i)] = c.note
		}
		st.Teams = append(st.Teams, team)
	}
	return st, nil
}

// statColumnTitle names a column for a note, falling back to its position when
// the header row had fewer cells than the data rows.
func statColumnTitle(columns []ArchiveStatColumn, i int) string {
	if i < len(columns) && columns[i].Title != "" {
		return columns[i].Title
	}
	return "столбец " + strconv.Itoa(i+1)
}

// statCell is one <td>/<th> of the results table, with the level/summary
// distinction the engine draws with comment markers around the task columns.
type statCell struct {
	node   *html.Node
	text   string
	note   string // the title attribute, where the engine puts the breakdown
	level  bool
	header bool
}

// statRowCells reads a row's cells in document order.
//
// The task columns are delimited by <!--lev--> and <!--levend-->, which is the
// engine's only marker for them: their headers look exactly like the summary
// ones and their count changes with the game.
func statRowCells(tr *html.Node) []statCell {
	var out []statCell
	inLevels := false
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.CommentNode:
			switch strings.TrimSpace(c.Data) {
			case "lev":
				inLevels = true
			case "levend":
				inLevels = false
			}
		case c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th"):
			out = append(out, statCell{
				node: c, text: text(c), note: strings.TrimSpace(attr(c, "title")),
				level: inLevels, header: c.Data == "th",
			})
		}
	}
	return out
}

// teamOfCell reads the team a row belongs to out of its first cell.
func teamOfCell(td *html.Node) (id int, name string) {
	if td == nil {
		return 0, ""
	}
	walk(td, func(n *html.Node) bool {
		if id != 0 || n.Type != html.ElementNode || n.Data != "a" {
			return true
		}
		q := linkQuery(attr(n, "href"))
		if v := q.Get("teamID"); v != "" {
			id, _ = strconv.Atoi(v)
			name = text(n)
			return false
		}
		return true
	})
	if name == "" {
		name = text(td)
	}
	return id, name
}

// nodeByID finds the first element with the given id.
func nodeByID(root *html.Node, id string) *html.Node {
	var found *html.Node
	walk(root, func(n *html.Node) bool {
		if found != nil {
			return false
		}
		if n.Type == html.ElementNode && attr(n, "id") == id {
			found = n
			return false
		}
		return true
	})
	return found
}

// tableRows returns a table's own rows. Rows of a nested table are left out:
// every team cell of the results carries one for its social widget, and those
// would otherwise be read as teams.
func tableRows(table *html.Node) []*html.Node {
	var out []*html.Node
	var descend func(*html.Node)
	descend = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "table":
			case "tr":
				out = append(out, c)
			default:
				descend(c)
			}
		}
	}
	descend(table)
	return out
}

// ---------------------------------------------------------------------------
// Scenario

// ArchiveSection is a titled block of a published scenario: the authors, the
// legend, the coverage area, or an author's commentary on a task.
type ArchiveSection struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// ArchiveSpoiler is a spoiler as published: its text and the code that opened
// it during the game.
type ArchiveSpoiler struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
	Code   string `json:"code,omitempty"`
}

// ArchiveLevel is one task of a published scenario, with everything the
// engine withheld during the game: the codes, both hints and the spoilers.
type ArchiveLevel struct {
	Title      string           `json:"title"`
	Codes      []string         `json:"codes,omitempty"`
	Task       string           `json:"task"`
	Notes      string           `json:"notes,omitempty"`      // «Примечания к заданию»
	Difficulty string           `json:"difficulty,omitempty"` // «Код сложности»
	Hint1      string           `json:"hint1,omitempty"`
	Hint2      string           `json:"hint2,omitempty"`
	Spoilers   []ArchiveSpoiler `json:"spoilers,omitempty"`
	// Sections holds the author's own headings under the task, «Комментарий»
	// above all.
	Sections []ArchiveSection `json:"sections,omitempty"`
	Media    []MediaRef       `json:"media,omitempty"`
}

// ArchiveDescription is a game's published scenario.
type ArchiveDescription struct {
	GameID   int              `json:"game_id"`
	Name     string           `json:"name"`
	Date     string           `json:"date"`
	Sections []ArchiveSection `json:"sections,omitempty"`
	Levels   []ArchiveLevel   `json:"levels,omitempty"`
	Media    []MediaRef       `json:"media,omitempty"` // the poster and anything outside a task
	// Restricted says the engine withheld the tasks because the visitor's
	// team has not earned the right to read them. The legend, the authors and
	// the coverage area come through anyway.
	Restricted bool `json:"restricted,omitzero"`
	// Notice is the engine's own explanation of that, including the games it
	// offers as samples instead.
	Notice string `json:"notice,omitempty"`
}

// GetArchiveDescription returns a finished game's published scenario: the
// legend and the authors, then every task with its codes, hints, spoilers and
// author commentary.
//
// Only the header is public. The tasks themselves are shown to the captain,
// the deputy captain or the chief of staff of a team that has played in this
// city recently; anyone else gets the header with Restricted set and the
// engine's own wording in Notice. An unreleased game carries no scenario at
// all.
func (c *Client) GetArchiveDescription(ctx context.Context, gameID int) (*ArchiveDescription, error) {
	if gameID <= 0 {
		return nil, fmt.Errorf("dzzzr: archive description: game id must be positive, got %d", gameID)
	}
	page, err := c.sitePage(ctx, "archive description", url.Values{
		"section": {archiveSection},
		"gmid":    {strconv.Itoa(gameID)},
		"what":    {"comment"},
	})
	if err != nil {
		return nil, err
	}
	return parseArchiveDescription(gameID, page, c.BaseURL())
}

var (
	h2Re          = regexp.MustCompile(`(?is)<h2[^>]*>(.*?)</h2>`)
	hintHeadRe    = regexp.MustCompile(`(?is)<h3[^>]*>\s*Подсказка\s*([12])\s*</h3>`)
	codeSpanRe    = regexp.MustCompile(`(?is)<span[^>]*\bid\s*=\s*["']?Code["']?[^>]*>(.*?)</span>`)
	taskHeadRe    = regexp.MustCompile(`^\s*Задани[ея]\s*:?\s*`)
	spoilerHeadRe = regexp.MustCompile(`^\s*Спойлер\s*(\d*)`)
	spoilerCodeRe = regexp.MustCompile(`(?im)^\s*Код\s+спойлера\s*\d*\s*:\s*(.+)$`)
	notesHeadRe   = regexp.MustCompile(`(?is)<strong>\s*Примечания к заданию\s*</strong>\s*:?`)
	difficultyRe  = regexp.MustCompile(`(?is)<strong>\s*Код сложности\s*:?\s*</strong>\s*:?`)
	// The codes of a task are separated by non-breaking spaces, and a code may
	// itself contain ordinary ones, so they are split before the entities are
	// resolved.
	nbspRe = regexp.MustCompile(`(?i)&nbsp;`)
	// The scenario carries commented-out markup the engine never renders, and
	// one of those blocks holds a half-written <fb:like> tag whose attributes
	// would otherwise be stripped down to visible text and prepended to a task.
	commentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	// The engine replaces the tasks with this paragraph, followed by a few
	// games it offers as samples, when the visitor's team has not played
	// recently enough to be shown a scenario.
	restrictedRe = regexp.MustCompile(`(?is)<p[^>]*>\s*<strong>\s*(Для просмотра[^<]{0,400}?)\s*</strong>\s*</p>`)
)

func parseArchiveDescription(gameID int, page, base string) (*ArchiveDescription, error) {
	content := siteContent(page)
	if err := siteAccessError(content); err != nil {
		return nil, err
	}
	d := &ArchiveDescription{GameID: gameID}
	d.Date, d.Name = archiveHeading(content)
	content = commentRe.ReplaceAllString(content, "")
	content = cutRestrictionNotice(d, content)

	heads := h2Re.FindAllStringSubmatchIndex(content, -1)
	if len(heads) == 0 {
		return nil, &UndecodableResponseError{
			StatusCode: http.StatusOK, Context: "archive description",
			Err: errNoDescription, Body: truncate([]byte(StripHTML(content))),
		}
	}
	// Everything before the first heading is the page's own furniture: the
	// poster, worth keeping, and the buttons that lead to the other views of
	// the same game, which are not.
	for _, ref := range ResolveMedia(base, ExtractMedia(content[:heads[0][0]])) {
		if ref.Kind == MediaImage {
			d.Media = append(d.Media, ref)
		}
	}

	var level *ArchiveLevel
	for i, h := range heads {
		heading := content[h[2]:h[3]]
		body := content[h[1]:]
		if i+1 < len(heads) {
			body = content[h[1]:heads[i+1][0]]
		}
		title := StripHTML(codeSpanRe.ReplaceAllString(heading, ""))
		// The first heading is the page's own title, and its body is the
		// checkbox form that hides the pictures.
		if i == 0 && title == "Описание" {
			continue
		}
		switch {
		case taskHeadRe.MatchString(title):
			d.Levels = append(d.Levels, ArchiveLevel{
				Title: strings.TrimSpace(taskHeadRe.ReplaceAllString(title, "")),
				Codes: parseArchiveCodes(heading),
			})
			level = &d.Levels[len(d.Levels)-1]
			fillArchiveLevel(level, body, base)
		case level != nil && spoilerHeadRe.MatchString(title):
			level.Spoilers = append(level.Spoilers, parseArchiveSpoiler(title, body))
		default:
			s := ArchiveSection{Title: strings.TrimSpace(title), Text: StripHTML(body)}
			if s.Text == "" {
				continue
			}
			if level != nil {
				level.Sections = append(level.Sections, s)
			} else {
				d.Sections = append(d.Sections, s)
			}
		}
	}
	return d, nil
}

// cutRestrictionNotice moves the engine's "your team may not read this" block
// out of the scenario and returns what is left of the page.
//
// The notice stands where the tasks would be, and everything after it — the
// games the engine offers as samples instead — belongs to it. That is only
// true while nothing else follows: were the engine to put the notice above a
// scenario it did show, taking the rest would throw the scenario away, so the
// tail is claimed only when no heading comes after it.
func cutRestrictionNotice(d *ArchiveDescription, content string) string {
	m := restrictedRe.FindStringIndex(content)
	if m == nil {
		return content
	}
	d.Restricted = true
	if h2Re.MatchString(content[m[1]:]) {
		d.Notice = StripHTML(content[m[0]:m[1]])
		return content[:m[0]] + content[m[1]:]
	}
	d.Notice = StripHTML(content[m[0]:])
	return content[:m[0]]
}

// parseArchiveCodes reads the codes out of a task heading. The engine writes
// them into one <span id=Code> as "Код: A&nbsp;B&nbsp;".
func parseArchiveCodes(heading string) []string {
	m := codeSpanRe.FindStringSubmatch(heading)
	if m == nil {
		return nil
	}
	var out []string
	for _, part := range nbspRe.Split(m[1], -1) {
		s := StripHTML(part)
		s = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "Коды:"), "Код:"))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// fillArchiveLevel splits a task's body into its parts. The engine writes them
// in a fixed order — task text, notes, difficulty line, then the two hints as
// <h3> headings — so each is cut off the end of what came before it.
func fillArchiveLevel(l *ArchiveLevel, body, base string) {
	l.Media = ResolveMedia(base, ExtractMedia(body))

	main := body
	if hints := hintHeadRe.FindAllStringSubmatchIndex(body, -1); len(hints) > 0 {
		main = body[:hints[0][0]]
		for i, h := range hints {
			end := len(body)
			if i+1 < len(hints) {
				end = hints[i+1][0]
			}
			switch body[h[2]:h[3]] {
			case "1":
				l.Hint1 = StripHTML(body[h[1]:end])
			case "2":
				l.Hint2 = StripHTML(body[h[1]:end])
			}
		}
	}
	if m := difficultyRe.FindStringIndex(main); m != nil {
		l.Difficulty = difficultyLine(StripHTML(main[m[1]:]))
		main = main[:m[0]]
	}
	if m := notesHeadRe.FindStringIndex(main); m != nil {
		l.Notes = StripHTML(main[m[1]:])
		main = main[:m[0]]
	}
	l.Task = StripHTML(main)
}

// difficultyLine drops the line the engine prints for a task whose codes were
// never given a difficulty: PHP nulls rendered one per code, which say nothing
// and read as data.
func difficultyLine(s string) string {
	for _, part := range strings.Split(s, ",") {
		switch strings.TrimSpace(part) {
		case "", "null":
		default:
			return s
		}
	}
	return ""
}

// parseArchiveSpoiler reads a spoiler block: its text and, on the last line,
// the code that opened it.
func parseArchiveSpoiler(title, body string) ArchiveSpoiler {
	sp := ArchiveSpoiler{Text: StripHTML(body)}
	if m := spoilerHeadRe.FindStringSubmatch(title); m != nil && m[1] != "" {
		sp.Number, _ = strconv.Atoi(m[1])
	}
	if m := spoilerCodeRe.FindStringSubmatchIndex(sp.Text); m != nil {
		sp.Code = strings.TrimSpace(sp.Text[m[2]:m[3]])
		sp.Text = strings.TrimSpace(sp.Text[:m[0]])
	}
	return sp
}
