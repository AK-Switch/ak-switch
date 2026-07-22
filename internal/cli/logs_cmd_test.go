//go:build unit

package cli

import (
	"strings"
	"testing"
)

func TestLogsCmd_HasSinceFlag(t *testing.T) {
	// Verify the --since flag is registered on the logs command
	flag := logsCmd.Flags().Lookup("since")
	if flag == nil {
		t.Fatal("expected --since flag to be registered on logs command")
	}
	if !strings.Contains(flag.Usage, "RFC3339") {
		t.Errorf("--since flag usage should mention RFC3339 format, got: %s", flag.Usage)
	}
	if flag.DefValue != "" {
		t.Errorf("--since flag default should be empty string, got: %q", flag.DefValue)
	}
}

func TestLogsCmd_HasLastFlag(t *testing.T) {
	// Verify the existing --last flag is still registered
	flag := logsCmd.Flags().Lookup("last")
	if flag == nil {
		t.Fatal("expected --last flag to be registered on logs command")
	}
}