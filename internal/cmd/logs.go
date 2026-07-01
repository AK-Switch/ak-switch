package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"alvus/internal/config"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(logsCmd)
}

var logsCmd = &cobra.Command{
	Use:   "logs [provider]",
	Short: "Show request logs from running instances",
	Long:  `Display recent request logs from one or all running instances.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source, fromToml, err := config.DetectConfigSource("")
		if err != nil {
			return fmt.Errorf("failed to detect config source: %w", err)
		}
		if !fromToml {
			return fmt.Errorf("no TOML configuration found (expected at %s)", source)
		}
		if _, statErr := os.Stat(source); statErr != nil {
			return fmt.Errorf("no configuration file found at %s", source)
		}

		providers, err := config.LoadAllTomlProviders(source)
		if err != nil {
			return fmt.Errorf("failed to load providers: %w", err)
		}
		if len(providers) == 0 {
			fmt.Println("No providers configured")
			return nil
		}

		// Filter by provider name if specified
		filterName := ""
		if len(args) > 0 {
			filterName = args[0]
			if _, ok := providers[filterName]; !ok {
				return fmt.Errorf("provider %q not found in configuration", filterName)
			}
		}

		client := &http.Client{Timeout: 5 * time.Second}

		names := make([]string, 0, len(providers))
		for n := range providers {
			names = append(names, n)
		}
		sort.Strings(names)

		found := false
		for _, name := range names {
			if filterName != "" && name != filterName {
				continue
			}

			cfg := providers[name]
			logURL := fmt.Sprintf("http://127.0.0.1:%d/logs", cfg.Port)
			resp, err := client.Get(logURL)
			if err != nil {
				fmt.Printf("Provider %q (port %d): not reachable\n", name, cfg.Port)
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var entries []interface{}
			if err := json.Unmarshal(body, &entries); err != nil {
				fmt.Printf("Provider %q (port %d): failed to parse logs\n", name, cfg.Port)
				continue
			}

			found = true
			if len(entries) == 0 {
				fmt.Printf("Provider %q (port %d): no log entries\n", name, cfg.Port)
				continue
			}

			fmt.Printf("=== Provider %q (port %d) ===\n", name, cfg.Port)
			for _, entry := range entries {
				entryMap, ok := entry.(map[string]interface{})
				if !ok {
					continue
				}
				// Reformat: show timestamp, method, path, status
				method := getStrField(entryMap, "method", "?")
				path := getStrField(entryMap, "url", "?")
				status := getStrField(entryMap, "status", "?")
				ts := getStrField(entryMap, "timestamp", "?")
				fmt.Printf("  [%s] %s %s -> %s\n", ts, method, path, status)
			}
		}

		if !found && filterName != "" {
			fmt.Printf("No logs available for provider %q\n", filterName)
		}

		return nil
	},
}

func getStrField(m map[string]interface{}, key, fallback string) string {
	if v, ok := m[key]; ok {
		switch s := v.(type) {
		case string:
			return s
		case float64:
			return fmt.Sprintf("%.0f", s)
		}
	}
	return fallback
}