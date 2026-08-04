package server

import (
	"akswitch/internal/circuitbreaker"
	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/logentry"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// ── Interfaces ──────────────────────────────────────────

// ProviderLookup defines the interface AdminAPI needs to access provider data.
type ProviderLookup interface {
	LookupProvider(name string) *ProviderState
	FirstProvider() *ProviderState
	ForEach(fn func(name string, ps *ProviderState))
	ProviderNames() []string
	ReloadConfig(providers map[string]*config.Config, logManager *LogManager)
}

// ── AdminAPI ────────────────────────────────────────────

// AdminAPI holds all management API handlers.
type AdminAPI struct {
	pm            ProviderLookup
	logManager    *LogManager
	dashboardHTML string
	startTime     time.Time

	// Key operation handlers (initialized in RegisterRoutes)
	disableKeyHandler  http.HandlerFunc
	enableKeyHandler   http.HandlerFunc
	cooldownKeyHandler http.HandlerFunc
	deleteKeyHandler   http.HandlerFunc
}

// NewAdminAPI creates a new AdminAPI.
func NewAdminAPI(pm ProviderLookup, logManager *LogManager, dashboardHTML string, startTime time.Time) *AdminAPI {
	api := &AdminAPI{pm: pm, logManager: logManager, dashboardHTML: dashboardHTML, startTime: startTime}
	api.disableKeyHandler = api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Disable(idx)
	})
	api.enableKeyHandler = api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Enable(idx)
	})
	api.cooldownKeyHandler = api.keyOperationHandler(func(pool *keypool.KeyPool, cfg *config.Config, idx int) error {
		return pool.Cooldown(idx, time.Duration(cfg.CooldownSec)*time.Second)
	})
	api.deleteKeyHandler = api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.RemoveKey(idx)
	})
	return api
}

// RegisterRoutes registers all admin API endpoints on the mux.
func (api *AdminAPI) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", api.healthHandler)
	mux.HandleFunc("/logs", api.logsHandler)
	mux.HandleFunc("/dashboard", api.dashboardHandler)
	mux.HandleFunc("/clear", api.clearHandler)
	mux.HandleFunc("/api/config", api.configHandler)
	mux.HandleFunc("/api/keys", api.keysHandler)
	mux.HandleFunc("POST /api/keys/{index}/disable", api.disableKeyHandler)
	mux.HandleFunc("POST /api/keys/{index}/enable", api.enableKeyHandler)
	mux.HandleFunc("PUT /api/keys/{index}/cooldown", api.cooldownKeyHandler)
	mux.HandleFunc("DELETE /api/keys/{index}", api.deleteKeyHandler)
	mux.HandleFunc("GET /api/stats", api.statsHandler)
	mux.HandleFunc("POST /api/stats/reset-upstream-cb", api.upstreamCBResetHandler)
	mux.HandleFunc("POST /api/reload", api.reloadHandler)
	mux.HandleFunc("/api/log-level", api.logLevelHandler)
	mux.HandleFunc("/api/runtime-config", api.runtimeConfigHandler)
	mux.HandleFunc("/sw.js", api.swHandler)
}

// ── SW Handler ──────────────────────────────────────────

func (api *AdminAPI) swHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// ── Log Level Handler ───────────────────────────────────

