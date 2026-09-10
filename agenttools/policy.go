package agenttools

import (
	"context"
	"fmt"
)

// Policy decides what an agent is allowed to do with the engine.
type Policy string

const (
	// PolicyReadonly hides mutating tools from the catalog and refuses them
	// if they are called anyway.
	PolicyReadonly Policy = "readonly"
	// PolicyApprove exposes mutating tools but routes every call through a
	// Confirmer first. This is the default.
	PolicyApprove Policy = "approve"
	// PolicyFull lets the agent change game state without asking.
	PolicyFull Policy = "full"
)

// DefaultPolicy applies when a caller leaves Options.Policy empty.
const DefaultPolicy = PolicyApprove

// ParsePolicy converts a textual policy name; the empty string is the default.
func ParsePolicy(s string) (Policy, error) {
	switch Policy(s) {
	case "":
		return DefaultPolicy, nil
	case PolicyReadonly, PolicyApprove, PolicyFull:
		return Policy(s), nil
	default:
		return "", fmt.Errorf("agenttools: unknown policy %q (want readonly, approve or full)", s)
	}
}

func (p Policy) normalized() Policy {
	if p == "" {
		return DefaultPolicy
	}
	return p
}

// ConfirmRequest describes a mutating call awaiting the user's decision.
type ConfirmRequest struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args,omitempty"`
}

// Confirmer approves or declines a mutating tool call. Implementations block
// until the user answers or ctx is canceled.
type Confirmer interface {
	ConfirmToolCall(ctx context.Context, req ConfirmRequest) (bool, error)
}

// ConfirmerFunc adapts a function to Confirmer.
type ConfirmerFunc func(ctx context.Context, req ConfirmRequest) (bool, error)

// ConfirmToolCall implements Confirmer.
func (f ConfirmerFunc) ConfirmToolCall(ctx context.Context, req ConfirmRequest) (bool, error) {
	return f(ctx, req)
}

// confirmerKey carries a per-call Confirmer in the context.
type confirmerKey struct{}

// WithConfirmer overrides the catalog's Confirmer for the calls made with
// this context. A surface whose approval is decided elsewhere — the MCP
// server, where the answer arrives with the retried request — installs the
// decision here instead of asking again mid-call.
func WithConfirmer(ctx context.Context, c Confirmer) context.Context {
	if c == nil {
		return ctx
	}
	return context.WithValue(ctx, confirmerKey{}, c)
}

func confirmerFrom(ctx context.Context) Confirmer {
	c, _ := ctx.Value(confirmerKey{}).(Confirmer)
	return c
}

// gate applies the policy to one call.
type gate struct {
	policy    Policy
	confirmer Confirmer
}

// authorize reports whether a mutating call may proceed. The refusal text is
// written for the model, not for a human: it says what to do instead.
func (g *gate) authorize(ctx context.Context, tool string, args map[string]any) (bool, string) {
	switch g.policy.normalized() {
	case PolicyFull:
		return true, ""
	case PolicyReadonly:
		return false, fmt.Sprintf("%s is not available: the session is read-only. Describe the action for the user instead of performing it.", tool)
	}
	confirmer := confirmerFrom(ctx)
	if confirmer == nil {
		confirmer = g.confirmer
	}
	if confirmer == nil {
		return false, fmt.Sprintf("%s cannot be authorized: no confirmation channel is available.", tool)
	}
	ok, err := confirmer.ConfirmToolCall(ctx, ConfirmRequest{Tool: tool, Args: args})
	if err != nil {
		return false, fmt.Sprintf("%s was not authorized: %v. Do not retry it; ask what to do instead.", tool, err)
	}
	if !ok {
		return false, fmt.Sprintf("The user declined %s. Do not retry it; ask what to do instead.", tool)
	}
	return true, ""
}
