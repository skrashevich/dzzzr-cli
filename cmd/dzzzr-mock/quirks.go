package main

import (
	"fmt"
	"slices"
	"strings"
)

// A quirk is one oddity of the real engine that the mock reproduces on
// request.
//
// The plain mock serves a well-behaved game, which is what most tests want.
// The shapes below only appear in states a fixed scenario cannot reach — a
// сквозное task in the main slot, a team with no game, a stopped engine — yet
// each of them broke a real client. Naming them makes those states reachable
// from a test and from the command line, so a fix cannot be believed on
// nothing more than a fixture.
type quirk string

const (
	// quirkNoise prefixes the game state with the leftover debug dump the
	// demo town wraps in an HTML comment.
	quirkNoise quirk = "noise"
	// quirkSkvozCurrent puts a сквозное task in the main level slot, where
	// templates/JSON/go_level.tpl adds a separator go.tpl has already
	// written and the reply becomes "},,". It reports isBonusLevel too,
	// which is the shape live level 12 had.
	quirkSkvozCurrent quirk = "skvoz-current"
	// quirkNoGame answers the game page with templates/JSON/go_nogame.tpl.
	quirkNoGame quirk = "nogame"
	// quirkStopped answers API/game.php with {"err":13}, the way the engine
	// does while the organizer has it switched off (API/game.php:96).
	quirkStopped quirk = "stopped"
	// quirkEmptyLevel answers API/game.php?level=1 with 200 and no body.
	quirkEmptyLevel quirk = "empty-level"
	// quirkChooseLevel fills NextLevelSelector with the engine's rendered
	// level-choice form.
	quirkChooseLevel quirk = "choose-level"
	// quirkLevelFinished gives the team time to collect bonus codes after
	// the last main one, so a level whose main codes are all in stays
	// current and the reply carries isLevelFinished "1". The default game
	// hands the next level over immediately (bonusAfter -1), which makes
	// that state unreachable in the only slot a player's summary renders.
	quirkLevelFinished quirk = "level-finished"
	// quirkSilentRefusal declines abandon with no result code at all, which
	// is what the live engine did when canAbandon was empty. Only abandon:
	// claiming more than is implemented is how a false parity claim starts.
	quirkSilentRefusal quirk = "silent-refusal"
	// quirkAdminNoAccess answers admin/?action=teams, admin/lookLog.php and
	// admin/gmcht.php with the engine's own 200 "Вы не имеете доступа к
	// этому разделу" reply, the way it does for an organizer account that
	// authenticates fine but is not this game's assigned organizer.
	// Measured against classic.dzzzr.ru on 2026-09-09; without a check for
	// this exact text, AdminListTeams/AdminGetLog misread it as "no access"
	// only by luck, and AdminListMessages/AdminSendMessage misread it as an
	// empty chat or a delivered message.
	quirkAdminNoAccess quirk = "admin-no-access"
	// quirkSelectedLevelZero prints "0." instead of a position for the level
	// open in the editor, the way the engine does. The real position stays in
	// that level's own order_p, which is what makes the list readable at all.
	// Measured against classic.dzzzr.ru on 2026-09-09: a client that reads
	// the printed number and drops a row without one loses exactly one level
	// of the game from every listing.
	quirkSelectedLevelZero quirk = "selected-level-zero"
	// quirkUnclosedForm leaves the create-team form open on the teams page,
	// the way the engine does. Per the HTML spec a <form> start tag is
	// ignored while a form element is open, so the team card that follows
	// disappears from the tree and its controls attach to the create form.
	// Measured against classic.dzzzr.ru on 2026-09-10: this alone made
	// admin-team answer "no updateTeam form" and admin-teams report no
	// applications for a game that had an accepted one.
	quirkUnclosedForm quirk = "unclosed-form"
)

// allQuirks lists every name -quirks accepts.
var allQuirks = []quirk{
	quirkNoise, quirkSkvozCurrent, quirkNoGame, quirkStopped,
	quirkEmptyLevel, quirkChooseLevel, quirkLevelFinished, quirkSilentRefusal,
	quirkAdminNoAccess, quirkSelectedLevelZero, quirkUnclosedForm,
}

// quirkSet is the set of quirks one mock serves.
type quirkSet map[quirk]bool

// has reports whether the mock serves the given quirk.
func (s *server) has(q quirk) bool { return s.quirks[q] }

// parseQuirks reads a comma-separated list of quirk names. An unknown name is
// an error rather than a quiet fall back to a well-behaved engine, which
// would let a typo make a test pass for the wrong reason.
func parseQuirks(list string) ([]quirk, error) {
	var out []quirk
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		q := quirk(name)
		if !slices.Contains(allQuirks, q) {
			return nil, fmt.Errorf("unknown quirk %s; known: %s", name, quirkNames())
		}
		out = append(out, q)
	}
	return out, nil
}

// quirkNames lists the accepted names, for the flag's help and for the error
// an unknown one produces.
func quirkNames() string {
	names := make([]string, 0, len(allQuirks))
	for _, q := range allQuirks {
		names = append(names, string(q))
	}
	return strings.Join(names, ", ")
}
