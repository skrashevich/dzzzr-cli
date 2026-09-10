package main

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/dzzzr-cli/agentloop"
	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// newTestChat builds a model over a private store, with no terminal attached.
func newTestChat(t *testing.T, policy agenttools.Policy, provider providers.LLMProvider) *chatModel {
	t.Helper()
	cfg := &config{city: "moscow", stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	store := newChatStore(t.TempDir())
	m := newChatModel(t.Context(), cfg, dzzzr.New("moscow"), store,
		agentloop.Config{Model: "fake", Provider: provider}, policy)
	// A size is what makes the panes real; without it nothing renders.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// key builds the key message a character produces.
func key(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// contentsOf joins the transcript so a test can look for a line in it.
func contentsOf(m *chatModel) string {
	var b strings.Builder
	for _, line := range m.transcript {
		b.WriteString(string(line.Role))
		b.WriteString(": ")
		b.WriteString(line.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func TestChatModelUpRecallsLastRequest(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyReadonly, nil)
	m.record(m.chatID, chatRoleUser, "первый вопрос", "")
	m.record(m.chatID, chatRoleUser, "последний вопрос", "")
	m.record(m.chatID, chatRoleAssistant, "ответ", "")
	m.input.SetValue("черновик")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.input.Value(); got != "последний вопрос" {
		t.Fatalf("поле ввода = %q", got)
	}
	m.Update(key("!"))
	if got := m.input.Value(); got != "последний вопрос!" {
		t.Errorf("курсор не в конце запроса: %q", got)
	}

	first := m.chatID
	m.submit("/new")
	m.input.SetValue("новый черновик")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.input.Value(); got != "новый черновик" {
		t.Errorf("пустой чат изменил ввод: %q", got)
	}
	m.switchChat(first)
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.input.Value(); got != "последний вопрос" {
		t.Errorf("запрос не восстановлен после переключения: %q", got)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.listIdx = 1
	m.input.SetValue("черновик")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.listIdx != 0 || m.input.Value() != "черновик" {
		t.Error("стрелка вверх в списке чатов изменила поле ввода или не переместила выделение")
	}
}

func TestChatModelOpensAChatOnStart(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyApprove, nil)
	if m.chatID == "" {
		t.Fatal("чат не открыт")
	}
	if len(m.chats) != 1 {
		t.Errorf("чатов в списке %d, ожидался 1", len(m.chats))
	}
	if m.policy != agenttools.PolicyApprove {
		t.Errorf("права чата = %q", m.policy)
	}
	if !m.ready {
		t.Error("модель не готова после сообщения о размере окна")
	}
}

func TestChatModelReopensTheMostRecentChat(t *testing.T) {
	dir := t.TempDir()
	store := newChatStore(dir)
	old := store.create("moscow", agenttools.PolicyFull)
	store.appendUser(old.ID, "старый вопрос")
	if err := store.persist(old.ID); err != nil {
		t.Fatal(err)
	}

	cfg := &config{city: "moscow", stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	fresh := newChatStore(dir)
	if err := fresh.loadFromDisk(quietf); err != nil {
		t.Fatal(err)
	}
	m := newChatModel(t.Context(), cfg, dzzzr.New("moscow"), fresh, agentloop.Config{Model: "fake"}, agenttools.PolicyFull)
	if m.chatID != old.ID {
		t.Errorf("открыт чат %q, ожидался сохранённый %q", m.chatID, old.ID)
	}
	if !strings.Contains(contentsOf(m), "старый вопрос") {
		t.Errorf("история не восстановлена:\n%s", contentsOf(m))
	}
}

func TestChatModelAppliesAgentEvents(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyReadonly, nil)

	m.Update(agentEventMsg{chatID: m.chatID, ev: agentloop.Event{Type: agentloop.EventToolStart, ToolName: "game_status", ToolArgs: "{}"}})
	m.Update(agentEventMsg{chatID: m.chatID, ev: agentloop.Event{Type: agentloop.EventAssistantText, Text: "Вы на четвёртом уровне."}})
	m.Update(agentEventMsg{chatID: m.chatID, ev: agentloop.Event{Type: agentloop.EventWarning, Message: "лимит близко"}})
	m.Update(agentEventMsg{chatID: m.chatID, ev: agentloop.Event{Type: agentloop.EventReport, Report: "\n--- Отчёт о выполнении ---\n"}})

	got := contentsOf(m)
	for _, want := range []string{
		"tool: game_status {}",
		"assistant: Вы на четвёртом уровне.",
		"system: лимит близко",
		"Отчёт о выполнении",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("в ленте нет %q:\n%s", want, got)
		}
	}

	// The report describes one run, not the conversation, so it is shown but
	// not kept.
	snap, _ := m.store.get(m.chatID)
	for _, line := range snap.Lines {
		if strings.Contains(line.Content, "Отчёт о выполнении") {
			t.Error("отчёт сохранён в истории чата")
		}
	}
}

func TestChatModelShowsAgentError(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyReadonly, nil)
	m.Update(agentEventMsg{chatID: m.chatID, ev: agentloop.Event{Type: agentloop.EventError, Message: "модель недоступна"}})
	if !strings.Contains(contentsOf(m), "Ошибка: модель недоступна") {
		t.Errorf("ошибка не показана:\n%s", contentsOf(m))
	}
}

func TestChatModelSlashMode(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyApprove, nil)

	m.submit("/mode readonly")
	if m.policy != agenttools.PolicyReadonly {
		t.Errorf("права чата = %q, ожидались readonly", m.policy)
	}
	snap, _ := m.store.get(m.chatID)
	if snap.Policy != agenttools.PolicyReadonly {
		t.Errorf("права не сохранены в чате: %q", snap.Policy)
	}

	m.submit("/mode немножко")
	if m.policy != agenttools.PolicyReadonly {
		t.Errorf("неизвестные права применены: %q", m.policy)
	}
	if !strings.Contains(contentsOf(m), "readonly, approve или full") {
		t.Errorf("подсказка о допустимых правах не показана:\n%s", contentsOf(m))
	}

	m.submit("/mode")
	if !strings.Contains(contentsOf(m), "Использование: /mode") {
		t.Errorf("подсказка об использовании не показана:\n%s", contentsOf(m))
	}
}

func TestChatModelSlashNewAndDelete(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyFull, nil)
	first := m.chatID

	m.submit("/new")
	if m.chatID == first {
		t.Fatal("/new не переключил на новый чат")
	}
	if len(m.chats) != 2 {
		t.Errorf("чатов %d, ожидалось 2", len(m.chats))
	}
	second := m.chatID

	m.submit("/delete")
	if m.chatID == second {
		t.Error("/delete не увёл с удалённого чата")
	}
	if len(m.store.list()) != 1 {
		t.Errorf("после удаления осталось %d чатов", len(m.store.list()))
	}
	if _, err := os.Stat(filepath.Join(m.store.dir, second+".json")); !os.IsNotExist(err) {
		t.Errorf("файл удалённого чата остался: %v", err)
	}
}

func TestChatModelSlashRename(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyFull, nil)
	m.submit("/rename Разбор четвёртого уровня")
	if m.title != "Разбор четвёртого уровня" {
		t.Errorf("заголовок = %q", m.title)
	}
	m.submit("/rename")
	if !strings.Contains(contentsOf(m), "Использование: /rename") {
		t.Errorf("подсказка не показана:\n%s", contentsOf(m))
	}
}

