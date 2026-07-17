package config

import (
	"sort"
	"strconv"
	"strings"

	"akswitch/internal/utils"
)

// fieldDef defines a single configuration field for diff comparison.
type fieldDef struct {
	name   string                       // env var name for diff output
	equal  func(c, o *Config) bool      // equality check
	valStr func(c *Config) string       // serializes field value for diff
}

// configDiffFields is the registry of all diff-comparable config fields.
// Add new fields here instead of adding if-blocks to Diff().
var configDiffFields = []fieldDef{
	{"PORT", func(c, o *Config) bool { return c.Port == o.Port }, func(c *Config) string { return strconv.Itoa(c.Port) }},
		{"HOST", func(c, o *Config) bool { return c.Host == o.Host }, func(c *Config) string { return c.Host }},
	{"TARGET_BASE_URL", func(c, o *Config) bool { return c.TargetBase == o.TargetBase }, func(c *Config) string { return c.TargetBase }},
	{"GENAI_BASE_URL", func(c, o *Config) bool { return c.GenaiBase == o.GenaiBase }, func(c *Config) string { return c.GenaiBase }},
	{"DISABLE_THINKING", func(c, o *Config) bool { return c.DisableThinking == o.DisableThinking }, func(c *Config) string { return strconv.FormatBool(c.DisableThinking) }},
	{"GENAI_MODEL", func(c, o *Config) bool { return c.GenaiModel == o.GenaiModel }, func(c *Config) string { return c.GenaiModel }},
	{"MAX_RETRIES", func(c, o *Config) bool { return c.MaxRetries == o.MaxRetries }, func(c *Config) string { return strconv.Itoa(c.MaxRetries) }},
	{"LOG_LEVEL", func(c, o *Config) bool { return c.LogLevel == o.LogLevel }, func(c *Config) string { return c.LogLevel }},
	{"COOLDOWN_SEC", func(c, o *Config) bool { return c.CooldownSec == o.CooldownSec }, func(c *Config) string { return strconv.Itoa(c.CooldownSec) }},
	{"BACKOFF_CAP_SEC", func(c, o *Config) bool { return c.BackoffCapSec == o.BackoffCapSec }, func(c *Config) string { return strconv.Itoa(c.BackoffCapSec) }},
	{"BACKOFF_MULTIPLIER", func(c, o *Config) bool { return c.BackoffMultiplier == o.BackoffMultiplier }, func(c *Config) string { return strconv.FormatFloat(c.BackoffMultiplier, 'g', -1, 64) }},
	{"CB_RESET_SEC", func(c, o *Config) bool { return c.CBResetSec == o.CBResetSec }, func(c *Config) string { return strconv.Itoa(c.CBResetSec) }},
	{"UPSTREAM_CB_THRESHOLD", func(c, o *Config) bool { return c.UpstreamCBThreshold == o.UpstreamCBThreshold }, func(c *Config) string { return strconv.Itoa(c.UpstreamCBThreshold) }},
	{"HEALTH_CHECK_INTERVAL_SEC", func(c, o *Config) bool { return c.HealthCheckIntervalSec == o.HealthCheckIntervalSec }, func(c *Config) string { return strconv.Itoa(c.HealthCheckIntervalSec) }},
	{"HEALTH_CHECK_PATH", func(c, o *Config) bool { return c.HealthCheckPath == o.HealthCheckPath }, func(c *Config) string { return c.HealthCheckPath }},
	{"HEALTH_CHECK_TIMEOUT_SEC", func(c, o *Config) bool { return c.HealthCheckTimeoutSec == o.HealthCheckTimeoutSec }, func(c *Config) string { return strconv.Itoa(c.HealthCheckTimeoutSec) }},
	{"HTTP_TIMEOUT_SEC", func(c, o *Config) bool { return c.HTTPTimeoutSec == o.HTTPTimeoutSec }, func(c *Config) string { return strconv.Itoa(c.HTTPTimeoutSec) }},
	{"LOG_FILE", func(c, o *Config) bool { return c.LogFile == o.LogFile }, func(c *Config) string { return c.LogFile }},
	{"LOG_MAX_SIZE", func(c, o *Config) bool { return c.LogMaxSize == o.LogMaxSize }, func(c *Config) string { return strconv.Itoa(c.LogMaxSize) }},
	{"LOG_MAX_AGE", func(c, o *Config) bool { return c.LogMaxAge == o.LogMaxAge }, func(c *Config) string { return strconv.Itoa(c.LogMaxAge) }},
}

// Diff returns a list of ConfigChange entries describing what differs
// between c and other. Sensitive fields (Keys) are masked in the output.
// Key names are serialized alongside keys (key==name format) in the diff.
func (c *Config) Diff(other *Config) []ConfigChange {
	var changes []ConfigChange

	// Iterate over the field registry
	for _, f := range configDiffFields {
		if !f.equal(c, other) {
			changes = append(changes, ConfigChange{
				Field:    f.name,
				OldValue: f.valStr(c),
				NewValue: f.valStr(other),
			})
		}
	}

	// Special fields that need custom handling

	// AdminToken — redact values
	if c.AdminToken != other.AdminToken {
		changes = append(changes, ConfigChange{
			Field:    "ADMIN_TOKEN",
			OldValue: "(redacted)",
			NewValue: "(redacted)",
		})
	}

	// EncryptionKey — only expose set/unset state
	if string(c.EncryptionKey) != string(other.EncryptionKey) {
		changes = append(changes, ConfigChange{
			Field:    "KEYS_ENCRYPTION_KEY",
			OldValue: encKeyState(c.EncryptionKey),
			NewValue: encKeyState(other.EncryptionKey),
		})
	}

	// Keys — compare as masked strings (with names)
	if !stringSliceEqual(c.Keys, other.Keys) {
		oldKeys := maskedSliceWithNames(c.Keys, c.KeyNames)
		newKeys := maskedSliceWithNames(other.Keys, other.KeyNames)
		changes = append(changes, ConfigChange{
			Field:    "API_KEYS",
			OldValue: strings.Join(oldKeys, ","),
			NewValue: strings.Join(newKeys, ","),
		})
	}

	// Sort for deterministic output
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Field < changes[j].Field
	})
	return changes
}

func joinKeyName(key, name string) string {
	if name == "" {
		return key
	}
	return key + "==" + name
}

func maskedSliceWithNames(keys []string, names []string) []string {
	result := make([]string, len(keys))
	for i, k := range keys {
		n := ""
		if i < len(names) {
			n = names[i]
		}
		result[i] = joinKeyName(utils.MaskKey(k), n)
	}
	return result
}

func encKeyState(key []byte) string {
	if len(key) == 0 {
		return "unset"
	}
	return "set (32 bytes)"
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}