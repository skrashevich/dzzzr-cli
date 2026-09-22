package main

import (
	"context"
	"embed"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/skrashevich/dzzzr-cli/agentfiles"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

//go:embed webui/*
var webUIFiles embed.FS

// defaultWebAddr keeps the server on the loopback interface: it answers with
// the team's session and with whatever the agent may do to the game, and
// nothing about it is authenticated.
const defaultWebAddr = "127.0.0.1:8788"

func init() {
	register(command{
		Name:  "web",
		Usage: "web [-web-addr АДРЕС] [-security readonly|approve|full]",
		Auth:  authNone,
		Run:   cmdWeb,
		Help:  "Браузерный интерфейс: тот же агент, что и «dzzzr chat», и редактор игр рядом с ним",
	}, command{
		Name:  "editor",
		Usage: "editor [-web-addr АДРЕС]",
		Auth:  authNone,
		Run:   cmdEditor,
		Help:  "Тот же браузерный интерфейс, что и «dzzzr web», но открытый сразу в редакторе игр",
	})
}

// noBrowserEnv keeps «dzzzr web» from opening a browser window. A test or a
// server run has nobody to show the window to, and on a desktop every run of
// the e2e suite would otherwise pop a tab in the developer's browser.
const noBrowserEnv = "DZZZR_NO_BROWSER"

// editorFragment is the address of the editor inside the page; editor.js reads
// it on load, so a browser opened on it lands in the editor rather than chat.
const editorFragment = "#/editor"

// webRunTurnFn carries out one turn of one chat. It is a field of the hub so
// a test can drive the HTTP surface without a model behind it.
type webRunTurnFn func(ctx context.Context, hub *webHub, chatID string)

// webHub ties the HTTP handlers to the one client, the chat store and the
// event streams.
type webHub struct {
	cfg    *config
	client *dzzzr.Client
	store  *chatStore
	sse    *sseHub
	run    webRunTurnFn

	// clientMu serializes the handlers that change what the client is signed
	// in as. The agent run deliberately does not take it: dzzzr.Client is safe
	// for concurrent use, and holding the lock for a whole run would leave the
	// browser unable to even ask who is signed in.
	clientMu sync.Mutex
	draftMu  sync.Mutex

	approvalMu sync.Mutex
	approvals  map[string]*approvalGate

	// codexMu guards codexLogin, the ChatGPT sign-ins in flight. It is built
	// on first use, so a hub assembled as a struct literal still serves them.
	codexMu    sync.Mutex
	codexLogin *codexLoginManager

	// polzaMu guards polza, the Polza.ai sign-ins in flight; built on first use.
	polzaMu sync.Mutex
	polza   *polzaManager
}

// publishSSE sends one event to everyone watching a chat.
func (h *webHub) publishSSE(chatID, eventType string, payload any) {
	h.sse.room(chatID).broadcast(formatSSE(eventType, payload))
}

// webChatsDir returns ~/.config/dzzzr/web/chats. The browser keeps its own
// history: the same directory as the TUI would mix two transcripts the user
// thinks of as separate.
func webChatsDir() (string, error) {
	dir, err := sessionDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "web", "chats"), nil
}

// webWriteJSON answers with a JSON body and a status code.
func webWriteJSON(w http.ResponseWriter, code int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte(`{"error":"не удалось сформировать ответ"}`)
		code = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(data)
}

// webError answers with one Russian message under the "error" key.
func webError(w http.ResponseWriter, code int, format string, args ...any) {
	webWriteJSON(w, code, map[string]string{"error": fmt.Sprintf(format, args...)})
}

// webReadJSON decodes a request body, answering the browser itself when it
// cannot. A megabyte is far more than any of these requests carry.
func webReadJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer func() { _ = r.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		webError(w, http.StatusBadRequest, "тело запроса не прочитано")
		return false
	}
	if len(data) == 0 {
		webError(w, http.StatusBadRequest, "пустое тело запроса")
		return false
	}
	if err := json.Unmarshal(data, dst); err != nil {
		webError(w, http.StatusBadRequest, "неверный JSON: %v", err)
		return false
	}
	return true
}