func TestChatModelSlashExport(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyFull, nil)
	m.store.appendUser(m.chatID, "что с уровнем")
	m.store.appendLine(m.chatID, chatRoleAssistant, "Вы на четвёртом.", "")
	m.loadTranscript()

	t.Chdir(t.TempDir())

	m.submit("/export")
	md, err := os.ReadFile("chat-" + m.chatID + ".md")
	if err != nil {
		t.Fatalf("markdown не записан: %v", err)
	}
	if !strings.Contains(string(md), "## Вы") || !strings.Contains(string(md), "Вы на четвёртом.") {
		t.Errorf("markdown не содержит переписку:\n%s", md)
	}

	m.submit("/export json")
	raw, err := os.ReadFile("chat-" + m.chatID + ".json")
	if err != nil {
		t.Fatalf("json не записан: %v", err)
	}
	var snap chatSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("выгрузка не разбирается (%v): %s", err, raw)
	}
	if len(snap.Lines) != 2 {
		t.Errorf("в выгрузке %d строк, ожидалось 2", len(snap.Lines))
	}

	m.submit("/export pdf")
	if !strings.Contains(contentsOf(m), "md или json") {
		t.Errorf("неизвестный формат принят:\n%s", contentsOf(m))
	}
}

func TestChatModelSlashUnknown(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyFull, nil)
	m.submit("/чегоизволите")
	if !strings.Contains(contentsOf(m), "Неизвестная команда") {
		t.Errorf("неизвестная команда принята:\n%s", contentsOf(m))
	}
}

