package deduplication

import (
	"fmt"
	"sync"
)

// Tracker prevents duplicate processing of the same PR to the same target branch.
type Tracker struct {
	mu        sync.RWMutex
	processed map[string]bool
}

// NewTracker creates a new deduplication tracker.
func NewTracker() *Tracker {
	return &Tracker{
		processed: make(map[string]bool),
	}
}

// ShouldProcess checks if a PR/branch combination should be processed.
// Returns true if this is the first time processing, false if already processed.
func (t *Tracker) ShouldProcess(prNumber int, targetBranch, headSHA string) bool {
	key := t.makeKey(prNumber, targetBranch, headSHA)

	t.mu.RLock()
	_, exists := t.processed[key]
	t.mu.RUnlock()

	return !exists
}

// MarkProcessed marks a PR/branch combination as processed.
func (t *Tracker) MarkProcessed(prNumber int, targetBranch, headSHA string) {
	key := t.makeKey(prNumber, targetBranch, headSHA)

	t.mu.Lock()
	t.processed[key] = true
	t.mu.Unlock()
}

// IsProcessed checks if a PR/branch combination has been processed.
func (t *Tracker) IsProcessed(prNumber int, targetBranch, headSHA string) bool {
	key := t.makeKey(prNumber, targetBranch, headSHA)

	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.processed[key]
}

// Clear removes all tracked entries.
func (t *Tracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.processed = make(map[string]bool)
}

// Count returns the number of tracked entries.
func (t *Tracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return len(t.processed)
}

// makeKey creates a unique key for tracking.
// Format: pr-{number}-{branch}-{sha}
func (t *Tracker) makeKey(prNumber int, targetBranch, headSHA string) string {
	return fmt.Sprintf("pr-%d-%s-%s", prNumber, targetBranch, headSHA)
}
