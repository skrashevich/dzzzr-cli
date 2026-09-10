package dzzzr

import (
	"bytes"
	"encoding/json"
)

// The engine builds most of its JSON by concatenating strings in PHP rather
// than encoding it, so what it sends is not always valid JSON. One offender
// is a bare carriage return inside a string: API/gamesList.php
// strips "\r\n", "\n" and "\t" from the document it assembled, which leaves a
// lone "\r" — legal in the database, illegal inside a JSON string. A single
// such byte in one game's announcement makes the whole reply undecodable.
//
// decodeEngineJSON therefore decodes normally and, only when that fails,
// escapes the raw control characters and tries once more. Repairing the reply
// unconditionally would hide a real change in the protocol behind a rewrite
// of every payload.
func decodeEngineJSON(body []byte, v any) error {
	err := json.Unmarshal(body, v)
	if err == nil {
		return nil
	}
	repaired, changed := repairEngineJSON(body)
	if !changed {
		return err
	}
	if err2 := json.Unmarshal(repaired, v); err2 != nil {
		// Report the original failure: the repair is a courtesy, and its own
		// error would only describe a document the engine never sent.
		return err
	}
	return nil
}

// repairEngineJSON applies every known repair to a reply the engine built by
// hand, and reports whether any of them changed something.
func repairEngineJSON(body []byte) ([]byte, bool) {
	// Stray backslashes go first: doubling one leaves the byte after it to be
	// judged on its own, so a backslash followed by a raw control character
	// still reaches the pass that escapes controls.
	out, strays := escapeStrayBackslashesInJSONStrings(body)
	out, changed := escapeControlsInJSONStrings(out)
	out, dropped := dropEmptyJSONElements(out)
	return out, strays || changed || dropped
}

// escapeStrayBackslashesInJSONStrings doubles a backslash that does not open a
// valid escape sequence, and reports whether anything was changed.
//
// The engine copies an announcement out of the database into the JSON string
// as it stands, so a backslash someone typed arrives unescaped and opens an
// escape sequence no parser knows: "веревки\лестницы" in a list of equipment,
// or the "\&lsquo;" a WYSIWYG editor leaves behind in a style attribute. One
// such byte in one archived game used to cost the whole list.
//
// A backslash that does start a valid escape is left alone, so a document the
// engine escaped properly is never rewritten.
func escapeStrayBackslashesInJSONStrings(body []byte) ([]byte, bool) {
	out := make([]byte, 0, len(body))
	inString := false
	changed := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case inString && c == '\\':
			if !opensJSONEscape(body[i+1:]) {
				changed = true
				out = append(out, '\\', '\\')
				continue
			}
			// Copy the escape whole: its second byte may be a quote, and
			// letting the scanner see that would end the string early.
			out = append(out, c, body[i+1])
			i++
			continue
		case c == '"':
			inString = !inString
		}
		out = append(out, c)
	}
	if !changed {
		return body, false
	}
	return out, true
}

// opensJSONEscape reports whether rest, the bytes right after a backslash
// inside a string, spell an escape sequence JSON allows.
func opensJSONEscape(rest []byte) bool {
	if len(rest) == 0 {
		return false
	}
	switch rest[0] {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return true
	case 'u':
		if len(rest) < 5 {
			return false
		}
		for _, c := range rest[1:5] {
			if !isHexDigit(c) {
				return false
			}
		}
		return true
	}
	return false
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// dropEmptyJSONElements removes the stray commas the engine emits when a
// template contributes its own separator on top of the one around it.
//
// templates/JSON/go_level.tpl ends with "}{if $r1.skvoz != ”},{/if}", while
// go.tpl already writes "level": {$MainLevelText},. A team whose current
// level is a сквозное задание therefore gets "},," and the whole game state
// becomes undecodable. A comma with nothing before it is never valid JSON, so
// collapsing runs of them and dropping one that sits right before a closing
// brace or bracket cannot change the meaning of a document that was already
// valid.
func dropEmptyJSONElements(body []byte) ([]byte, bool) {
	out := make([]byte, 0, len(body))
	inString := false
	escaped := false
	changed := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && c == ',':
			if next := nextJSONByte(body[i+1:]); next == ',' || next == '}' || next == ']' {
				changed = true
				continue
			}
		}
		out = append(out, c)
	}
	if !changed {
		return body, false
	}
	return out, true
}

// nextJSONByte returns the next byte that is not JSON whitespace, or 0.
func nextJSONByte(b []byte) byte {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		}
		return c
	}
	return 0
}

// escapeControlsInJSONStrings rewrites raw control bytes that appear inside
// JSON string literals as their escape sequences, leaving everything outside
// a string untouched (there they are legal whitespace). It reports whether
// anything was changed.
func escapeControlsInJSONStrings(body []byte) ([]byte, bool) {
	out := make([]byte, 0, len(body))
	inString := false
	escaped := false
	changed := false
	for _, c := range body {
		switch {
		case escaped:
			// The previous byte was a backslash, so this one is part of an
			// escape sequence and means nothing to the scanner.
			escaped = false
			out = append(out, c)
		case inString && c == '\\':
			escaped = true
			out = append(out, c)
		case c == '"':
			inString = !inString
			out = append(out, c)
		case inString && c < 0x20:
			changed = true
			out = append(out, escapeControl(c)...)
		default:
			out = append(out, c)
		}
	}
	if !changed {
		return body, false
	}
	return out, true
}

const hexDigits = "0123456789abcdef"

func escapeControl(c byte) []byte {
	switch c {
	case '\n':
		return []byte(`\n`)
	case '\r':
		return []byte(`\r`)
	case '\t':
		return []byte(`\t`)
	case '\b':
		return []byte(`\b`)
	case '\f':
		return []byte(`\f`)
	}
	return []byte{'\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0x0F]}
}

// trimEngineNoise removes what a deployment printed before the JSON.
//
// The live demo town answers go/?api=true with a leftover debug dump that the
// engine wraps in an HTML comment:
//
//	<!--string(11) "dzzzr_ndemo"
//	-->{ "gameName": "Тестовая", ...
//
// The payload after it is ordinary JSON, so only the comment has to go.
// Comments are stripped from the front alone, and the result is kept only
// when what remains actually starts like JSON — a genuine HTML page opening
// with a comment is returned untouched so the caller still rejects it.
func trimEngineNoise(body []byte) []byte {
	rest := bytes.TrimLeft(body, " \t\r\n")
	for bytes.HasPrefix(rest, []byte("<!--")) {
		end := bytes.Index(rest, []byte("-->"))
		if end < 0 {
			return body
		}
		rest = bytes.TrimLeft(rest[end+len("-->"):], " \t\r\n")
	}
	if len(rest) == 0 || (rest[0] != '{' && rest[0] != '[') {
		return body
	}
	return rest
}

// isEmptyBody reports whether the engine answered with nothing. It does that
// under HTTP 200 when a handler exits early — API/game.php does it for a
// blocked team — and every read has to name it rather than talk about JSON
// syntax it never received.
func isEmptyBody(body []byte) bool { return len(bytes.TrimSpace(body)) == 0 }
