// Package config provides centralized configuration management for AK Switch.
//
// It reads from TOML configuration files and validates required fields.
package config

import (
	"fmt"
	"log/slog"
	"reflect"
	"strconv"

	"akswitch/internal/logentry"
)

// ProviderConfig holds all provider-level configuration fields with TOML tags.
// Embedded in Config so all existing callers can continue using cfg.FieldName.
type ProviderConfig struct {
	Port            int      `toml:"port" default:"8080"`
	Host            string   `toml:"host,omitempty" default:"127.0.0.1"`
	TargetBase      string   `toml:"target"`                     // Upstream target base URL (required)
	AdminToken      string   `toml:"admin_token,omitempty"`      // Optional admin authentication token
	DisableThinking      bool     `toml:"disable_thinking,omitempty"` // Deprecated: use thinking_mode
	ThinkingMode         string   `toml:"thinking_mode,omitempty"`        // "default" | "rectify"
	RectifyThinkingMapTo string   `toml:"rectify_thinking_map_to,omitempty"` // "enabled" | "auto" | "disabled"
	GenaiModel      string   `toml:"genai_model,omitempty"`      // Generative AI model name
	MaxRetries      int      `toml:"max_retries,omitempty" default:"1"`
	LogLevel        string   `toml:"log_level,omitempty" default:"info"`
	CooldownSec     int      `toml:"cooldown_sec,omitempty" default:"15"`
	HTTPTimeoutSec  int      `toml:"http_timeout_sec,omitempty" default:"30"`
	Keys            []string `toml:"-"` // API keys (at least one required)
	KeyNames        []string `toml:"-"` // Corresponding key names (empty string if unnamed), same length as Keys
	KeysFile        string   `toml:"keys_file,omitempty" default:"keys.json"`

	BackoffCapSec       int     `toml:"backoff_cap_sec,omitempty" default:"120"`
	BackoffMultiplier   float64 `toml:"backoff_multiplier,omitempty" default:"2"`
	CBResetSec          int     `toml:"cb_reset_sec,omitempty" default:"30"`
	UpstreamCBThreshold int     `toml:"upstream_cb_threshold,omitempty" default:"5"`

	HealthCheckIntervalSec int    `toml:"health_check_interval_sec,omitempty" default:"30"`
	HealthCheckPath        string `toml:"-" default:"/health"`
	HealthCheckTimeoutSec  int    `toml:"-" default:"5"`

	LogFile    string `toml:"log_file,omitempty"` // 日志文件路径（空 = 不启用文件日志）
	LogMaxSize int    `toml:"log_max_size,omitempty" default:"100"`
	LogMaxAge  int    `toml:"log_max_age,omitempty" default:"7"`

	CalibrationIntervalSec int `toml:"calibration_interval_sec,omitempty" default:"3600"` // Token 校准间隔（秒，默认 1 小时）
}

// Config holds all provider-level configuration via embedded ProviderConfig.
// Use LoadAllTomlProviders() to create Config slices from TOML, then Validate()
// to ensure required fields are present.
//
// All existing callers access fields via cfg.FieldName — this continues to work
// because Go promotes embedded fields.
//
// Fields with a `default` struct tag are automatically filled by mergeDefaults()
// if they are at their zero value after TOML parsing.
type Config struct {
	ProviderConfig
	RuntimeConfig RuntimeConfig `toml:"-"`
}

// RuntimeConfig holds runtime-only fields that overlap with ProviderConfig
// but are validated independently. Not embedded in Config — accessed via
// cfg.RuntimeConfig.FieldName.
type RuntimeConfig struct {
	HTTPTimeoutSec      int     `toml:"http_timeout_sec"`
	MaxRetries          int     `toml:"max_retries"`
	CooldownSec         int     `toml:"cooldown_sec"`
	BackoffCapSec       int     `toml:"backoff_cap_sec"`
	BackoffMultiplier   float64 `toml:"backoff_multiplier"`
	CBResetSec          int     `toml:"cb_reset_sec"`
	UpstreamCBThreshold int     `toml:"upstream_cb_threshold"`
	LogLevel            string  `toml:"log_level,omitempty"`
}

// ConfigPayload is the JSON structure for config API responses.
type ConfigPayload struct {
	TargetBase string   `json:"targetBase"`
	Keys       []string `json:"keys"`
}

// ConfigError carries a category tag for error classification.
type ConfigError struct {
	Category string // "config" or "system"
	Message  string
}

