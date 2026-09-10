package dzzzr

import (
	"net/url"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// The administration area is server-rendered PHP with no JSON. Reading it
// means walking HTML: lists are tables of rows with an edit link, details are
// <form> elements whose inputs carry the current values. This file holds the
// small parser both need. It deliberately understands only what the engine
// emits: forms, inputs, selects, textareas, links and table rows.

// htmlForm is one <form> with its field values as a browser would submit
// them (checked checkboxes, selected options, textarea text).
type htmlForm struct {
	Action  string
	Method  string
	Name    string
	Fields  url.Values
	Selects map[string][]htmlOption
	// Checkboxes lists every checkbox name, checked or not, so a caller can
	// tell "unchecked" from "not on the form".
	Checkboxes map[string]bool
}

type htmlOption struct {
	Value    string
	Text     string
	Selected bool
}

// htmlRow is one <tr>: the text of its direct <td>/<th> children (nested
// tables flattened into the cell text) and every link found inside.
type htmlRow struct {
	Cells []string
	Links []htmlLink
	Attrs map[string]string
	// Images lists the src of every picture in the row. The engine marks an
	// unpublished game or level, and a blocked team, with a dimmed icon
	// (vw_d.gif); its bgcolor cannot be used for that, because a selected row
	// carries the attribute twice and an HTML parser keeps only the first.
	Images []string
	// Hidden collects every hidden input inside the row. The engine writes
	// per-row delete forms inside <tr>, which the HTML parser strips of their
	// <form> wrapper, so the fields are read from the row itself.
	Hidden url.Values
}

type htmlLink struct {
	Href string
	Text string
}

// htmlDoc is a parsed page.
type htmlDoc struct {
	root *html.Node
	// raw keeps the source, because a form the engine spreads across table
	// structure cannot be recovered from the tree. See rawFormWithAction.
	raw string
}

func parseHTML(s string) (*htmlDoc, error) {
	n, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return nil, err
	}
	return &htmlDoc{root: n, raw: s}, nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return true
		}
	}
	return false
}

