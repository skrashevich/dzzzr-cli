package main

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/skrashevich/dzzzr-cli/agentloop"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// focusTarget says which pane the keyboard is talking to.
type focusTarget int

const (
	focusInput focusTarget = iota
	focusList
)

// chatModel is the Bubble Tea model behind "dzzzr chat".
type chatModel struct {
	ctx      context.Context
	cfg      *config
	client   *dzzzr.Client
	store    *chatStore
	agentCfg agentloop.Config
	program  *tea.Program

	chatID     string
	chats      []chatSnapshot
	transcript []chatLine

	// Fields of the active chat, refreshed by refreshChats.
	title  string
	city   string
	policy agenttools.Policy

	// ceiling is what -security allowed this run. A chat remembers the policy
	// it was last used with, and a chat saved as "full" must not hand the
	// agent that back in a run the user started read-only.
	ceiling agenttools.Policy

	view  viewport.Model
	input textarea.Model

	// The markdown renderer fixes its wrap width when it is built, so it is
	// rebuilt when the window changes.
	renderer *glamour.TermRenderer
	wrap     int

	ready         bool
	width, height int
	showList      bool
	focus         focusTarget
	listIdx       int

	running bool
	// approvals is a queue: two chats can be waiting on the user at once, and
	// the second must not displace the first.
	approvals []approvalMsg

	status string
	note   string
}

// newChatModel prepares the conversation, opening the most recent chat or
// creating one when there is none.
func newChatModel(ctx context.Context, cfg *config, c *dzzzr.Client, store *chatStore,
	agentCfg agentloop.Config, policy agenttools.Policy) *chatModel {
	input := textarea.New()
	input.Placeholder = "Спросите агента (/help — команды)"
	input.Prompt = "┃ "
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.SetHeight(chatInputHeight)
	// Enter sends the message, so the textarea must not take it as a newline.
	input.KeyMap.InsertNewline.SetEnabled(false)
	input.Focus()

	view := viewport.New(60, 10)
	view.MouseWheelEnabled = true

	m := &chatModel{
		ctx:      ctx,
		cfg:      cfg,
		client:   c,
		store:    store,
		agentCfg: agentCfg,
		policy:   policy,
		ceiling:  policy,
		view:     view,
		input:    input,
		showList: true,
		status:   "модель: " + agentCfg.Model,
	}
	m.openMostRecent(policy)
	return m
}

// openMostRecent selects the newest chat, creating one when the store is empty.
func (m *chatModel) openMostRecent(policy agenttools.Policy) {
	list := m.store.list()
	if len(list) == 0 {
		snap := m.store.create(m.cfg.city, policy)
		m.persist(snap.ID)
		m.chatID = snap.ID
	} else {
		m.chatID = list[0].ID
	}
	m.refreshChats()
	m.loadTranscript()
}

// refreshChats reloads the sidebar and the header fields of the active chat.
func (m *chatModel) refreshChats() {
	m.chats = m.store.list()
	for i, c := range m.chats {
		if c.ID != m.chatID {
			continue
		}
		m.listIdx = i
		m.title, m.city = c.Title, c.City
		m.policy = clampPolicy(c.Policy, m.ceiling)
		m.running = c.Running
		return
	}
	m.listIdx = min(m.listIdx, max(0, len(m.chats)-1))
}

// policyRank orders the policies from the most restrictive to the least.
func policyRank(p agenttools.Policy) int {
	switch p {
	case agenttools.PolicyReadonly:
		return 0
	case agenttools.PolicyFull:
		return 2
	case agenttools.PolicyApprove:
		return 1
	default:
		// An empty or unknown value means the catalog's own default.
		return policyRank(agenttools.DefaultPolicy)
	}
}

// clampPolicy keeps a stored policy from raising what the run was started
// with. The stored value is left on disk: a later run started with wider
// rights gets the chat back as it was.
func clampPolicy(stored, ceiling agenttools.Policy) agenttools.Policy {
	if policyRank(stored) > policyRank(ceiling) {
		return ceiling
	}
	return stored
}

func (m *chatModel) loadTranscript() {
	snap, ok := m.store.get(m.chatID)
	if !ok {
		m.transcript = nil
		return
	}
	m.transcript = snap.Lines
	m.refreshView()
}

