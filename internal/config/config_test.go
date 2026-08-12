//go:build unit

package config

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"akswitch/internal/logentry"
)

// resetEnv cleans up all config-related env vars to prevent leakage between tests.
func resetEnv() {
	ResetConfigEnv()
}

func TestValidate_InvalidPort(t *testing.T) {
	tests := []struct {
		port int
		name string
	}{
		{0, "port 0"},
		{-1, "negative port"},
		{65536, "port too high"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Port = tt.port
			cfg.TargetBase = "https://example.com"
			cfg.Keys = []string{"nvapi-key1"}
			if err := cfg.Validate(); err == nil {
				t.Errorf("Validate() expected error for port %d, got nil", tt.port)
			}
		})
	}
}

func TestValidate_RequiredFields(t *testing.T) {
	tests := []struct {
		name   string
		modify func(cfg *Config)
	}{
		{name: "empty keys", modify: func(cfg *Config) { cfg.Keys = nil }},
		{name: "empty target base", modify: func(cfg *Config) { cfg.TargetBase = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Port = 8080
			cfg.TargetBase = "https://example.com"
			cfg.Keys = []string{"nvapi-key1"}
			tt.modify(cfg)
			if err := cfg.Validate(); err == nil {
				t.Error("Validate() expected error, got nil")
			}
		})
	}
}

func TestValidate_CircuitBreakerFields(t *testing.T) {
	tests := []struct {
		name   string
		modify func(cfg *Config)
	}{
		{name: "backoff cap too low", modify: func(cfg *Config) { cfg.BackoffCapSec = 10 }},
		{name: "backoff multiplier < 1", modify: func(cfg *Config) { cfg.BackoffMultiplier = 0.5 }},
		{name: "cb reset too low", modify: func(cfg *Config) { cfg.CBResetSec = 1 }},
		{name: "cb threshold too low", modify: func(cfg *Config) { cfg.UpstreamCBThreshold = 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Port = 8080
			cfg.TargetBase = "https://example.com"
			cfg.Keys = []string{"nvapi-key1"}
			tt.modify(cfg)
			if err := cfg.Validate(); err == nil {
				t.Error("Validate() expected error, got nil")
			}
		})
	}
}

func TestSanitized(t *testing.T) {
	cfg := &Config{ProviderConfig: ProviderConfig{Keys: []string{
		"nvapi-xiKMDpevXK60t6gLsGW1",
		"short",
		"nvapi-KXZ6a_5Mwcew7Ekd32DD85OaLVZu3Q",
	}}}
	s := cfg.Sanitized()

	// Original must be unchanged
	if len(cfg.Keys) != 3 {
		t.Fatal("original keys length changed")
	}
	if cfg.Keys[0] != "nvapi-xiKMDpevXK60t6gLsGW1" {
		t.Error("original key mutated")
	}

	// Sanitized copy: first 4 + "..." + last 4 chars (per logentry.MaskKey)
	if s.Keys[0] != "nvap...sGW1" {
		t.Errorf("sanitized Keys[0] = %q, want %q", s.Keys[0], "nvap...sGW1")
	}
	if s.Keys[1] != "****" {
		t.Errorf("short key masked to %q, want %q", s.Keys[1], "****")
	}
	if s.Keys[2] != "nvap...Zu3Q" {
		t.Errorf("sanitized Keys[2] = %q, want %q", s.Keys[2], "nvap...Zu3Q")
	}

	// Sanitized must not share underlying array with original
	s.Keys[0] = "tampered"
	if cfg.Keys[0] == "tampered" {
		t.Error("Sanitized() returned a view into original, not a copy")
	}
}

func TestSanitized_UsesUtilsMaskKey(t *testing.T) {
	key := "sk-abcdefghijklmn"
	cfg := &Config{ProviderConfig: ProviderConfig{Keys: []string{key}}}
	s := cfg.Sanitized()
	expected := logentry.MaskKey(key)
	if s.Keys[0] != expected {
		t.Errorf("Sanitized Keys[0] = %q, want %q (must match logentry.MaskKey)", s.Keys[0], expected)
	}
}

func TestConfig_HealthCheckDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.HealthCheckIntervalSec != 30 {
		t.Errorf("HealthCheckIntervalSec default = %d, want 30", cfg.HealthCheckIntervalSec)
	}
	if cfg.HealthCheckPath != "/health" {
		t.Errorf("HealthCheckPath default = %q, want %q", cfg.HealthCheckPath, "/health")
	}
	if cfg.HealthCheckTimeoutSec != 5 {
		t.Errorf("HealthCheckTimeoutSec default = %d, want 5", cfg.HealthCheckTimeoutSec)
	}
}

func TestConfig_HealthCheckIntervalTooSmall(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Port = 8080
	cfg.TargetBase = "https://example.com"
	cfg.Keys = []string{"nvapi-key1"}
	cfg.HealthCheckIntervalSec = 4
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() expected error for HealthCheckIntervalSec=4, got nil")
	}
}

func TestConfig_HTTPTimeoutSec_Default(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.HTTPTimeoutSec != 30 {
		t.Errorf("HTTPTimeoutSec default = %d, want 30", cfg.HTTPTimeoutSec)
	}
}

