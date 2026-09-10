package dzzzr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// gameAPI performs a GET against API/game.php with the given switch
// (stat=true, log=true, level=1, bonuslevel=1) and decodes the JSON reply.
func (c *Client) gameAPI(ctx context.Context, ctxName string, q url.Values, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, "API/game.php", q, nil, requestOptions{basic: true, session: true, requireAuth: true})
	if err != nil {
		return err
	}
	status, _, body, err := c.do(req)
	if err != nil {
		return err
	}
	if err := classifyStatus(status, body, ctxName); err != nil {
		return err
	}
	body = trimEngineNoise(body)
	if isEmptyBody(body) {
		return &UndecodableResponseError{StatusCode: status, Context: ctxName, Err: errEmptyBody}
	}
	if looksLikeHTML(body) {
		return &UndecodableResponseError{StatusCode: status, Context: ctxName, Err: errHTMLBody, Body: truncate(body)}
	}
	if err := engineEnvelope(body); err != nil {
		return err
	}
	if err := bareErrEnvelope(body); err != nil {
		return err
	}
	if err := decodeEngineJSON(body, out); err != nil {
		return &UndecodableResponseError{StatusCode: status, Context: ctxName, Err: err, Body: truncate(body)}
	}
	return nil
}

// GetStat returns the team's per-level statistics (API/game.php?stat=true).
func (c *Client) GetStat(ctx context.Context) (*TeamStat, error) {
	var out TeamStat
	if err := c.gameAPI(ctx, "team stat", url.Values{"stat": {"true"}}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetLog returns the team's game log (API/game.php?log=true): levels
// issued, codes accepted and rejected. Entries keep the engine's HTML
// wrapping; use LogEntry.Clean for text.
func (c *Client) GetLog(ctx context.Context) ([]LogEntry, error) {
	var out struct {
		Log []LogEntry `json:"log"`
	}
	if err := c.gameAPI(ctx, "team log", url.Values{"log": {"true"}}, &out); err != nil {
		return nil, err
	}
	return out.Log, nil
}

// GetLevelInfo returns the bot-oriented view of the current main level
// (API/game.php?level=1): per-code difficulty and found flags, sectors,
// hints, timer and which controls are available.
func (c *Client) GetLevelInfo(ctx context.Context) (*LevelInfo, error) {
	var out LevelInfo
	if err := c.gameAPI(ctx, "level info", url.Values{"level": {"1"}}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetBonusLevelInfo returns the same view for the bonus (skvoz) levels; the
// Skvoz field carries them.
func (c *Client) GetBonusLevelInfo(ctx context.Context) (*LevelInfo, error) {
	var out LevelInfo
	if err := c.gameAPI(ctx, "bonus level info", url.Values{"bonuslevel": {"1"}}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMessages returns organizer and team chat messages (API/messages.php),
// oldest first. after limits the reply to messages newer than the given
// "YYYY-MM-DD HH:MM:SS"; empty returns everything.
func (c *Client) GetMessages(ctx context.Context, after string) ([]ChatMessage, error) {
	q := url.Values{}
	if after != "" {
		q.Set("after", after)
	}
	req, err := c.newRequest(ctx, http.MethodGet, "API/messages.php", q, nil, requestOptions{basic: true, session: true, requireAuth: true})
	if err != nil {
		return nil, err
	}
	status, _, body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if err := classifyStatus(status, body, "messages"); err != nil {
		return nil, err
	}
	body = trimEngineNoise(body)
	if isEmptyBody(body) {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "messages", Err: errEmptyBody}
	}
	if looksLikeHTML(body) {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "messages", Err: errHTMLBody, Body: truncate(body)}
	}
	if err := engineEnvelope(body); err != nil {
		return nil, err
	}
	var out struct {
		Messages []ChatMessage `json:"messages"`
	}
	if err := decodeEngineJSON(body, &out); err != nil {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "messages", Err: err, Body: truncate(body)}
	}
	return out.Messages, nil
}

// PostMessage posts a message to the team chat (API/postmessage.php),
// attributed to the captain login the client is configured with.
//
// Dozor Classic answers this endpoint with {"message":"Ok"} and delivers
// nothing: the text reaches neither API/messages.php nor the game state. Use
// SendMessageToOrg, which posts action=send_message the way the game page's
// own chat form does and does arrive.
func (c *Client) PostMessage(ctx context.Context, text string) error {
	c.mu.RLock()
	login := c.login
	if login == "" {
		login = c.creds.Captain
	}
	c.mu.RUnlock()
	form := url.Values{"content": {text}, "login": {login}}
	req, err := c.newRequest(ctx, http.MethodPost, "API/postmessage.php", nil, form, requestOptions{basic: true, session: true, requireAuth: true})
	if err != nil {
		return err
	}
	status, _, body, err := c.do(req)
	if err != nil {
		return err
	}
	if err := classifyStatus(status, body, "post message"); err != nil {
		return err
	}
	body = trimEngineNoise(body)
	if isEmptyBody(body) {
		return &UndecodableResponseError{StatusCode: status, Context: "post message", Err: errEmptyBody}
	}
	if looksLikeHTML(body) {
		return &UndecodableResponseError{StatusCode: status, Context: "post message", Err: errHTMLBody, Body: truncate(body)}
	}
	if err := engineEnvelope(body); err != nil {
		return err
	}
	var out struct {
		Message string `json:"message"`
	}
	if err := decodeEngineJSON(body, &out); err != nil {
		return &UndecodableResponseError{StatusCode: status, Context: "post message", Err: err, Body: truncate(body)}
	}
	if !strings.EqualFold(out.Message, "Ok") {
		return fmt.Errorf("dzzzr: post message: engine refused the message (%q); check that the team has an accepted application for a running game", out.Message)
	}
	return nil
}

// GetGamesList lists the city's announced games (API/gamesList.php).
//
// The handler includes APIconst.php like the rest of the API, so it needs the
// site session and the captain/PIN pair even though it only returns public
// announcements. Measured against classic.dzzzr.ru on 2026-09-08: without
// them the engine answers 200 with the session-error envelope.
func (c *Client) GetGamesList(ctx context.Context, opts GamesListOptions) ([]GameInfo, error) {
	// The handler pages with limit and offset and defaults to 50, so a city
	// with a long archive is silently truncated unless the caller pages. When
	// neither is given, walk the pages until one comes back short.
	if opts.Limit == 0 && opts.Offset == 0 {
		return c.allGames(ctx, opts)
	}
	return c.gamesPage(ctx, opts)
}

// allGames reads every page of the list.
func (c *Client) allGames(ctx context.Context, opts GamesListOptions) ([]GameInfo, error) {
	const pageSize = 50
	page := opts
	page.Limit = pageSize
	var out []GameInfo
	for page.Offset = 0; ; page.Offset += pageSize {
		games, err := c.gamesPage(ctx, page)
		if err != nil {
			return nil, err
		}
		out = append(out, games...)
		if len(games) < pageSize {
			return out, nil
		}
		// A city cannot plausibly have this many games; stop rather than
		// loop forever if the engine ignores the offset.
		if len(out) > 10000 {
			return out, nil
		}
	}
}

func (c *Client) gamesPage(ctx context.Context, opts GamesListOptions) ([]GameInfo, error) {
	q := url.Values{"cityID": {c.city}}
	if opts.NewOnly {
		q.Set("new", "1")
	}
	if opts.Archive {
		q.Set("archive", "1")
	}
	if opts.After != "" {
		q.Set("after", opts.After)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		q.Set("offset", strconv.Itoa(opts.Offset))
	}
	req, err := c.newRequest(ctx, http.MethodGet, "API/gamesList.php", q, nil, requestOptions{basic: true, session: true, requireAuth: true})
	if err != nil {
		return nil, err
	}
	status, _, body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if err := classifyStatus(status, body, "games list"); err != nil {
		return nil, err
	}
	body = trimEngineNoise(body)
	if isEmptyBody(body) {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "games list", Err: errEmptyBody}
	}
	if looksLikeHTML(body) {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "games list", Err: errHTMLBody, Body: truncate(body)}
	}
	if err := engineEnvelope(body); err != nil {
		return nil, err
	}
	var out struct {
		Games []GameInfo `json:"games"`
	}
	if err := decodeEngineJSON(body, &out); err != nil {
		return nil, &UndecodableResponseError{StatusCode: status, Context: "games list", Err: err, Body: truncate(body)}
	}
	return out.Games, nil
}
