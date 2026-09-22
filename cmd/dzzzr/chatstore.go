package main

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/skrashevich/dzzzr-cli/agentloop"
	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// chatRole labels a line of the transcript the user sees. It is wider than
// the model's own roles: a tool call and a note from the program are worth
// showing but are not part of what the model was told.
type chatRole string

const (
	chatRoleUser      chatRole = "user"
	chatRoleAssistant chatRole = "assistant"
	chatRoleTool      chatRole = "tool"
	chatRoleSystem    chatRole = "system"
)

// chatLine is one row of the transcript.
type chatLine struct {
	Role      chatRole  `json:"role"`
	Content   string    `json:"content"`
	ToolName  string    `json:"tool_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// chatFile is a file the agent wrote to DZZZR_FILES_ROOT during this chat. The
// browser offers it for download; Path stays server-side only, addressed by
// Name.
type chatFile struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Tool      string    `json:"tool"`
	CreatedAt time.Time `json:"created_at"`
}

// chatThread is one conversation: what the model was told, what the user saw,
// and whether a run is in flight.
type chatThread struct {
	ID        string
	Title     string
	City      string
	Policy    agenttools.Policy
	CreatedAt time.Time
	UpdatedAt time.Time

	messages []agentloop.Message
	lines    []chatLine
	files    []chatFile
	running  bool
	cancel   func()
}

// chatSnapshot is a copy of a thread taken under the store's lock, so a caller
// can read it without holding anything.
type chatSnapshot struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	City      string            `json:"city"`
	Policy    agenttools.Policy `json:"policy"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Lines     []chatLine        `json:"lines"`
	Files     []chatFile        `json:"files"`
	Running   bool              `json:"running"`
}

// chatStore holds every conversation of one run of the TUI.
type chatStore struct {
	mu    sync.Mutex
	chats map[string]*chatThread
	dir   string
}

// newChatStore builds an empty store that persists under dir.
func newChatStore(dir string) *chatStore {
	return &chatStore{chats: make(map[string]*chatThread), dir: dir}
}

