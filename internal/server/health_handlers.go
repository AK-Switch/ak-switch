package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"akswitch/internal/circuitbreaker"
	"akswitch/internal/config"
)

// ── SW Handler ──────────────────────────────────────────

func (api *AdminAPI) swHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
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

		var upCB = ps.UpstreamCB()

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
			Keys:              ps.PoolLen(),
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
		totalActive += ps.PoolActiveCount()
		totalCooling += ps.PoolCoolingCount()
		totalDisabled += ps.PoolDisabledCount()
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

	if !api.checkAdminToken(w, r, ps.Name()) {
		return
	}

	ps.ResetUpstreamCB()
	slog.Info("upstream circuit breaker reset", "provider", ps.Name())
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"provider": ps.Name(),
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
