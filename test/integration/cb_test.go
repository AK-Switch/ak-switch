//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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

	// First request: all 3 keys get 429 → all keys cooling → 429
	req, err := http.NewRequest("GET", ts.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on first request (all keys cooling), got %d", resp.StatusCode)
	}

	// Wait for the key cooldown to expire (2s backoff, plus up to 50% jitter)
	time.Sleep(4 * time.Second)

	// Second request: all keys recovered, upstream returns 200
	req2, err := http.NewRequest("GET", ts.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after 429 recovery, got %d", resp2.StatusCode)
	}
}

// TestCB_QuotaExhausted verifies that repeated 429s escalate to PERMA
// and return ALL_KEYS_INVALID when all keys are exhausted.
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

	// Send multiple requests with waits between them so the key's
	// exponential backoff escalates to Permanent (backoff cap reached).
	// Each request returns 429 immediately (all keys cooling),
	// then we wait for the cooldown to expire before retrying.
	//
	// Attempts: 0(raw=1s), 1(raw=2s), 2(raw=4s), 3(raw=8s≥5s cap → Permanent)
	// Each wait = 2×raw + 2s buffer (covers up to 50% jitter).
	for i := 0; i < 3; i++ {
		req, err := http.NewRequest("GET", ts.URL+"/test", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("request %d: expected 429, got %d", i+1, resp.StatusCode)
		}
		time.Sleep(time.Duration(2<<i) * time.Second) // 2s, 4s, 8s
	}

	// Fourth request: key reaches backoff cap → Permanent → ALL_KEYS_INVALID → 503
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
	if body.Error.Code != "ALL_KEYS_INVALID" {
		t.Errorf("expected error.code ALL_KEYS_INVALID, got %q", body.Error.Code)
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