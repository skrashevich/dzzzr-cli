package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/skrashevich/dzzzr-cli/agentloop"
	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// chatsDir returns ~/.config/dzzzr/chats.
func chatsDir() (string, error) {
	dir, err := sessionDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "chats"), nil
}

// storedChat is a conversation as it is written to disk.
type storedChat struct {
	ID        string              `json:"id"`
	Title     string              `json:"title"`
	City      string              `json:"city"`
	Policy    agenttools.Policy   `json:"policy"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
	Messages  []agentloop.Message `json:"messages"`
	Lines     []chatLine          `json:"lines"`
	Files     []chatFile          `json:"files,omitempty"`
}

// loadFromDisk restores the conversations saved by an earlier run.
//
// A file that cannot be read or parsed is skipped rather than fatal: one
// damaged chat must not take the other chats — or the game — with it.
func (s *chatStore) loadFromDisk(debugf func(string, ...any)) error {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			debugf("чат %s не прочитан: %v", e.Name(), err)
			continue
		}
		var sc storedChat
		if err := json.Unmarshal(data, &sc); err != nil || sc.ID == "" {
			debugf("чат %s пропущен: файл повреждён", e.Name())
			continue
		}
		// The identifier from the file becomes a file name again on the next
		// save, so it is checked rather than trusted: a hand-edited or
		// tampered chat must not be able to name a path of its choosing.
		if !validChatID(sc.ID) {
			debugf("чат %s пропущен: недопустимый идентификатор %q", e.Name(), sc.ID)
			continue
		}
		policy, err := agenttools.ParsePolicy(string(sc.Policy))
		if err != nil {
			policy = agenttools.DefaultPolicy
		}
		s.chats[sc.ID] = &chatThread{
			ID:        sc.ID,
			Title:     sc.Title,
			City:      sc.City,
			Policy:    policy,
			CreatedAt: sc.CreatedAt,
			UpdatedAt: sc.UpdatedAt,
			messages:  sc.Messages,
			lines:     sc.Lines,
			files:     sc.Files,
		}
	}
	return nil
}

// persist writes one conversation. A chat holds level text and the codes the
// team has found, so it is written the way the session file is: through a
// fresh temporary file, owner-readable only.
func (s *chatStore) persist(id string) error {
	s.mu.Lock()
	t, ok := s.chats[id]
	var sc storedChat
	if ok {
		sc = storedChat{
			ID:        t.ID,
			Title:     t.Title,
			City:      t.City,
			Policy:    t.Policy,
			CreatedAt: t.CreatedAt,
			UpdatedAt: t.UpdatedAt,
			Messages:  slices.Clone(t.messages),
			Lines:     slices.Clone(t.lines),
			Files:     slices.Clone(t.files),
		}
	}
	s.mu.Unlock()
	if !ok {
		return nil
	}

	if err := os.MkdirAll(s.dir, sessionDirPerm); err != nil {
		return fatal("не удалось создать каталог %s: %v", s.dir, err)
	}
	// MkdirAll leaves an existing directory alone, so its mode is set too.
	if err := os.Chmod(s.dir, sessionDirPerm); err != nil {
		return fatal("не удалось выставить права на %s: %v", s.dir, err)
	}
	data, err := json.Marshal(sc, jsontext.WithIndent("  "))
	if err != nil {
		return fatal("не удалось сохранить чат: %v", err)
	}
	return writeSecretFile(filepath.Join(s.dir, id+".json"), data)
}

// removePersisted deletes a conversation's file.
func (s *chatStore) removePersisted(id string) {
	_ = os.Remove(filepath.Join(s.dir, id+".json"))
}
