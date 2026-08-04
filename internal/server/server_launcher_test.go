package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerLauncher_PidFilePath(t *testing.T) {
	sl := &ServerLauncher{}
	path := sl.PidFilePath(false)
	if !strings.HasSuffix(path, "akswitch.pid") {
		t.Errorf("expected akswitch.pid suffix, got %s", path)
	}
}

func TestServerLauncher_PidFilePath_DevMode(t *testing.T) {
	sl := &ServerLauncher{}
	path := sl.PidFilePath(true)
	if !strings.HasSuffix(path, "akswitch-dev.pid") {
		t.Errorf("expected akswitch-dev.pid suffix, got %s", path)
	}
}

func TestServerLauncher_CheckPidFile_NonExistent(t *testing.T) {
	sl := &ServerLauncher{}
	running, pid := sl.checkPidFile("/nonexistent/pid")
	if running || pid != 0 {
		t.Errorf("non-existent file should return (false, 0), got (%v, %d)", running, pid)
	}
}

func TestServerLauncher_CheckPidFile_FakePID(t *testing.T) {
	sl := &ServerLauncher{}
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")
	if err := os.WriteFile(pidFile, []byte("99999\n"), 0644); err != nil {
		t.Fatalf("write PID file: %v", err)
	}
	running, pid := sl.checkPidFile(pidFile)
	if running {
		t.Errorf("PID 99999 should not be running, got running=%v", running)
	}
	if pid != 99999 {
		t.Errorf("expected pid=99999, got %d", pid)
	}
}

func TestNewServerLauncher(t *testing.T) {
	sl := NewServerLauncher("html", "provider1", "compact", "debug", true, false)
	if sl.providerFilter != "provider1" {
		t.Errorf("providerFilter = %q, want %q", sl.providerFilter, "provider1")
	}
	if !sl.startAll {
		t.Error("startAll should be true")
	}
	if sl.logFormat != "compact" {
		t.Errorf("logFormat = %q, want %q", sl.logFormat, "compact")
	}
	if sl.logLevel != "debug" {
		t.Errorf("logLevel = %q, want %q", sl.logLevel, "debug")
	}
	if sl.dashboardHTML != "html" {
		t.Errorf("dashboardHTML = %q, want %q", sl.dashboardHTML, "html")
	}
}