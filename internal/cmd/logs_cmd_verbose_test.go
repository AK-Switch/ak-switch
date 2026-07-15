//go:build unit

package cmd

import (
	"strings"
	"testing"
)

func TestLogsCmd_HasVerboseFlag(t *testing.T) {
	// Verify the --verbose flag is registered on the logs command
	flag := logsCmd.Flags().Lookup("verbose")
	if flag == nil {
		t.Fatal("expected --verbose flag to be registered on logs command")
	}
	if !strings.Contains(flag.Usage, "request details") {
		t.Errorf("--verbose flag usage should mention request details, got: %s", flag.Usage)
	}
	if flag.DefValue != "false" {
		t.Errorf("--verbose flag default should be false, got: %q", flag.DefValue)
	}
}