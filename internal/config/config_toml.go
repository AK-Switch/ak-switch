package config

import (
	"bytes"
	"fmt"
	"os"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

// ---- TOML 配置支持 ----

// TomlProviderConfig 对应 TOML 中单个 [provider.*] 的配置。
type TomlProviderConfig struct {
	Target                 string  `toml:"target"`
	Genai                  string  `toml:"genai,omitempty"`
	CooldownSec            int     `toml:"cooldown_sec,omitempty"`
	MaxRetries             int     `toml:"max_retries,omitempty"`
	DisableThinking        bool    `toml:"disable_thinking,omitempty"`
	GenaiModel             string  `toml:"genai_model,omitempty"`
	LogLevel               string  `toml:"log_level,omitempty"`
	AdminToken             string  `toml:"admin_token,omitempty"`
	KeysFile               string  `toml:"keys_file,omitempty"`
	BackoffCapSec          int     `toml:"backoff_cap_sec,omitempty"`
	BackoffMultiplier      float64 `toml:"backoff_multiplier,omitempty"`
	CBResetSec             int     `toml:"cb_reset_sec,omitempty"`
	UpstreamCBThreshold    int     `toml:"upstream_cb_threshold,omitempty"`
	HealthCheckIntervalSec int     `toml:"health_check_interval_sec,omitempty"`
	HTTPTimeoutSec          int     `toml:"http_timeout_sec,omitempty"`
	LogFile    string `toml:"log_file,omitempty"`
	LogMaxSize int    `toml:"log_max_size,omitempty"`
	LogMaxAge  int    `toml:"log_max_age,omitempty"`
}

// TomlConfig 对应整个 config.toml 文件结构。
type TomlConfig struct {
	Port            int                            `toml:"port,omitempty"`
	DefaultProvider string                         `toml:"default_provider,omitempty"`
	LogFile    string                         `toml:"log_file,omitempty"`
	LogMaxSize int                            `toml:"log_max_size,omitempty"`
	LogMaxAge  int                            `toml:"log_max_age,omitempty"`
	Provider   map[string]TomlProviderConfig `toml:"provider"`
}

// DefaultProviderName 保存从 TOML 配置中读取的默认 provider 名称。
// 由 LoadAllTomlProviders 设置，被 start 命令用于确定要启动的 provider。
var DefaultProviderName string

// LoadToml 读取 TOML 配置文件并转换为 Config。
// 文件必须存在且格式合法；格式错误或缺少 [provider] 段返回 error。
func LoadToml(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ConfigError{
			Category: "system",
			Message:  fmt.Sprintf("系统错误: 读取 TOML 配置文件 %q 失败: %v", path, err),
		}
	}
	var tc TomlConfig
	if err := toml.Unmarshal(data, &tc); err != nil {
		return nil, &ConfigError{
			Category: "config",
			Message:  fmt.Sprintf("配置错误: TOML 解析失败: %v", err),
		}
	}
	DefaultProviderName = tc.DefaultProvider
	if len(tc.Provider) == 0 {
		return nil, &ConfigError{
			Category: "config",
			Message:  "配置错误: TOML 配置缺少 [provider] 段",
		}
	}
	// 取第一个 provider 作为主配置（按名称排序确保确定性）
	names := make([]string, 0, len(tc.Provider))
	for n := range tc.Provider {
		names = append(names, n)
	}
	sort.Strings(names)
	p := tc.Provider[names[0]]
	port := tc.Port
	if port == 0 {
		port = DefaultConfig().Port
	}
	return tomlToConfig(names[0], &p, port), nil
}

