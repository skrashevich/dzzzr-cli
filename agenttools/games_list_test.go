package agenttools_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

type pagedGamesEngine struct {
	agenttools.Engine
	options []dzzzr.GamesListOptions
}

func (e *pagedGamesEngine) GetGamesList(_ context.Context, opts dzzzr.GamesListOptions) ([]dzzzr.GameInfo, error) {
	e.options = append(e.options, opts)
	end := 45
	if opts.Limit > 0 {
		end = min(end, opts.Offset+opts.Limit)
	}
	games := []dzzzr.GameInfo{}
	for i := opts.Offset; i < end; i++ {
		games = append(games, dzzzr.GameInfo{ID: dzzzr.FlexInt(i + 1), Name: fmt.Sprintf("Игра %d", i+1), Date: "2026-09-01", Start: strings.Repeat("Длинное описание старта ", 10000), Teams: []string{strings.Repeat("Команда ", 10000)}})
	}
	return games, nil
}

func TestGamesListPagesWithoutBulkyDetails(t *testing.T) {
	e := &pagedGamesEngine{}
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly})
	offset := 0
	ids := []int{}
	for page := 0; page < 3; page++ {
		res := call(t, c, "games_list", map[string]any{"archive": true, "offset": offset})
		if res.IsError {
			t.Fatal(res.Content)
		}
		if len(res.Content) > 12000 {
			t.Fatalf("list overflows context: %d bytes", len(res.Content))
		}
		var result struct {
			Games []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"games"`
			NextOffset *int `json:"next_offset"`
		}
		if err := json.Unmarshal([]byte(res.Content), &result); err != nil {
			t.Fatal(err)
		}
		for _, game := range result.Games {
			ids = append(ids, game.ID)
			if game.Name == "" {
				t.Fatal("missing name")
			}
		}
		if page < 2 {
			if result.NextOffset == nil {
				t.Fatal("missing continuation")
			}
			offset = *result.NextOffset
		} else if result.NextOffset != nil {
			t.Fatal("unexpected continuation")
		}
	}
	if len(ids) != 45 {
		t.Fatalf("lost games: %v", ids)
	}
	for i, id := range ids {
		if id != i+1 {
			t.Fatalf("duplicate or skipped game: %v", ids)
		}
	}
	for _, opts := range e.options {
		if opts.Limit == 0 || opts.Limit > 51 || !opts.Archive {
			t.Fatalf("unbounded/incorrect request: %+v", opts)
		}
	}
}

type archiveTeamEngine struct {
	agenttools.Engine
	stats int
}

func (e *archiveTeamEngine) GetGamesList(_ context.Context, opts dzzzr.GamesListOptions) ([]dzzzr.GameInfo, error) {
	games := []dzzzr.GameInfo{}
	for i := opts.Offset; i < 6; i++ {
		games = append(games, dzzzr.GameInfo{ID: dzzzr.FlexInt(i + 1), Name: fmt.Sprintf("Игра %d", i+1), Date: "2026-05-01"})
	}
	return games, nil
}

func (e *archiveTeamEngine) GetArchiveStat(_ context.Context, gameID int) (*dzzzr.ArchiveStat, error) {
	e.stats++
	if gameID == 4 {
		return nil, fmt.Errorf("страница архива без таблицы результатов")
	}
	st := &dzzzr.ArchiveStat{
		GameID: gameID, Name: fmt.Sprintf("Игра %d", gameID), Date: "2026-05-01",
		Columns: []dzzzr.ArchiveStatColumn{{Title: "Чистое"}, {Title: "Место"}, {Title: "Отставание"}},
		Teams:   []dzzzr.ArchiveStatTeam{{ID: 10, Name: "Другая команда", Cells: []string{"3:00:00", "1", "0:00:00"}}},
	}
	if cells, ok := map[int][]string{2: {"3:40:00", "2", "0:40:00"}, 5: {"4:10:00", "5", "1:10:00"}}[gameID]; ok {
		st.Teams = append(st.Teams, dzzzr.ArchiveStatTeam{ID: 11, Name: "<b>Поручик Ржевский</b>", Cells: cells})
	}
	return st, nil
}

func TestGamesListFindsArchiveTeamByResultsTable(t *testing.T) {
	e := &archiveTeamEngine{}
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly})
	res := call(t, c, "games_list", map[string]any{"archive": true, "team": "поручик ржевский"})
	if res.IsError {
		t.Fatal(res.Content)
	}
	var result struct {
		Games []struct {
			ID    int    `json:"id"`
			Date  string `json:"date"`
			Place string `json:"place"`
			Gap   string `json:"gap"`
		} `json:"games"`
		Count      int  `json:"count"`
		StatErrors int  `json:"stat_errors"`
		NextOffset *int `json:"next_offset"`
	}
	if err := json.Unmarshal([]byte(res.Content), &result); err != nil {
		t.Fatal(err)
	}
	if result.Count != 2 || len(result.Games) != 2 {
		t.Fatalf("expected two archive matches, got %s", res.Content)
	}
	for i, want := range []struct {
		id         int
		place, gap string
	}{{2, "2", "0:40:00"}, {5, "5", "1:10:00"}} {
		got := result.Games[i]
		if got.ID != want.id || got.Place != want.place || got.Gap != want.gap || got.Date != "2026-05-01" {
			t.Fatalf("game %d: got %+v, want %+v", i, got, want)
		}
	}
	if result.StatErrors != 1 {
		t.Fatalf("unreadable table not reported: %s", res.Content)
	}
	if result.NextOffset != nil {
		t.Fatalf("unexpected continuation: %s", res.Content)
	}
	if e.stats != 6 {
		t.Fatalf("expected one stat request per archive game, got %d", e.stats)
	}
}

// archivePagerEngine is a full archive: it honors Limit, so the multi-page
// scan, the stat cap and the match limit are all reachable.
type archivePagerEngine struct {
	agenttools.Engine
	total   int
	matches bool
	stats   int
}

func (e *archivePagerEngine) GetGamesList(_ context.Context, opts dzzzr.GamesListOptions) ([]dzzzr.GameInfo, error) {
	games := []dzzzr.GameInfo{}
	for i := opts.Offset; i < min(e.total, opts.Offset+opts.Limit); i++ {
		games = append(games, dzzzr.GameInfo{ID: dzzzr.FlexInt(i + 1), Name: fmt.Sprintf("Игра %d", i+1), Date: "2026-05-01"})
	}
	return games, nil
}

func (e *archivePagerEngine) GetArchiveStat(_ context.Context, gameID int) (*dzzzr.ArchiveStat, error) {
	e.stats++
	st := &dzzzr.ArchiveStat{
		GameID: gameID, Date: "2026-05-01",
		Columns: []dzzzr.ArchiveStatColumn{{Title: "Место"}, {Title: "Отставание"}},
		Teams:   []dzzzr.ArchiveStatTeam{{ID: 10, Name: "Другая команда", Cells: []string{"1", "0:00:00"}}},
	}
	if e.matches {
		st.Teams = append(st.Teams, dzzzr.ArchiveStatTeam{ID: 11, Name: "Поручик Ржевский", Cells: []string{"2", "0:10:00"}})
	}
	return st, nil
}

type archivePage struct {
	Games []struct {
		ID int `json:"id"`
	} `json:"games"`
	Count      int  `json:"count"`
	StatErrors int  `json:"stat_errors"`
	NextOffset *int `json:"next_offset"`
}

func archiveTeamPage(t *testing.T, c *agenttools.Catalog, args map[string]any) archivePage {
	t.Helper()
	res := call(t, c, "games_list", args)
	if res.IsError {
		t.Fatal(res.Content)
	}
	var page archivePage
	if err := json.Unmarshal([]byte(res.Content), &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestGamesListStopsArchiveScanAtStatCap(t *testing.T) {
	e := &archivePagerEngine{total: 130}
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly})
	page := archiveTeamPage(t, c, map[string]any{"archive": true, "team": "поручик ржевский"})
	if page.Count != 0 || len(page.Games) != 0 {
		t.Fatalf("unexpected matches: %+v", page)
	}
	if page.NextOffset == nil || *page.NextOffset != 60 {
		t.Fatalf("scan did not resume at the cap: %+v", page.NextOffset)
	}
	if e.stats != 60 {
		t.Fatalf("stat requests unbounded: %d", e.stats)
	}
}

func TestGamesListWalksWholeArchiveByNextOffset(t *testing.T) {
	e := &archivePagerEngine{total: 130, matches: true}
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly})
	ids := []int{}
	offset := 0
	for range 20 {
		page := archiveTeamPage(t, c, map[string]any{"archive": true, "team": "поручик ржевский", "offset": offset, "limit": 20})
		if page.Count != len(page.Games) {
			t.Fatalf("count disagrees with games: %+v", page)
		}
		for _, g := range page.Games {
			ids = append(ids, g.ID)
		}
		if page.NextOffset == nil {
			break
		}
		if *page.NextOffset <= offset {
			t.Fatalf("scan does not advance: %d", *page.NextOffset)
		}
		offset = *page.NextOffset
	}
	if len(ids) != 130 {
		t.Fatalf("lost archive games: %d", len(ids))
	}
	for i, id := range ids {
		if id != i+1 {
			t.Fatalf("duplicate or skipped game: %v", ids)
		}
	}
}

func TestGamesListFiltersRunningGamesByListRoster(t *testing.T) {
	e := &teamGamesEngine{}
	c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly})
	res := call(t, c, "games_list", map[string]any{"team": "поручик ржевский", "limit": 3})
	if res.IsError {
		t.Fatal(res.Content)
	}
	var result struct {
		Games []struct {
			ID int `json:"id"`
		} `json:"games"`
		Count      int  `json:"count"`
		NextOffset *int `json:"next_offset"`
	}
	if err := json.Unmarshal([]byte(res.Content), &result); err != nil {
		t.Fatal(err)
	}
	if result.Count != 3 {
		t.Fatalf("expected three roster matches, got %s", res.Content)
	}
	for i, g := range result.Games {
		if g.ID != i*3+1 {
			t.Fatalf("nonmatching games: %s", res.Content)
		}
	}
	// The fourth match sits at index 9 of the list, and that is where the next
	// page must resume.
	if result.NextOffset == nil || *result.NextOffset != 9 {
		t.Fatalf("scan does not resume at the limit: %s", res.Content)
	}
}

func TestGamesListRejectsInvalidPagination(t *testing.T) {
	for _, args := range []map[string]any{{"limit": 0}, {"limit": 51}, {"offset": -1}, {"limit": "bad"}} {
		e := &pagedGamesEngine{}
		c := mustCatalog(t, e, agenttools.Options{Policy: agenttools.PolicyReadonly})
		if res := call(t, c, "games_list", args); !res.IsError {
			t.Errorf("accepted %v", args)
		}
		if len(e.options) != 0 {
			t.Error("invalid pagination reached engine")
		}
	}
}
