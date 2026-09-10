package dzzzr

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// AdminReplaceLevel replaces every modeled field, including empty strings,
// zero numbers and empty lists. Unlike AdminUpdateLevel this is a full snapshot,
// intended for importing a prepared source. Unmodeled engine fields are retained.
// Nil checkboxes mean off, nil penalty means the game's default.
func (c *Client) AdminReplaceLevel(ctx context.Context, gameID, levelID int, p LevelParams) error {
	if gameID <= 0 || levelID <= 0 {
		return fmt.Errorf("dzzzr: game and level IDs must be positive")
	}
	info, err := c.AdminGetLevel(ctx, gameID, levelID)
	if err != nil {
		return err
	}
	if info.ID != levelID || info.GameID != gameID {
		return fmt.Errorf("dzzzr: returned level does not belong to requested game or ID")
	}
	form := info.Fields.Clone()
	resetLevelFields(form)
	applyLevelParams(form, p)
	// The patch API defaults an empty bonus difficulty to 1. A snapshot must
	// retain the exact value, including the engine's empty selection.
	for i, code := range p.BonusCodes {
		form.Set("dangerB["+strconv.Itoa(i)+"]", code.Danger)
	}
	form.Set("action", "update_zadanie")
	form.Set("id", strconv.Itoa(levelID))
	form.Set("category", strconv.Itoa(gameID))
	form.Set("categoryValue", strconv.Itoa(gameID))
	_, _, err = c.adminPost(ctx, adminPath, form)
	return err
}

func resetLevelFields(form url.Values) {
	for _, key := range []string{"title", "subtitle", "question", "clue1", "clue2", "location", "locationComment", "comment", "skvozStart", "lat", "lon", "radius", "greeting", "penalty"} {
		form.Set(key, "")
	}
	for _, key := range []string{"ClueMin", "ClueMin2", "ClueMin3", "interval1", "shtraf1", "interval2", "shtraf2", "codeCount", "tryLimit", "bonusTime", "skvozMin", "timeAddBonusAll"} {
		form.Set(key, "0")
	}
	for _, key := range []string{"zapros1", "zapros2", "noMasterCode", "isSabotage", "bonus", "skvoz", "showCoordWith2ndClue", "nobreak", "clueBefore", "zapas", "publish"} {
		form.Del(key)
	}
	deleteIndexed(form, "code", "codeS", "danger", "sector", "secName", "codeB", "codeBS", "dangerB", "timeB", "codeF", "codeFS", "fakeShtraf", "spoiler", "spoilerCode", "spoilerSynonyms", "spoilerPenalty")
}

// TechnicalLevelParams returns DzrSourceHelper's optional where.games technical
// level. Its question loads the same external integration scripts as the helper.
// Nothing adds this level automatically.
func TechnicalLevelParams() LevelParams {
	return LevelParams{
		Title: "Технический уровень",
		Question: `<p>Это техническая заглушка</p>
<script src="../../uploaded/moscow/Night/jquery-1.11.2.min.txt"></script>
<script src="../../uploaded/moscow/Night/jquery.cookie.txt"></script>
<script type="text/javascript">// <![CDATA[
$(document).ready(function() {
  $.ajax({ url: "https://alt.where.games/initn.js?v=1", dataType: "script", cache: true, });
});
// ]]></script>`,
		Hint1: "-", Hint2: "-", Comment: new("-"),
		Codes: []LevelCode{{Code: "456457643867837433636", Danger: "null"}},
		Bonus: new(true), Skvoz: new(true),
	}
}

// AdminCreateTechnicalLevel adds the optional integration level on explicit request.
func (c *Client) AdminCreateTechnicalLevel(ctx context.Context, gameID int) (int, error) {
	if gameID <= 0 {
		return 0, fmt.Errorf("dzzzr: game ID must be positive")
	}
	return c.AdminCreateLevel(ctx, gameID, TechnicalLevelParams())
}