// SaveToml 将 Config 写入 TOML 文件。覆盖已存在的文件。
func SaveToml(cfg *Config, path string) error {
	tc := configToToml(cfg)
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(tc); err != nil {
		return fmt.Errorf("TOML 编码失败: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

// LoadTomlConfig 读取 TOML 配置文件并返回完整的 TomlConfig（包含所有 provider）。
// 文件不存在时返回原始错误（可通过 os.IsNotExist 检查）。
func LoadTomlConfig(path string) (*TomlConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tc TomlConfig
	if err := toml.Unmarshal(data, &tc); err != nil {
		return nil, err
	}
	if tc.Provider == nil {
		tc.Provider = make(map[string]TomlProviderConfig)
	}
	return &tc, nil
}

// SaveTomlConfig 将完整 TomlConfig 写入 TOML 文件。覆盖已存在的文件。
func SaveTomlConfig(tc *TomlConfig, path string) error {
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(tc); err != nil {
		return fmt.Errorf("TOML 编码失败: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

// tomlToConfig 将单 provider 的 TOML 配置转换为 *Config，未指定的字段使用默认值。
func tomlToConfig(name string, tc *TomlProviderConfig, port int) *Config {
	cfg := DefaultConfig()
	cfg.TargetBase = tc.Target
	cfg.GenaiBase = tc.Genai
	if port > 0 {
		cfg.Port = port
	}
	if tc.CooldownSec > 0 {
		cfg.CooldownSec = tc.CooldownSec
	}
	if tc.MaxRetries > 0 {
		cfg.MaxRetries = tc.MaxRetries
	}
	if tc.DisableThinking {
		cfg.DisableThinking = true
	}
	if tc.GenaiModel != "" {
		cfg.GenaiModel = tc.GenaiModel
	}
	if tc.LogLevel != "" {
		cfg.LogLevel = tc.LogLevel
	}
	if tc.AdminToken != "" {
		cfg.AdminToken = tc.AdminToken
	}
	if tc.KeysFile != "" {
		cfg.KeysFile = tc.KeysFile
	}
	if tc.BackoffCapSec > 0 {
		cfg.BackoffCapSec = tc.BackoffCapSec
	}
	if tc.BackoffMultiplier > 0 {
		cfg.BackoffMultiplier = tc.BackoffMultiplier
	}
	if tc.CBResetSec > 0 {
		cfg.CBResetSec = tc.CBResetSec
	}
	if tc.UpstreamCBThreshold > 0 {
		cfg.UpstreamCBThreshold = tc.UpstreamCBThreshold
	}
	if tc.HealthCheckIntervalSec > 0 {
		cfg.HealthCheckIntervalSec = tc.HealthCheckIntervalSec
	}
	if tc.HTTPTimeoutSec > 0 {
		cfg.HTTPTimeoutSec = tc.HTTPTimeoutSec
	}
	if tc.LogFile != "" {
		cfg.LogFile = tc.LogFile
	}
	if tc.LogMaxSize > 0 {
		cfg.LogMaxSize = tc.LogMaxSize
	}
	if tc.LogMaxAge > 0 {
		cfg.LogMaxAge = tc.LogMaxAge
	}
	return cfg
}

// configToToml 将 *Config 转换为 *TomlConfig（用于写入 TOML 文件）。
func configToToml(cfg *Config) *TomlConfig {
	return &TomlConfig{
		Port:            cfg.Port,
		DefaultProvider: DefaultProviderName,
		LogFile:    cfg.LogFile,
		LogMaxSize: cfg.LogMaxSize,
		LogMaxAge:  cfg.LogMaxAge,
		Provider: map[string]TomlProviderConfig{
			"default": {
				Target:                 cfg.TargetBase,
				Genai:                  cfg.GenaiBase,
				CooldownSec:            cfg.CooldownSec,
				MaxRetries:             cfg.MaxRetries,
				DisableThinking:        cfg.DisableThinking,
				GenaiModel:             cfg.GenaiModel,
				LogLevel:               cfg.LogLevel,
				AdminToken:             cfg.AdminToken,
				KeysFile:               cfg.KeysFile,
				BackoffCapSec:          cfg.BackoffCapSec,
				BackoffMultiplier:      cfg.BackoffMultiplier,
				CBResetSec:             cfg.CBResetSec,
				UpstreamCBThreshold:    cfg.UpstreamCBThreshold,
				HealthCheckIntervalSec: cfg.HealthCheckIntervalSec,
				HTTPTimeoutSec:    cfg.HTTPTimeoutSec,
			},
		},
	}
}