package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// approvalAction is the answer the browser sends back about a mutating call.
type approvalAction string

const (
	approvalYes  approvalAction = "yes"
	approvalNo   approvalAction = "no"
	approvalQuit approvalAction = "quit"
)

// approvalGate holds one question the agent is blocked on. It keeps the
// prompt as well as the reply channel, so a browser that reconnects mid-run
// can ask what it is being asked instead of leaving the agent hanging.
type approvalGate struct {
	mu      sync.Mutex
	replyCh chan approvalAction
	prompt  map[string]any
	closed  bool
}

func newApprovalGate() *approvalGate {
	return &approvalGate{replyCh: make(chan approvalAction, 1)}
}

// setPrompt records what the user is being asked.
func (g *approvalGate) setPrompt(prompt map[string]any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prompt = prompt
}

// currentPrompt copies the pending question, if there still is one.
func (g *approvalGate) currentPrompt() (map[string]any, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.prompt == nil {
		return nil, false
	}
	prompt := make(map[string]any, len(g.prompt))
	for key, value := range g.prompt {
		prompt[key] = value
	}
	return prompt, true
}

// respond delivers the user's answer. Two browser tabs may both answer the
// same question; the second one is refused rather than queued, because the
// buffered slot belongs to the call that is already being decided.
func (g *approvalGate) respond(action approvalAction) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return fmt.Errorf("согласование уже закрыто")
	}
	select {
	case g.replyCh <- action:
		g.prompt = nil
		return nil
	default:
		return fmt.Errorf("ответ на согласование уже отправлен")
	}
}

// wait blocks until an answer arrives or the run is canceled.
func (g *approvalGate) wait(ctx context.Context) (approvalAction, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case a, ok := <-g.replyCh:
		if !ok {
			return "", fmt.Errorf("согласование закрыто")
		}
		return a, nil
	}
}

// close releases everyone waiting on the gate.
func (g *approvalGate) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed {
		g.closed = true
		close(g.replyCh)
	}
}

// setApprovalGate installs the question of one chat, replacing any earlier
// one: a run that has moved on left nobody waiting for the old answer.
func (h *webHub) setApprovalGate(chatID string, gate *approvalGate) {
	h.approvalMu.Lock()
	defer h.approvalMu.Unlock()
	if h.approvals == nil {
		h.approvals = make(map[string]*approvalGate)
	}
	if old := h.approvals[chatID]; old != nil {
		old.close()
	}
	h.approvals[chatID] = gate
}

// clearApprovalGate drops the question of one chat once it is answered.
func (h *webHub) clearApprovalGate(chatID string) {
	h.approvalMu.Lock()
	defer h.approvalMu.Unlock()
	if g := h.approvals[chatID]; g != nil {
		g.close()
		delete(h.approvals, chatID)
	}
}

// approvalGate returns the question one chat is blocked on, or nil.
func (h *webHub) approvalGate(chatID string) *approvalGate {
	h.approvalMu.Lock()
	defer h.approvalMu.Unlock()
	return h.approvals[chatID]
}

// webConfirmer puts a mutating call to the browser and blocks the agent until
// an answer comes back. It is the web counterpart of tuiConfirmer.
type webConfirmer struct {
	hub    *webHub
	chatID string
}

// ConfirmToolCall implements agenttools.Confirmer.
func (c *webConfirmer) ConfirmToolCall(ctx context.Context, req agenttools.ConfirmRequest) (bool, error) {
	gate := newApprovalGate()
	prompt := map[string]any{
		"kind":   "tool",
		"tool":   req.Tool,
		"args":   encodeConfirmArgs(req.Args),
		"action": "Агент хочет выполнить " + req.Tool,
	}
	gate.setPrompt(prompt)
	c.hub.setApprovalGate(c.chatID, gate)
	defer c.hub.clearApprovalGate(c.chatID)

	c.hub.publishSSE(c.chatID, "approval_prompt", prompt)

	action, err := gate.wait(ctx)
	c.hub.publishSSE(c.chatID, "approval_resolved", map[string]any{
		"action": string(action),
		"error":  errString(err),
	})
	if err != nil {
		return false, err
	}
	switch action {
	case approvalYes:
		return true, nil
	case approvalQuit:
		// Stopping is not the same as refusing one call: the error ends the
		// run instead of letting the model try the next mutating step.
		return false, fmt.Errorf("выполнение остановлено пользователем")
	default:
		return false, nil
	}
}

// errString renders an error for a JSON payload, nil included.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// parseApprovalAction reads the browser's answer. Russian words are accepted
// because the same endpoint is convenient from a terminal.
func parseApprovalAction(s string) (approvalAction, error) {
	switch s {
	case "yes", "y", "да", "д":
		return approvalYes, nil
	case "no", "n", "нет", "н", "":
		return approvalNo, nil
	case "quit", "q", "стоп", "с":
		return approvalQuit, nil
	default:
		return "", fmt.Errorf("неизвестное действие %q: допустимы yes, no и quit", s)
	}
}
