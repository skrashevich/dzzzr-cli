package agenttools_test

import (
	"context"
	"encoding/json/v2"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"testing"
)

type teamGamesEngine struct {
	agenttools.Engine
	calls int
}

func (e *teamGamesEngine) GetGamesList(_ context.Context, o dzzzr.GamesListOptions) ([]dzzzr.GameInfo, error) {
	e.calls++
	games := []dzzzr.GameInfo{}
	end := min(90, o.Offset+o.Limit)
	for i := o.Offset; i < end; i++ {
		team := "Другая команда"
		if i%3 == 0 {
			team = "<b>Поручик Ржевский</b>"
		}
		games = append(games, dzzzr.GameInfo{ID: dzzzr.FlexInt(i + 1), Teams: []string{team}})
	}
	return games, nil
}
func TestGamesListFiltersTeamAcrossPages(t *testing.T) {
	e := &teamGamesEngine{}
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly})
	offset := 0
	ids := []int{}
	for range 2 {
		r := call(t, c, "games_list", map[string]any{"team": "поручик ржевский", "offset": offset})
		if r.IsError {
			t.Fatal(r.Content)
		}
		var page struct {
			Games []struct {
				ID int `json:"id"`
			} `json:"games"`
			NextOffset *int `json:"next_offset"`
		}
		if err := json.Unmarshal([]byte(r.Content), &page); err != nil {
			t.Fatal(err)
		}
		for _, g := range page.Games {
			ids = append(ids, g.ID)
		}
		if page.NextOffset != nil {
			offset = *page.NextOffset
		}
	}
	if len(ids) != 30 {
		t.Fatalf("got %d matches: %v", len(ids), ids)
	}
	for i, id := range ids {
		if id != i*3+1 {
			t.Fatalf("nonmatching or skipped games: %v", ids)
		}
	}
}
