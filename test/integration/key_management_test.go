//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/logentry"
	"akswitch/internal/server"
)

// ── Keys GET ───────────────────────────────────────

func TestKeysGet(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var keys []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}

	for i, k := range keys {
		if idx, ok := k["index"].(float64); !ok || int(idx) != i+1 {
			t.Errorf("keys[%d] index=%v, want %d", i, k["index"], i+1)
		}
		if key, ok := k["key"].(string); !ok || key == "" {
			t.Errorf("keys[%d] key=%v, want non-empty masked string", i, k["key"])
		} else if key != "****" {
			t.Errorf("keys[%d] key=%q, want fully masked (key source ≤12 chars)", i, key)
		}
		if status, ok := k["status"].(string); !ok || status == "" {
			t.Errorf("keys[%d] status=%v, want non-empty string", i, k["status"])
		}
		if _, ok := k["requests_1m"]; !ok {
			t.Errorf("keys[%d] missing requests_1m field", i)
		}
	}
}

// ── Keys POST ──────────────────────────────────────

func TestKeysPost(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	reqBody := `{"key":"new-test-key"}`
	resp, err := http.Post(srv.URL+"/api/keys", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/keys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var addResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&addResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if idx, ok := addResp["index"].(float64); !ok || int(idx) != 3 {
		t.Errorf("expected index=3, got %v", addResp["index"])
	}
	if key, ok := addResp["key"].(string); !ok || key == "" {
		t.Errorf("expected non-empty masked key, got %v", addResp["key"])
	}
	if addResp["key"] != logentry.MaskKey("new-test-key") {
		t.Errorf("expected key=%q, got %q", logentry.MaskKey("new-test-key"), addResp["key"])
	}

	resp2, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp2.Body.Close()

	var keys []map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&keys); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(keys) != 4 {
		t.Errorf("expected 4 keys after POST, got %d", len(keys))
	}
}

// ── Keys DELETE ────────────────────────────────────

func TestKeysDelete(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp.Body.Close()

	var before []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&before); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	beforeCount := len(before)

	reqBody := `{"index":1}`
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp2.StatusCode)
	}

	var delResp map[string]string
	if err := json.NewDecoder(resp2.Body).Decode(&delResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if delResp["status"] != "removed" {
		t.Errorf(`expected status="removed", got %q`, delResp["status"])
	}

	resp3, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp3.Body.Close()

	var after []map[string]interface{}
	if err := json.NewDecoder(resp3.Body).Decode(&after); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(after) != beforeCount-1 {
		t.Errorf("expected %d keys after DELETE, got %d", beforeCount-1, len(after))
	}
}

// ── Clear ──────────────────────────────────────────

func TestClearHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/clear", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /clear: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["status"] != "cleared" {
		t.Errorf(`expected status="cleared", got %v`, body["status"])
	}
}

// ── Disable Key POST ──────────────────────────────────

func TestDisableKeyHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/api/keys/1/disable", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["success"] != true {
		t.Errorf("expected success=true, got %v", body["success"])
	}

	resp2, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp2.Body.Close()

	var keys []map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&keys); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(keys) < 1 {
		t.Fatal("expected at least 1 key")
	}
	status, _ := keys[0]["status"].(string)
	if status != "disabled" {
		t.Errorf("expected keys[0] status=disabled, got %q", status)
	}

	req2, _ := http.NewRequest("POST", srv.URL+"/api/keys/999/disable", nil)
	resp3, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST /api/keys/999/disable: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for out-of-range index, got %d", resp3.StatusCode)
	}
}

func TestDisableKeyHandlerAuth(t *testing.T) {
	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:  "http://localhost:19999",
			Port:        19999,
			MaxRetries:  3,
			CooldownSec: 60,
			AdminToken:  "my-token",
			Keys:        []string{"key-a", "key-b", "key-c"},
		},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/api/keys/1/disable", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("POST", srv.URL+"/api/keys/1/disable", nil)
	req.Header.Set("X-Admin-Token", "wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable (wrong token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("POST", srv.URL+"/api/keys/1/disable", nil)
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["success"] != true {
		t.Errorf("expected success=true, got %v", body["success"])
	}
}

// ── Cooldown Key PUT ──────────────────────────────────

func TestCooldownKeyHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	req, _ := http.NewRequest("PUT", srv.URL+"/api/keys/1/cooldown", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/keys/1/cooldown: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["success"] != true {
		t.Errorf("expected success=true, got %v", body["success"])
	}

	req2, _ := http.NewRequest("PUT", srv.URL+"/api/keys/999/cooldown", nil)
	resp3, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("PUT /api/keys/999/cooldown: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for out-of-range index, got %d", resp3.StatusCode)
	}
}

// ── Delete Key by Index ───────────────────────────────

func TestDeleteKeyByIndexHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/keys/1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys/1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["success"] != true {
		t.Errorf("expected success=true, got %v", body["success"])
	}

	resp2, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp2.Body.Close()

	var keys []map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&keys); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys after DELETE, got %d", len(keys))
	}

	req2, _ := http.NewRequest("DELETE", srv.URL+"/api/keys/999", nil)
	resp3, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("DELETE /api/keys/999: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for out-of-range index, got %d", resp3.StatusCode)
	}
}

func TestDeleteKeyByIndexHandlerAuth(t *testing.T) {
	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:  "http://localhost:19999",
			Port:        19999,
			MaxRetries:  3,
			CooldownSec: 60,
			AdminToken:  "my-token",
			Keys:        []string{"key-a", "key-b", "key-c"},
		},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/keys/1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys/1 (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("DELETE", srv.URL+"/api/keys/1", nil)
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys/1 (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["success"] != true {
		t.Errorf("expected success=true, got %v", body["success"])
	}
}

// ── DELETE /api/keys — unauthenticated ──────────────────

func TestKeysHandlerDelete_Unauthenticated(t *testing.T) {
	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:  "http://localhost:19999",
			Port:        19999,
			MaxRetries:  3,
			CooldownSec: 60,
			AdminToken:  "my-token",
			Keys:        []string{"key-a", "key-b", "key-c"},
		},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	reqBody := `{"index":1}`
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", resp.StatusCode)
	}
}

// ── DELETE /api/keys — 1-based index validation ─────────────

func TestKeysHandlerDelete_OneBased(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp.Body.Close()
	var before []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&before); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	beforeCount := len(before)

	reqBody := `{"index":1}`
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp2.StatusCode)
	}

	resp3, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp3.Body.Close()

	var after []map[string]interface{}
	if err := json.NewDecoder(resp3.Body).Decode(&after); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(after) != beforeCount-1 {
		t.Errorf("expected %d keys after DELETE, got %d", beforeCount-1, len(after))
	}
}

func TestKeysHandlerDelete_IndexZeroReturns400(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	reqBody := `{"index":0}`
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for index=0, got %d", resp.StatusCode)
	}
}

func TestKeysHandlerDelete_IndexNegativeReturns400(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	reqBody := `{"index":-1}`
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for index=-1, got %d", resp.StatusCode)
	}
}

func TestKeysHandlerDelete_IndexTooLargeReturns400(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	reqBody := `{"index":999}`
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for index=999, got %d", resp.StatusCode)
	}
}
