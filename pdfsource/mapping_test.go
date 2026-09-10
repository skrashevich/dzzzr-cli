package pdfsource

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"html"
	"strings"
	"testing"
)

func mappingPDF(lines ...string) []byte {
	var content strings.Builder
	content.WriteString("BT /F1 12 Tf 72 720 Td ")
	for i, line := range lines {
		if i > 0 {
			content.WriteString("0 -24 Td ")
		}
		fmt.Fprintf(&content, "<%x> Tj ", []byte(line))
	}
	content.WriteString("ET")
	return minimalPDF([]string{pdfStream([]byte(content.String()), ""), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}, "")
}

func mappingIndex(t *testing.T, data []byte) ([]IndexedPage, []Range) {
	t.Helper()
	var pages []IndexedPage
	var ignored []Range
	for start := 1; ; start += MaxPages {
		doc, err := Index(t.Context(), data, Options{StartPage: start})
		if err != nil {
			t.Fatal(err)
		}
		for _, page := range doc.Pages {
			pages = append(pages, page)
			if !page.RequiresOCR {
				ignored = append(ignored, Range{Page: page.Number, StartLine: 1, EndLine: len(page.Lines)})
			}
		}
		if !doc.HasMore {
			break
		}
	}
	return pages, ignored
}

func extractMapping(t *testing.T, data []byte, output any, ignored []Range) (*Extraction, error) {
	t.Helper()
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := json.Marshal(Mapping{Version: 1, SourceSHA256: SourceHash(data), Output: raw, Ignored: ignored})
	if err != nil {
		t.Fatal(err)
	}
	return Extract(t.Context(), data, mapping)
}

func sourceMapping(ranges ...Range) map[string]any {
	return map[string]any{"$source": Selector{Ranges: ranges}}
}

func TestMappingExactTextAndChangedLayouts(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	const text = `Question: "quotes" <img src="https://example.test/a?a=1&b=2"> \literal`
	for _, lines := range [][]string{{text, "42"}, {"Heading", "42", text, "Footer"}} {
		data := mappingPDF(lines...)
		pages, ignored := mappingIndex(t, data)
		var question, number Range
		for _, line := range pages[0].Lines {
			if line.Text == text {
				question = Range{Page: 1, StartLine: line.Number, EndLine: line.Number}
			}
			if line.Text == "42" {
				number = Range{Page: 1, StartLine: line.Number, EndLine: line.Number}
			}
		}
		if question.Page == 0 || number.Page == 0 {
			t.Fatalf("fixture text changed: %+v", pages)
		}
		out, err := extractMapping(t, data, map[string]any{"question": sourceMapping(question), "number": map[string]any{"$source": Selector{Ranges: []Range{number}, Format: "integer"}}, "empty": nil}, ignored)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Question string `json:"question"`
			Number   int    `json:"number"`
			Empty    any    `json:"empty"`
		}
		if err := json.Unmarshal(out.Data, &got); err != nil {
			t.Fatal(err)
		}
		if got.Question != text || got.Number != 42 || got.Empty != nil {
			t.Fatalf("regenerated content: %+v", got)
		}
	}
}

func TestMappingCrossPageAndMediaReferences(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	data := fixture(t)
	pages, ignored := mappingIndex(t, data)
	first, last := pages[0], pages[len(pages)-1]
	ranges := []Range{{Page: first.Number, StartLine: 1, EndLine: len(first.Lines)}, {Page: last.Number, StartLine: 1, EndLine: len(last.Lines)}}
	output := map[string]any{"text": sourceMapping(ranges...), "link": map[string]any{"$source": Selector{Page: 1, Link: 1}}, "image": map[string]any{"$source": Selector{Page: 1, ImageID: first.Images[0].ImageID}}}
	preserveMappingScans(output, pages)
	result, err := extractMapping(t, data, output, ignored)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(result.Data, &got); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, page := range []IndexedPage{first, last} {
		for _, line := range page.Lines {
			want = append(want, line.Text)
		}
	}
	if got["text"] != strings.Join(want, "\n") || got["link"] != first.Links[0].URI || got["image"] != "pdf-image://"+SourceHash(data)+"/1/"+first.Images[0].ImageID || result.PageCount != 12 {
		t.Fatalf("changed source: %+v", got)
	}
}