func TestChatModelSlashHelpAndChats(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyFull, nil)
	m.submit("/help")
	if !strings.Contains(contentsOf(m), "/export") {
		t.Errorf("справка не показана:\n%s", contentsOf(m))
	}
	before := m.showList
	m.submit("/chats")
	if m.showList == before {
		t.Error("/chats не переключил список")
	}
}

func TestChatModelApprovalKeys(t *testing.T) {
	tests := []struct {
		key   string
		allow bool
		label string
	}{
		{"y", true, "разрешено"},
		{"n", false, "отклонено"},
		{"q", false, "прервано"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			m := newTestChat(t, agenttools.PolicyApprove, nil)
			reply := make(chan bool, 1)
			m.Update(approvalMsg{chatID: m.chatID, tool: "send_code", args: "(code=ЛУНА)", reply: reply})
			if m.currentApproval() == nil {
				t.Fatal("запрос согласования не показан")
			}
			if !strings.Contains(m.View(), "send_code") {
				t.Error("в блоке согласования нет имени инструмента")
			}

			m.Update(key(tc.key))
			select {
			case got := <-reply:
				if got != tc.allow {
					t.Errorf("ответ %v, ожидался %v", got, tc.allow)
				}
			case <-time.After(time.Second):
				t.Fatal("решение не отправлено агенту")
			}
			if m.currentApproval() != nil {
				t.Error("блок согласования не убран")
			}
			if !strings.Contains(contentsOf(m), tc.label) {
				t.Errorf("решение не записано в ленту:\n%s", contentsOf(m))
			}
		})
	}
}

func TestChatModelApprovalIgnoresOtherKeys(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyApprove, nil)
	reply := make(chan bool, 1)
	m.Update(approvalMsg{chatID: m.chatID, tool: "send_code", reply: reply})
	m.Update(key("z"))
	if m.currentApproval() == nil {
		t.Error("посторонняя клавиша убрала запрос согласования")
	}
	select {
	case <-reply:
		t.Error("посторонняя клавиша отправила решение")
	default:
	}
}

func TestChatModelRunsATurn(t *testing.T) {
	provider := &scriptedProvider{replies: []*providers.LLMResponse{answer("Уровень четвёртый.")}}
	m := newTestChat(t, agenttools.PolicyReadonly, provider)

	m.submit("что с уровнем")
	if !m.running {
		t.Fatal("ход не начат")
	}
	if _, ok := m.store.beginRun(m.chatID, nil); ok {
		t.Error("во время хода начался второй")
	}
	waitIdle(t, m)

	snap, _ := m.store.get(m.chatID)
	if snap.Running {
		t.Error("признак running не снят после хода")
	}
	history, _ := m.store.beginRun(m.chatID, nil)
	if len(history) != 2 || history[1].Content != "Уровень четвёртый." {
		t.Errorf("ответ модели не сохранён в истории: %+v", history)
	}
}

