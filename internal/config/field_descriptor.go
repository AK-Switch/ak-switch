package config

import (
	"fmt"
	"strconv"
	"strings"
)

// FieldScope identifies whether a field belongs to a provider or is global.
type FieldScope string

const (
	FieldScopeProvider FieldScope = "provider"
	FieldScopeGlobal   FieldScope = "global"
)

// FieldType is the value type of a config field.
type FieldType string

const (
	FieldTypeInt     FieldType = "int"
	FieldTypeString  FieldType = "string"
	FieldTypeBool    FieldType = "bool"
	FieldTypeFloat64 FieldType = "float64"
)

// ConfigFieldDescriptor describes one configurable field.
type ConfigFieldDescriptor struct {
	Key             string
	DisplayName     string
	Scope           FieldScope
	TomlPath        string // "field" for global, "provider.%s.field" for provider
	Type            FieldType
	Default         string // string representation of default value
	RuntimeEditable bool
	ReadOnly        bool // true = cannot be set via config set (needs TOML edit + reload)

	// Parse converts a string value to the field's type. Returns error on invalid input.
	Parse func(string) (any, error)

	// Format converts a typed value back to string for display.
	Format func(any) string

	// Persist writes the parsed value into the Config struct. Provider is the
	// target provider name; for global fields, provider is empty and c is nil.
	Persist func(tc *TomlConfig, provider string, c *Config, value any)
}