func walk(n *html.Node, fn func(*html.Node) bool) {
	if !fn(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

// text returns the visible text of a subtree, with <br> as newlines and
// whitespace collapsed the way StripHTML does.
func text(n *html.Node) string {
	var sb strings.Builder
	walk(n, func(c *html.Node) bool {
		switch c.Type {
		case html.TextNode:
			sb.WriteString(c.Data)
		case html.ElementNode:
			switch c.Data {
			case "br", "p", "div", "tr", "li", "option":
				sb.WriteString("\n")
			case "script", "style":
				return false
			case "td", "th":
				sb.WriteString(" ")
			}
		}
		return true
	})
	return StripHTML(sb.String())
}

// forms returns every form on the page.
func (d *htmlDoc) forms() []*htmlForm {
	var out []*htmlForm
	walk(d.root, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "form" {
			out = append(out, parseFormNode(n))
			return false
		}
		return true
	})
	return out
}

// formWithAction returns the first form whose hidden "action" field equals
// name, or nil. A form the tree lost is rebuilt from the source.
func (d *htmlDoc) formWithAction(name string) *htmlForm {
	for _, f := range d.forms() {
		if f.Fields.Get("action") == name {
			return f
		}
	}
	return d.rawFormWithAction(name)
}

// actionInputRe matches the engine's hidden action field, whose attributes
// are written unquoted.
var actionInputRe = regexp.MustCompile(`(?is)<input[^>]*>`)

// rawFormWithAction rebuilds a form the HTML parser dropped. The engine
// leaves a <form> on its team page unclosed, and the HTML spec says a <form>
// start tag is ignored while a form element is already open, so every later
// form on that page vanishes and its controls attach to the unclosed one.
// Measured against classic.dzzzr.ru on 2026-09-10: the organizer's team card
// holds 219 controls — 184 of them the roster's pl[<login>] — and the tree
// offers no form carrying them, so reading a team answered "no updateTeam
// form" and the application list came back empty.
//
// Slicing the source between the <form> that precedes the action marker and
// the next </form> recovers exactly that form, so the caller sees what a
// browser would submit.
func (d *htmlDoc) rawFormWithAction(name string) *htmlForm {
	at := -1
	for _, m := range actionInputRe.FindAllStringIndex(d.raw, -1) {
		tag := d.raw[m[0]:m[1]]
		if fieldNameOfTag(tag) == "action" && attrValueOfTag(tag, "value") == name {
			at = m[0]
			break
		}
	}
	if at < 0 {
		return nil
	}
	start := strings.LastIndex(d.raw[:at], "<form")
	if start < 0 {
		return nil
	}
	end := formClose(d.raw, at)
	if end < 0 {
		return nil
	}
	frag, err := parseHTML(d.raw[start:end])
	if err != nil {
		return nil
	}
	for _, f := range frag.forms() {
		if f.Fields.Get("action") == name {
			return f
		}
	}
	return nil
}

// formClose returns the offset just past the </form> that ends the form
// holding the control at position at, skipping the ones that close forms
// opened after it. The engine does emit <form>…</form> pairs inside a row
// (see the per-row delete forms on its list pages), and stopping at the
// first </form> would cut the form short. That matters beyond reading:
// AdminSetApplication reposts the whole form it read, so a slice that ended
// early would drop the team members it never saw.
func formClose(raw string, at int) int {
	depth := 0
	for i := at; i < len(raw); {
		open := strings.Index(raw[i:], "<form")
		closed := strings.Index(raw[i:], "</form>")
		switch {
		case closed < 0:
			return -1
		case open >= 0 && open < closed:
			depth++
			i += open + len("<form")
		case depth > 0:
			depth--
			i += closed + len("</form>")
		default:
			return i + closed + len("</form>")
		}
	}
	return -1
}

var tagAttrRe = regexp.MustCompile(`(?is)([a-z_][a-z0-9_-]*)\s*=\s*("[^"]*"|'[^']*'|[^\s>]*)`)

// attrValueOfTag reads one attribute out of a raw tag, tolerating the
// unquoted values the engine writes. The value is returned as written, minus
// its quotes: the stray-quote repair belongs to the name, not to every
// attribute — see fieldNameOfTag.
func attrValueOfTag(tag, key string) string {
	for _, m := range tagAttrRe.FindAllStringSubmatch(tag, -1) {
		if strings.EqualFold(m[1], key) {
			return strings.Trim(m[2], `"'`)
		}
	}
	return ""
}

// fieldNameOfTag reads a raw tag's name attribute, applying the same repair
// fieldName applies to a parsed node.
func fieldNameOfTag(tag string) string {
	return strings.TrimRight(attrValueOfTag(tag, "name"), `"'`)
}

// fieldName reads a control's name, repairing the engine's own malformed
// markup. The level editor writes the difficulty and sector selects as
// name=danger[0]" — an unquoted attribute with a stray closing quote — and an
// HTML parser keeps that quote inside the value. Measured against
// classic.dzzzr.ru on 2026-09-09: danger[N]", sector[N]" and dangerB[N]" come
// back that way, 3503 controls on a single level page.
//
// Trimming it matters beyond reading: the update path re-posts the form it
// read, so the broken names went back to the engine while the correct ones
// were never sent. A title-only admin-update-level therefore moved one code's
// difficulty onto another code, dropped the rest, and left the level
// unpublished, because the engine only publishes a level whose codes carry a
// difficulty.
func fieldName(n *html.Node) string {
	return strings.TrimRight(attr(n, "name"), `"'`)
}

func parseFormNode(form *html.Node) *htmlForm {
	f := &htmlForm{
		Action:     attr(form, "action"),
		Method:     strings.ToUpper(attr(form, "method")),
		Name:       attr(form, "name"),
		Fields:     url.Values{},
		Selects:    map[string][]htmlOption{},
		Checkboxes: map[string]bool{},
	}
	walk(form, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return true
		}
		switch n.Data {
		case "input":
			name := fieldName(n)
			if name == "" {
				return true
			}
			switch strings.ToLower(attr(n, "type")) {
			case "checkbox":
				checked := hasAttr(n, "checked")
				f.Checkboxes[name] = checked
				if checked {
					v := attr(n, "value")
					if !hasAttr(n, "value") {
						v = "on"
					}
					f.Fields.Add(name, v)
				}
			case "radio":
				if hasAttr(n, "checked") {
					f.Fields.Set(name, attr(n, "value"))
				}
			case "submit", "button", "image", "reset", "file":
				// not part of the submitted state
			default:
				f.Fields.Add(name, attr(n, "value"))
			}
		case "textarea":
			if name := fieldName(n); name != "" {
				f.Fields.Add(name, innerText(n))
			}
		case "select":
			name := fieldName(n)
			if name == "" {
				return true
			}
			var opts []htmlOption
			selected := ""
			hasSelected := false
			walk(n, func(o *html.Node) bool {
				if o.Type == html.ElementNode && o.Data == "option" {
					v := attr(o, "value")
					t := strings.TrimSpace(innerText(o))
					if !hasAttr(o, "value") {
						v = t
					}
					sel := hasAttr(o, "selected")
					opts = append(opts, htmlOption{Value: v, Text: t, Selected: sel})
					// A browser submits the LAST selected option of a
					// single select. The engine marks both the empty
					// placeholder and the real value as selected on the
					// difficulty selects, so taking the first one read
					// every difficulty as empty.
					if sel {
						selected, hasSelected = v, true
					}
					return false
				}
				return true
			})
			if !hasSelected && len(opts) > 0 && !hasAttr(n, "multiple") {
				selected = opts[0].Value
			}
			f.Selects[name] = opts
			if hasAttr(n, "multiple") {
				// A browser submits every selected option of a multi-select.
				for _, o := range opts {
					if o.Selected {
						f.Fields.Add(name, o.Value)
					}
				}
				return false
			}
			if hasSelected || len(opts) > 0 {
				f.Fields.Add(name, selected)
			}
			return false
		}
		return true
	})
	return f
}