func TestConfig_HTTPTimeoutSec_TooSmall(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Port = 8080
	cfg.TargetBase = "https://example.com"
	cfg.Keys = []string{"nvapi-key1"}
	cfg.HTTPTimeoutSec = 0
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() expected error for HTTPTimeoutSec=0, got nil")
	}
}

func TestConfig_HTTPTimeoutSec_Valid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Port = 8080
	cfg.TargetBase = "https://example.com"
	cfg.Keys = []string{"nvapi-key1"}
	cfg.HTTPTimeoutSec = 15
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error for HTTPTimeoutSec=15: %v", err)
	}
}

func TestConfig_HealthCheckTimeoutTooSmall(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Port = 8080
	cfg.TargetBase = "https://example.com"
	cfg.Keys = []string{"nvapi-key1"}
	cfg.HealthCheckTimeoutSec = 0
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() expected error for HealthCheckTimeoutSec=0, got nil")
	}
}

// ============================================================
// mergeDefaults 测试
// ============================================================

func TestMergeDefaults_EmptyConfig(t *testing.T) {
	cfg := &Config{}
	cfg.mergeDefaults()
	def := DefaultConfig()

	if cfg.Port != def.Port {
		t.Errorf("Port = %d, want %d", cfg.Port, def.Port)
	}
	if cfg.Host != def.Host {
		t.Errorf("Host = %q, want %q", cfg.Host, def.Host)
	}
	if cfg.MaxRetries != def.MaxRetries {
		t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, def.MaxRetries)
	}
	if cfg.LogLevel != def.LogLevel {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, def.LogLevel)
	}
	if cfg.CooldownSec != def.CooldownSec {
		t.Errorf("CooldownSec = %d, want %d", cfg.CooldownSec, def.CooldownSec)
	}
	if cfg.HTTPTimeoutSec != def.HTTPTimeoutSec {
		t.Errorf("HTTPTimeoutSec = %d, want %d", cfg.HTTPTimeoutSec, def.HTTPTimeoutSec)
	}
	if cfg.KeysFile != def.KeysFile {
		t.Errorf("KeysFile = %q, want %q", cfg.KeysFile, def.KeysFile)
	}
	if cfg.BackoffCapSec != def.BackoffCapSec {
		t.Errorf("BackoffCapSec = %d, want %d", cfg.BackoffCapSec, def.BackoffCapSec)
	}
	if cfg.BackoffMultiplier != def.BackoffMultiplier {
		t.Errorf("BackoffMultiplier = %g, want %g", cfg.BackoffMultiplier, def.BackoffMultiplier)
	}
	if cfg.CBResetSec != def.CBResetSec {
		t.Errorf("CBResetSec = %d, want %d", cfg.CBResetSec, def.CBResetSec)
	}
	if cfg.UpstreamCBThreshold != def.UpstreamCBThreshold {
		t.Errorf("UpstreamCBThreshold = %d, want %d", cfg.UpstreamCBThreshold, def.UpstreamCBThreshold)
	}
	if cfg.HealthCheckIntervalSec != def.HealthCheckIntervalSec {
		t.Errorf("HealthCheckIntervalSec = %d, want %d", cfg.HealthCheckIntervalSec, def.HealthCheckIntervalSec)
	}
	if cfg.HealthCheckPath != def.HealthCheckPath {
		t.Errorf("HealthCheckPath = %q, want %q", cfg.HealthCheckPath, def.HealthCheckPath)
	}
	if cfg.HealthCheckTimeoutSec != def.HealthCheckTimeoutSec {
		t.Errorf("HealthCheckTimeoutSec = %d, want %d", cfg.HealthCheckTimeoutSec, def.HealthCheckTimeoutSec)
	}
	if cfg.LogMaxSize != def.LogMaxSize {
		t.Errorf("LogMaxSize = %d, want %d", cfg.LogMaxSize, def.LogMaxSize)
	}
	if cfg.LogMaxAge != def.LogMaxAge {
		t.Errorf("LogMaxAge = %d, want %d", cfg.LogMaxAge, def.LogMaxAge)
	}
}

func TestMergeDefaults_PreservesSetValues(t *testing.T) {
	cfg := &Config{ProviderConfig: ProviderConfig{
		Port:              9090,
		Host:              "0.0.0.0",
		CooldownSec:       45,
		MaxRetries:        7,
		BackoffCapSec:     300,
		BackoffMultiplier: 3.5,
	}}
	cfg.mergeDefaults()

	if cfg.Port != 9090 {
		t.Errorf("Port should be preserved, got %d", cfg.Port)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host should be preserved, got %q", cfg.Host)
	}
	if cfg.CooldownSec != 45 {
		t.Errorf("CooldownSec should be preserved, got %d", cfg.CooldownSec)
	}
	if cfg.MaxRetries != 7 {
		t.Errorf("MaxRetries should be preserved, got %d", cfg.MaxRetries)
	}
	if cfg.BackoffCapSec != 300 {
		t.Errorf("BackoffCapSec should be preserved, got %d", cfg.BackoffCapSec)
	}
	if cfg.BackoffMultiplier != 3.5 {
		t.Errorf("BackoffMultiplier should be preserved, got %g", cfg.BackoffMultiplier)
	}
	// Unset fields should still get defaults
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel should be default, got %q", cfg.LogLevel)
	}
}

