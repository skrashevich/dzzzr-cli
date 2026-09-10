package pdfsource

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMappingTestJSON(t *testing.T, root, name string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMappingPartsResumeAndWholeDocumentCoverage(t *testing.T) {
	root := t.TempDir()
	data := mappingPDF("First exact text", "Second exact text")
	if err := os.WriteFile(filepath.Join(root, "source.pdf"), data, 0600); err != nil {
		t.Fatal(err)
	}
	pages, _ := mappingIndex(t, data)
	var ranges []Range
	var want []string
	for _, line := range pages[0].Lines {
		if strings.TrimSpace(line.Text) != "" {
			ranges = append(ranges, Range{Page: 1, StartLine: line.Number, EndLine: line.Number})
			want = append(want, line.Text)
		}
	}
	if len(ranges) != 2 {
		t.Fatal("fixture must have two nonempty lines")
	}
	savePart := func(name string, r Range) {
		writeMappingTestJSON(t, root, name, map[string]any{"version": 1, "source_sha256": SourceHash(data), "output": map[string]any{"levels": []any{sourceMapping(r)}}})
	}
	savePart("first.json", ranges[0])
	manifest := MappingManifest{Version: 2, SourceSHA256: SourceHash(data), Parts: []string{"first.json", "second.json"}}
	writeMappingTestJSON(t, root, "manifest.json", manifest)
	if _, err := ExtractFiles(t.Context(), root, "source.pdf", "manifest.json", "result.json"); err == nil {
		t.Fatal("missing part accepted")
	}
	manifest.Parts = []string{"first.json"}
	writeMappingTestJSON(t, root, "manifest.json", manifest)
	if _, err := ExtractFiles(t.Context(), root, "source.pdf", "manifest.json", "result.json"); err == nil {
		t.Fatal("incomplete coverage accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "result.json")); !os.IsNotExist(err) {
		t.Fatal("failed extraction wrote a result")
	}
	// Resume with the already saved first part; no model transcribes its text.
	savePart("second.json", ranges[1])
	manifest.Parts = []string{"second.json", "first.json"}
	writeMappingTestJSON(t, root, "manifest.json", manifest)
	result, err := ExtractFiles(t.Context(), root, "source.pdf", "manifest.json", "result.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Levels []string `json:"levels"`
	}
	if err := json.Unmarshal(raw, &got); err != nil || len(got.Levels) != 2 || got.Levels[0] != want[1] || got.Levels[1] != want[0] {
		t.Fatalf("order or exact content changed: %s %v", raw, err)
	}
}

func TestMappingManifestRejectsInvalidParts(t *testing.T) {
	for _, test := range []struct {
		name   string
		parts  []string
		change func(map[string]any)
	}{
		{name: "duplicate", parts: []string{"a.json", "./a.json"}},
		{name: "escape", parts: []string{"../a.json"}},
		{name: "missing", parts: []string{"missing.json"}},
		{name: "empty"},
		{name: "hash", parts: []string{"a.json"}, change: func(p map[string]any) { p["source_sha256"] = strings.Repeat("b", 64) }},
		{name: "nested manifest", parts: []string{"a.json"}, change: func(p map[string]any) { p["version"] = 2 }},
		{name: "unknown", parts: []string{"a.json"}, change: func(p map[string]any) { p["unexpected"] = true }},
		{name: "conflict", parts: []string{"a.json", "b.json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			hash := strings.Repeat("a", 64)
			part := map[string]any{"version": 1, "source_sha256": hash, "output": map[string]any{"title": sourceMapping(Range{Page: 1, StartLine: 1, EndLine: 1})}}
			if test.change != nil {
				test.change(part)
			}
			writeMappingTestJSON(t, root, "a.json", part)
			writeMappingTestJSON(t, root, "b.json", part)
			writeMappingTestJSON(t, root, "manifest.json", MappingManifest{Version: 2, SourceSHA256: hash, Parts: test.parts})
			if _, err := loadMapping(root, "manifest.json"); err == nil {
				t.Fatal("invalid parts accepted")
			}
		})
	}
}
