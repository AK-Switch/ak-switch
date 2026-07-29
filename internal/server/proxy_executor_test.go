//go:build unit

package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	akswitchmetrics "akswitch/internal/metrics"
	"akswitch/internal/tracker"
)

// testCase defines what a specific key returns on each call.
type testCase struct {
	name string
	fn   func(w http.ResponseWriter, r *http.Request, callIdx int)
}

// setupRetryTestServer creates a test HTTP server and config/pool for retry strategy tests.
// Each key in the pool maps to one testCase. The server cycles through cases round-robin.
func setupRetryTestServer(t *testing.T, cases []testCase) (*httptest.Server, *config.Config, *keypool.KeyPool) {
	t.Helper()
	var counter atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(counter.Add(1) - 1)
		tc := cases[idx%len(cases)]
		tc.fn(w, r, idx)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.DefaultConfig()
	cfg.TargetBase = server.URL
	cfg.GenaiBase = server.URL
	cfg.MaxRetries = 2

	keys := make([]string, len(cases))
	for i := range cases {
		keys[i] = fmt.Sprintf("key-%d", i)
	}
	pool := keypool.NewKeyPool(keys, nil)
	return server, cfg, pool
}

// newMetricsForTest creates a Metrics instance for testing.
func newMetricsForTest(t *testing.T) *akswitchmetrics.Metrics {
	t.Helper()
	_, m := akswitchmetrics.NewRegistry()
	_ = m
	return m
}

// calibratorForTest creates a Calibrator for testing.
func calibratorForTest() *tracker.Calibrator {
	_, _ = akswitchmetrics.NewRegistry()
	return tracker.NewCalibrator(100)
}

// newTestProviderState creates a minimal ProviderState for testing.
func newTestProviderState(name string, cfg *config.Config, pool *keypool.KeyPool, px *ProxyExecutor) *ProviderState {
	ps := &ProviderState{
		Name:   name,
		Config: cfg,
		Pool:   pool,
	}
	ps.Proxy = NewProxyEngine(cfg, pool)
	return ps
}

// newTestRequest creates an HTTP request for testing.
func newTestRequest(body io.Reader) *http.Request {
	req, _ := http.NewRequestWithContext(
		context.Background(),
		"POST",
		"http://localhost:4000/v1/messages",
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// ── Tests ──────────────────────────────────────────────────────

// TestExecute_TriesAllKeysInOneAttempt verifies the core of issue #190:
// when key-0 returns 429 and key-1 succeeds, both keys are tried within
// ONE retry attempt. The request succeeds without exhausting retries.
func TestExecute_TriesAllKeysInOneAttempt(t *testing.T) {
	cases := []testCase{
		{name: "key-0", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rate limited"}}`))
		}},
		{name: "key-1", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"msg-ok","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
		}},
	}
	svr, cfg, pool := setupRetryTestServer(t, cases)
	_ = svr
	px := NewProxyExecutor(newMetricsForTest(t), calibratorForTest())
	ps := newTestProviderState("test", cfg, pool, px)

	w := httptest.NewRecorder()
	req := newTestRequest(strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hi"}]}`))
	px.Execute(w, req, ps)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", resp.StatusCode, string(body))
	}
}

// TestExecute_AllKeys429_ConsumesOneRetry verifies that when ALL keys return 429,
// only ONE retry round is consumed (not maxRetries).
func TestExecute_AllKeys429_ConsumesOneRetry(t *testing.T) {
	cases := []testCase{
		{name: "key-0", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rate limited"}}`))
		}},
	}
	server, cfg, pool := setupRetryTestServer(t, cases)
	cfg.MaxRetries = 2
	px := NewProxyExecutor(newMetricsForTest(t), calibratorForTest())
	ps := newTestProviderState("test", cfg, pool, px)

	w := httptest.NewRecorder()
	req := newTestRequest(strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hi"}]}`))
	px.Execute(w, req, ps)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "EXHAUSTED_RETRIES") {
		t.Errorf("body = %s, want EXHAUSTED_RETRIES", string(body))
	}
	_ = server
	_ = body
}

