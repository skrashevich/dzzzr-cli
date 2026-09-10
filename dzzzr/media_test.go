package dzzzr_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// Demo level 3 carries its code in a picture and links out to two channels.
// StripHTML leaves none of that, so a text-only client shows a task it cannot
// solve.
func TestExtractMediaFindsPicturesAndLinks(t *testing.T) {
	const question = `<p>Выглядит это примерно так:</p>` +
		`<p><img src="../../uploaded/demo/Night/x_68417492.jpg" alt="" width="419" height="314" /></p>` +
		`<p>Вот две ссылки на канал в <a href="http://t.me/questDozoR">телеграм</a> и ` +
		`<a href='http://zen.yandex.ru/id/5af1d9'>yandex.zen</a></p>`
	refs := dzzzr.ExtractMedia(question)
	if len(refs) != 3 {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0].Kind != dzzzr.MediaImage || refs[0].URL != "../../uploaded/demo/Night/x_68417492.jpg" {
		t.Errorf("image = %+v", refs[0])
	}
	if refs[1].Kind != dzzzr.MediaLink || refs[1].URL != "http://t.me/questDozoR" || refs[1].Text != "телеграм" {
		t.Errorf("link = %+v", refs[1])
	}
	if refs[2].URL != "http://zen.yandex.ru/id/5af1d9" {
		t.Errorf("single-quoted href = %+v", refs[2])
	}

	resolved := dzzzr.ResolveMedia("https://classic.dzzzr.ru/demo/go/", refs)
	if resolved[0].URL != "https://classic.dzzzr.ru/uploaded/demo/Night/x_68417492.jpg" {
		t.Errorf("resolved image = %q", resolved[0].URL)
	}
	if resolved[1].URL != refs[1].URL {
		t.Errorf("absolute link rewritten: %q", resolved[1].URL)
	}
}

func TestExtractMediaSkipsAnchorsWithoutATarget(t *testing.T) {
	refs := dzzzr.ExtractMedia(`<a name="top">якорь</a><a href="#top">вверх</a><a href="javascript:void(0)">x</a>`)
	if len(refs) != 0 {
		t.Errorf("refs = %+v", refs)
	}
}

// In games with a free level order the engine sends the choices only as a
// rendered form inside NextLevelSelector.
func TestNextLevelChoices(t *testing.T) {
	st := &dzzzr.GameState{NextLevelSelector: `<form method=post><input name="action" type="hidden" value="selectlevel">` +
		`<input name="llevel" type="hidden" value=3>` +
		`<input type=radio name=level value=4 checked>Гаражи<br>` +
		`<input type=radio name=level value=7 >Пустырь<br>` +
		`<input type=submit value="выбрать уровень"></form>`}
	got := st.NextLevelChoices()
	if len(got) != 2 {
		t.Fatalf("choices = %+v", got)
	}
	if got[0] != (dzzzr.LevelChoice{Number: 4, Title: "Гаражи"}) || got[1] != (dzzzr.LevelChoice{Number: 7, Title: "Пустырь"}) {
		t.Errorf("choices = %+v", got)
	}
	if len((&dzzzr.GameState{}).NextLevelChoices()) != 0 {
		t.Error("an empty selector must offer nothing")
	}
}

// Level HTML is written by the organizer and its links are printed straight
// to a terminal, so an escape sequence must not survive extraction.
func TestExtractMediaStripsTerminalControls(t *testing.T) {
	in := "<a href=\"http://e.x/\x1b[31mred\">под\x1bсказка</a><img src=\"a\x07.jpg\">"
	refs := dzzzr.ExtractMedia(in)
	if len(refs) != 2 {
		t.Fatalf("refs = %+v", refs)
	}
	for _, r := range refs {
		if strings.ContainsFunc(r.URL, unicode.IsControl) || strings.ContainsFunc(r.Text, unicode.IsControl) {
			t.Errorf("control character survived: %+v", r)
		}
	}
	// Pictures are listed before links.
	if refs[0].URL != "a.jpg" {
		t.Errorf("image = %+v", refs[0])
	}
	if refs[1].URL != "http://e.x/[31mred" || refs[1].Text != "подсказка" {
		t.Errorf("link = %+v", refs[1])
	}
}

// Everything StripHTML is given comes from someone else and ends up on a
// terminal, so escape sequences must not survive it either — while the line
// breaks it builds its layout from must.
func TestStripHTMLDropsTerminalControlsAndKeepsBreaks(t *testing.T) {
	got := dzzzr.StripHTML("<p>Код \x1b[2J\x07один</p><p>и\x1bдва</p>")
	for _, r := range got {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("control %q survived in %q", r, got)
		}
	}
	// Only the control bytes go; what they addressed stays as ordinary text,
	// and the paragraph break survives.
	if got != "Код [2Jодин\nидва" {
		t.Errorf("got = %q", got)
	}
}