// newChatID returns an identifier that is also a safe file name.
func newChatID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on any supported platform, and a chat
		// without an identifier could overwrite another one's file.
		panic("dzzzr: не удалось получить случайный идентификатор чата: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// validChatID reports whether an identifier is one newChatID could have
// produced, and therefore safe to put in a file name.
func validChatID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// create opens a conversation for one city under one policy.
func (s *chatStore) create(city string, policy agenttools.Policy) chatSnapshot {
	now := time.Now().UTC()
	t := &chatThread{
		ID:        newChatID(),
		Title:     "новый чат",
		City:      city,
		Policy:    policy,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chats[t.ID] = t
	return snapshotOf(t)
}

// list returns every conversation, most recently touched first.
func (s *chatStore) list() []chatSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]chatSnapshot, 0, len(s.chats))
	for _, t := range s.chats {
		out = append(out, snapshotOf(t))
	}
	slices.SortFunc(out, func(a, b chatSnapshot) int {
		if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
			return c
		}
		// Two chats touched in the same instant still need one order.
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// get returns one conversation.
func (s *chatStore) get(id string) (chatSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[id]
	if !ok {
		return chatSnapshot{}, false
	}
	return snapshotOf(t), true
}

// chatPatch is what update may change. A nil field is left alone.
type chatPatch struct {
	Title  *string
	Policy *agenttools.Policy
}

// update applies a patch and returns the conversation as it now stands.
func (s *chatStore) update(id string, patch chatPatch) (chatSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[id]
	if !ok {
		return chatSnapshot{}, false
	}
	if patch.Title != nil {
		t.Title = *patch.Title
	}
	if patch.Policy != nil {
		t.Policy = *patch.Policy
	}
	t.UpdatedAt = time.Now().UTC()
	return snapshotOf(t), true
}

// remove drops a conversation, canceling a run it may still have.
func (s *chatStore) remove(id string) bool {
	s.mu.Lock()
	t, ok := s.chats[id]
	if ok {
		delete(s.chats, id)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	if t.cancel != nil {
		t.cancel()
	}
	return true
}

// appendUser records the user's message in both histories and gives the chat
// its title. It refuses while a run is in flight, because the model is already
// answering the previous message.
func (s *chatStore) appendUser(id, content string) (busy, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, exists := s.chats[id]
	if !exists {
		return false, false
	}
	if t.running {
		return true, false
	}
	now := time.Now().UTC()
	// Only the model's copy carries the stamp; the transcript keeps what the
	// operator typed.
	t.messages = append(t.messages, agentloop.Message{Role: agentloop.RoleUser, Content: stampedUserMessage(content)})
	t.lines = append(t.lines, chatLine{Role: chatRoleUser, Content: content, CreatedAt: now})
	t.Title = autoTitle(t.Title, content)
	t.UpdatedAt = now
	return false, true
}

// appendLine records something the user should see that the model was not
// told: a tool call, a warning, the report.
func (s *chatStore) appendLine(id string, role chatRole, content, toolName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[id]
	if !ok {
		return
	}
	t.lines = append(t.lines, chatLine{
		Role: role, Content: content, ToolName: toolName, CreatedAt: time.Now().UTC(),
	})
	t.UpdatedAt = time.Now().UTC()
}

// recordFile notes a file the agent generated in this chat. It is idempotent
// on the absolute path: a tool re-reported across turns is stored once. The
// bool reports whether this call added it.
func (s *chatStore) recordFile(id string, f chatFile) (chatFile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[id]
	if !ok {
		return chatFile{}, false
	}
	for _, existing := range t.files {
		if existing.Path == f.Path {
			return existing, false
		}
	}
	t.files = append(t.files, f)
	t.UpdatedAt = time.Now().UTC()
	return f, true
}

// beginRun marks the conversation busy and hands out a copy of what the model
// has been told. It fails when a run is already in flight.
func (s *chatStore) beginRun(id string, cancel func()) ([]agentloop.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[id]
	if !ok || t.running {
		return nil, false
	}
	t.running = true
	t.cancel = cancel
	t.UpdatedAt = time.Now().UTC()
	return slices.Clone(t.messages), true
}

// finishRun stores the conversation the run produced and clears the busy mark.
// A nil history leaves the stored one alone, which is what a failed run wants.
func (s *chatStore) finishRun(id string, messages []agentloop.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[id]
	if !ok {
		return
	}
	if messages != nil {
		t.messages = messages
	}
	t.running = false
	t.cancel = nil
	t.UpdatedAt = time.Now().UTC()
}

// cancelRun stops a run in flight, reporting whether there was one.
func (s *chatStore) cancelRun(id string) bool {
	s.mu.Lock()
	t, ok := s.chats[id]
	var cancel func()
	if ok {
		cancel = t.cancel
	}
	s.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// cancelAll stops every run in flight. The user may have started one in a
// chat and moved to another, so leaving on quit has to reach all of them.
func (s *chatStore) cancelAll() {
	s.mu.Lock()
	cancels := make([]func(), 0, len(s.chats))
	for _, t := range s.chats {
		if t.cancel != nil {
			cancels = append(cancels, t.cancel)
		}
	}
	s.mu.Unlock()
	// The cancel functions run outside the lock: they wake goroutines that
	// come straight back to the store.
	for _, cancel := range cancels {
		cancel()
	}
}

// snapshotOf copies a thread. The caller must hold the store's lock.
func snapshotOf(t *chatThread) chatSnapshot {
	return chatSnapshot{
		ID:        t.ID,
		Title:     t.Title,
		City:      t.City,
		Policy:    t.Policy,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
		Lines:     slices.Clone(t.lines),
		Files:     slices.Clone(t.files),
		Running:   t.running,
	}
}

// autoTitle names a chat after the first thing asked of it, and leaves a title
// the user has already earned alone.
func autoTitle(current, content string) string {
	if current != "" && current != "новый чат" {
		return current
	}
	line, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	line = strings.TrimSpace(line)
	if line == "" {
		return current
	}
	const limit = 48
	runes := []rune(line)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return line
}
