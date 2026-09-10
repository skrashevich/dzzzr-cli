package dzzzr_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// The engine assembles JSON by hand: API/gamesList.php strips "\r\n", "\n"
// and "\t" from the document it built, so a lone carriage return from the
// database survives inside a string and makes the reply invalid JSON. One
// such byte in one announcement used to cost the whole games list.
func TestGamesListWithBareCarriageReturn(t *testing.T) {
	body := "{ \"games\" : [{\"id\" : 4242,\"project\" : \"\",\"number\" : \"112\",\"name\" : \"Ночной дозор\"," +
		"\"league\" : 1,\"date\" : \"2026-09-08 21:00:00\",\"authors\" : \"Штаб\",\"territory\" : \"ЦАО\"," +
		"\"additionalTools\" : \"фонарь\",\"legend\" : \"Первая строка\rвторая строка\",\"information\" : \"\"," +
		"\"image\" : null,\"season\" : 12,\"start\" : \"Парковка\",\"isPrequelAvailable\" : false," +
		"\"isLeague\" : false,\"isLinear\" : true,\"teams\" : null}]}"

	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	games, err := c.GetGamesList(context.Background(), dzzzr.GamesListOptions{})
	if err != nil {
		t.Fatalf("a bare carriage return must not cost the whole list: %v", err)
	}
	if len(games) != 1 || games[0].ID != 4242 || games[0].Name != "Ночной дозор" {
		t.Fatalf("games = %+v", games)
	}
	// The repair escapes the byte rather than dropping it, so no words merge.
	if !strings.Contains(games[0].Legend, "Первая строка") || !strings.Contains(games[0].Legend, "\rвторая") {
		t.Errorf("legend = %q", games[0].Legend)
	}
}

// The engine copies an announcement into the JSON string as it stands, so a
// backslash its author typed opens an escape sequence no parser knows. Both
// shapes below are live archived games of the Moscow city: someone's
// "верёвки\лестницы", and the "\&lsquo;" a WYSIWYG editor left in a style.
func TestGamesListWithStrayBackslash(t *testing.T) {
	const legend = `<span style="font-family: \&lsquo;times new roman\&lsquo;">верёвки\лестницы</span>`
	body := `{ "games" : [{"id" : 4242,"number" : "112","name" : "Ночной дозор",` +
		`"legend" : "` + strings.ReplaceAll(legend, `"`, `\"`) + `","teams" : null}]}`

	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	games, err := c.GetGamesList(context.Background(), dzzzr.GamesListOptions{})
	if err != nil {
		t.Fatalf("a stray backslash must not cost the whole list: %v", err)
	}
	// The repair restores the byte the author typed rather than dropping it.
	if len(games) != 1 || games[0].Legend != legend {
		t.Fatalf("legend = %q", games[0].Legend)
	}
}