// ConfigFieldDescriptors is the single source of truth for all configurable fields.
// Provider-scoped fields come first, then global fields.
var ConfigFieldDescriptors = []ConfigFieldDescriptor{
	// ── Provider-scoped fields ──────────────────────────────────────
	{
		Key:             "target",
		DisplayName:     "Target URL",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.target_base",
		Type:            FieldTypeString,
		Default:         "",
		RuntimeEditable: false,
		Parse:           func(s string) (any, error) { return s, nil },
		Format:          func(v any) string { return fmt.Sprintf("%v", v) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.TargetBase = value.(string)
			}
		},
	},
	{
		Key:             "cooldown_sec",
		DisplayName:     "Cooldown (sec)",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.cooldown_sec",
		Type:            FieldTypeInt,
		Default:         "60",
		RuntimeEditable: true,
		Parse:           func(s string) (any, error) { return strconv.Atoi(s) },
		Format:          func(v any) string { return strconv.Itoa(v.(int)) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.CooldownSec = value.(int)
			}
		},
	},
	{
		Key:             "max_retries",
		DisplayName:     "Max Retries",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.max_retries",
		Type:            FieldTypeInt,
		Default:         "1",
		RuntimeEditable: true,
		Parse:           func(s string) (any, error) { return strconv.Atoi(s) },
		Format:          func(v any) string { return strconv.Itoa(v.(int)) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.MaxRetries = value.(int)
			}
		},
	},
	{
		Key:             "backoff_cap_sec",
		DisplayName:     "Backoff Cap (sec)",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.backoff_cap_sec",
		Type:            FieldTypeInt,
		Default:         "120",
		RuntimeEditable: true,
		Parse:           func(s string) (any, error) { return strconv.Atoi(s) },
		Format:          func(v any) string { return strconv.Itoa(v.(int)) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.BackoffCapSec = value.(int)
			}
		},
	},
	{
		Key:             "backoff_multiplier",
		DisplayName:     "Backoff Multiplier",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.backoff_multiplier",
		Type:            FieldTypeFloat64,
		Default:         "2",
		RuntimeEditable: true,
		Parse: func(s string) (any, error) {
			return strconv.ParseFloat(s, 64)
		},
		Format: func(v any) string {
			f := v.(float64)
			if f == float64(int64(f)) {
				return strconv.FormatInt(int64(f), 10)
			}
			return strconv.FormatFloat(f, 'f', 1, 64)
		},
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.BackoffMultiplier = value.(float64)
			}
		},
	},
	{
		Key:             "cb_reset_sec",
		DisplayName:     "Circuit Breaker Reset (sec)",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.cb_reset_sec",
		Type:            FieldTypeInt,
		Default:         "30",
		RuntimeEditable: true,
		Parse:           func(s string) (any, error) { return strconv.Atoi(s) },
		Format:          func(v any) string { return strconv.Itoa(v.(int)) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.CBResetSec = value.(int)
			}
		},
	},
	{
		Key:             "upstream_cb_threshold",
		DisplayName:     "Upstream CB Threshold",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.upstream_cb_threshold",
		Type:            FieldTypeInt,
		Default:         "5",
		RuntimeEditable: true,
		Parse:           func(s string) (any, error) { return strconv.Atoi(s) },
		Format:          func(v any) string { return strconv.Itoa(v.(int)) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.UpstreamCBThreshold = value.(int)
			}
		},
	},
	{
		Key:             "http_timeout_sec",
		DisplayName:     "HTTP Timeout (sec)",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.http_timeout_sec",
		Type:            FieldTypeInt,
		Default:         "30",
		RuntimeEditable: true,
		Parse:           func(s string) (any, error) { return strconv.Atoi(s) },
		Format:          func(v any) string { return strconv.Itoa(v.(int)) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.HTTPTimeoutSec = value.(int)
			}
		},
	},
	{
		Key:             "log_level",
		DisplayName:     "Log Level",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.log_level",
		Type:            FieldTypeString,
		Default:         "info",
		RuntimeEditable: true,
		Parse: func(s string) (any, error) {
			v := strings.TrimSpace(strings.ToLower(s))
			switch v {
			case "debug", "info", "warn", "error":
				return v, nil
			}
			return nil, fmt.Errorf("invalid log level %q, use: debug, info, warn, error", s)
		},
		Format:          func(v any) string { return fmt.Sprintf("%v", v) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.LogLevel = value.(string)
			}
		},
	},
	{

		Key:             "health_check_interval_sec",
		DisplayName:     "Health Check Interval (sec)",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.health_check_interval_sec",
		Type:            FieldTypeInt,
		Default:         "30",
		RuntimeEditable: false,
		Parse:           func(s string) (any, error) { return strconv.Atoi(s) },
		Format:          func(v any) string { return strconv.Itoa(v.(int)) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.HealthCheckIntervalSec = value.(int)
			}
		},
	},
	{
		Key:             "admin_token",
		DisplayName:     "Admin Token",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.admin_token",
		Type:            FieldTypeString,
		Default:         "",
		RuntimeEditable: false,
		ReadOnly:        true,
		Parse:           func(s string) (any, error) { return s, nil },
		Format:          func(v any) string { return fmt.Sprintf("%v", v) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.AdminToken = value.(string)
			}
		},
	},
	{
		Key:             "disable_thinking",
		DisplayName:     "Disable Thinking",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.disable_thinking",
		Type:            FieldTypeBool,
		Default:         "false",
		RuntimeEditable: false,
		Parse:           func(s string) (any, error) { return strconv.ParseBool(s) },
		Format:          func(v any) string { return strconv.FormatBool(v.(bool)) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.DisableThinking = value.(bool)
			}
		},
	},
	{
		Key:             "genai_model",
		DisplayName:     "GenAI Model",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.genai_model",
		Type:            FieldTypeString,
		Default:         "",
		RuntimeEditable: false,
		Parse:           func(s string) (any, error) { return s, nil },
		Format:          func(v any) string { return fmt.Sprintf("%v", v) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.GenaiModel = value.(string)
			}
		},
	},
	{
		Key:             "keys_file",
		DisplayName:     "Keys File",
		Scope:           FieldScopeProvider,
		TomlPath:        "provider.%s.keys_file",
		Type:            FieldTypeString,
		Default:         "keys.json",
		RuntimeEditable: false,
		ReadOnly:        true,
		Parse:           func(s string) (any, error) { return s, nil },
		Format:          func(v any) string { return fmt.Sprintf("%v", v) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if c != nil {
				c.KeysFile = value.(string)
			}
		},
	},

	// ── Global fields ───────────────────────────────────────────────
	{
		Key:             "port",
		DisplayName:     "Port",
		Scope:           FieldScopeGlobal,
		TomlPath:        "port",
		Type:            FieldTypeInt,
		Default:         "8080",
		RuntimeEditable: false,
		ReadOnly:        true,
		Parse:           func(s string) (any, error) { return strconv.Atoi(s) },
		Format:          func(v any) string { return strconv.Itoa(v.(int)) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if tc != nil {
				tc.Port = value.(int)
			}
		},
	},
	{
		Key:             "log_file",
		DisplayName:     "Log File",
		Scope:           FieldScopeGlobal,
		TomlPath:        "log_file",
		Type:            FieldTypeString,
		Default:         "",
		RuntimeEditable: false,
		ReadOnly:        true,
		Parse:           func(s string) (any, error) { return s, nil },
		Format:          func(v any) string { return fmt.Sprintf("%v", v) },
		Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
			if tc != nil {
				tc.LogFile = value.(string)
			}
		},
	},
}

// FindField returns the descriptor for the given key, or nil if not found.
func FindField(key string) *ConfigFieldDescriptor {
	for i := range ConfigFieldDescriptors {
		if ConfigFieldDescriptors[i].Key == key {
			return &ConfigFieldDescriptors[i]
		}
	}
	return nil
}

// ParseDefault converts the Default string to the field's type.
func ParseDefault(d *ConfigFieldDescriptor) (any, error) {
	switch d.Type {
	case FieldTypeInt:
		return strconv.Atoi(d.Default)
	case FieldTypeFloat64:
		return strconv.ParseFloat(d.Default, 64)
	case FieldTypeBool:
		return strconv.ParseBool(d.Default)
	case FieldTypeString:
		return d.Default, nil
	default:
		return nil, fmt.Errorf("unknown field type: %s", d.Type)
	}
}
