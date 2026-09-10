package dzzzr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// GetGame fetches the team's current view of the game (go/?api=true).
//
// The engine requires the site session; captain/PIN are sent too so the
// request also passes the Basic wall that fronts some deployments.
func (c *Client) GetGame(ctx context.Context) (*GameState, error) {
	q := url.Values{"api": {"true"}}
	req, err := c.newRequest(ctx, http.MethodGet, "go/", q, nil, requestOptions{basic: true, session: true, pin: true, requireAuth: true})
	if err != nil {
		return nil, err
	}
	status, _, body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if err := classifyStatus(status, body, "game state"); err != nil {
		return nil, err
	}
	body = trimEngineNoise(body)
	if isEmptyBody(body) {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "game state", Err: errEmptyBody}
	}
	if looksLikeHTML(body) {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "game state", Err: errHTMLBody, Body: truncate(body)}
	}
	if err := engineEnvelope(body); err != nil {
		return nil, err
	}
	var st GameState
	if err := decodeEngineJSON(body, &st); err != nil {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "game state", Err: err, Body: truncate(body)}
	}
	return &st, nil
}

// action posts a form to go/?api=true and returns the engine's result code.
//
// The engine answers every action with a redirect to the game page whose
// query carries err=N. entcod appends api=true to that redirect, the other
// actions do not; the client follows neither and reads the code from the
// Location header. A 200 with a JSON body (some deployments answer directly)
// is decoded for errNo.
func (c *Client) action(ctx context.Context, form url.Values) (*ActionResult, error) {
	q := url.Values{"api": {"true"}}
	req, err := c.newRequest(ctx, http.MethodPost, "go/", q, form, requestOptions{basic: true, session: true, pin: true, requireAuth: true})
	if err != nil {
		return nil, err
	}
	// go2.php only accepts POSTs whose Referer is on dzzzr.ru or that carry
	// api=true; the referer keeps browsers-only deployments happy as well.
	req.Header.Set("Referer", c.resolve("go/", nil))
	status, hdr, body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	ctxName := "action " + form.Get("action")
	switch {
	case status == http.StatusMovedPermanently || status == http.StatusFound || status == http.StatusSeeOther || status == http.StatusTemporaryRedirect:
		loc := hdr.Get("Location")
		code, penalty, err := resultCodeFromLocation(loc)
		if err != nil {
			return nil, &UndecodableResponseError{StatusCode: status, Context: ctxName, Err: err, Body: loc}
		}
		return &ActionResult{Err: code, Text: actionText(code, penalty), Penalty: penalty, Location: loc}, nil
	case status == http.StatusUnauthorized:
		return nil, &AuthError{Kind: AuthBasic, Message: strings.TrimSpace(string(body))}
	case status == http.StatusOK:
		body = trimEngineNoise(body)
		if isEmptyBody(body) {
			return nil, &UndecodableResponseError{StatusCode: status, Context: ctxName, Err: errEmptyBody}
		}
		if looksLikeHTML(body) {
			return nil, &UndecodableResponseError{StatusCode: status, Context: ctxName, Err: errHTMLBody, Body: truncate(body)}
		}
		if err := engineEnvelope(body); err != nil {
			return nil, err
		}
		var st struct {
			ErrNo   FlexInt `json:"errNo"`
			Err     FlexInt `json:"err"`
			ErrText string  `json:"errText"`
		}
		if err := decodeEngineJSON(body, &st); err != nil {
			return nil, &UndecodableResponseError{StatusCode: status, Context: ctxName, Err: err, Body: truncate(body)}
		}
		code := st.ErrNo.Int()
		if code == 0 {
			code = st.Err.Int()
		}
		// templates/JSON/go.tpl sends the engine's own wording beside the
		// code. Preferring it keeps the interpolations the local table cannot
		// produce — an organizer's phone number, a try limit, a greeting.
		//
		// Only when there is a code, though: go.tpl puts errText on every
		// state reply, not just on an answer to an action, so standing text
		// beside errNo 0 would make a control action the engine performed
		// look like one it refused.
		text := ErrText(code)
		if code != 0 {
			if own := StripHTML(st.ErrText); own != "" {
				text = own
			}
		}
		return &ActionResult{Err: code, Text: text}, nil
	default:
		return nil, &HTTPError{StatusCode: status, Context: ctxName}
	}
}

// resultCodeFromLocation extracts err=N from a redirect target, along with
// the penalty the engine reports beside it. A missing or empty err is code 0
// (the engine sends "err=" for "nothing to report").
//
// go/errors.php interpolates $_GET[bcs] into the text of code 42, so the
// minutes charged for an early hint only exist in the redirect: the live
// engine answers beforeClue with "?...&err=42&bcs=21&...".
func resultCodeFromLocation(loc string) (code, penalty int, err error) {
	if loc == "" {
		return 0, 0, errNoLocation
	}
	u, err := url.Parse(loc)
	if err != nil {
		return 0, 0, err
	}
	q := u.Query()
	if v := strings.TrimSpace(q.Get("bcs")); v != "" {
		penalty, _ = strconv.Atoi(v)
	}
	v := strings.TrimSpace(q.Get("err"))
	if v == "" {
		return 0, penalty, nil
	}
	code, err = strconv.Atoi(v)
	if err != nil {
		return 0, penalty, err
	}
	return code, penalty, nil
}