// newMux wires every endpoint plus the embedded interface.
func (h *webHub) newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/chats", h.httpListChats)
	mux.HandleFunc("POST /api/v1/chats", h.httpCreateChat)
	mux.HandleFunc("GET /api/v1/chats/{id}", h.httpGetChat)
	mux.HandleFunc("PATCH /api/v1/chats/{id}", h.httpPatchChat)
	mux.HandleFunc("DELETE /api/v1/chats/{id}", h.httpDeleteChat)
	mux.HandleFunc("POST /api/v1/chats/{id}/messages", h.httpPostMessage)
	mux.HandleFunc("POST /api/v1/chats/{id}/files", h.httpUploadChatFile)
	mux.HandleFunc("GET /api/v1/chats/{id}/files/{name}", h.httpDownloadChatFile)
	mux.HandleFunc("GET /api/v1/chats/{id}/events", h.httpSSE)
	mux.HandleFunc("POST /api/v1/chats/{id}/cancel", h.httpCancelChat)
	mux.HandleFunc("GET /api/v1/chats/{id}/approval", h.httpGetApproval)
	mux.HandleFunc("POST /api/v1/chats/{id}/approval", h.httpPostApproval)
	mux.HandleFunc("GET /api/v1/chats/{id}/export", h.httpExportChat)

	mux.HandleFunc("POST /api/v1/auth/login", h.httpAuthLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", h.httpAuthLogout)
	mux.HandleFunc("GET /api/v1/auth/status", h.httpAuthStatus)
	mux.HandleFunc("GET /api/v1/catalog/games", h.httpCatalogGames)
	mux.HandleFunc("GET /api/v1/agent/config", h.httpAgentConfig)
	mux.HandleFunc("/api/v1/llm/settings", h.httpLLMSettings)
	mux.HandleFunc("GET /api/v1/llm/codex/status", h.httpCodexStatus)
	mux.HandleFunc("POST /api/v1/llm/codex/logout", h.httpCodexLogout)
	mux.HandleFunc("POST /api/v1/llm/codex/login", h.httpCodexLoginStart)
	mux.HandleFunc("GET /api/v1/llm/codex/login/{id}", h.httpCodexLoginStatus)
	mux.HandleFunc("POST /api/v1/llm/codex/login/{id}/code", h.httpCodexLoginCode)
	mux.HandleFunc("DELETE /api/v1/llm/codex/login/{id}", h.httpCodexLoginCancel)
	mux.HandleFunc("POST /api/v1/llm/polza/login", h.httpPolzaLoginStart)
	mux.HandleFunc("GET /api/v1/llm/polza/login/{id}", h.httpPolzaLoginStatus)
	mux.HandleFunc("DELETE /api/v1/llm/polza/login/{id}", h.httpPolzaLoginCancel)
	mux.HandleFunc("POST /api/v1/llm/polza/login/{id}/code", h.httpPolzaLoginCode)
	mux.HandleFunc("GET /api/v1/llm/polza/models", h.httpPolzaModels)
	mux.HandleFunc("POST /api/v1/llm/polza/connect", h.httpPolzaConnect)
	mux.HandleFunc("POST /api/v1/llm/polza/check", h.httpPolzaCheck)
	h.registerAdminRoutes(mux)
	h.registerDraftRoutes(mux)

	sub, err := fs.Sub(webUIFiles, "webui")
	if err != nil {
		// The directory is embedded at build time; a failure here means the
		// binary itself is malformed.
		panic("webui: " + err.Error())
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return mux
}

// cmdWeb serves the browser conversation until the run is interrupted.
func cmdWeb(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	return runWeb(ctx, cfg, c, args, "web", false)
}

// cmdEditor is «dzzzr web» opened on the editor: the same server, the same
// chats beside it, only the first screen differs.
func cmdEditor(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	return runWeb(ctx, cfg, c, args, "editor", true)
}

// runWeb serves the browser interface until the run is interrupted. editor
// asks for the browser to open on the editor.
func runWeb(ctx context.Context, cfg *config, c *dzzzr.Client, args []string, name string, editor bool) error {
	if len(args) > 0 {
		return fatal("команда %s не принимает аргументов", name)
	}
	if _, err := agenttools.ParsePolicy(cfg.security); err != nil {
		return fatal("неверное значение -security %q: допустимы readonly, approve и full", cfg.security)
	}
	// A missing model is reported by /api/v1/agent/config rather than here:
	// the interface is still worth serving so the user can see what is wrong.
	if err := webAuthorize(ctx, cfg, c); err != nil {
		return err
	}

	dir, err := webChatsDir()
	if err != nil {
		return err
	}
	store := newChatStore(dir)
	if err := store.loadFromDisk(cfg.debugf); err != nil {
		cfg.debugf("чаты не загружены: %v", err)
	}
	defer store.cancelAll()

	hub := &webHub{cfg: cfg, client: c, store: store, sse: newSSEHub(), run: runWebChatTurn}
	// A Polza sign-in holds a local callback port; stopping the server frees it.
	defer hub.closePolza()
	return serveWeb(ctx, hub, cfg.webAddr, webStartPage(c, editor))
}

// webStartPage picks the fragment the browser opens on. Besides an explicit
// «dzzzr editor», a run that carries organizer credentials and no playing
// session is an organizer at work: the chat there can only ask for a player
// login, so the editor is the screen that has something to show. An empty
// result leaves the choice to the page, which returns to the view last used.
func webStartPage(c *dzzzr.Client, editor bool) string {
	if editor || (c.Session() == "" && c.HasAdminCredentials()) {
		return editorFragment
	}
	return ""
}

// webAuthorize prepares the client the browser will use, and never refuses to
// start over a missing playing session.
//
// The terminal surfaces have to insist on one: «dzzzr chat» has no way to ask
// for a login. The browser does — its login panel posts to
// /api/v1/auth/login — and a run started with organizer credentials alone
// («dzzzr -admin-login … -admin-password … web») is a whole working mode of
// its own: the administration area needs no playing session at all. Refusing
// to serve the interface would leave the user without the very screen that
// fixes the problem.
//
// A session file that cannot be read is still an error: it is a real fault
// with a real remedy, and silently ignoring it would sign nobody in and say
// nothing about why.
func webAuthorize(ctx context.Context, cfg *config, c *dzzzr.Client) error {
	path, err := sessionPath(cfg.city)
	if err != nil {
		return err
	}
	if _, _, err := loadSession(cfg, c); err != nil {
		return err
	}
	applyCredentialOverrides(cfg, c)

	if c.Session() != "" {
		return nil
	}
	if canSignIn(cfg) {
		if err := signIn(ctx, cfg, c); err != nil {
			_, _ = fmt.Fprintf(cfg.stderr, "Вход игроком не выполнен (%v): войдите в браузере.\n", err)
		}
		return nil
	}
	if c.HasAdminCredentials() {
		_, _ = fmt.Fprintf(cfg.stderr, "Игровой сессии нет (%s); организаторской части она не нужна.\n", path)
		return nil
	}
	_, _ = fmt.Fprintf(cfg.stderr, "Игровой сессии нет (%s): войдите в браузере или задайте -login и -password.\n", path)
	return nil
}

// serveWeb runs the HTTP server until ctx is canceled, announcing the address
// and opening a browser on it; page is the fragment the browser opens on.
func serveWeb(ctx context.Context, hub *webHub, addr, page string) error {
	if strings.TrimSpace(addr) == "" {
		addr = defaultWebAddr
	}
	srv := &http.Server{Addr: addr, Handler: hub.newMux()}

	url := "http://" + addr + "/" + page
	_, _ = fmt.Fprintf(hub.cfg.stderr, "dzzzr web: %s (Ctrl+C — выход)\n", url)
	if os.Getenv(noBrowserEnv) == "" {
		if err := openBrowser(url); err != nil {
			hub.cfg.debugf("браузер не открыт: %v", err)
		}
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fatal("не удалось запустить сервер на %s: %v", addr, err)
		}
		return nil
	}
}

