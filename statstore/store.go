// Package statstore keeps local game logs and versioned calculation settings.
package statstore

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/skrashevich/dzzzr-cli/gamestats"
)

var validID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Store struct {
	Root string
	mu   sync.Mutex
}
type Log struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Data     []byte           `json:"data"`
	Config   gamestats.Config `json:"cfg"`
	Revision int              `json:"revision"`
}

func (l *Log) Report() (*gamestats.Report, error) { return gamestats.Build(l.Name, l.Data, &l.Config) }

func (s *Store) Create(name string, data []byte, cfg *gamestats.Config) (*Log, error) {
	if len(data) > 32<<20 {
		return nil, fmt.Errorf("журнал превышает 32 МБ")
	}
	report, err := gamestats.Build(name, data, cfg)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return nil, err
	}
	l := &Log{ID: hex.EncodeToString(id), Name: filepath.Base(name), Data: data, Config: report.Config, Revision: 1}
	s.mu.Lock()
	defer s.mu.Unlock()
	return l, s.save(l)
}

func (s *Store) Load(id string) (*Log, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id)
}
func (s *Store) load(id string) (*Log, error) {
	if !validID.MatchString(id) {
		return nil, fmt.Errorf("неверный идентификатор журнала")
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := root.ReadFile(id + ".json")
	if err != nil {
		return nil, err
	}
	var l Log
	if err = json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// Update rejects stale UI/agent edits instead of overwriting another calculation.
func (s *Store) Update(id string, revision int, cfg gamestats.Config) (*Log, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, err := s.load(id)
	if err != nil {
		return nil, err
	}
	if revision != l.Revision {
		return nil, fmt.Errorf("параметры изменились (версия %d): перечитайте журнал и повторите изменение", l.Revision)
	}
	report, err := gamestats.Build(l.Name, l.Data, &cfg)
	if err != nil {
		return nil, err
	}
	l.Config = report.Config
	l.Revision++
	return l, s.save(l)
}
func (s *Store) save(l *Log) error {
	if err := os.MkdirAll(s.Root, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	tmp := l.ID + ".tmp"
	if err = root.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	defer root.Remove(tmp)
	return root.Rename(tmp, l.ID+".json")
}
