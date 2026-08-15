// Package server provides the HTTP server, proxy, and management handlers for AK Switch.
package server

import (
	"strings"
)

// keyPrefixes are known API key prefixes to mask in debug logging.
var keyPrefixes = []string{"sk-", "nvapi-"}

// MaskSensitiveData scrubs potential API key patterns from a string for safe debug logging.
// It masks any word-like token starting with a known key prefix by replacing it with "***".
// It also truncates the result to maxLen bytes.
func MaskSensitiveData(data string, maxLen int) string {
	if len(data) > maxLen {
		data = data[:maxLen]
	}
	// Mask known API key prefixes
	result := data
	lower := strings.ToLower(data)
	for _, prefix := range keyPrefixes {
		idx := strings.Index(lower, prefix)
		for idx >= 0 {
			// Find end of token (word boundary)
			end := idx + len(prefix)
			for end < len(result) && (isAlphaNum(result[end]) || result[end] == '-' || result[end] == '_') {
				end++
			}
			if end > idx+len(prefix) {
				result = result[:idx] + "***" + result[end:]
				lower = strings.ToLower(result)
			}
			idx = strings.Index(lower, prefix)
		}
	}
	return result
}

func isAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