func TestMappingRejectsUnsafeOrInvalidSelectors(t *testing.T) {
	data := mappingPDF("42 and 42", "true", "<quoted>")
	pages, ignored := mappingIndex(t, data)
	r := Range{Page: 1, StartLine: 1, EndLine: 1}
	for name, output := range map[string]any{
		"literal text": "rewritten", "literal number": 42, "literal bool": true,
		"nested literal":       map[string]any{"levels": []any{map[string]any{"question": "rewritten"}}},
		"source siblings":      map[string]any{"$source": Selector{Ranges: []Range{r}}, "replace": "invented"},
		"unknown member":       map[string]any{"$source": map[string]any{"ranges": []Range{r}, "replacement": "invented"}},
		"unknown range member": jsontext.Value(`{"$source":{"ranges":[{"page":1,"start_line":1,"end_line":1,"rewrite":"evil"}]}}`),
		"unknown format":       map[string]any{"$source": Selector{Ranges: []Range{r}, Format: "execute"}},
		"invalid regex":        map[string]any{"$source": Selector{Ranges: []Range{r}, Pattern: "("}},
		"ambiguous regex":      map[string]any{"$source": Selector{Ranges: []Range{r}, Pattern: `\d+`}},
		"unmatched regex":      map[string]any{"$source": Selector{Ranges: []Range{r}, Pattern: "missing"}},
		"invalid capture":      map[string]any{"$source": Selector{Ranges: []Range{r}, Pattern: "(.*)", Group: 2}},
		"group without regex":  map[string]any{"$source": Selector{Ranges: []Range{r}, Group: 1}},
		"wrong integer":        map[string]any{"$source": Selector{Ranges: []Range{r}, Format: "integer"}},
		"wrong boolean":        map[string]any{"$source": Selector{Ranges: []Range{r}, Format: "boolean"}},
		"missing ranges":       map[string]any{"$source": Selector{}},
		"mixed media ranges":   map[string]any{"$source": Selector{Page: 1, Link: 1, Ranges: []Range{r}}},
		"missing media":        map[string]any{"$source": Selector{Page: 1, ImageID: "not-found"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := extractMapping(t, data, output, ignored); err == nil {
				t.Fatal("invalid selector accepted")
			}
		})
	}
	for _, bad := range []Range{
		{Page: 0, StartLine: 1, EndLine: 1}, {Page: 2, StartLine: 1, EndLine: 1}, {Page: 1, StartLine: 0, EndLine: 1},
		{Page: 1, StartLine: 2, EndLine: 1}, {Page: 1, StartLine: 1, EndLine: len(pages[0].Lines) + 1},
		{Page: 1, StartLine: 1, EndLine: 1, StartColumn: -1}, {Page: 1, StartLine: 1, EndLine: 1, EndColumn: 1000},
		{Page: 1, StartLine: 1, EndLine: 1, StartColumn: 3, EndColumn: 1},
		{Page: 1, StartLine: 1, EndLine: 1, StartColumn: len([]rune(pages[0].Lines[0].Text)) + 1},
	} {
		if _, err := extractMapping(t, data, sourceMapping(bad), ignored); err == nil {
			t.Errorf("invalid range accepted: %+v", bad)
		}
	}
}

func TestMappingCoverageHashAndStrictEnvelope(t *testing.T) {
	data := mappingPDF("first", "second")
	_, ignored := mappingIndex(t, data)
	if _, err := extractMapping(t, data, sourceMapping(Range{Page: 1, StartLine: 1, EndLine: 1}), nil); err == nil || !strings.Contains(err.Error(), "unmapped") {
		t.Fatalf("uncovered lines accepted: %v", err)
	}
	for _, raw := range []string{
		`{"version":1,"source_sha256":"wrong","output":null}`,
		fmt.Sprintf(`{"version":2,"source_sha256":%q,"output":null}`, SourceHash(data)),
		fmt.Sprintf(`{"version":1,"source_sha256":%q,"output":null,"unknown":true}`, SourceHash(data)),
		fmt.Sprintf(`{"version":1,"version":1,"source_sha256":%q,"output":null}`, SourceHash(data)),
	} {
		if _, err := Extract(t.Context(), data, []byte(raw)); err == nil {
			t.Fatalf("bad envelope accepted: %s", raw)
		}
	}
	if _, err := extractMapping(t, data, map[string]any{"empty": []any{}, "object": map[string]any{}, "null": nil, "text": sourceMapping(Range{Page: 1, StartLine: 1, EndLine: 1})}, ignored); err != nil {
		t.Fatal(err)
	}
}

