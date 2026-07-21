//go:build unit

package cmd

import (
	"testing"

	"akswitch/internal/keypool"
	"github.com/spf13/cobra"
)

func TestKeyAddCmd_HasNameFlag(t *testing.T) {
	flag := keyAddCmd.Flags().Lookup("name")
	if flag == nil {
		t.Fatal("expected --name flag to be registered on key add command")
	}
}

func TestKeyAddCmd_HasInsecureStorageFlag(t *testing.T) {
	flag := keyAddCmd.Flags().Lookup("insecure-storage")
	if flag == nil {
		t.Fatal("expected --insecure-storage flag to be registered on key add command")
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

func TestKeyUpdateCmd_Exists(t *testing.T) {
	if keyUpdateCmd == nil {
		t.Fatal("expected keyUpdateCmd to be defined")
	}
}

func TestKeyUpdateCmd_HasNameFlag(t *testing.T) {
	flag := keyUpdateCmd.Flags().Lookup("name")
	if flag == nil {
		t.Fatal("expected --name flag to be registered on key update command")
	}
}

func TestKeyUpdateCmd_HasByNameFlag(t *testing.T) {
	flag := keyUpdateCmd.Flags().Lookup("by-name")
	if flag == nil {
		t.Fatal("expected --by-name flag to be registered on key update command")
	}
}

func TestKeyRenameCmd_Exists(t *testing.T) {
	if keyRenameCmd == nil {
		t.Fatal("expected keyRenameCmd to be defined")
	}
}

func TestKeyRenameCmd_HasByNameFlag(t *testing.T) {
	flag := keyRenameCmd.Flags().Lookup("by-name")
	if flag == nil {
		t.Fatal("expected --by-name flag to be registered on key rename command")
	}
}

func TestFindKeyIndexByName_Found(t *testing.T) {
	store := &keypool.KeyStore{
		Keys: []keypool.KeyEntry{
			{Key: "sk-111", Name: "alpha"},
			{Key: "sk-222", Name: "beta"},
			{Key: "sk-333", Name: "gamma"},
		},
	}
	idx, err := findKeyIndexByName(store, "beta")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if idx != 1 {
		t.Fatalf("expected index 1, got %d", idx)
	}
}

func TestFindKeyIndexByName_NotFound(t *testing.T) {
	store := &keypool.KeyStore{
		Keys: []keypool.KeyEntry{
			{Key: "sk-111", Name: "alpha"},
		},
	}
	_, err := findKeyIndexByName(store, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent name")
	}
}

func TestFindKeyIndexByName_Duplicate(t *testing.T) {
	store := &keypool.KeyStore{
		Keys: []keypool.KeyEntry{
			{Key: "sk-111", Name: "dup"},
			{Key: "sk-222", Name: "dup"},
		},
	}
	_, err := findKeyIndexByName(store, "dup")
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

// Parameterized test: all commands that accept a key index must have --by-name.
// This enforces the invariant that addKeyIndexFlags is called for every command.
func TestAllKeyIndexCommands_HaveByNameFlag(t *testing.T) {
	commands := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"keyRemove", keyRemoveCmd},
		{"keyDisable", keyDisableCmd},
		{"keyEnable", keyEnableCmd},
		{"keyUpdate", keyUpdateCmd},
		{"keyRename", keyRenameCmd},
	}
	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cmd == nil {
				t.Fatal("command is nil")
			}
			if tc.cmd.Flags().Lookup("by-name") == nil {
				t.Errorf("expected --by-name flag to be registered on %s command", tc.name)
			}
		})
	}
}