// openBrowser opens url in the system browser where the platform allows it.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// webChatListItem is one row of the sidebar.
type webChatListItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	City      string    `json:"city"`
	UpdatedAt time.Time `json:"updated_at"`
	Running   bool      `json:"running"`
}

func (h *webHub) httpListChats(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	snaps := h.store.list()
	items := make([]webChatListItem, 0, len(snaps))
	for _, s := range snaps {
		if q != "" && !chatMatchesQuery(s, q) {
			continue
		}
		items = append(items, webChatListItem{
			ID:        s.ID,
			Title:     s.Title,
			City:      s.City,
			UpdatedAt: s.UpdatedAt,
			Running:   s.Running,
		})
	}
	webWriteJSON(w, http.StatusOK, map[string]any{"chats": items})
}

// chatMatchesQuery reports whether the search box should keep a chat. The
// search runs over the transcript as well as the title, because a chat is
// usually remembered by what was said in it.
func chatMatchesQuery(s chatSnapshot, q string) bool {
	q = strings.ToLower(q)
	if strings.Contains(strings.ToLower(s.Title), q) {
		return true
	}
	for _, line := range s.Lines {
		if strings.Contains(strings.ToLower(line.Content), q) {
			return true
		}
	}
	return false
}

// createChatRequest is the body of POST /api/v1/chats. The city is not part
// of it: one run of the command serves one city.
type createChatRequest struct {
	Policy string `json:"policy"`
}

