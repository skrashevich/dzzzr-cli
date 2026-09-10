package gamesource

import (
	"reflect"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func TestValidateRejectsUnresolvedPDFReferences(t *testing.T) {
	const unresolved = `<img src="pdf-image://page-1-image-1">`
	for _, tc := range []struct {
		name, path string
		set        func(*dzzzr.LevelParams)
	}{
		{"question", "params.question", func(p *dzzzr.LevelParams) { p.Question = unresolved }},
		{"comment", "params.comment", func(p *dzzzr.LevelParams) { p.Comment = new(unresolved) }},
		{"sector", "params.sector_names[0]", func(p *dzzzr.LevelParams) { p.SectorNames = []string{unresolved} }},
		{"spoiler", "params.spoilers[0].text", func(p *dzzzr.LevelParams) { p.Spoilers = []dzzzr.SpoilerParams{{Text: unresolved, Code: "CODE"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := validPlan("create")
			tc.set(&p.Levels[1].Params)
			engine := &fakeEngine{}
			_, err := p.Apply(t.Context(), engine)
			if err == nil || !strings.Contains(err.Error(), tc.path) || !strings.Contains(err.Error(), "bind uploaded image URLs") || !strings.Contains(err.Error(), "re-extract") {
				t.Fatalf("missing actionable reference error: %v", err)
			}
			if len(engine.calls) != 0 {
				t.Fatalf("unresolved references reached engine: %v", engine.calls)
			}
		})
	}
}

func TestValidatePreservesResolvedPDFContent(t *testing.T) {
	p := validPlan("create")
	p.Levels[0].Params.Question = "  Ёж & Щука\n<img src=\"https://example.test/upload/image.png?a=1&amp;b=2\">\n"
	p.Levels[0].Params.Comment = new("")
	before := p.Levels[0].Params
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Levels[0].Params, before) {
		t.Fatal("validation changed resolved content")
	}
}