func TestChatModelRefusesSecondQuestionWhileRunning(t *testing.T) {
	provider := &scriptedProvider{replies: []*providers.LLMResponse{answer("готово")}}
	m := newTestChat(t, agenttools.PolicyReadonly, provider)
	m.store.beginRun(m.chatID, func() {})

	m.submit("вопрос во время хода")
	if !strings.Contains(m.status, "ещё отвечает") {
		t.Errorf("состояние = %q", m.status)
	}
	if len(m.transcript) != 0 {
		t.Errorf("вопрос попал в ленту во время хода: %+v", m.transcript)
	}
}

func TestChatModelEscapeCancelsTheTurn(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyReadonly, nil)
	canceled := make(chan struct{})
	m.store.beginRun(m.chatID, func() { close(canceled) })
	m.running = true

	m.Update(key("esc"))
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("Esc не отменил ход")
	}
	if !strings.Contains(m.status, "отмена") {
		t.Errorf("состояние = %q", m.status)
	}
}

func TestChatModelTurnDoneReportsFailure(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyReadonly, nil)
	m.running = true
	m.Update(turnDoneMsg{chatID: m.chatID, err: errUnavailable})
	if m.running {
		t.Error("признак работы не снят")
	}
	if !strings.Contains(contentsOf(m), "Ошибка: "+errUnavailable.Error()) {
		t.Errorf("ошибка хода не показана:\n%s", contentsOf(m))
	}
}

func TestChatModelViewRefusesTinyWindow(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyFull, nil)
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 5})
	if !strings.Contains(m.View(), "слишком мало") {
		t.Errorf("маленькое окно отрисовано как обычное:\n%s", m.View())
	}
}

func TestChatModelViewShowsHeaderAndStatus(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyApprove, nil)
	view := m.View()
	for _, want := range []string{"dzzzr chat", "город moscow", "права approve", "fake", "готов"} {
		if !strings.Contains(view, want) {
			t.Errorf("в экране нет %q:\n%s", want, view)
		}
	}
}

func TestChatModelListNavigation(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyFull, nil)
	first := m.chatID
	m.submit("/new")
	second := m.chatID

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != focusList {
		t.Fatal("Tab не перевёл фокус на список")
	}
	m.Update(key("j"))
	m.Update(key("enter"))
	if m.chatID == second {
		t.Errorf("переключение на другой чат не произошло: остался %q", m.chatID)
	}
	if m.chatID != first {
		t.Errorf("открыт чат %q, ожидался %q", m.chatID, first)
	}
}

func TestExportChatMarkdownCoversEveryRole(t *testing.T) {
	snap := chatSnapshot{
		Title: "Разбор", City: "moscow", Policy: agenttools.PolicyApprove,
		Lines: []chatLine{
			{Role: chatRoleUser, Content: "вопрос"},
			{Role: chatRoleAssistant, Content: "ответ"},
			{Role: chatRoleTool, Content: "game_status {}", ToolName: "game_status"},
			{Role: chatRoleSystem, Content: "заметка"},
		},
	}
	got := exportChatMarkdown(snap)
	for _, want := range []string{"# Разбор", "Город: moscow", "Права: approve", "## Вы", "вопрос", "## Агент", "ответ", "> game_status {}", "_заметка_"} {
		if !strings.Contains(got, want) {
			t.Errorf("в выгрузке нет %q:\n%s", want, got)
		}
	}
}

// waitIdle waits for the agent goroutine to release the chat.
func waitIdle(t *testing.T, m *chatModel) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if snap, ok := m.store.get(m.chatID); ok && !snap.Running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("ход агента не завершился")
}

// errUnavailable stands in for a provider that could not be reached.
var errUnavailable = errors.New("модель недоступна")

