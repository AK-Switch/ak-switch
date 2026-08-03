//go:build integration

package integration

import (
	"encoding/json"

	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"testing"


	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/logentry"
	"akswitch/internal/server"
)

func TestHealthHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf(`expected status="ok", got %v`, body["status"])
	}

	if n, ok := body["providers"].(float64); !ok || int(n) != 1 {
		t.Errorf("expected providers=1, got %v", body["providers"])
	}

	details, ok := body["details"].(map[string]interface{})
	if !ok {
		t.Fatal("expected details field with per-provider data")
	}
	testProv, ok := details["test"].(map[string]interface{})
	if !ok {
		t.Fatal("expected test provider in details")
	}
	if n, ok := testProv["keys"].(float64); !ok || int(n) != 3 {
		t.Errorf("expected keys=3 for test provider, got %v", testProv["keys"])
	}
}

// TestHealthHandlerProviderFilter verifies that /health?provider=<name> returns
// only that provider's data, /health?provider=<unknown> returns 404, and
// /health (no param) returns all providers.
func TestHealthHandlerProviderFilter(t *testing.T) {
	cfg1 := &config.Config{
		TargetBase: "http://localhost:19998",
		Port:       19999, MaxRetries: 3, CooldownSec: 60,
		Keys: []string{"key-a", "key-b", "key-c"},
	}
	cfg2 := &config.Config{
		TargetBase: "http://localhost:19997",
		Port:       19998, MaxRetries: 3, CooldownSec: 60,
		Keys: []string{"key-x", "key-y"},
	}
	pool1 := keypool.NewKeyPool(cfg1.Keys, nil)
	pool2 := keypool.NewKeyPool(cfg2.Keys, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("alpha", cfg1, pool1)
	pr.AddProvider("beta", cfg2, pool2)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Scoped: only "alpha"
	resp, err := http.Get(srv.URL + "/health?provider=alpha")
	if err != nil {
		t.Fatalf("GET /health?provider=alpha: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	details, ok := body["details"].(map[string]interface{})
	if !ok {
		t.Fatal("expected details field")
	}
	if _, hasAlpha := details["alpha"]; !hasAlpha {
		t.Error("expected 'alpha' provider in filtered response")
	}
	if _, hasBeta := details["beta"]; hasBeta {
		t.Error("did not expect 'beta' in alpha-filtered response")
	}
	if len(details) != 1 {
		t.Errorf("expected 1 provider in filtered response, got %d", len(details))
	}

	// Unknown provider → 404
	resp2, err := http.Get(srv.URL + "/health?provider=nonexistent")
	if err != nil {
		t.Fatalf("GET /health?provider=nonexistent: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown provider, got %d", resp2.StatusCode)
	}
}

// ── Config GET ─────────────────────────────────────

func TestConfigGet(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/config")
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["targetBase"] != "http://localhost:19999" {
		t.Errorf(`expected targetBase="http://localhost:19999", got %v`, body["targetBase"])
	}

	keys, ok := body["keys"].([]interface{})
	if !ok {
		t.Fatal("expected keys field as array")
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}

	expectedMasked := logentry.MaskKey("key-a")

	for i, k := range keys {
		masked, ok := k.(string)
		if !ok {
			t.Errorf("keys[%d] is not a string", i)
			continue
		}
		// All keys should be masked — none should equal the raw key
		if masked == "key-a" || masked == "key-b" || masked == "key-c" {
			t.Errorf("keys[%d]=%q appears unmasked", i, masked)
		}
		// The masking format should match logentry.MaskKey()
		if i == 0 && masked != expectedMasked {
			t.Errorf("keys[0]=%q, want masking like %q", masked, expectedMasked)
		}
	}
}

// ── Config POST ────────────────────────────────────

func TestConfigPost(t *testing.T) {
	// ConfigPost 会写 .env 并调用 reloadConfig，需要隔离到临时目录
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// 写入初始 .env 供 reloadConfig 使用
	envContent := "PORT=19999\nTARGET_BASE_URL=http://localhost:19999\nAPI_KEYS=key-a,key-b\nCOOLDOWN_SEC=60\nMAX_RETRIES=3\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "",
		Keys:        []string{"key-a", "key-b"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// POST /api/config is no longer supported in ProviderRouter architecture
	reqBody := `{"targetBase":"https://new.example.com/v1","keys":["new-key-1","new-key-2"]}`
	resp, err := http.Post(srv.URL+"/api/config", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405 (POST removed), got %d", resp.StatusCode)
	}
}

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
		// index is 1-based in keysHandler GET
		if idx, ok := k["index"].(float64); !ok || int(idx) != i+1 {
			t.Errorf("keys[%d] index=%v, want %d", i, k["index"], i+1)
		}
		if key, ok := k["key"].(string); !ok || key == "" {
			t.Errorf("keys[%d] key=%v, want non-empty masked string", i, k["key"])
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

	// POST 添加新 key
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

	// AddKey 返回的 index 为 0-based: 长度 3→新 key idx=3
	if idx, ok := addResp["index"].(float64); !ok || int(idx) != 3 {
		t.Errorf("expected index=3, got %v", addResp["index"])
	}
	if key, ok := addResp["key"].(string); !ok || key == "" {
		t.Errorf("expected non-empty masked key, got %v", addResp["key"])
	}
	if addResp["key"] != logentry.MaskKey("new-test-key") {
		t.Errorf("expected key=%q, got %q", logentry.MaskKey("new-test-key"), addResp["key"])
	}

	// GET 验证 key 数量为 4
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

	// 先 GET 确认当前 key 数
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

	// DELETE 移除 index=1 (1-based) 的 key
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

	// GET 验证 key 数减 1
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

// ── Health with AdminToken auth ──────────────────────

func TestHealthHandlerAuth(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Without token → 401
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// With wrong token → 401
	req, _ := http.NewRequest("GET", srv.URL+"/health", nil)
	req.Header.Set("X-Admin-Token", "wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health (wrong token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", resp.StatusCode)
	}

	// With correct token → 200
	req, _ = http.NewRequest("GET", srv.URL+"/health", nil)
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf(`expected status="ok", got %v`, body["status"])
	}
}

// ── Clear with AdminToken auth ───────────────────────

func TestClearHandlerAuth(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Without token → 401
	resp, err := http.Post(srv.URL+"/clear", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /clear (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// With wrong token → 401
	req, _ := http.NewRequest("POST", srv.URL+"/clear", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", "wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /clear (wrong token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", resp.StatusCode)
	}

	// With correct token → 200
	req, _ = http.NewRequest("POST", srv.URL+"/clear", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /clear (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "cleared" {
		t.Errorf(`expected status="cleared", got %v`, body["status"])
	}
}

// ── Stats GET ───────────────────────────────────────

func TestStatsHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatalf("GET /api/stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	fields := []string{"active_keys", "cooling_keys", "disabled_keys", "uptime_seconds"}
	for _, f := range fields {
		if _, ok := body[f]; !ok {
			t.Errorf("missing field %q in response", f)
		}
	}
}

// TestStatsHandlerPerProvider verifies that /api/stats?provider=<name> returns
// key counts scoped to a single provider, while /api/stats (no param) returns
// aggregated totals across all providers.
func TestStatsHandlerPerProvider(t *testing.T) {
	cfg1 := &config.Config{
		TargetBase: "http://localhost:19998",
		Port:       19999,
		MaxRetries: 3, CooldownSec: 60,
		Keys: []string{"key-a", "key-b", "key-c"},
	}
	cfg2 := &config.Config{
		TargetBase: "http://localhost:19997",
		Port:       19998,
		MaxRetries: 3, CooldownSec: 60,
		Keys: []string{"key-x", "key-y"},
	}
	pool1 := keypool.NewKeyPool(cfg1.Keys, nil)
	pool2 := keypool.NewKeyPool(cfg2.Keys, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("alpha", cfg1, pool1)
	pr.AddProvider("beta", cfg2, pool2)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// No provider param → aggregated: 3 + 2 = 5 active keys
	resp, err := http.Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatalf("GET /api/stats: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var allStats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&allStats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(allStats["active_keys"].(float64)) != 5 {
		t.Errorf("expected active_keys=5 (aggregated), got %v", allStats["active_keys"])
	}

	// Provider=alpha → scoped: 3 active keys
	resp2, err := http.Get(srv.URL + "/api/stats?provider=alpha")
	if err != nil {
		t.Fatalf("GET /api/stats?provider=alpha: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
	var alphaStats map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&alphaStats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(alphaStats["active_keys"].(float64)) != 3 {
		t.Errorf("expected active_keys=3 for alpha, got %v", alphaStats["active_keys"])
	}

	// Provider=beta → scoped: 2 active keys
	resp3, err := http.Get(srv.URL + "/api/stats?provider=beta")
	if err != nil {
		t.Fatalf("GET /api/stats?provider=beta: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp3.StatusCode)
	}
	var betaStats map[string]interface{}
	if err := json.NewDecoder(resp3.Body).Decode(&betaStats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(betaStats["active_keys"].(float64)) != 2 {
		t.Errorf("expected active_keys=2 for beta, got %v", betaStats["active_keys"])
	}

	// Unknown provider → 404
	resp4, err := http.Get(srv.URL + "/api/stats?provider=nonexistent")
	if err != nil {
		t.Fatalf("GET /api/stats?provider=nonexistent: %v", err)
	}
	resp4.Body.Close()
	if resp4.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown provider, got %d", resp4.StatusCode)
	}

	// Verify required fields exist in per-provider response
	fields := []string{"active_keys", "cooling_keys", "disabled_keys", "uptime_seconds"}
	for _, f := range fields {
		if _, ok := alphaStats[f]; !ok {
			t.Errorf("missing field %q in per-provider stats response", f)
		}
	}
}

// TestStatsHandlerPerProviderAfterDisable verifies that /api/stats?provider=<name>
// reflects disabled keys correctly.
func TestStatsHandlerPerProviderAfterDisable(t *testing.T) {
	cfg := &config.Config{
		TargetBase: "http://localhost:19999",
		Port:       19999, MaxRetries: 3, CooldownSec: 60, AdminToken: "",
		Keys: []string{"key-a", "key-b", "key-c"},
	}
	pool := keypool.NewKeyPool(cfg.Keys, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Disable key[1] via API
	req, _ := http.NewRequest("POST", srv.URL+"/api/keys/1/disable", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable: %v", err)
	}
	resp.Body.Close()

	// Per-provider stats should show 2 active, 1 disabled
	resp2, err := http.Get(srv.URL + "/api/stats?provider=test")
	if err != nil {
		t.Fatalf("GET /api/stats?provider=test: %v", err)
	}
	defer resp2.Body.Close()
	var stats map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(stats["active_keys"].(float64)) != 2 {
		t.Errorf("expected active_keys=2 after disable, got %v", stats["active_keys"])
	}
	if int(stats["disabled_keys"].(float64)) != 1 {
		t.Errorf("expected disabled_keys=1 after disable, got %v", stats["disabled_keys"])
	}
}

// ── Disable Key POST ──────────────────────────────────

func TestDisableKeyHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	// 禁用 index=1
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

	// GET /api/keys 验证该 key 状态为 "disabled"
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

	// 越界 index=999 → 404
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
		TargetBase:  "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
		Keys:        []string{"key-a", "key-b", "key-c"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Without token → 401
	req, _ := http.NewRequest("POST", srv.URL+"/api/keys/1/disable", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// Wrong token → 401
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

	// Correct token → 200
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

	// 冷却 index=1
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

	// 越界 index=999 → 404
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

	// 删除 index=1
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

	// GET /api/keys 验证只剩 2 个 key
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

	// 越界 index=999 → 404
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
		TargetBase:  "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
		Keys:        []string{"key-a", "key-b", "key-c"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Without token → 401
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/keys/1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys/1 (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// With correct token → 200
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

// ── Reload POST ──────────────────────────────────────

func TestReloadHandler(t *testing.T) {
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	// Write a valid config.toml at the XDG path for reload to read
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(xdgPath), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	tomlContent := `[provider.test]
target = "http://localhost:19999/v1"
port = 19999
max_retries = 3
cooldown_sec = 60
`
	if err := os.WriteFile(xdgPath, []byte(tomlContent), 0600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "",
		Keys:        []string{"key-a", "key-b"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/reload", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /api/reload: %v", err)
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
}

// ── Log Level API ─────────────────────────────────────

func TestLogLevelHandler_Success(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/log-level", "application/json", strings.NewReader(`{"level":"debug"}`))
	if err != nil {
		t.Fatalf("POST /api/log-level: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if level, ok := body["level"].(string); !ok || level != "debug" {
		t.Errorf(`expected level="debug", got %v`, body["level"])
	}
}

func TestLogLevelHandler_InvalidLevel(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/log-level", "application/json", strings.NewReader(`{"level":"verbose"}`))
	if err != nil {
		t.Fatalf("POST /api/log-level: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestLogLevelHandler_GetLevel(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/log-level")
	if err != nil {
		t.Fatalf("GET /api/log-level: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if level, ok := body["level"].(string); !ok || level == "" {
		t.Errorf(`expected level to be non-empty, got %v`, body["level"])
	}
}

func TestLogLevelHandler_Unauthorized(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "secret-token",
		Keys:        []string{"key-a"},
	}
	pool := keypool.NewKeyPool([]string{"key-a"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// No token → 401
	resp, err := http.Post(srv.URL+"/api/log-level", "application/json", strings.NewReader(`{"level":"debug"}`))
	if err != nil {
		t.Fatalf("POST /api/log-level: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}
}

// ── Runtime Config API ─────────────────────────────────

func TestRuntimeConfigHandler_GetList(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/runtime-config")
	if err != nil {
		t.Fatalf("GET /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// No provider specified: returns all providers as a map
	var body map[string]map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("expected at least one provider in response")
	}

	for name, params := range body {
		if name == "" {
			t.Errorf("expected non-empty provider name")
		}
		if _, ok := params["http_timeout_sec"]; !ok {
			t.Errorf("expected http_timeout_sec in params for %s", name)
		}
		if _, ok := params["max_retries"]; !ok {
			t.Errorf("expected max_retries in params for %s", name)
		}
		// log_level is no longer in per-provider params
		break // only check first provider
	}
}

func TestRuntimeConfigHandler_GetSingleKey(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	// Use ?provider=test (the default provider name from newTestServer)
	resp, err := http.Get(srv.URL + "/api/runtime-config?provider=test&key=http_timeout_sec")
	if err != nil {
		t.Fatalf("GET /api/runtime-config?provider=test&key=http_timeout_sec: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if key, ok := body["key"].(string); !ok || key != "http_timeout_sec" {
		t.Errorf("expected key=http_timeout_sec, got %v", body["key"])
	}
	if _, ok := body["value"]; !ok {
		t.Errorf("expected value to be present")
	}
}

func TestRuntimeConfigHandler_GetUnknownKey(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/runtime-config?provider=test&key=unknown_key")
	if err != nil {
		t.Fatalf("GET /api/runtime-config?key=unknown_key: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestRuntimeConfigHandler_SetHttpTimeout(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	// Set http_timeout_sec to 60
	payload := `{"key":"http_timeout_sec","value":60}`
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if key, ok := body["key"].(string); !ok || key != "http_timeout_sec" {
		t.Errorf("expected key=http_timeout_sec, got %v", body["key"])
	}
	// JSON numbers are float64; 60 == 60.0
	if val, ok := body["value"].(float64); !ok || int(val) != 60 {
		t.Errorf("expected value=60, got %v", body["value"])
	}

	// Verify the change was applied
	checkResp, err := http.Get(srv.URL + "/api/runtime-config?provider=test&key=http_timeout_sec")
	if err != nil {
		t.Fatalf("GET /api/runtime-config?key=http_timeout_sec: %v", err)
	}
	defer checkResp.Body.Close()
	var checkBody map[string]interface{}
	if err := json.NewDecoder(checkResp.Body).Decode(&checkBody); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if val, ok := checkBody["value"].(float64); !ok || int(val) != 60 {
		t.Errorf("expected value=60 after set, got %v", checkBody["value"])
	}
}

func TestRuntimeConfigHandler_SetMaxRetries(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	payload := `{"key":"max_retries","value":5}`
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if val, ok := body["value"].(float64); !ok || int(val) != 5 {
		t.Errorf("expected value=5, got %v", body["value"])
	}
}

func TestRuntimeConfigHandler_SetLogLevel(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	payload := `{"key":"log_level","value":"debug"}`
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if val, ok := body["value"].(string); !ok || val != "debug" {
		t.Errorf("expected value=debug, got %v", body["value"])
	}

	// Verify via log-level API
	checkResp, err := http.Get(srv.URL + "/api/log-level")
	if err != nil {
		t.Fatalf("GET /api/log-level: %v", err)
	}
	defer checkResp.Body.Close()
	var checkBody map[string]interface{}
	if err := json.NewDecoder(checkResp.Body).Decode(&checkBody); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if level, ok := checkBody["level"].(string); !ok || level != "debug" {
		t.Errorf("expected log level debug, got %v", checkBody["level"])
	}
}

func TestRuntimeConfigHandler_SetInvalidKey(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	payload := `{"key":"invalid_key","value":123}`
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestRuntimeConfigHandler_SetInvalidLogLevel(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	payload := `{"key":"log_level","value":"verbose"}`
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestRuntimeConfigHandler_Unauthorized(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "secret-token",
		Keys:        []string{"key-a"},
	}
	pool := keypool.NewKeyPool([]string{"key-a"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// No token → 401
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(`{"key":"http_timeout_sec","value":60}`))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}

	// GET also requires token
	getResp, err := http.Get(srv.URL + "/api/runtime-config")
	if err != nil {
		t.Fatalf("GET /api/runtime-config: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401 for GET, got %d", getResp.StatusCode)
	}
}

// ── Keys DELETE — 1-based index validation ─────────────

func TestKeysHandlerDelete_OneBased(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	// GET initial count
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

	// DELETE index=1 (1-based) → should succeed
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

	// GET after delete — count should be reduced by 1
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

// ── DELETE /api/keys — unauthenticated ──────────────────

func TestKeysHandlerDelete_Unauthenticated(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
		Keys:        []string{"key-a", "key-b", "key-c"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Without token → 401
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

	// With correct token → 200
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

// ── Config POST — unauthenticated ──────────────────────

func TestConfigHandlerPost_Unauthenticated(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// Write initial .env for ReloadConfig
	envContent := "PORT=19999\nTARGET_BASE_URL=http://localhost:19999\nAPI_KEYS=key-a,key-b\nCOOLDOWN_SEC=60\nMAX_RETRIES=3\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
		Keys:        []string{"key-a", "key-b"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// POST /api/config is no longer supported — both no token and with token return 405
	reqBody := `{"targetBase":"http://example.com","keys":["new-key"]}`
	resp, err := http.Post(srv.URL+"/api/config", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/config (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST (method not allowed), got %d", resp.StatusCode)
	}

	// With correct token — still 405
	req, _ := http.NewRequest("POST", srv.URL+"/api/config", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/config (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 with token, got %d", resp.StatusCode)
	}
}