func (api *AdminAPI) logLevelHandler(w http.ResponseWriter, r *http.Request) {
	if !api.checkAnyAdminToken(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		respondJSON(w, http.StatusOK, map[string]string{"level": api.logManager.CurrentLevel()})

	case http.MethodPost:
		var body struct {
			Level string `json:"level"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		body.Level = strings.TrimSpace(strings.ToLower(body.Level))
		switch body.Level {
		case "debug", "info", "warn", "error":
			api.logManager.ApplyLevel(body.Level)
			respondJSON(w, http.StatusOK, map[string]string{"level": body.Level})
		default:
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log level, use: debug, info, warn, error"})
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// ── Config Handler ──────────────────────────────────────

func (api *AdminAPI) configHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Return config for a specific provider or all providers
		ps, _ := api.resolveProvider(r)
		if ps == nil {
			// Return all providers
			result := make(map[string]config.ConfigPayload)
			api.pm.ForEach(func(name string, p *ProviderState) {
				keys := p.Pool.Keys()
				maskedKeys := make([]string, len(keys))
				for i, k := range keys {
					maskedKeys[i] = logentry.MaskKey(k)
				}
				result[name] = config.ConfigPayload{
					TargetBase: p.Config.TargetBase,
					Keys:       maskedKeys,
				}
			})
			respondJSON(w, http.StatusOK, map[string]interface{}{"providers": result})
			return
		}

		keys := ps.Pool.Keys()
		maskedKeys := make([]string, len(keys))
		for i, k := range keys {
			maskedKeys[i] = logentry.MaskKey(k)
		}
		respondJSON(w, http.StatusOK, config.ConfigPayload{
			TargetBase: ps.Config.TargetBase,
			Keys:       maskedKeys,
		})
		return
	}

	// POST is removed — no more .env writing
	w.WriteHeader(http.StatusMethodNotAllowed)
}

// ── Keys Handler ────────────────────────────────────────

func (api *AdminAPI) keysHandler(w http.ResponseWriter, r *http.Request) {
	ps, errMsg := api.resolveProvider(r)
	if ps == nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
		return
	}

	pool := ps.Pool

	if r.Method == http.MethodPost || r.Method == http.MethodDelete {
		if !api.checkAdminToken(w, r, ps.Name) {
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		keys := pool.Keys()
		now := time.Now()
		result := make([]map[string]interface{}, len(keys))
		for i := range keys {
			pool.CleanupOldRequests(i)
			nameVal, _ := pool.Name(i)
			result[i] = map[string]interface{}{
				"index":       i + 1,
				"key":         logentry.MaskKey(keys[i]),
				"status":      pool.KeyStatusLabel(i, now),
				"requests_1m": pool.RequestsInLastMinute(i),
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
		idx := pool.AddKey(body.Key, body.KeyName)
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
		if body.Index < 1 || body.Index > len(pool.Keys()) {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid index"})
			return
		}
		if err := pool.RemoveKey(body.Index - 1); err != nil {
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

// ── Health Handler ──────────────────────────────────────

func (api *AdminAPI) healthHandler(w http.ResponseWriter, r *http.Request) {
	if !api.checkAnyAdminToken(w, r) {
		return
	}

	pName := r.URL.Query().Get("provider")
	type providerHealth struct {
		Status            string `json:"status"`
		Keys              int    `json:"keys"`
		UpstreamCBState   string `json:"upstream_cb_state"`
		LastHealthCheck   string `json:"last_health_check,omitempty"`
		LastHealthCheckOK *bool  `json:"last_health_check_ok,omitempty"`
	}

	result := make(map[string]*providerHealth)
	overallOK := true

	api.pm.ForEach(func(name string, ps *ProviderState) {
		if pName != "" && name != pName {
			return
		}

		upCB := ps.Proxy.upCB

		var cbState string
		switch upCB.State() {
		case circuitbreaker.Closed:
			cbState = "closed"
		case circuitbreaker.Open:
			cbState = "open"
		case circuitbreaker.HalfOpen:
			cbState = "half_open"
		default:
			cbState = "unknown"
		}

		lastCheckTime, lastCheckOK := ps.LastHealthCheck()
		var lastCheckISO string
		if !lastCheckTime.IsZero() {
			lastCheckISO = lastCheckTime.Format(time.RFC3339)
		}
		var lastCheckResult *bool
		if !lastCheckTime.IsZero() {
			lastCheckResult = &lastCheckOK
		}

		ph := &providerHealth{
			Status:            "ok",
			Keys:              len(ps.Pool.Keys()),
			UpstreamCBState:   cbState,
			LastHealthCheck:   lastCheckISO,
			LastHealthCheckOK: lastCheckResult,
		}
		result[name] = ph

		if cbState != "closed" || (lastCheckResult != nil && !*lastCheckResult) {
			overallOK = false
		}
	})

	status := "ok"
	if !overallOK {
		status = "degraded"
	}

	if pName != "" && len(result) == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("provider %q not found", pName)})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    status,
		"providers": len(result),
		"details":   result,
	})
}

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

// ── Dashboard Handler ───────────────────────────────────

func (api *AdminAPI) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(api.dashboardHTML))
}

// ── Clear Handler ───────────────────────────────────────

func (api *AdminAPI) clearHandler(w http.ResponseWriter, r *http.Request) {
	if !api.checkAnyAdminToken(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// Ring buffer removed — no-op
	respondJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// ── Stats Handler ───────────────────────────────────────

func (api *AdminAPI) statsHandler(w http.ResponseWriter, r *http.Request) {
	if !api.checkAnyAdminToken(w, r) {
		return
	}
	pName := r.URL.Query().Get("provider")
	if pName != "" {
		ps, errMsg := api.resolveProviderByName(pName)
		if ps == nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
			return
		}
	}

	var totalActive, totalCooling, totalDisabled int
	api.pm.ForEach(func(name string, ps *ProviderState) {
		if pName != "" && name != pName {
			return
		}
		totalActive += ps.Pool.ActiveCount()
		totalCooling += ps.Pool.CoolingCount()
		totalDisabled += ps.Pool.DisabledCount()
	})

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"active_keys":    totalActive,
		"cooling_keys":   totalCooling,
		"disabled_keys":  totalDisabled,
		"uptime_seconds": time.Since(api.startTime).Seconds(),
	})
}

// ── Upstream CB Reset Handler ───────────────────────────

func (api *AdminAPI) upstreamCBResetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	pName := r.URL.Query().Get("provider")
	ps, errMsg := api.resolveProviderByName(pName)
	if ps == nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
		return
	}

	if !api.checkAdminToken(w, r, ps.Name) {
		return
	}

	ps.Proxy.upCB.Reset()
	slog.Info("upstream circuit breaker reset", "provider", ps.Name)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"provider": ps.Name,
		"reset":    true,
	})
}

// ── Reload Handler ──────────────────────────────────────

func (api *AdminAPI) reloadHandler(w http.ResponseWriter, r *http.Request) {
	if !api.checkAnyAdminToken(w, r) {
		return
	}

	// Reload TOML config from the XDG path
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to determine config path: " + err.Error(),
		})
		return
	}

	providers, err := config.LoadAllTomlProviders(xdgPath)
	if err != nil {
		slog.Warn("reload failed", "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	api.pm.ReloadConfig(providers, api.logManager)

	slog.Info("config reloaded", "providers", len(providers))
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ── Runtime Config Handlers ─────────────────────────────

func (api *AdminAPI) runtimeConfigHandler(w http.ResponseWriter, r *http.Request) {
	if !api.checkAnyAdminToken(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		api.handleRuntimeConfigGet(w, r)
	case http.MethodPost:
		api.handleRuntimeConfigSet(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (api *AdminAPI) handleRuntimeConfigGet(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	pName := r.URL.Query().Get("provider")

	if pName != "" {
		ps := api.pm.LookupProvider(pName)
		if ps == nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("provider %q not found", pName)})
			return
		}
		params := api.getRuntimeParams(ps)
		if key != "" {
			val, ok := params[key]
			if !ok {
				respondJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unknown key %q", key)})
				return
			}
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"provider": ps.Name,
				"key":      key,
				"value":    val,
			})
			return
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"provider": ps.Name,
			"params":   params,
		})
		return
	}

	// All providers
	result := make(map[string]map[string]interface{})
	api.pm.ForEach(func(name string, ps *ProviderState) {
		result[name] = api.getRuntimeParams(ps)
	})

	if key != "" {
		// Key specified without provider — return from first provider that has it
		names := api.pm.ProviderNames()
		for _, name := range names {
			ps := api.pm.LookupProvider(name)
			params := api.getRuntimeParams(ps)
			val, ok := params[key]
			if !ok {
				respondJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unknown key %q", key)})
				return
			}
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"provider": name,
				"key":      key,
				"value":    val,
			})
			return
		}
	}

	respondJSON(w, http.StatusOK, result)
}

func (api *AdminAPI) handleRuntimeConfigSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key   string      `json:"key"`
		Value interface{} `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if body.Key == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}

	pName := r.URL.Query().Get("provider")
	persist := r.URL.Query().Get("persist") == "true"

	ps, errMsg := api.resolveProviderByName(pName)
	if ps == nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
		return
	}

	var newValue interface{}
	switch body.Key {
	case "http_timeout_sec":
		v, err := toInt(body.Value)
		if err != nil || v < 1 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "http_timeout_sec must be a positive integer"})
			return
		}
		ps.Proxy.client.Timeout = time.Duration(v) * time.Second
		ps.Config.HTTPTimeoutSec = v
		newValue = v

	case "max_retries":
		v, err := toInt(body.Value)
		if err != nil || v < 0 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "max_retries must be a non-negative integer"})
			return
		}
		ps.Config.MaxRetries = v
		newValue = v

	case "cooldown_sec":
		v, err := toInt(body.Value)
		if err != nil || v < 1 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "cooldown_sec must be a positive integer"})
			return
		}
		ps.Config.CooldownSec = v
		ps.Pool.ConfigureCBs(
			time.Duration(v)*time.Second,
			time.Duration(ps.Config.BackoffCapSec)*time.Second,
			ps.Config.BackoffMultiplier,
		)
		newValue = v

	case "backoff_cap_sec":
		v, err := toInt(body.Value)
		if err != nil || v < 1 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "backoff_cap_sec must be a positive integer"})
			return
		}
		ps.Config.BackoffCapSec = v
		ps.Pool.ConfigureCBs(
			time.Duration(ps.Config.CooldownSec)*time.Second,
			time.Duration(v)*time.Second,
			ps.Config.BackoffMultiplier,
		)
		newValue = v

	case "backoff_multiplier":
		v, err := toFloat64(body.Value)
		if err != nil || v < 1.0 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "backoff_multiplier must be a number >= 1.0"})
			return
		}
		ps.Config.BackoffMultiplier = v
		ps.Pool.ConfigureCBs(
			time.Duration(ps.Config.CooldownSec)*time.Second,
			time.Duration(ps.Config.BackoffCapSec)*time.Second,
			v,
		)
		newValue = v

	case "cb_reset_sec":
		v, err := toInt(body.Value)
		if err != nil || v < 1 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "cb_reset_sec must be a positive integer"})
			return
		}
		ps.Proxy.upCB.SetResetTimeout(time.Duration(v) * time.Second)
		ps.Config.CBResetSec = v
		newValue = v

	case "upstream_cb_threshold":
		v, err := toInt(body.Value)
		if err != nil || v < 1 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "upstream_cb_threshold must be a positive integer"})
			return
		}
		ps.Proxy.upCB.SetThreshold(v)
		ps.Config.UpstreamCBThreshold = v
		newValue = v

	case "log_level":
		v, ok := body.Value.(string)
		if !ok {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "log_level must be a string"})
			return
		}
		v = strings.TrimSpace(strings.ToLower(v))
		switch v {
		case "debug", "info", "warn", "error":
			api.logManager.ApplyLevel(v)
			ps.Config.LogLevel = v
			newValue = v
		default:
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log level, use: debug, info, warn, error"})
			return
		}

	default:
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unknown key %q", body.Key)})
		return
	}

	// Persist to config file if requested
	persisted := false
	if persist {
		if err := api.persistRuntimeConfigField(ps, body.Key, newValue); err != nil {
			slog.Warn("failed to persist runtime config", "error", err)
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"provider":  ps.Name,
				"key":       body.Key,
				"value":     newValue,
				"persisted": false,
				"warn":      "change applied but not persisted: " + err.Error(),
			})
			return
		}
		persisted = true
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"provider":  ps.Name,
		"key":       body.Key,
		"value":     newValue,
		"persisted": persisted,
	})
}

