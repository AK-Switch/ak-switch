package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/logentry"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(providerCmd)
	providerCmd.AddCommand(providerAddCmd)
	providerCmd.AddCommand(providerListCmd)
	providerCmd.AddCommand(providerRemoveCmd)
	providerCmd.AddCommand(providerDefaultCmd)
	providerCmd.AddCommand(providerInfoCmd)

	providerAddCmd.Flags().StringP("target", "t", "", "Upstream target URL (required)")
	providerAddCmd.Flags().IntP("port", "p", 0, "HTTP listen port (required for first provider)")
	providerAddCmd.Flags().StringP("genai", "g", "", "GenAI base URL (optional)")
	providerAddCmd.Flags().IntP("cooldown-sec", "c", 60, "Cooldown seconds after rate-limit")
	providerAddCmd.Flags().IntP("max-retries", "r", 3, "Max retry attempts for upstream")
	providerAddCmd.Flags().Bool("default", false, "Set this provider as the default")
}

var providerCmd = &cobra.Command{
	Use:   "provider",
	Short: "Manage providers",
	Long:  `Add, list, and remove provider configurations in config.toml.`,
}

var providerAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a new provider",
	Long: `Add a new provider to the TOML configuration.

The --target flag is required. --port is required for the first provider;
subsequent providers reuse the existing port and --port can be omitted.

Example:
  akswitch provider add nvidia --target https://integrate.api.nvidia.com/v1 --port 3002
  akswitch provider add sensenova --target https://api.sensenova.com/v1`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		target, _ := cmd.Flags().GetString("target")
		port, _ := cmd.Flags().GetInt("port")
		genai, _ := cmd.Flags().GetString("genai")
		cooldown, _ := cmd.Flags().GetInt("cooldown-sec")
		maxRetries, _ := cmd.Flags().GetInt("max-retries")

		if target == "" {
			return fmt.Errorf("--target/-t is required")
		}

		source, err := config.XDGConfigPath()
		if err != nil {
			return fmt.Errorf("cannot determine XDG config path: %w", err)
		}

		// Load existing config or create a fresh one
		tc, err := config.LoadTomlConfig(source)
		if err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to load config: %w", err)
			}
			tc = &config.TomlConfig{
				Provider: make(map[string]*config.Config),
			}
		}

		// Check for duplicate
		if _, exists := tc.Provider[name]; exists {
			return fmt.Errorf("provider %q already exists in %s", name, source)
		}

		// Port: first provider must set it; subsequent providers reuse the existing one
		if port == 0 {
			if tc.Port == 0 {
				return fmt.Errorf("--port/-p is required for the first provider")
			}
			port = tc.Port // reuse existing port
		} else if tc.Port == 0 {
			// First provider with a port — set it
			tc.Port = port
		}
		// If both port > 0 and tc.Port > 0, user explicitly passed --port;
		// we don't override tc.Port (first provider's port wins).

		// Add new provider
		tc.Provider[name] = &config.Config{
			TargetBase:  target,
			GenaiBase:   genai,
			CooldownSec: cooldown,
			MaxRetries:  maxRetries,
		}

		// Ensure directory exists
		dir := filepath.Dir(source)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory %s: %w", dir, err)
		}

		// Check os.Args for --default to avoid cobra flag persistence across test runs
		hasDefaultFlag := false
		for _, a := range os.Args {
			if a == "--default" {
				hasDefaultFlag = true
				break
			}
		}
		if hasDefaultFlag {
			tc.DefaultProvider = name
			config.DefaultProviderName = name
		}

		// Save
		if err := config.SaveTomlConfig(tc, source); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		if hasDefaultFlag {
			fmt.Printf("Provider %q added to %s (default)\n", name, source)
		} else {
			fmt.Printf("Provider %q added to %s\n", name, source)
		}
		return nil
	},
}

var providerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all providers",
	Long: `Display all configured providers and their settings from config.toml.

Example output:
  Providers (from /home/user/.config/akswitch/config.toml):
    NAME        TARGET                                            PORT
    nvidia      https://integrate.api.nvidia.com/v1               3002
    sensenova   https://api.sensenova.com/v1                      3001`,
	RunE: func(cmd *cobra.Command, args []string) error {
		source, err := config.XDGConfigPath()
		if err != nil {
			return fmt.Errorf("failed to determine XDG config path: %w", err)
		}
		if _, statErr := os.Stat(source); statErr != nil {
			return fmt.Errorf("no configuration file found at %s", source)
		}

		tc, err := config.LoadTomlConfig(source)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if len(tc.Provider) == 0 {
			fmt.Printf("No providers configured in %s\n", source)
			return nil
		}

		// Sort names for deterministic output
		names := make([]string, 0, len(tc.Provider))
		for n := range tc.Provider {
			names = append(names, n)
		}
		sort.Strings(names)

		fmt.Printf("Providers (from %s):\n", source)
		fmt.Printf("  %-12s %-50s %s\n", "NAME", "TARGET", "PORT")
		for _, n := range names {
			p := tc.Provider[n]
			defaultMark := ""
			if n == tc.DefaultProvider {
				defaultMark = "  (default)"
			}
			fmt.Printf("  %-12s %-50s %d%s\n", n, p.TargetBase, tc.Port, defaultMark)
		}

		return nil
	},
}

var providerRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a provider",
	Long: `Remove a provider from the TOML configuration.

This only removes the provider configuration; any associated keys file
is NOT deleted. Use 'akswitch key remove' to manage individual keys.

Example:
  akswitch provider remove nvidia`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		source, err := config.XDGConfigPath()
		if err != nil {
			return fmt.Errorf("failed to determine XDG config path: %w", err)
		}
		if _, statErr := os.Stat(source); statErr != nil {
			return fmt.Errorf("no configuration file found at %s", source)
		}

		tc, err := config.LoadTomlConfig(source)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if _, exists := tc.Provider[name]; !exists {
			return fmt.Errorf("provider %q not found in %s", name, source)
		}

		delete(tc.Provider, name)

		// If the removed provider was the default, clear it
		if tc.DefaultProvider == name {
			tc.DefaultProvider = ""
			config.DefaultProviderName = ""
			fmt.Printf("Default provider cleared (was %q)\n", name)
		}

		if err := config.SaveTomlConfig(tc, source); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("Provider %q removed from %s\n", name, source)
		fmt.Println("Note: the keys file for this provider was not removed (if any)")
		return nil
	},
}

