//go:build unit

package cli

import (
	"testing"
)

func TestLogLevelCmd_Exists(t *testing.T) {
	if logLevelCmd == nil {
		t.Fatal("expected logLevelCmd to be defined")
	}
}

func TestLogLevelCmd_Use(t *testing.T) {
	if logLevelCmd.Use != "log-level [level]" {
		t.Fatalf("expected Use to be 'log-level [level]', got %q", logLevelCmd.Use)
	}
}

func TestLogLevelCmd_ValidArgs(t *testing.T) {
	if logLevelCmd.Args == nil {
		t.Fatal("expected logLevelCmd to have Args validator")
	}
}
