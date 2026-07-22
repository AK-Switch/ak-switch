package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	logsCmd.Flags().IntVar(&logsLast, "last", 0, "Show only the last N entries (0 = all)")
	logsCmd.Flags().StringVar(&logsSince, "since", "", "Show entries after this timestamp (RFC3339, e.g. 2026-07-14T00:00:00Z)")
	logsCmd.Flags().BoolVar(&logsVerbose, "verbose", false, "Show full request details (method, URL, body size)")
	logsCmd.Flags().BoolVar(&logsCompact, "compact", false, "Use compact format (TTFB, total time, body sizes)")
	rootCmd.AddCommand(logsCmd)
}

var logsLast int
var logsSince string
var logsVerbose bool
var logsCompact bool

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show request logs",
	Long:  `Display recent request logs from the running akswitch server.`,
	Args:  cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := &http.Client{Timeout: 5 * time.Second}

		// Determine the server port from config or default
		port := detectServerPort()

		logURL := fmt.Sprintf("http://%s:%d/logs", detectServerHost(), port)
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
			mode := "default"
			if logsVerbose {
				mode = "verbose"
			}
			if logsCompact {
				mode = "compact"
			}
			fmt.Println(formatLogLine(entryMap, mode))
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

func formatLogLine(entry map[string]interface{}, mode string) string {
	ts := getStrField(entry, "timestamp", "?")
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		if mode == "compact" {
			ts = t.Format("15:04:05")
		} else {
			ts = t.Format("15:04:05.000")
		}
	}

	method := getStrField(entry, "method", "?")
	path := getStrField(entry, "url", "?")
	status := getStrField(entry, "status", "?")
	provider := getStrField(entry, "provider", "")
	duration := getStrField(entry, "duration_ms", "")
	retry := getStrField(entry, "retry", "")
	keyName := getStrField(entry, "key_name", "")

	prefix := fmt.Sprintf("  [%s]", ts)

	switch mode {
	case "compact":
		path = shortenURL(path)
		keyPart := "key: " + keyName
		if retry != "" && retry != "0" {
			keyPart += ", retry " + retry
		}

		var timingParts []string
		ttfb := getFloatField(entry, "ttfb_ms")
		if ttfb > 0 {
			timingParts = append(timingParts, "ttfb="+fmtDurationCompact(ttfb))
		}
		total := getFloatField(entry, "duration_ms")
		if total > 0 {
			timingParts = append(timingParts, "total="+fmtDurationCompact(total))
		}

		reqSize := getFloatField(entry, "request_body_size")
		respSize := getFloatField(entry, "response_body_size")
		reqStr := fmtSizeCompact(reqSize)
		if respSize > 0 {
			timingParts = append(timingParts, reqStr+"→"+fmtSizeCompact(respSize))
		} else {
			timingParts = append(timingParts, reqStr)
		}

		inTok := getIntField(entry, "input_tokens")
		outTok := getIntField(entry, "output_tokens")
		if inTok > 0 || outTok > 0 {
			timingParts = append(timingParts, fmt.Sprintf("tok=%d+%d", inTok, outTok))
		}

		timingStr := strings.Join(timingParts, " ")
		if provider != "" {
			return fmt.Sprintf("%s %s %s %s %s (%s) [%s]", prefix, status, provider, method, path, keyPart, timingStr)
		}
		return fmt.Sprintf("%s %s %s %s (%s) [%s]", prefix, status, method, path, keyPart, timingStr)

	case "verbose":
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

	default:
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
}

func getFloatField(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case string:
			f, _ := strconv.ParseFloat(val, 64)
			return f
		case int:
			return float64(val)
		case int64:
			return float64(val)
		}
	}
	return 0
}

func getIntField(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return int64(val)
		case string:
			i, _ := strconv.ParseInt(val, 10, 64)
			return i
		case int:
			return int64(val)
		case int64:
			return val
		}
	}
	return 0
}

func fmtDurationCompact(ms float64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.1fs", ms/1000)
	}
	return fmt.Sprintf("%.0fms", ms)
}

func fmtSizeCompact(bytes float64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1fGB", bytes/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1fMB", bytes/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.0fKB", bytes/float64(1<<10))
	default:
		return "0KB"
	}
}

func shortenURL(rawURL string) string {
	if idx := strings.Index(rawURL, "://"); idx >= 0 {
		if pathIdx := strings.Index(rawURL[idx+3:], "/"); pathIdx >= 0 {
			return rawURL[idx+3+pathIdx:]
		}
	}
	return rawURL
}
