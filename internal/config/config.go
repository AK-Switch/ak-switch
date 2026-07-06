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
	Port            int      // HTTP listen port (default 8080)
	TargetBase      string   // Upstream target base URL (required)
	GenaiBase       string   // Generative AI base URL (required)
	AdminToken      string   // Optional admin authentication token
	DisableThinking bool     // Disable thinking mode
	GenaiModel      string   // Generative AI model name
	MaxRetries      int      // Max retry attempts for upstream (default 2)
	LogLevel        string   // Log level (default "info")
	CooldownSec     int      // Cooldown seconds after rate-limit (default 15)
	HTTPTimeoutSec  int      // HTTP client timeout in seconds (default 30)
	Keys            []string // API keys (at least one required)
	KeyNames        []string // Corresponding key names (empty string if unnamed), same length as Keys
	KeysFile        string   // JSON file path for key persistence (default "keys.json")
	EncryptionKey   []byte   `json:"-"` // AES-256 key for keys.json encryption (32 bytes, hex-encoded via KEYS_ENCRYPTION_KEY)

	BackoffCapSec       int     // Key 退避上限(秒)，达到此值自动禁用 (default 120)
	BackoffMultiplier   float64 // 指数退避倍数 (default 2)
	CBResetSec          int     // 上游熔断器 OPEN→HALF_OPEN 超时(秒) (default 30)
	UpstreamCBThreshold int     // 上游熔断器连续失败触发阈值 (default 5)

	HealthCheckIntervalSec int    // 健康检查间隔(秒)，默认 30，最小 5
	HealthCheckPath       string // 健康检查路径，默认 "/health"
	HealthCheckTimeoutSec int    // 健康检查超时(秒)，默认 5，最小 1

	LogFile    string // 日志文件路径（空 = 不启用文件日志）
	LogMaxSize int    // 日志文件轮转大小（MB，默认 100）
	LogMaxAge  int    // 日志文件保留天数（默认 7）
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