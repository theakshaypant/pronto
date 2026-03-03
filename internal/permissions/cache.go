package permissions

import (
	"sync"
)

// cache provides in-memory caching of permission levels.
// This prevents redundant API calls during a single action run.
type cache struct {
	mu    sync.RWMutex
	data  map[string]Level
}

// newCache creates a new permission cache.
func newCache() *cache {
	return &cache{
		data: make(map[string]Level),
	}
}

// get retrieves a cached permission level for a user.
func (c *cache) get(username string) (Level, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	level, found := c.data[username]
	return level, found
}

// set stores a permission level for a user.
func (c *cache) set(username string, level Level) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[username] = level
}

// clear removes all cached entries.
func (c *cache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[string]Level)
}