// getRuntimeParams returns all runtime-configurable parameters for a provider.
func (api *AdminAPI) getRuntimeParams(ps *ProviderState) map[string]interface{} {
	return map[string]interface{}{
		"http_timeout_sec":      ps.Config.HTTPTimeoutSec,
		"max_retries":           ps.Config.MaxRetries,
		"cooldown_sec":          ps.Config.CooldownSec,
		"backoff_cap_sec":       ps.Config.BackoffCapSec,
		"backoff_multiplier":    ps.Config.BackoffMultiplier,
		"cb_reset_sec":          ps.Config.CBResetSec,
		"upstream_cb_threshold": ps.Config.UpstreamCBThreshold,
		"log_level":             ps.Config.LogLevel,
	}
}

// ── Key CRUD Handler Factory ────────────────────────────

// keyOperationHandler creates a handler for key operations (disable/enable/cooldown/delete).
// operation is a function that performs the actual key operation on the resolved provider's pool.
// The factory handles provider resolution, admin token check, key index parsing, persistence, and response.
//
// API uses 1-based indices (from URL path), converted to 0-based internally.
// In contrast, CLI commands use 0-based indices directly.
func (api *AdminAPI) keyOperationHandler(operation func(*keypool.KeyPool, *config.Config, int) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ps, errMsg := api.resolveProvider(r)
		if ps == nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
			return
		}
		if !api.checkAdminToken(w, r, ps.Name) {
			return
		}

		idx, err := parseKeyIndex(r)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		if idx >= len(ps.Pool.Keys()) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
			return
		}

		if err := operation(ps.Pool, ps.Config, idx); err != nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		ps.PersistKeys()
		respondJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