func (e *ConfigError) Error() string { return e.Message }

// DefaultProviderConfig returns a ProviderConfig with all optional fields set to their defaults.
func DefaultProviderConfig() *ProviderConfig {
	return &ProviderConfig{
		Port:                   8080,
		Host:                   "127.0.0.1",
		MaxRetries:             1,
		LogLevel:               "info",
		CooldownSec:            15,
		HTTPTimeoutSec:         30,
		BackoffCapSec:          120,
		BackoffMultiplier:      2,
		CBResetSec:             30,
		UpstreamCBThreshold:    5,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
		KeysFile:               "keys.json",
		LogMaxSize:             100,
		LogMaxAge:              7,
		CalibrationIntervalSec: 3600,
	}
}

// DefaultRuntimeConfig returns a RuntimeConfig with all fields set to their defaults.
func DefaultRuntimeConfig() *RuntimeConfig {
	return &RuntimeConfig{
		HTTPTimeoutSec:      30,
		MaxRetries:          1,
		CooldownSec:         15,
		BackoffCapSec:       120,
		BackoffMultiplier:   2,
		CBResetSec:          30,
		UpstreamCBThreshold: 5,
		LogLevel:            "info",
	}
}

// DefaultConfig returns a Config with all optional fields set to their defaults.
func DefaultConfig() *Config {
	pc := DefaultProviderConfig()
	return &Config{ProviderConfig: *pc}
}

// Validate checks that all provider-level required fields are present and valid.
// Returns a descriptive error for the first problem found.
func (pc *ProviderConfig) Validate() error {
	if pc.Port < 1 || pc.Port > 65535 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: PORT=%d 不在有效范围(1-65535)内", pc.Port)}
	}
	if pc.TargetBase == "" {
		return &ConfigError{Category: "config", Message: "配置错误: TARGET_BASE_URL 为必填字段，请设置上游 API 基础地址"}
	}
	if len(pc.Keys) == 0 {
		return &ConfigError{Category: "config", Message: "配置错误: 至少需要一个 API Key（请通过 akswitch key add 添加）"}
	}
	if pc.BackoffCapSec < 30 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: BACKOFF_CAP_SEC=%d 不能小于 30 秒", pc.BackoffCapSec)}
	}
	if pc.BackoffMultiplier < 1 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: BACKOFF_MULTIPLIER=%.1f 不能小于 1.0", pc.BackoffMultiplier)}
	}
	if pc.CBResetSec < 5 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: CB_RESET_SEC=%d 不能小于 5 秒", pc.CBResetSec)}
	}
	if pc.UpstreamCBThreshold < 2 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: UPSTREAM_CB_THRESHOLD=%d 不能小于 2", pc.UpstreamCBThreshold)}
	}
	if pc.HealthCheckIntervalSec < 5 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: HEALTH_CHECK_INTERVAL_SEC=%d 不能小于 5", pc.HealthCheckIntervalSec)}
	}
	if pc.HealthCheckTimeoutSec < 1 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: HEALTH_CHECK_TIMEOUT_SEC=%d 不能小于 1", pc.HealthCheckTimeoutSec)}
	}
	return nil
}

// Validate checks runtime-level fields. Returns a descriptive error for the first problem found.
func (rc *RuntimeConfig) Validate() error {
	if rc.HTTPTimeoutSec < 1 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: HTTP_TIMEOUT_SEC=%d 不能小于 1 秒", rc.HTTPTimeoutSec)}
	}
	return nil
}

// Validate checks that all required fields are present and valid.
// Also syncs overlapping fields from ProviderConfig into RuntimeConfig
// so RuntimeConfig is ready for the runtime-config endpoint.
// Returns a descriptive error for the first problem found.
func (c *Config) Validate() error {
	if c.HealthCheckTimeoutSec < 1 {
		return &ConfigError{Category: "config", Message: fmt.Sprintf("配置错误: HEALTH_CHECK_TIMEOUT_SEC=%d 不能小于 1", c.HealthCheckTimeoutSec)}
	}
	if err := c.ProviderConfig.Validate(); err != nil {
		return err
	}
	c.RuntimeConfig.HTTPTimeoutSec = c.HTTPTimeoutSec
	c.RuntimeConfig.MaxRetries = c.MaxRetries
	c.RuntimeConfig.CooldownSec = c.CooldownSec
	c.RuntimeConfig.BackoffCapSec = c.BackoffCapSec
	c.RuntimeConfig.BackoffMultiplier = c.BackoffMultiplier
	c.RuntimeConfig.CBResetSec = c.CBResetSec
	c.RuntimeConfig.UpstreamCBThreshold = c.UpstreamCBThreshold
	c.RuntimeConfig.LogLevel = c.LogLevel
	return c.RuntimeConfig.Validate()
}

