//go:build unit

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"akswitch/internal/config"
)

func TestConfigSetCmd_AllUpdatesAllProviders(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"alpha": {ProviderConfig: config.ProviderConfig{CooldownSec: 60}},
			"beta":  {ProviderConfig: config.ProviderConfig{CooldownSec: 60}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Override ConfigDir to point to temp directory
	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	// Simulate what configSetCmd.RunE does with --all:
	// load providers, sort them, persist to each one individually
	source, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("xdg path: %v", err)
	}
	tcLoaded, _ := config.LoadTomlConfig(source)
	if tcLoaded == nil {
		t.Fatal("config not loaded")
	}

	var providerList []string
	for name := range tcLoaded.Provider {
		providerList = append(providerList, name)
	}
	sort.Strings(providerList)

	fd := config.FindField("cooldown_sec")
	if fd == nil {
		t.Fatal("cooldown_sec field not found")
	}
	val, parseErr := fd.Parse("30")
	if parseErr != nil {
		t.Fatalf("parse: %v", parseErr)
	}

	// Persist to each provider individually (this is what --all should do)
	for _, p := range providerList {
		if err := persistFieldToToml(p, fd, val); err != nil {
			t.Fatalf("persist %s: %v", p, err)
		}
	}

	// Reload and verify
	loaded, err := config.LoadTomlConfig(tomlPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.Provider["alpha"].CooldownSec != 30 {
		t.Errorf("alpha cooldown = %d, want 30", loaded.Provider["alpha"].CooldownSec)
	}
	if loaded.Provider["beta"].CooldownSec != 30 {
		t.Errorf("beta cooldown = %d, want 30", loaded.Provider["beta"].CooldownSec)
	}
	if _, hasGhost := loaded.Provider["all"]; hasGhost {
		t.Error("ghost provider 'all' should not exist")
	}
}

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

func TestConfigListCmd_NoArgsShowsFirstProvider(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"alpha": {ProviderConfig: config.ProviderConfig{TargetBase: "http://a.example.com"}},
			"beta":  {ProviderConfig: config.ProviderConfig{TargetBase: "http://b.example.com"}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup save config: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	// Capture os.Stdout since RunE uses fmt.Printf
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := configListCmd
	cmd.SetArgs([]string{})
	err := cmd.RunE(cmd, []string{})
	w.Close()
	os.Stdout = oldStdout

	var output bytes.Buffer
	output.ReadFrom(r)

	if err != nil {
		t.Fatalf("config list should not error when providers exist: %v", err)
	}

	out := output.String()
	if strings.Contains(out, "Provider: beta") {
		t.Error("config list with no args should show only the first provider, not all providers")
	}
	if !strings.Contains(out, "Provider: alpha") {
		t.Error("config list with no args should show the first provider")
	}
}

func TestConfigGetCmd_HelpTextListsAllKeys(t *testing.T) {
	helpText := configGetCmd.Long
	expectedKeys := []string{
		"http_timeout_sec", "max_retries", "cooldown_sec", "backoff_cap_sec",
		"backoff_multiplier", "cb_reset_sec", "upstream_cb_threshold",
		"health_check_interval_sec", "log_level", "disable_thinking",
		"genai_model", "admin_token", "keys_file",
		"port", "log_file",
	}
	for _, key := range expectedKeys {
		if !strings.Contains(helpText, key) {
			t.Errorf("configGetCmd help text missing key %q", key)
		}
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
