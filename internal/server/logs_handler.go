package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// ── Logs Handler ────────────────────────────────────────

func (api *AdminAPI) logsHandler(w http.ResponseWriter, r *http.Request) {
	logFile := api.logManager.LogFilePath()
	if logFile == "" {
		respondJSON(w, http.StatusOK, []interface{}{})
		return
	}

	// Read only the last 1 MB of the log file to bound memory usage
	const maxRead = 1 << 20 // 1 MB
	fi, err := os.Stat(logFile)
	if err != nil {
		slog.Error("failed to stat log file", "path", logFile, "error", err)
		respondJSON(w, http.StatusOK, []interface{}{})
		return
	}
	fileSize := fi.Size()
	readOffset := fileSize - maxRead
	if readOffset < 0 {
		readOffset = 0
	}

	f, err := os.Open(logFile)
	if err != nil {
		slog.Error("failed to open log file", "path", logFile, "error", err)
		respondJSON(w, http.StatusOK, []interface{}{})
		return
	}
	defer func() { _ = f.Close() }()

	if readOffset > 0 {
		_, err = f.Seek(readOffset, 0)
		if err != nil {
			slog.Error("failed to seek in log file", "path", logFile, "error", err)
			respondJSON(w, http.StatusOK, []interface{}{})
			return
		}
		// Skip to the first complete line
		var buf [1]byte
		for {
			_, err := f.Read(buf[:])
			if err != nil {
				break
			}
			if buf[0] == '\n' {
				break
			}
		}
	}

	// Read only the portion of the file we need
	data := make([]byte, maxRead)
	// Re-read from the offset we calculated
	_, _ = f.Seek(readOffset, 0)
	// Skip first partial line again
	if readOffset > 0 {
		var buf2 [1]byte
		for {
			_, err := f.Read(buf2[:])
			if err != nil {
				break
			}
			if buf2[0] == '\n' {
				break
			}
		}
	}
	n, err := f.Read(data)
	if err != nil && err.Error() != "EOF" {
		slog.Error("failed to read log file", "path", logFile, "error", err)
		respondJSON(w, http.StatusOK, []interface{}{})
		return
	}
	dataStr := string(data[:n])

	// Parse the since parameter
	var sinceTime time.Time
	since := r.URL.Query().Get("since")
	if since != "" {
		var parseErr error
		sinceTime, parseErr = time.Parse(time.RFC3339, since)
		if parseErr != nil {
			sinceTime = time.Time{} // fallback to no filter
		}
	}

	var entries []map[string]interface{}
	lines := strings.Split(dataStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		// Include any request-related entry (has method and url fields)
		// This covers both "proxy success" and error entries (rate limited, auth rejected, etc.)
		if _, hasMethod := raw["method"]; !hasMethod {
			continue
		}
		if _, hasURL := raw["url"]; !hasURL {
			continue
		}

		// Parse timestamp for since filtering
		if !sinceTime.IsZero() {
			ts, _ := raw["time"].(string)
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				if t.Before(sinceTime) {
					continue
				}
			}
		}

		entry := map[string]interface{}{
			"timestamp":          raw["time"],
			"key_name":           raw["key_name"],
			"method":             raw["method"],
			"url":                raw["url"],
			"status":             raw["status"],
			"request_body_size":  raw["request_body_size"],
			"provider":           raw["provider"],
			"duration_ms":        raw["duration_ms"],
			"ttfb_ms":            raw["ttfb_ms"],
			"retry":              raw["retry"],
			"input_tokens":       raw["input_tokens"],
			"output_tokens":      raw["output_tokens"],
			"response_body_size": raw["response_body_size"],
		}
		// key_index was 1-based in the old LogEntry; slog logs 0-based
		if ki, ok := raw["key_index"].(float64); ok {
			entry["key_index"] = int(ki) + 1
		}
		// key field for dashboard compatibility (no longer stores actual key)
		entry["key"] = raw["key_name"]
		entries = append(entries, entry)
	}

	respondJSON(w, http.StatusOK, entries)
}
