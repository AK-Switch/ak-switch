package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// respondJSON writes a JSON response with the given status code and data.
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// parseKeyIndex extracts and validates a key index from the request path.
// The API uses 1-based indices (user-facing), returns 0-based for internal use.
// Returns an error if the index is missing, non-numeric, or < 1.
func parseKeyIndex(r *http.Request) (int, error) {
	raw := r.PathValue("index")
	idx, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid key index %q: must be a positive integer", raw)
	}
	if idx < 1 {
		return 0, fmt.Errorf("invalid key index %d: must be >= 1", idx)
	}
	return idx - 1, nil // convert to 0-based
}

// filterEmpty removes empty strings from a slice.
func filterEmpty(ss []string) []string {
	filtered := make([]string, 0, len(ss))
	for _, s := range ss {
		if s != "" {
			filtered = append(filtered, s)
		}
	}
	return filtered
}
