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

func TestMergeWithDefaults_KeySelection(t *testing.T) {
	// 验证 KeySelection 从全局段继承（当前手写版漏这个字段，会失败）
	base := &Config{
		ProviderConfig: ProviderConfig{
			KeySelection: "random",
		},
	}
	override := &Config{
		ProviderConfig: ProviderConfig{
			TargetBase: "https://example.com",
		},
	}
	result := mergeWithDefaults(base, override)
	if result.KeySelection != "random" {
		t.Errorf("KeySelection = %q, want %q (inherited from default)", result.KeySelection, "random")
	}
}

func TestMergeWithDefaults_AllFieldsInherit(t *testing.T) {
	// base 模拟 [provider.default] 段，override 只设必要字段
	// 验证所有非排除字段都从 base 继承
	// Base 也携带排除字段的"垃圾值"——如果排除失效，这些值会被错误继承
	base := &Config{
		ProviderConfig: ProviderConfig{
			Port:                   9090,
			Host:                   "0.0.0.0",
			MaxRetries:             3,
			HTTPTimeoutSec:         60,
			CooldownSec:            20,
			BackoffCapSec:          240,
			BackoffMultiplier:      3,
			CBResetSec:             60,
			UpstreamCBThreshold:    10,
			HealthCheckIntervalSec: 10,
			LogLevel:               "warn",
			AdminToken:             "global-token",
			DisableThinking:        true,
			ThinkingMode:           "rectify",
			RectifyThinkingMapTo:   "enabled",
			GenaiModel:             "claude-opus-4",
			KeysFile:               "global.keys",
			KeySelection:           "random",
			HealthCheckPath:        "/healthz",
			HealthCheckTimeoutSec:  10,
			LogFile:                "/var/log/akswitch.log",
			LogMaxSize:             200,
			LogMaxAge:              30,
			ErrorDumpMaxAge:        14,
			CalibrationIntervalSec: 7200,
			// 排除字段设置垃圾值，验证不会泄漏到 result
			TargetBase: "https://should-not-inherit.example.com",
			Keys:       []string{"should-not-inherit-key"},
			KeyNames:   []string{"should-not-inherit-name"},
		},
	}
	override := &Config{
		ProviderConfig: ProviderConfig{
			TargetBase: "https://api.example.com/v1",
		},
	}
	result := mergeWithDefaults(base, override)

	// 验证继承字段全部来自 base
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"Port", result.Port, 9090},
		{"Host", result.Host, "0.0.0.0"},
		{"MaxRetries", result.MaxRetries, 3},
		{"HTTPTimeoutSec", result.HTTPTimeoutSec, 60},
		{"CooldownSec", result.CooldownSec, 20},
		{"BackoffCapSec", result.BackoffCapSec, 240},
		{"BackoffMultiplier", result.BackoffMultiplier, 3.0},
		{"CBResetSec", result.CBResetSec, 60},
		{"UpstreamCBThreshold", result.UpstreamCBThreshold, 10},
		{"HealthCheckIntervalSec", result.HealthCheckIntervalSec, 10},
		{"LogLevel", result.LogLevel, "warn"},
		{"AdminToken", result.AdminToken, "global-token"},
		{"DisableThinking", result.DisableThinking, true},
		{"ThinkingMode", result.ThinkingMode, "rectify"},
		{"RectifyThinkingMapTo", result.RectifyThinkingMapTo, "enabled"},
		{"GenaiModel", result.GenaiModel, "claude-opus-4"},
		{"KeysFile", result.KeysFile, "global.keys"},
		{"KeySelection", result.KeySelection, "random"},
		{"HealthCheckPath", result.HealthCheckPath, "/healthz"},
		{"HealthCheckTimeoutSec", result.HealthCheckTimeoutSec, 10},
		{"LogFile", result.LogFile, "/var/log/akswitch.log"},
		{"LogMaxSize", result.LogMaxSize, 200},
		{"LogMaxAge", result.LogMaxAge, 30},
		{"ErrorDumpMaxAge", result.ErrorDumpMaxAge, 14},
		{"CalibrationIntervalSec", result.CalibrationIntervalSec, 7200},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}

	// 验证排除字段的行为
	// TargetBase: override 设了真实值 → 覆盖生效，且 base 垃圾值不泄漏
	// Keys/KeyNames: override 未设 → result 必须为空，base 垃圾值绝不泄漏
	if result.TargetBase != "https://api.example.com/v1" {
		t.Errorf("TargetBase = %q, want override value", result.TargetBase)
	}
	if len(result.Keys) != 0 {
		t.Errorf("Keys = %v, want empty (base garbage must not leak)", result.Keys)
	}
	if len(result.KeyNames) != 0 {
		t.Errorf("KeyNames = %v, want empty (base garbage must not leak)", result.KeyNames)
	}
}

func TestMergeWithDefaults_OverridePriority(t *testing.T) {
	base := &Config{
		ProviderConfig: ProviderConfig{
			MaxRetries:   3,
			CooldownSec:  20,
			LogLevel:     "info",
			KeySelection: "polling",
			// 排除字段的垃圾值——验证 override 覆盖
			TargetBase: "https://should-not-leak.example.com",
			Keys:       []string{"should-not-leak"},
			KeyNames:   []string{"should-not-leak"},
		},
	}
	override := &Config{
		ProviderConfig: ProviderConfig{
			TargetBase:   "https://api.example.com",
			MaxRetries:   5,
			LogLevel:     "debug",
			KeySelection: "random",
			Keys:         []string{"override-key"},
		},
	}
	result := mergeWithDefaults(base, override)
	if result.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5 (overridden)", result.MaxRetries)
	}
	if result.CooldownSec != 20 {
		t.Errorf("CooldownSec = %d, want 20 (inherited)", result.CooldownSec)
	}
	if result.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want \"debug\" (overridden)", result.LogLevel)
	}
	if result.KeySelection != "random" {
		t.Errorf("KeySelection = %q, want \"random\" (overridden)", result.KeySelection)
	}
	// 排除字段：override 非零 → 覆盖生效，base 垃圾值不泄漏
	if result.TargetBase != "https://api.example.com" {
		t.Errorf("TargetBase = %q, want override value", result.TargetBase)
	}
	if len(result.Keys) != 1 || result.Keys[0] != "override-key" {
		t.Errorf("Keys = %v, want [override-key]", result.Keys)
	}
}
