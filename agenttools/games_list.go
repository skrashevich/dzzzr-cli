package agenttools

import (
	"context"
	"fmt"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func gamesListPage(ctx context.Context, e Engine, args arguments) (any, error) {
	offset, limit := 0, 20
	var err error
	if _, ok := args["offset"]; ok {
		offset, err = args.requireInt("offset")
		if err != nil {
			return nil, err
		}
	}
	if _, ok := args["limit"]; ok {
		limit, err = args.requireInt("limit")
		if err != nil {
			return nil, err
		}
	}
	if offset < 0 || limit < 1 || limit > 50 {
		return nil, fmt.Errorf("offset must be non-negative and limit must be 1..50")
	}
	newOnly, _ := args.optionalBool("new_only")
	archive, _ := args.optionalBool("archive")
	if team, ok := args.optionalString("team"); ok {
		return gamesForTeam(ctx, e, dzzzr.GamesListOptions{NewOnly: newOnly, Archive: archive, Offset: offset}, limit, team)
	}
	// A single lookahead row tells the model whether another page exists.
	// An explicit limit also disables the client's automatic full-archive walk.
	games, err := e.GetGamesList(ctx, dzzzr.GamesListOptions{NewOnly: newOnly, Archive: archive, Offset: offset, Limit: limit + 1})
	if err != nil {
		return nil, err
	}
	count := min(len(games), limit)
	result := map[string]any{"games": newGameListViews(games[:count]), "offset": offset, "count": count}
	if len(games) > limit {
		result["next_offset"] = offset + count
	}
	return result, nil
}

func normalizeTeam(s string) string { return strings.Join(strings.Fields(dzzzr.StripHTML(s)), " ") }

// The offset remains an engine offset, not a count of matches. Bound each
// scan so a sparse match cannot trigger an unlimited number of requests.
func gamesForTeam(ctx context.Context, e Engine, opts dzzzr.GamesListOptions, limit int, team string) (any, error) {
	start := opts.Offset
	opts.Limit = 50
	team = normalizeTeam(team)
	if team == "" {
		return nil, fmt.Errorf("team must be a non-empty name")
	}
	if opts.Archive {
		return archiveGamesForTeam(ctx, e, opts, limit, team)
	}
	matches := []dzzzr.GameInfo{}
	page := func(next *int) any {
		result := map[string]any{"games": newGameListViews(matches), "offset": start, "count": len(matches), "team": team}
		if next != nil {
			result["next_offset"] = *next
		}
		return result
	}
	for range 10 {
		games, err := e.GetGamesList(ctx, opts)
		if err != nil {
			return nil, err
		}
		for i, g := range games {
			found := false
			for _, name := range g.Teams {
				if strings.EqualFold(normalizeTeam(name), team) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
			if len(matches) == limit {
				return page(new(opts.Offset + i)), nil
			}
			matches = append(matches, g)
		}
		opts.Offset += len(games)
		if len(games) < opts.Limit {
			return page(nil), nil
		}
	}
	return page(new(opts.Offset)), nil
}

// An archive list row carries no roster, so whether a team played a finished
// game is only visible in that game's public results table. Cap the extra
// fetches so a sparse match cannot turn one call into an unbounded crawl;
// next_offset lets the model resume where the cap stopped it.
const maxArchiveStatFetches = 60

func archiveGamesForTeam(ctx context.Context, e Engine, opts dzzzr.GamesListOptions, limit int, team string) (any, error) {
	start := opts.Offset
	opts.Limit = 50
	matches := []teamArchiveResultView{}
	statErrors := 0
	page := func(next *int) any {
		result := map[string]any{"games": matches, "offset": start, "count": len(matches), "team": team}
		if next != nil {
			result["next_offset"] = *next
		}
		if statErrors > 0 {
			result["stat_errors"] = statErrors
		}
		return result
	}
	fetches := 0
	for range 10 {
		games, err := e.GetGamesList(ctx, opts)
		if err != nil {
			return nil, err
		}
		for i, g := range games {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// Both stops come before the fetch: the game they stop on is
			// examined again from next_offset, not paid for twice.
			if len(matches) == limit || fetches == maxArchiveStatFetches {
				return page(new(opts.Offset + i)), nil
			}
			fetches++
			stat, err := e.GetArchiveStat(ctx, g.ID.Int())
			if err != nil {
				// One unreadable table must not hide the rest of the archive.
				statErrors++
				continue
			}
			for _, t := range stat.Teams {
				if !strings.EqualFold(normalizeTeam(t.Name), team) {
					continue
				}
				matches = append(matches, teamArchiveResultView{
					ID: g.ID.Int(), Name: dzzzr.StripHTML(g.Name), Number: g.Number.String(), Date: g.Date,
					Place: stat.Cell(&t, "Место"), Gap: stat.Cell(&t, "Отставание"),
				})
				break
			}
		}
		opts.Offset += len(games)
		if len(games) < opts.Limit {
			return page(nil), nil
		}
		// Stopping here spares a list request the next page would only reject.
		if len(matches) == limit || fetches == maxArchiveStatFetches {
			return page(new(opts.Offset)), nil
		}
	}
	return page(new(opts.Offset)), nil
}