// record files a line under the chat it belongs to and puts it on screen only
// when that chat is the one being looked at.
//
// The pane is appended to rather than reloaded from the store, because it also
// holds lines the store never sees — the help text, the execution report — and
// reloading would wipe them the moment anything else was said.
func (m *chatModel) record(chatID string, role chatRole, content, toolName string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	m.store.appendLine(chatID, role, content, toolName)
	if chatID == m.chatID {
		m.append(chatLine{Role: role, Content: content, ToolName: toolName})
	}
}

// persist saves a chat and reports a failure to the user rather than losing it
// silently: a chat that stopped being written to disk is worth knowing about.
func (m *chatModel) persist(chatID string) {
	if err := m.store.persist(chatID); err != nil {
		m.status = "чат не сохранён: " + err.Error()
	}
}

// transient adds a line for this session only. The help text and the report of
// one run are worth reading and not worth keeping.
func (m *chatModel) transient(content string) {
	m.append(chatLine{Role: chatRoleSystem, Content: content})
}

func (m *chatModel) append(line chatLine) {
	if strings.TrimSpace(line.Content) == "" {
		return
	}
	m.transcript = append(m.transcript, line)
	m.refreshView()
}

func (m *chatModel) refreshView() {
	if m.view.Width <= 0 {
		return
	}
	m.view.SetContent(m.renderTranscript())
	m.view.GotoBottom()
}

// applySize recomputes the panes after a resize or a sidebar toggle.
func (m *chatModel) applySize() {
	m.view.Height = max(1, m.height-chatChromeHeight)
	m.view.Width = max(10, m.width-m.listWidth())
	m.input.SetWidth(max(10, m.width-4))
	m.input.SetHeight(chatInputHeight)
	m.refreshView()
}

func (m *chatModel) listWidth() int {
	if !m.showList {
		return 0
	}
	return chatListWidth + 1 // plus the border
}

// Init implements tea.Model.
func (m *chatModel) Init() tea.Cmd { return textarea.Blink }

// Update implements tea.Model.
func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.applySize()
		return m, nil

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.view, cmd = m.view.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)

	case approvalMsg:
		// Queued rather than assigned: a second run must not overwrite a
		// confirmation someone is still blocked on.
		m.approvals = append(m.approvals, msg)
		return m, nil

	case agentEventMsg:
		m.applyEvent(msg.chatID, msg.ev)
		return m, nil

	case agentStatusMsg:
		if msg.chatID == m.chatID {
			m.status = msg.message
		}
		return m, nil

	case turnDoneMsg:
		// A run can end while its confirmation is still queued — the context
		// was canceled, say. Nobody is waiting for that answer any more.
		m.dropApprovals(msg.chatID)
		if msg.err != nil {
			m.record(msg.chatID, chatRoleSystem, "Ошибка: "+msg.err.Error(), "")
		}
		if msg.chatID == m.chatID {
			m.status = "готово"
			if msg.err != nil {
				m.status = "ошибка"
			}
		}
		m.persist(msg.chatID)
		m.refreshChats()
		return m, nil
	}
	return m, nil
}

// applyEvent turns one milestone of a run into a line of its own chat.
func (m *chatModel) applyEvent(chatID string, ev agentloop.Event) {
	switch ev.Type {
	case agentloop.EventAssistantText:
		m.record(chatID, chatRoleAssistant, ev.Text, "")
	case agentloop.EventToolStart:
		m.record(chatID, chatRoleTool, ev.ToolName+" "+ev.ToolArgs, ev.ToolName)
	case agentloop.EventReport:
		// The report describes one run, not the conversation, so it is shown
		// without being kept — and only to whoever is looking at that chat.
		if chatID == m.chatID {
			m.transient(strings.TrimSpace(ev.Report))
		}
	case agentloop.EventWarning:
		m.record(chatID, chatRoleSystem, ev.Message, "")
	case agentloop.EventError:
		message := ev.Message
		if message == "" && ev.Err != nil {
			message = ev.Err.Error()
		}
		m.record(chatID, chatRoleSystem, "Ошибка: "+message, "")
	}
}

// dropApprovals discards the confirmations of a run that has ended. Their
// waiters are gone, so answering them would be answering nobody.
func (m *chatModel) dropApprovals(chatID string) {
	kept := m.approvals[:0]
	for _, a := range m.approvals {
		if a.chatID != chatID {
			kept = append(kept, a)
		}
	}
	m.approvals = kept
}

// currentApproval returns the confirmation the user is being asked about.
func (m *chatModel) currentApproval() *approvalMsg {
	if len(m.approvals) == 0 {
		return nil
	}
	return &m.approvals[0]
}

