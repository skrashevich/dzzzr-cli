package dzzzr

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// Organizer messages live at admin/gmcht.php: a list of everything sent in
// a game plus a form to post a new one. Messages with a negative team id are
// what a team sent to the organizer; the rest are the organizer's.

const chatPath = adminPath + "gmcht.php"

// AdminMessage is one message of a game's organizer chat.
//
// The chat lists five columns: time, addressee, text, the moment the message
// becomes visible, and the delete button. The "important" flag is stored but
// never rendered there, so it can only be set, not read back.
type AdminMessage struct {
	Time      string `json:"time"` // HH:MM:SS as the engine prints it
	Addressee string `json:"addressee"`
	Content   string `json:"content"`
	FromTeam  bool   `json:"from_team"`
	// ShowAfter is when the message becomes visible to the team, empty when
	// it already is.
	ShowAfter string `json:"show_after,omitempty"`
	// Timestamp identifies the message for deletion; the engine keys on it.
	Timestamp string `json:"timestamp"`
}

// AdminListMessages returns the organizer chat of a game, newest first.
// teamID filters to one team (0 = all).
func (c *Client) AdminListMessages(ctx context.Context, gameID, teamID int) ([]AdminMessage, error) {
	q := url.Values{"gm": {strconv.Itoa(gameID)}, "Project": {""}}
	if teamID != 0 {
		q.Set("team", strconv.Itoa(teamID))
	}
	page, err := c.adminGet(ctx, chatPath, q)
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(page)
	if err != nil {
		return nil, &UndecodableResponseError{StatusCode: 200, Context: "admin messages", Err: err}
	}
	var out []AdminMessage
	for _, row := range doc.rows() {
		if len(row.Cells) < 3 {
			continue
		}
		if row.Hidden.Get("action") != "delMessage" {
			continue
		}
		ts := row.Hidden.Get("tm")
		if ts == "" {
			continue
		}
		m := AdminMessage{
			Time:      strings.TrimSpace(row.Cells[0]),
			Addressee: strings.TrimSpace(row.Cells[1]),
			Content:   strings.TrimSpace(row.Cells[2]),
			Timestamp: ts,
			// Team messages are drawn on a blue background, organizer ones on red.
			FromTeam: strings.Contains(strings.ToLower(row.Attrs["style"]), "#99cccc"),
		}
		if len(row.Cells) > 3 {
			m.ShowAfter = strings.TrimSpace(row.Cells[3])
		}
		out = append(out, m)
	}
	return out, nil
}

// AdminSendMessage posts a message to one team (teamID) or to everyone
// (teamID 0). showAfter delays it until a "YYYY-MM-DD HH:MM:SS" moment;
// empty shows it at once.
func (c *Client) AdminSendMessage(ctx context.Context, gameID, teamID int, text, showAfter string, important bool) error {
	form := url.Values{
		"action": {"addMessage"}, "Project": {""},
		"gm": {strconv.Itoa(gameID)}, "team": {strconv.Itoa(teamID)},
		"content": {text}, "showafter": {showAfter},
	}
	if important {
		form.Set("important", "on")
	}
	_, _, err := c.adminPost(ctx, chatPath, form)
	return err
}

// AdminDeleteMessage removes one message, identified by the timestamp from
// AdminListMessages.
func (c *Client) AdminDeleteMessage(ctx context.Context, gameID int, timestamp string) error {
	form := url.Values{"action": {"delMessage"}, "Project": {""}, "gm": {strconv.Itoa(gameID)}, "tm": {timestamp}}
	_, _, err := c.adminPost(ctx, chatPath, form)
	return err
}

// AdminDeleteAllMessages clears a game's organizer chat.
func (c *Client) AdminDeleteAllMessages(ctx context.Context, gameID int) error {
	form := url.Values{"action": {"delAllMessages"}, "Project": {""}, "gm": {strconv.Itoa(gameID)}}
	_, _, err := c.adminPost(ctx, chatPath, form)
	return err
}
