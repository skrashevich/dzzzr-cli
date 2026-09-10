package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/skrashevich/dzzzr-cli/agentloop"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func init() {
	register(command{
		Name:  "chat",
		Usage: "chat [-security readonly|approve|full]",
		Auth:  authNone,
		Run:   cmdChat,
		Help:  "Полноэкранный диалог с агентом: он читает игру и действует инструментами движка",
	})
}

// cmdChat runs the full-screen conversation.
func cmdChat(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error {
	if len(args) > 0 {
		return fatal("команда chat не принимает аргументов")
	}
	policy, err := agenttools.ParsePolicy(cfg.security)
	if err != nil {
		return fatal("неверное значение -security %q: допустимы readonly, approve и full", cfg.security)
	}
	agentCfg, err := llmConfig()
	if err != nil {
		return err
	}
	if err := agentAuthorize(ctx, cfg, c); err != nil {
		return err
	}

	dir, err := chatsDir()
	if err != nil {
		return err
	}
	store := newChatStore(dir)
	if err := store.loadFromDisk(cfg.debugf); err != nil {
		cfg.debugf("чаты не загружены: %v", err)
	}

	m := newChatModel(ctx, cfg, c, store, agentCfg, policy)

	// -debug writes to stderr, which would be painted over the alternate
	// screen; it goes to a file for as long as the conversation lasts.
	if cfg.debug {
		restore, path, err := redirectDebugToFile(cfg)
		if err == nil {
			m.note = "журнал отладки → " + path
			defer restore()
		} else {
			m.note = "отладка выключена: " + err.Error()
		}
	}

	program := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	m.program = program
	_, err = program.Run()
	store.cancelAll()
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	if err != nil {
		// Being run without a terminal is not a malfunction, it is the wrong
		// command: the one-shot mode exists for exactly that.
		if strings.Contains(err.Error(), "/dev/tty") || strings.Contains(err.Error(), "TTY") {
			return fatal("«dzzzr chat» нужен терминал. В скрипте и конвейере используйте «dzzzr agent ЗАПРОС»")
		}
		return fatal("диалог завершился с ошибкой: %v", err)
	}
	return nil
}

// redirectDebugToFile points diagnostics at a log file for as long as the
// alternate screen is up, and returns the function that puts them back.
//
// It is cfg.stderr that has to move: the run captured os.Stderr into it at
// startup, so swapping the package variable alone would leave every debugf and
// every logged HTTP request painting over the conversation. The package
// variable is swapped too, for anything that writes to it directly.
func redirectDebugToFile(cfg *config) (func(), string, error) {
	dir, err := sessionDir()
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(dir, sessionDirPerm); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, "chat-debug.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, sessionFilePerm)
	if err != nil {
		return nil, "", err
	}
	prevCfg, prevOS := cfg.stderr, os.Stderr
	cfg.stderr, os.Stderr = f, f
	return func() {
		cfg.stderr, os.Stderr = prevCfg, prevOS
		_ = f.Close()
	}, path, nil
}

// Messages the agent goroutine pushes into the Bubble Tea loop.
//
// Every one of them carries the chat it belongs to. A run outlives the user's
// attention: they can move to another conversation while the model is still
// answering, and a message without its chat would be filed under whichever one
// happens to be on screen.
type (
	agentEventMsg struct {
		chatID string
		ev     agentloop.Event
	}
	agentStatusMsg struct {
		chatID  string
		message string
	}
	turnDoneMsg struct {
		chatID string
		err    error
	}
	approvalMsg struct {
		chatID string
		tool   string
		args   string
		reply  chan bool
	}
)

// chatBridge runs one turn on its own goroutine and reports back through the
// program. It never touches the terminal itself.
type chatBridge struct {
	program  *tea.Program
	store    *chatStore
	cfg      *config
	client   *dzzzr.Client
	agentCfg agentloop.Config
	chatID   string
	policy   agenttools.Policy
}

func (b *chatBridge) send(msg tea.Msg) {
	if b.program != nil {
		b.program.Send(msg)
	}
}

// run carries out one turn and always reports its end, so the UI never stays
// stuck showing a run that has already stopped.
func (b *chatBridge) run(ctx context.Context, history []agentloop.Message) {
	var err error
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("агент аварийно завершился: %v", rec)
		}
		b.send(turnDoneMsg{chatID: b.chatID, err: err})
	}()

	catalog, cerr := b.catalog()
	if cerr != nil {
		err = cerr
		b.store.finishRun(b.chatID, nil)
		return
	}
	files := agentFileTools(b.cfg)

	res, rerr := agentloop.Run(ctx, b.agentCfg, &agentloop.RunInput{
		Catalog:      catalog,
		Extra:        files,
		Messages:     history,
		SystemPrompt: agentSystemPrompt(b.cfg, catalog, len(files) > 0),
	}, agentloop.Callbacks{
		OnEvent:  func(ev agentloop.Event) { b.send(agentEventMsg{chatID: b.chatID, ev: ev}) },
		OnStatus: func(_, message string) { b.send(agentStatusMsg{chatID: b.chatID, message: message}) },
	})
	err = rerr
	if rerr != nil {
		b.store.finishRun(b.chatID, nil)
		return
	}
	b.store.finishRun(b.chatID, res.Messages)
	_ = b.store.persist(b.chatID)
}

// catalog builds the toolset for this turn under the chat's own policy.
func (b *chatBridge) catalog() (*agenttools.Catalog, error) {
	opts := agenttools.Options{
		Policy:       b.policy,
		ReadCacheTTL: readCacheTTL,
		IncludeAdmin: b.client.HasAdminCredentials(),
	}
	if b.policy == agenttools.PolicyApprove {
		opts.Confirmer = &tuiConfirmer{bridge: b}
	}
	catalog, err := agenttools.NewCatalog(b.client, opts)
	if err != nil {
		return nil, fatal("не удалось собрать набор инструментов: %v", err)
	}
	return catalog, nil
}

// tuiConfirmer puts a mutating call to the user through the Bubble Tea loop
// and blocks the agent until an answer comes back.
type tuiConfirmer struct{ bridge *chatBridge }

// ConfirmToolCall implements agenttools.Confirmer.
func (t *tuiConfirmer) ConfirmToolCall(ctx context.Context, req agenttools.ConfirmRequest) (bool, error) {
	reply := make(chan bool, 1)
	t.bridge.send(approvalMsg{
		chatID: t.bridge.chatID,
		tool:   req.Tool,
		args:   encodeConfirmArgs(req.Args),
		reply:  reply,
	})
	select {
	case allowed := <-reply:
		return allowed, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// exportChatMarkdown renders a conversation as a document a team can keep.
func exportChatMarkdown(snap chatSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", snap.Title)
	fmt.Fprintf(&b, "Город: %s  \nПрава: %s  \nНачат: %s\n\n",
		snap.City, snap.Policy, snap.CreatedAt.Format("2006-01-02 15:04:05"))
	for _, line := range snap.Lines {
		switch line.Role {
		case chatRoleUser:
			fmt.Fprintf(&b, "## Вы\n\n%s\n\n", line.Content)
		case chatRoleAssistant:
			fmt.Fprintf(&b, "## Агент\n\n%s\n\n", line.Content)
		case chatRoleTool:
			fmt.Fprintf(&b, "> %s\n\n", line.Content)
		case chatRoleSystem:
			fmt.Fprintf(&b, "_%s_\n\n", line.Content)
		}
	}
	return b.String()
}
