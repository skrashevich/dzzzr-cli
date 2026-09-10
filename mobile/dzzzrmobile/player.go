package dzzzrmobile

import (
	"errors"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// SetCredentials installs the team's captain login and game PIN. They are
// required for every game request.
func (c *DzzzrClient) SetCredentials(captain, pin string) {
	c.client.SetCredentials(dzzzr.Credentials{Captain: captain, Pin: pin})
}

// Captain returns the captain login the client uses.
func (c *DzzzrClient) Captain() string { return c.client.Credentials().Captain }

// Login signs in with a site account and stores the session. Returns the
// LoginResponse as JSON; check its "code" field (2 means success) and pass it
// to LoginCodeText for the engine's own wording.
func (c *DzzzrClient) Login(login, password string) (string, error) {
	resp, err := c.client.Login(c.ctx(), login, password)
	if err != nil {
		return "", err
	}
	return marshalJSON(resp)
}

// Logout forgets the session token; credentials stay.
func (c *DzzzrClient) Logout() { c.client.Logout() }

// Session returns the current session token, empty when signed out.
func (c *DzzzrClient) Session() string { return c.client.Session() }

// SetSession installs a session token obtained elsewhere.
func (c *DzzzrClient) SetSession(token string) { c.client.SetSession(token) }

// GetGame returns the current game state as GameState JSON.
func (c *DzzzrClient) GetGame() (string, error) {
	st, err := c.client.GetGame(c.ctx())
	if err != nil {
		return "", err
	}
	return marshalJSON(st)
}

// GetLevelInfo returns the current level's per-code detail as LevelInfo JSON.
func (c *DzzzrClient) GetLevelInfo() (string, error) {
	info, err := c.client.GetLevelInfo(c.ctx())
	if err != nil {
		return "", err
	}
	return marshalJSON(info)
}

// GetBonusLevelInfo returns the bonus (skvoz) levels as LevelInfo JSON.
func (c *DzzzrClient) GetBonusLevelInfo() (string, error) {
	info, err := c.client.GetBonusLevelInfo(c.ctx())
	if err != nil {
		return "", err
	}
	return marshalJSON(info)
}

// GetStat returns the team's per-level statistics as TeamStat JSON.
func (c *DzzzrClient) GetStat() (string, error) {
	st, err := c.client.GetStat(c.ctx())
	if err != nil {
		return "", err
	}
	return marshalJSON(st)
}

// GetLog returns the team's game log as a JSON array.
func (c *DzzzrClient) GetLog() (string, error) {
	log, err := c.client.GetLog(c.ctx())
	if err != nil {
		return "", err
	}
	return marshalJSON(log)
}

// GetMessages returns the chat as a JSON array. after limits the reply to
// messages newer than "YYYY-MM-DD HH:MM:SS"; empty returns everything.
func (c *DzzzrClient) GetMessages(after string) (string, error) {
	msgs, err := c.client.GetMessages(c.ctx(), after)
	if err != nil {
		return "", err
	}
	return marshalJSON(msgs)
}

// PostMessage writes to API/postmessage.php. Dozor Classic acknowledges that
// endpoint and delivers nothing; use SendMessageToOrg to reach the organizer.
func (c *DzzzrClient) PostMessage(text string) error {
	return c.client.PostMessage(c.ctx(), text)
}

// GetGamesList returns the city's games as a JSON array. It needs no
// credentials.
func (c *DzzzrClient) GetGamesList(newOnly, archive bool) (string, error) {
	games, err := c.client.GetGamesList(c.ctx(), dzzzr.GamesListOptions{NewOnly: newOnly, Archive: archive})
	if err != nil {
		return "", err
	}
	return marshalJSON(games)
}

// SendCode submits a code for the current level and returns ActionResult
// JSON. It uses the short code-send timeout.
func (c *DzzzrClient) SendCode(code string) (string, error) {
	ctx, cancel := c.codeCtx()
	defer cancel()
	r, err := c.client.SendCode(ctx, code)
	if err != nil {
		return "", err
	}
	return marshalJSON(r)
}

// SendBonusCode submits a code for a bonus (skvoz) level.
func (c *DzzzrClient) SendBonusCode(levelNumber int64, code string) (string, error) {
	ctx, cancel := c.codeCtx()
	defer cancel()
	r, err := c.client.SendBonusCode(ctx, int(levelNumber), code)
	if err != nil {
		return "", err
	}
	return marshalJSON(r)
}

// SendSpoilerCode submits the code that unlocks a spoiler of a level.
func (c *DzzzrClient) SendSpoilerCode(level int64, code string) (string, error) {
	ctx, cancel := c.codeCtx()
	defer cancel()
	r, err := c.client.SendSpoilerCode(ctx, int(level), code)
	if err != nil {
		return "", err
	}
	return marshalJSON(r)
}

// TakeHint requests hint 1 or 2 of a level that issues hints on demand.
func (c *DzzzrClient) TakeHint(level, hint int64) (string, error) {
	r, err := c.client.TakeHint(c.ctx(), int(level), int(hint))
	if err != nil {
		return "", err
	}
	return marshalJSON(r)
}

// TakeHintEarly takes hint 1 or 2 of a level before its time, for a penalty.
// The engine picks a hint of its own when it is given neither number, so both
// are required here.
func (c *DzzzrClient) TakeHintEarly(level, hint int64) (string, error) {
	r, err := c.client.TakeHintEarly(c.ctx(), int(level), int(hint))
	if err != nil {
		return "", err
	}
	return marshalJSON(r)
}

// Abandon gives up the current level and asks for the next one.
func (c *DzzzrClient) Abandon() (string, error) {
	r, err := c.client.Abandon(c.ctx())
	if err != nil {
		return "", err
	}
	return marshalJSON(r)
}

// TakeBreak schedules a 15-minute break after the current level, or cancels
// one already scheduled.
func (c *DzzzrClient) TakeBreak() (string, error) {
	r, err := c.client.TakeBreak(c.ctx())
	if err != nil {
		return "", err
	}
	return marshalJSON(r)
}

// StopBreak ends a running break early. It takes no level: the engine issues
// none while the team is paused.
func (c *DzzzrClient) StopBreak() (string, error) {
	r, err := c.client.StopBreak(c.ctx())
	if err != nil {
		return "", err
	}
	return marshalJSON(r)
}

// NextLevel moves on without collecting the level's bonus codes.
func (c *DzzzrClient) NextLevel(level int64) (string, error) {
	r, err := c.client.NextLevel(c.ctx(), int(level))
	if err != nil {
		return "", err
	}
	return marshalJSON(r)
}

// SelectLevel picks the next level in games with a free order.
func (c *DzzzrClient) SelectLevel(current, next int64) (string, error) {
	r, err := c.client.SelectLevel(c.ctx(), int(current), int(next))
	if err != nil {
		return "", err
	}
	return marshalJSON(r)
}

// SendMessageToOrg posts a message through the game interface. Captain,
// chief of staff or an authorized helper only.
func (c *DzzzrClient) SendMessageToOrg(text string) (string, error) {
	r, err := c.client.SendMessageToOrg(c.ctx(), text)
	if err != nil {
		return "", err
	}
	return marshalJSON(r)
}

// GetLevelMedia returns the current level's pictures and links as a JSON
// array of MediaRef.
//
// A task routinely carries the code inside a picture, so a client that shows
// only the text shows a task it cannot solve. Remote addresses come back
// absolute. A picture the organizer embedded rather than linked has
// "inline": true and keeps its data: URI in "url", which an image view
// decodes as it stands; "media_type" and "bytes" describe it.
func (c *DzzzrClient) GetLevelMedia() (string, error) {
	refs, err := c.client.CurrentLevelMedia(c.ctx())
	if err != nil {
		return "", err
	}
	return marshalJSON(refs)
}

// GetGameView returns a game or its access explanation as JSON.
// Classify access errors before crossing gomobile, which loses Go error types.
func (c *DzzzrClient) GetGameView() (string, error) {
	st, err := c.client.GetGame(c.ctx())
	var view struct {
		Game       *dzzzr.GameState `json:"game"`
		AccessText *string          `json:"accessText"`
	}
	if access, ok := errors.AsType[*dzzzr.GameAccessError](err); ok {
		text := dzzzr.StripHTML(access.Text)
		view.AccessText = &text
	} else if err != nil {
		return "", err
	} else {
		view.Game = st
	}
	return marshalJSON(view)
}
