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
	"strconv"
	"strings"
	"time"
)

// ── Management Handlers ────────────────────────────────

func (pr *ProviderRouter) swHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (pr *ProviderRouter) logLevelHandler(w http.ResponseWriter, r *http.Request) {
	if !pr.checkAnyAdminToken(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		respondJSON(w, http.StatusOK, map[string]string{"level": pr.logManager.CurrentLevel()})

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
			pr.logManager.ApplyLevel(body.Level)
			respondJSON(w, http.StatusOK, map[string]string{"level": body.Level})
		default:
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log level, use: debug, info, warn, error"})
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (pr *ProviderRouter) configHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Return config for a specific provider or all providers
		ps, _ := pr.resolveProvider(r)
		if ps == nil {
			// Return all providers
			pr.mu.RLock()
			result := make(map[string]ConfigPayload)
			for name, p := range pr.providers {
				keys := p.Pool.Keys()
				maskedKeys := make([]string, len(keys))
				for i, k := range keys {
					maskedKeys[i] = logentry.MaskKey(k)
				}
				result[name] = ConfigPayload{
					TargetBase: p.Config.TargetBase,
					GenaiBase:  p.Config.GenaiBase,
					Keys:       maskedKeys,
				}
			}
			pr.mu.RUnlock()
			respondJSON(w, http.StatusOK, map[string]interface{}{"providers": result})
			return
		}

		keys := ps.Pool.Keys()
		maskedKeys := make([]string, len(keys))
		for i, k := range keys {
			maskedKeys[i] = logentry.MaskKey(k)
		}
		respondJSON(w, http.StatusOK, ConfigPayload{
			TargetBase: ps.Config.TargetBase,
			GenaiBase:  ps.Config.GenaiBase,
			Keys:       maskedKeys,
		})
		return
	}

	// POST is removed — no more .env writing
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func (pr *ProviderRouter) keysHandler(w http.ResponseWriter, r *http.Request) {
	ps, errMsg := pr.resolveProvider(r)
	if ps == nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
		return
	}

	pool := ps.Pool

	if r.Method == http.MethodPost || r.Method == http.MethodDelete {
		if !pr.checkAdminToken(w, r, ps.Name) {
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
		json.NewEncoder(w).Encode(result)

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
		json.NewEncoder(w).Encode(map[string]interface{}{
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
		json.NewEncoder(w).Encode(map[string]string{"status": "removed"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (pr *ProviderRouter) healthHandler(w http.ResponseWriter, r *http.Request) {
	if !pr.checkAnyAdminToken(w, r) {
		return
	}

	pr.mu.RLock()
	defer pr.mu.RUnlock()

	// Aggregate health info across all providers
	type providerHealth struct {
		Status            string `json:"status"`
		Keys              int    `json:"keys"`
		UpstreamCBState   string `json:"upstream_cb_state"`
		LastHealthCheck   string `json:"last_health_check,omitempty"`
		LastHealthCheckOK *bool  `json:"last_health_check_ok,omitempty"`
	}

	result := make(map[string]*providerHealth)
	overallOK := true

	for name, ps := range pr.providers {
		upCB := ps.Proxy.upCB

		var cbState string
		switch upCB.State() {
		case circuitbreaker.UpstreamClosed:
			cbState = "closed"
		case circuitbreaker.UpstreamOpen:
			cbState = "open"
		case circuitbreaker.UpstreamHalfOpen:
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
	}

	status := "ok"
	if !overallOK {
		status = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    status,
		"providers": len(pr.providers),
		"details":   result,
	})
}

func (pr *ProviderRouter) logsHandler(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	if since != "" {
		entries := pr.logs.SnapshotSince(since)
		respondJSON(w, http.StatusOK, entries)
		return
	}
	entries := pr.logs.Snapshot()
	respondJSON(w, http.StatusOK, entries)
}

func (pr *ProviderRouter) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(pr.dashboardHTML))
}

func (pr *ProviderRouter) clearHandler(w http.ResponseWriter, r *http.Request) {
	if !pr.checkAnyAdminToken(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	pr.logs.Clear()
	respondJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (pr *ProviderRouter) statsHandler(w http.ResponseWriter, r *http.Request) {
	total := pr.logs.Len()
	entries := pr.logs.Snapshot()

	successful := 0
	failed := 0
	for _, e := range entries {
		if e.Status < 400 {
			successful++
		} else {
			failed++
		}
	}

	var successRate float64
	if total > 0 {
		successRate = float64(successful) / float64(total) * 100
	}

	// Aggregate key stats across all providers
	pr.mu.RLock()
	totalActive := 0
	totalCooling := 0
	totalDisabled := 0
	for _, ps := range pr.providers {
		totalActive += ps.Pool.ActiveCount()
		totalCooling += ps.Pool.CoolingCount()
		totalDisabled += ps.Pool.DisabledCount()
	}
	pr.mu.RUnlock()

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"total_requests":      total,
		"successful_requests": successful,
		"failed_requests":     failed,
		"success_rate":        fmt.Sprintf("%.2f", successRate),
		"active_keys":         totalActive,
		"cooling_keys":        totalCooling,
		"disabled_keys":       totalDisabled,
		"uptime_seconds":      time.Since(pr.startTime).Seconds(),
	})
}

func (pr *ProviderRouter) reloadHandler(w http.ResponseWriter, r *http.Request) {
	if !pr.checkAnyAdminToken(w, r) {
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

	pr.mu.Lock()
	defer pr.mu.Unlock()

	for name, cfg := range providers {
		// Load keys from configured keys file or standard encrypted store
		keys, keyNames := loadKeysFromConfig(name, cfg)
		if len(keys) > 0 {
			cfg.Keys = keys
			cfg.KeyNames = keyNames
		}

		if existing, ok := pr.providers[name]; ok {
			// Update existing provider — preserve disabled state across reload
			oldPool := existing.Pool
			var disabledNames []string
			for i := 0; i < oldPool.Len(); i++ {
				if oldPool.IsDisabled(i) {
					n, _ := oldPool.Name(i)
					disabledNames = append(disabledNames, n)
				}
			}

			existing.Config = cfg
			existing.Pool = keypool.NewKeyPool(cfg.Keys, cfg.KeyNames)

			for _, name := range disabledNames {
				for i := 0; i < existing.Pool.Len(); i++ {
					n, _ := existing.Pool.Name(i)
					if n == name {
						_ = existing.Pool.Disable(i)
						break
					}
				}
			}
			pr.logManager.ApplyLevel(cfg.LogLevel)
		} else {
			// New provider — add it
			pool := keypool.NewKeyPool(cfg.Keys, cfg.KeyNames)
			ps := NewProviderState(name, cfg, pool, pr.dashboardHTML, cfg.KeysFile)
			pr.logManager.ApplyLevel(cfg.LogLevel)
			pr.providers[name] = ps
		}
	}

	slog.Info("config reloaded", "providers", len(pr.providers))
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ── Runtime Config Handler ──────────────────────────────

// runtimeConfigHandler handles GET and POST for runtime-configurable parameters.
//   GET  /api/runtime-config?provider=xxx&key=yyy  — list params (key optional)
//   POST /api/runtime-config?provider=xxx&persist=true — set a param
func (pr *ProviderRouter) runtimeConfigHandler(w http.ResponseWriter, r *http.Request) {
	if !pr.checkAnyAdminToken(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		pr.handleRuntimeConfigGet(w, r)
	case http.MethodPost:
		pr.handleRuntimeConfigSet(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (pr *ProviderRouter) handleRuntimeConfigGet(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	pName := r.URL.Query().Get("provider")

	pr.mu.RLock()
	defer pr.mu.RUnlock()

	if pName != "" {
		ps := pr.lookupProvider(pName)
		if ps == nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("provider %q not found", pName)})
			return
		}
		params := pr.getRuntimeParams(ps)
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
	for name, ps := range pr.providers {
		result[name] = pr.getRuntimeParams(ps)
	}

	if key != "" {
		// Key specified without provider — return from first provider
		for name, ps := range pr.providers {
			params := pr.getRuntimeParams(ps)
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
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "no providers configured"})
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (pr *ProviderRouter) handleRuntimeConfigSet(w http.ResponseWriter, r *http.Request) {
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

	pr.mu.RLock()
	ps, errMsg := pr.resolveProviderByName(pName)
	pr.mu.RUnlock()
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
			pr.logManager.ApplyLevel(v)
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
		if err := pr.persistRuntimeConfigField(ps, body.Key, newValue); err != nil {
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
func (pr *ProviderRouter) getRuntimeParams(ps *ProviderState) map[string]interface{} {
	return map[string]interface{}{
		"http_timeout_sec":     ps.Config.HTTPTimeoutSec,
		"max_retries":           ps.Config.MaxRetries,
		"cooldown_sec":          ps.Config.CooldownSec,
		"backoff_cap_sec":       ps.Config.BackoffCapSec,
		"backoff_multiplier":    ps.Config.BackoffMultiplier,
		"cb_reset_sec":          ps.Config.CBResetSec,
		"upstream_cb_threshold": ps.Config.UpstreamCBThreshold,
	}
}

// resolveProviderByName resolves a provider by name, or returns the first provider if name is empty.
func (pr *ProviderRouter) resolveProviderByName(name string) (*ProviderState, string) {
	if name != "" {
		ps := pr.lookupProvider(name)
		if ps == nil {
			return nil, fmt.Sprintf("provider %q not found", name)
		}
		return ps, ""
	}
	ps := pr.firstProvider()
	if ps == nil {
		return nil, "no providers configured"
	}
	return ps, ""
}

// persistRuntimeConfigField saves a single field change to the TOML config file.
// Only the specified field is modified; other fields are preserved.
func (pr *ProviderRouter) persistRuntimeConfigField(ps *ProviderState, key string, value interface{}) error {
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

// ── Helpers ─────────────────────────────────────────────

// toInt converts a JSON-decoded value to int.
func toInt(v interface{}) (int, error) {
	switch val := v.(type) {
	case float64:
		return int(val), nil
	case int:
		return val, nil
	case string:
		return strconv.Atoi(val)
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

// toFloat64 converts a JSON-decoded value to float64.
func toFloat64(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

// loadKeysFromConfig loads API keys for a provider from the configured keys file
// or the standard encrypted store. Returns nil if no keys can be loaded.
func loadKeysFromConfig(name string, cfg *config.Config) (keys, names []string) {
	keys, names, loaded := keypool.LoadKeysFromStore(name, cfg)
	if !loaded {
		return nil, nil
	}
	return keys, names
}

// ── Key CRUD Handler Factory ──────────────────────────

// keyOperationHandler creates a handler for key operations (disable/enable/cooldown/delete).
// operation is a function that performs the actual key operation on the resolved provider's pool.
// The factory handles provider resolution, admin token check, key index parsing, persistence, and response.
//
// API uses 1-based indices (from URL path), converted to 0-based internally.
// In contrast, CLI commands use 0-based indices directly.
func (pr *ProviderRouter) keyOperationHandler(operation func(*keypool.KeyPool, *config.Config, int) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ps, errMsg := pr.resolveProvider(r)
		if ps == nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
			return
		}
		if !pr.checkAdminToken(w, r, ps.Name) {
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