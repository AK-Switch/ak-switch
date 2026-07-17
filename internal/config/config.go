// Package config provides centralized configuration management for AK Switch.
//
// It reads from TOML configuration files, validates required fields,
// and supports runtime diffing for hot-reload scenarios.
package config

import (
	"fmt"

	"akswitch/internal/utils"
)

// Config holds all application configuration.
// Use LoadAllTomlProviders() to create Config slices from TOML, then Validate()
// to ensure required fields are present.
type Config struct {
	Port            int      `toml:"port,omitempty"`            // HTTP listen port (default 8080)
	Host            string   `toml:"host,omitempty"`            // HTTP listen address (default "127.0.0.1")
	TargetBase      string   `toml:"target"`                    // Upstream target base URL (required)
	GenaiBase       string   `toml:"genai,omitempty"`           // Generative AI base URL (required)
	AdminToken      string   `toml:"admin_token,omitempty"`     // Optional admin authentication token
	DisableThinking bool     `toml:"disable_thinking,omitempty"` // Disable thinking mode
	GenaiModel      string   `toml:"genai_model,omitempty"`     // Generative AI model name
	MaxRetries      int      `toml:"max_retries,omitempty"`     // Max retry attempts for upstream (default 2)
	LogLevel        string   `toml:"log_level,omitempty"`       // Log level (default "info")
	CooldownSec     int      `toml:"cooldown_sec,omitempty"`    // Cooldown seconds after rate-limit (default 15)
	HTTPTimeoutSec  int      `toml:"http_timeout_sec,omitempty"` // HTTP client timeout in seconds (default 30)
	Keys            []string `toml:"-"`                         // API keys (at least one required)
	KeyNames        []string `toml:"-"`                         // Corresponding key names (empty string if unnamed), same length as Keys
	KeysFile        string   `toml:"keys_file,omitempty"`       // JSON file path for key persistence (default "keys.json")
	EncryptionKey   []byte   `json:"-" toml:"-"`                // AES-256 key for keys.json encryption (32 bytes, hex-encoded via KEYS_ENCRYPTION_KEY)

	BackoffCapSec       int     `toml:"backoff_cap_sec,omitempty"`       // Key 退避上限(秒)，达到此值自动禁用 (default 120)
	BackoffMultiplier   float64 `toml:"backoff_multiplier,omitempty"`    // 指数退避倍数 (default 2)
	CBResetSec          int     `toml:"cb_reset_sec,omitempty"`          // 上游熔断器 OPEN→HALF_OPEN 超时(秒) (default 30)
	UpstreamCBThreshold int     `toml:"upstream_cb_threshold,omitempty"` // 上游熔断器连续失败触发阈值 (default 5)

	HealthCheckIntervalSec int    `toml:"health_check_interval_sec,omitempty"` // 健康检查间隔(秒)，默认 30，最小 5
	HealthCheckPath       string `toml:"-"`                                     // 健康检查路径，默认 "/health"
	HealthCheckTimeoutSec int    `toml:"-"`                                     // 健康检查超时(秒)，默认 5，最小 1

	LogFile    string `toml:"log_file,omitempty"`    // 日志文件路径（空 = 不启用文件日志）
	LogMaxSize int    `toml:"log_max_size,omitempty"` // 日志文件轮转大小（MB，默认 100）
	LogMaxAge  int    `toml:"log_max_age,omitempty"`  // 日志文件保留天数（默认 7）
}

// ConfigError carries a category tag for error classification.
type ConfigError struct {
	Category string // "config" or "system"
	Message  string
}

func (e *ConfigError) Error() string { return e.Message }

// ConfigChange represents a single field that changed between two Config values.
type ConfigChange struct {
	Field    string
	OldValue string
	NewValue string
}

// DefaultConfig returns a Config with all optional fields set to their defaults.
func DefaultConfig() *Config {
	return &Config{
		Port:                8080,
		Host:                "127.0.0.1",
		MaxRetries:          2,
		LogLevel:            "info",
		CooldownSec:         15,
		HTTPTimeoutSec:      30,
		BackoffCapSec:       120,
		BackoffMultiplier:   2,
		CBResetSec:          30,
		UpstreamCBThreshold: 5,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:       "/health",
		HealthCheckTimeoutSec:  5,
		KeysFile:            "keys.json",
		LogMaxSize:          100,
		LogMaxAge:           7,
	}
}