func TestMergeDefaults_SkipsFieldsWithoutDefaultTag(t *testing.T) {
	cfg := &Config{ProviderConfig: ProviderConfig{
		TargetBase: "https://api.example.com",
		AdminToken: "my-token",
	}}
	cfg.mergeDefaults()

	// Fields without default tag should be preserved
	if cfg.TargetBase != "https://api.example.com" {
		t.Errorf("TargetBase should be preserved, got %q", cfg.TargetBase)
	}
	if cfg.AdminToken != "my-token" {
		t.Errorf("AdminToken should be preserved, got %q", cfg.AdminToken)
	}
}

// ============================================================
// TOML 配置测试
// ============================================================

func TestLoadAllTomlProviders_ExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	tomlPath := filepath.Join(tmpDir, "config.toml")
	content := `port = 9090

[provider.default]
target = "https://api.example.com"

cooldown_sec = 45
max_retries = 7

[provider.myapi]
target = "https://myapi.example.com"
`
	if err := os.WriteFile(tomlPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	providers, err := LoadAllTomlProviders(tomlPath)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders() unexpected error: %v", err)
	}
	cfg, ok := providers["myapi"]
	if !ok {
		t.Fatal("provider myapi not found in map")
	}
	if cfg.TargetBase != "https://myapi.example.com" {
		t.Errorf("TargetBase = %q, want %q", cfg.TargetBase, "https://myapi.example.com")
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want %d", cfg.Port, 9090)
	}
	if cfg.CooldownSec != 45 {
		t.Errorf("CooldownSec = %d, want %d", cfg.CooldownSec, 45)
	}
	if cfg.MaxRetries != 7 {
		t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, 7)
	}
}

func TestLoadToml_NotExist(t *testing.T) {
	_, err := LoadAllTomlProviders("/nonexistent/path/config.toml")
	if err == nil {
		t.Error("LoadAllTomlProviders() expected error for non-existent file, got nil")
	}
}

func TestLoadToml_Malformed(t *testing.T) {
	tmpDir := t.TempDir()
	tomlPath := filepath.Join(tmpDir, "bad.toml")
	if err := os.WriteFile(tomlPath, []byte("this is not toml {{"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAllTomlProviders(tomlPath)
	if err == nil {
		t.Error("LoadAllTomlProviders() expected error for malformed TOML, got nil")
	}
}

func TestSaveToml_LoadToml_Roundtrip(t *testing.T) {
	orig := DefaultConfig()
	orig.TargetBase = "https://api.example.com"
	orig.Port = 7070
	orig.CooldownSec = 30
	orig.MaxRetries = 5

	tmpDir := t.TempDir()
	tomlPath := filepath.Join(tmpDir, "roundtrip.toml")
	if err := SaveTomlConfig(&TomlConfig{Port: orig.Port, Provider: map[string]*Config{"myapi": orig}}, tomlPath); err != nil {
		t.Fatalf("SaveTomlConfig() error: %v", err)
	}

	providers, err := LoadAllTomlProviders(tomlPath)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders() error: %v", err)
	}
	loaded, ok := providers["myapi"]
	if !ok {
		t.Fatal("provider myapi not found in map")
	}

	if loaded.TargetBase != orig.TargetBase {
		t.Errorf("TargetBase = %q, want %q", loaded.TargetBase, orig.TargetBase)
	}
	if loaded.Port != orig.Port {
		t.Errorf("Port = %d, want %d", loaded.Port, orig.Port)
	}
	if loaded.CooldownSec != orig.CooldownSec {
		t.Errorf("CooldownSec = %d, want %d", loaded.CooldownSec, orig.CooldownSec)
	}
	if loaded.MaxRetries != orig.MaxRetries {
		t.Errorf("MaxRetries = %d, want %d", loaded.MaxRetries, orig.MaxRetries)
	}
}

func TestSaveToml_NoRuntimeConfigSection(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TargetBase = "https://api.example.com"
	cfg.Port = 7070
	cfg.Keys = []string{"test-key"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	tmpDir := t.TempDir()
	tomlPath := filepath.Join(tmpDir, "validate_save.toml")
	if err := SaveTomlConfig(&TomlConfig{Port: cfg.Port, Provider: map[string]*Config{"myapi": cfg}}, tomlPath); err != nil {
		t.Fatalf("SaveTomlConfig() error: %v", err)
	}

	data, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if strings.Contains(string(data), "RuntimeConfig") {
		t.Errorf("TOML should not contain 'RuntimeConfig' section, got:\n%s", string(data))
	}
}

func TestLoadToml_MissingFieldsUseDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	tomlPath := filepath.Join(tmpDir, "minimal.toml")
	content := `[provider.myapi]
target = "https://api.example.com"
`
	if err := os.WriteFile(tomlPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	providers, err := LoadAllTomlProviders(tomlPath)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders() unexpected error: %v", err)
	}
	cfg, ok := providers["myapi"]
	if !ok {
		t.Fatal("provider myapi not found in map")
	}
	// TargetBase should be set from TOML
	if cfg.TargetBase != "https://api.example.com" {
		t.Errorf("TargetBase = %q, want %q", cfg.TargetBase, "https://api.example.com")
	}
	// Port should use default from DefaultConfig
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want default 8080", cfg.Port)
	}
	// CooldownSec should use default from DefaultConfig
	if cfg.CooldownSec != 15 {
		t.Errorf("CooldownSec = %d, want default 60", cfg.CooldownSec)
	}
	// MaxRetries should use default from DefaultConfig
	if cfg.MaxRetries != 1 {
		t.Errorf("MaxRetries = %d, want default 1", cfg.MaxRetries)
	}
}

