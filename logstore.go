package main

import (
	"sync"
)

// LogStore is a thread-safe, fixed-size log store for API usage logs.
type LogStore struct {
	mu      sync.Mutex
	entries []LogEntry
	maxSize int
}

// NewLogStore creates a LogStore with the given max size.
func NewLogStore(maxSize int) *LogStore {
	return &LogStore{
		entries: make([]LogEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// Append adds an entry. The entry's Key is masked immediately before storing.
// If the store is full, the oldest entry is removed (FIFO).
func (s *LogStore) Append(entry LogEntry) {
	entry.Key = maskKey(entry.Key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= s.maxSize {
		s.entries = s.entries[1:]
	}
	s.entries = append(s.entries, entry)
}

// Len returns the current number of entries (thread-safe convenience).
func (s *LogStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Snapshot returns a deep copy of all entries.
func (s *LogStore) Snapshot() []LogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]LogEntry, len(s.entries))
	copy(result, s.entries)
	return result
}

// Clear removes all entries.
func (s *LogStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make([]LogEntry, 0, s.maxSize)
}