// Validate checks that all required fields are present and valid.
// Returns a descriptive error for the first problem found.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: PORT=%d 不在有效范围(1-65535)内", c.Port)}
	}
	if c.TargetBase == "" {
		return &ConfigError{Category: "config", Message: "配置错误: TARGET_BASE_URL 为必填字段，请设置上游 API 基础地址"}
	}
	if c.GenaiBase == "" {
		return &ConfigError{Category: "config", Message: "配置错误: GENAI_BASE_URL 为必填字段，请设置 GenAI API 基础地址"}
	}
	if len(c.Keys) == 0 {
		return &ConfigError{Category: "config", Message: "配置错误: 至少需要一个 API Key（请通过 akswitch key add 添加）"}
	}
	if c.BackoffCapSec < 30 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: BACKOFF_CAP_SEC=%d 不能小于 30 秒", c.BackoffCapSec)}
	}
	if c.BackoffMultiplier < 1 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: BACKOFF_MULTIPLIER=%.1f 不能小于 1.0", c.BackoffMultiplier)}
	}
	if c.CBResetSec < 5 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: CB_RESET_SEC=%d 不能小于 5 秒", c.CBResetSec)}
	}
	if c.UpstreamCBThreshold < 2 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: UPSTREAM_CB_THRESHOLD=%d 不能小于 2", c.UpstreamCBThreshold)}
	}
	if c.HealthCheckIntervalSec < 5 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: HEALTH_CHECK_INTERVAL_SEC=%d 不能小于 5", c.HealthCheckIntervalSec)}
	}
	if c.HTTPTimeoutSec < 1 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: HTTP_TIMEOUT_SEC=%d 不能小于 1 秒", c.HTTPTimeoutSec)}
	}
	if c.HealthCheckTimeoutSec < 1 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: HEALTH_CHECK_TIMEOUT_SEC=%d 不能小于 1", c.HealthCheckTimeoutSec)}
	}
	if len(c.EncryptionKey) > 0 && len(c.EncryptionKey) != 32 {
		return &ConfigError{Category: "config", Message: "配置错误: KEYS_ENCRYPTION_KEY 必须正好是 32 字节（64 个十六进制字符）"}
	}
	return nil
}

// Sanitized returns a copy of the Config with sensitive fields masked.
// API keys in Keys are masked via utils.MaskKey — first 4 chars + "..." + last 4 chars.
// KeyNames are not sensitive and are copied as-is.
func (c *Config) Sanitized() *Config {
	s := *c // shallow copy
	s.Keys = make([]string, len(c.Keys))
	for i, k := range c.Keys {
		s.Keys[i] = utils.MaskKey(k)
	}
	s.KeyNames = make([]string, len(c.KeyNames))
	copy(s.KeyNames, c.KeyNames)
	s.EncryptionKey = nil // excluded from sanitized output
	return &s
}

// mergeConfig fills in zero-value fields with defaults from DefaultConfig.
// This is used after TOML parsing to ensure optional fields have sensible defaults.
func mergeConfig(cfg *Config) {
	def := DefaultConfig()
	if cfg.Port == 0 {
		cfg.Port = def.Port
	}
	if cfg.CooldownSec == 0 {
		cfg.CooldownSec = def.CooldownSec
	}
	if cfg.Host == "" {
		cfg.Host = def.Host
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = def.MaxRetries
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = def.LogLevel
	}
	if cfg.HTTPTimeoutSec == 0 {
		cfg.HTTPTimeoutSec = def.HTTPTimeoutSec
	}
	if cfg.KeysFile == "" {
		cfg.KeysFile = def.KeysFile
	}
	if cfg.BackoffCapSec == 0 {
		cfg.BackoffCapSec = def.BackoffCapSec
	}
	if cfg.BackoffMultiplier == 0 {
		cfg.BackoffMultiplier = def.BackoffMultiplier
	}
	if cfg.CBResetSec == 0 {
		cfg.CBResetSec = def.CBResetSec
	}
	if cfg.UpstreamCBThreshold == 0 {
		cfg.UpstreamCBThreshold = def.UpstreamCBThreshold
	}
	if cfg.HealthCheckIntervalSec == 0 {
		cfg.HealthCheckIntervalSec = def.HealthCheckIntervalSec
	}
	if cfg.HealthCheckTimeoutSec == 0 {
		cfg.HealthCheckTimeoutSec = def.HealthCheckTimeoutSec
	}
	if cfg.HealthCheckPath == "" {
		cfg.HealthCheckPath = def.HealthCheckPath
	}
	if cfg.LogMaxSize == 0 {
		cfg.LogMaxSize = def.LogMaxSize
	}
	if cfg.LogMaxAge == 0 {
		cfg.LogMaxAge = def.LogMaxAge
	}
}