func (m *chatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.currentApproval() != nil {
		m.answerApproval(msg)
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		if m.running {
			m.cancelTurn()
			return m, nil
		}
		return m, tea.Quit
	case "esc":
		if m.running {
			m.cancelTurn()
		}
		return m, nil
	case "ctrl+b":
		m.toggleList()
		return m, nil
	case "tab":
		if !m.showList {
			return m, nil
		}
		if m.focus == focusInput {
			m.focus = focusList
			m.input.Blur()
		} else {
			m.focus = focusInput
			m.input.Focus()
		}
		return m, nil
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.view, cmd = m.view.Update(msg)
		return m, cmd
	}

	if m.focus == focusList {
		switch msg.String() {
		case "up", "k":
			m.listIdx = max(0, m.listIdx-1)
		case "down", "j":
			m.listIdx = min(len(m.chats)-1, m.listIdx+1)
		case "enter":
			if m.listIdx >= 0 && m.listIdx < len(m.chats) {
				m.switchChat(m.chats[m.listIdx].ID)
			}
		}
		return m, nil
	}

	if msg.String() == "up" {
		for i := len(m.transcript) - 1; i >= 0; i-- {
			if m.transcript[i].Role == chatRoleUser {
				m.input.SetValue(m.transcript[i].Content)
				m.input.CursorEnd()
				break
			}
		}
		return m, nil
	}

	if msg.String() == "enter" {
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.input.Reset()
		return m, m.submit(text)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// answerApproval hands the user's decision back to the waiting agent.
func (m *chatModel) answerApproval(msg tea.KeyMsg) {
	var (
		allowed bool
		label   string
	)
	switch strings.ToLower(msg.String()) {
	case "y", "д":
		allowed, label = true, "разрешено"
	case "n", "н", "esc":
		allowed, label = false, "отклонено"
	case "q", "й", "ctrl+c":
		// Refusing and canceling the run are different answers: the agent is
		// told no either way, and the run is then stopped.
		allowed, label = false, "прервано"
	default:
		return
	}

	current := *m.currentApproval()
	select {
	case current.reply <- allowed:
	default:
	}
	// The decision is filed in the chat that asked, not the one on screen.
	m.record(current.chatID, chatRoleSystem, "Согласование "+current.tool+": "+label, "")
	m.approvals = m.approvals[1:]
	if label == "прервано" {
		m.cancelChatRun(current.chatID)
	}
}

func (m *chatModel) cancelTurn() { m.cancelChatRun(m.chatID) }

func (m *chatModel) cancelChatRun(chatID string) {
	if m.store.cancelRun(chatID) && chatID == m.chatID {
		m.status = "отмена…"
	}
}

func (m *chatModel) toggleList() {
	m.showList = !m.showList
	if !m.showList && m.focus == focusList {
		m.focus = focusInput
		m.input.Focus()
	}
	m.applySize()
}

func (m *chatModel) switchChat(id string) {
	if id == "" || id == m.chatID {
		return
	}
	m.chatID = id
	m.refreshChats()
	m.loadTranscript()
	// The status line describes the chat on screen. Carrying over a step
	// number from the conversation just left would describe nothing.
	m.status = "готов"
	if m.running {
		m.status = "агент отвечает"
	}
}

// submit routes a submitted line to a slash command or to the agent.
func (m *chatModel) submit(text string) tea.Cmd {
	if strings.HasPrefix(text, "/") {
		return m.runSlashCommand(text)
	}
	return m.startTurn(text)
}

// startTurn records the question and launches the agent.
func (m *chatModel) startTurn(text string) tea.Cmd {
	busy, ok := m.store.appendUser(m.chatID, text)
	switch {
	case !ok && busy:
		m.status = "агент ещё отвечает на предыдущий вопрос"
		return nil
	case !ok:
		m.status = "чат не найден"
		return nil
	}
	m.persist(m.chatID)
	m.append(chatLine{Role: chatRoleUser, Content: text})

	runCtx, cancel := context.WithCancel(m.ctx)
	history, started := m.store.beginRun(m.chatID, cancel)
	if !started {
		cancel()
		m.status = "агент ещё отвечает на предыдущий вопрос"
		return nil
	}
	m.running = true
	m.status = "запуск агента…"

	bridge := &chatBridge{
		program:  m.program,
		store:    m.store,
		cfg:      m.cfg,
		client:   m.client,
		agentCfg: m.agentCfg,
		chatID:   m.chatID,
		policy:   m.policy,
	}
	go bridge.run(runCtx, history)
	m.refreshChats()
	return nil
}

const chatHelpText = `Команды:
  /help                        эта справка
  /new                         начать новый чат
  /chats                       показать или скрыть список чатов
  /mode readonly|approve|full  права агента в этом чате
  /rename ИМЯ                  переименовать чат
  /export [md|json]            выгрузить чат в файл
  /delete                      удалить текущий чат
  /quit                        выйти

Клавиши: Enter — отправить, ↑ — последний запрос, Tab — перейти к списку чатов,
Ctrl+B — показать или скрыть список, Esc — отменить запрос,
Ctrl+C — отменить запрос или выйти, PgUp и PgDn — прокрутка,
y, n, q — ответ на запрос согласования.`

func (m *chatModel) runSlashCommand(line string) tea.Cmd {
	fields := strings.Fields(line)
	args := fields[1:]

	switch strings.ToLower(fields[0]) {
	case "/help", "/?":
		m.transient(chatHelpText)
	case "/quit", "/exit":
		return tea.Quit
	case "/chats":
		m.toggleList()
	case "/new":
		snap := m.store.create(m.cfg.city, m.policy)
		m.persist(snap.ID)
		m.chatID = snap.ID
		m.refreshChats()
		m.loadTranscript()
		m.transient("Новый чат " + snap.ID)
	case "/mode":
		m.setPolicy(args)
	case "/rename":
		m.rename(args)
	case "/delete":
		m.deleteActive()
	case "/export":
		format := "md"
		if len(args) > 0 {
			format = strings.ToLower(args[0])
		}
		m.transient(m.exportActive(format))
	default:
		m.transient("Неизвестная команда " + fields[0] + " (/help)")
	}
	return nil
}

func (m *chatModel) setPolicy(args []string) {
	if len(args) == 0 {
		m.transient("Использование: /mode readonly|approve|full")
		return
	}
	policy, err := agenttools.ParsePolicy(strings.ToLower(args[0]))
	if err != nil {
		m.transient("Права должны быть readonly, approve или full")
		return
	}
	if policyRank(policy) > policyRank(m.ceiling) {
		m.transient("Запуск ограничен «-security " + string(m.ceiling) +
			"», расширить права в диалоге нельзя. Перезапустите «dzzzr chat -security " + string(policy) + "».")
		return
	}
	if _, ok := m.store.update(m.chatID, chatPatch{Policy: &policy}); !ok {
		m.transient("Чат не найден")
		return
	}
	m.persist(m.chatID)
	m.refreshChats()
	m.transient("Права чата: " + string(policy))
}

func (m *chatModel) rename(args []string) {
	title := strings.TrimSpace(strings.Join(args, " "))
	if title == "" {
		m.transient("Использование: /rename ИМЯ")
		return
	}
	if _, ok := m.store.update(m.chatID, chatPatch{Title: &title}); !ok {
		m.transient("Чат не найден")
		return
	}
	m.persist(m.chatID)
	m.refreshChats()
}

func (m *chatModel) deleteActive() {
	id := m.chatID
	if !m.store.remove(id) {
		m.transient("Чат не найден")
		return
	}
	m.store.removePersisted(id)
	m.chatID = ""
	m.openMostRecent(m.policy)
	m.transient("Чат " + id + " удалён")
}

// exportActive writes the conversation to a file next to the working
// directory and returns the line to show for it.
func (m *chatModel) exportActive(format string) string {
	snap, ok := m.store.get(m.chatID)
	if !ok {
		return "Чат не найден"
	}
	var (
		path string
		data []byte
	)
	switch format {
	case "json":
		path = "chat-" + snap.ID + ".json"
		encoded, err := json.Marshal(snap, jsontext.WithIndent("  "))
		if err != nil {
			return "Не удалось выгрузить чат: " + err.Error()
		}
		data = encoded
	case "md", "markdown":
		path = "chat-" + snap.ID + ".md"
		data = []byte(exportChatMarkdown(snap))
	default:
		return "Формат должен быть md или json"
	}
	// A chat carries level text and found codes, so it is written the way the
	// session file is. os.WriteFile would not do: it applies the mode only
	// when it creates the file, so exporting twice over an existing
	// world-readable chat-<id>.md would leave it readable.
	if err := writeSecretFile(path, data); err != nil {
		return "Не удалось выгрузить чат: " + err.Error()
	}
	return fmt.Sprintf("Чат выгружен в %s", path)
}
