package main

import (
	"errors"
	"net/http"
	"sort"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// webGame is one row of the informational game list. The agent's tools always
// act on the city's current game, so this is shown to orient the user and
// nothing is selected from it.
type webGame struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Date  string `json:"date"`
	Start string `json:"start"`
}

// httpCatalogGames answers with whatever games the current credentials can
// reach. A run started with only organizer credentials — «dzzzr web -admin-login
// … -admin-password …» — has no playing session, so the player-facing
// gamesList.php rejects it with a session error; the administration area still
// lists the city's games. Either source alone is enough, and the two are
// merged when both answer. The list only fails when neither does.
func (h *webHub) httpCatalogGames(w http.ResponseWriter, r *http.Request) {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()

	merge := newGameMerge()
	var errs []error

	// The playing session first: it carries the start time the admin table
	// omits. A stale or expired session is worth one silent re-sign-in, the
	// same recovery the read tools make.
	player, err := h.client.GetGamesList(r.Context(), dzzzr.GamesListOptions{})
	if err != nil && refreshSession(r.Context(), h.cfg, h.client, err) {
		player, err = h.client.GetGamesList(r.Context(), dzzzr.GamesListOptions{})
	}
	if err != nil {
		errs = append(errs, err)
	}
	for _, g := range player {
		merge.add(g.ID.Int(), g.Name, g.Date, g.Start)
	}

	// The administration area needs no playing session: the organizer
	// credentials reach it on their own.
	if h.client.HasAdminCredentials() {
		admin, aerr := h.client.AdminListGames(r.Context())
		if aerr != nil {
			errs = append(errs, aerr)
		}
		for _, g := range admin {
			merge.add(g.ID, g.Name, g.Date, "")
		}
	}

	items := merge.list()
	if len(items) == 0 && len(errs) > 0 {
		webError(w, http.StatusBadGateway, "не удалось получить список игр: %v", errors.Join(errs...))
		return
	}
	webWriteJSON(w, http.StatusOK, map[string]any{"games": items})
}

// gameMerge collects rows from the player list and the admin table into one
// set keyed by game ID, so a game listed by both is shown once with the
// fuller name and whatever start time only the player list carries.
type gameMerge struct {
	byID  map[int]*webGame
	order []int
}

func newGameMerge() *gameMerge {
	return &gameMerge{byID: map[int]*webGame{}}
}

func (m *gameMerge) add(id int, name, date, start string) {
	if id <= 0 {
		return
	}
	g, ok := m.byID[id]
	if !ok {
		g = &webGame{ID: id}
		m.byID[id] = g
		m.order = append(m.order, id)
	}
	if len(name) > len(g.Name) {
		g.Name = name
	}
	if g.Date == "" {
		g.Date = date
	}
	if g.Start == "" {
		g.Start = start
	}
}

func (m *gameMerge) list() []webGame {
	sort.Ints(m.order)
	out := make([]webGame, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, *m.byID[id])
	}
	return out
}