// ============================================================
// TomlProviderConfig 扩展字段测试
// ============================================================

func TestTomlProviderConfig_AllFields(t *testing.T) {
	content := `port = 7070

[provider.default]
target = "https://api.example.com"
cooldown_sec = 45
max_retries = 7
disable_thinking = true
genai_model = "opus-4.8"
log_level = "debug"
admin_token = "myadmintoken"
keys_file = "/data/keys.json"
backoff_cap_sec = 300
backoff_multiplier = 3.5
cb_reset_sec = 60
upstream_cb_threshold = 10
health_check_interval_sec = 15
http_timeout_sec = 45
log_file = "/var/log/akswitch.log"
log_max_size = 200
log_max_age = 30

[provider.myapi]
target = "https://myapi.example.com"
`
	path := writeTempToml(t, content)
	providers, err := LoadAllTomlProviders(path)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders() unexpected error: %v", err)
	}
	cfg, ok := providers["myapi"]
	if !ok {
		t.Fatal("provider myapi not found in map")
	}

	if cfg.TargetBase != "https://myapi.example.com" {
		t.Errorf("TargetBase = %q, want %q", cfg.TargetBase, "https://myapi.example.com")
	}
	if cfg.Port != 7070 {
		t.Errorf("Port = %d, want %d", cfg.Port, 7070)
	}
	if cfg.CooldownSec != 45 {
		t.Errorf("CooldownSec = %d, want %d", cfg.CooldownSec, 45)
	}
	if cfg.MaxRetries != 7 {
		t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, 7)
	}
	if !cfg.DisableThinking {
		t.Error("DisableThinking = false, want true")
	}
	if cfg.GenaiModel != "opus-4.8" {
		t.Errorf("GenaiModel = %q, want %q", cfg.GenaiModel, "opus-4.8")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.AdminToken != "myadmintoken" {
		t.Errorf("AdminToken = %q, want %q", cfg.AdminToken, "myadmintoken")
	}
	if cfg.KeysFile != "/data/keys.json" {
		t.Errorf("KeysFile = %q, want %q", cfg.KeysFile, "/data/keys.json")
	}
	if cfg.BackoffCapSec != 300 {
		t.Errorf("BackoffCapSec = %d, want %d", cfg.BackoffCapSec, 300)
	}
	if cfg.BackoffMultiplier != 3.5 {
		t.Errorf("BackoffMultiplier = %g, want %g", cfg.BackoffMultiplier, 3.5)
	}
	if cfg.CBResetSec != 60 {
		t.Errorf("CBResetSec = %d, want %d", cfg.CBResetSec, 60)
	}
	if cfg.UpstreamCBThreshold != 10 {
		t.Errorf("UpstreamCBThreshold = %d, want %d", cfg.UpstreamCBThreshold, 10)
	}
	if cfg.HealthCheckIntervalSec != 15 {
		t.Errorf("HealthCheckIntervalSec = %d, want %d", cfg.HealthCheckIntervalSec, 15)
	}
	if cfg.HTTPTimeoutSec != 45 {
		t.Errorf("HTTPTimeoutSec = %d, want %d", cfg.HTTPTimeoutSec, 45)
	}
	if cfg.LogFile != "/var/log/akswitch.log" {
		t.Errorf("LogFile = %q, want %q", cfg.LogFile, "/var/log/akswitch.log")
	}
	if cfg.LogMaxSize != 200 {
		t.Errorf("LogMaxSize = %d, want %d", cfg.LogMaxSize, 200)
	}
	if cfg.LogMaxAge != 30 {
		t.Errorf("LogMaxAge = %d, want %d", cfg.LogMaxAge, 30)
	}
}

