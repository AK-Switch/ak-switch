//go:build unit

package logentry

import (
	"encoding/json"
	"testing"
)

// ── MaskKey ─────────────────────────────────────────

func TestMaskKey_FullyMasked(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"empty", "", "****"},
		{"1 char", "a", "****"},
		{"exactly 12 chars", "123456789012", "****"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskKey(tt.key); got != tt.want {
				t.Errorf("MaskKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestMaskKey_PartiallyMasked(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"13 chars", "1234567890123", "1234...0123"},
		{"long key", "sk-1234567890abcdefghij", "sk-1...ghij"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskKey(tt.key); got != tt.want {
				t.Errorf("MaskKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// ── LogEntry JSON round-trip ─────────────────────────

func TestLogEntry_JSONRoundTrip(t *testing.T) {
	entry := LogEntry{
		Timestamp:       "2025-01-01T00:00:00Z",
		Key:             MaskKey("sk-1234567890abcdef"),
		KeyIndex:        0,
		KeyName:         "primary",
		Method:          "POST",
		URL:             "https://example.com/v1/chat",
		Status:          200,
		RequestBodySize: 128,
		DurationMs:      250,
		TtfbMs:          150,
		Retries:         1,
		Provider:        "openai",
		InputTokens:     100,
		OutputTokens:    200,
	}

	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got LogEntry
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Method != entry.Method {
		t.Errorf("Method = %q, want %q", got.Method, entry.Method)
	}
	if got.Status != entry.Status {
		t.Errorf("Status = %d, want %d", got.Status, entry.Status)
	}
	if got.DurationMs != entry.DurationMs {
		t.Errorf("DurationMs = %d, want %d", got.DurationMs, entry.DurationMs)
	}
}
