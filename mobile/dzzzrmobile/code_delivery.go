package dzzzrmobile

import (
	"errors"
	"net"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// SendCodeView preserves delivery certainty across gomobile's NSError boundary.
// kind is main, bonus or spoiler. Only a failed dial proves that no HTTP
// request reached the engine. HTTP errors, EOF and read timeouts are ambiguous.
func (c *DzzzrClient) SendCodeView(code string, level int64, kind string) (string, error) {
	ctx, cancel := c.codeCtx()
	defer cancel()
	var result *dzzzr.ActionResult
	var err error
	switch kind {
	case "main":
		result, err = c.client.SendCode(ctx, code)
	case "bonus":
		result, err = c.client.SendBonusCode(ctx, int(level), code)
	case "spoiler":
		result, err = c.client.SendSpoilerCode(ctx, int(level), code)
	default:
		return "", errors.New("invalid code kind")
	}
	view := struct {
		Result  *dzzzr.ActionResult `json:"result"`
		Error   string              `json:"error"`
		NotSent bool                `json:"notSent"`
	}{Result: result}
	if err != nil {
		view.Error = err.Error()
		if dial, ok := errors.AsType[*net.OpError](err); ok && dial.Op == "dial" {
			view.NotSent = true
		}
	}
	return marshalJSON(view)
}