// ── Auth Helpers ────────────────────────────────────────

// checkAdminToken validates the X-Admin-Token header against a specific provider's admin token.
func (api *AdminAPI) checkAdminToken(w http.ResponseWriter, r *http.Request, providerName string) bool {
	ps := api.pm.LookupProvider(providerName)
	if ps == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	token := r.Header.Get("X-Admin-Token")
	if ps.Config.AdminToken == "" {
		// No admin token configured for this provider — access allowed
		return true
	}
	if ps.Config.AdminToken == token {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

// checkAnyAdminToken validates the X-Admin-Token header against any configured admin token.
// If at least one provider has an AdminToken configured, a valid token must be provided.
// If no providers have AdminToken configured, access is allowed without token.
func (api *AdminAPI) checkAnyAdminToken(w http.ResponseWriter, r *http.Request) bool {
	token := r.Header.Get("X-Admin-Token")
	names := api.pm.ProviderNames()
	matched := false
	hasAnyToken := false
	for _, name := range names {
		ps := api.pm.LookupProvider(name)
		if ps.Config.AdminToken != "" {
			hasAnyToken = true
			if ps.Config.AdminToken == token {
				matched = true
			}
		}
	}
	if matched {
		return true
	}
	if !hasAnyToken {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

// resolveProvider gets the provider specified by the "provider" query parameter.
// If not set, returns the first provider. Returns an error string if no provider found.
func (api *AdminAPI) resolveProvider(r *http.Request) (*ProviderState, string) {
	pName := r.URL.Query().Get("provider")
	if pName == "" {
		ps := api.pm.FirstProvider()
		if ps == nil {
			return nil, "no providers configured"
		}
		return ps, ""
	}
	ps := api.pm.LookupProvider(pName)
	if ps == nil {
		return nil, fmt.Sprintf("provider %q not found", pName)
	}
	return ps, ""
}

// resolveProviderByName resolves a provider by name, or returns the first provider if name is empty.
func (api *AdminAPI) resolveProviderByName(name string) (*ProviderState, string) {
	if name != "" {
		ps := api.pm.LookupProvider(name)
		if ps == nil {
			return nil, fmt.Sprintf("provider %q not found", name)
		}
		return ps, ""
	}
	ps := api.pm.FirstProvider()
	if ps == nil {
		return nil, "no providers configured"
	}
	return ps, ""
}

// persistRuntimeConfigField saves a single field change to the TOML config file.
// Only the specified field is modified; other fields are preserved.
func (api *AdminAPI) persistRuntimeConfigField(ps *ProviderState, key string, value interface{}) error {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		return fmt.Errorf("failed to determine config path: %w", err)
	}

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}

	if tc.Provider == nil {
		tc.Provider = make(map[string]*config.Config)
	}

	providerCfg, ok := tc.Provider[ps.Name]
	if !ok {
		providerCfg = &config.Config{}
		tc.Provider[ps.Name] = providerCfg
	}

	// Only modify the specific field
	switch key {
	case "http_timeout_sec":
		v, _ := toInt(value)
		providerCfg.HTTPTimeoutSec = v
	case "max_retries":
		v, _ := toInt(value)
		providerCfg.MaxRetries = v
	case "cooldown_sec":
		v, _ := toInt(value)
		providerCfg.CooldownSec = v
	case "backoff_cap_sec":
		v, _ := toInt(value)
		providerCfg.BackoffCapSec = v
	case "backoff_multiplier":
		v, _ := toFloat64(value)
		providerCfg.BackoffMultiplier = v
	case "cb_reset_sec":
		v, _ := toInt(value)
		providerCfg.CBResetSec = v
	case "upstream_cb_threshold":
		v, _ := toInt(value)
		providerCfg.UpstreamCBThreshold = v
	case "log_level":
		v, _ := value.(string)
		providerCfg.LogLevel = v
	}

	return config.SaveTomlConfig(tc, xdgPath)
}