func TestMappingConversionsAndUnicodeColumns(t *testing.T) {
	const cmap = `1 begincodespacerange <00> <FF> endcodespacerange 3 beginbfchar <01> <0416> <41> <0041> <42> <0042> endbfchar`
	data := minimalPDF([]string{pdfStream([]byte("BT /F1 12 Tf 72 720 Td <014142> Tj ET"), ""), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode 6 0 R >>", pdfStream([]byte(cmap), "")}, "")
	pages, ignored := mappingIndex(t, data)
	if pages[0].Lines[0].Text != "ЖAB" {
		t.Fatalf("unicode fixture: %+v", pages)
	}
	out, err := extractMapping(t, data, sourceMapping(Range{Page: 1, StartLine: 1, EndLine: 1, StartColumn: 2, EndColumn: 3}), ignored)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	if err := json.Unmarshal(out.Data, &got); err != nil || got != "AB" {
		t.Fatalf("columns are not runes: %q %v", got, err)
	}
	data = mappingPDF("<quotes> & \"text\"", "42", "true")
	pages, ignored = mappingIndex(t, data)
	for _, test := range []struct {
		line            int
		format, pattern string
		group           int
		want            any
	}{
		{1, "html", "", 0, html.EscapeString(pages[0].Lines[0].Text)},
		{2, "integer", "([0-9]+)", 1, 42}, {3, "boolean", "", 0, true},
	} {
		out, err := extractMapping(t, data, map[string]any{"$source": Selector{Ranges: []Range{{Page: 1, StartLine: test.line, EndLine: test.line}}, Format: test.format, Pattern: test.pattern, Group: test.group}}, ignored)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(test.want)
		if err != nil {
			t.Fatal(err)
		}
		if string(out.Data) != string(want) {
			t.Fatalf("%s got %s want %s", test.format, out.Data, want)
		}
	}
}

func TestMappingConcatAndImageBindings(t *testing.T) {
	data := fixture(t)
	pages, ignored := mappingIndex(t, data)
	page := pages[0]
	id := page.Images[0].ImageID
	text := page.Lines[0].Text
	combined := map[string]any{"text": map[string]any{"$concat": []any{
		sourceMapping(Range{Page: 1, StartLine: 1, EndLine: 1}),
		map[string]any{"$source": Selector{Page: 1, ImageID: id, Format: "image_html"}},
	}}}
	preserveMappingScans(combined, pages)
	output, err := json.Marshal(combined)
	if err != nil {
		t.Fatal(err)
	}
	for _, bound := range []string{"", "https://example.test/uploaded/image.png?a=1&b=2"} {
		mapping := Mapping{Version: 1, SourceSHA256: SourceHash(data), Output: output, Ignored: ignored}
		uri := "pdf-image://" + SourceHash(data) + "/1/" + id
		if bound != "" {
			mapping.ImageURLs = map[string]string{id: bound}
			uri = bound
		}
		raw, err := json.Marshal(mapping)
		if err != nil {
			t.Fatal(err)
		}
		result, err := Extract(t.Context(), data, raw)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]string
		if err := json.Unmarshal(result.Data, &fields); err != nil {
			t.Fatal(err)
		}
		got := fields["text"]
		if want := text + `<img src="` + html.EscapeString(uri) + `">`; got != want {
			t.Fatalf("concat changed source: got %q want %q", got, want)
		}
	}
	for _, bad := range []any{
		map[string]any{"$concat": []any{"model-written text"}},
		map[string]any{"$concat": []any{nil}},
		map[string]any{"$concat": []any{sourceMapping(Range{Page: 1, StartLine: 1, EndLine: 1})}, "extra": nil},
		map[string]any{"$source": Selector{Ranges: []Range{{Page: 1, StartLine: 1, EndLine: 1}}, Format: "image_html"}},
	} {
		if _, err := extractMapping(t, data, bad, ignored); err == nil {
			t.Fatalf("invalid composition accepted: %+v", bad)
		}
	}
	for _, badURL := range []string{"javascript:alert(1)", "file:///tmp/image.png", "/relative.png", "https://user:password@example.test/image.png"} {
		raw, err := json.Marshal(Mapping{Version: 1, SourceSHA256: SourceHash(data), Output: output, Ignored: ignored, ImageURLs: map[string]string{id: badURL}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Extract(t.Context(), data, raw); err == nil {
			t.Fatalf("invalid image URL accepted: %q", badURL)
		}
	}
}

func TestMappingRejectsEmptyCapture(t *testing.T) {
	data := mappingPDF("42 and 42")
	_, ignored := mappingIndex(t, data)
	for _, pattern := range []string{`^(missing)?42 and 42$`, `^()42 and 42$`} {
		if _, err := extractMapping(t, data, map[string]any{"$source": Selector{Ranges: []Range{{Page: 1, StartLine: 1, EndLine: 1}}, Pattern: pattern, Group: 1}}, ignored); err == nil {
			t.Fatalf("empty capture accepted: %s", pattern)
		}
	}
}

func TestMappingRejectsEmptyConcatWithOtherSource(t *testing.T) {
	data := mappingPDF("Valid source")
	_, ignored := mappingIndex(t, data)
	for _, parts := range []any{nil, []any{}} {
		output := map[string]any{"valid": sourceMapping(Range{Page: 1, StartLine: 1, EndLine: 1}), "invalid": map[string]any{"$concat": parts}}
		if _, err := extractMapping(t, data, output, ignored); err == nil || !strings.Contains(err.Error(), "$concat requires") {
			t.Fatalf("empty concat accepted or wrong guard: %v", err)
		}
	}
}

func mappingImageOnlyPDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 3 /Kids [3 0 R 4 0 R 5 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 7 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im1 9 0 R >> >> /Contents 8 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> >>",
		pdfStream([]byte("BT /F1 12 Tf 72 720 Td (Readable title) Tj ET"), ""),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		pdfStream([]byte("q 100 0 0 100 72 600 cm /Im1 Do Q"), ""),
		pdfStream([]byte{255, 0, 0}, "/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8"),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func TestMappingRequiresExplicitImageOnlyPagePreservation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	data := mappingImageOnlyPDF()
	pages, ignored := mappingIndex(t, data)
	if len(pages) != 3 || !pages[1].RequiresOCR || len(pages[1].Images) != 1 || !pages[2].RequiresOCR || len(pages[2].Images) != 0 {
		t.Fatalf("invalid image/blank fixture: %+v", pages)
	}
	output := map[string]any{"title": sourceMapping(Range{Page: 1, StartLine: 1, EndLine: 1})}
	if _, err := extractMapping(t, data, output, ignored); err == nil || !strings.Contains(err.Error(), "page 2 has image content") {
		t.Fatalf("image-only page silently omitted: %v", err)
	}
	id := pages[1].Images[0].ImageID
	output["scan"] = map[string]any{"$source": Selector{Page: 2, ImageID: id}}
	result, err := extractMapping(t, data, output, ignored)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(result.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got["title"] != "Readable title" || got["scan"] != "pdf-image://"+SourceHash(data)+"/2/"+id || result.PageCount != 3 {
		t.Fatalf("source page lost: %+v", got)
	}
}

func preserveMappingScans(output map[string]any, pages []IndexedPage) {
	for _, page := range pages {
		if page.RequiresOCR && len(page.Images) > 0 {
			output[fmt.Sprintf("scan_page_%d", page.Number)] = map[string]any{"$source": Selector{Page: page.Number, ImageID: page.Images[0].ImageID}}
		}
	}
}