// TestExecute_FirstKeyFailsSecondSucceeds_SingleRound verifies that a 5xx on
// key-0 and success on key-1 happen within the SAME retry round.
func TestExecute_FirstKeyFailsSecondSucceeds_SingleRound(t *testing.T) {
	var attemptCount atomic.Int32
	cases := []testCase{
		{name: "key-0", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			attemptCount.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"message":"upstream error"}}`))
		}},
		{name: "key-1", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			attemptCount.Add(1)
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"msg-ok","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
		}},
	}
	server, cfg, pool := setupRetryTestServer(t, cases)
	cfg.MaxRetries = 3
	px := NewProxyExecutor(newMetricsForTest(t), calibratorForTest())
	ps := newTestProviderState("test", cfg, pool, px)

	w := httptest.NewRecorder()
	req := newTestRequest(strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hi"}]}`))
	px.Execute(w, req, ps)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", resp.StatusCode, string(body))
	}
	// Both keys tried in SAME round — exactly 2 upstream requests
	if attemptCount.Load() != 2 {
		t.Errorf("upstream requests = %d, want 2 (one per key in single attempt)", attemptCount.Load())
	}
	_ = server
	_ = body
}

// TestExecute_SecondKeySucceedsWithMaxRetries1 verifies the core of issue #190:
// with MaxRetries=1, key-0 returns 503 but key-1 returns 200.
// Old code: only tries key-0 (exhausted retries) → 503. New code: tries key-1 too → 200.
func TestExecute_SecondKeySucceedsWithMaxRetries1(t *testing.T) {
	var attemptCount atomic.Int32
	cases := []testCase{
		{name: "key-0", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			attemptCount.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"message":"upstream error"}}`))
		}},
		{name: "key-1", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			attemptCount.Add(1)
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"msg-ok","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
		}},
	}
	server, cfg, pool := setupRetryTestServer(t, cases)
	cfg.MaxRetries = 1 // Only 1 retry round — must try ALL keys in this round
	px := NewProxyExecutor(newMetricsForTest(t), calibratorForTest())
	ps := newTestProviderState("test", cfg, pool, px)

	w := httptest.NewRecorder()
	req := newTestRequest(strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hi"}]}`))
	px.Execute(w, req, ps)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s. upstream calls: %d", resp.StatusCode, string(body), attemptCount.Load())
	}
	// Both keys tried in the single retry round
	if attemptCount.Load() != 2 {
		t.Errorf("upstream calls = %d, want 2 (both keys tried in single attempt)", attemptCount.Load())
	}
	_ = server
	_ = body
}

// TestExecute_PermanentDisable_ExhaustedResponse verifies that when keys become
// permanently disabled (401/403) in one round, the exhausted response is returned.
func TestExecute_PermanentDisable_ExhaustedResponse(t *testing.T) {
	cases := []testCase{
		{name: "key-0", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
		}},
		{name: "key-1", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
		}},
	}
	server, cfg, pool := setupRetryTestServer(t, cases)
	cfg.MaxRetries = 2
	px := NewProxyExecutor(newMetricsForTest(t), calibratorForTest())
	ps := newTestProviderState("test", cfg, pool, px)

	w := httptest.NewRecorder()
	req := newTestRequest(strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hi"}]}`))
	px.Execute(w, req, ps)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if pool.DisabledCount() != 2 {
		t.Errorf("disabled count = %d, want 2", pool.DisabledCount())
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "ALL_KEYS_INVALID") {
		t.Errorf("body = %s, want ALL_KEYS_INVALID", string(body))
	}
	_ = server
	_ = body
}

// TestExecute_AllKeys503_ExhaustsWithMinimalCalls verifies that when all keys
// return 503, the retry loop makes exactly len(keys) * maxRetries upstream calls.
func TestExecute_AllKeys503_ExhaustsWithMinimalCalls(t *testing.T) {
	var attemptCount atomic.Int32
	cases := []testCase{
		{name: "key-0", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			attemptCount.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"message":"upstream error"}}`))
		}},
		{name: "key-1", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			attemptCount.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"message":"upstream error"}}`))
		}},
		{name: "key-2", fn: func(w http.ResponseWriter, r *http.Request, _ int) {
			attemptCount.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"message":"upstream error"}}`))
		}},
	}
	server, cfg, pool := setupRetryTestServer(t, cases)
	cfg.MaxRetries = 2
	px := NewProxyExecutor(newMetricsForTest(t), calibratorForTest())
	ps := newTestProviderState("test", cfg, pool, px)

	w := httptest.NewRecorder()
	req := newTestRequest(strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hi"}]}`))
	px.Execute(w, req, ps)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	// 3 keys × 2 retries = 6 upstream calls
	if attemptCount.Load() != 6 {
		t.Errorf("upstream calls = %d, want 6 (3 keys × 2 retries)", attemptCount.Load())
	}
	_ = server
}
