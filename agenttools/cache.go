package agenttools

import (
	"encoding/json"
	"sync"
	"time"
)

// readCache memoizes read-tool results for a short while.
//
// Within one agent turn the model typically asks for the same facts several
// times: the game state, then the level, then the state again to check
// itself. Each of those is a round trip to a slow PHP engine and the answers
// are identical. Only reads are cached, and any mutating call clears the whole
// cache: once a code is submitted the level, the codes and the log are all
// suspect.
type readCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]cacheEntry
	now     func() time.Time
}

type cacheEntry struct {
	value    any
	storedAt time.Time
}

func newReadCache(ttl time.Duration) *readCache {
	if ttl <= 0 {
		return nil
	}
	return &readCache{ttl: ttl, entries: map[string]cacheEntry{}, now: time.Now}
}

// cacheKey identifies one call; arguments are part of it.
func cacheKey(tool string, args map[string]any) (string, bool) {
	if len(args) == 0 {
		return tool, true
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return "", false
	}
	return tool + "|" + string(encoded), true
}

func (c *readCache) get(key string) (any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if c.now().Sub(e.storedAt) > c.ttl {
		delete(c.entries, key)
		return nil, false
	}
	return e.value, true
}

func (c *readCache) put(key string, value any) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{value: value, storedAt: c.now()}
}

func (c *readCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]cacheEntry{}
}