// SendCode submits a code for the current main level (action=entcod).
func (c *Client) SendCode(ctx context.Context, code string) (*ActionResult, error) {
	return c.action(ctx, url.Values{"action": {"entcod"}, "cod": {code}})
}

// SendBonusCode submits a code for a bonus (skvoz) level that runs alongside
// the main line; levelNumber is the bonus level's number.
func (c *Client) SendBonusCode(ctx context.Context, levelNumber int, code string) (*ActionResult, error) {
	return c.action(ctx, url.Values{"action": {"entcod"}, "cod": {code}, "skvoz": {"1"}, "level": {strconv.Itoa(levelNumber)}})
}

// Abandon gives up the current level and asks for the next one. Only the
// captain, chief of staff or an authorized helper may do this, and only
// twice per game.
func (c *Client) Abandon(ctx context.Context) (*ActionResult, error) {
	return c.action(ctx, url.Values{"action": {"abandon"}})
}

// TakeBreak schedules a 15-minute break after the current level ends, or
// cancels one already scheduled.
func (c *Client) TakeBreak(ctx context.Context) (*ActionResult, error) {
	return c.action(ctx, url.Values{"action": {"break"}})
}

// StopBreak ends a running break early.
//
// None of these three carries a level: templates/NEW/go_header.tpl posts the
// action alone (breakStop adds only the interface's bonus flag). Asking for
// one made the client refuse to end a break, since the engine issues no level
// while a team is paused.
func (c *Client) StopBreak(ctx context.Context) (*ActionResult, error) {
	// bonus is the page's own "show bonus levels" checkbox, which its form
	// round-trips. An API client has no such preference to carry, and the
	// live engine accepted the empty value without changing anything.
	return c.action(ctx, url.Values{"action": {"breakStop"}, "bonus": {""}})
}

// TakeHint requests hint 1 or 2 of a level that issues hints on demand
// (bonus levels with the "on request" flag). The engine logs event 4 or 5.
func (c *Client) TakeHint(ctx context.Context, level, hint int) (*ActionResult, error) {
	event := "4"
	if hint == 2 {
		event = "5"
	}
	return c.action(ctx, url.Values{"action": {"takeClue"}, "event": {event}, "level": {strconv.Itoa(level)}})
}

// TakeHintEarly requests hint 1 or 2 of the given level before its time, for
// a penalty (the game's clueBeforeShtraf plus the minutes skipped). The
// penalty lands in ActionResult.Penalty.
//
// Both parameters matter. templates/NEW/go_level.tpl posts clue and level
// beside the action, and the live engine given neither picks a hint of its
// own: on a level whose first hint was still due it issued the second one,
// charged for it and left the first unissued, which also keeps abandon
// locked.
func (c *Client) TakeHintEarly(ctx context.Context, level, hint int) (*ActionResult, error) {
	if hint != 1 && hint != 2 {
		return nil, fmt.Errorf("dzzzr: take hint early: hint must be 1 or 2, got %d", hint)
	}
	return c.action(ctx, url.Values{
		"action": {"beforeClue"},
		"clue":   {strconv.Itoa(hint)},
		"level":  {strconv.Itoa(level)},
	})
}

// NextLevel moves on to the next level once all main codes are found but
// bonus codes remain (games with bonusAfter).
func (c *Client) NextLevel(ctx context.Context, level int) (*ActionResult, error) {
	return c.action(ctx, url.Values{"action": {"nextLevel"}, "level": {strconv.Itoa(level)}})
}

// SelectLevel picks the next level in games that let the team choose;
// current is the level being played (the engine refuses stale choices).
func (c *Client) SelectLevel(ctx context.Context, current, next int) (*ActionResult, error) {
	return c.action(ctx, url.Values{"action": {"selectlevel"}, "llevel": {strconv.Itoa(current)}, "level": {strconv.Itoa(next)}})
}

// SendSpoilerCode submits the code that unlocks a spoiler of the given level.
func (c *Client) SendSpoilerCode(ctx context.Context, level int, code string) (*ActionResult, error) {
	return c.action(ctx, url.Values{"action": {"spoilerCode"}, "skvoz": {strconv.Itoa(level)}, "spoilerCode": {code}})
}

// SendMessageToOrg posts a message to the organizer through the game
// interface (action=send_message). Captain, chief of staff or helper only.
func (c *Client) SendMessageToOrg(ctx context.Context, text string) (*ActionResult, error) {
	return c.action(ctx, url.Values{"action": {"send_message"}, "content": {text}})
}

