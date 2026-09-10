package pdfsource

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/synthetic.pdf")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReadTextLinksImagesAndPaging(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	doc, err := Read(t.Context(), fixture(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.PageCount != 12 || doc.StartPage != 1 || doc.EndPage != 10 || !doc.HasMore || len(doc.Pages) != 10 {
		t.Fatalf("bad paging: %+v", doc)
	}
	page := doc.Pages[0]
	if !strings.Contains(page.Text, `<img src="https://example.test/game.jpg">`) || page.RequiresOCR {
		t.Fatalf("text lost: %q", page.Text)
	}
	if len(page.Links) != 1 || page.Links[0].URI != "https://drive.google.com/file/d/synthetic/view" || len(page.Links[0].Rect) != 4 || !strings.Contains(page.Links[0].Text, "Drive image chip") {
		t.Fatalf("bad links: %+v", page.Links)
	}
	if len(page.Images) != 1 {
		t.Fatalf("expected deduplicated image, got %d", len(page.Images))
	}
	image := page.Images[0]
	if image.Width != 1 || image.Height != 1 || image.MIMEType != "image/png" || image.Name != fmt.Sprintf("%x.png", sha256.Sum256(image.Data)) {
		t.Fatalf("bad image: %+v", image)
	}
	originalJPEG, err := os.ReadFile("testdata/original.jpg")
	if err != nil {
		t.Fatal(err)
	}
	foundJPEG := false
	for _, exported := range doc.Pages[2].Images {
		if exported.MIMEType == "image/jpeg" {
			foundJPEG = true
			if !bytes.Equal(exported.Data, originalJPEG) {
				t.Fatal("JPEG bytes changed during extraction")
			}
		}
	}
	if !foundJPEG {
		t.Fatal("JPEG not extracted")
	}
	if !doc.Pages[1].RequiresOCR || doc.Pages[1].Text != "" {
		t.Fatal("blank page should explicitly require OCR")
	}
	last, err := Read(t.Context(), fixture(t), Options{StartPage: 11})
	if err != nil || last.EndPage != 12 || last.HasMore || len(last.Pages) != 2 {
		t.Fatalf("last=%+v err=%v", last, err)
	}
	again, err := Read(t.Context(), fixture(t), Options{StartPage: 1, EndPage: 1})
	if err != nil || again.Pages[0].Images[0].Name != image.Name {
		t.Fatalf("image export not deterministic: %v", err)
	}
}

func TestReadInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
		opts Options
	}{
		{name: "empty"}, {name: "non PDF", data: []byte("hello")}, {name: "oversized", data: make([]byte, MaxPDFBytes+1)},
		{name: "negative start", data: []byte("%PDF-1.7"), opts: Options{StartPage: -1}},
		{name: "too many pages", data: []byte("%PDF-1.7"), opts: Options{EndPage: 11}},
		{name: "reverse range", data: []byte("%PDF-1.7"), opts: Options{StartPage: 4, EndPage: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Read(t.Context(), test.data, test.opts); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
}

func TestReadMalformedEncryptedAndMissingPages(t *testing.T) {
	if _, err := Read(t.Context(), []byte("%PDF-1.7\ninvalid"), Options{}); err == nil {
		t.Fatal("accepted malformed PDF")
	}
	if _, err := Read(t.Context(), fixture(t), Options{StartPage: 13}); err == nil {
		t.Fatal("accepted missing page")
	}
	encrypted := minimalPDF([]string{"<< /Filter /Standard /V 1 /R 2 /Length 40 /P -4 /O <0000000000000000000000000000000000000000000000000000000000000000> /U <0000000000000000000000000000000000000000000000000000000000000000> >>"}, " /Encrypt 4 0 R /ID [<0123456789abcdef0123456789abcdef> <0123456789abcdef0123456789abcdef>]")
	if _, err := Read(t.Context(), encrypted, Options{}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "encrypt") {
		t.Fatalf("encrypted err=%v", err)
	}
}

func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Read(ctx, fixture(t), Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestImagePlacementDistinguishesQuestionCommentAndNestedReuse(t *testing.T) {
	doc, err := Read(t.Context(), fixture(t), Options{StartPage: 4, EndPage: 5})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Pages[0].Width != 612 || doc.Pages[0].Height != 792 || len(doc.Pages[0].Images) != 2 {
		t.Fatalf("unexpected page geometry: %+v", doc.Pages[0])
	}
	for _, img := range doc.Pages[0].Images {
		if img.PlacementUnknown || len(img.Placements) != 1 || img.Width != 1 || img.Height != 1 {
			t.Fatalf("same-sized image placement missing: %+v", img)
		}
		placement := img.Placements[0]
		switch placement.NearbyText {
		case "Question heading":
			if !slices.Equal(placement.Rect, []float64{72, 600, 172, 700}) {
				t.Fatalf("question rect=%v", placement.Rect)
			}
		case "Comment heading":
			if !slices.Equal(placement.Rect, []float64{72, 300, 172, 400}) {
				t.Fatalf("comment rect=%v", placement.Rect)
			}
		default:
			t.Fatalf("missing heading association: %+v", placement)
		}
	}
	nested := doc.Pages[1].Images
	if len(nested) != 1 || nested[0].PlacementUnknown || len(nested[0].Placements) != 2 {
		t.Fatalf("nested/reused image lost: %+v", nested)
	}
	if !slices.Equal(nested[0].Placements[0].Rect, []float64{300, 520, 360, 600}) || !slices.Equal(nested[0].Placements[1].Rect, []float64{600, 520, 660, 600}) {
		t.Fatalf("nested matrices incorrect: %+v", nested[0].Placements)
	}
}

func TestContentDecompressionLimit(t *testing.T) {
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(bytes.Repeat([]byte(" "), 17<<20)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data := minimalPDF([]string{pdfStream(compressed.Bytes(), "/Filter /FlateDecode")}, "")
	if len(data) > MaxPDFBytes {
		t.Fatal("fixture should be a small compressed PDF")
	}
	if _, err := Read(t.Context(), data, Options{}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "limit") {
		t.Fatalf("oversized decompressed content accepted: %v", err)
	}
}

func pdfStream(data []byte, attributes string) string {
	return fmt.Sprintf("<< /Length %d %s >>\nstream\n%s\nendstream", len(data), attributes, data)
}

// Build a tiny PDF with correct object offsets entirely in Go. Extra objects
// start at 4; object 4 is the page content and optional object 5 is its font.
func minimalPDF(extra []string, trailer string) []byte {
	return minimalPDFContents(extra, trailer, "")
}

func minimalPDFContents(extra []string, trailer, contentOverride string) []byte {
	resources, contents := "<< >>", ""
	if len(extra) > 0 {
		contents = "/Contents 4 0 R"
	}
	if len(extra) > 1 {
		resources = "<< /Font << /F1 5 0 R >> >>"
	}
	if contentOverride != "" {
		contents = "/Contents " + contentOverride
	}
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Count 1 /Kids [3 0 R] >>", fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources %s %s >>", resources, contents)}
	objects = append(objects, extra...)
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = b.Len()
		_, _ = fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	_, _ = fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		_, _ = fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	_, _ = fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R%s >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, trailer, xref)
	return b.Bytes()
}

func TestContentArrayRetainsStateAndTJWordSpacing(t *testing.T) {
	data := minimalPDFContents([]string{
		pdfStream([]byte("q BT /F1 12 Tf 72 720 Td"), ""),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		pdfStream([]byte("[(First) -500 (second)] TJ ET Q"), ""),
	}, "", "[4 0 R 6 0 R]")
	doc, err := Read(t.Context(), data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 1 || !strings.Contains(doc.Pages[0].Text, "First second") {
		t.Fatalf("text state or word spacing lost: %+v", doc)
	}
}

func TestCyrillicToUnicode(t *testing.T) {
	const cmap = `/CIDInit /ProcSet findresource begin
12 dict begin begincmap
/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def
/CMapName /TestUnicode def /CMapType 2 def
1 begincodespacerange <00> <FF> endcodespacerange
6 beginbfchar
<01> <041F> <02> <0440> <03> <0438> <04> <0432> <05> <0435> <06> <0442>
endbfchar endcmap CMapName currentdict /CMap defineresource pop end end`
	data := minimalPDF([]string{
		pdfStream([]byte("BT /F1 12 Tf 72 720 Td <010203040506> Tj ET"), ""),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding /ToUnicode 6 0 R >>",
		pdfStream([]byte(cmap), ""),
	}, "")
	doc, err := Read(t.Context(), data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 1 || !strings.Contains(doc.Pages[0].Text, "Привет") {
		t.Fatalf("Cyrillic font mapping lost: %+v", doc)
	}
}
