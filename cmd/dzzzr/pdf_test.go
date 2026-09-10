package main

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type pdfCLIIndex struct {
	SourceSHA256 string `json:"source_sha256"`
	PageCount    int    `json:"page_count"`
	StartPage    int    `json:"start_page"`
	EndPage      int    `json:"end_page"`
	HasMore      bool   `json:"has_more"`
	Pages        []struct {
		Number int `json:"number"`
		Images []struct {
			ImageID string `json:"image_id"`
		} `json:"images"`
		Lines []struct {
			Number int    `json:"number"`
			Text   string `json:"text"`
		} `json:"lines"`
	} `json:"pages"`
}

func setupPDFCLI(t *testing.T) (string, pdfCLIIndex) {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	t.Setenv("DZZR_FILES_ROOT", root)
	data, err := os.ReadFile("../../pdfsource/testdata/synthetic.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.pdf"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := runCLI(t, "pdf-index", "source.pdf")
	if code != 0 {
		t.Fatalf("pdf-index: code=%d stderr=%s", code, stderr)
	}
	var indexed pdfCLIIndex
	if err := json.Unmarshal([]byte(out), &indexed); err != nil {
		t.Fatalf("index must be JSON without -json: %v\n%s", err, out)
	}
	if len(indexed.SourceSHA256) != 64 || indexed.PageCount == 0 || len(indexed.Pages) == 0 {
		t.Fatalf("incomplete index: %+v", indexed)
	}
	if strings.Contains(out, `"data":`) {
		t.Fatal("index leaked image bytes")
	}
	return root, indexed
}

func TestPDFCLIIndexPageRange(t *testing.T) {
	_, indexed := setupPDFCLI(t)
	code, out, stderr := runCLI(t, "pdf-index", "source.pdf", "1", "1")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	var selected pdfCLIIndex
	if err := json.Unmarshal([]byte(out), &selected); err != nil {
		t.Fatal(err)
	}
	if selected.SourceSHA256 != indexed.SourceSHA256 || selected.StartPage != 1 || selected.EndPage != 1 || len(selected.Pages) != 1 {
		t.Fatalf("wrong page selection: %+v", selected)
	}
}

func TestPDFCLIExtractCopiesSource(t *testing.T) {
	root, indexed := setupPDFCLI(t)
	for indexed.HasMore {
		start := indexed.EndPage + 1
		end := min(start+9, indexed.PageCount)
		code, out, stderr := runCLI(t, "pdf-index", "source.pdf", strconv.Itoa(start), strconv.Itoa(end))
		if code != 0 {
			t.Fatalf("pdf-index continuation: code=%d stderr=%s", code, stderr)
		}
		var next pdfCLIIndex
		if err := json.Unmarshal([]byte(out), &next); err != nil {
			t.Fatal(err)
		}
		if next.SourceSHA256 != indexed.SourceSHA256 || next.StartPage != start || next.EndPage != end {
			t.Fatalf("wrong continuation: %+v", next)
		}
		indexed.Pages = append(indexed.Pages, next.Pages...)
		indexed.EndPage, indexed.HasMore = next.EndPage, next.HasMore
	}
	var ranges []map[string]int
	var images []any
	var texts []string
	for _, page := range indexed.Pages {
		for _, img := range page.Images {
			images = append(images, map[string]any{"$source": map[string]any{"page": page.Number, "image_id": img.ImageID}})
		}
		for _, line := range page.Lines {
			if strings.TrimSpace(line.Text) == "" {
				continue
			}
			ranges = append(ranges, map[string]int{"page": page.Number, "start_line": line.Number, "end_line": line.Number})
			texts = append(texts, line.Text)
		}
	}
	if len(texts) == 0 {
		t.Fatal("fixture has no source lines")
	}
	mapping := map[string]any{
		"version": 1, "source_sha256": indexed.SourceSHA256,
		"output": map[string]any{"text": map[string]any{"$source": map[string]any{"ranges": ranges, "format": "text"}}, "images": images},
	}
	data, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mapping.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, jsonMode := range []bool{false, true} {
		name := "extracted.json"
		args := []string{"pdf-extract", "source.pdf", "mapping.json", name}
		if jsonMode {
			name = "extracted-json.json"
			args = []string{"-json", "pdf-extract", "source.pdf", "mapping.json", name}
		}
		code, out, stderr := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("pdf-extract: code=%d out=%s stderr=%s", code, out, stderr)
		}
		if jsonMode {
			var summary struct {
				Path            string `json:"path"`
				ReferencedLines int    `json:"referenced_lines"`
			}
			if err := json.Unmarshal([]byte(out), &summary); err != nil {
				t.Fatal(err)
			}
			if summary.ReferencedLines != len(texts) || !strings.HasSuffix(summary.Path, name) {
				t.Fatalf("wrong summary: %+v", summary)
			}
		} else if !strings.Contains(out, name) {
			t.Fatalf("missing output path: %s", out)
		}
		generated, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Text   string   `json:"text"`
			Images []string `json:"images"`
		}
		if err := json.Unmarshal(generated, &result); err != nil {
			t.Fatal(err)
		}
		if result.Text != strings.Join(texts, "\n") {
			t.Fatalf("source text changed: got %q want %q", result.Text, strings.Join(texts, "\n"))
		}
		if len(images) == 0 || len(result.Images) != len(images) {
			t.Fatal("extraction lost image references")
		}
	}
}

func TestPDFCLIUsage(t *testing.T) {
	isolate(t)
	for _, args := range [][]string{
		{"pdf-index"}, {"pdf-index", "source.pdf", "1"},
		{"pdf-index", "source.pdf", "zero", "1"}, {"pdf-index", "source.pdf", "0", "1"},
		{"pdf-index", "source.pdf", "2", "1"}, {"pdf-index", "source.pdf", "1", "11"},
		{"pdf-extract", "source.pdf", "mapping.json"},
		{"pdf-extract", "source.pdf", "mapping.json", "out.json", "extra"},
	} {
		code, _, _ := runCLI(t, args...)
		if code != 2 {
			t.Errorf("%v: exit=%d, want 2", args, code)
		}
	}
}

func TestPDFCLIRejectsOutsideRoot(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	t.Setenv("DZZR_FILES_ROOT", root)
	code, _, stderr := runCLI(t, "pdf-index", "../outside.pdf")
	if code == 0 || stderr == "" {
		t.Fatalf("outside path accepted: code=%d stderr=%s", code, stderr)
	}
}
