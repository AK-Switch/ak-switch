//go:build unit

package cmd

import (
	"testing"

	"akswitch/internal/keypool"
	"github.com/spf13/cobra"
)

func TestKeyAddCmd_Flags(t *testing.T) {
	flags := []string{"name", "insecure-storage"}
	for _, f := range flags {
		t.Run(f, func(t *testing.T) {
			if keyAddCmd.Flags().Lookup(f) == nil {
				t.Fatalf("expected --%s flag on key add command", f)
			}
		})
	}
}

func TestProviderAddCmd_Flags(t *testing.T) {
	flags := []string{"target", "port", "genai", "cooldown-sec", "max-retries", "default"}
	for _, f := range flags {
		t.Run(f, func(t *testing.T) {
			if providerAddCmd.Flags().Lookup(f) == nil {
				t.Fatalf("expected --%s flag on provider add command", f)
			}
		})
	}
}

func TestKeyUpdateCmd_Exists(t *testing.T) {
	if keyUpdateCmd == nil {
		t.Fatal("expected keyUpdateCmd to be defined")
	}
}

func TestKeyUpdateCmd_Flags(t *testing.T) {
	flags := []string{"name", "by-name"}
	for _, f := range flags {
		t.Run(f, func(t *testing.T) {
			if keyUpdateCmd.Flags().Lookup(f) == nil {
				t.Fatalf("expected --%s flag on key update command", f)
			}
		})
	}
}

func TestKeyRenameCmd_Exists(t *testing.T) {
	if keyRenameCmd == nil {
		t.Fatal("expected keyRenameCmd to be defined")
	}
}

func TestKeyRenameCmd_Flags(t *testing.T) {
	flags := []string{"by-name"}
	for _, f := range flags {
		t.Run(f, func(t *testing.T) {
			if keyRenameCmd.Flags().Lookup(f) == nil {
				t.Fatalf("expected --%s flag on key rename command", f)
			}
		})
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