package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"akswitch/internal/config"

	"github.com/spf13/cobra"
)

// validateFieldRange checks numeric range constraints for config fields.
func validateFieldRange(fd *config.ConfigFieldDescriptor, valueStr string) error {
	parsed, err := fd.Parse(valueStr)
	if err != nil {
		return fmt.Errorf("validateFieldRange: invalid --%s value: %w", fd.Key, err)
	}
	switch v := parsed.(type) {
	case int:
		min := -1
		if fd.MinInt >= 0 {
			min = fd.MinInt
		}
		if v < min {
			return fmt.Errorf("--%s must be >= %d", fd.Key, min)
		}
	case float64:
		if v < 1 {
			return fmt.Errorf("--%s must be >= %g", fd.Key, 1.0)
		}
	}
	return nil
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configViewCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)

	configInitCmd.Flags().StringP("path", "p", "", "Output path for config.toml (default: XDG config directory)")
	configListCmd.Flags().Bool("all", false, "Show all providers")
	configGetCmd.Flags().Bool("all", false, "Show value from all providers")
	configSetCmd.Flags().Bool("runtime-only", false, "Apply to runtime only, do not persist to config file")
	configSetCmd.Flags().Bool("all", false, "Apply to all providers")
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
	Long:  `View and initialize the akswitch configuration file.`,
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize default config.toml",
	Long: `Create a default configuration file at the XDG config directory
	(or a custom path via --path).

	If the file already exists, the command refuses to overwrite it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("path")
		if path == "" {
			var err error
			path, err = config.XDGConfigPath()
			if err != nil {
				return fmt.Errorf("failed to determine XDG config path: %w", err)
			}
		}

		// Refuse to overwrite existing file
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("config file already exists at %s (remove it first or use --path to specify a different location)", path)
		}

		// Create directory if needed
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory %s: %w", dir, err)
		}

		// Write example config with placeholder providers
		tc := &config.TomlConfig{
			Port: 8080,
			Provider: map[string]*config.Config{
				"example-a": {
					ProviderConfig: config.ProviderConfig{
						TargetBase:  "https://api.example-a.com/v1",
						CooldownSec: 60,
						MaxRetries:  3,
					},
				},
				"example-b": {
					ProviderConfig: config.ProviderConfig{
						TargetBase:  "https://api.example-b.com/v1",
						CooldownSec: 30,
						MaxRetries:  5,
					},
				},
			},
			Default: &config.Config{
				ProviderConfig: config.ProviderConfig{
					MaxRetries:  2,
					CooldownSec: 15,
					LogLevel:    "info",
				},
			},
		}
		if err := config.SaveTomlConfig(tc, path); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}

		fmt.Printf("Example configuration written to %s\n", path)
		fmt.Println("Edit the file to add your providers, then run:")
		fmt.Println("  akswitch key add <provider> <api-key>  # to add API keys")
		fmt.Println("  akswitch start                         # to start the proxy")

		return nil
	},
}

var configViewCmd = &cobra.Command{
	Use:   "view",
	Short: "Display current configuration",
	Long:  `Read the TOML configuration file and print its contents in a human-readable format.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		source, err := config.XDGConfigPath()
		if err != nil {
			return fmt.Errorf("failed to determine XDG config path: %w", err)
		}

		// Check if the source file actually exists
		if _, statErr := os.Stat(source); statErr != nil {
			return fmt.Errorf("no configuration file found (looked at %s)", source)
		}

		// Load all providers from TOML
		providers, err := config.LoadAllTomlProviders(source)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		fmt.Printf("Configuration source: %s\n", source)
		for name, cfg := range providers {
			sanitized := cfg.Sanitized()
			fmt.Printf("\n--- Provider: %s ---\n", name)
			fmt.Printf("  Port: %d\n", sanitized.Port)
			fmt.Printf("  Target base URL: %s\n", sanitized.TargetBase)
			if sanitized.AdminToken != "" {
				fmt.Println("  Admin token: (set)")
			}
			fmt.Printf("  Disable thinking: %t\n", sanitized.DisableThinking)
			fmt.Printf("  GenAI model: %s\n", sanitized.GenaiModel)
			fmt.Printf("  Max retries: %d\n", sanitized.MaxRetries)
			fmt.Printf("  Log level: %s\n", sanitized.LogLevel)
			fmt.Printf("  Cooldown seconds: %d\n", sanitized.CooldownSec)
			fmt.Printf("  Backoff cap seconds: %d\n", sanitized.BackoffCapSec)
			fmt.Printf("  Backoff multiplier: %.1f\n", sanitized.BackoffMultiplier)
			fmt.Printf("  Circuit breaker reset seconds: %d\n", sanitized.CBResetSec)
			fmt.Printf("  Circuit breaker threshold: %d\n", sanitized.UpstreamCBThreshold)
			fmt.Printf("  Health check interval seconds: %d\n", sanitized.HealthCheckIntervalSec)
			fmt.Printf("  Health check path: %s\n", sanitized.HealthCheckPath)
			fmt.Printf("  Health check timeout seconds: %d\n", sanitized.HealthCheckTimeoutSec)
			fmt.Printf("  Keys file: %s\n", sanitized.KeysFile)
			for i, key := range sanitized.Keys {
				keyName := ""
				if i < len(sanitized.KeyNames) {
					keyName = sanitized.KeyNames[i]
				}
				if keyName != "" {
					fmt.Printf("  Key[%d]: %s (name: %s)\n", i, key, keyName)
				} else {
					fmt.Printf("  Key[%d]: %s\n", i, key)
				}
			}
		}

		return nil
	},
}

