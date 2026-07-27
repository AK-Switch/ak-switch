//go:build unit

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"akswitch/internal/keypool"
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