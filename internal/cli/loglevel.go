package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(logLevelCmd)
}

var logLevelCmd = &cobra.Command{
	Use:   "log-level [level]",
	Short: "Show or set the log level",
	Long: `Display the current log level, or set it to a new value.

Valid levels: debug, info, warn, error

Examples:
  akswitch log-level       # show current level
  akswitch log-level debug # set level to debug`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := &http.Client{Timeout: 5 * time.Second}
		port := detectServerPort()
		host := detectServerHost()
		baseURL := fmt.Sprintf("http://%s:%d/api/log-level", host, port)

		if len(args) == 0 {
			// GET — show current log level
			resp, err := client.Get(baseURL)
			if err != nil {
				return fmt.Errorf("server not reachable at %s:%d: %w", host, port, err)
			}
			defer func() { _ = resp.Body.Close() }()

			body, _ := io.ReadAll(resp.Body)
			var result struct {
				Level string `json:"level"`
			}
			if err := json.Unmarshal(body, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			fmt.Println(result.Level)
			return nil
		}

		// POST — set log level
		level := strings.TrimSpace(strings.ToLower(args[0]))
		validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
		if !validLevels[level] {
			return fmt.Errorf("invalid log level %q, use: debug, info, warn, error", args[0])
		}

		payload := fmt.Sprintf(`{"level":"%s"}`, level)
		resp, err := client.Post(baseURL, "application/json", strings.NewReader(payload))
		if err != nil {
			return fmt.Errorf("server not reachable at %s:%d: %w", host, port, err)
		}
		defer func() { _ = resp.Body.Close() }()

		body, _ := io.ReadAll(resp.Body)
		var result struct {
			Level string `json:"level"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
		fmt.Println(result.Level)
		return nil
	},
}
