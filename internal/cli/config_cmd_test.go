//go:build unit

package cli

import (
	"testing"
)

func TestConfigInitCmd_HasPathFlag(t *testing.T) {
	flag := configInitCmd.Flags().Lookup("path")
	if flag == nil {
		t.Fatal("expected --path flag to be registered on config init command")
	}
}