package server

import (
	"akswitch/internal/config"
	"akswitch/internal/logentry"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ── Auth Helpers ────────────────────────────────────────

// checkAdminToken validates the X-Admin-Token header against a specific provider's admin token.
func (api *AdminAPI) checkAdminToken(w http.ResponseWriter, r *http.Request, providerName string) bool {
	ps := api.pm.LookupProvider(providerName)
	if ps == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	token := r.Header.Get("X-Admin-Token")
	if !ps.HasAdminToken() {
		// No admin token configured for this provider — access allowed
		return true
	}
	if ps.CheckAdminToken(token) {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

// checkAnyAdminToken validates the X-Admin-Token header against any configured admin token.
// If no provider has an AdminToken configured, access is allowed (no auth enforced).
// When tokens are configured, a matching token is required.
func (api *AdminAPI) checkAnyAdminToken(w http.ResponseWriter, r *http.Request) bool {
	token := r.Header.Get("X-Admin-Token")
	matched := false
	hasAnyToken := false
	api.pm.ForEach(func(_ string, ps *ProviderState) {
		if !ps.HasAdminToken() {
			return
		}
		hasAnyToken = true
		if ps.CheckAdminToken(token) {
			matched = true
		}
	})
	if !hasAnyToken {
		return true
	}
	if matched {
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

	providerCfg, ok := tc.Provider[ps.Name()]
	if !ok {
		providerCfg = &config.Config{}
		tc.Provider[ps.Name()] = providerCfg
	}

	// Only modify the specific field
	fd := config.FindField(key)
	if fd != nil && fd.Persist != nil {
		fd.Persist(tc, ps.Name(), providerCfg, value)
	}

	return config.SaveTomlConfig(tc, xdgPath)
}

// persistRuntimeConfigFieldToDefault saves a field to the [provider.default] section.
// This is used when applying config to all providers so new providers inherit the value.
func (api *AdminAPI) persistRuntimeConfigFieldToDefault(key string, value interface{}) error {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		return err
	}

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		return err
	}
	if tc.Default == nil {
		tc.Default = &config.Config{}
	}

	fd := config.FindField(key)
	if fd != nil && fd.Persist != nil {
		fd.Persist(tc, "", tc.Default, value)
	}

	return config.SaveTomlConfig(tc, xdgPath)
}

// ── SW Handler ──────────────────────────────────────────

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
		if config.IsValidLogLevel(body.Level) {
			api.logManager.ApplyLevel(body.Level)
			respondJSON(w, http.StatusOK, map[string]string{"level": body.Level})
		} else {
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
		ps, errMsg := api.resolveProvider(r)
		if ps == nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
			return
		}

		keys := ps.PoolKeys()
		maskedKeys := make([]string, len(keys))
		for i, k := range keys {
			maskedKeys[i] = logentry.MaskKey(k)
		}
		respondJSON(w, http.StatusOK, config.ConfigPayload{
			TargetBase: ps.TargetBase(),
			Keys:       maskedKeys,
		})
		return
	}

	// POST is removed — no more .env writing
	w.WriteHeader(http.StatusMethodNotAllowed)
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
