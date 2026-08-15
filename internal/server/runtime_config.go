package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"akswitch/internal/config"
)

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

	if pName == "all" {
		result := make(map[string]map[string]interface{})
		api.pm.ForEach(func(name string, ps *ProviderState) {
			result[name] = api.getRuntimeParams(ps)
		})
		respondJSON(w, http.StatusOK, result)
		return
	}

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
				"provider": ps.Name(),
				"key":      key,
				"value":    val,
			})
			return
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"provider": ps.Name(),
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
		names := api.pm.ProviderNames()
		for _, name := range names {
			ps := api.pm.LookupProvider(name)
			params := api.getRuntimeParams(ps)
			val, ok := params[key]
			if !ok {
				continue
			}
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"provider": name,
				"key":      key,
				"value":    val,
			})
			return
		}
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unknown key %q", key)})
		return
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

	if pName == "all" {
		var names []string
		var newValue interface{}
		var firstErr error
		api.pm.ForEach(func(name string, ps *ProviderState) {
			val, err := api.setRuntimeConfigField(ps, body.Key, body.Value)
			if firstErr == nil && err != nil {
				firstErr = err
			}
			if newValue == nil && val != nil {
				newValue = val
			}
			names = append(names, name)
		})
		if firstErr != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": firstErr.Error()})
			return
		}
		if body.Key == "log_level" {
			api.logManager.ApplyLevel(newValue.(string))
		}
		if persist {
			_ = api.persistRuntimeConfigFieldToDefault(body.Key, newValue)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"key":       body.Key,
			"value":     newValue,
			"persisted": persist,
			"providers": names,
		})
		return
	}

	ps, errMsg := api.resolveProviderByName(pName)
	if ps == nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
		return
	}

	newValue, err := api.setRuntimeConfigField(ps, body.Key, body.Value)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Key == "log_level" {
		api.logManager.ApplyLevel(newValue.(string))
	}

	// Persist to config file if requested
	persisted := false
	if persist {
		if err := api.persistRuntimeConfigField(ps, body.Key, newValue); err != nil {
			slog.Warn("failed to persist runtime config", "error", err)
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"provider":  ps.Name(),
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
		"provider":  ps.Name(),
		"key":       body.Key,
		"value":     newValue,
		"persisted": persisted,
	})
}

// setRuntimeConfigField applies a runtime config change to a provider's
// in-memory config and runtime state. Returns the new value and an error
// if the key is unknown or the value is invalid.
func (api *AdminAPI) setRuntimeConfigField(ps *ProviderState, key string, value interface{}) (interface{}, error) {
	fd := config.FindField(key)
	if fd == nil || !fd.RuntimeEditable || fd.ApplyRuntime == nil {
		return nil, fmt.Errorf("unknown key %q", key)
	}
	return fd.ApplyRuntime(ps, "", fmt.Sprintf("%v", value))
}

// getRuntimeParams returns all runtime-configurable parameters for a provider.
func (api *AdminAPI) getRuntimeParams(ps *ProviderState) map[string]interface{} {
	return map[string]interface{}{
		"http_timeout_sec":        ps.HTTPTimeoutSec(),
		"max_retries":             ps.MaxRetries(),
		"cooldown_sec":            ps.CooldownSec(),
		"backoff_cap_sec":         ps.BackoffCapSec(),
		"backoff_multiplier":      ps.BackoffMultiplier(),
		"cb_reset_sec":            ps.CBResetSec(),
		"upstream_cb_threshold":   ps.UpstreamCBThreshold(),
		"log_level":               ps.LogLevel(),
		"thinking_mode":           ps.ThinkingMode(),
		"rectify_thinking_map_to": ps.RectifyThinkingMapTo(),
	}
}