// A backslash that does open a valid escape still means what JSON says it
// means, so the repair must not double it.
func TestDecodeEngineJSONKeepsValidEscapes(t *testing.T) {
	var out struct {
		S string `json:"s"`
	}
	// The trailing carriage return is what makes the reply need a repair at
	// all; without it the document is valid and never reaches this pass.
	if err := dzzzr.DecodeEngineJSONForTest([]byte("{\"s\":\"a\\\\b\\\"c\\u0041d\\/e\r\"}"), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.S != "a\\b\"cAd/e\r" {
		t.Errorf("s = %q", out.S)
	}
}

// A "\u" that is not followed by four hex digits is not an escape either, and
// the byte after the backslash may be a quote that must not end the string.
func TestDecodeEngineJSONRepairsTruncatedUnicodeEscape(t *testing.T) {
	var out struct {
		S string `json:"s"`
		N int    `json:"n"`
	}
	if err := dzzzr.DecodeEngineJSONForTest([]byte(`{"s":"\u00zz\ ","n":1}`), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.S != `\u00zz\ ` || out.N != 1 {
		t.Errorf("out = %+v", out)
	}
}

func TestGameStateWithControlCharacters(t *testing.T) {
	body := `{"gameName":"Тест","gameId":"4242","level":{"levelNumber":"1","question":"Строка` + "\r\v" +
		`продолжение","totalCodes":"1","spoilers":[],"bonusCodesTimes":[]},"bonusLevels":[ { }],"messages":[],"errNo":0,"errText":""}`
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	st, err := c.GetGame(context.Background())
	if err != nil {
		t.Fatalf("control characters in a task must not break the state: %v", err)
	}
	if st.Level == nil || !strings.Contains(st.Level.Question, "продолжение") {
		t.Fatalf("level = %+v", st.Level)
	}
}

func TestBrokenJSONStillFails(t *testing.T) {
	// A reply that is not JSON at all must still be reported, not silently
	// repaired into something.
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"games" : [{"id" : 4242,`))
	}))
	if _, err := c.GetGamesList(context.Background(), dzzzr.GamesListOptions{}); err == nil {
		t.Fatal("truncated JSON must fail")
	}
}

func TestControlCharacterOutsideStringsIsLeftAlone(t *testing.T) {
	// Whitespace between tokens is legal JSON and must not be touched.
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\r\n\t\"games\" : null }"))
	}))
	games, err := c.GetGamesList(context.Background(), dzzzr.GamesListOptions{})
	if err != nil || len(games) != 0 {
		t.Fatalf("games = %+v, err = %v", games, err)
	}
}

// The engine answers with a bare {"error": …} envelope and HTTP 200 not only
// for an unknown session: APIconst.php's connectDB() does the same when the
// town's database is unreachable. Decoding that as an empty game would have
// the client report a game that does not exist.
func TestEngineErrorEnvelopeIsNotAnEmptyState(t *testing.T) {
	const dbDown = `{"error" : "Can't connect to DB"}`
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(dbDown))
	}))
	ctx := context.Background()

	if st, err := c.GetGame(ctx); err == nil {
		t.Errorf("GetGame returned %+v, want an error", st)
	} else if !dzzzr.IsEngineError(err) || !strings.Contains(err.Error(), "connect to DB") {
		t.Errorf("GetGame err = %v", err)
	}
	if games, err := c.GetGamesList(ctx, dzzzr.GamesListOptions{}); err == nil {
		t.Errorf("GetGamesList returned %+v, want an error", games)
	}
	if st, err := c.GetStat(ctx); err == nil {
		t.Errorf("GetStat returned %+v, want an error", st)
	}
	if _, err := c.GetLog(ctx); err == nil {
		t.Error("GetLog must fail")
	}
	if _, err := c.GetMessages(ctx, ""); err == nil {
		t.Error("GetMessages must fail")
	}
	if err := c.PostMessage(ctx, "текст"); err == nil {
		t.Error("PostMessage must fail")
	}

	// The session error stays an auth error, so the caller is still told to
	// sign in rather than to wait for the engine.
	c = authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error" : "Ошибка авторизации. Неверный идентификатор сессии"}`))
	}))
	if _, err := c.GetGame(ctx); dzzzr.AuthErrorKindOf(err) != dzzzr.AuthSession {
		t.Errorf("session envelope err = %v", err)
	}
}

// A payload that happens to carry an "error" field alongside real data is not
// an envelope: the game state always has errNo, and the games list has games.
func TestErrorFieldNextToDataIsNotAnEnvelope(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"что-то","gameName":"Тест","gameId":"1","level":{"levelNumber":"2","totalCodes":"1","spoilers":[],"bonusCodesTimes":[]},"bonusLevels":[ { }],"messages":[],"errNo":0,"errText":""}`))
	}))
	st, err := c.GetGame(context.Background())
	if err != nil {
		t.Fatalf("a state with errNo must decode: %v", err)
	}
	if st.Level == nil || st.Level.LevelNumber != 2 {
		t.Errorf("state = %+v", st)
	}
}

// templates/JSON/go_level.tpl appends its own separator on a сквозное level
// while go.tpl has already written one, so the engine sends "},," and the
// whole state is undecodable. Seen live on the demo game's level 12.
func TestDecodeEngineJSONRepairsDoubledComma(t *testing.T) {
	body := []byte(`{"gameName":"Тестовая","level":{"levelNumber":"12","skvoz":true},,"errNo":0}`)
	var st dzzzr.GameState
	if err := dzzzr.DecodeEngineJSONForTest(body, &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.GameName != "Тестовая" || st.Level == nil || st.Level.LevelNumber.Int() != 12 {
		t.Errorf("state = %+v", st)
	}
	if !st.Level.Skvoz.Bool() {
		t.Error("skvoz lost")
	}
}

func TestDecodeEngineJSONDropsTrailingCommas(t *testing.T) {
	var out struct {
		A []int `json:"a"`
		B int   `json:"b"`
	}
	if err := dzzzr.DecodeEngineJSONForTest([]byte(`{"a":[1,2,],"b":3,}`), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.A) != 2 || out.B != 3 {
		t.Errorf("out = %+v", out)
	}
}

// The repair must not touch a comma inside a string.
func TestDecodeEngineJSONKeepsCommasInStrings(t *testing.T) {
	var out struct {
		S string `json:"s"`
	}
	if err := dzzzr.DecodeEngineJSONForTest([]byte("{\"s\":\"a,,b\r\"}"), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.S != "a,,b\r" {
		t.Errorf("s = %q", out.S)
	}
}
