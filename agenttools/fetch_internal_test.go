package agenttools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

// allowPrivate lets one test reach an httptest server, which always listens on
// a loopback address the guard refuses in earnest.
func allowPrivate(t *testing.T) {
	t.Helper()
	fetchAllowPrivateHosts.Store(true)
	t.Cleanup(func() { fetchAllowPrivateHosts.Store(false) })
}

// serve starts a page server for one test.
func serve(t *testing.T, contentType string, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fetchMap(t *testing.T, url string, maxBytes, offset int) map[string]any {
	t.Helper()
	got, err := fetchURL(t.Context(), url, maxBytes, offset)
	if err != nil {
		t.Fatalf("fetchURL(%s): %v", url, err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("fetchURL вернул %T, ожидалась карта", got)
	}
	return m
}

// The guard is the whole point of the tool: a link the user pasted is
// attacker-controlled, and a hostname that resolves to the loopback or to the
// local network looks perfectly public until DNS answers.
func TestFetchURLRefusesNonPublicAddresses(t *testing.T) {
	srv := serve(t, "text/plain", []byte("секрет"))

	_, err := fetchURL(t.Context(), srv.URL, 0, 0)
	if err == nil {
		t.Fatal("fetchURL прочитал loopback-адрес")
	}
	if !strings.Contains(err.Error(), "непубличный адрес") {
		t.Errorf("ошибка = %v, ожидался отказ по адресу", err)
	}
}

func TestFetchDialControlVerdicts(t *testing.T) {
	for _, tc := range []struct {
		address string
		refuse  bool
	}{
		{address: "127.0.0.1:80", refuse: true},
		{address: "10.1.2.3:443", refuse: true},
		{address: "192.168.0.1:80", refuse: true},
		{address: "169.254.169.254:80", refuse: true}, // cloud metadata
		{address: "100.100.100.200:80", refuse: true}, // Alibaba metadata, CGNAT range
		{address: "[::ffff:127.0.0.1]:80", refuse: true},
		{address: "[::127.0.0.1]:80", refuse: true}, // IPv4-compatible IPv6
		{address: "[fc00::1]:80", refuse: true},
		{address: "0.0.0.0:80", refuse: true},
		{address: "168.63.129.16:80", refuse: true},          // Azure WireServer: globally routable on paper
		{address: "240.0.0.1:80", refuse: true},              // reserved, handed out inside AWS EKS/Fargate
		{address: "255.255.255.255:80", refuse: true},        // broadcast, inside 240/4
		{address: "[2002:7f00:1::1]:80", refuse: true},       // 6to4 wrapping 127.0.0.1
		{address: "[2001::7f00:1]:80", refuse: true},         // Teredo
		{address: "[64:ff9b:1::a9fe:a9fe]:80", refuse: true}, // local-use NAT64 wrapping 169.254.169.254
		{address: "93.184.216.34:80", refuse: false},
		{address: "[2606:2800:220:1:248:1893:25c8:1946]:443", refuse: false},
	} {
		t.Run(tc.address, func(t *testing.T) {
			err := fetchDialControl("tcp", tc.address, nil)
			if tc.refuse && err == nil {
				t.Errorf("адрес %s пропущен", tc.address)
			}
			if !tc.refuse && err != nil {
				t.Errorf("адрес %s отклонён: %v", tc.address, err)
			}
		})
	}
}

func TestFetchURLRefusesOtherSchemes(t *testing.T) {
	for _, raw := range []string{"ftp://example.com/x", "file:///etc/passwd", "gopher://example.com"} {
		if _, err := fetchURL(t.Context(), raw, 0, 0); err == nil {
			t.Errorf("fetchURL принял %q", raw)
		} else if !strings.Contains(err.Error(), "http и https") {
			t.Errorf("%q: ошибка = %v", raw, err)
		}
	}
}

func TestFetchURLRendersHTMLAsReadableText(t *testing.T) {
	allowPrivate(t)
	page := `<html><head><title> Уровень  3 </title></head><body>
		<script>var secret = "не показывать";</script>
		<style>.a{color:red}</style>
		<h1>Задание</h1>
		<p><b>Дата:</b> 11.09.2020</p>
		<ul><li>первый</li><li>второй</li></ul>
	</body></html>`
	srv := serve(t, "text/html; charset=utf-8", []byte(page))

	got := fetchMap(t, srv.URL, 0, 0)
	if got["title"] != "Уровень 3" {
		t.Errorf("заголовок = %q", got["title"])
	}
	text, _ := got["content"].(string)
	if strings.Contains(text, "secret") || strings.Contains(text, "color:red") {
		t.Errorf("тело script/style попало в текст: %q", text)
	}
	// The space after </b> belongs to the next text node; collapsing nodes
	// independently would glue the label to its value.
	if !strings.Contains(text, "Дата: 11.09.2020") {
		t.Errorf("пробел после тега потерян: %q", text)
	}
	for _, want := range []string{"Задание", "первый", "второй"} {
		if !strings.Contains(text, want) {
			t.Errorf("в тексте нет %q: %q", want, text)
		}
	}
	if strings.Contains(text, "\n\n\n") {
		t.Errorf("вложенные блоки размножили пустые строки: %q", text)
	}
}

// Half the Russian question-pack mirrors are still windows-1251; decoded as
// UTF-8 they reach the model as replacement characters.
func TestFetchURLDecodesDeclaredCharset(t *testing.T) {
	allowPrivate(t)
	body, err := charmap.Windows1251.NewEncoder().Bytes([]byte("Штаб на Тверской"))
	if err != nil {
		t.Fatal(err)
	}
	srv := serve(t, "text/plain; charset=windows-1251", body)

	got := fetchMap(t, srv.URL, 0, 0)
	if content, _ := got["content"].(string); !strings.Contains(content, "Штаб на Тверской") {
		t.Errorf("содержимое = %q, кодировка не распознана", content)
	}
}

// A charset declared only in the document's own meta tag must work too: static
// mirrors routinely serve "text/html" with no charset parameter.
func TestFetchURLDecodesCharsetFromMetaTag(t *testing.T) {
	allowPrivate(t)
	page := `<html><head><meta charset="windows-1251"><title>Штаб</title></head><body>Тверская</body></html>`
	body, err := charmap.Windows1251.NewEncoder().Bytes([]byte(page))
	if err != nil {
		t.Fatal(err)
	}
	srv := serve(t, "text/html", body)

	got := fetchMap(t, srv.URL, 0, 0)
	if got["title"] != "Штаб" {
		t.Errorf("заголовок = %q", got["title"])
	}
	if content, _ := got["content"].(string); !strings.Contains(content, "Тверская") {
		t.Errorf("содержимое = %q", content)
	}
}

// Truncation only helps if the model is told how to continue: a bare
// "truncated": true makes it retry the same URL with a bigger budget.
func TestFetchURLPagesWithOffset(t *testing.T) {
	allowPrivate(t)
	full := strings.Repeat("щ", 200) // two bytes per rune: paging must not split one
	srv := serve(t, "text/plain; charset=utf-8", []byte(full))

	first := fetchMap(t, srv.URL, 101, 0)
	content, _ := first["content"].(string)
	if content == full {
		t.Fatal("текст не был обрезан")
	}
	if first["truncated"] != true {
		t.Errorf("truncated = %v", first["truncated"])
	}
	// The budget is odd and the runes are two bytes wide, so the cut has to
	// step back to a boundary rather than leave half a character.
	if !strings.HasPrefix(full, content) || len(content) != 100 {
		t.Fatalf("первая часть длиной %d байт: %q", len(content), content)
	}

	next, ok := first["next_offset"].(int)
	if !ok {
		t.Fatalf("next_offset = %v (%T)", first["next_offset"], first["next_offset"])
	}
	second := fetchMap(t, srv.URL, 0, next)
	rest, _ := second["content"].(string)
	if content+rest != full {
		t.Errorf("склейка частей не даёт исходный текст: %q + %q", content, rest)
	}
	if second["truncated"] != false {
		t.Errorf("вторая часть помечена обрезанной")
	}
}

func TestFetchURLReportsHTTPFailure(t *testing.T) {
	allowPrivate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "нет такой страницы", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, err := fetchURL(t.Context(), srv.URL, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("ошибка = %v, ожидался HTTP 404", err)
	}
}

func TestFetchURLRefusesBinaryContent(t *testing.T) {
	allowPrivate(t)
	srv := serve(t, "image/png", []byte{0x89, 'P', 'N', 'G', 0, 1, 2, 3})

	if _, err := fetchURL(t.Context(), srv.URL, 0, 0); err == nil {
		t.Error("fetchURL вернул двоичные данные как текст")
	}
}

// A tool nobody can see is not a tool: fetch_url must reach the model under
// every policy, because reading a page changes nothing in the game.
func TestCatalogOffersFetchURL(t *testing.T) {
	for _, policy := range []Policy{PolicyReadonly, PolicyFull} {
		t.Run(string(policy), func(t *testing.T) {
			c, err := NewCatalog(&pdfTestEngine{}, Options{Policy: policy, FilesRoot: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			tool, ok := c.Lookup("fetch_url")
			if !ok {
				t.Fatal("в каталоге нет fetch_url")
			}
			if tool.Mutating() {
				t.Error("fetch_url помечен изменяющим состояние игры")
			}
		})
	}
}

// Paging is only useful if every call moves forward and the parts rejoin into
// exactly the original. A budget smaller than the first rune used to return
// nothing and name its own offset as next_offset, so a model following the
// hint asked for the same bytes forever.
func TestFetchURLPagingAlwaysAdvancesAndRejoins(t *testing.T) {
	allowPrivate(t)
	// Mixed rune widths: ASCII, two-byte Cyrillic, three-byte punctuation and
	// a four-byte emoji, so a cut lands mid-rune at many budgets.
	full := strings.Repeat("ab щё — 🎯 ", 40)
	srv := serve(t, "text/plain; charset=utf-8", []byte(full))

	for _, budget := range []int{1, 2, 3, 4, 5, 7, 13, 64, 199, 1 << 20} {
		t.Run(fmt.Sprintf("max_bytes=%d", budget), func(t *testing.T) {
			var joined strings.Builder
			offset := 0
			for step := 0; ; step++ {
				if step > 4000 {
					t.Fatalf("постраничность не сходится: %d шагов, offset=%d", step, offset)
				}
				got := fetchMap(t, srv.URL, budget, offset)
				part, _ := got["content"].(string)
				joined.WriteString(part)
				next, ok := got["next_offset"].(int)
				if !ok {
					break
				}
				if next <= offset {
					t.Fatalf("next_offset=%d не продвинулся от offset=%d", next, offset)
				}
				offset = next
			}
			if joined.String() != full {
				t.Errorf("склейка длиной %d не равна исходным %d байтам", joined.Len(), len(full))
			}
		})
	}
}

// An offset at or past the end is the natural last step of paging, not an
// error: it must return an empty tail and stop, rather than name another
// offset to go to.
func TestFetchURLOffsetAtEndStopsCleanly(t *testing.T) {
	allowPrivate(t)
	full := "штаб"
	srv := serve(t, "text/plain; charset=utf-8", []byte(full))

	for _, offset := range []int{len(full), len(full) + 1, len(full) + 1000} {
		got := fetchMap(t, srv.URL, 0, offset)
		if content, _ := got["content"].(string); content != "" {
			t.Errorf("offset=%d вернул %q", offset, content)
		}
		if _, ok := got["next_offset"]; ok {
			t.Errorf("offset=%d назвал next_offset, хотя текст кончился", offset)
		}
		if got["truncated"] != false {
			t.Errorf("offset=%d помечен обрезанным", offset)
		}
	}
}

// An offset the model made up can land inside a rune. The window then starts
// at the next boundary, and everything reported back — offset, next_offset,
// the hint — has to be measured from there. Reporting the requested offset
// instead names a position behind what was read, so the next window repeats
// those bytes.
func TestFetchURLUnalignedOffsetReportsTheRealPosition(t *testing.T) {
	allowPrivate(t)
	// One two-byte rune, then ASCII: a window that ends on ASCII is what makes
	// the discrepancy observable.
	full := "щ" + strings.Repeat("a", 300)
	srv := serve(t, "text/plain; charset=utf-8", []byte(full))

	// offset 1 is the second byte of "щ": the real window starts at 2.
	got := fetchMap(t, srv.URL, 50, 1)
	if got["offset"] != 2 {
		t.Errorf("offset = %v, ожидалось 2 (выровненная позиция)", got["offset"])
	}
	content, _ := got["content"].(string)
	next, ok := got["next_offset"].(int)
	if !ok {
		t.Fatalf("next_offset = %v", got["next_offset"])
	}
	if want := 2 + len(content); next != want {
		t.Errorf("next_offset = %d, ожидалось %d: части перекроются на %d байт", next, want, want-next)
	}
	rest := fetchMap(t, srv.URL, 0, next)
	tail, _ := rest["content"].(string)
	if content+tail != full[2:] {
		t.Errorf("склейка с невыровненного offset не восстанавливает хвост (%d + %d байт против %d)",
			len(content), len(tail), len(full)-2)
	}
}

// A page is not a fact about the game: answering "read it again" with a
// twenty-second-old copy is wrong, and holding the body that long costs
// memory in a long-lived «dzzzr web».
func TestFetchURLIsNotCached(t *testing.T) {
	tool := fetchTool(&gate{policy: PolicyReadonly})
	if !tool.noCache {
		t.Error("fetch_url попал под общий кеш чтений")
	}
}
