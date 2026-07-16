//go:build unit

package cmd

import (
	"testing"
)

func TestKeyAddCmd_HasNameFlag(t *testing.T) {
	flag := keyAddCmd.Flags().Lookup("name")
	if flag == nil {
		t.Fatal("expected --name flag to be registered on key add command")
	}
}

func TestProviderAddCmd_HasTargetFlag(t *testing.T) {
	flag := providerAddCmd.Flags().Lookup("target")
	if flag == nil {
		t.Fatal("expected --target flag to be registered on provider add command")
	}
}

func TestProviderAddCmd_HasPortFlag(t *testing.T) {
	flag := providerAddCmd.Flags().Lookup("port")
	if flag == nil {
		t.Fatal("expected --port flag to be registered on provider add command")
	}
}

func TestProviderAddCmd_HasGenaiFlag(t *testing.T) {
	flag := providerAddCmd.Flags().Lookup("genai")
	if flag == nil {
		t.Fatal("expected --genai flag to be registered on provider add command")
	}
}

func TestProviderAddCmd_HasCooldownSecFlag(t *testing.T) {
	flag := providerAddCmd.Flags().Lookup("cooldown-sec")
	if flag == nil {
		t.Fatal("expected --cooldown-sec flag to be registered on provider add command")
	}
}

func TestProviderAddCmd_HasMaxRetriesFlag(t *testing.T) {
	flag := providerAddCmd.Flags().Lookup("max-retries")
	if flag == nil {
		t.Fatal("expected --max-retries flag to be registered on provider add command")
	}
}

func TestProviderAddCmd_HasDefaultFlag(t *testing.T) {
	flag := providerAddCmd.Flags().Lookup("default")
	if flag == nil {
		t.Fatal("expected --default flag to be registered on provider add command")
	}
}