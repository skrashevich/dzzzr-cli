package statstore

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"sync"

	"github.com/skrashevich/dzzzr-cli/gamestats"
)

// Repository lets the browser reuse the agent tools without filesystem access.
type Repository interface {
	Create(string, []byte, *gamestats.Config) (*Log, error)
	Load(string) (*Log, error)
	Update(string, int, gamestats.Config) (*Log, error)
}

type Memory struct {
	mu   sync.Mutex
	logs map[string][]byte
}

func (s *Memory) Create(name string, data []byte, cfg *gamestats.Config) (*Log, error) {
	if len(data) > 32<<20 {
		return nil, fmt.Errorf("журнал превышает 32 МБ")
	}
	r, err := gamestats.Build(name, data, cfg)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return nil, err
	}
	l := &Log{ID: hex.EncodeToString(id), Name: name, Data: data, Config: r.Config, Revision: 1}
	s.mu.Lock()
	defer s.mu.Unlock()
	return l, s.save(l)
}
func (s *Memory) save(l *Log) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if s.logs == nil {
		s.logs = map[string][]byte{}
	}
	s.logs[l.ID] = b
	return nil
}
func (s *Memory) load(id string) (*Log, error) {
	b, ok := s.logs[id]
	if !ok {
		return nil, fmt.Errorf("журнал не загружен")
	}
	var l Log
	err := json.Unmarshal(b, &l)
	return &l, err
}
func (s *Memory) Load(id string) (*Log, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.load(id) }
func (s *Memory) Update(id string, revision int, cfg gamestats.Config) (*Log, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, err := s.load(id)
	if err != nil {
		return nil, err
	}
	if l.Revision != revision {
		return nil, fmt.Errorf("параметры изменились: перечитайте журнал")
	}
	r, err := gamestats.Build(l.Name, l.Data, &cfg)
	if err != nil {
		return nil, err
	}
	l.Config = r.Config
	l.Revision++
	return l, s.save(l)
}
