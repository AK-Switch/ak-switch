//go:build unit

package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestStartCmd_Flags(t *testing.T) {
	tests := []struct {
		name     string
		cmd      *cobra.Command
		flag     string
		defValue string // expected default, empty to skip
		usage    string // expected substring in usage, empty to skip
	}{
		{name: "log-format", cmd: startCmd, flag: "log-format", defValue: "compact", usage: "compact"},
		{name: "provider", cmd: startCmd, flag: "provider"},
		{name: "all", cmd: startCmd, flag: "all", defValue: "false"},
		{name: "dev", cmd: startCmd, flag: "dev", defValue: "false"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flag := tc.cmd.Flags().Lookup(tc.flag)
			if flag == nil {
				t.Fatalf("expected --%s flag on %s command", tc.flag, tc.cmd.Name())
			}
			if tc.usage != "" && !strings.Contains(flag.Usage, tc.usage) {
				t.Errorf("--%s flag usage should mention %q, got: %s", tc.flag, tc.usage, flag.Usage)
			}
			if tc.defValue != "" && flag.DefValue != tc.defValue {
				t.Errorf("--%s flag default should be %q, got: %q", tc.flag, tc.defValue, flag.DefValue)
			}
		})
	}
}

func TestStopCmd_Flags(t *testing.T) {
	tests := []struct {
		name     string
		flag     string
		defValue string
	}{
		{name: "dev", flag: "dev", defValue: "false"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flag := stopCmd.Flags().Lookup(tc.flag)
			if flag == nil {
				t.Fatalf("expected --%s flag on stop command", tc.flag)
			}
			if tc.defValue != "" && flag.DefValue != tc.defValue {
				t.Errorf("--%s flag default should be %q, got: %q", tc.flag, tc.defValue, flag.DefValue)
			}
		})
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