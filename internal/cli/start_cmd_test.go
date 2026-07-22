//go:build unit

package cli

import (
	"strings"
	"testing"
)

func TestStartCmd_HasLogFormatFlag(t *testing.T) {
	flag := startCmd.Flags().Lookup("log-format")
	if flag == nil {
		t.Fatal("expected --log-format flag to be registered on start command")
	}
	if !strings.Contains(flag.Usage, "compact") {
		t.Errorf("--log-format flag usage should mention compact, got: %s", flag.Usage)
	}
	if flag.DefValue != "compact" {
		t.Errorf("--log-format flag default should be \"compact\", got: %q", flag.DefValue)
	}
}

func TestStartCmd_HasProviderFlag(t *testing.T) {
	flag := startCmd.Flags().Lookup("provider")
	if flag == nil {
		t.Fatal("expected --provider flag to be registered on start command")
	}
	if flag.DefValue != "" {
		t.Errorf("--provider flag default should be empty, got: %q", flag.DefValue)
	}
}

func TestStartCmd_HasAllFlag(t *testing.T) {
	flag := startCmd.Flags().Lookup("all")
	if flag == nil {
		t.Fatal("expected --all flag to be registered on start command")
	}
	if flag.DefValue != "false" {
		t.Errorf("--all flag default should be false, got: %q", flag.DefValue)
	}
}

func TestStartCmd_HasDevFlag(t *testing.T) {
	flag := startCmd.Flags().Lookup("dev")
	if flag == nil {
		t.Fatal("expected --dev flag to be registered on start command")
	}
	if flag.DefValue != "false" {
		t.Errorf("--dev flag default should be false, got: %q", flag.DefValue)
	}
}

func TestStopCmd_HasDevFlag(t *testing.T) {
	flag := stopCmd.Flags().Lookup("dev")
	if flag == nil {
		t.Fatal("expected --dev flag to be registered on stop command")
	}
	if flag.DefValue != "false" {
		t.Errorf("--dev flag default should be false, got: %q", flag.DefValue)
	}
}

func TestPidFilePath_DevMode(t *testing.T) {
	path := pidFilePath(true)
	if !strings.Contains(path, "akswitch-dev.pid") {
		t.Errorf("dev mode pid path should contain akswitch-dev.pid, got: %s", path)
	}
}

func TestPidFilePath_NormalMode(t *testing.T) {
	path := pidFilePath(false)
	if !strings.Contains(path, "akswitch.pid") {
		t.Errorf("normal mode pid path should contain akswitch.pid, got: %s", path)
	}
	if strings.Contains(path, "akswitch-dev") {
		t.Errorf("normal mode pid path should not contain akswitch-dev, got: %s", path)
	}
}