func TestTomlProviderConfig_DefaultValues(t *testing.T) {
	content := `[provider.default]
target = "https://api.example.com"

[provider.myapi]
target = "https://myapi.example.com"
`
	path := writeTempToml(t, content)
	providers, err := LoadAllTomlProviders(path)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders() unexpected error: %v", err)
	}
	cfg, ok := providers["myapi"]
	if !ok {
		t.Fatal("provider myapi not found in map")
	}

	// Core fields set from TOML
	if cfg.TargetBase != "https://myapi.example.com" {
		t.Errorf("TargetBase = %q, want %q", cfg.TargetBase, "https://myapi.example.com")
	}

	// All optional fields should fall through to DefaultConfig
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want default 8080", cfg.Port)
	}
	if cfg.CooldownSec != 15 {
		t.Errorf("CooldownSec = %d, want default 60", cfg.CooldownSec)
	}
	if cfg.MaxRetries != 1 {
		t.Errorf("MaxRetries = %d, want default 1", cfg.MaxRetries)
	}
	if cfg.DisableThinking {
		t.Error("DisableThinking = true, want default false")
	}
	if cfg.GenaiModel != "" {
		t.Errorf("GenaiModel = %q, want empty", cfg.GenaiModel)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want default %q", cfg.LogLevel, "info")
	}
	if cfg.AdminToken != "" {
		t.Errorf("AdminToken = %q, want empty", cfg.AdminToken)
	}
	if cfg.KeysFile != "keys.json" {
		t.Errorf("KeysFile = %q, want default %q", cfg.KeysFile, "keys.json")
	}
	if cfg.BackoffCapSec != 120 {
		t.Errorf("BackoffCapSec = %d, want default %d", cfg.BackoffCapSec, 120)
	}
	if cfg.BackoffMultiplier != 2 {
		t.Errorf("BackoffMultiplier = %g, want default %g", cfg.BackoffMultiplier, 2.0)
	}
	if cfg.CBResetSec != 30 {
		t.Errorf("CBResetSec = %d, want default %d", cfg.CBResetSec, 30)
	}
	if cfg.UpstreamCBThreshold != 5 {
		t.Errorf("UpstreamCBThreshold = %d, want default %d", cfg.UpstreamCBThreshold, 5)
	}
	if cfg.HealthCheckIntervalSec != 30 {
		t.Errorf("HealthCheckIntervalSec = %d, want default %d", cfg.HealthCheckIntervalSec, 30)
	}
	if cfg.LogFile != "" {
		t.Errorf("LogFile = %q, want empty (default)", cfg.LogFile)
	}
	if cfg.LogMaxSize != 100 {
		t.Errorf("LogMaxSize = %d, want default 100", cfg.LogMaxSize)
	}
	if cfg.LogMaxAge != 7 {
		t.Errorf("LogMaxAge = %d, want default 7", cfg.LogMaxAge)
	}
}

func TestTomlProviderConfig_Roundtrip(t *testing.T) {
	orig := DefaultConfig()
	orig.TargetBase = "https://api.example.com"
	orig.Port = 7070
	orig.CooldownSec = 45
	orig.MaxRetries = 7
	orig.DisableThinking = true
	orig.GenaiModel = "sonnet-4.6"
	orig.LogLevel = "warn"
	orig.AdminToken = "secrettoken"
	orig.KeysFile = "/app/keys.json"
	orig.BackoffCapSec = 300
	orig.BackoffMultiplier = 3.5
	orig.CBResetSec = 60
	orig.UpstreamCBThreshold = 10
	orig.HealthCheckIntervalSec = 15
	orig.HTTPTimeoutSec = 45
	orig.LogFile = "/var/log/akswitch.log"
	orig.LogMaxSize = 200
	orig.LogMaxAge = 30

	tmpDir := t.TempDir()
	tomlPath := filepath.Join(tmpDir, "roundtrip_ext.toml")
	if err := SaveTomlConfig(&TomlConfig{Port: orig.Port, Provider: map[string]*Config{"myapi": orig}}, tomlPath); err != nil {
		t.Fatalf("SaveTomlConfig() error: %v", err)
	}

	providers, err := LoadAllTomlProviders(tomlPath)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders() error: %v", err)
	}
	loaded, ok := providers["myapi"]
	if !ok {
		t.Fatal("provider myapi not found in map")
	}

	if loaded.TargetBase != orig.TargetBase {
		t.Errorf("TargetBase = %q, want %q", loaded.TargetBase, orig.TargetBase)
	}
	if loaded.Port != orig.Port {
		t.Errorf("Port = %d, want %d", loaded.Port, orig.Port)
	}
	if loaded.CooldownSec != orig.CooldownSec {
		t.Errorf("CooldownSec = %d, want %d", loaded.CooldownSec, orig.CooldownSec)
	}
	if loaded.MaxRetries != orig.MaxRetries {
		t.Errorf("MaxRetries = %d, want %d", loaded.MaxRetries, orig.MaxRetries)
	}
	if loaded.DisableThinking != orig.DisableThinking {
		t.Errorf("DisableThinking = %v, want %v", loaded.DisableThinking, orig.DisableThinking)
	}
	if loaded.GenaiModel != orig.GenaiModel {
		t.Errorf("GenaiModel = %q, want %q", loaded.GenaiModel, orig.GenaiModel)
	}
	if loaded.LogLevel != orig.LogLevel {
		t.Errorf("LogLevel = %q, want %q", loaded.LogLevel, orig.LogLevel)
	}
	if loaded.AdminToken != orig.AdminToken {
		t.Errorf("AdminToken = %q, want %q", loaded.AdminToken, orig.AdminToken)
	}
	if loaded.KeysFile != orig.KeysFile {
		t.Errorf("KeysFile = %q, want %q", loaded.KeysFile, orig.KeysFile)
	}
	if loaded.BackoffCapSec != orig.BackoffCapSec {
		t.Errorf("BackoffCapSec = %d, want %d", loaded.BackoffCapSec, orig.BackoffCapSec)
	}
	if loaded.BackoffMultiplier != orig.BackoffMultiplier {
		t.Errorf("BackoffMultiplier = %g, want %g", loaded.BackoffMultiplier, orig.BackoffMultiplier)
	}
	if loaded.CBResetSec != orig.CBResetSec {
		t.Errorf("CBResetSec = %d, want %d", loaded.CBResetSec, orig.CBResetSec)
	}
	if loaded.UpstreamCBThreshold != orig.UpstreamCBThreshold {
		t.Errorf("UpstreamCBThreshold = %d, want %d", loaded.UpstreamCBThreshold, orig.UpstreamCBThreshold)
	}
	if loaded.HTTPTimeoutSec != orig.HTTPTimeoutSec {
		t.Errorf("HTTPTimeoutSec = %d, want %d", loaded.HTTPTimeoutSec, orig.HTTPTimeoutSec)
	}
	if loaded.HealthCheckIntervalSec != orig.HealthCheckIntervalSec {
		t.Errorf("HealthCheckIntervalSec = %d, want %d", loaded.HealthCheckIntervalSec, orig.HealthCheckIntervalSec)
	}
}

