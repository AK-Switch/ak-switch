//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/server"
)

// TestCB_RateLimitRecovery verifies that 429 triggers exponential backoff
// but the key recovers after the backoff period and success is possible.
func TestCB_RateLimitRecovery(t *testing.T) {
	var mu sync.Mutex
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count := callCount
		callCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if count < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limited"}`))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer upstream.Close()

	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:          upstream.URL,
			Port:                0,
			MaxRetries:          10,
			CooldownSec:         2,
			BackoffCapSec:       120,
			BackoffMultiplier:   2,
			CBResetSec:          30,
			UpstreamCBThreshold: 5,
		},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	ts := httptest.NewServer(pr.Handler())
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after 429 recovery, got %d", resp.StatusCode)
	}
}

// TestCB_QuotaExhausted verifies that repeated 429s lead to retry exhaustion
// (503 EXHAUSTED_RETRIES) rather than permanently disabling the key.
func TestCB_QuotaExhausted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:          upstream.URL,
			Port:                0,
			MaxRetries:          3,
			CooldownSec:         1,
			BackoffCapSec:       5,
			BackoffMultiplier:   2,
			CBResetSec:          60,
			UpstreamCBThreshold: 10,
		},
	}
	pool := keypool.NewKeyPool([]string{"single-key"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	ts := httptest.NewServer(pr.Handler())
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", resp.StatusCode)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON error response: %v", err)
	}
	if body.Error.Code != "EXHAUSTED_RETRIES" {
		t.Errorf("expected error.code EXHAUSTED_RETRIES, got %q", body.Error.Code)
	}
}

// TestCB_UpstreamErrorNoKeyPenalty verifies that 502/503 errors do NOT
// disable the API key — only the upstream circuit breaker is affected.
func TestCB_UpstreamErrorNoKeyPenalty(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"upstream down"}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:          upstream.URL,
			Port:                0,
			MaxRetries:          3,
			CooldownSec:         1,
			BackoffCapSec:       120,
			BackoffMultiplier:   2,
			CBResetSec:          300,
			UpstreamCBThreshold: 10,
		},
	}
	pool := keypool.NewKeyPool([]string{"test-key"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	ts := httptest.NewServer(pr.Handler())
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", resp.StatusCode)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON error response: %v", err)
	}
	if body.Error.Code != "EXHAUSTED_RETRIES" {
		t.Errorf("expected error.code EXHAUSTED_RETRIES, got %q", body.Error.Code)
	}

	healthResp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer healthResp.Body.Close()

	var health struct {
		Details map[string]struct {
			Status string `json:"status"`
			Keys   int    `json:"keys"`
		} `json:"details"`
	}
	if err := json.NewDecoder(healthResp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	ph, ok := health.Details["test"]
	if !ok {
		t.Fatal("health response missing provider 'test'")
	}
	if ph.Keys == 0 {
		t.Error("expected at least 1 key after 503 errors (upstream error should not disable keys)")
	}
}

// TestCB_UpstreamCircuitBreakerOpens verifies that after UPSTREAM_CB_THRESHOLD
// consecutive 503s, the upstream circuit breaker opens and subsequent retries
// fail fast without calling the upstream.
func TestCB_UpstreamCircuitBreakerOpens(t *testing.T) {
	var mu sync.Mutex
	upstreamCallCount := 0

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		upstreamCallCount++
		mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"upstream down"}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:          upstream.URL,
			Port:                0,
			MaxRetries:          10,
			CooldownSec:         1,
			BackoffCapSec:       120,
			BackoffMultiplier:   2,
			CBResetSec:          60,
			UpstreamCBThreshold: 3,
		},
	}
	pool := keypool.NewKeyPool([]string{"test-key"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	ts := httptest.NewServer(pr.Handler())
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	mu.Lock()
	count := upstreamCallCount
	mu.Unlock()
	if count > 5 {
		t.Errorf("expected at most ~3 upstream calls after CB opens, got %d", count)
	}
	t.Logf("upstream call count: %d (threshold=3, should be ~3)", count)
}