// The scheme filter is an allowlist: case and spelling belong to whoever
// wrote the level, and the result reaches a terminal that may make it
// clickable.
func TestExtractMediaAllowsOnlySafeSchemes(t *testing.T) {
	for _, href := range []string{
		"javascript:alert(1)", "JavaScript:alert(1)", "JAVASCRIPT:x",
		"data:text/html;base64,PHNjcmlwdD4=", "DATA:text/html,x",
		"vbscript:msgbox", "file:///etc/passwd", "#anchor",
	} {
		if got := dzzzr.ExtractMedia(`<a href="` + href + `">x</a>`); len(got) != 0 {
			t.Errorf("%q was let through: %+v", href, got)
		}
	}
	for _, href := range []string{
		"http://e.x/a", "HTTPS://e.x/a", "mailto:org@dzzzr.ru", "tel:+70000000000",
		"/uploaded/a.jpg", "../../uploaded/a.jpg", "//cdn.example/a.jpg", "a?b=c:d",
	} {
		if got := dzzzr.ExtractMedia(`<a href="` + href + `">x</a>`); len(got) != 1 {
			t.Errorf("%q was dropped: %+v", href, got)
		}
	}
}

// A remote picture's address is filtered like a link's, but a picture the
// organizer embedded is the task itself and must survive: a mobile client
// renders it, and dropping it would lose a code that is often only there.
func TestExtractMediaFiltersImageSchemes(t *testing.T) {
	if got := dzzzr.ExtractMedia(`<img src="javascript:alert(1)">`); len(got) != 0 {
		t.Errorf("refs = %+v", got)
	}
	if got := dzzzr.ExtractMedia(`<img src="/uploaded/a.jpg">`); len(got) != 1 {
		t.Errorf("refs = %+v", got)
	}
	// data: is media only when it carries an image.
	if got := dzzzr.ExtractMedia(`<img src="data:text/html;base64,PHNjcmlwdD4=">`); len(got) != 0 {
		t.Errorf("a non-image data URI was kept: %+v", got)
	}
}

// An inline picture keeps its URI whole and describes itself, so a terminal
// can name it and a mobile image view can decode it.
func TestExtractMediaKeepsInlineImages(t *testing.T) {
	// A one-pixel GIF, the smallest honest example.
	const gif = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"
	refs := dzzzr.ExtractMedia(`<p>Код на картинке:</p><img src="` + gif + `" alt="">`)
	if len(refs) != 1 {
		t.Fatalf("refs = %+v", refs)
	}
	r := refs[0]
	if !r.Inline || r.Kind != dzzzr.MediaImage {
		t.Fatalf("ref = %+v", r)
	}
	if r.URL != gif {
		t.Errorf("the URI was rewritten: %q", r.URL)
	}
	if r.MediaType != "image/gif" {
		t.Errorf("media type = %q", r.MediaType)
	}
	if r.Bytes != 42 {
		t.Errorf("bytes = %d, want the decoded size 42", r.Bytes)
	}
	// Resolving must leave it alone: it is the picture, not an address.
	if got := dzzzr.ResolveMedia("https://classic.dzzzr.ru/demo/go/", refs); got[0].URL != gif {
		t.Errorf("resolved = %q", got[0].URL)
	}
}

// A percent-encoded inline picture reports its size too.
func TestExtractMediaSizesPlainInlineImages(t *testing.T) {
	refs := dzzzr.ExtractMedia(`<img src="data:image/svg+xml,%3Csvg%2F%3E">`)
	if len(refs) != 1 || !refs[0].Inline {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0].MediaType != "image/svg+xml" || refs[0].Bytes != len("<svg/>") {
		t.Errorf("ref = %+v", refs[0])
	}
}

// data-src holds a lazy-loading placeholder; src holds the picture.
func TestExtractMediaIgnoresLookalikeAttributes(t *testing.T) {
	got := dzzzr.ExtractMedia(`<img data-src="placeholder.gif" src="real.jpg">`)
	if len(got) != 1 || got[0].URL != "real.jpg" {
		t.Fatalf("refs = %+v", got)
	}
}

// A task that shows one picture twice should list it once.
func TestExtractMediaDeduplicates(t *testing.T) {
	got := dzzzr.ExtractMedia(`<img src="a.jpg"><p>и ещё раз</p><img src="a.jpg">`)
	if len(got) != 1 {
		t.Fatalf("refs = %+v", got)
	}
}
