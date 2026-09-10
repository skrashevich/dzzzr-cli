package dzzzr

import (
	"context"
	"encoding/base64"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// MediaKind tells an image apart from a hyperlink.
type MediaKind string

const (
	MediaImage MediaKind = "image"
	MediaLink  MediaKind = "link"
)

// MediaRef is a picture or link carried by a level's text.
type MediaRef struct {
	Kind MediaKind `json:"kind"`
	URL  string    `json:"url"`
	Text string    `json:"text,omitempty"` // a link's anchor text
	// Inline marks a data: URI, whose bytes are the picture itself rather
	// than an address to fetch. A mobile image view can decode URL as it
	// stands; a terminal cannot show it, so the CLI prints a description
	// instead of the blob.
	Inline    bool   `json:"inline,omitzero"`
	MediaType string `json:"media_type,omitempty"` // "image/jpeg" for an inline picture
	Bytes     int    `json:"bytes,omitzero"`       // size of an inline picture, estimated if it does not decode
}

var (
	imgRe    = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	anchorRe = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	// The attribute has to start a word in the tag: \b alone also matches
	// data-src and xlink:href, which would hand back a lazy-loading
	// placeholder instead of the real picture.
	attrRe = regexp.MustCompile(`(?is)(?:^|[\s"'])(src|href)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
)

// ExtractMedia lists the images and links in a level's HTML.
//
// StripHTML drops them, and a Dozor task routinely puts the code inside a
// picture — demo level 3 does exactly that — so a text-only client would show
// a task it cannot solve. Relative URLs are returned as the engine wrote
// them; use ResolveMedia to make them absolute.
func ExtractMedia(s string) []MediaRef {
	if s == "" {
		return nil
	}
	var refs []MediaRef
	for _, tag := range imgRe.FindAllString(s, -1) {
		u := attrValue(tag, "src")
		if u == "" {
			continue
		}
		if mediaType, size, ok := parseInlineImage(u); ok {
			refs = append(refs, MediaRef{
				Kind: MediaImage, URL: u,
				Inline: true, MediaType: mediaType, Bytes: size,
			})
			continue
		}
		if safeURL(u) {
			refs = append(refs, MediaRef{Kind: MediaImage, URL: u})
		}
	}
	for _, m := range anchorRe.FindAllStringSubmatch(s, -1) {
		u := attrValue(m[1], "href")
		if !safeURL(u) {
			continue
		}
		refs = append(refs, MediaRef{Kind: MediaLink, URL: u, Text: StripHTML(m[2])})
	}
	return dedupe(refs)
}

// ResolveMedia rewrites every relative URL in refs against base, which is
// the address of the page the text came from (the client's BaseURL plus
// "go/"). Refs whose URL cannot be parsed are left alone.
func ResolveMedia(base string, refs []MediaRef) []MediaRef {
	b, err := url.Parse(base)
	if err != nil {
		return refs
	}
	out := make([]MediaRef, len(refs))
	copy(out, refs)
	for i := range out {
		if out[i].Inline {
			// A data: URI is the picture, not a place to look for one.
			continue
		}
		u, err := url.Parse(out[i].URL)
		if err != nil {
			continue
		}
		out[i].URL = b.ResolveReference(u).String()
	}
	return out
}

// attrValue reads one attribute out of a tag's source.
func attrValue(tag, name string) string {
	for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
		if !strings.EqualFold(m[1], name) {
			continue
		}
		v := strings.TrimSpace(m[2])
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		return strings.TrimSpace(stripControls(html.UnescapeString(v), ""))
	}
	return ""
}

// LevelChoice is one option of the engine's next-level form.
type LevelChoice struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

var radioRe = regexp.MustCompile(`(?is)<input\b[^>]*\btype\s*=\s*["']?radio["']?[^>]*>([^<]*)`)
var valueRe = regexp.MustCompile(`(?is)\bvalue\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)

// NextLevelChoices lists the levels the team may pick next.
//
// In games with a free level order the engine does not send the numbers as
// data: templates/JSON/go.tpl puts the whole rendered form into the
// NextLevelSelector string, one radio button per level. Without reading it a
// client can only guess what SelectLevel accepts.
func (g *GameState) NextLevelChoices() []LevelChoice {
	if g == nil || g.NextLevelSelector == "" {
		return nil
	}
	var out []LevelChoice
	for _, m := range radioRe.FindAllStringSubmatch(g.NextLevelSelector, -1) {
		v := valueRe.FindStringSubmatch(m[0])
		if v == nil {
			continue
		}
		raw := strings.Trim(strings.TrimSpace(v[1]), `"'`)
		// order_p is one-based, and parseLevel refuses a zero level, so a
		// zero here would be a choice the client then declines to make.
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || n <= 0 {
			continue
		}
		out = append(out, LevelChoice{Number: n, Title: StripHTML(m[1])})
	}
	return out
}

// parseInlineImage recognizes a data: URI that carries a picture and reports
// its declared type and decoded size.
//
// A task may embed a picture instead of linking one, and dropping it would
// lose the task: the code is often in the picture. It is kept whole here so a
// mobile client can render it; only a terminal cannot, and the CLI describes
// it rather than printing the bytes. Anything that is not an image — a
// data:text/html payload, say — is not media and does not come back.
func parseInlineImage(u string) (mediaType string, size int, ok bool) {
	if len(u) < len("data:") || !strings.EqualFold(u[:len("data:")], "data:") {
		return "", 0, false
	}
	header, payload, found := strings.Cut(u[len("data:"):], ",")
	if !found {
		return "", 0, false
	}
	isBase64 := false
	if rest, cut := strings.CutSuffix(header, ";base64"); cut {
		header, isBase64 = rest, true
	}
	mediaType = strings.ToLower(strings.TrimSpace(strings.Split(header, ";")[0]))
	if !strings.HasPrefix(mediaType, "image/") {
		return "", 0, false
	}
	return mediaType, payloadSize(payload, isBase64), true
}

// payloadSize is how many bytes a data: URI's payload stands for. It is an
// estimate when the payload does not decode: the URI is handed on whole
// either way, since an image decoder may be more lenient than this, and the
// size only ever labels the picture for a human.
func payloadSize(payload string, isBase64 bool) int {
	payload = strings.TrimSpace(payload)
	if isBase64 {
		if decoded, err := base64.StdEncoding.DecodeString(payload); err == nil {
			return len(decoded)
		}
		return base64.StdEncoding.DecodedLen(len(payload))
	}
	if unescaped, err := url.QueryUnescape(payload); err == nil {
		return len(unescaped)
	}
	return len(payload)
}

// safeURL reports whether a link may be shown. The scheme list is an
// allowlist rather than a denylist of javascript: and friends, because the
// output reaches a terminal that may turn it into something clickable and
// case and spelling are the attacker's to choose.
func safeURL(u string) bool {
	if u == "" || strings.HasPrefix(u, "#") {
		return false
	}
	scheme, _, found := strings.Cut(u, ":")
	if !found {
		return true // relative
	}
	// "//host/path" and "path?a=b:c" carry no scheme of their own.
	if strings.ContainsAny(scheme, "/?#") {
		return true
	}
	switch strings.ToLower(scheme) {
	case "http", "https", "mailto", "tel":
		return true
	}
	return false
}

// dedupe drops repeats, which a task that shows one picture twice would
// otherwise list twice.
func dedupe(refs []MediaRef) []MediaRef {
	if len(refs) < 2 {
		return refs
	}
	seen := make(map[MediaRef]bool, len(refs))
	out := refs[:0:0]
	for _, r := range refs {
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// LevelMedia lists the pictures and links a level carries — its task text,
// its issued hints and any opened spoiler — with remote addresses resolved
// against the game page. An inline picture keeps its data: URI, which a
// mobile image view decodes directly.
func (c *Client) LevelMedia(l *Level) []MediaRef {
	if l == nil {
		return nil
	}
	refs := ExtractMedia(l.Question)
	for _, sp := range l.Spoilers {
		refs = append(refs, ExtractMedia(sp.Text)...)
	}
	refs = append(refs, ExtractMedia(l.Hint1)...)
	refs = append(refs, ExtractMedia(l.Hint2)...)
	refs = dedupe(refs)
	if len(refs) == 0 {
		return nil
	}
	return ResolveMedia(strings.TrimSuffix(c.BaseURL(), "/")+"/go/", refs)
}

// CurrentLevelMedia fetches the game and returns the current level's media.
func (c *Client) CurrentLevelMedia(ctx context.Context) ([]MediaRef, error) {
	st, err := c.GetGame(ctx)
	if err != nil {
		return nil, err
	}
	return c.LevelMedia(st.Level), nil
}