var providerDefaultCmd = &cobra.Command{
	Use:   "default <name>",
	Short: "Set the default provider",
	Long: `Set a provider as the default, which ` + "`" + `akswitch start` + "`" + ` will start
when no --provider or --all flag is given.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		source, err := config.XDGConfigPath()
		if err != nil {
			return fmt.Errorf("failed to determine XDG config path: %w", err)
		}
		if _, statErr := os.Stat(source); statErr != nil {
			return fmt.Errorf("no configuration file found at %s", source)
		}

		tc, err := config.LoadTomlConfig(source)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Verify the provider exists
		if _, exists := tc.Provider[name]; !exists {
			return fmt.Errorf("provider %q not found in %s", name, source)
		}

		tc.DefaultProvider = name
		config.DefaultProviderName = name

		if err := config.SaveTomlConfig(tc, source); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("Default provider set to %q\n", name)
		return nil
	},
}

var providerInfoCmd = &cobra.Command{
	Use:   "info <name>",
	Short: "Show detailed information about a provider",
	Long: `Display detailed configuration and status for a single provider.

Combines config settings, key summary, and runtime status (when server is running)
in one output.

Example:
  akswitch provider info nvidia
  akswitch provider info sensenova`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		source, err := config.XDGConfigPath()
		if err != nil {
			return fmt.Errorf("failed to determine XDG config path: %w", err)
		}
		if _, statErr := os.Stat(source); statErr != nil {
			return fmt.Errorf("no configuration file found at %s", source)
		}

		// Load all providers from TOML
		providers, err := config.LoadAllTomlProviders(source)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		// Find the provider
		cfg, exists := providers[name]
		if !exists {
			return fmt.Errorf("provider %q not found in %s", name, source)
		}

		sanitized := cfg.Sanitized()

		// Header
		defaultMark := ""
		if name == config.DefaultProviderName {
			defaultMark = "  (default)"
		}
		fmt.Printf("Provider: %s%s\n", name, defaultMark)

		// Runtime status (try server)
		client := &http.Client{Timeout: 3 * time.Second}
		port := detectServerPort()
		host := detectServerHost()

		healthURL := fmt.Sprintf("http://%s:%d/health", host, port)
		healthReq, err := http.NewRequest(http.MethodGet, healthURL, nil)
		if err == nil {
			if token, tokErr := loadAdminTokenFromConfig(); tokErr == nil && token != "" {
				healthReq.Header.Set("X-Admin-Token", token)
			}
		}
		healthResp, err := client.Do(healthReq)
		if err == nil {
			healthBody, _ := io.ReadAll(healthResp.Body)
			healthResp.Body.Close()
			if healthResp.StatusCode == http.StatusUnauthorized || healthResp.StatusCode == http.StatusForbidden {
				fmt.Fprintf(os.Stderr, "health check auth failed (HTTP %d): check X-Admin-Token in server config\n", healthResp.StatusCode)
				fmt.Printf("  Status:  auth required\n")
			} else if healthResp.StatusCode == http.StatusOK {
				var healthData map[string]interface{}
				if json.Unmarshal(healthBody, &healthData) == nil {
					if details, ok := healthData["details"]; ok {
						if det, ok2 := details.(map[string]interface{}); ok2 {
							if info, ok3 := det[name]; ok3 {
								if inf, ok4 := info.(map[string]interface{}); ok4 {
									cbState := "unknown"
									if cs, ok5 := inf["upstream_cb_state"]; ok5 {
										cbState = fmt.Sprintf("%v", cs)
									}
									fmt.Printf("  Status:  running  →  http://%s:%d\n", host, port)
									fmt.Printf("  CB:      %s\n", cbState)
								}
							}
						}
					}
				}
			} else {
				fmt.Println("  Status:  not running")
			}
		} else {
			fmt.Println("  Status:  not running")
		}

		// Stats
		statsURL := fmt.Sprintf("http://%s:%d/api/stats", host, port)
		statsReq, _ := http.NewRequest(http.MethodGet, statsURL, nil)
		if token, tokErr := loadAdminTokenFromConfig(); tokErr == nil && token != "" {
			statsReq.Header.Set("X-Admin-Token", token)
		}
		statsResp, err := client.Do(statsReq)
		if err == nil {
			statsBody, _ := io.ReadAll(statsResp.Body)
			statsResp.Body.Close()
			var stats map[string]interface{}
			if json.Unmarshal(statsBody, &stats) == nil {
				fmt.Printf("  Requests: %v (success: %v, failed: %v)\n",
					stats["total_requests"], stats["successful_requests"], stats["failed_requests"])
			}
		}

		// Config section
		fmt.Println("\n  Config:")
		fmt.Printf("    Target:  %s\n", sanitized.TargetBase)
		fmt.Printf("    Port:    %d\n", sanitized.Port)
		if sanitized.GenaiBase != "" {
			fmt.Printf("    GenAI:   %s\n", sanitized.GenaiBase)
		}
		if sanitized.AdminToken != "" {
			fmt.Println("    Admin token: (set)")
		}

		// Tuning section
		fmt.Println("\n  Tuning:")
		fmt.Printf("    Max retries:        %d\n", sanitized.MaxRetries)
		fmt.Printf("    Cooldown:           %ds\n", sanitized.CooldownSec)
		fmt.Printf("    Backoff cap:        %ds\n", sanitized.BackoffCapSec)
		fmt.Printf("    Backoff multiplier: %.1f\n", sanitized.BackoffMultiplier)
		fmt.Printf("    CB threshold:       %d\n", sanitized.UpstreamCBThreshold)
		fmt.Printf("    CB reset:           %ds\n", sanitized.CBResetSec)

		// Health check section
		fmt.Println("\n  Health check:")
		fmt.Printf("    Interval:  %ds\n", sanitized.HealthCheckIntervalSec)
		fmt.Printf("    Path:      %s\n", sanitized.HealthCheckPath)
		fmt.Printf("    Timeout:   %ds\n", sanitized.HealthCheckTimeoutSec)

		// Keys section
		store, keyErr := keypool.LoadKeys(name)
		fmt.Println("\n  Keys:")
		if keyErr != nil || store == nil || len(store.Keys) == 0 {
			fmt.Println("    (no keys configured)")
		} else {
			total := len(store.Keys)
			active := 0
			disabled := 0
			for _, entry := range store.Keys {
				if entry.Disabled {
					disabled++
				} else {
					active++
				}
			}
			fmt.Printf("    Total: %d  Active: %d  Disabled: %d\n", total, active, disabled)
			for i, entry := range store.Keys {
				status := "active"
				if entry.Disabled {
					status = "disabled"
				}
				line := fmt.Sprintf("    [%d] %s  (%s)", i, logentry.MaskKey(entry.Key), status)
				if entry.Name != "" {
					line += fmt.Sprintf("  name: %s", entry.Name)
				}
				fmt.Println(line)
			}
		}

		return nil
	},
}
