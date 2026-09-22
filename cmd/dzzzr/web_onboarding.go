package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// onboardingStep is one item of the wizard's checklist. Its ID is what the
// browser keys on and its Title is a stable English identifier: the wizard's
// HTML already carries the Russian labels the user reads.
type onboardingStep struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Done   bool   `json:"done"`
	Detail string `json:"detail,omitempty"`
}

type onboardingStatusPayload struct {
	Required    bool             `json:"required"`
	Completed   bool             `json:"completed"`
	CompletedAt string           `json:"completed_at,omitempty"`
	Skipped     bool             `json:"skipped"`
	Steps       []onboardingStep `json:"steps"`
	Error       string           `json:"error,omitempty"`
}

// Who the wizard's last step signs in as.
const (
	onboardingRolePlayer    = "player"
	onboardingRoleOrganizer = "organizer"
)

// onboardingCompleteBody is the wizard's last step: one sign-in, as a player
// or as an organizer — whichever the user came to dzzzr for.
type onboardingCompleteBody struct {
	Role     string `json:"role"`
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (h *webHub) httpOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	webWriteJSON(w, http.StatusOK, h.onboardingStatusPayload())
}

func (h *webHub) httpOnboardingComplete(w http.ResponseWriter, r *http.Request) {
	var body onboardingCompleteBody
	if !webReadJSON(w, r, &body) {
		return
	}
	var (
		code int
		err  error
	)
	switch strings.TrimSpace(body.Role) {
	case onboardingRolePlayer:
		code, err = h.loginPlayer(r.Context(), body.Login, body.Password)
	case onboardingRoleOrganizer:
		code, err = h.loginOrganizer(r.Context(), body.Login, body.Password)
	default:
		webError(w, http.StatusBadRequest, "укажите роль: player или organizer")
		return
	}
	if err != nil {
		webError(w, code, "%v", err)
		return
	}

	state := onboardingState{
		Completed:   true,
		CompletedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveOnboardingState(state); err != nil {
		webError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	webWriteJSON(w, http.StatusOK, h.onboardingStatusPayload())
}

func (h *webHub) httpOnboardingReset(w http.ResponseWriter, r *http.Request) {
	if err := resetOnboardingState(); err != nil {
		webError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	webWriteJSON(w, http.StatusOK, h.onboardingStatusPayload())
}

// onboardingStatusPayload reports whether the wizard has to run and how far
// the two things it configures already are. It has to run until it was
// completed once or both things are configured anyway.
//
// A state file that will not parse is reported rather than propagated:
// refusing the request would make the wizard unreachable, and the user would
// have no way left to configure the thing that wrote the broken file. An
// unreadable state counts as not completed, so the worst case is being asked
// once more.
func (h *webHub) onboardingStatusPayload() onboardingStatusPayload {
	state, loadErr := loadOnboardingState()
	steps := []onboardingStep{
		h.onboardingLLMStep(),
		h.onboardingAuthStep(),
	}
	// A user who configured dzzzr before the wizard existed — a model from the
	// environment, a session on disk — has nothing left for it to do.
	configured := true
	for _, step := range steps {
		configured = configured && step.Done
	}
	payload := onboardingStatusPayload{
		Required:    !state.Completed && !configured,
		Completed:   state.Completed,
		CompletedAt: state.CompletedAt,
		Skipped:     state.Skipped,
		Steps:       steps,
	}
	if loadErr != nil {
		payload.Error = loadErr.Error()
	}
	return payload
}

// onboardingLLMStep is done when llmConfig would hand the agent a working
// transport, which is the question the wizard's model step asks.
func (h *webHub) onboardingLLMStep() onboardingStep {
	step := onboardingStep{ID: "llm", Title: "LLM transport"}
	cfg, err := llmConfig()
	if err != nil {
		step.Detail = err.Error()
		return step
	}
	step.Done = true
	step.Detail = fmt.Sprintf("%s, модель %s", agentAuthMethod(cfg), cfg.Model)
	return step
}

// onboardingAuthStep is done as soon as the city has a player session or the
// client carries an organizer: the wizard asks for one sign-in, not both.
func (h *webHub) onboardingAuthStep() onboardingStep {
	step := onboardingStep{ID: "auth", Title: "Dozor login", Detail: "вход не выполнен"}
	h.clientMu.Lock()
	defer h.clientMu.Unlock()
	switch {
	case h.client.HasAdminCredentials():
		step.Done = true
		step.Detail = "организатор: " + h.client.AdminLogin()
	case h.status().HasSession:
		step.Done = true
		step.Detail = "игрок: " + h.client.LoginName()
	}
	return step
}
