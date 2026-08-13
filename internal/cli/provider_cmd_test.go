//go:build unit

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"akswitch/internal/config"
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
	flags := []string{"target", "port", "cooldown-sec", "max-retries", "default"}
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
func TestProviderInfoCmd_Exists(t *testing.T) {
	if providerInfoCmd == nil {
		t.Fatal("expected providerInfoCmd to be defined")
	}
}

func TestProviderUpdateCmd_Exists(t *testing.T) {
	if providerUpdateCmd == nil {
		t.Fatal("expected providerUpdateCmd to be defined")
	}
}

func TestProviderUpdateCmd_Flags(t *testing.T) {
	flags := []string{"target", "cooldown-sec", "max-retries",
		"backoff-cap-sec", "backoff-multiplier", "cb-reset-sec",
		"upstream-cb-threshold", "http-timeout-sec", "health-check-interval-sec",
		"admin-token", "disable-thinking", "genai-model", "keys-file", "default"}
	for _, f := range flags {
		t.Run(f, func(t *testing.T) {
			if providerUpdateCmd.Flags().Lookup(f) == nil {
				t.Fatalf("expected --%s flag on provider update command", f)
			}
		})
	}
}

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
func TestProviderUsageCmd_Exists(t *testing.T) {
	if usageCmd == nil {
		t.Fatal("expected usageCmd to be defined")
	}
}

func TestProviderUpstreamCBResetCmd_Exists(t *testing.T) {
	if providerUpstreamCBResetCmd == nil {
		t.Fatal("providerUpstreamCBResetCmd is nil")
	}
	if providerUpstreamCBResetCmd.Use != "upstream-cb-reset <name>" {
		t.Errorf("unexpected Use: %q", providerUpstreamCBResetCmd.Use)
	}
}

func TestKeyUpstreamCBResetCmd_Deprecated(t *testing.T) {
	if keyUpstreamCBResetCmd == nil {
		t.Fatal("keyUpstreamCBResetCmd should still exist as deprecated alias")
	}
	if keyUpstreamCBResetCmd.Use != "upstream-cb-reset <provider>" {
		t.Errorf("unexpected Use: %q", keyUpstreamCBResetCmd.Use)
	}
}

func TestProviderUpdateCmd_BackoffMultiplierRangeValidation(t *testing.T) {
	tmpDir := t.TempDir()

	// Save a config with one provider so the update command can find it
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{TargetBase: "http://localhost:11434"}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup save config: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	// hasCLIFlag/getCLIFlagValue read os.Args, not Cobra args.
	// Must include the full command path so Cobra routes correctly.
	origArgs := os.Args
	os.Args = []string{"akswitch", "provider", "update", "test", "--backoff-multiplier", "0.5"}
	defer func() { os.Args = origArgs }()

	cmd := providerUpdateCmd
	cmd.SetArgs([]string{"test", "--backoff-multiplier", "0.5"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for backoff_multiplier = 0.5, got nil")
	}
	if !strings.Contains(err.Error(), "must be >= 1") {
		t.Errorf("expected '>= 1' error, got: %v", err)
	}
}

func TestProviderUpdateCmd_ReadOnlyGuard(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{
				TargetBase: "http://localhost:11434",
				AdminToken: "oldtoken",
			}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup save config: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	origArgs := os.Args
	os.Args = []string{"akswitch", "provider", "update", "test", "--admin-token", "newtoken"}
	defer func() { os.Args = origArgs }()

	cmd := providerUpdateCmd
	cmd.SetArgs([]string{"test", "--admin-token", "newtoken"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for ReadOnly field admin_token, got nil")
	}
}

func TestProviderUpdateCmd_TargetEmptyCheckBeforePersist(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{
				TargetBase:  "http://localhost:11434",
				CooldownSec: 60,
			}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup save config: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	origArgs := os.Args
	os.Args = []string{"akswitch", "provider", "update", "test", "--cooldown-sec", "30", "--target", ""}
	defer func() { os.Args = origArgs }()

	cmd := providerUpdateCmd
	cmd.SetArgs([]string{"test", "--cooldown-sec", "30", "--target", ""})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for empty --target, got nil")
	}

	// Verify cooldown was NOT persisted (fail-fast before persist)
	loaded, _ := config.LoadTomlConfig(tomlPath)
	if loaded.Provider["test"].CooldownSec != 60 {
		t.Errorf("cooldown should not have changed (expected 60, got %d)", loaded.Provider["test"].CooldownSec)
	}
}

func TestProviderUpdateCmd_NonRuntimeEditableWarning(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{
				TargetBase:      "http://localhost:11434",
				DisableThinking: false,
			}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup save config: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	// hasCLIFlag/getCLIFlagValue scan os.Args; set the full command path
	origArgs := os.Args
	os.Args = []string{"akswitch", "provider", "update", "test", "--disable-thinking", "true"}
	defer func() { os.Args = origArgs }()

	// Call RunE directly to avoid Cobra command-tree args parsing issues
	// with boolean flags (--disable-thinking true is misparsed as 2 positional args)
	err := providerUpdateCmd.RunE(providerUpdateCmd, []string{"test"})
	if err != nil {
		t.Fatalf("expected no error for non-runtime-editable field, got: %v", err)
	}

	// Verify the value was persisted to TOML (warning only, not error)
	loaded, _ := config.LoadTomlConfig(tomlPath)
	if !loaded.Provider["test"].DisableThinking {
		t.Error("disable_thinking should have been persisted to TOML")
	}
}
