package dzzzr

import (
	"context"
	"fmt"
	"strconv"
)

// AdminReplaceGame replaces all modeled settings, including explicit zeroes.
// Unmodeled fields from the destination form are retained.
func (c *Client) AdminReplaceGame(ctx context.Context, gameID int, p GameParams) error {
	if gameID <= 0 {
		return fmt.Errorf("dzzzr: game ID must be positive")
	}
	info, err := c.AdminGetGame(ctx, gameID)
	if err != nil {
		return err
	}
	if info.ID != gameID {
		return fmt.Errorf("dzzzr: returned a different game")
	}
	form := info.Fields.Clone()
	for _, field := range gameTextFields {
		form.Set(field, "")
	}
	for _, field := range []string{"league", "clueMin", "clueMin2", "clueMin3", "masterCodeShtraf", "clueBeforeShtraf", "penalty", "duration", "bonusAfter"} {
		form.Set(field, "0")
	}
	for _, field := range []string{"otherLeague", "publish", "finished", "invitation"} {
		form.Del(field)
	}
	applyGameParams(form, p)
	form.Set("action", "update_game")
	form.Set("id", strconv.Itoa(gameID))
	_, _, err = c.adminPost(ctx, adminPath, form)
	return err
}
