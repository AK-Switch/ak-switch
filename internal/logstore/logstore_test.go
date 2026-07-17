//go:build unit

package logstore

import (
	"akswitch/internal/utils"
	"testing"
)

func TestAppendAndSnapshot(t *testing.T) {
	s := New(100)
	entries := []utils.LogEntry{
		{Timestamp: "2025-01-01T00:00:00Z", Key: "sk-real-key-12345", KeyIndex: 1, Method: "POST", URL: "https://example.com/v1/chat", Status: 200, RequestBodySize: 100},
		{Timestamp: "2025-01-01T00:00:01Z", Key: "another-key-67890", KeyIndex: 2, Method: "GET", URL: "https://example.com/v1/models", Status: 200, RequestBodySize: 0},
		{Timestamp: "2025-01-01T00:00:02Z", Key: "test-key-abcde", KeyIndex: 3, Method: "POST", URL: "https://example.com/v1/completions", Status: 400, RequestBodySize: 50},
	}

	for _, e := range entries {
		s.Append(e)
	}

	if s.Len() != 3 {
		t.Errorf("expected Len() = 3, got %d", s.Len())
	}

	snap := s.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected Snapshot len = 3, got %d", len(snap))
	}

	// Check all fields match (Key will be masked)
	expectedKeys := []string{
		utils.MaskKey("sk-real-key-12345"),
		utils.MaskKey("another-key-67890"),
		utils.MaskKey("test-key-abcde"),
	}

	for i, got := range snap {
		if got.Key != expectedKeys[i] {
			t.Errorf("entry[%d] Key = %q, want %q", i, got.Key, expectedKeys[i])
		}
		if got.Timestamp != entries[i].Timestamp {
			t.Errorf("entry[%d] Timestamp = %q, want %q", i, got.Timestamp, entries[i].Timestamp)
		}
		if got.KeyIndex != entries[i].KeyIndex {
			t.Errorf("entry[%d] KeyIndex = %d, want %d", i, got.KeyIndex, entries[i].KeyIndex)
		}
		if got.Method != entries[i].Method {
			t.Errorf("entry[%d] Method = %q, want %q", i, got.Method, entries[i].Method)
		}
		if got.URL != entries[i].URL {
			t.Errorf("entry[%d] URL = %q, want %q", i, got.URL, entries[i].URL)
		}
		if got.Status != entries[i].Status {
			t.Errorf("entry[%d] Status = %d, want %d", i, got.Status, entries[i].Status)
		}
		if got.RequestBodySize != entries[i].RequestBodySize {
			t.Errorf("entry[%d] RequestBodySize = %d, want %d", i, got.RequestBodySize, entries[i].RequestBodySize)
		}
	}
}

func TestFIFOLimit(t *testing.T) {
	s := New(3)
	for i := 0; i < 5; i++ {
		s.Append(utils.LogEntry{
			Timestamp:       "entry",
			Key:             "key",
			KeyIndex:        i + 1,
			Method:          "GET",
			URL:             "/",
			Status:          200,
			RequestBodySize: 0,
		})
	}

	if s.Len() != 3 {
		t.Errorf("expected Len() = 3, got %d", s.Len())
	}

	snap := s.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected Snapshot len = 3, got %d", len(snap))
	}

	// Should contain only the last 3 entries (indices 2, 3, 4 — 0-indexed)
	for i, entry := range snap {
		expectedIndex := i + 3 // we appended 0-4, so last 3 are 2,3,4 but KeyIndex is 1-based: 3,4,5
		if entry.KeyIndex != expectedIndex {
			t.Errorf("entry[%d] KeyIndex = %d, want %d", i, entry.KeyIndex, expectedIndex)
		}
	}
}

func TestClear(t *testing.T) {
	s := New(10)
	for i := 0; i < 3; i++ {
		s.Append(utils.LogEntry{
			Timestamp:       "entry",
			Key:             "key",
			KeyIndex:        i + 1,
			Method:          "GET",
			URL:             "/",
			Status:          200,
			RequestBodySize: 0,
		})
	}

	if s.Len() != 3 {
		t.Fatalf("expected Len() = 3 before Clear, got %d", s.Len())
	}

	s.Clear()

	if s.Len() != 0 {
		t.Errorf("expected Len() = 0 after Clear, got %d", s.Len())
	}

	snap := s.Snapshot()
	if snap == nil {
		t.Errorf("expected Snapshot to be non-nil empty slice, got nil")
	}
	if len(snap) != 0 {
		t.Errorf("expected Snapshot to be empty, got %d entries", len(snap))
	}
}


func TestKeyMaskedOnAppend(t *testing.T) {
	s := New(10)
	rawKey := "sk-real-key-12345"
	expectedMasked := utils.MaskKey(rawKey)

	s.Append(utils.LogEntry{
		Timestamp:       "2025-01-01T00:00:00Z",
		Key:             rawKey,
		KeyIndex:        1,
		Method:          "POST",
		URL:             "https://example.com/v1/chat",
		Status:          200,
		RequestBodySize: 100,
	})

	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}

	if snap[0].Key == rawKey {
		t.Errorf("Key was NOT masked — still contains raw key %q", rawKey)
	}

	if snap[0].Key != expectedMasked {
		t.Errorf("Key = %q, want masked value %q", snap[0].Key, expectedMasked)
	}

	// Verify the internal entry is also masked (access via Snapshot only)
	if snap[0].Key == rawKey || snap[0].Key == "sk-real-key-12345" {
		t.Error("Key appears unmasked in Snapshot; expected masking on Append")
	}
}

