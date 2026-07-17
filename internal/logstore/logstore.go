package logstore

import (
	"sync"
	"time"

	"akswitch/internal/utils"
)

// LogStore is a thread-safe, fixed-size log store for API usage logs.
type LogStore struct {
	mu       sync.Mutex
	logs     []utils.LogEntry
	maxLen   int
	OnAppend func(prevLen, newLen, maxLen int) // optional callback, called under lock
}

// New creates a LogStore with the given max size.
func New(maxLen int) *LogStore {
	return &LogStore{
		logs:   make([]utils.LogEntry, 0, maxLen),
		maxLen: maxLen,
	}
}

// Append adds an entry. The entry's Key is masked immediately before storing.
// If the store is full, the oldest entries are dropped in bulk (O(1) amortized).
func (ls *LogStore) Append(entry utils.LogEntry) {
	entry.Key = utils.MaskKey(entry.Key)
	ls.mu.Lock()
	defer ls.mu.Unlock()
	prevLen := len(ls.logs)
	ls.logs = append(ls.logs, entry)
	newLen := len(ls.logs)
	if newLen > ls.maxLen {
		ls.logs = ls.logs[newLen-ls.maxLen:]
		newLen = ls.maxLen
	}
	if ls.OnAppend != nil {
		ls.OnAppend(prevLen, newLen, ls.maxLen)
	}
}

// Len returns the current number of entries (thread-safe convenience).
func (ls *LogStore) Len() int {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return len(ls.logs)
}

// Snapshot returns a deep copy of all entries.
func (ls *LogStore) Snapshot() []utils.LogEntry {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	result := make([]utils.LogEntry, len(ls.logs))
	copy(result, ls.logs)
	return result
}

// SnapshotSince returns a deep copy of entries whose timestamp >= since.
// since is parsed as RFC3339. If since is empty or unparseable, returns all entries.
func (ls *LogStore) SnapshotSince(since string) []utils.LogEntry {
	sinceTime, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return ls.Snapshot()
	}

	ls.mu.Lock()
	defer ls.mu.Unlock()

	// Binary search: find first entry with timestamp >= sinceTime
	// entries are stored in insertion order, timestamps are monotonic.
	cut := len(ls.logs)
	lo, hi := 0, len(ls.logs)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		t, err := time.Parse(time.RFC3339, ls.logs[mid].Timestamp)
		if err != nil || !t.Before(sinceTime) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	cut = lo

	result := make([]utils.LogEntry, len(ls.logs)-cut)
	copy(result, ls.logs[cut:])
	return result
}

// Clear removes all entries.
func (ls *LogStore) Clear() {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.logs = make([]utils.LogEntry, 0, ls.maxLen)
}