// innerText concatenates text nodes without HTML processing (for textarea
// and option contents, which the engine writes verbatim).
func innerText(n *html.Node) string {
	var sb strings.Builder
	walk(n, func(c *html.Node) bool {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
		return true
	})
	return sb.String()
}

// rows returns every table row on the page in document order.
func (d *htmlDoc) rows() []htmlRow {
	var out []htmlRow
	walk(d.root, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "tr" {
			out = append(out, parseRow(n))
			// Nested tables produce their own rows too; keep walking.
		}
		return true
	})
	return out
}

func parseRow(tr *html.Node) htmlRow {
	row := htmlRow{Attrs: map[string]string{}, Hidden: url.Values{}}
	for _, a := range tr.Attr {
		key := strings.ToLower(a.Key)
		if _, seen := row.Attrs[key]; !seen {
			row.Attrs[key] = a.Val
		}
	}
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch c.Data {
		case "td", "th":
			row.Cells = append(row.Cells, text(c))
		case "form":
			// A <form> is not valid inside <tr>; the parser usually hoists it
			// out, but when it survives its cells still belong to this row.
			f := parseFormNode(c)
			for k, vs := range f.Fields {
				row.Hidden[k] = append(row.Hidden[k], vs...)
			}
			for cc := c.FirstChild; cc != nil; cc = cc.NextSibling {
				if cc.Type == html.ElementNode && (cc.Data == "td" || cc.Data == "th") {
					row.Cells = append(row.Cells, text(cc))
				}
			}
		}
	}
	walk(tr, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "img" {
			if src := attr(n, "src"); src != "" {
				row.Images = append(row.Images, src)
			}
			return false
		}
		if n.Type == html.ElementNode && n.Data == "input" && strings.EqualFold(attr(n, "type"), "hidden") {
			if name := attr(n, "name"); name != "" {
				row.Hidden.Set(name, attr(n, "value"))
			}
			return false
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			row.Links = append(row.Links, htmlLink{Href: attr(n, "href"), Text: text(n)})
			return false
		}
		return true
	})
	return row
}

// headerIndex maps the text of a header row's cells to their positions, so a
// data row can be read by column name instead of by counting spacers. The
// engine renders both rows with the same cell layout.
func headerIndex(rows []htmlRow) (map[string]int, bool) {
	for _, r := range rows {
		if !strings.EqualFold(r.Attrs["bgcolor"], "a1a1a1") {
			continue
		}
		idx := map[string]int{}
		for i, c := range r.Cells {
			c = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(c, "\n", " ")))
			if c != "" {
				idx[c] = i
			}
		}
		if len(idx) > 0 {
			return idx, true
		}
	}
	return nil, false
}

// cellByHeader returns the trimmed cell of the column whose header matches
// one of the given names, exactly first and by prefix after that. Header
// names are compared in a fixed order rather than by ranging over the map,
// so a page whose headers share a prefix parses the same way every run.
func (r htmlRow) cellByHeader(idx map[string]int, prefixes ...string) string {
	names := make([]string, 0, len(idx))
	for name := range idx {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, p := range prefixes {
		p = strings.ToLower(p)
		if i, ok := idx[p]; ok && i < len(r.Cells) {
			return strings.TrimSpace(r.Cells[i])
		}
		for _, name := range names {
			if i := idx[name]; strings.HasPrefix(name, p) && i < len(r.Cells) {
				return strings.TrimSpace(r.Cells[i])
			}
		}
	}
	return ""
}

// linkQuery parses the query of an engine link ("?action=games&edit=1&id=5").
func linkQuery(href string) url.Values {
	i := strings.Index(href, "?")
	if i < 0 {
		return url.Values{}
	}
	q, err := url.ParseQuery(href[i+1:])
	if err != nil {
		return url.Values{}
	}
	return q
}

// linkWithParams returns the first link of the row whose query has all the
// given key/value pairs.
func (r htmlRow) linkWithParams(kv ...string) (htmlLink, url.Values, bool) {
	for _, l := range r.Links {
		q := linkQuery(l.Href)
		ok := true
		for i := 0; i+1 < len(kv); i += 2 {
			if q.Get(kv[i]) != kv[i+1] {
				ok = false
				break
			}
		}
		if ok && len(q) > 0 {
			return l, q, true
		}
	}
	return htmlLink{}, nil, false
}

// selected reports whether the row is the item currently open in the form
// below the list.
func (r htmlRow) selected() bool { return strings.EqualFold(r.Attrs["bgcolor"], "FFFEDE") }

// dimmed reports whether the engine drew the row as inactive: an unpublished
// game or level, or a blocked team. The icon is the reliable signal, since
// the grey background is written as a second bgcolor attribute that an HTML
// parser discards. A selected row shows neither — it carries the arrow icon
// and the highlight — so its state has to come from the form instead.
func (r htmlRow) dimmed() bool {
	if strings.EqualFold(r.Attrs["bgcolor"], "F0F0F0") {
		return true
	}
	for _, src := range r.Images {
		if strings.Contains(src, "vw_d") {
			return true
		}
	}
	return false
}
