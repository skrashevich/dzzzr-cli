package dzzrmobile

// The organizer surface is exposed too, so a staff app can watch a game from
// a phone. Every method returns JSON and needs organizer credentials; without
// them the engine answers 401 and the call fails with an admin auth error.

// AdminListGames returns the organizer's games as a JSON array.
func (c *DzzrClient) AdminListGames() (string, error) {
	games, err := c.client.AdminListGames(c.ctx())
	if err != nil {
		return "", err
	}
	return marshalJSON(games)
}

// AdminListLevels returns a game's levels as a JSON array.
func (c *DzzrClient) AdminListLevels(gameID int64) (string, error) {
	levels, err := c.client.AdminListLevels(c.ctx(), int(gameID))
	if err != nil {
		return "", err
	}
	return marshalJSON(levels)
}

// AdminListTeams returns a game's teams as a JSON array.
func (c *DzzrClient) AdminListTeams(gameID int64) (string, error) {
	teams, err := c.client.AdminListTeams(c.ctx(), int(gameID))
	if err != nil {
		return "", err
	}
	return marshalJSON(teams)
}

// AdminMonitor returns the live game screen state as JSON.
func (c *DzzrClient) AdminMonitor(gameID int64) (string, error) {
	st, err := c.client.AdminMonitor(c.ctx(), int(gameID))
	if err != nil {
		return "", err
	}
	return marshalJSON(st)
}

// AdminGetLog polls the organizer log. since is the timestamp from a
// previous call; the reply is {"entries":[...],"next_since":"..."}.
func (c *DzzrClient) AdminGetLog(gameID int64, since string) (string, error) {
	entries, next, err := c.client.AdminGetLog(c.ctx(), int(gameID), since)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"entries": entries, "next_since": next})
}

// AdminListMessages returns the organizer chat of a game; teamID 0 means all.
func (c *DzzrClient) AdminListMessages(gameID, teamID int64) (string, error) {
	msgs, err := c.client.AdminListMessages(c.ctx(), int(gameID), int(teamID))
	if err != nil {
		return "", err
	}
	return marshalJSON(msgs)
}

// AdminSendMessage posts a message to one team (teamID) or to everyone (0).
func (c *DzzrClient) AdminSendMessage(gameID, teamID int64, text, showAfter string, important bool) error {
	return c.client.AdminSendMessage(c.ctx(), int(gameID), int(teamID), text, showAfter, important)
}

// AdminGiveLevel issues a level to a team; at is an optional
// "YYYY-MM-DD HH:MM:SS".
func (c *DzzrClient) AdminGiveLevel(gameID, teamID, level int64, at string) error {
	return c.client.AdminGiveLevel(c.ctx(), int(gameID), int(teamID), int(level), at)
}

// AdminAcceptCode records a code for a team as if it had entered it.
func (c *DzzrClient) AdminAcceptCode(gameID, teamID, level int64, code, at string) error {
	return c.client.AdminAcceptCode(c.ctx(), int(gameID), int(teamID), int(level), code, at)
}

// AdminAddCorrection grants bonus ("bonus") or penalty ("penalty") minutes.
func (c *DzzrClient) AdminAddCorrection(gameID, teamID int64, kind string, minutes int64, comment string) error {
	return c.client.AdminAddCorrection(c.ctx(), int(gameID), int(teamID), kind, int(minutes), comment)
}

// AdminSetTeamBlocked stops or resumes a team's game.
func (c *DzzrClient) AdminSetTeamBlocked(gameID, teamID int64, blocked bool) error {
	if blocked {
		return c.client.AdminBlockTeam(c.ctx(), int(gameID), int(teamID))
	}
	return c.client.AdminUnblockTeam(c.ctx(), int(gameID), int(teamID))
}

// AdminSetEngine stops or starts the engine for every team.
func (c *DzzrClient) AdminSetEngine(gameID int64, running bool) error {
	return c.client.AdminSetEngine(c.ctx(), int(gameID), running)
}