func (h *webHub) httpCreateChat(w http.ResponseWriter, r *http.Request) {
	var req createChatRequest
	if !webReadJSON(w, r, &req) {
		return
	}
	policy := h.defaultPolicy()
	if strings.TrimSpace(req.Policy) != "" {
		parsed, err := agenttools.ParsePolicy(req.Policy)
		if err != nil {
			webError(w, http.StatusBadRequest, "неверные права %q: допустимы readonly, approve и full", req.Policy)
			return
		}
		policy = parsed
	}
	snap := h.store.create(h.cfg.city, policy)
	_ = h.store.persist(snap.ID)
	webWriteJSON(w, http.StatusCreated, snap)
}

// defaultPolicy is what a new chat starts with: whatever -security the run
// carries, and the package default when that cannot be read.
func (h *webHub) defaultPolicy() agenttools.Policy {
	policy, err := agenttools.ParsePolicy(h.cfg.security)
	if err != nil {
		return agenttools.DefaultPolicy
	}
	return policy
}

func (h *webHub) httpGetChat(w http.ResponseWriter, r *http.Request) {
	snap, ok := h.store.get(r.PathValue("id"))
	if !ok {
		webError(w, http.StatusNotFound, "чат не найден")
		return
	}
	webWriteJSON(w, http.StatusOK, snap)
}

// patchChatRequest is the body of PATCH /api/v1/chats/{id}. An absent field
// leaves what it names alone.
type patchChatRequest struct {
	Title  *string `json:"title"`
	Policy *string `json:"policy"`
}

func (h *webHub) httpPatchChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req patchChatRequest
	if !webReadJSON(w, r, &req) {
		return
	}
	patch := chatPatch{Title: req.Title}
	if req.Policy != nil {
		policy, err := agenttools.ParsePolicy(*req.Policy)
		if err != nil {
			webError(w, http.StatusBadRequest, "неверные права %q: допустимы readonly, approve и full", *req.Policy)
			return
		}
		patch.Policy = &policy
	}
	snap, ok := h.store.update(id, patch)
	if !ok {
		webError(w, http.StatusNotFound, "чат не найден")
		return
	}
	_ = h.store.persist(id)
	webWriteJSON(w, http.StatusOK, snap)
}

func (h *webHub) httpDeleteChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.store.remove(id) {
		webError(w, http.StatusNotFound, "чат не найден")
		return
	}
	h.store.removePersisted(id)
	h.clearApprovalGate(id)
	h.sse.removeChat(id)
	webWriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// postMessageRequest is the body of POST /api/v1/chats/{id}/messages.
type postMessageRequest struct {
	Content string            `json:"content"`
	Files   []uploadedFileRef `json:"files,omitempty"`
}

func (h *webHub) httpPostMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req postMessageRequest
	if !webReadJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Content) == "" && len(req.Files) == 0 {
		webError(w, http.StatusBadRequest, "нужен текст сообщения или вложение")
		return
	}
	content := req.Content
	if len(req.Files) > 0 {
		content = appendUploadedFilesNote(content, req.Files)
	}
	busy, ok := h.store.appendUser(id, content)
	if busy {
		webError(w, http.StatusConflict, "агент ещё отвечает на предыдущее сообщение")
		return
	}
	if !ok {
		webError(w, http.StatusNotFound, "чат не найден")
		return
	}
	_ = h.store.persist(id)

	// The run outlives the request: the browser watches it over the event
	// stream rather than holding this connection open for minutes, so it must
	// not be tied to a context that ends with the response.
	run := h.run
	if run == nil {
		run = runWebChatTurn
	}
	go run(context.Background(), h, id)

	snap, _ := h.store.get(id)
	webWriteJSON(w, http.StatusAccepted, snap)
}