// Sanitized returns a copy of the Config with sensitive fields masked.
// API keys in Keys are masked via logentry.MaskKey — first 4 chars + "..." + last 4 chars.
// KeyNames are not sensitive and are copied as-is.
func (c *Config) Sanitized() *Config {
	s := *c // shallow copy
	s.Keys = make([]string, len(c.Keys))
	for i, k := range c.Keys {
		s.Keys[i] = logentry.MaskKey(k)
	}
	s.KeyNames = make([]string, len(c.KeyNames))
	copy(s.KeyNames, c.KeyNames)
	return &s
}

// DeepCopy returns a deep copy of the Config.
func (c *Config) DeepCopy() *Config {
	keys := make([]string, len(c.Keys))
	copy(keys, c.Keys)
	keyNames := make([]string, len(c.KeyNames))
	copy(keyNames, c.KeyNames)
	return &Config{
		ProviderConfig: ProviderConfig{
			Port:                   c.Port,
			Host:                   c.Host,
			TargetBase:             c.TargetBase,
			AdminToken:             c.AdminToken,
			DisableThinking:        c.DisableThinking,
			ThinkingMode:         c.ThinkingMode,
			RectifyThinkingMapTo: c.RectifyThinkingMapTo,
			GenaiModel:             c.GenaiModel,
			MaxRetries:             c.MaxRetries,
			LogLevel:               c.LogLevel,
			CooldownSec:            c.CooldownSec,
			HTTPTimeoutSec:         c.HTTPTimeoutSec,
			Keys:                   keys,
			KeyNames:               keyNames,
			KeysFile:               c.KeysFile,
			BackoffCapSec:          c.BackoffCapSec,
			BackoffMultiplier:      c.BackoffMultiplier,
			CBResetSec:             c.CBResetSec,
			UpstreamCBThreshold:    c.UpstreamCBThreshold,
			HealthCheckIntervalSec: c.HealthCheckIntervalSec,
			HealthCheckPath:        c.HealthCheckPath,
			HealthCheckTimeoutSec:  c.HealthCheckTimeoutSec,
			LogFile:                c.LogFile,
			LogMaxSize:             c.LogMaxSize,
			LogMaxAge:              c.LogMaxAge,
			CalibrationIntervalSec: c.CalibrationIntervalSec,
		},
		RuntimeConfig: RuntimeConfig{
			HTTPTimeoutSec:      c.RuntimeConfig.HTTPTimeoutSec,
			MaxRetries:          c.RuntimeConfig.MaxRetries,
			CooldownSec:         c.RuntimeConfig.CooldownSec,
			BackoffCapSec:       c.RuntimeConfig.BackoffCapSec,
			BackoffMultiplier:   c.RuntimeConfig.BackoffMultiplier,
			CBResetSec:          c.RuntimeConfig.CBResetSec,
			UpstreamCBThreshold: c.RuntimeConfig.UpstreamCBThreshold,
			LogLevel:            c.RuntimeConfig.LogLevel,
		},
	}
}

