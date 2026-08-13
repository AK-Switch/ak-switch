//go:build unit

package cli

import (
	"testing"

	"akswitch/internal/config"
)

func TestConfigInitCmd_Flags(t *testing.T) {
	flags := []string{"path"}
	for _, f := range flags {
		t.Run(f, func(t *testing.T) {
			if configInitCmd.Flags().Lookup(f) == nil {
				t.Fatalf("expected --%s flag on config init command", f)
			}
		})
	}
}

func TestConfigInitCmd_Exists(t *testing.T) {
	if configInitCmd == nil {
		t.Fatal("configInitCmd is nil")
	}
	if configInitCmd.Use != "init" {
		t.Errorf("expected Use 'init', got %q", configInitCmd.Use)
	}
}

func TestConfigViewCmd_Exists(t *testing.T) {
	if configViewCmd == nil {
		t.Fatal("configViewCmd is nil")
	}
	if configViewCmd.Use != "view" {
		t.Errorf("expected Use 'view', got %q", configViewCmd.Use)
	}
}

func TestConfigListCmd_Exists(t *testing.T) {
	if configListCmd == nil {
		t.Fatal("configListCmd is nil")
	}
	if configListCmd.Use != "list [provider]" {
		t.Errorf("expected Use 'list [provider]', got %q", configListCmd.Use)
	}
}

func TestConfigListCmd_HasAllFlag(t *testing.T) {
	if configListCmd.Flags().Lookup("all") == nil {
		t.Fatal("expected --all flag on config list command")
	}
}

func TestConfigGetCmd_HasAllFlag(t *testing.T) {
	if configGetCmd.Flags().Lookup("all") == nil {
		t.Fatal("expected --all flag on config get command")
	}
}

func TestConfigGetCmd_Exists(t *testing.T) {
	if configGetCmd == nil {
		t.Fatal("configGetCmd is nil")
	}
	if configGetCmd.Use != "get <key> [provider]" {
		t.Errorf("expected Use 'get <key> [provider]', got %q", configGetCmd.Use)
	}
}

func TestConfigSetCmd_HasAllFlag(t *testing.T) {
	if configSetCmd.Flags().Lookup("all") == nil {
		t.Fatal("expected --all flag on config set command")
	}
}

func TestConfigSetCmd_Exists(t *testing.T) {
	if configSetCmd == nil {
		t.Fatal("configSetCmd is nil")
	}
	if configSetCmd.Use != "set <key> <value> [provider]" {
		t.Errorf("expected Use 'set <key> <value> [provider]', got %q", configSetCmd.Use)
	}
}

func TestConfigSetCmd_HasRuntimeOnlyFlag(t *testing.T) {
	if configSetCmd.Flags().Lookup("runtime-only") == nil {
		t.Fatal("expected --runtime-only flag on config set command")
	}
}

func TestConfigSetCmd_NoPersistFlag(t *testing.T) {
	if configSetCmd.Flags().Lookup("persist") != nil {
		t.Fatal("--persist flag should be removed (replaced by --runtime-only)")
	}
}

func TestGetFieldValue_AdminTokenMasked(t *testing.T) {
	fd := config.FindField("admin_token")
	if fd == nil {
		t.Fatal("admin_token field not found")
	}

	// Set token
	tc := &config.TomlConfig{
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{AdminToken: "secret123"}},
		},
	}
	val, _ := getFieldValue(tc, "test", fd)
	masked := maskSensitiveValue(fd, val)
	if masked != "(set)" {
		t.Errorf("expected '(set)', got %q", masked)
	}

	// Empty token
	tc2 := &config.TomlConfig{
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{AdminToken: ""}},
		},
	}
	val2, _ := getFieldValue(tc2, "test", fd)
	masked2 := maskSensitiveValue(fd, val2)
	if masked2 != "(not set)" {
		t.Errorf("expected '(not set)', got %q", masked2)
	}

	// Non-sensitive field passes through unchanged
	fdInt := config.FindField("cooldown_sec")
	tc3 := &config.TomlConfig{
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{CooldownSec: 60}},
		},
	}
	val3, _ := getFieldValue(tc3, "test", fdInt)
	masked3 := maskSensitiveValue(fdInt, val3)
	if masked3 != "60" {
		t.Errorf("expected '60', got %q", masked3)
	}
}

func TestConfigGetCmd_AdminTokenMasked(t *testing.T) {
	if configGetCmd == nil {
		t.Fatal("configGetCmd is nil")
	}
	// Verify the command accepts admin_token as a valid key (FindField lookup)
	fd := config.FindField("admin_token")
	if fd == nil {
		t.Fatal("admin_token not registered in descriptors")
	}
	// Masking is applied via maskSensitiveValue in the print path —
	// getFieldValue returns raw token, but maskSensitiveValue masks it.
	rawVal, _ := getFieldValue(&config.TomlConfig{
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{AdminToken: "tok123"}},
		},
	}, "test", fd)
	masked := maskSensitiveValue(fd, rawVal)
	if masked == "tok123" {
		t.Error("admin_token should be masked, not printed raw")
	}
}
