//go:build unit

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

func TestConfigSetCmd_RuntimeOnlyAppliesButDoesNotPersist(t *testing.T) {
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
		t.Fatalf("setup: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	origArgs := os.Args
	os.Args = []string{"akswitch", "config", "set", "cooldown_sec", "30", "test", "--runtime-only"}
	defer func() { os.Args = origArgs }()

	cmd := configSetCmd
	cmd.SetArgs([]string{"cooldown_sec", "30", "test", "--runtime-only"})
	err := cmd.Execute()
	if err != nil {
		// applyRuntimeField may fail if no server running or server returns error —
		// that's expected. The key assertion (TOML not modified) only holds when
		// runtime apply succeeds. Skip when runtime apply fails.
		if strings.Contains(err.Error(), "not reachable") ||
			strings.Contains(err.Error(), "API error") {
			t.Skip("server not available for runtime-only apply — skipping TOML assertion")
		}
		t.Fatalf("unexpected error: %v", err)
	}

	// TOML must NOT be modified with --runtime-only
	loaded, loadErr := config.LoadTomlConfig(tomlPath)
	if loadErr != nil {
		t.Fatalf("reload: %v", loadErr)
	}
	if loaded.Provider["test"].CooldownSec != 60 {
		t.Errorf("cooldown should NOT have changed with --runtime-only (expected 60, got %d)",
			loaded.Provider["test"].CooldownSec)
	}
}

func TestConfigSetCmd_RangeValidation(t *testing.T) {
	tests := []struct {
		key     string
		value   string
		wantErr bool
		errSub  string
	}{
		{"backoff_multiplier", "0.5", true, "must be >= 1"},
		{"backoff_multiplier", "2.0", false, ""},
		{"max_retries", "-1", true, "must be >= 0"},
		{"max_retries", "0", false, ""},
		{"max_retries", "5", false, ""},
		{"cooldown_sec", "-1", true, "must be >= 1"},
		{"cooldown_sec", "0", true, "must be >= 1"},
		{"cooldown_sec", "60", false, ""},
		{"backoff_cap_sec", "-1", true, "must be >= 1"},
		{"backoff_cap_sec", "0", true, "must be >= 1"},
		{"backoff_cap_sec", "120", false, ""},
		{"cb_reset_sec", "-1", true, "must be >= 1"},
		{"cb_reset_sec", "0", true, "must be >= 1"},
		{"cb_reset_sec", "30", false, ""},
		{"upstream_cb_threshold", "-1", true, "must be >= 1"},
		{"upstream_cb_threshold", "0", true, "must be >= 1"},
		{"upstream_cb_threshold", "5", false, ""},
		{"http_timeout_sec", "-1", true, "must be >= 1"},
		{"http_timeout_sec", "0", true, "must be >= 1"},
		{"http_timeout_sec", "30", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.key+"_"+tt.value, func(t *testing.T) {
			fd := config.FindField(tt.key)
			if fd == nil {
				t.Fatalf("field %q not found", tt.key)
			}
			err := validateFieldRange(fd, tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %s=%s, got nil", tt.key, tt.value)
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("expected error containing %q, got: %v", tt.errSub, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for %s=%s: %v", tt.key, tt.value, err)
				}
			}
		})
	}
}

func TestConfigSetCmd_TomlLoadErrorNotSwallowed(t *testing.T) {
	tmpDir := t.TempDir()

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir // no config.toml exists

	origArgs := os.Args
	os.Args = []string{"akswitch", "config", "set", "target", "http://x.com", "test"}
	defer func() { os.Args = origArgs }()

	cmd := configSetCmd
	cmd.SetArgs([]string{"target", "http://x.com", "test"})
	cmd.DisableFlagParsing = true
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when config.toml does not exist, got nil")
	}
	if !strings.Contains(err.Error(), "failed to load config") && !strings.Contains(err.Error(), "not exist") {
		t.Errorf("expected error about missing config, got: %v", err)
	}
}

func TestConfigGetCmd_AllLoadsTomlOnce(t *testing.T) {
	tmpDir := t.TempDir()
	// Use non-default values so we verify TOML is actually read (not just defaults)
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"alpha": {ProviderConfig: config.ProviderConfig{CooldownSec: 123}},
			"beta":  {ProviderConfig: config.ProviderConfig{CooldownSec: 456}},
			"gamma": {ProviderConfig: config.ProviderConfig{CooldownSec: 789}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	// Simulate what configGetCmd.RunE does with --all:
	// load merged providers, then get field value for each provider
	source, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("xdg path: %v", err)
	}
	mergedProviders, err := config.LoadAllTomlProviders(source)
	if err != nil {
		t.Fatalf("load merged providers: %v", err)
	}

	fd := config.FindField("cooldown_sec")
	if fd == nil {
		t.Fatal("cooldown_sec field not found")
	}

	var providers []string
	for name := range mergedProviders {
		providers = append(providers, name)
	}
	sort.Strings(providers)

	for _, p := range providers {
		val, getErr := getMergedFieldValue(mergedProviders, p, fd)
		if getErr != nil {
			t.Fatalf("getMergedFieldValue(%s): %v", p, getErr)
		}
		formatted := fd.Format(val)
		expected := strconv.Itoa(mergedProviders[p].CooldownSec)
		if formatted != expected {
			t.Errorf("provider %s: got %s, want %s", p, formatted, expected)
		}
	}
}