// The report and the help text are shown but never stored, so they must
// survive whatever is said next — reloading the pane from the store would
// wipe them.
func TestChatModelKeepsTransientLinesOnScreen(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyReadonly, nil)

	m.submit("/help")
	m.Update(agentEventMsg{chatID: m.chatID, ev: agentloop.Event{Type: agentloop.EventAssistantText, Text: "ответ"}})
	m.Update(agentEventMsg{chatID: m.chatID, ev: agentloop.Event{Type: agentloop.EventReport, Report: "--- Отчёт о выполнении ---"}})
	m.Update(turnDoneMsg{chatID: m.chatID})

	got := contentsOf(m)
	for _, want := range []string{"/export", "ответ", "Отчёт о выполнении"} {
		if !strings.Contains(got, want) {
			t.Errorf("строка %q пропала с экрана:\n%s", want, got)
		}
	}
}

func TestChatModelShowsTheQuestionItSent(t *testing.T) {
	provider := &scriptedProvider{replies: []*providers.LLMResponse{answer("ответ")}}
	m := newTestChat(t, agenttools.PolicyReadonly, provider)
	m.submit("/help")
	m.submit("что с уровнем")
	waitIdle(t, m)

	got := contentsOf(m)
	if !strings.Contains(got, "user: что с уровнем") {
		t.Errorf("вопрос не показан:\n%s", got)
	}
	if !strings.Contains(got, "/export") {
		t.Errorf("справка стёрта отправкой вопроса:\n%s", got)
	}
}

// A run can end while a confirmation is on screen; nobody is waiting for the
// answer then, so the box must give the input field back.
func TestChatModelClearsApprovalWhenTheTurnEnds(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyApprove, nil)
	m.running = true
	m.Update(approvalMsg{chatID: m.chatID, tool: "send_code", args: "(code=ЛУНА)", reply: make(chan bool, 1)})
	if m.currentApproval() == nil {
		t.Fatal("запрос согласования не показан")
	}

	m.Update(turnDoneMsg{chatID: m.chatID, err: errUnavailable})
	if m.currentApproval() != nil {
		t.Error("блок согласования остался после конца хода")
	}
	if !strings.Contains(m.View(), "Спросите агента") {
		t.Errorf("поле ввода не вернулось:\n%s", m.View())
	}
}

// A run outlives the user's attention: they can move to another conversation
// while the model is still answering. What the run produces must be filed
// under its own chat, not under whichever one is on screen.
func TestChatModelFilesEventsUnderTheirOwnChat(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyReadonly, nil)
	background := m.chatID
	m.submit("/new")
	foreground := m.chatID
	if background == foreground {
		t.Fatal("второй чат не создан")
	}

	m.Update(agentEventMsg{chatID: background, ev: agentloop.Event{
		Type: agentloop.EventAssistantText, Text: "ответ фонового чата"}})
	m.Update(agentEventMsg{chatID: background, ev: agentloop.Event{
		Type: agentloop.EventToolStart, ToolName: "game_status", ToolArgs: "{}"}})
	m.Update(turnDoneMsg{chatID: background, err: errUnavailable})

	if got := contentsOf(m); strings.Contains(got, "ответ фонового чата") ||
		strings.Contains(got, "game_status") || strings.Contains(got, errUnavailable.Error()) {
		t.Errorf("чужой ход попал на экран текущего чата:\n%s", got)
	}
	if snap, _ := m.store.get(foreground); len(snap.Lines) != 0 {
		t.Errorf("чужой ход записан в текущий чат: %+v", snap.Lines)
	}

	snap, _ := m.store.get(background)
	var joined strings.Builder
	for _, l := range snap.Lines {
		joined.WriteString(l.Content + "\n")
	}
	for _, want := range []string{"ответ фонового чата", "game_status", errUnavailable.Error()} {
		if !strings.Contains(joined.String(), want) {
			t.Errorf("в фоновом чате нет %q:\n%s", want, joined.String())
		}
	}
}

// The status line belongs to the chat on screen; a background run must not
// take it over.
func TestChatModelIgnoresStatusOfOtherChats(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyReadonly, nil)
	m.status = "готов"
	m.Update(agentStatusMsg{chatID: "чужой", message: "Шаг 7: ожидание ответа модели…"})
	if m.status != "готов" {
		t.Errorf("состояние перебито чужим ходом: %q", m.status)
	}
	m.Update(agentStatusMsg{chatID: m.chatID, message: "Шаг 1"})
	if m.status != "Шаг 1" {
		t.Errorf("состояние своего хода не показано: %q", m.status)
	}
}