// SaveOptions stores the interface options the engine keeps per user
// (nostat, notext, notags, refresh, log, legend, bonus, kladMap).
func (c *Client) SaveOptions(ctx context.Context, options map[string]string) (*ActionResult, error) {
	form := url.Values{"action": {"saveOptions"}}
	for k, v := range options {
		form.Set("option["+k+"]", v)
	}
	return c.action(ctx, form)
}

// classifyStatus maps non-200 statuses of read endpoints to errors.
func classifyStatus(status int, body []byte, ctxName string) error {
	switch {
	case status == http.StatusOK:
		return nil
	case status == http.StatusUnauthorized:
		return &AuthError{Kind: AuthBasic, Message: strings.TrimSpace(StripHTML(string(body)))}
	default:
		return &HTTPError{StatusCode: status, Context: ctxName}
	}
}

// bareErrEnvelope recognizes the {"err": N} reply API/game.php sends instead
// of a payload while the organizer has the engine switched off:
//
//	if ($siteOptions['botStoped'.$Project]) { print json_encode(Array('err' => 13)); exit; }
//
// It carries no "error" key, so engineEnvelope does not see it, and every
// read endpoint would otherwise decode it into an empty stat, log or level
// and report a success. Only a document whose single key is a non-zero "err"
// qualifies; an action's reply names its result the same way but arrives on a
// path that means to read it.
func bareErrEnvelope(body []byte) error {
	t := strings.TrimSpace(string(body))
	if !strings.HasPrefix(t, "{") || !strings.Contains(t, `"err"`) {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := decodeEngineJSON(body, &fields); err != nil || len(fields) != 1 {
		return nil
	}
	raw, ok := fields["err"]
	if !ok {
		return nil
	}
	var code FlexInt
	if err := json.Unmarshal(raw, &code); err != nil || code.Int() == 0 {
		return nil
	}
	return &EngineError{Code: code.Int(), Text: ErrText(code.Int())}
}

// actionText renders the engine's text for a result code, filling in the
// penalty that go/errors.php interpolates from the redirect.
func actionText(code, penalty int) string {
	text := ErrText(code)
	if code == ErrEarlyHintPenalty && penalty > 0 && text != "" {
		text += " " + strconv.Itoa(penalty) + " мин."
	}
	return text
}

// engineEnvelope recognizes the bare {"error": "..."} reply the engine sends
// instead of a payload, and returns it as the error to report.
//
// APIconst.php answers an unknown session that way, and its connectDB() the
// same way when the town's database is unreachable — both with HTTP 200. A
// reply that is only an error is never a valid payload for any endpoint, so
// anything else would decode as an empty, successful state and the caller
// would report a game that does not exist.
func engineEnvelope(body []byte) error {
	t := strings.TrimSpace(string(body))
	if !strings.HasPrefix(t, "{") || !strings.Contains(t, `"error"`) {
		return nil
	}
	var e struct {
		Error     FlexString      `json:"error"`
		ErrorText string          `json:"errorText"`
		Code      json.RawMessage `json:"code"`
		ErrNo     json.RawMessage `json:"errNo"`
		Games     json.RawMessage `json:"games"`
		Messages  json.RawMessage `json:"messages"`
		Log       json.RawMessage `json:"log"`
		Level     json.RawMessage `json:"level"`
	}
	if err := decodeEngineJSON(body, &e); err != nil {
		return nil
	}
	if e.Error.String() == "" || len(e.Code) > 0 || len(e.ErrNo) > 0 ||
		len(e.Games) > 0 || len(e.Messages) > 0 || len(e.Log) > 0 || len(e.Level) > 0 {
		return nil
	}
	// The go_nogame / go_reserv / go_wrongteam / go_lite_noauth templates put
	// a number in "error" and the wording in "errorText". Without this the
	// decode above failed, the envelope went unrecognized and the caller
	// reported an empty game as a success.
	if e.ErrorText != "" {
		state, _ := strconv.Atoi(e.Error.String())
		if state == GameStateNotAuthorized {
			return &AuthError{Kind: AuthSession, Message: e.ErrorText}
		}
		return &GameAccessError{State: state, Text: e.ErrorText}
	}
	if strings.Contains(e.Error.String(), "авторизации") {
		return &AuthError{Kind: AuthSession, Message: e.Error.String()}
	}
	return &EngineError{Code: 0, Text: e.Error.String()}
}

type stringError string

func (e stringError) Error() string { return string(e) }

const (
	errHTMLBody = stringError("HTML page instead of JSON")
	// The engine answers some reads with 200 and nothing at all; live
	// API/game.php?level=1 did so mid-game.
	errEmptyBody  = stringError("engine sent an empty body")
	errNoLocation = stringError("redirect without Location header")
)
