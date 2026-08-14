package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sanitizeKeyPrefix reduces a key name to a filesystem-safe prefix:
// keeps [A-Za-z0-9_-], truncates to 8 chars, falls back to "key".
func sanitizeKeyPrefix(keyName string) string {
	var b strings.Builder
	for _, r := range keyName {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	prefix := strings.Trim(b.String(), "-_")
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	if prefix == "" {
		return "key"
	}
	return prefix
}

// errorDumpFilename builds the dump filename: {status}-{YYYYMMDD}-{HHMMSS纳秒}-{keyPrefix}.txt.
// Nanosecond precision avoids collisions between concurrent requests.
func errorDumpFilename(status int, ts time.Time, prefix string) string {
	return fmt.Sprintf("%d-%s%09d-%s.txt", status, ts.Format("20060102-150405"), ts.Nanosecond(), prefix)
}

// writeErrorDump persists a non-retryable 4xx request/response pair to dir.
// Writes atomically (temp file + rename) so a partially-written dump is
// never observed mid-flight. Returns an error; the caller decides how to log it.
func writeErrorDump(dir string, ps *ProviderState, keyName, method, target string, status, round int, start time.Time, reqBody, respBody []byte, rectified bool) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("writeErrorDump: mkdir %s: %w", dir, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Time:      %s\n", start.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "Provider:  %s\n", ps.Name())
	fmt.Fprintf(&b, "Key:       %s\n", keyName)
	fmt.Fprintf(&b, "URL:       %s\n", target)
	fmt.Fprintf(&b, "Method:    %s\n", method)
	fmt.Fprintf(&b, "Status:    %d\n", status)
	fmt.Fprintf(&b, "Round:     %d\n", round)
	fmt.Fprintf(&b, "Duration:  %.1fs\n", time.Since(start).Seconds())
	if ps.ThinkingMode() == "rectify" {
		fmt.Fprintf(&b, "Rectifier: enabled (mapTo: %s)\n", ps.RectifyThinkingMapTo())
	} else {
		fmt.Fprintf(&b, "Rectifier: default\n")
	}
	fmt.Fprintf(&b, "Rectified: %t\n\n", rectified)

	fmt.Fprintf(&b, "--- Request Body ---\n")
	b.Write(reqBody)
	b.WriteString("\n\n--- Response Body ---\n")
	b.Write(respBody)
	b.WriteString("\n")

	path := filepath.Join(dir, errorDumpFilename(status, start, sanitizeKeyPrefix(keyName)))

	// Atomic write: temp file + rename (Windows rename fails if target exists).
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("writeErrorDump: write temp: %w", err)
	}
	_ = os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("writeErrorDump: rename: %w", err)
	}
	return nil
}