// Two chats can be waiting on the user at once; the second confirmation must
// queue behind the first instead of displacing it and stranding its goroutine.
func TestChatModelQueuesApprovals(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyApprove, nil)
	first := m.chatID
	m.submit("/new")
	second := m.chatID

	replyFirst := make(chan bool, 1)
	replySecond := make(chan bool, 1)
	m.Update(approvalMsg{chatID: first, tool: "send_code", args: "(code=ОДИН)", reply: replyFirst})
	m.Update(approvalMsg{chatID: second, tool: "send_code", args: "(code=ДВА)", reply: replySecond})

	if got := m.currentApproval(); got == nil || got.args != "(code=ОДИН)" {
		t.Fatalf("первым показан не первый запрос: %+v", got)
	}
	// The first belongs to a chat that is not on screen, so the box says which.
	if !strings.Contains(m.View(), "в чате") {
		t.Errorf("в блоке согласования не назван чужой чат:\n%s", m.View())
	}

	m.Update(key("y"))
	select {
	case got := <-replyFirst:
		if !got {
			t.Error("первый запрос отклонён вместо разрешения")
		}
	case <-time.After(time.Second):
		t.Fatal("ответ на первый запрос не отправлен")
	}
	if got := m.currentApproval(); got == nil || got.args != "(code=ДВА)" {
		t.Fatalf("второй запрос не показан после первого: %+v", got)
	}

	m.Update(key("n"))
	select {
	case got := <-replySecond:
		if got {
			t.Error("второй запрос разрешён вместо отклонения")
		}
	case <-time.After(time.Second):
		t.Fatal("ответ на второй запрос не отправлен")
	}
	if m.currentApproval() != nil {
		t.Error("очередь согласований не опустела")
	}
	// Each decision is written to the chat that asked.
	if snap, _ := m.store.get(first); !linesContain(snap.Lines, "разрешено") {
		t.Error("решение по первому запросу не записано в его чат")
	}
	if snap, _ := m.store.get(second); !linesContain(snap.Lines, "отклонено") {
		t.Error("решение по второму запросу не записано в его чат")
	}
}

func TestChatModelDropsApprovalsOfAFinishedRun(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyApprove, nil)
	other := "чужой-чат"
	m.Update(approvalMsg{chatID: other, tool: "send_code", reply: make(chan bool, 1)})
	m.Update(approvalMsg{chatID: m.chatID, tool: "take_hint", reply: make(chan bool, 1)})

	m.Update(turnDoneMsg{chatID: other})
	got := m.currentApproval()
	if got == nil || got.tool != "take_hint" {
		t.Errorf("после конца чужого хода остался не тот запрос: %+v", got)
	}
	m.Update(turnDoneMsg{chatID: m.chatID})
	if m.currentApproval() != nil {
		t.Error("запрос завершённого хода остался в очереди")
	}
}

