// Package config provides centralized configuration management for AK Switch.
//
// It reads from TOML configuration files, validates required fields,
// and supports runtime diffing for hot-reload scenarios.
package config

import (
	"fmt"
	"reflect"
	"strconv"

	"akswitch/internal/utils"
)

// Config holds all application configuration.
// Use LoadAllTomlProviders() to create Config slices from TOML, then Validate()
// to ensure required fields are present.
//
// Fields with a `default` struct tag are automatically filled by mergeDefaults()
// if they are at their zero value after TOML parsing. To add a new optional field:
//  1. Add the field with a `default:"..."` tag
//  2. Add it to DefaultConfig() for direct callers
//  3. mergeDefaults() handles the rest automatically
type Config struct {
	Port            int      `toml:"port,omitempty" default:"8080"`
	Host            string   `toml:"host,omitempty" default:"127.0.0.1"`
	TargetBase      string   `toml:"target"`                    // Upstream target base URL (required)
	GenaiBase       string   `toml:"genai,omitempty"`           // Generative AI base URL (required)
	AdminToken      string   `toml:"admin_token,omitempty"`     // Optional admin authentication token
	DisableThinking bool     `toml:"disable_thinking,omitempty"` // Disable thinking mode
	GenaiModel      string   `toml:"genai_model,omitempty"`     // Generative AI model name
	MaxRetries      int      `toml:"max_retries,omitempty" default:"2"`
	LogLevel        string   `toml:"log_level,omitempty" default:"info"`
	CooldownSec     int      `toml:"cooldown_sec,omitempty" default:"15"`
	HTTPTimeoutSec  int      `toml:"http_timeout_sec,omitempty" default:"30"`
	Keys            []string `toml:"-"`                         // API keys (at least one required)
	KeyNames        []string `toml:"-"`                         // Corresponding key names (empty string if unnamed), same length as Keys
	KeysFile        string   `toml:"keys_file,omitempty" default:"keys.json"`

	BackoffCapSec       int     `toml:"backoff_cap_sec,omitempty" default:"120"`
	BackoffMultiplier   float64 `toml:"backoff_multiplier,omitempty" default:"2"`
	CBResetSec          int     `toml:"cb_reset_sec,omitempty" default:"30"`
	UpstreamCBThreshold int     `toml:"upstream_cb_threshold,omitempty" default:"5"`

	HealthCheckIntervalSec int    `toml:"health_check_interval_sec,omitempty" default:"30"`
	HealthCheckPath       string `toml:"-" default:"/health"`
	HealthCheckTimeoutSec int    `toml:"-" default:"5"`

	LogFile    string `toml:"log_file,omitempty"`    // 日志文件路径（空 = 不启用文件日志）
	LogMaxSize int    `toml:"log_max_size,omitempty" default:"100"`
	LogMaxAge  int    `toml:"log_max_age,omitempty" default:"7"`
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
	return &s
}

// mergeDefaults fills in zero-value fields with their default values using
// reflection on `default` struct tags. This is used after TOML parsing to
// ensure optional fields have sensible defaults without hand-writing if-statements
// for each field.
//
// Adding a new optional field: just add a `default:"..."` tag to the struct field
// and include it in DefaultConfig(). mergeDefaults handles the rest automatically.
func mergeDefaults(cfg *Config) {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		defaultVal := field.Tag.Get("default")
		if defaultVal == "" {
			continue // no default tag → skip (required field or no default)
		}

		f := v.Field(i)
		if !f.CanSet() {
			continue
		}

		switch f.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if f.Int() == 0 {
				val, _ := strconv.ParseInt(defaultVal, 10, 64)
				f.SetInt(val)
			}
		case reflect.String:
			if f.String() == "" {
				f.SetString(defaultVal)
			}
		case reflect.Float32, reflect.Float64:
			if f.Float() == 0 {
				val, _ := strconv.ParseFloat(defaultVal, 64)
				f.SetFloat(val)
			}
		case reflect.Bool:
			// Only set bool if it's false and default is true
			if !f.Bool() && defaultVal == "true" {
				f.SetBool(true)
			}
		}
	}
}

// mergeConfig fills in zero-value fields with defaults from DefaultConfig.
// This is used after TOML parsing to ensure optional fields have sensible defaults.
// Deprecated: kept for backward compatibility. mergeDefaults replaces this.
func mergeConfig(cfg *Config) {
	mergeDefaults(cfg)
}