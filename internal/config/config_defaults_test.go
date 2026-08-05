//go:build unit

package config

import (
	"os"
	"testing"
)

func TestMergeWithDefaults_InheritsMissingFields(t *testing.T) {
	base := &Config{
		ProviderConfig: ProviderConfig{
			TargetBase:     "https://default.example.com",
			MaxRetries:     3,
			CooldownSec:    20,
			LogLevel:       "info",
			HTTPTimeoutSec: 30,
		},
	}
	override := &Config{
		ProviderConfig: ProviderConfig{
			TargetBase:  "https://override.example.com",
			MaxRetries:  5,
			CooldownSec: 0, // zero — should inherit from base (20)
		},
	}
	result := mergeWithDefaults(base, override)
	if result.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5 (overridden)", result.MaxRetries)
	}
	if result.CooldownSec != 20 {
		t.Errorf("CooldownSec = %d, want 20 (inherited)", result.CooldownSec)
	}
	if result.TargetBase != "https://override.example.com" {
		t.Errorf("TargetBase = %q, want overridden value", result.TargetBase)
	}
}

func TestMergeWithDefaults_NoDefault(t *testing.T) {
	// Without a base, mergeWithDefaults should behave like a copy of override
	override := &Config{
		ProviderConfig: ProviderConfig{
			TargetBase:     "https://example.com",
			MaxRetries:     2,
			CooldownSec:    10,
			HTTPTimeoutSec: 15,
			Keys:           []string{"key1"},
		},
	}
	result := mergeWithDefaults(override, override)
	if result.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", result.MaxRetries)
	}
	if result.CooldownSec != 10 {
		t.Errorf("CooldownSec = %d, want 10", result.CooldownSec)
	}
	if len(result.Keys) != 1 || result.Keys[0] != "key1" {
		t.Errorf("Keys = %v, want [key1]", result.Keys)
	}
}

func TestMergeWithDefaults_AllInherited(t *testing.T) {
	base := &Config{
		ProviderConfig: ProviderConfig{
			TargetBase:     "https://default.example.com",
			MaxRetries:     3,
			CooldownSec:    20,
			HTTPTimeoutSec: 30,
			BackoffCapSec:  120,
			Keys:           []string{"base-key"},
		},
	}
	override := &Config{
		ProviderConfig: ProviderConfig{
			TargetBase: "https://override.example.com",
			Keys:       []string{"override-key"},
		},
	}
	result := mergeWithDefaults(base, override)
	if result.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3 (inherited)", result.MaxRetries)
	}
	if result.CooldownSec != 20 {
		t.Errorf("CooldownSec = %d, want 20 (inherited)", result.CooldownSec)
	}
	if result.BackoffCapSec != 120 {
		t.Errorf("BackoffCapSec = %d, want 120 (inherited)", result.BackoffCapSec)
	}
	if result.TargetBase != "https://override.example.com" {
		t.Errorf("TargetBase = %q, want overridden", result.TargetBase)
	}
	if len(result.Keys) != 1 || result.Keys[0] != "override-key" {
		t.Errorf("Keys = %v, want [override-key]", result.Keys)
	}
}

func TestDeepCopy(t *testing.T) {
	original := &Config{
		ProviderConfig: ProviderConfig{
			TargetBase: "https://example.com",
			MaxRetries: 3,
			Keys:       []string{"key1", "key2"},
			KeyNames:   []string{"primary", "secondary"},
		},
	}
	copied := original.DeepCopy()
	copied.MaxRetries = 99
	copied.Keys[0] = "modified"
	if original.MaxRetries != 3 {
		t.Errorf("original.MaxRetries changed to %d", original.MaxRetries)
	}
	if original.Keys[0] != "key1" {
		t.Errorf("original.Keys[0] changed to %s", original.Keys[0])
	}
}

func TestLoadAllTomlProviders_WithDefaultSection(t *testing.T) {
	toml := `
port = 8080

[provider.default]
max_retries = 3
cooldown_sec = 20
log_level = "warn"

[provider.sensenova]
target = "https://api.sensenova.com/v1"
keys_file = "sensenova.keys"

[provider.claude]
target = "https://api.anthropic.com/v1"
max_retries = 5
`
	tmpFile := writeTempToml(t, toml)
	defer os.Remove(tmpFile)

	providers, err := LoadAllTomlProviders(tmpFile)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders failed: %v", err)
	}

	// sensenova inherits max_retries=3, cooldown_sec=20, log_level="warn" from default
	s := providers["sensenova"]
	if s == nil {
		t.Fatal("provider 'sensenova' not found")
	}
	if s.MaxRetries != 3 {
		t.Errorf("sensenova.MaxRetries = %d, want 3 (inherited)", s.MaxRetries)
	}
	if s.CooldownSec != 20 {
		t.Errorf("sensenova.CooldownSec = %d, want 20 (inherited)", s.CooldownSec)
	}
	if s.LogLevel != "warn" {
		t.Errorf("sensenova.LogLevel = %q, want \"warn\" (inherited)", s.LogLevel)
	}
	if s.TargetBase != "https://api.sensenova.com/v1" {
		t.Errorf("sensenova.TargetBase = %q", s.TargetBase)
	}

	// claude overrides max_retries=5, inherits cooldown_sec=20, log_level="warn"
	c := providers["claude"]
	if c == nil {
		t.Fatal("provider 'claude' not found")
	}
	if c.MaxRetries != 5 {
		t.Errorf("claude.MaxRetries = %d, want 5 (overridden)", c.MaxRetries)
	}
	if c.CooldownSec != 20 {
		t.Errorf("claude.CooldownSec = %d, want 20 (inherited)", c.CooldownSec)
	}
	if c.LogLevel != "warn" {
		t.Errorf("claude.LogLevel = %q, want \"warn\" (inherited)", c.LogLevel)
	}
}

func TestLoadAllTomlProviders_WithoutDefaultSection(t *testing.T) {
	// No [provider.default] — behavior unchanged
	toml := `
port = 9090

[provider.sensenova]
target = "https://api.sensenova.com/v1"
max_retries = 3
`
	tmpFile := writeTempToml(t, toml)
	defer os.Remove(tmpFile)

	providers, err := LoadAllTomlProviders(tmpFile)
	if err != nil {
		t.Fatalf("LoadAllTomlProviders failed: %v", err)
	}
	s := providers["sensenova"]
	if s == nil {
		t.Fatal("provider 'sensenova' not found")
	}
	if s.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", s.MaxRetries)
	}
	if s.Port != 9090 {
		t.Errorf("Port = %d, want 9090", s.Port)
	}
}
