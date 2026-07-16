//go:build unit

package cmd

import (
	"testing"
)

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