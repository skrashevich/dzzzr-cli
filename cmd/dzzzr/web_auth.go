package main

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// authLoginBody is the body of POST /api/v1/auth/login. There is no city in
// it: one run of «dzzzr web» serves the city it was started with.
type authLoginBody struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (h *webHub) httpAuthStatus(w http.ResponseWriter, r *http.Request) {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()
	webWriteJSON(w, http.StatusOK, h.status())
}

// httpAuthLogin signs in and stores the session, so the next run of any dzzzr
// command finds it where «dzzzr login» would have left it.
func (h *webHub) httpAuthLogin(w http.ResponseWriter, r *http.Request) {
	var body authLoginBody
	if !webReadJSON(w, r, &body) {
		return
	}
	login := strings.TrimSpace(body.Login)
	if login == "" || body.Password == "" {
		webError(w, http.StatusBadRequest, "укажите логин и пароль")
		return
	}

	h.clientMu.Lock()
	defer h.clientMu.Unlock()

	// The credentials go on the config because signIn — and the automatic
	// re-authentication the tools rely on — read them from there.
	h.cfg.login, h.cfg.password = login, body.Password
	if err := signIn(r.Context(), h.cfg, h.client); err != nil {
		webError(w, loginStatusCode(err), "%v", err)
		return
	}
	webWriteJSON(w, http.StatusOK, h.status())
}

// loginStatusCode separates a login the engine refused from an engine that
// could not be reached, so the interface can say which of the two happened.
// signIn reports a refusal as a command-level error and lets a transport
// failure through as it came.
func loginStatusCode(err error) int {
	var ce *cliError
	if dzzzr.AuthErrorKindOf(err) != 0 || errors.As(err, &ce) {
		return http.StatusUnauthorized
	}
	return http.StatusBadGateway
}

// httpAuthLogout forgets the session both in memory and on disk: leaving the
// file behind would sign the next run back in.
func (h *webHub) httpAuthLogout(w http.ResponseWriter, r *http.Request) {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()

	h.client.Logout()
	h.cfg.login, h.cfg.password = "", ""
	path, err := sessionPath(h.cfg.city)
	if err != nil {
		webError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		webError(w, http.StatusInternalServerError, "не удалось удалить %s: %v", path, err)
		return
	}
	webWriteJSON(w, http.StatusOK, h.status())
}
