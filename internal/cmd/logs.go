package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	logsCmd.Flags().IntVar(&logsLast, "last", 0, "Show only the last N entries (0 = all)")
	logsCmd.Flags().StringVar(&logsSince, "since", "", "Show entries after this timestamp (RFC3339, e.g. 2026-07-14T00:00:00Z)")
	logsCmd.Flags().BoolVar(&logsVerbose, "verbose", false, "Show full request details (method, URL, body size)")
	rootCmd.AddCommand(logsCmd)
}

var logsLast int
var logsSince string
var logsVerbose bool

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show request logs",
	Long:  `Display recent request logs from the running akswitch server.`,
	Args:  cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := &http.Client{Timeout: 5 * time.Second}

		// Determine the server port from config or default
		port := detectServerPort()

		logURL := fmt.Sprintf("http://127.0.0.1:%d/logs", port)
		if logsSince != "" {
			logURL += "?" + url.Values{"since": {logsSince}}.Encode()
		}
		resp, err := client.Get(logURL)
		if err != nil {
			return fmt.Errorf("server not reachable at %s: %w", logURL, err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)

		var entries []interface{}
		if err := json.Unmarshal(body, &entries); err != nil {
			// Check if response is non-JSON (e.g., HTML from another service)
			if len(body) > 0 && body[0] != '{' && body[0] != '[' {
				return fmt.Errorf("server not running or returned unexpected response (HTTP %d)", resp.StatusCode)
			}
			return fmt.Errorf("failed to parse logs: %w", err)
		}

		if len(entries) == 0 {
			fmt.Println("No log entries")
			return nil
		}

		if logsLast > 0 && len(entries) > logsLast {
			entries = entries[len(entries)-logsLast:]
		}

		for _, entry := range entries {
			entryMap, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			fmt.Println(formatLogLine(entryMap, logsVerbose))
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

func formatLogLine(entry map[string]interface{}, verbose bool) string {
	method := getStrField(entry, "method", "?")
	path := getStrField(entry, "url", "?")
	status := getStrField(entry, "status", "?")
	ts := getStrField(entry, "timestamp", "?")
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		ts = t.Format("15:04:05.000")
	}

	provider := getStrField(entry, "provider", "")
	duration := getStrField(entry, "duration_ms", "")
	retry := getStrField(entry, "retry", "")
	keyName := getStrField(entry, "key_name", "")

	prefix := fmt.Sprintf("  [%s]", ts)

	if verbose {
		// Full debugging view: [ts] provider METHOD url -> status (retry N, durationMsms, key: name)
		if provider != "" {
			prefix += " " + provider
		}
		var extras []string
		if retry != "" && retry != "0" {
			extras = append(extras, "retry "+retry)
		}
		if duration != "" {
			extras = append(extras, duration+"ms")
		}
		if keyName != "" {
			extras = append(extras, "key: "+keyName)
		}
		extraStr := ""
		if len(extras) > 0 {
			extraStr = " (" + strings.Join(extras, ", ") + ")"
		}
		return fmt.Sprintf("%s %s %s -> %s%s", prefix, method, path, status, extraStr)
	}

	// Default user-friendly view: [HH:MM:SS.mmm] status (provider, key: name, durationMsms[, retry N])
	var extras []string
	if provider != "" {
		extras = append(extras, provider)
	}
	if keyName != "" {
		extras = append(extras, "key: "+keyName)
	}
	if duration != "" {
		extras = append(extras, duration+"ms")
	}
	if retry != "" && retry != "0" {
		extras = append(extras, "retry "+retry)
	}
	if len(extras) > 0 {
		return fmt.Sprintf("%s %s (%s)", prefix, status, strings.Join(extras, ", "))
	}
	return fmt.Sprintf("%s %s", prefix, status)
}
