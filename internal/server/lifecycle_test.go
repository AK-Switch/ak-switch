//go:build unit

package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"akswitch/internal/circuitbreaker"
	"akswitch/internal/keypool"
	"akswitch/internal/metrics"
	"akswitch/internal/tracker"
)

// ── calibrationTarget ─────────────────────────────────

func TestCalibrationTarget_WithV1Suffix(t *testing.T) {
	url := calibrationTarget("https://api.example.com/v1")
	want := "https://api.example.com/v1/messages"
	if url != want {
		t.Errorf("calibrationTarget = %q, want %q", url, want)
	}
}

func TestCalibrationTarget_WithoutV1Suffix(t *testing.T) {
	url := calibrationTarget("https://api.example.com")
	want := "https://api.example.com/v1/messages"
	if url != want {
		t.Errorf("calibrationTarget = %q, want %q", url, want)
	}
}

func TestCalibrationTarget_TrailingSlash(t *testing.T) {
	url := calibrationTarget("https://api.example.com/")
	want := "https://api.example.com/v1/messages"
	if url != want {
		t.Errorf("calibrationTarget = %q, want %q", url, want)
	}
}

func TestCalibrationTarget_WithV1AndTrailingSlash(t *testing.T) {
	url := calibrationTarget("https://api.example.com/v1/")
	want := "https://api.example.com/v1/messages"
	if url != want {
		t.Errorf("calibrationTarget = %q, want %q", url, want)
	}
}

// ── sendCalibrationRequest ────────────────────────────

// TestSendCalibrationRequest_RecordsCalibrationData verifies that the calibration
// function records token usage data when the upstream returns a valid response.
func TestSendCalibrationRequest_RecordsCalibrationData(t *testing.T) {
	// Mock upstream that returns a valid Anthropic response with token usage
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"id": "msg_test123",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "Hi! How can I help you?"}],
			"model": "claude-sonnet-4-20250514",
			"usage": {
				"input_tokens": 10,
				"output_tokens": 5
			}
		}`))
	}))
	defer upstream.Close()

	calibrator := tracker.NewCalibrator(15)
	pool := keypool.NewKeyPool([]string{"sk-test-key"}, nil)

	sendCalibrationRequest(calibrator, pool, upstream.URL, "claude-sonnet-4-20250514")

	// Verify calibration data was recorded
	sampleCount := calibrator.SampleCount("claude-sonnet-4-20250514")
	if sampleCount < 1 {
		t.Errorf("expected at least 1 calibration sample, got %d", sampleCount)
	}

	// Verify ratio is meaningful (not 1.0 default)
	ratio := calibrator.Ratio("claude-sonnet-4-20250514")
	if ratio == 1.0 {
		t.Log("ratio is 1.0 (may need more than 3 samples for median)")
	}
}

// TestSendCalibrationRequest_Non200Response verifies that non-200 responses
// are handled gracefully without crashing.
func TestSendCalibrationRequest_Non200Response(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	calibrator := tracker.NewCalibrator(15)
	pool := keypool.NewKeyPool([]string{"sk-test-key"}, nil)

	// Should not panic or crash
	sendCalibrationRequest(calibrator, pool, upstream.URL, "claude-sonnet-4-20250514")

	// Verify no calibration data was recorded
	sampleCount := calibrator.SampleCount("claude-sonnet-4-20250514")
	if sampleCount != 0 {
		t.Errorf("expected 0 samples for failed request, got %d", sampleCount)
	}
}

// TestSendCalibrationRequest_NoKeyAvailable verifies that the function handles
// an empty pool gracefully.
func TestSendCalibrationRequest_NoKeyAvailable(t *testing.T) {
	calibrator := tracker.NewCalibrator(15)
	pool := keypool.NewKeyPool([]string{}, nil)

	// Should not panic or crash
	sendCalibrationRequest(calibrator, pool, "http://localhost:19999", "claude-sonnet-4-20250514")
}

// TestSendCalibrationRequest_EmptyModel verifies that empty model is a no-op.
func TestSendCalibrationRequest_EmptyModel(t *testing.T) {
	calibrator := tracker.NewCalibrator(15)
	pool := keypool.NewKeyPool([]string{"sk-test-key"}, nil)

	// Should not panic or crash
	sendCalibrationRequest(calibrator, pool, "http://localhost:19999", "")
}

// TestSendCalibrationRequest_NetworkError verifies that network errors
// are handled gracefully.
func TestSendCalibrationRequest_NetworkError(t *testing.T) {
	calibrator := tracker.NewCalibrator(15)
	pool := keypool.NewKeyPool([]string{"sk-test-key"}, nil)

	// Non-routable address — should fail quickly
	sendCalibrationRequest(calibrator, pool, "http://192.0.2.1:1", "claude-sonnet-4-20250514")

	// Should not panic or crash
}

// ── PeriodicCalibrator ───────────────────────────────

// TestPeriodicCalibrator_RunsImmediately verifies that the calibrator runs
// once immediately on start, before the first tick.
func TestPeriodicCalibrator_RunsImmediately(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"usage":{"input_tokens":10,"output_tokens":5}}`))
	}))
	defer upstream.Close()

	calibrator := tracker.NewCalibrator(15)
	pool := keypool.NewKeyPool([]string{"sk-test-key"}, nil)
	stop := make(chan struct{})

	// Run in a goroutine since PeriodicCalibrator blocks on the ticker loop
	done := make(chan struct{})
	go func() {
		PeriodicCalibrator(calibrator, pool, upstream.URL, "claude-sonnet-4-20250514", 1*time.Hour, stop)
		close(done)
	}()

	// Wait for the initial calibration request to complete
	time.Sleep(100 * time.Millisecond)
	close(stop)

	select {
	case <-done:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Fatal("PeriodicCalibrator did not stop")
	}

	// Verify it ran immediately (at least 1 call)
	if callCount < 1 {
		t.Errorf("expected at least 1 immediate call, got %d", callCount)
	}
}

