package server

import (
	"net/http"
	"time"

	"akswitch/internal/config"
)

// ProviderLookup defines the interface AdminAPI needs to access provider data.
type ProviderLookup interface {
	LookupProvider(name string) *ProviderState
	FirstProvider() *ProviderState
	ForEach(fn func(name string, ps *ProviderState))
	ProviderNames() []string
	ReloadConfig(providers map[string]*config.Config, logManager *LogManager)
}

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
	api.disableKeyHandler = api.keyOperationHandler(func(ps *ProviderState, idx int) error {
		return ps.PoolDisable(idx)
	})
	api.enableKeyHandler = api.keyOperationHandler(func(ps *ProviderState, idx int) error {
		return ps.PoolEnable(idx)
	})
	api.cooldownKeyHandler = api.keyOperationHandler(func(ps *ProviderState, idx int) error {
		return ps.PoolCooldown(idx, time.Duration(ps.CooldownSec())*time.Second)
	})
	api.deleteKeyHandler = api.keyOperationHandler(func(ps *ProviderState, idx int) error {
		return ps.PoolRemoveKey(idx)
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

// keyOperationHandler creates a handler for key operations (disable/enable/cooldown/delete).
// operation is a function that performs the actual key operation on the resolved provider's pool.
// The factory handles provider resolution, admin token check, key index parsing, persistence, and response.
//
// API uses 1-based indices (from URL path), converted to 0-based internally.
// In contrast, CLI commands use 0-based indices directly.
func (api *AdminAPI) keyOperationHandler(operation func(ps *ProviderState, idx int) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ps, errMsg := api.resolveProvider(r)
		if ps == nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
			return
		}
		if !api.checkAdminToken(w, r, ps.Name()) {
			return
		}

		idx, err := parseKeyIndex(r)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		if idx >= ps.PoolLen() {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
			return
		}

		if err := operation(ps, idx); err != nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		ps.PersistKeys()
		respondJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}
