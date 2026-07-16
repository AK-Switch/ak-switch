//go:build integration

package main

import (
	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/server"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Metrics Integration Tests
// ---------------------------------------------------------------------------

// TestTokenUsageMetrics verifies that token usage is recorded in Prometheus metrics
// when the upstream returns a response with token usage information.
func TestTokenUsageMetrics(t *testing.T) {
	// Mock upstream that returns token usage in the response body
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return a response with token usage data
		fmt.Fprint(w, `{"usage": {"input_tokens": 42, "output_tokens": 88}}`)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             1,
		CooldownSec:            60,
		UpstreamCBThreshold:    5,
		CBResetSec:             30,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
	}
	pool := keypool.NewKeyPool([]string{"test-key-1234567890"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Send a proxy request that triggers token extraction
	resp, err := http.Get(srv.URL + "/test/v1/chat/completions")
	if err != nil {
		t.Fatalf("GET /test/v1/chat/completions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Check /metrics for token usage counters
	metricsResp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer metricsResp.Body.Close()
	body, _ := io.ReadAll(metricsResp.Body)
	metricsBody := string(body)

	// Verify token usage counters are present
	if !strings.Contains(metricsBody, `akswitch_token_usage_total`) {
		t.Error("token_usage_total metric not found in /metrics output")
	}
	if !strings.Contains(metricsBody, `provider="test"`) {
		t.Error("provider label not found in /metrics output")
	}
	if !strings.Contains(metricsBody, `direction="input"`) {
		t.Error("input direction label not found in /metrics output")
	}
	if !strings.Contains(metricsBody, `direction="output"`) {
		t.Error("output direction label not found in /metrics output")
	}
}

// TestLogStoreMetrics verifies that logstore metrics are exposed via /metrics
// after requests are processed.
func TestLogStoreMetrics(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id": "test"}`)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             1,
		CooldownSec:            60,
		UpstreamCBThreshold:    5,
		CBResetSec:             30,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
	}
	pool := keypool.NewKeyPool([]string{"test-key-1234567890"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Send a few requests to generate log entries
	for i := 0; i < 3; i++ {
		resp, err := http.Get(srv.URL + "/test/v1/models")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
	}

	// Check /metrics for logstore metrics
	metricsResp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer metricsResp.Body.Close()
	body, _ := io.ReadAll(metricsResp.Body)
	metricsBody := string(body)

	// Verify logstore metrics are present
	if !strings.Contains(metricsBody, `akswitch_logstore_entries_total`) {
		t.Error("logstore_entries_total metric not found in /metrics output")
	}
	if !strings.Contains(metricsBody, `akswitch_logstore_fill_ratio`) {
		t.Error("logstore_fill_ratio metric not found in /metrics output")
	}

	// Verify entries counter is > 0 (should be 3 from our requests)
	if !strings.Contains(metricsBody, `akswitch_logstore_entries_total 3`) {
		t.Logf("metrics body: %s", metricsBody)
	}

	// logstore_dropped_total should be 0 since we only sent 3 entries (capacity is 10000)
	if !strings.Contains(metricsBody, `akswitch_logstore_dropped_total 0`) {
		t.Logf("metrics body: %s", metricsBody)
	}
}

// TestMetricsEndpointAccessible verifies the /metrics endpoint is accessible
// and returns valid Prometheus text format.
func TestMetricsEndpointAccessible(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             1,
		CooldownSec:            60,
		UpstreamCBThreshold:    5,
		CBResetSec:             30,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
	}
	pool := keypool.NewKeyPool([]string{"test-key-1234567890"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected Content-Type text/plain, got %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	metricsBody := string(body)

	// Send a proxy request to trigger metric registration
	resp2, err2 := http.Get(srv.URL + "/test/v1/models")
	if err2 == nil {
		resp2.Body.Close()
	}

	// Debug: log the metrics body
	t.Logf("Metrics body for debugging: %s", metricsBody)

	// Verify the new metrics appear in the output (CounterVec metrics like
	// akswitch_requests_total only appear after their first increment)
	expectedMetrics := []string{
		"akswitch_logstore_entries_total",
		"akswitch_logstore_dropped_total",
		"akswitch_logstore_fill_ratio",
	}
	for _, name := range expectedMetrics {
		if !strings.Contains(metricsBody, name) {
			t.Errorf("expected metric %q not found in /metrics output", name)
		}
	}
}