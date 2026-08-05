//go:build unit

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
)

// ── keyOperationHandler ────────────────────────────────

func TestKeyOperationHandler_Disable(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0", "sk-key-1"})
	handler := pr.api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Disable(idx)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/keys/1/disable", nil)
	r.SetPathValue("index", "1")
	handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !pr.Provider("test").Pool.IsDisabled(0) {
		t.Error("key 0 should be disabled, but it is not")
	}
}

func TestKeyOperationHandler_Enable(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0", "sk-key-1"})
	handler := pr.api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Enable(idx)
	})

	// First disable key 0
	pr.Provider("test").Pool.Disable(0)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/keys/1/enable", nil)
	r.SetPathValue("index", "1")
	handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if pr.Provider("test").Pool.IsDisabled(0) {
		t.Error("key 0 should be enabled, but it is disabled")
	}
}

func TestKeyOperationHandler_Cooldown(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0", "sk-key-1"})
	handler := pr.api.keyOperationHandler(func(pool *keypool.KeyPool, cfg *config.Config, idx int) error {
		return pool.Cooldown(idx, time.Duration(cfg.CooldownSec)*time.Second)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/keys/1/cooldown", nil)
	r.SetPathValue("index", "1")
	handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestKeyOperationHandler_Delete(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0", "sk-key-1"})
	handler := pr.api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.RemoveKey(idx)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/keys/1", nil)
	r.SetPathValue("index", "1")
	handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if pr.Provider("test").Pool.Len() != 1 {
		t.Errorf("pool length = %d, want 1", pr.Provider("test").Pool.Len())
	}
}

func TestKeyOperationHandler_ProviderNotFound(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0"})
	handler := pr.api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Disable(idx)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/keys/1/disable?provider=nonexistent", nil)
	r.SetPathValue("index", "1")
	handler(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !strings.Contains(body["error"], "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", body["error"])
	}
}

func TestKeyOperationHandler_InvalidIndex(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0"})
	handler := pr.api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Disable(idx)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/keys/abc/disable", nil)
	r.SetPathValue("index", "abc")
	handler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestKeyOperationHandler_IndexZero(t *testing.T) {
	// API uses 1-based indexing, so index "0" should be rejected
	pr := newTestRouterWithKeys(t, []string{"sk-key-0"})
	handler := pr.api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Disable(idx)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/keys/0/disable", nil)
	r.SetPathValue("index", "0")
	handler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestKeyOperationHandler_IndexOutOfBounds(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0"})
	handler := pr.api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Disable(idx)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/keys/999/disable", nil)
	r.SetPathValue("index", "999")
	handler(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestKeyOperationHandler_AdminTokenRequired(t *testing.T) {
	pr := NewProviderRouter("")
	cfg := config.DefaultConfig()
	cfg.AdminToken = "secret-token"
	cfg.Keys = []string{"sk-key-0"}
	pool := keypool.NewKeyPool(cfg.Keys, nil)
	pr.AddProvider("test", cfg, pool)

	handler := pr.api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Disable(idx)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/keys/1/disable", nil)
	r.SetPathValue("index", "1")
	handler(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestKeyOperationHandler_OperationError(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0"})
	handler := pr.api.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Disable(999) // out-of-range inside operation
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/keys/1/disable", nil)
	r.SetPathValue("index", "1")
	handler(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ── Runtime Config ─────────────────────────────────────

func TestHandleRuntimeConfigGet_ProviderAll(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0", "sk-key-1"})
	cfg2 := config.DefaultConfig()
	cfg2.MaxRetries = 5
	cfg2.CooldownSec = 30
	pool2 := keypool.NewKeyPool([]string{"sk-key-2"}, nil)
	pr.AddProvider("provider-b", cfg2, pool2)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/runtime-config?provider=all", nil)
	r.Header.Set("X-Admin-Token", "")
	pr.api.runtimeConfigHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var result map[string]map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if _, ok := result["test"]; !ok {
		t.Error("missing provider 'test' in response")
	}
	if _, ok := result["provider-b"]; !ok {
		t.Error("missing provider 'provider-b' in response")
	}
	// Verify provider-b has the overridden values
	if result["provider-b"]["max_retries"] != float64(5) {
		t.Errorf("provider-b max_retries = %v, want 5", result["provider-b"]["max_retries"])
	}
	if result["provider-b"]["cooldown_sec"] != float64(30) {
		t.Errorf("provider-b cooldown_sec = %v, want 30", result["provider-b"]["cooldown_sec"])
	}
}

func TestHandleRuntimeConfigGet_ProviderAllWithKey(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0", "sk-key-1"})
	cfg2 := config.DefaultConfig()
	cfg2.MaxRetries = 5
	pool2 := keypool.NewKeyPool([]string{"sk-key-2"}, nil)
	pr.AddProvider("provider-b", cfg2, pool2)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/runtime-config?provider=all&key=max_retries", nil)
	r.Header.Set("X-Admin-Token", "")
	pr.api.runtimeConfigHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var result map[string]map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if _, ok := result["test"]; !ok {
		t.Error("missing provider 'test' in response")
	}
	if _, ok := result["provider-b"]; !ok {
		t.Error("missing provider 'provider-b' in response")
	}
}

func TestHandleRuntimeConfigSet_ProviderAll(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0", "sk-key-1"})
	cfg2 := config.DefaultConfig()
	cfg2.MaxRetries = 5
	pool2 := keypool.NewKeyPool([]string{"sk-key-2"}, nil)
	pr.AddProvider("provider-b", cfg2, pool2)

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"key":"cooldown_sec","value":60}`)
	r := httptest.NewRequest(http.MethodPost, "/api/runtime-config?provider=all", body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Admin-Token", "")
	pr.api.runtimeConfigHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if result["key"] != "cooldown_sec" {
		t.Errorf("key = %q, want cooldown_sec", result["key"])
	}
	if result["value"] != float64(60) {
		t.Errorf("value = %v, want 60", result["value"])
	}
	providers, ok := result["providers"].([]interface{})
	if !ok {
		t.Fatalf("providers type = %T, want []interface{}", result["providers"])
	}
	if len(providers) != 2 {
		t.Errorf("providers count = %d, want 2", len(providers))
	}

	// Verify both providers' runtime config was updated
	testPs := pr.Provider("test")
	if testPs.Config.CooldownSec != 60 {
		t.Errorf("test CooldownSec = %d, want 60", testPs.Config.CooldownSec)
	}
	providerBPs := pr.Provider("provider-b")
	if providerBPs.Config.CooldownSec != 60 {
		t.Errorf("provider-b CooldownSec = %d, want 60", providerBPs.Config.CooldownSec)
	}
}

func TestHandleRuntimeConfigSet_ProviderAllInvalidValue(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0"})

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"key":"http_timeout_sec","value":0}`)
	r := httptest.NewRequest(http.MethodPost, "/api/runtime-config?provider=all", body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Admin-Token", "")
	pr.api.runtimeConfigHandler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if result["error"] != "http_timeout_sec must be a positive integer" {
		t.Errorf("error = %q, want 'http_timeout_sec must be a positive integer'", result["error"])
	}
}

func TestHandleRuntimeConfigSet_ProviderAllInvalidKey(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0"})

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"key":"nonexistent_key","value":1}`)
	r := httptest.NewRequest(http.MethodPost, "/api/runtime-config?provider=all", body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Admin-Token", "")
	pr.api.runtimeConfigHandler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if result["error"] != "unknown key \"nonexistent_key\"" {
		t.Errorf("error = %q, want 'unknown key \"nonexistent_key\"'", result["error"])
	}
}

// ── Helpers ────────────────────────────────────────────

// newTestRouterWithKeys creates a ProviderRouter with a single provider named "test"
// using the given keys and a default config.
func newTestRouterWithKeys(t *testing.T, keys []string) *ProviderRouter {
	t.Helper()
	pr := NewProviderRouter("")
	cfg := config.DefaultConfig()
	cfg.Keys = keys
	pool := keypool.NewKeyPool(keys, nil)
	pr.AddProvider("test", cfg, pool)
	return pr
}