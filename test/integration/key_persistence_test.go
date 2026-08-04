//go:build integration

package integration

import (
	"encoding/json"

	"io"

	"net/http"
	"net/http/httptest"

	"strings"
	"testing"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/server"
)

func TestKeyPersistence_AddKeyRestart(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:  upstream.URL,
			Port:        0,
			MaxRetries:  3,
			CooldownSec: 60,
		},
	}
	pool := keypool.NewKeyPool([]string{"initial-key"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())

	resp, err := http.Post(srv.URL+"/api/keys", "application/json",
		strings.NewReader(`{"key":"persistent-key","name":"test-key"}`))
	if err != nil {
		t.Fatalf("POST /api/keys: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/keys: got status %d, want 200", resp.StatusCode)
	}

	srv.Close()

	// Verify keys are persisted via keyring
	store, err := keypool.LoadStoreFromKeyring("test")
	if err != nil {
		t.Fatalf("LoadStoreFromKeyring: %v", err)
	}
	if store == nil {
		t.Fatal("keys should be persisted to keyring after adding a key")
	}

	found := false
	for _, entry := range store.Keys {
		if entry.Key == "persistent-key" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("persistent-key not found in keyring store, got keys: %v", store.Keys)
	}

	// Load keys from store for restart simulation
	fileKeys := make([]string, len(store.Keys))
	fileNames := make([]string, len(store.Keys))
	for i, entry := range store.Keys {
		fileKeys[i] = entry.Key
		fileNames[i] = entry.Name
	}

	restoredPool := keypool.NewKeyPool(fileKeys, fileNames)
	newCfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:  upstream.URL,
			Port:        0,
			MaxRetries:  3,
			CooldownSec: 60,
		},
	}
	pr2 := server.NewProviderRouter("")
	pr2.AddProvider("test", newCfg, restoredPool)
	srv2 := httptest.NewServer(pr2.Handler())
	defer srv2.Close()

	// Verify the key through the API
	resp2, err := http.Get(srv2.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)

	// Name is not masked in the API response
	if !strings.Contains(string(body), "test-key") {
		t.Errorf("restored pool should contain name 'test-key', got: %s", string(body))
	}

	// Key is masked (e.g. "pers...ey"); decode JSON to verify structure
	var keyList []map[string]interface{}
	if err := json.Unmarshal(body, &keyList); err != nil {
		t.Fatalf("failed to decode /api/keys response: %v", err)
	}
	if len(keyList) != 2 {
		t.Errorf("expected 2 keys after restoration, got %d", len(keyList))
	}
}

// TestKeyPersistence_DeleteKeyRestart verifies that deleting a key via API
// persists the removal and the key is gone after restart.
func TestKeyPersistence_DeleteKeyRestart(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:  upstream.URL,
			Port:        0,
			MaxRetries:  3,
			CooldownSec: 60,
		},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())

	// Delete key-a via API (index 1 = first key, 1-based in URL)
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys/1", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys/1: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/keys/1: got status %d, want 200", resp.StatusCode)
	}

	srv.Close()

	// Verify keys are persisted via keyring
	store, err := keypool.LoadStoreFromKeyring("test")
	if err != nil {
		t.Fatalf("LoadStoreFromKeyring: %v", err)
	}
	if store == nil {
		t.Fatal("keys should be persisted to keyring after deletion")
	}

	for _, entry := range store.Keys {
		if entry.Key == "key-a" {
			t.Error("key-a should not be in the persisted keys after deletion")
		}
	}
	found := false
	for _, entry := range store.Keys {
		if entry.Key == "key-b" {
			found = true
			break
		}
	}
	if !found {
		t.Error("key-b should still be in the persisted keys")
	}
}

// TestKeyPersistence_DisableKeyAndPersist verifies that disabling a key
// via API persists the disabled state to disk.
func TestKeyPersistence_DisableKeyAndPersist(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:  upstream.URL,
			Port:        0,
			MaxRetries:  3,
			CooldownSec: 60,
		},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())

	// Disable key 1 (first key, 1-based in URL) via API
	resp, err := http.Post(srv.URL+"/api/keys/1/disable", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want 200", resp.StatusCode)
	}

	srv.Close()

	// Verify keys are persisted via keyring with disabled state
	store, err := keypool.LoadStoreFromKeyring("test")
	if err != nil {
		t.Fatalf("LoadStoreFromKeyring: %v", err)
	}
	if store == nil {
		t.Fatal("keys should be persisted to keyring after disabling a key")
	}

	t.Logf("store contents: %+v", store.Keys)

	for _, entry := range store.Keys {
		if entry.Key == "key-a" && !entry.Disabled {
			t.Error("key-a should be disabled in persisted store")
		}
	}
}

// TestKeyEncryption_NoEncryption_BackwardCompatible verifies that
// without an encryption key, the system works as before (plaintext keys).
func TestKeyEncryption_NoEncryption_BackwardCompatible(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:  upstream.URL,
			Port:        0,
			MaxRetries:  3,
			CooldownSec: 60,
		},
	}
	pool := keypool.NewKeyPool([]string{"plaintext-key-a"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())

	// Add another key
	resp, err := http.Post(srv.URL+"/api/keys", "application/json",
		strings.NewReader(`{"key":"plaintext-key-b"}`))
	if err != nil {
		t.Fatalf("POST /api/keys: %v", err)
	}
	resp.Body.Close()
	srv.Close()

	// Verify keys are persisted via keyring
	store, err := keypool.LoadStoreFromKeyring("test")
	if err != nil {
		t.Fatalf("LoadStoreFromKeyring: %v", err)
	}
	if store == nil {
		t.Fatal("keys should be persisted to keyring")
	}

	foundA := false
	foundB := false
	for _, entry := range store.Keys {
		if entry.Key == "plaintext-key-a" {
			foundA = true
		}
		if entry.Key == "plaintext-key-b" {
			foundB = true
		}
	}
	if !foundA {
		t.Error("plaintext-key-a not found in keyring store")
	}
	if !foundB {
		t.Error("plaintext-key-b not found in keyring store")
	}
}

// ---------------------------------------------------------------------------
// LogEntry new field integration tests (T4: 测试覆盖)
// ---------------------------------------------------------------------------

// TestLogEntry_HasNewFields verifies that a successful proxy request
// produces a log entry with DurationMs, Attempt, Provider, and KeyName.