// mergeWithDefaults merges override into base, returning a new Config.
// Non-zero fields in override take precedence over base.
// Non-inheritable fields (TargetBase, Keys, KeyNames, Port, Host, etc.)
// are taken from override only when non-zero/non-empty.
//
// The 9 inheritable fields are: MaxRetries, HTTPTimeoutSec, CooldownSec,
// BackoffCapSec, BackoffMultiplier, CBResetSec, UpstreamCBThreshold,
// HealthCheckIntervalSec, LogLevel.
func mergeWithDefaults(base, override *Config) *Config {
	result := base.DeepCopy()
	// Inheritable fields — only override if non-zero
	if override.MaxRetries != 0 {
		result.MaxRetries = override.MaxRetries
	}
	if override.HTTPTimeoutSec != 0 {
		result.HTTPTimeoutSec = override.HTTPTimeoutSec
	}
	if override.CooldownSec != 0 {
		result.CooldownSec = override.CooldownSec
	}
	if override.BackoffCapSec != 0 {
		result.BackoffCapSec = override.BackoffCapSec
	}
	if override.BackoffMultiplier != 0 {
		result.BackoffMultiplier = override.BackoffMultiplier
	}
	if override.CBResetSec != 0 {
		result.CBResetSec = override.CBResetSec
	}
	if override.UpstreamCBThreshold != 0 {
		result.UpstreamCBThreshold = override.UpstreamCBThreshold
	}
	if override.HealthCheckIntervalSec != 0 {
		result.HealthCheckIntervalSec = override.HealthCheckIntervalSec
	}
	if override.LogLevel != "" {
		result.LogLevel = override.LogLevel
	}
	// Non-inheritable fields — only override if non-zero/non-empty
	if override.Port != 0 {
		result.Port = override.Port
	}
	if override.Host != "" {
		result.Host = override.Host
	}
	if override.TargetBase != "" {
		result.TargetBase = override.TargetBase
	}
	if override.AdminToken != "" {
		result.AdminToken = override.AdminToken
	}
	if override.DisableThinking {
		result.DisableThinking = true
	}
	if override.ThinkingMode != "" {
		result.ThinkingMode = override.ThinkingMode
	}
	if override.RectifyThinkingMapTo != "" {
		result.RectifyThinkingMapTo = override.RectifyThinkingMapTo
	}
	if override.GenaiModel != "" {
		result.GenaiModel = override.GenaiModel
	}
	if len(override.Keys) > 0 {
		result.Keys = override.Keys
	}
	if len(override.KeyNames) > 0 {
		result.KeyNames = override.KeyNames
	}
	if override.KeysFile != "" {
		result.KeysFile = override.KeysFile
	}
	if override.HealthCheckPath != "" {
		result.HealthCheckPath = override.HealthCheckPath
	}
	if override.HealthCheckTimeoutSec != 0 {
		result.HealthCheckTimeoutSec = override.HealthCheckTimeoutSec
	}
	if override.LogFile != "" {
		result.LogFile = override.LogFile
	}
	if override.LogMaxSize != 0 {
		result.LogMaxSize = override.LogMaxSize
	}
	if override.LogMaxAge != 0 {
		result.LogMaxAge = override.LogMaxAge
	}
	if override.CalibrationIntervalSec != 0 {
		result.CalibrationIntervalSec = override.CalibrationIntervalSec
	}
	// Sync runtime config
	result.RuntimeConfig.HTTPTimeoutSec = result.HTTPTimeoutSec
	result.RuntimeConfig.MaxRetries = result.MaxRetries
	result.RuntimeConfig.CooldownSec = result.CooldownSec
	result.RuntimeConfig.BackoffCapSec = result.BackoffCapSec
	result.RuntimeConfig.BackoffMultiplier = result.BackoffMultiplier
	result.RuntimeConfig.CBResetSec = result.CBResetSec
	result.RuntimeConfig.UpstreamCBThreshold = result.UpstreamCBThreshold
	result.RuntimeConfig.LogLevel = result.LogLevel
	return result
}

// migrateDisableThinking handles backward compatibility for the deprecated
// DisableThinking field. When DisableThinking is true and ThinkingMode is
// unset, it maps to the new field names.
func (pc *ProviderConfig) migrateDisableThinking() {
	if pc.DisableThinking && pc.ThinkingMode == "" {
		pc.ThinkingMode = "rectify"
		pc.RectifyThinkingMapTo = "enabled"
	}
}

// mergeDefaults fills in zero-value fields with their default values using
// reflection on `default` struct tags. This is used after TOML parsing to
// ensure optional fields have sensible defaults without hand-writing if-statements
// for each field.
//
// Adding a new optional field: just add a `default:"..."` tag to the struct field
// and include it in DefaultProviderConfig(). mergeDefaults handles the rest automatically.
func (pc *ProviderConfig) mergeDefaults() {
	v := reflect.ValueOf(pc).Elem()
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
				val, err := strconv.ParseInt(defaultVal, 10, 64)
				if err != nil {
					slog.Warn("mergeDefaults: invalid int default tag value", "field", field.Name, "default", defaultVal, "error", err)
					continue
				}
				f.SetInt(val)
			}
		case reflect.String:
			if f.String() == "" {
				f.SetString(defaultVal)
			}
		case reflect.Float32, reflect.Float64:
			if f.Float() == 0 {
				val, err := strconv.ParseFloat(defaultVal, 64)
				if err != nil {
					slog.Warn("mergeDefaults: invalid float default tag value", "field", field.Name, "default", defaultVal, "error", err)
					continue
				}
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

