package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"akswitch/internal/config"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configViewCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)

	configInitCmd.Flags().StringP("path", "p", "", "Output path for config.toml (default: XDG config directory)")
	configSetCmd.Flags().Bool("persist", false, "Persist the change to the config file")
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
					TargetBase:  "https://api.example-a.com/v1",
					GenaiBase:   "https://api.example-a.com",
					CooldownSec: 60,
					MaxRetries:  3,
				},
				"example-b": {
					TargetBase:  "https://api.example-b.com/v1",
					CooldownSec: 30,
					MaxRetries:  5,
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
	Long: `Read the TOML configuration file and print its contents in a human-readable format.`,
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
			fmt.Printf("  GenAI base URL: %s\n", sanitized.GenaiBase)
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
	Long: `Display all runtime-configurable parameters and their current values.

	If a provider name is given, shows parameters for that provider only.
	Otherwise, shows the first (or only) provider.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := &http.Client{Timeout: 5 * time.Second}
		baseURL := fmt.Sprintf("http://%s:%d/api/runtime-config", detectServerHost(), detectServerPort())

		if len(args) > 0 {
			baseURL += "?provider=" + url.QueryEscape(args[0])
		}

		resp, err := client.Get(baseURL)
		if err != nil {
			return fmt.Errorf("server not reachable: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var result struct {
			Provider string                 `json:"provider"`
			Params   map[string]interface{} `json:"params"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		fmt.Printf("Provider: %s\n", result.Provider)
		fmt.Println()

		// Sort keys for deterministic output
		keys := make([]string, 0, len(result.Params))
		for k := range result.Params {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := result.Params[k]
			switch val := v.(type) {
			case float64:
				if val == float64(int(val)) {
					fmt.Printf("  %-25s %d\n", k+":", int(val))
				} else {
					fmt.Printf("  %-25s %.1f\n", k+":", val)
				}
			default:
				fmt.Printf("  %-25s %v\n", k+":", v)
			}
		}

		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key> [provider]",
	Short: "Get a runtime parameter",
	Long: `Display the current value of a single runtime-configurable parameter.

	Valid keys: http_timeout_sec, max_retries, cooldown_sec, backoff_cap_sec,
	backoff_multiplier, cb_reset_sec, upstream_cb_threshold, log_level

	Examples:
	  akswitch config get http_timeout_sec
	  akswitch config get max_retries sensenova`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := &http.Client{Timeout: 5 * time.Second}
		key := args[0]

		params := url.Values{}
		params.Set("key", key)
		if len(args) > 1 {
			params.Set("provider", args[1])
		}
		baseURL := fmt.Sprintf("http://%s:%d/api/runtime-config?%s", detectServerHost(), detectServerPort(), params.Encode())

		resp, err := client.Get(baseURL)
		if err != nil {
			return fmt.Errorf("server not reachable: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var result struct {
			Provider string      `json:"provider"`
			Key      string      `json:"key"`
			Value    interface{} `json:"value"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		switch val := result.Value.(type) {
		case float64:
			if val == float64(int(val)) {
				fmt.Printf("%d\n", int(val))
			} else {
				fmt.Printf("%.1f\n", val)
			}
		default:
			fmt.Printf("%v\n", result.Value)
		}

		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value> [provider]",
	Short: "Set a runtime parameter",
	Long: `Change a runtime-configurable parameter immediately.

	Use --persist to also write the change to the config file.

	Valid keys: http_timeout_sec, max_retries, cooldown_sec, backoff_cap_sec,
	backoff_multiplier, cb_reset_sec, upstream_cb_threshold, log_level

	Examples:
	  akswitch config set http_timeout_sec 60
	  akswitch config set max_retries 5 --persist
	  akswitch config set log_level debug sensenova`,
	Args: cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := &http.Client{Timeout: 5 * time.Second}
		key := args[0]
		value := args[1]

		persist, _ := cmd.Flags().GetBool("persist")

		params := url.Values{}
		if len(args) > 2 {
			params.Set("provider", args[2])
		}
		if persist {
			params.Set("persist", "true")
		}

		baseURL := fmt.Sprintf("http://%s:%d/api/runtime-config?%s", detectServerHost(), detectServerPort(), params.Encode())

		// Try to parse value as number first, fall back to string
		payload := fmt.Sprintf(`{"key":"%s","value":%s}`, key, value)
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			// Not a number, send as string
			payload = fmt.Sprintf(`{"key":"%s","value":"%s"}`, key, value)
		}

		resp, err := client.Post(baseURL, "application/json", strings.NewReader(payload))
		if err != nil {
			return fmt.Errorf("server not reachable: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			var errResult struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(body, &errResult) == nil {
				return fmt.Errorf("failed to set %s: %s", key, errResult.Error)
			}
			return fmt.Errorf("failed to set %s (HTTP %d)", key, resp.StatusCode)
		}

		var result struct {
			Provider  string      `json:"provider"`
			Key       string      `json:"key"`
			Value     interface{} `json:"value"`
			Persisted bool        `json:"persisted"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		switch val := result.Value.(type) {
		case float64:
			if val == float64(int(val)) {
				fmt.Printf("set %s = %d", result.Key, int(val))
			} else {
				fmt.Printf("set %s = %.1f", result.Key, val)
			}
		default:
			fmt.Printf("set %s = %v", result.Key, result.Value)
		}
		if result.Persisted {
			fmt.Println(" (persisted)")
		} else {
			fmt.Println()
		}

		return nil
	},
}