// ── Runtime Config Commands ─────────────────────────────

var configListCmd = &cobra.Command{
	Use:   "list [provider]",
	Short: "List runtime-configurable parameters",
	Long: `Display all runtime-configurable parameters and their current values from the config file.

	If a provider name is given, shows parameters for that provider only.
	Use --all to show parameters for all providers.
	Otherwise, shows the first (or only) provider.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		var targetProvider string
		if len(args) > 0 {
			targetProvider = args[0]
		}

		// Load TOML for persistent values
		source, err := config.XDGConfigPath()
		if err != nil {
			return fmt.Errorf("failed to determine config path: %w", err)
		}
		tc, err := config.LoadTomlConfig(source)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Build provider list
		var names []string
		if all {
			if tc != nil {
				for n := range tc.Provider {
					names = append(names, n)
				}
			}
			sort.Strings(names)
			if len(names) == 0 {
				return fmt.Errorf("no providers configured")
			}
		} else if targetProvider != "" {
			names = []string{targetProvider}
		} else {
			// No args, no --all: show the first (or only) provider
			if tc != nil {
				for n := range tc.Provider {
					names = append(names, n)
				}
				sort.Strings(names)
			}
			if len(names) == 0 {
				return fmt.Errorf("no providers configured")
			}
			names = []string{names[0]}
		}

		for _, name := range names {
			fmt.Printf("Provider: %s\n", name)
			fmt.Println()
			for _, fd := range config.ConfigFieldDescriptors {
				if fd.Scope != config.FieldScopeProvider {
					continue
				}
				val, _ := getFieldValue(tc, name, &fd)
				fmt.Printf("  %-30s %s\n", fd.DisplayName+":", maskSensitiveValue(&fd, val))
			}
		}
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key> [provider]",
	Short: "Get a runtime parameter",
	Long: `Display the current value of a single runtime-configurable parameter from the config file.

	Valid keys: port, log_file, target, cooldown_sec, max_retries,
	backoff_cap_sec, backoff_multiplier, cb_reset_sec, upstream_cb_threshold,
	http_timeout_sec, health_check_interval_sec, log_level,
	disable_thinking, genai_model, admin_token, keys_file

	Examples:
	  akswitch config get http_timeout_sec
	  akswitch config get max_retries sensenova
	  akswitch config get log_level --all`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		all, _ := cmd.Flags().GetBool("all")

		fd := config.FindField(key)
		if fd == nil {
			return fmt.Errorf("unknown config key %q (use 'config list' to see available keys)", key)
		}

		var providers []string
		var tc *config.TomlConfig
		if all {
			source, err := config.XDGConfigPath()
			if err != nil {
				return fmt.Errorf("failed to determine config path: %w", err)
			}
			tc, err = config.LoadTomlConfig(source)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if tc != nil {
				for n := range tc.Provider {
					providers = append(providers, n)
				}
			}
			sort.Strings(providers)
		} else if fd.Scope == config.FieldScopeProvider {
			if len(args) < 2 {
				return fmt.Errorf("%s requires a provider name (or --all)", key)
			}
			providers = []string{args[1]}
			source, err := config.XDGConfigPath()
			if err != nil {
				return fmt.Errorf("failed to determine config path: %w", err)
			}
			tc, err = config.LoadTomlConfig(source)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
		} else {
			// Global field — no provider needed
			source, err := config.XDGConfigPath()
			if err != nil {
				return fmt.Errorf("failed to determine config path: %w", err)
			}
			tc, err := config.LoadTomlConfig(source)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			val, _ := getGlobalFieldValue(tc, fd)
			fmt.Println(maskSensitiveValue(fd, val))
			return nil
		}

		for _, p := range providers {
			val, _ := getFieldValue(tc, p, fd)
			if all {
				fmt.Printf("%s: %s\n", p, maskSensitiveValue(fd, val))
			} else {
				fmt.Println(maskSensitiveValue(fd, val))
			}
		}
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value> [provider]",
	Short: "Set a runtime parameter",
	Long: `Change a runtime-configurable parameter immediately.

	Use --runtime-only to apply without persisting to the config file.

	Valid keys: port, log_file, target, cooldown_sec, max_retries,
	backoff_cap_sec, backoff_multiplier, cb_reset_sec, upstream_cb_threshold,
	http_timeout_sec, health_check_interval_sec, log_level,
	disable_thinking, genai_model, admin_token, keys_file

	Examples:
	  akswitch config set http_timeout_sec 60
	  akswitch config set max_retries 5 --runtime-only
	  akswitch config set log_level debug sensenova
	  akswitch config set log_level info --all`,
	Args: cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		valueStr := args[1]
		runtimeOnly, _ := cmd.Flags().GetBool("runtime-only")

		fd := config.FindField(key)
		if fd == nil {
			return fmt.Errorf("unknown config key %q (use 'config list' to see available keys)", key)
		}

		parsed, err := fd.Parse(valueStr)
		if err != nil {
			return fmt.Errorf("invalid value for %s: %w", key, err)
		}

		if err := validateFieldRange(fd, valueStr); err != nil {
			return err
		}

		if fd.ReadOnly {
			return fmt.Errorf("%s cannot be changed at runtime — edit the TOML config file and reload", key)
		}

		// For provider-scoped fields, require provider argument
		provider := ""
		if fd.Scope == config.FieldScopeProvider {
			all, _ := cmd.Flags().GetBool("all")
			if all {
				provider = "all"
			} else if len(args) > 2 {
				provider = args[2]
			} else {
				return fmt.Errorf("%s requires a provider name (or --all)", key)
			}
		}

		// Resolve provider list (expand "all" to actual provider names)
		var providerList []string
		if provider == "all" {
			source, err := config.XDGConfigPath()
			if err != nil {
				return fmt.Errorf("failed to determine config path: %w", err)
			}
			tc, err := config.LoadTomlConfig(source)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			for name := range tc.Provider {
				providerList = append(providerList, name)
			}
			if len(providerList) == 0 {
				return fmt.Errorf("no providers configured for --all")
			}
			sort.Strings(providerList)
		} else {
			providerList = []string{provider}
		}

		// Validate providers exist before any modifications
		if fd.Scope == config.FieldScopeProvider && provider != "all" {
			source, err := config.XDGConfigPath()
			if err != nil {
				return fmt.Errorf("failed to determine config path: %w", err)
			}
			tc, err := config.LoadTomlConfig(source)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			for _, p := range providerList {
				if _, ok := tc.Provider[p]; !ok {
					return fmt.Errorf("provider %q not found in config — run 'provider add' first", p)
				}
			}
		}

		// 1. Apply to runtime (call server API for provider-scoped runtime-editable fields)
		if fd.Scope == config.FieldScopeProvider && fd.RuntimeEditable {
			for _, p := range providerList {
				if err := applyRuntimeField(p, fd, parsed); err != nil {
					return err
				}
			}
		}

		// 2. Persist to TOML
		if !runtimeOnly {
			for _, p := range providerList {
				if err := persistFieldToToml(p, fd, parsed); err != nil {
					return err
				}
			}
		}

		fmt.Printf("set %s = %s", key, fd.Format(parsed))
		if runtimeOnly {
			fmt.Println(" (runtime only)")
		} else {
			fmt.Println(" (persisted)")
		}
		return nil
	},
}

// applyRuntimeField sends a runtime-config update to the server for a provider-scoped field.
func applyRuntimeField(provider string, fd *config.ConfigFieldDescriptor, value any) error {
	client, err := NewAdminClient(5*time.Second, provider)
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}

	// Build POST to /api/runtime-config
	payloadMap := map[string]interface{}{"key": fd.Key}
	switch v := value.(type) {
	case int:
		payloadMap["value"] = float64(v)
	case float64:
		payloadMap["value"] = v
	default:
		payloadMap["value"] = value
	}
	payloadBytes, _ := json.Marshal(payloadMap)

	path := "/api/runtime-config"
	if provider != "" && provider != "all" {
		path += "?provider=" + url.QueryEscape(provider)
	} else if provider == "all" {
		path += "?provider=all"
	}

	resp, err := client.Post(path, "application/json", bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// persistFieldToToml writes a parsed value into the TOML config file.
// For provider-scoped fields, the value goes into tc.Provider[provider].
// For global fields, the value goes into tc directly.
func persistFieldToToml(provider string, fd *config.ConfigFieldDescriptor, value any) error {
	source, err := config.XDGConfigPath()
	if err != nil {
		return fmt.Errorf("persistFieldToToml: %w", err)
	}
	tc, err := config.LoadTomlConfig(source)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("persistFieldToToml: %w", err)
	}
	if tc == nil {
		tc = &config.TomlConfig{Provider: make(map[string]*config.Config)}
	}

	if fd.Scope == config.FieldScopeProvider {
		// Provider-scoped: ensure provider entry exists
		targetConfig, ok := tc.Provider[provider]
		if !ok {
			targetConfig = &config.Config{}
			tc.Provider[provider] = targetConfig
		}
		fd.Persist(tc, provider, targetConfig, value)
	} else {
		// Global field: persist directly into tc
		fd.Persist(tc, "", nil, value)
	}

	dir := filepath.Dir(source)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}
	return config.SaveTomlConfig(tc, source)
}

// maskSensitiveValue returns a safe representation for sensitive fields
// (e.g. admin_token). Non-sensitive fields pass through unchanged.
func maskSensitiveValue(fd *config.ConfigFieldDescriptor, val any) string {
	if fd.Key == "admin_token" {
		if s, ok := val.(string); ok && s != "" {
			return "(set)"
		}
		return "(not set)"
	}
	if fd.Key == "keys_file" {
		if s, ok := val.(string); ok && s != "" {
			return "(set)"
		}
		return "(not set)"
	}
	return fd.Format(val)
}

// getFieldValue reads a provider-scoped field value from a loaded TOML config.
// Falls back to the field's default if the config is missing or unset.
func getFieldValue(tc *config.TomlConfig, provider string, fd *config.ConfigFieldDescriptor) (any, error) {
	if tc != nil {
		if p, ok := tc.Provider[provider]; ok && p != nil {
			switch fd.Key {
			case "target":
				return p.TargetBase, nil
			case "cooldown_sec":
				return p.CooldownSec, nil
			case "max_retries":
				return p.MaxRetries, nil
			case "backoff_cap_sec":
				return p.BackoffCapSec, nil
			case "backoff_multiplier":
				return p.BackoffMultiplier, nil
			case "cb_reset_sec":
				return p.CBResetSec, nil
			case "upstream_cb_threshold":
				return p.UpstreamCBThreshold, nil
			case "http_timeout_sec":
				return p.HTTPTimeoutSec, nil
			case "log_level":
				return p.LogLevel, nil
			case "health_check_interval_sec":
				return p.HealthCheckIntervalSec, nil
			case "admin_token":
				if p.AdminToken != "" {
					return p.AdminToken, nil
				}
				return "", nil
			case "disable_thinking":
				return p.DisableThinking, nil
			case "genai_model":
				return p.GenaiModel, nil
			case "keys_file":
				return p.KeysFile, nil
			case "key_selection":
				return p.KeySelection, nil
			}
		}
	}
	// Fall back to default value
	return config.ParseDefault(fd)
}

// getGlobalFieldValue reads a global-scoped field value from a loaded TOML config.
func getGlobalFieldValue(tc *config.TomlConfig, fd *config.ConfigFieldDescriptor) (any, error) {
	if tc != nil {
		switch fd.Key {
		case "port":
			return tc.Port, nil
		case "log_file":
			return tc.LogFile, nil
		}
	}
	return config.ParseDefault(fd)
}