func TestXDGConfigPath(t *testing.T) {
	path, err := XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath() unexpected error: %v", err)
	}
	if path == "" {
		t.Error("XDGConfigPath() returned empty path")
	}
	if !strings.Contains(path, ".akswitch") {
		t.Errorf("XDGConfigPath() = %q, want path containing \".akswitch\"", path)
	}
	// Should NOT contain AppData, Roaming, or .config
	if strings.Contains(path, "AppData") || strings.Contains(path, "Roaming") || strings.Contains(path, ".config") {
		t.Errorf("XDGConfigPath() = %q, should not contain AppData/Roaming/.config", path)
	}
}

func TestLoadToml_BasicTarget(t *testing.T) {
	tmpDir := t.TempDir()
	tomlPath := filepath.Join(tmpDir, "basic.toml")
	content := `[provider.default]
target = "https://api.example.com"
cooldown_sec = 45
max_retries = 7

[provider.myapi]
target = "https://myapi.example.com"
`
	if err := os.WriteFile(tomlPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	providers, err := LoadAllTomlProviders(tomlPath)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders() unexpected error: %v", err)
	}
	cfg, ok := providers["myapi"]
	if !ok {
		t.Fatal("provider myapi not found in map")
	}
	if cfg.TargetBase != "https://myapi.example.com" {
		t.Errorf("TargetBase = %q, want %q", cfg.TargetBase, "https://myapi.example.com")
	}
	// myapi should inherit cooldown_sec=45, max_retries=7 from [provider.default]
	if cfg.CooldownSec != 45 {
		t.Errorf("CooldownSec = %d, want 45 (inherited)", cfg.CooldownSec)
	}
	if cfg.MaxRetries != 7 {
		t.Errorf("MaxRetries = %d, want 7 (inherited)", cfg.MaxRetries)
	}
}

func TestLoadAllTomlProviders_DeprecatedFieldWarns(t *testing.T) {
	t.Run("standard", func(t *testing.T) {
		tmpDir := t.TempDir()
		tomlPath := filepath.Join(tmpDir, "deprecated.toml")
		content := `[provider.default]
target = "https://api.example.com"
genai = "https://ai.example.com"

[provider.myapi]
target = "https://myapi.example.com"
`
		if err := os.WriteFile(tomlPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
		slog.SetDefault(slog.New(handler))
		t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) })
		_, err := LoadAllTomlProviders(tomlPath)
		if err != nil {
			t.Fatalf("LoadAllTomlProviders() unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "deprecated field") {
			t.Errorf("expected deprecation warning in log output, got: %s", buf.String())
		}
		if !strings.Contains(buf.String(), "genai") {
			t.Errorf("expected warning to mention 'genai' field, got: %s", buf.String())
		}
	})
	t.Run("no_space_around_equals", func(t *testing.T) {
		tmpDir := t.TempDir()
		tomlPath := filepath.Join(tmpDir, "deprecated_nospace.toml")
		content := `[provider.default]
target = "https://api.example.com"
genai="https://ai.example.com"

[provider.myapi]
target = "https://myapi.example.com"
`
		if err := os.WriteFile(tomlPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
		slog.SetDefault(slog.New(handler))
		t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) })
		_, err := LoadAllTomlProviders(tomlPath)
		if err != nil {
			t.Fatalf("LoadAllTomlProviders() unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "deprecated field") {
			t.Errorf("expected deprecation warning for genai=..., got: %s", buf.String())
		}
	})
	t.Run("section_header_not_matched", func(t *testing.T) {
		tmpDir := t.TempDir()
		tomlPath := filepath.Join(tmpDir, "section_header.toml")
		content := `[provider.genai]
target = "https://api.example.com"
`
		if err := os.WriteFile(tomlPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
		slog.SetDefault(slog.New(handler))
		t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) })
		_, err := LoadAllTomlProviders(tomlPath)
		if err != nil {
			t.Fatalf("LoadAllTomlProviders() unexpected error: %v", err)
		}
		if strings.Contains(buf.String(), "deprecated field") {
			t.Errorf("[provider.genai] section header should not trigger deprecation warning, got: %s", buf.String())
		}
	})
}