func TestSnapshotSince(t *testing.T) {
	s := New(100)
	entries := []utils.LogEntry{
		{Timestamp: "2025-01-01T00:00:00Z", Key: "k1", KeyIndex: 1, Method: "GET", URL: "/", Status: 200, RequestBodySize: 0},
		{Timestamp: "2025-01-01T00:00:01Z", Key: "k2", KeyIndex: 2, Method: "GET", URL: "/", Status: 200, RequestBodySize: 0},
		{Timestamp: "2025-01-01T00:00:02Z", Key: "k3", KeyIndex: 3, Method: "GET", URL: "/", Status: 200, RequestBodySize: 0},
		{Timestamp: "2025-01-01T00:00:03Z", Key: "k4", KeyIndex: 4, Method: "GET", URL: "/", Status: 200, RequestBodySize: 0},
	}
	for _, e := range entries {
		s.Append(e)
	}

	t.Run("since_before_first", func(t *testing.T) {
		snap := s.SnapshotSince("2024-12-31T23:59:59Z")
		if len(snap) != 4 {
			t.Fatalf("expected 4 entries, got %d", len(snap))
		}
	})

	t.Run("since_middle", func(t *testing.T) {
		snap := s.SnapshotSince("2025-01-01T00:00:01Z")
		if len(snap) != 3 {
			t.Fatalf("expected 3 entries (idx 1,2,3), got %d", len(snap))
		}
		if snap[0].KeyIndex != 2 {
			t.Errorf("first entry KeyIndex = %d, want 2", snap[0].KeyIndex)
		}
	})

	t.Run("since_after_last", func(t *testing.T) {
		snap := s.SnapshotSince("2025-01-01T00:00:04Z")
		if len(snap) != 0 {
			t.Fatalf("expected 0 entries, got %d", len(snap))
		}
	})

	t.Run("since_exact_match", func(t *testing.T) {
		snap := s.SnapshotSince("2025-01-01T00:00:02Z")
		if len(snap) != 2 {
			t.Fatalf("expected 2 entries (idx 3,4), got %d", len(snap))
		}
		if snap[0].KeyIndex != 3 {
			t.Errorf("first entry KeyIndex = %d, want 3", snap[0].KeyIndex)
		}
	})

	t.Run("since_empty_fallback", func(t *testing.T) {
		snap := s.SnapshotSince("")
		if len(snap) != 4 {
			t.Fatalf("expected 4 entries on empty since, got %d", len(snap))
		}
	})

	t.Run("since_invalid_fallback", func(t *testing.T) {
		snap := s.SnapshotSince("not-a-timestamp")
		if len(snap) != 4 {
			t.Fatalf("expected 4 entries on invalid since, got %d", len(snap))
		}
	})
}

func TestOnAppendCallback(t *testing.T) {
	s := New(3)
	var calls []struct{ prev, next, max int }
	s.OnAppend = func(prev, next, max int) {
		calls = append(calls, struct{ prev, next, max int }{prev, next, max})
	}

	// Append 1 entry — no overflow
	s.Append(utils.LogEntry{Timestamp: "e1", Key: "k1", KeyIndex: 1, Method: "GET", URL: "/", Status: 200, RequestBodySize: 0})
	if len(calls) != 1 {
		t.Fatalf("expected 1 callback call, got %d", len(calls))
	}
	if calls[0].prev != 0 || calls[0].next != 1 || calls[0].max != 3 {
		t.Errorf("got callback(%d,%d,%d), want (0,1,3)", calls[0].prev, calls[0].next, calls[0].max)
	}

	// Append 2 more — still no overflow
	s.Append(utils.LogEntry{Timestamp: "e2", Key: "k2", KeyIndex: 2, Method: "GET", URL: "/", Status: 200, RequestBodySize: 0})
	s.Append(utils.LogEntry{Timestamp: "e3", Key: "k3", KeyIndex: 3, Method: "GET", URL: "/", Status: 200, RequestBodySize: 0})
	if len(calls) != 3 {
		t.Fatalf("expected 3 callback calls, got %d", len(calls))
	}
	if calls[2].prev != 2 || calls[2].next != 3 || calls[2].max != 3 {
		t.Errorf("got callback(%d,%d,%d), want (2,3,3)", calls[2].prev, calls[2].next, calls[2].max)
	}

	// Append 4th entry — overflow (drop oldest)
	s.Append(utils.LogEntry{Timestamp: "e4", Key: "k4", KeyIndex: 4, Method: "GET", URL: "/", Status: 200, RequestBodySize: 0})
	if len(calls) != 4 {
		t.Fatalf("expected 4 callback calls, got %d", len(calls))
	}
	// With maxLen=3, after append: prev=3, new entries would be 4, truncated to 3 → next=3
	// dropped = (prev+1) - next = 4-3 = 1
	if calls[3].prev != 3 || calls[3].next != 3 || calls[3].max != 3 {
		t.Errorf("overflow: got callback(%d,%d,%d), want (3,3,3)", calls[3].prev, calls[3].next, calls[3].max)
	}
	if s.Len() != 3 {
		t.Errorf("expected Len()=3 after overflow, got %d", s.Len())
	}

	// Verify OnAppend nil is safe
	s.OnAppend = nil
	s.Append(utils.LogEntry{Timestamp: "e5", Key: "k5", KeyIndex: 5, Method: "GET", URL: "/", Status: 200, RequestBodySize: 0})
	if len(calls) != 4 {
		t.Errorf("expected no extra callback calls after setting nil, got %d", len(calls))
	}
}
