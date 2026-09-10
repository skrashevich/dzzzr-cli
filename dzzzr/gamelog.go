package dzzzr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// The full log of a game — every level issued and every code entered, for all
// teams at once — is exported by {base}gameLogJSON.php?gmid=<game>. The engine
// offers the same data as a page (?section=gmlog) and as a spreadsheet
// (gameLogExcel.php); the JSON is the one worth reading.
//
// It is not part of the API: the handler sits in the site's own area and
// answers a visitor it will not show the log with an empty body. The rights
// are the scenario's — captain, deputy captain or chief of staff of a team
// that has played in this city recently. The session token is therefore
// required, and an empty reply is reported as a permission problem rather
// than as an unreadable one.
//
// The observed export is an object of numbered rows: row 1 is the header,
// subsequent rows are cell arrays. Duplicate headings receive numeric suffixes.
// Older object-record responses remain supported.

const gameLogPath = "gameLogJSON.php"

// GameLog is a game's event log as exported by the engine.
type GameLog struct {
	GameID int `json:"game_id"`
	// Columns are the record fields in the order the export listed them.
	Columns []string `json:"columns"`
	// Entries are the records, each keyed by the engine's own field names.
	Entries []map[string]string `json:"entries"`
}

// GetGameLog returns the full log of one game.
//
// It needs the site permissions for this game; a client
// without them gets an EngineError with code ErrNoPermission.
func (c *Client) GetGameLog(ctx context.Context, gameID int) (*GameLog, error) {
	if gameID <= 0 {
		return nil, fmt.Errorf("dzzzr: game log: game id must be positive, got %d", gameID)
	}
	req, err := c.newRequest(ctx, http.MethodGet, gameLogPath,
		url.Values{"gmid": {strconv.Itoa(gameID)}}, nil,
		requestOptions{siteCookie: true, session: true, requireAuth: true})
	if err != nil {
		return nil, err
	}
	status, _, body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if err := classifyStatus(status, body, "game log"); err != nil {
		return nil, err
	}
	body = trimEngineNoise(body)
	if isEmptyBody(body) {
		// The handler prints nothing at all rather than an error when it will
		// not show the log, which is how a visitor without the rights leaves.
		return nil, &EngineError{
			Code: ErrNoPermission,
			Text: fmt.Sprintf("движок не отдал лог игры %d: смотреть его может капитан, замкапитана или начштаба команды, недавно игравшей в этом городе", gameID),
		}
	}
	if looksLikeHTML(body) {
		// A signed-out visitor is answered with the site's own page instead.
		if err := siteAccessError(siteContent(decodeWindows1251IfNeeded(body))); err != nil {
			return nil, err
		}
		return nil, &UndecodableResponseError{StatusCode: status, Context: "game log", Err: errHTMLBody, Body: truncate(body)}
	}
	if err := engineEnvelope(body); err != nil {
		return nil, err
	}
	if err := bareErrEnvelope(body); err != nil {
		return nil, err
	}
	log := &GameLog{GameID: gameID}
	if err := decodeGameLog(body, log); err != nil {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "game log", Err: err, Body: truncate(body)}
	}
	return log, nil
}

const errNoLogRecords = stringError("в ответе нет массива записей лога")

// decodeGameLog reads the records out of the export. The engine may send the
// array on its own or wrap it in an object under a name of its choosing, so
// the first array of objects found at the top level is the log.
func decodeGameLog(body []byte, log *GameLog) error {
	var table map[string]json.RawMessage
	if json.Unmarshal(body, &table) == nil && table["1"] != nil {
		return decodeGameLogTable(table, log)
	}
	records, err := gameLogRecords(body)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, rec := range records {
		var fields map[string]any
		if err := decodeEngineJSON(rec, &fields); err != nil {
			return err
		}
		entry := make(map[string]string, len(fields))
		for k, v := range fields {
			entry[k] = jsonScalarString(v)
		}
		for _, k := range jsonObjectKeys(rec) {
			if !seen[k] {
				seen[k] = true
				log.Columns = append(log.Columns, k)
			}
		}
		log.Entries = append(log.Entries, entry)
	}
	return nil
}

// gameLogRecords locates the array of records in the export.
func gameLogRecords(body []byte) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var out []json.RawMessage
		if err := decodeEngineJSON(body, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	var doc map[string]json.RawMessage
	if err := decodeEngineJSON(body, &doc); err != nil {
		return nil, err
	}
	// Field order is not preserved by a map, so the wrapper key is taken in
	// the order the document wrote it and the first array of objects wins.
	for _, key := range jsonObjectKeys(trimmed) {
		raw, ok := doc[key]
		if !ok || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '[' {
			continue
		}
		var out []json.RawMessage
		if err := decodeEngineJSON(raw, &out); err != nil {
			continue
		}
		return out, nil
	}
	return nil, errNoLogRecords
}

// jsonObjectKeys returns an object's field names in document order, which a
// map cannot keep. Anything that is not an object yields none.
func jsonObjectKeys(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	t, err := dec.Token()
	if err != nil {
		return nil
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil
	}
	var keys []string
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return keys
		}
		k, ok := kt.(string)
		if !ok {
			return keys
		}
		keys = append(keys, k)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}

// jsonScalarString renders a decoded JSON value as the text a table shows.
// The engine writes numbers as numbers in some columns and as strings in
// others, so a caller comparing them wants one form.
func jsonScalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return StripHTML(t)
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// The site exports spreadsheet rows keyed by their one-based row number.
// Row 1 contains headings; duplicate headings need distinct map keys.
func decodeGameLogTable(table map[string]json.RawMessage, log *GameLog) error {
	var headers []string
	if err := json.Unmarshal(table["1"], &headers); err != nil {
		return err
	}
	if len(headers) == 0 {
		return errNoLogRecords
	}
	seen := map[string]bool{}
	for i, h := range headers {
		if h == "" {
			h = fmt.Sprintf("столбец %d", i+1)
		}
		key := h
		for n := 2; seen[key]; n++ {
			key = fmt.Sprintf("%s (%d)", h, n)
		}
		seen[key] = true
		log.Columns = append(log.Columns, key)
	}
	keys := make([]int, 0, len(table))
	for k := range table {
		n, err := strconv.Atoi(k)
		if err != nil || n < 1 || strconv.Itoa(n) != k {
			return errNoLogRecords
		}
		if n > 1 {
			keys = append(keys, n)
		}
	}
	slices.Sort(keys)
	log.Entries = make([]map[string]string, 0, len(keys))
	for _, k := range keys {
		var cells []any
		if err := json.Unmarshal(table[strconv.Itoa(k)], &cells); err != nil {
			return err
		}
		if len(cells) != len(headers) {
			return fmt.Errorf("game log row %d: expected %d cells, got %d", k, len(headers), len(cells))
		}
		entry := make(map[string]string, len(headers))
		for i, v := range cells {
			entry[log.Columns[i]] = jsonScalarString(v)
		}
		log.Entries = append(log.Entries, entry)
	}
	return nil
}