func TestLoadToml_MultiProvider(t *testing.T) {
	tmpDir := t.TempDir()
	tomlPath := filepath.Join(tmpDir, "multi.toml")
	content := `port = 9090

[provider.primary]
target = "https://primary.example.com"

[provider.secondary]
target = "https://secondary.example.com"
`
	if err := os.WriteFile(tomlPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	providers, err := LoadAllTomlProviders(tomlPath)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders() unexpected error: %v", err)
	}
	cfg, ok := providers["primary"]
	if !ok {
		t.Fatal("provider default not found in map")
	}
	// Should use first provider (primary) as the main config
	if cfg.TargetBase != "https://primary.example.com" {
		t.Errorf("TargetBase = %q, want %q (first provider)", cfg.TargetBase, "https://primary.example.com")
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want %d (first provider)", cfg.Port, 9090)
	}
}

// ============================================================
// LoadAllTomlProviders 测试
// ============================================================

func writeTempToml(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAllTomlProviders_MultiProvider(t *testing.T) {
	content := `port = 9090

[provider.sensenova]
target = "https://api.sensenova.com"

[provider.nvidia]
target = "https://integrate.api.nvidia.com/v1"
`
	path := writeTempToml(t, content)
	providers, err := LoadAllTomlProviders(path)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders() unexpected error: %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("providers count = %d, want 2", len(providers))
	}

	sense, ok := providers["sensenova"]
	if !ok {
		t.Fatal("providers missing key 'sensenova'")
	}
	if sense.TargetBase != "https://api.sensenova.com" {
		t.Errorf("sensenova TargetBase = %q, want %q", sense.TargetBase, "https://api.sensenova.com")
	}
	if sense.Port != 9090 {
		t.Errorf("sensenova Port = %d, want %d", sense.Port, 9090)
	}

	nv, ok := providers["nvidia"]
	if !ok {
		t.Fatal("providers missing key 'nvidia'")
	}
	if nv.TargetBase != "https://integrate.api.nvidia.com/v1" {
		t.Errorf("nvidia TargetBase = %q, want %q", nv.TargetBase, "https://integrate.api.nvidia.com/v1")
	}
	if nv.Port != 9090 {
		t.Errorf("nvidia Port = %d, want %d", nv.Port, 9090)
	}
}

func TestLoadAllTomlProviders_EmptyProvider(t *testing.T) {
	content := `[server]
port = 8080
`
	path := writeTempToml(t, content)
	_, err := LoadAllTomlProviders(path)
	if err == nil {
		t.Error("LoadAllTomlProviders() expected error for missing [provider] section, got nil")
	}
}

func TestLoadAllTomlProviders_MissingFile(t *testing.T) {
	_, err := LoadAllTomlProviders("/nonexistent/path/config.toml")
	if err == nil {
		t.Error("LoadAllTomlProviders() expected error for non-existent file, got nil")
	}
}

func TestLoadAllTomlProviders_Defaults(t *testing.T) {
	content := `[provider.test]
target = "https://api.example.com"

`
	path := writeTempToml(t, content)
	providers, err := LoadAllTomlProviders(path)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders() unexpected error: %v", err)
	}
	cfg, ok := providers["test"]
	if !ok {
		t.Fatal("providers missing key 'test'")
	}
	if cfg.TargetBase != "https://api.example.com" {
		t.Errorf("TargetBase = %q, want %q", cfg.TargetBase, "https://api.example.com")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want default 8080", cfg.Port)
	}
	if cfg.CooldownSec != 15 {
		t.Errorf("CooldownSec = %d, want default 60", cfg.CooldownSec)
	}
	if cfg.MaxRetries != 1 {
		t.Errorf("MaxRetries = %d, want default 1", cfg.MaxRetries)
	}
}

// ── FindServerPort ───────────────────────────────

func TestFindServerPort_WithPort(t *testing.T) {
	content := "port = 8080\n\n[provider.test]\ntarget = \"https://api.example.com\"\n\n"
	path := writeTempToml(t, content)
	port := FindServerPort(path)
	if port != 8080 {
		t.Errorf("FindServerPort() = %d, want 8080", port)
	}
}

func TestFindServerPort_NoPort(t *testing.T) {
	content := "port = 0\n\n[provider.test]\ntarget = \"https://api.example.com\"\n\n"
	path := writeTempToml(t, content)
	port := FindServerPort(path)
	if port != 8080 {
		t.Errorf("FindServerPort() = %d, want 8080 (default)", port)
	}
}

func TestFindServerPort_MissingFile(t *testing.T) {
	port := FindServerPort("/nonexistent/path/config.toml")
	if port != 0 {
		t.Errorf("FindServerPort() = %d, want 0", port)
	}
}

func TestFindServerPort_FirstProviderPicked(t *testing.T) {
	content := "port = 9999\n\n[provider.first]\ntarget = \"https://first.example.com\"\n\n\n[provider.second]\ntarget = \"https://second.example.com\"\n\n"
	path := writeTempToml(t, content)
	port := FindServerPort(path)
	if port != 9999 {
		t.Errorf("FindServerPort() = %d, want 9999", port)
	}
}

