// Package pdfsource extracts local PDF text, hyperlinks and embedded images in
// pure Go. Document contents are untrusted data, never agent instructions.
package pdfsource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	pdf "github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

const MaxPDFBytes = 32 << 20
const MaxPages = 10
const maxDecodedBytes = 16 << 20
const maxImageBytes = 32 << 20

type Options struct {
	StartPage int
	EndPage   int
}

type Document struct {
	PageCount int    `json:"page_count"`
	StartPage int    `json:"start_page"`
	EndPage   int    `json:"end_page"`
	HasMore   bool   `json:"has_more"`
	Pages     []Page `json:"pages"`
}

type Page struct {
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	Number      int     `json:"number"`
	Text        string  `json:"text"`
	RequiresOCR bool    `json:"requires_ocr"`
	Links       []Link  `json:"links"`
	Images      []Image `json:"images"`
}

// Link.Text is a best-effort label derived from overlapping text origins.
// Rect retains the PDF annotation coordinates even when no label is available.
type Link struct {
	URI  string    `json:"uri"`
	Text string    `json:"text"`
	Rect []float64 `json:"rect"`
}

// Placement is a known image occurrence in unrotated PDF user coordinates.
// NearbyText is a best-effort heading association, not a semantic role.
type Placement struct {
	Rect       []float64 `json:"rect"`
	NearbyText string    `json:"nearby_text"`
}

type Image struct {
	Placements       []Placement `json:"placements"`
	PlacementUnknown bool        `json:"placement_unknown"`
	Name             string      `json:"name"`
	MIMEType         string      `json:"mime_type"`
	Data             []byte      `json:"data"`
	Width            int         `json:"width"`
	Height           int         `json:"height"`
}

// Read extracts a 1-based inclusive range of at most ten pages. Zero start means
// one; zero end means up to ten pages. It never runs subprocesses or follows links.
// Decoded streams/images and object counts are bounded. Context checks are
// cooperative: there is no per-call OS memory ceiling or forcible decoder kill.
func Read(ctx context.Context, data []byte, opts Options) (doc *Document, err error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	defer func() {
		if problem := recover(); problem != nil {
			doc = nil
			err = fmt.Errorf("PDF decode: %v", problem)
		}
		if ctx.Err() != nil {
			doc = nil
			err = ctx.Err()
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > MaxPDFBytes {
		return nil, fmt.Errorf("PDF size must be between 1 and %d bytes", MaxPDFBytes)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, errors.New("input does not have a PDF header")
	}
	if opts.StartPage == 0 {
		opts.StartPage = 1
	}
	if opts.StartPage < 1 || opts.EndPage < 0 || (opts.EndPage != 0 && (opts.EndPage < opts.StartPage || opts.EndPage-opts.StartPage >= MaxPages)) {
		return nil, errors.New("invalid page range: select at most ten pages, 1-based inclusive")
	}
	// Explicit configuration avoids pdfcpu's user config/font directory creation.
	conf := &model.Configuration{Reader15: true, ValidationMode: model.ValidationRelaxed, Offline: true, UnsupportedResourcePolicy: model.UnsupportedResourceFail, Limits: model.ResourceLimits{
		MaxStreamBytes: MaxPDFBytes, MaxDecodeBytes: maxDecodedBytes, MaxImagePixels: 20_000_000, MaxImageBytes: maxImageBytes, MaxObjectCount: 100_000, MaxObjectStreamCount: 100_000, MaxObjectStreamFirst: 1 << 20, MaxXRefEntries: 100_000, MaxRecursionDepth: 64,
	}}
	cpu, err := pdfcpu.ReadWithContext(ctx, bytes.NewReader(data), conf)
	if err != nil {
		return nil, fmt.Errorf("PDF structure: %w", err)
	}
	if cpu.Encrypt != nil {
		return nil, errors.New("encrypted PDFs are not supported")
	}
	if err = cpu.EnsurePageCount(); err != nil {
		return nil, err
	}
	total := cpu.PageCount
	if opts.EndPage == 0 {
		opts.EndPage = min(total, opts.StartPage+MaxPages-1)
	}
	if opts.EndPage < opts.StartPage || opts.EndPage > total {
		return nil, errors.New("page range does not select existing pages")
	}
	reader, err := pdf.NewReader(contextReader{ctx, bytes.NewReader(data)}, int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("PDF text reader: %w", err)
	}
	if !reader.Trailer().Key("Encrypt").IsNull() {
		return nil, errors.New("encrypted PDFs are not supported")
	}
	doc = &Document{PageCount: total, StartPage: opts.StartPage, EndPage: opts.EndPage, HasMore: opts.EndPage < total, Pages: []Page{}}
	var totalImages int
	for number := opts.StartPage; number <= opts.EndPage; number++ {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		p, err := extractPage(ctx, reader.Page(number), cpu, number, &totalImages)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", number, err)
		}
		doc.Pages = append(doc.Pages, p)
	}
	return doc, nil
}

type contextReader struct {
	ctx context.Context
	io.ReaderAt
}

func (r contextReader) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.ReaderAt.ReadAt(p, off)
}

func readStream(ctx context.Context, v pdf.Value) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r := v.Reader()
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(io.LimitReader(r, maxDecodedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDecodedBytes {
		return nil, errors.New("decoded content exceeds 16 MiB limit")
	}
	return data, ctx.Err()
}
