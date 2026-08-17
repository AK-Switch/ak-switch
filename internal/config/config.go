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
	// ── 进 descriptor 表的 provider 字段（顺序 = descriptor 顺序）──────────
	TargetBase             string  `toml:"target" field:"target,display:Target URL,scope:provider"` // Upstream target base URL (required)
	CooldownSec            int     `toml:"cooldown_sec,omitempty" default:"15" field:"cooldown_sec,display:Cooldown (sec),scope:provider,default:15,runtime,min:1"`
	MaxRetries             int     `toml:"max_retries,omitempty" default:"1" field:"max_retries,display:Max Retries,scope:provider,default:1,runtime,min:0"`
	BackoffCapSec          int     `toml:"backoff_cap_sec,omitempty" default:"120" field:"backoff_cap_sec,display:Backoff Cap (sec),scope:provider,default:120,runtime,min:1"`
	BackoffMultiplier      float64 `toml:"backoff_multiplier,omitempty" default:"2" field:"backoff_multiplier,display:Backoff Multiplier,scope:provider,default:2,runtime"`
	CBResetSec             int     `toml:"cb_reset_sec,omitempty" default:"30" field:"cb_reset_sec,display:Circuit Breaker Reset (sec),scope:provider,default:30,runtime,min:1"`
	UpstreamCBThreshold    int     `toml:"upstream_cb_threshold,omitempty" default:"5" field:"upstream_cb_threshold,display:Upstream CB Threshold,scope:provider,default:5,runtime,min:1"`
	HTTPTimeoutSec         int     `toml:"http_timeout_sec,omitempty" default:"30" field:"http_timeout_sec,display:HTTP Timeout (sec),scope:provider,default:30,runtime,min:1"`
	LogLevel               string  `toml:"log_level,omitempty" default:"info" field:"log_level,display:Log Level,scope:provider,default:info,runtime"`
	HealthCheckIntervalSec int     `toml:"health_check_interval_sec,omitempty" default:"30" field:"health_check_interval_sec,display:Health Check Interval (sec),scope:provider,default:30"`
	AdminToken             string  `toml:"admin_token,omitempty" field:"admin_token,display:Admin Token,scope:provider,readonly"` // Optional admin authentication token

	ThinkingMode         string `toml:"thinking_mode,omitempty" field:"thinking_mode,display:Thinking Mode,scope:provider,default:default,runtime"`               // "default" | "rectify"
	RectifyThinkingMapTo string `toml:"rectify_thinking_map_to,omitempty" field:"rectify_thinking_map_to,display:Rectify Thinking Map To,scope:provider,runtime"` // "enabled" | "auto" | "disabled"
	GenaiModel           string `toml:"genai_model,omitempty" field:"genai_model,display:GenAI Model,scope:provider"`                                             // Generative AI model name
	KeysFile             string `toml:"keys_file,omitempty" default:"keys.json" field:"keys_file,display:Keys File,scope:provider,default:keys.json,readonly"`
	KeySelection         string `toml:"key_selection,omitempty" default:"polling" field:"key_selection,display:Key Selection Mode,scope:provider,default:polling"`
	// ── 不进 descriptor 表的字段（无 field tag，反射跳过）──────────────────
	Port                   int      `toml:"port" default:"4000"`
	Host                   string   `toml:"host,omitempty" default:"127.0.0.1"`
	Keys                   []string `toml:"-"` // API keys (at least one required)
	KeyNames               []string `toml:"-"` // Corresponding key names (empty string if unnamed), same length as Keys
	HealthCheckPath        string   `toml:"-" default:"/health"`
	HealthCheckTimeoutSec  int      `toml:"-" default:"5"`
	LogFile                string   `toml:"log_file,omitempty"` // 日志文件路径（空 = 不启用文件日志）
	LogMaxSize             int      `toml:"log_max_size,omitempty" default:"100"`
	LogMaxAge              int      `toml:"log_max_age,omitempty" default:"7"`
	ErrorDumpMaxAge        int      `toml:"error_dump_max_age,omitempty" default:"7"`
	CalibrationIntervalSec int      `toml:"calibration_interval_sec,omitempty" default:"3600"` // Token 校准间隔（秒，默认 1 小时）
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
		Port:                   4000,
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
		ErrorDumpMaxAge:        7,
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