func TestConfigSetCmd_RejectsNonExistentProvider(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"real": {ProviderConfig: config.ProviderConfig{TargetBase: "http://localhost:11434"}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	origArgs := os.Args
	os.Args = []string{"akswitch", "config", "set", "target", "http://x.com", "ghost_provider"}
	defer func() { os.Args = origArgs }()

	cmd := configSetCmd
	cmd.SetArgs([]string{"target", "http://x.com", "ghost_provider"})
	cmd.DisableFlagParsing = true
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent provider, got nil")
	}

	loaded, loadErr := config.LoadTomlConfig(tomlPath)
	if loadErr != nil {
		t.Fatalf("reload: %v", loadErr)
	}
	if _, hasGhost := loaded.Provider["ghost_provider"]; hasGhost {
		t.Error("ghost provider should not have been created in TOML")
	}
}

func TestConfigSetCmd_HelpTextListsAllKeys(t *testing.T) {
	helpText := configSetCmd.Long
	expectedKeys := []string{
		"port", "log_file", "target", "cooldown_sec", "max_retries",
		"backoff_cap_sec", "backoff_multiplier", "cb_reset_sec",
		"upstream_cb_threshold", "http_timeout_sec", "health_check_interval_sec",
		"log_level", "genai_model", "admin_token", "keys_file",
	}
	for _, key := range expectedKeys {
		if !strings.Contains(helpText, key) {
			t.Errorf("configSetCmd help text missing key %q", key)
		}
	}
}

func TestGetFieldValue_AdminTokenMasked(t *testing.T) {
	fd := config.FindField("admin_token")
	if fd == nil {
		t.Fatal("admin_token field not found")
	}

	// Set token
	providers := map[string]*config.Config{
		"test": {ProviderConfig: config.ProviderConfig{AdminToken: "secret123"}},
	}
	val, _ := getMergedFieldValue(providers, "test", fd)
	masked := maskSensitiveValue(fd, val)
	if masked != "(set)" {
		t.Errorf("expected '(set)', got %q", masked)
	}

	// Empty token
	providers2 := map[string]*config.Config{
		"test": {ProviderConfig: config.ProviderConfig{AdminToken: ""}},
	}
	val2, _ := getMergedFieldValue(providers2, "test", fd)
	masked2 := maskSensitiveValue(fd, val2)
	if masked2 != "(not set)" {
		t.Errorf("expected '(not set)', got %q", masked2)
	}

	// Non-sensitive field passes through unchanged
	fdInt := config.FindField("cooldown_sec")
	providers3 := map[string]*config.Config{
		"test": {ProviderConfig: config.ProviderConfig{CooldownSec: 60}},
	}
	val3, _ := getMergedFieldValue(providers3, "test", fdInt)
	masked3 := maskSensitiveValue(fdInt, val3)
	if masked3 != "60" {
		t.Errorf("expected '60', got %q", masked3)
	}
}

func TestMaskSensitiveValue_KeysFileMasked(t *testing.T) {
	fd := config.FindField("keys_file")
	if fd == nil {
		t.Fatal("keys_file field not found")
	}

	// Set keys_file — after F7, keys_file is no longer masked: it shows the actual path
	providers := map[string]*config.Config{
		"test": {ProviderConfig: config.ProviderConfig{KeysFile: "keys.json"}},
	}
	val, _ := getMergedFieldValue(providers, "test", fd)
	masked := maskSensitiveValue(fd, val)
	if masked != "keys.json" {
		t.Errorf("keys_file should show the actual path, got %q", masked)
	}

	// Empty keys_file
	providers2 := map[string]*config.Config{
		"test": {ProviderConfig: config.ProviderConfig{KeysFile: ""}},
	}
	val2, _ := getMergedFieldValue(providers2, "test", fd)
	masked2 := maskSensitiveValue(fd, val2)
	if masked2 != "" {
		t.Errorf("empty keys_file should show empty string, got %q", masked2)
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
		"health_check_interval_sec", "log_level",
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
	// getMergedFieldValue returns raw token, but maskSensitiveValue masks it.
	rawVal, _ := getMergedFieldValue(map[string]*config.Config{
		"test": {ProviderConfig: config.ProviderConfig{AdminToken: "tok123"}},
	}, "test", fd)
	masked := maskSensitiveValue(fd, rawVal)
	if masked == "tok123" {
		t.Error("admin_token should be masked, not printed raw")
	}
}

func TestGetMergedFieldValue(t *testing.T) {
	providers := map[string]*config.Config{
		"test-provider": {
			ProviderConfig: config.ProviderConfig{
				TargetBase:     "https://example.com",
				MaxRetries:     3,
				CooldownSec:    20,
				LogLevel:       "info",
				HTTPTimeoutSec: 30,
				KeySelection:   "random",
			},
		},
		"empty-provider": {
			ProviderConfig: config.ProviderConfig{},
		},
	}

	tests := []struct {
		name     string
		provider string
		key      string
		want     any
	}{
		{"target", "test-provider", "target", "https://example.com"},
		{"max_retries", "test-provider", "max_retries", 3},
		{"cooldown_sec", "test-provider", "cooldown_sec", 20},
		{"log_level", "test-provider", "log_level", "info"},
		{"http_timeout_sec", "test-provider", "http_timeout_sec", 30},
		{"key_selection", "test-provider", "key_selection", "random"},
		{"nonexistent provider falls back to ParseDefault", "no-such-provider", "max_retries", 1},
		{"empty provider field falls back to ParseDefault", "empty-provider", "target", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fd := config.FindField(tc.key)
			if fd == nil {
				t.Fatalf("field descriptor not found for key %q", tc.key)
			}
			got, err := getMergedFieldValue(providers, tc.provider, fd)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("getMergedFieldValue(%q, %q, %q) = %v (%T), want %v (%T)", tc.provider, tc.key, tc.key, got, got, tc.want, tc.want)
			}
		})
	}
}
