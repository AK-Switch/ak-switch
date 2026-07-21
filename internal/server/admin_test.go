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
	handler := pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
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
	handler := pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
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
	handler := pr.keyOperationHandler(func(pool *keypool.KeyPool, cfg *config.Config, idx int) error {
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
	handler := pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
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
	handler := pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
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
	handler := pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
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
	handler := pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
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
	handler := pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
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

	handler := pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
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
	handler := pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
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