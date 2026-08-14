package server

import (
	"akswitch/internal/logentry"
	"encoding/json"
	"net/http"
	"time"
)

// ── Keys Handler ────────────────────────────────────────

func (api *AdminAPI) keysHandler(w http.ResponseWriter, r *http.Request) {
	ps, errMsg := api.resolveProvider(r)
	if ps == nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
		return
	}

	if r.Method == http.MethodPost || r.Method == http.MethodDelete {
		if !api.checkAdminToken(w, r, ps.Name()) {
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		keys := ps.PoolKeys()
		now := time.Now()
		result := make([]map[string]interface{}, len(keys))
		for i := range keys {
			ps.PoolCleanupOldRequests(i)
			nameVal, _ := ps.PoolName(i)
			result[i] = map[string]interface{}{
				"index":       i + 1,
				"key":         logentry.MaskKey(keys[i]),
				"status":      ps.PoolKeyStatusLabel(i, now),
				"requests_1m": ps.PoolRequestsInLastMinute(i),
				"name":        nameVal,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)

	case http.MethodPost:
		var body struct {
			Key     string `json:"key"`
			KeyName string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.Key == "" {
			http.Error(w, "key is required", http.StatusBadRequest)
			return
		}
		idx := ps.PoolAddKey(body.Key, body.KeyName)
		ps.PersistKeys()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"index": idx,
			"key":   logentry.MaskKey(body.Key),
			"name":  body.KeyName,
		})

	case http.MethodDelete:
		var body struct {
			Index int `json:"index"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.Index < 1 || body.Index > ps.PoolLen() {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid index"})
			return
		}
		if err := ps.PoolRemoveKey(body.Index - 1); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ps.PersistKeys()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "removed"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