// ============================================================
// ProviderConfig, RuntimeConfig, backward compatibility tests
// ============================================================

func TestDefaultProviderConfig(t *testing.T) {
	pc := DefaultProviderConfig()
	if pc.Port != 8080 {
		t.Errorf("Port = %d, want 8080", pc.Port)
	}
	if pc.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", pc.Host, "127.0.0.1")
	}
	if pc.TargetBase != "" {
		t.Errorf("TargetBase should be empty, got %q", pc.TargetBase)
	}
	if pc.MaxRetries != 1 {
		t.Errorf("MaxRetries = %d, want 1", pc.MaxRetries)
	}
	if pc.CooldownSec != 15 {
		t.Errorf("CooldownSec = %d, want 15", pc.CooldownSec)
	}
	if pc.HealthCheckPath != "/health" {
		t.Errorf("HealthCheckPath = %q, want %q", pc.HealthCheckPath, "/health")
	}
	if pc.CalibrationIntervalSec != 3600 {
		t.Errorf("CalibrationIntervalSec = %d, want 3600", pc.CalibrationIntervalSec)
	}
}

func TestDefaultRuntimeConfig(t *testing.T) {
	rc := DefaultRuntimeConfig()
	if rc.HTTPTimeoutSec != 30 {
		t.Errorf("HTTPTimeoutSec = %d, want 30", rc.HTTPTimeoutSec)
	}
	if rc.MaxRetries != 1 {
		t.Errorf("MaxRetries = %d, want 1", rc.MaxRetries)
	}
	if rc.CooldownSec != 15 {
		t.Errorf("CooldownSec = %d, want 15", rc.CooldownSec)
	}
	if rc.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", rc.LogLevel, "info")
	}
}

func TestProviderConfig_Validate_PortRange(t *testing.T) {
	pc := DefaultProviderConfig()
	pc.TargetBase = "https://example.com"
	pc.Keys = []string{"key1"}
	tests := []struct {
		port    int
		wantErr bool
	}{
		{0, true}, {-1, true}, {65536, true}, {8080, false},
	}
	for _, tt := range tests {
		pc.Port = tt.port
		err := pc.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("Port=%d: wantErr=%v, got err=%v", tt.port, tt.wantErr, err)
		}
	}
}

func TestRuntimeConfig_Validate_HTTPTimeoutSec(t *testing.T) {
	tests := []struct {
		sec     int
		wantErr bool
	}{
		{0, true}, {-1, true}, {1, false}, {30, false},
	}
	for _, tt := range tests {
		rc := &RuntimeConfig{HTTPTimeoutSec: tt.sec}
		err := rc.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("HTTPTimeoutSec=%d: wantErr=%v, got err=%v", tt.sec, tt.wantErr, err)
		}
	}
}

func TestProviderConfig_Validate_HealthCheckTimeoutSec(t *testing.T) {
	tests := []struct {
		sec     int
		wantErr bool
	}{
		{0, true}, {-1, true}, {1, false}, {5, false},
	}
	for _, tt := range tests {
		pc := DefaultProviderConfig()
		pc.TargetBase = "https://example.com"
		pc.Keys = []string{"key1"}
		pc.HealthCheckTimeoutSec = tt.sec
		err := pc.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("HealthCheckTimeoutSec=%d: wantErr=%v, got err=%v", tt.sec, tt.wantErr, err)
		}
	}
}

func TestConfig_BackwardCompatibility(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.HTTPTimeoutSec != 30 {
		t.Errorf("HTTPTimeoutSec = %d, want 30", cfg.HTTPTimeoutSec)
	}
	if cfg.MaxRetries != 1 {
		t.Errorf("MaxRetries = %d, want 1", cfg.MaxRetries)
	}

	cfg.Port = 9090
	cfg.HTTPTimeoutSec = 60
	if cfg.Port != 9090 {
		t.Error("field mutation broken")
	}
	if cfg.HTTPTimeoutSec != 60 {
		t.Error("field mutation broken")
	}
}

func TestSaveTomlConfig_OmitZeroValues(t *testing.T) {
	tc := &TomlConfig{
		Provider: map[string]*Config{
			"test": {
				ProviderConfig: ProviderConfig{
					Port:       7070,
					TargetBase: "https://example.com",
					// CooldownSec, MaxRetries, etc. left at zero
				},
			},
		},
	}
	tmpDir := t.TempDir()
	tomlPath := filepath.Join(tmpDir, "omit_zero.toml")
	if err := SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("SaveTomlConfig() error: %v", err)
	}

	data, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	output := string(data)

	zeroValueFields := []string{
		"cooldown_sec",
		"max_retries",
		"backoff_cap_sec",
		"backoff_multiplier",
		"cb_reset_sec",
		"upstream_cb_threshold",
		"http_timeout_sec",
		"health_check_interval_sec",
		"log_max_size",
		"log_max_age",
		"calibration_interval_sec",
	}
	for _, f := range zeroValueFields {
		if strings.Contains(output, f) {
			t.Errorf("TOML should not contain zero-value %s", f)
		}
	}

	required := []string{"port", "target", "provider"}
	for _, f := range required {
		if !strings.Contains(output, f) {
			t.Errorf("TOML must contain %s", f)
		}
	}
}
