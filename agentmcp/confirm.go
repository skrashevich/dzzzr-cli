package agentmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// confirmRequestID names the single input request a mutating call makes.
const confirmRequestID = "confirm"

// A mutating call must be confirmed by the user. Since protocol version
// 2026-07-28 a server may not elicit while it is serving a request; instead
// it answers with an InputRequests map and the client fulfills it and retries
// the call (SEP-2322). The go-sdk does that round trip for both new and old
// clients, so the handler only has to answer "input required" once and then
// read the answer out of the retried request.
//
// answerFromRequest reports what the user replied to a retried call:
// (answered, confirmed).
//
// The answer is bound to the arguments it was given for. The user is shown
// one call; the retry that carries their answer is a separate request, and
// nothing in the protocol stops a client from retrying with different
// arguments. The elicitation asks for the fingerprint back, so a widened
// call arrives without a matching answer and is refused.
func answerFromRequest(req *mcp.CallToolRequest, args map[string]any) (bool, bool) {
	if req == nil || req.Params == nil || len(req.Params.InputResponses) == 0 {
		return false, false
	}
	resp, ok := req.Params.InputResponses[confirmRequestID]
	if !ok {
		return false, false
	}
	elicited, ok := resp.(*mcp.ElicitResult)
	if !ok {
		return true, false
	}
	if elicited.Action != "accept" {
		return true, false
	}
	confirmed, _ := elicited.Content["confirm"].(bool)
	if !confirmed {
		return true, false
	}
	answered, _ := elicited.Content["call"].(string)
	return true, answered == argsFingerprint(args)
}

// argsFingerprint identifies one call's arguments. Keys are sorted so the
// same call always fingerprints the same way.
func argsFingerprint(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	h := sha256.New()
	for _, k := range keys {
		encoded, err := json.Marshal(args[k])
		if err != nil {
			continue
		}
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write(encoded)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// confirmationRequest builds the input request that asks the user to
// authorize one mutating tool call.
func confirmationRequest(tool string, args map[string]any) mcp.InputRequestMap {
	fingerprint := argsFingerprint(args)
	return mcp.InputRequestMap{
		confirmRequestID: &mcp.ElicitParams{
			Message: confirmationMessage(agenttools.ConfirmRequest{Tool: tool, Args: args}),
			RequestedSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"confirm": map[string]any{
						"type":        "boolean",
						"description": "Set to true to let the call reach the Dozor engine.",
					},
					"call": map[string]any{
						"type":        "string",
						"description": "Echo this value back unchanged: it identifies the call being confirmed.",
						"const":       fingerprint,
						"default":     fingerprint,
					},
				},
				"required": []string{"confirm"},
			},
		},
	}
}

func confirmationMessage(req agenttools.ConfirmRequest) string {
	var b strings.Builder
	b.WriteString("The agent wants to change the Dozor game state by calling ")
	b.WriteString(req.Tool)
	if len(req.Args) > 0 {
		if encoded, err := json.Marshal(req.Args); err == nil {
			b.WriteString(" with ")
			b.Write(encoded)
		}
	}
	b.WriteString(". Confirm?")
	return b.String()
}

// decidedConfirmer answers with a decision that was already made, so the
// catalog's own gate can stay the single place that enforces the policy.
type decidedConfirmer struct{ allow bool }

func (d decidedConfirmer) ConfirmToolCall(context.Context, agenttools.ConfirmRequest) (bool, error) {
	if d.allow {
		return true, nil
	}
	return false, nil
}

// NewElicitConfirmer returns a Confirmer for callers that drive the catalog
// outside a tool handler (tests, embedders). Inside the MCP server the
// confirmation travels through InputRequests instead, so this confirmer
// refuses rather than eliciting mid-request.
func NewElicitConfirmer() agenttools.Confirmer { return elicitConfirmer{} }

type elicitConfirmer struct{}

// ConfirmToolCall implements agenttools.Confirmer.
func (elicitConfirmer) ConfirmToolCall(context.Context, agenttools.ConfirmRequest) (bool, error) {
	return false, errors.New("confirmation must travel through the MCP input-request round trip")
}
