// Package dzzzr is an HTTP client for the Dozor Classic city-quest game engine
// (classic.dzzzr.ru). It covers the player API (game state, code submission,
// hints, breaks, team log and chat) and the organizer's administration area
// (games, levels, teams, live monitoring, messages).
//
// The engine has two independent credentials. A site account (login and
// password) produces a session token through API/login.php; the token travels
// as the "s" query parameter. Game access additionally needs the team's
// captain login and the numeric PIN issued for the game, sent as HTTP Basic
// "{city}_{captain}:{pin}". Both are stored on the Client, see Credentials.
//
// Actions (submitting a code, taking a hint, abandoning a level) are POST
// requests whose reply is an HTTP redirect carrying the engine's result code
// in the "err" query parameter; the client never follows redirects and parses
// that code instead. ErrText translates codes to the engine's own wording.
package dzzzr
