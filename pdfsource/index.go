package pdfsource

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
)

// Line addresses are stable within a PDF hash and this extraction version.
type Line struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

type IndexedImage struct {
	ImageID    string      `json:"image_id"`
	Name       string      `json:"name"`
	Placements []Placement `json:"placements"`
}

type IndexedPage struct {
	Number      int            `json:"number"`
	Lines       []Line         `json:"lines"`
	Links       []Link         `json:"links"`
	Images      []IndexedImage `json:"images"`
	RequiresOCR bool           `json:"requires_ocr"`
}

type IndexedDocument struct {
	Version      int           `json:"version"`
	SourceSHA256 string        `json:"source_sha256"`
	PageCount    int           `json:"page_count"`
	StartPage    int           `json:"start_page"`
	EndPage      int           `json:"end_page"`
	HasMore      bool          `json:"has_more"`
	Pages        []IndexedPage `json:"pages"`
}

func SourceHash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func TextLines(text string) []Line {
	parts := strings.Split(text, "\n")
	lines := make([]Line, len(parts))
	for i, text := range parts {
		lines[i] = Line{Number: i + 1, Text: text}
	}
	return lines
}

// Index exposes addresses, not a model-generated transcription. Annotation URLs
// remain available separately so wrapped links need not be reconstructed by LLM.
func Index(ctx context.Context, data []byte, opts Options) (*IndexedDocument, error) {
	doc, err := Read(ctx, data, opts)
	if err != nil {
		return nil, err
	}
	out := &IndexedDocument{Version: 1, SourceSHA256: SourceHash(data), PageCount: doc.PageCount, StartPage: doc.StartPage, EndPage: doc.EndPage, HasMore: doc.HasMore}
	for _, p := range doc.Pages {
		page := IndexedPage{Number: p.Number, Lines: TextLines(p.Text), Links: p.Links, RequiresOCR: p.RequiresOCR}
		for _, img := range p.Images {
			page.Images = append(page.Images, IndexedImage{ImageID: SourceHash(img.Data), Name: img.Name, Placements: img.Placements})
		}
		out.Pages = append(out.Pages, page)
	}
	return out, nil
}
