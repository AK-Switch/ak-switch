// Package logentry defines the LogEntry data model and key masking utilities.
//
// LogEntry is the core data structure for the logging system, representing
// a single proxy request with its metadata. MaskKey provides consistent
// key sanitization across the entire codebase.
package logentry

// LogEntry represents a single proxy request log entry.
type LogEntry struct {
	Timestamp       string `json:"timestamp"`
	Key             string `json:"key"`
	KeyIndex        int    `json:"key_index"`
	KeyName         string `json:"key_name"`
	Method          string `json:"method"`
	URL             string `json:"url"`
	Status          int    `json:"status"`
	RequestBodySize int    `json:"request_body_size"`
	DurationMs      int64  `json:"duration_ms"`
	TtfbMs          int64  `json:"ttfb_ms"`
	Retries         int    `json:"retry"`
	Provider        string `json:"provider,omitempty"`
	InputTokens     int    `json:"input_tokens,omitempty"`
	OutputTokens    int    `json:"output_tokens,omitempty"`
}

// MaskKey returns a masked version of the API key for display purposes.
// It shows the first 4 and last 4 characters, separated by "...".
// Keys 12 characters or shorter are fully masked as "****".
func MaskKey(key string) string {
	if len(key) <= 12 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}