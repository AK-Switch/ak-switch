//go:build unit

package cmd

import (
	"strings"
	"testing"
)

func TestStartCmd_HasLogFormatFlag(t *testing.T) {
	// Verify the --log-format flag is registered on the start command
	flag := startCmd.Flags().Lookup("log-format")
	if flag == nil {
		t.Fatal("expected --log-format flag to be registered on start command")
	}
	if !strings.Contains(flag.Usage, "compact") {
		t.Errorf("--log-format flag usage should mention compact, got: %s", flag.Usage)
	}
	if flag.DefValue != "default" {
		t.Errorf("--log-format flag default should be \"default\", got: %q", flag.DefValue)
	}
}