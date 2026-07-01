package server

import (
	"encoding/json"
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
// Returns the 0-based index and true on success.
func parseKeyIndex(r *http.Request) (int, bool) {
	raw := r.PathValue("index")
	idx, err := strconv.Atoi(raw)
	if err != nil || idx < 1 {
		return 0, false
	}
	return idx - 1, true // convert to 0-based
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