// -security is a ceiling for the run: a chat saved as "full" must not hand
// those rights back in a run the user started read-only.
func TestChatModelClampsStoredPolicyToTheFlag(t *testing.T) {
	dir := t.TempDir()
	store := newChatStore(dir)
	snap := store.create("moscow", agenttools.PolicyFull)
	if err := store.persist(snap.ID); err != nil {
		t.Fatal(err)
	}

	fresh := newChatStore(dir)
	if err := fresh.loadFromDisk(quietf); err != nil {
		t.Fatal(err)
	}
	cfg := &config{city: "moscow", stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	m := newChatModel(t.Context(), cfg, dzzzr.New("moscow"), fresh,
		agentloop.Config{Model: "fake"}, agenttools.PolicyReadonly)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if m.policy != agenttools.PolicyReadonly {
		t.Errorf("действующие права %q, а запуск был readonly", m.policy)
	}
	if !strings.Contains(m.View(), "права readonly") {
		t.Errorf("в заголовке показаны не действующие права:\n%s", m.View())
	}

	// Raising the ceiling from inside the conversation is refused.
	m.submit("/mode full")
	if m.policy != agenttools.PolicyReadonly {
		t.Errorf("/mode расширил права до %q", m.policy)
	}
	if !strings.Contains(contentsOf(m), "-security") {
		t.Errorf("отказ не объясняет причину:\n%s", contentsOf(m))
	}
}

func TestClampPolicy(t *testing.T) {
	tests := []struct {
		stored, ceiling, want agenttools.Policy
	}{
		{agenttools.PolicyFull, agenttools.PolicyReadonly, agenttools.PolicyReadonly},
		{agenttools.PolicyFull, agenttools.PolicyApprove, agenttools.PolicyApprove},
		{agenttools.PolicyApprove, agenttools.PolicyFull, agenttools.PolicyApprove},
		{agenttools.PolicyReadonly, agenttools.PolicyFull, agenttools.PolicyReadonly},
		{agenttools.PolicyFull, agenttools.PolicyFull, agenttools.PolicyFull},
		{"", agenttools.PolicyReadonly, agenttools.PolicyReadonly},
	}
	for _, tc := range tests {
		if got := clampPolicy(tc.stored, tc.ceiling); got != tc.want {
			t.Errorf("clampPolicy(%q, %q) = %q, ожидалось %q", tc.stored, tc.ceiling, got, tc.want)
		}
	}
}

func linesContain(lines []chatLine, want string) bool {
	for _, l := range lines {
		if strings.Contains(l.Content, want) {
			return true
		}
	}
	return false
}

// A chat carries level text and found codes. os.WriteFile applies its mode
// only when it creates the file, so exporting over an existing world-readable
// file would leave it readable.
func TestChatModelExportResetsFilePermissions(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyFull, nil)
	m.store.appendUser(m.chatID, "код на щите ЛУНА")
	m.loadTranscript()
	t.Chdir(t.TempDir())

	path := "chat-" + m.chatID + ".md"
	if err := os.WriteFile(path, []byte("старая выгрузка"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.submit("/export")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("выгрузка не записана: %v", err)
	}
	if perm := info.Mode().Perm(); perm != sessionFilePerm {
		t.Errorf("права выгрузки %v, ожидались %v", perm, sessionFilePerm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ЛУНА") || strings.Contains(string(data), "старая выгрузка") {
		t.Errorf("файл не перезаписан свежей выгрузкой:\n%s", data)
	}
}

// A save that fails is worth telling the user about rather than losing the
// conversation quietly.
func TestChatModelReportsAFailedSave(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyFull, nil)
	// A file where the directory should be makes every write fail.
	blocked := filepath.Join(t.TempDir(), "chats")
	if err := os.WriteFile(blocked, []byte("не каталог"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.store.dir = blocked

	m.persist(m.chatID)
	if !strings.Contains(m.status, "не сохранён") {
		t.Errorf("неудачное сохранение не показано: %q", m.status)
	}
}

// The status line describes the chat on screen, so switching must not leave a
// step number from the conversation just left.
func TestChatModelClearsStatusOnSwitch(t *testing.T) {
	m := newTestChat(t, agenttools.PolicyReadonly, nil)
	first := m.chatID
	m.submit("/new")
	second := m.chatID

	m.Update(agentStatusMsg{chatID: second, message: "Шаг 7: ожидание ответа модели…"})
	if m.status != "Шаг 7: ожидание ответа модели…" {
		t.Fatalf("состояние своего хода не показано: %q", m.status)
	}

	m.switchChat(first)
	if strings.Contains(m.status, "Шаг 7") {
		t.Errorf("состояние чужого чата осталось после переключения: %q", m.status)
	}
	if m.status != "готов" {
		t.Errorf("состояние = %q, ожидалось «готов»", m.status)
	}
}
