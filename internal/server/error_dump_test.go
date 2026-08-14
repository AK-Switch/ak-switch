//go:build unit

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizeKeyPrefix_KeepsAlnum(t *testing.T) {
	got := sanitizeKeyPrefix("联通-735s4pv")
	if got != "735s4pv" {
		t.Errorf("sanitizeKeyPrefix(联通-735s4pv) = %q, want 735s4pv", got)
	}
}

func TestSanitizeKeyPrefix_TruncatesTo8(t *testing.T) {
	got := sanitizeKeyPrefix("abcdefghijklmn")
	if len(got) > 8 {
		t.Errorf("sanitizeKeyPrefix long = %q, len > 8", got)
	}
}

func TestSanitizeKeyPrefix_EmptyFallback(t *testing.T) {
	if got := sanitizeKeyPrefix("联通 中文！！"); got != "key" {
		t.Errorf("sanitizeKeyPrefix(only-cjk) = %q, want key", got)
	}
}

func TestErrorDumpFilename_Format(t *testing.T) {
	ts := time.Date(2026, 8, 14, 20, 21, 48, 123456789, time.UTC)
	got := errorDumpFilename(400, ts, "735s4pv")
	if !strings.HasPrefix(got, "400-20260814-202148") || !strings.HasSuffix(got, "-735s4pv.txt") {
		t.Errorf("errorDumpFilename = %q, want prefix/suffix match", got)
	}
}

func TestWriteErrorDump_WritesFileWithBodies(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a"})
	ps.SetThinkingMode("rectify")
	ps.SetRectifyThinkingMapTo("auto")
	dir := t.TempDir()

	err := writeErrorDump(dir, ps, "key-a", "POST", "/v1/messages", 400, 2,
		time.Now(), []byte(`{"thinking":{"type":"adaptive"}}`), []byte(`{"error":"bad"}`), true)
	if err != nil {
		t.Fatalf("writeErrorDump: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 file in %s, got %v (err=%v)", dir, entries, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"Provider:  test",
		"Key:       key-a",
		"URL:       /v1/messages",
		"Method:    POST",
		"Status:    400",
		"Round:     2",
		`Rectifier: enabled (mapTo: auto)`,
		"Rectified: true",
		`{"thinking":{"type":"adaptive"}}`,
		`{"error":"bad"}`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("dump missing %q in:\n%s", want, content)
		}
	}
}