// reflectCopyStruct 遍历 src 的字段拷贝到 dst。slice 做深拷贝，其余值类型直接 Set。
// dst/src 必须是指向同类型 struct 的指针。当前 ProviderConfig/RuntimeConfig 无指针/嵌套
// struct 字段，故不递归（YAGNI）。
func reflectCopyStruct(dst, src interface{}) {
	dv := reflect.ValueOf(dst).Elem()
	sv := reflect.ValueOf(src).Elem()
	for i := 0; i < sv.NumField(); i++ {
		sf := sv.Field(i)
		if sf.Kind() == reflect.Slice {
			if sf.IsNil() {
				dv.Field(i).Set(reflect.MakeSlice(sf.Type(), 0, 0))
				continue
			}
			newSlice := reflect.MakeSlice(sf.Type(), sf.Len(), sf.Cap())
			reflect.Copy(newSlice, sf)
			dv.Field(i).Set(newSlice)
		} else {
			dv.Field(i).Set(sf)
		}
	}
}

// DeepCopy returns a deep copy of the Config.
func (c *Config) DeepCopy() *Config {
	cp := &Config{}
	reflectCopyStruct(&cp.ProviderConfig, &c.ProviderConfig)
	reflectCopyStruct(&cp.RuntimeConfig, &c.RuntimeConfig)
	return cp
}

// syncRuntimeConfig 把 ProviderConfig 中与 RuntimeConfig 同名的字段同步过去。
// 两者字段名一一对应（HTTPTimeoutSec/MaxRetries/CooldownSec/BackoffCapSec/
// BackoffMultiplier/CBResetSec/UpstreamCBThreshold/LogLevel）。
func syncRuntimeConfig(rc *RuntimeConfig, pc *ProviderConfig) {
	rcVal := reflect.ValueOf(rc).Elem()
	pcVal := reflect.ValueOf(pc).Elem()
	for i := 0; i < rcVal.NumField(); i++ {
		fieldName := rcVal.Type().Field(i).Name
		if f := pcVal.FieldByName(fieldName); f.IsValid() {
			rcVal.Field(i).Set(f)
		}
	}
}

// mergeExcludeFields 列出不应从 [provider.default] 段继承的字段。
// 这些字段是 provider 专属，全局段设置无意义。
var mergeExcludeFields = map[string]struct{}{
	"TargetBase": {},
	"Keys":       {},
	"KeyNames":   {},
}

// mergeWithDefaults merges override into base, returning a new Config.
// Non-zero fields in override take precedence over base.
// Uses reflection to iterate all ProviderConfig fields automatically.
// Fields in mergeExcludeFields are never inherited from base.
func mergeWithDefaults(base, override *Config) *Config {
	result := base.DeepCopy()

	baseVal := reflect.ValueOf(&base.ProviderConfig).Elem()
	overrideVal := reflect.ValueOf(&override.ProviderConfig).Elem()
	resultVal := reflect.ValueOf(&result.ProviderConfig).Elem()

	for i := 0; i < baseVal.NumField(); i++ {
		name := baseVal.Type().Field(i).Name
		if _, excluded := mergeExcludeFields[name]; excluded {
			resultVal.Field(i).SetZero() // 不继承 base 值
		}
		if o := overrideVal.Field(i); !o.IsZero() {
			resultVal.Field(i).Set(o)
		}
	}
	// Sync runtime config
	syncRuntimeConfig(&result.RuntimeConfig, &result.ProviderConfig)
	return result
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
