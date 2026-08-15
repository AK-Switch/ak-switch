package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// CrashLogDir is the subdirectory under the user's home/config dir for crash logs.
const CrashLogDir = ".akswitch"

// CrashLogFilename is the crash log file name.
const CrashLogFilename = "crash.log"

// defaultCrashLogPath returns the default crash log path (~/.akswitch/crash.log).
func defaultCrashLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return CrashLogFilename
	}
	return filepath.Join(home, CrashLogDir, CrashLogFilename)
}

// CrashRecover wraps a function with panic recovery that writes crash details
// to the crash log file and stderr. Returns the recovered value (nil if no panic).
//
// Usage in startServer:
//
//	defer CrashRecover("startServer")
func CrashRecover(context string) (recovered any) {
	recovered = recover()
	if recovered == nil {
		return nil
	}

	crashPath := defaultCrashLogPath()

	// Ensure dir exists
	if dir := filepath.Dir(crashPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "[CRASH] failed to create crash log directory %s: %v\n", dir, err)
		}
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	stack := debug.Stack()

	report := fmt.Sprintf("\n%s\n[CRASH] %s — %s\n%s\nMessage: %v\n\nStack:\n%s\n%s\n",
		"══════════════════════════════════════════════════",
		timestamp, context,
		"──────────────────────────────────────────────────",
		recovered,
		string(stack),
		"══════════════════════════════════════════════════\n",
	)

	// Append to crash log
	if f, err := os.OpenFile(crashPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		_, _ = f.WriteString(report)
		_ = f.Close()
	} else {
		fmt.Fprintf(os.Stderr, "[CRASH] failed to write crash log: %v\n", err)
	}

	// Also output to stderr
	fmt.Fprint(os.Stderr, report)

	return recovered
}

// SetupCrashLogDir ensures the crash log directory exists.
// Returns the crash log path.
func SetupCrashLogDir() string {
	path := defaultCrashLogPath()
	dir := filepath.Dir(path)
	if dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	return path
}

// ErrorLogDir is the subdirectory under the user's home/config dir for error dumps.
const ErrorLogDir = "errors"

// defaultErrorLogDir returns the default error dump directory
// (~/.akswitch/errors), matching CrashLogDir's home-based convention.
func defaultErrorLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ErrorLogDir
	}
	return filepath.Join(home, CrashLogDir, ErrorLogDir)
}

// SetupErrorLogDir ensures the error dump directory exists.
// Returns the directory path.
func SetupErrorLogDir(maxAgeDays int) string {
	dir := defaultErrorLogDir()
	_ = os.MkdirAll(dir, 0755)
	cleanErrorDumps(dir, maxAgeDays)
	return dir
}

// cleanErrorDumps removes error dump files older than maxAgeDays from dir.
// Best-effort: read/remove failures logged at warn level, not fatal.
func cleanErrorDumps(dir string, maxAgeDays int) {
	if maxAgeDays <= 0 {
		maxAgeDays = 7
	}
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn("failed to read error dump directory", "dir", dir, "error", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			slog.Warn("failed to get error dump entry info", "file", e.Name(), "error", err)
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				slog.Warn("failed to remove stale error dump", "file", e.Name(), "error", err)
			}
		}
	}
}