func (h *webHub) httpCancelChat(w http.ResponseWriter, r *http.Request) {
	if !h.store.cancelRun(r.PathValue("id")) {
		webError(w, http.StatusBadRequest, "в этом чате агент не работает")
		return
	}
	webWriteJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}

// approvalPostBody is the body of POST /api/v1/chats/{id}/approval.
type approvalPostBody struct {
	Action string `json:"action"`
}

func (h *webHub) httpGetApproval(w http.ResponseWriter, r *http.Request) {
	gate := h.approvalGate(r.PathValue("id"))
	if gate == nil {
		webError(w, http.StatusNotFound, "нечего согласовывать")
		return
	}
	prompt, ok := gate.currentPrompt()
	if !ok {
		webError(w, http.StatusNotFound, "нечего согласовывать")
		return
	}
	webWriteJSON(w, http.StatusOK, prompt)
}

func (h *webHub) httpPostApproval(w http.ResponseWriter, r *http.Request) {
	var body approvalPostBody
	if !webReadJSON(w, r, &body) {
		return
	}
	action, err := parseApprovalAction(strings.TrimSpace(body.Action))
	if err != nil {
		webError(w, http.StatusBadRequest, "%v", err)
		return
	}
	gate := h.approvalGate(r.PathValue("id"))
	if gate == nil {
		webError(w, http.StatusNotFound, "нечего согласовывать")
		return
	}
	if err := gate.respond(action); err != nil {
		webError(w, http.StatusConflict, "%v", err)
		return
	}
	webWriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "action": string(action)})
}

func (h *webHub) httpExportChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snap, ok := h.store.get(id)
	if !ok {
		webError(w, http.StatusNotFound, "чат не найден")
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	switch format {
	case "json":
		data, err := json.Marshal(snap, jsontext.WithIndent("  "))
		if err != nil {
			webError(w, http.StatusInternalServerError, "не удалось выгрузить чат: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="chat-`+id+`.json"`)
		_, _ = w.Write(data)
	case "", "markdown", "md":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="chat-`+id+`.md"`)
		_, _ = io.WriteString(w, exportChatMarkdown(snap))
	default:
		webError(w, http.StatusBadRequest, "формат должен быть markdown или json")
	}
}

func (h *webHub) httpAgentConfig(w http.ResponseWriter, r *http.Request) {
	_, filesErr := agentfiles.RootFromEnv()
	agentCfg, err := llmConfig()
	if err != nil {
		// A missing key is worth showing in the interface, not worth refusing
		// to answer over: the rest of the page still works.
		webWriteJSON(w, http.StatusOK, map[string]any{
			"model":         "",
			"base_url":      "",
			"files_enabled": filesErr == nil,
			"error":         err.Error(),
		})
		return
	}
	webWriteJSON(w, http.StatusOK, map[string]any{
		"model":         agentCfg.Model,
		"base_url":      agentCfg.BaseURL,
		"files_enabled": filesErr == nil,
	})
}

func (h *webHub) httpSSE(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.store.get(id); !ok {
		webError(w, http.StatusNotFound, "чат не найден")
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		webError(w, http.StatusInternalServerError, "поток событий не поддерживается")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	room := h.sse.room(id)
	ch := room.subscribe(32)
	defer room.unsubscribe(ch)
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	done := r.Context().Done()
	for {
		select {
		case <-done:
			return
		case frame, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// authStatus is what the interface shows about the one city it serves.
type authStatus struct {
	City       string `json:"city"`
	Login      string `json:"login"`
	HasSession bool   `json:"has_session"`
}

// status reports who the client is signed in as. The caller must hold
// clientMu.
func (h *webHub) status() authStatus {
	out := authStatus{City: h.cfg.city, Login: h.client.LoginName()}
	if path, err := sessionPath(h.cfg.city); err == nil {
		if _, statErr := os.Stat(path); statErr == nil {
			out.HasSession = true
		}
	}
	return out
}
