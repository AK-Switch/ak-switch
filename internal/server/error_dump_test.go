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
		time.Now(), []byte(`{"thinking":{"type":"adaptive"}}`), []byte(`{"error":"bad"}`), true, 7)
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

func TestCleanErrorDumps_RemovesOldFiles(t *testing.T) {
	dir := t.TempDir()

	// Create an old file (10 days ago)
	oldPath := filepath.Join(dir, "old.txt")
	os.WriteFile(oldPath, []byte("old"), 0644)
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(oldPath, oldTime, oldTime)

	// Create a recent file (now)
	newPath := filepath.Join(dir, "new.txt")
	os.WriteFile(newPath, []byte("new"), 0644)

	cleanErrorDumps(dir, 7)

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old file should be removed, stat err: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new file should remain, stat err: %v", err)
	}
}

func TestCleanErrorDumps_ZeroMaxAge(t *testing.T) {
	dir := t.TempDir()

	// Create a file that is 3 days old (< 7 day default)
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("data"), 0644)
	oldTime := time.Now().AddDate(0, 0, -3)
	os.Chtimes(path, oldTime, oldTime)

	cleanErrorDumps(dir, 0)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("3-day-old file should not be removed with maxAge=0 (fallback to 7), stat err: %v", err)
	}
}

func TestCleanErrorDumps_NoDir(t *testing.T) {
	// Should not panic or error when dir doesn't exist
	cleanErrorDumps(filepath.Join(t.TempDir(), "nonexistent"), 7)
}

func TestCleanErrorDumps_SkipsSubdirs(t *testing.T) {
	dir := t.TempDir()

	subDir := filepath.Join(dir, "sub")
	os.MkdirAll(subDir, 0755)

	// Old file in subdir (should be skipped)
	oldSub := filepath.Join(subDir, "old.txt")
	os.WriteFile(oldSub, []byte("old"), 0644)
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(oldSub, oldTime, oldTime)

	cleanErrorDumps(dir, 7)

	// Subdir itself should still exist
	if _, err := os.Stat(subDir); err != nil {
		t.Errorf("subdir should not be removed, stat err: %v", err)
	}
	// File inside subdir should still exist
	if _, err := os.Stat(oldSub); err != nil {
		t.Errorf("file inside subdir should not be removed, stat err: %v", err)
	}
}