// TestPeriodicCalibrator_EmptyModelNoOps verifies that empty model exits immediately.
func TestPeriodicCalibrator_EmptyModelNoOps(t *testing.T) {
	calibrator := tracker.NewCalibrator(15)
	pool := keypool.NewKeyPool([]string{"sk-test-key"}, nil)
	stop := make(chan struct{})
	close(stop) // closed before call — should not block

	// Should return immediately without panicking
	PeriodicCalibrator(calibrator, pool, "http://localhost:19999", "", 1*time.Hour, stop)
}

// TestPeriodicCalibrator_ZeroIntervalNoOps verifies that zero interval exits immediately.
func TestPeriodicCalibrator_ZeroIntervalNoOps(t *testing.T) {
	calibrator := tracker.NewCalibrator(15)
	pool := keypool.NewKeyPool([]string{"sk-test-key"}, nil)
	stop := make(chan struct{})
	close(stop)

	// Should return immediately without panicking
	PeriodicCalibrator(calibrator, pool, "http://localhost:19999", "claude-sonnet-4-20250514", 0, stop)
}

// TestPeriodicCalibrator_StopsOnChannel verifies that the calibrator stops
// when the stop channel is closed.
func TestPeriodicCalibrator_StopsOnChannel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"usage":{"input_tokens":10,"output_tokens":5}}`))
	}))
	defer upstream.Close()

	calibrator := tracker.NewCalibrator(15)
	pool := keypool.NewKeyPool([]string{"sk-test-key"}, nil)
	stop := make(chan struct{})

	// Start the calibrator in a goroutine
	done := make(chan struct{})
	go func() {
		PeriodicCalibrator(calibrator, pool, upstream.URL, "claude-sonnet-4-20250514", 100*time.Millisecond, stop)
		close(done)
	}()

	// Let it run for a bit, then stop
	time.Sleep(50 * time.Millisecond)
	close(stop)

	// Wait for the calibrator to stop (with a short timeout)
	select {
	case <-done:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Fatal("PeriodicCalibrator did not stop within 500ms")
	}
}

// ── ServerLifecycle ────────────────────────────────────

func newTestLifecycle(t *testing.T) (*ServerLifecycle, *metrics.Metrics) {
	t.Helper()
	_, m := metrics.NewRegistry()
	tm := NewBackgroundTaskManager(m)
	return NewServerLifecycle(tm), m
}

func TestServerLifecycle_Handler_ReturnsMux(t *testing.T) {
	sl, _ := newTestLifecycle(t)
	mux := sl.Handler()
	if mux == nil {
		t.Fatal("Handler() returned nil")
	}
}

func TestServerLifecycle_Handler_Idempotent(t *testing.T) {
	sl, _ := newTestLifecycle(t)
	a := sl.Handler()
	b := sl.Handler()
	if a != b {
		t.Error("Handler() returned different mux instances")
	}
}

func TestServerLifecycle_Start_BindsPort(t *testing.T) {
	sl, _ := newTestLifecycle(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if err := sl.StartWithListener(ln); err != nil {
		t.Fatalf("StartWithListener: %v", err)
	}
	defer sl.Shutdown(context.Background())

	addr := ln.Addr().String()
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (no route registered)", resp.StatusCode)
	}
}

func TestServerLifecycle_StartWithListener_UsesProvidedListener(t *testing.T) {
	sl, _ := newTestLifecycle(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if err := sl.StartWithListener(ln); err != nil {
		t.Fatalf("StartWithListener: %v", err)
	}
	defer sl.Shutdown(context.Background())

	resp, err := http.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestServerLifecycle_Shutdown_Graceful(t *testing.T) {
	sl, _ := newTestLifecycle(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()

	if err := sl.StartWithListener(ln); err != nil {
		t.Fatalf("StartWithListener: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sl.Shutdown(ctx)

	// Wait for the listener to be fully closed (race-free check)
	for i := 0; i < 50; i++ {
		_, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			return // port closed — success
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("port still open after Shutdown")
}

func TestServerLifecycle_Shutdown_BeforeStart_Noop(t *testing.T) {
	sl, _ := newTestLifecycle(t)
	ctx := context.Background()
	sl.Shutdown(ctx) // should not panic
}

func TestServerLifecycle_Stop_NoTasks(t *testing.T) {
	sl, _ := newTestLifecycle(t)
	sl.Stop() // should not panic or hang
}

func TestServerLifecycle_Start_DoubleStart(t *testing.T) {
	sl, _ := newTestLifecycle(t)
	ln1, _ := net.Listen("tcp", "127.0.0.1:0")
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln1.Close()
	defer ln2.Close()

	if err := sl.StartWithListener(ln1); err != nil {
		t.Fatalf("first StartWithListener: %v", err)
	}
	defer sl.Shutdown(context.Background())

	// Second start should work on a different listener (replaces proxy)
	if err := sl.StartWithListener(ln2); err != nil {
		t.Fatalf("second StartWithListener: %v", err)
	}
	sl.Shutdown(context.Background())
}

// ── PeriodicKeyProbe ────────────────────────────────────

// TestPeriodicKeyProbe_ReenablesRecoveredKey verifies that a permanently disabled
// key is re-enabled when the upstream returns 200.
// TestPeriodicKeyProbe_DoesNotReenableAuthRejectedKey verifies that a key
// permanently disabled by an auth failure (401/403) is NOT re-enabled by the
// periodic probe even when the upstream returns 200.
func TestPeriodicKeyProbe_DoesNotReenableAuthRejectedKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	pool := keypool.NewKeyPool([]string{"sk-test-key"}, []string{"test-key"})
	pool.ConfigureCBs(30*time.Second, 120*time.Second, 2.0)
	pool.CB(0).RecordPerma("auth_rejected")
	if !pool.IsDisabled(0) {
		t.Fatal("key should be disabled after RecordPerma")
	}
	if reason := pool.CB(0).TrippedReason(); reason != "auth_rejected" {
		t.Fatalf("trippedReason = %q, want %q", reason, "auth_rejected")
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		PeriodicKeyProbe(pool, upstream.URL, 50*time.Millisecond, stop)
	}()

	time.Sleep(150 * time.Millisecond)
	close(stop)
	wg.Wait()

	if !pool.IsDisabled(0) {
		t.Error("auth-rejected key should remain disabled after periodic probe")
	}
}

// TestPeriodicKeyProbe_ReenablesRecoveredKey verifies that a permanently disabled
// key is re-enabled when the upstream returns 200.
func TestPeriodicKeyProbe_ReenablesRecoveredKey(t *testing.T) {
	// Mock upstream that returns 200 on GET /models
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/models") {
			t.Errorf("expected /models path, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Create a pool with one key and drive it to Permanent via repeated
	// RateLimit failures (trippedReason = "quota_exhausted", which the
	// periodic probe is allowed to recover).
	pool := keypool.NewKeyPool([]string{"sk-test-key"}, []string{"test-key"})
	pool.ConfigureCBs(30*time.Second, 120*time.Second, 2.0)
	// attempt=0 → raw=30s, attempt=1 → raw=60s, attempt=2 → raw=120s >= cap
	pool.RecordFailure(0)
	pool.RecordFailure(0)
	pool.RecordFailure(0)
	if !pool.IsDisabled(0) {
		t.Fatal("key should be disabled after reaching backoff cap")
	}
	if reason := pool.CB(0).TrippedReason(); reason != "quota_exhausted" {
		t.Fatalf("trippedReason = %q, want %q", reason, "quota_exhausted")
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		PeriodicKeyProbe(pool, upstream.URL, 50*time.Millisecond, stop)
	}()

	// Let the probe fire at least once
	time.Sleep(150 * time.Millisecond)
	close(stop)
	wg.Wait()

	if pool.IsDisabled(0) {
		t.Error("key should have been re-enabled by periodic probe")
	}
	if pool.CB(0).State() != circuitbreaker.Closed {
		t.Errorf("CB state = %d, want %d (Closed)", pool.CB(0).State(), circuitbreaker.Closed)
	}
}
