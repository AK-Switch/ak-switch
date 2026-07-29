//go:build unit

package circuitbreaker

import (
	"testing"
	"time"
)

// TestUpstreamBreakerNew verifies initial state: CLOSED, Allow=true, FailureCount=0.
func TestUpstreamBreakerNew(t *testing.T) {
	u := NewUpstreamCircuitBreaker(3, time.Minute)
	if s := u.State(); s != Closed {
		t.Errorf("initial state = %d, want %d (Closed)", s, Closed)
	}
	if !u.Allow() {
		t.Error("Allow() = false, want true (initial closed state)")
	}
	if n := u.FailureCount(); n != 0 {
		t.Errorf("initial FailureCount = %d, want 0", n)
	}
}

// TestUpstreamNotTrippedBelowThreshold verifies that failures below the threshold
// keep the breaker in CLOSED state with Allow returning true.
func TestUpstreamNotTrippedBelowThreshold(t *testing.T) {
	u := NewUpstreamCircuitBreaker(3, time.Minute)
	for i := 1; i <= 2; i++ {
		u.RecordFailure()
		if n := u.FailureCount(); n != i {
			t.Errorf("FailureCount after %d failures = %d, want %d", i, n, i)
		}
		if s := u.State(); s != Closed {
			t.Errorf("state after %d failures = %d, want %d (Closed)", i, s, Closed)
		}
		if !u.Allow() {
			t.Errorf("Allow() = false after %d failures, want true", i)
		}
	}
}

// TestUpstreamTripsAtThreshold verifies that when failure count reaches the threshold,
// the breaker transitions to OPEN and Allow returns false.
func TestUpstreamTripsAtThreshold(t *testing.T) {
	u := NewUpstreamCircuitBreaker(3, time.Minute)
	for i := 0; i < 3; i++ {
		u.RecordFailure()
	}
	if s := u.State(); s != Open {
		t.Errorf("state after threshold = %d, want %d (Open)", s, Open)
	}
	if u.Allow() {
		t.Error("Allow() = true when Open, want false")
	}
}

// TestOpenRejectsBeforeTimeout verifies that in OPEN state, Allow returns false
// before the reset timeout elapses.
func TestOpenRejectsBeforeTimeout(t *testing.T) {
	u := NewUpstreamCircuitBreaker(2, time.Hour) // long timeout
	u.RecordFailure()
	u.RecordFailure()
	if s := u.State(); s != Open {
		t.Fatalf("state = %d, want %d (Open)", s, Open)
	}
	if u.Allow() {
		t.Error("Allow() = true before timeout, want false")
	}
}

// TestHalfOpenAfterTimeout verifies that in OPEN state, once the reset timeout
// elapses, Allow returns true and transitions the state to HALF_OPEN.
func TestHalfOpenAfterTimeout(t *testing.T) {
	u := NewUpstreamCircuitBreaker(2, 50*time.Millisecond)
	u.RecordFailure()
	u.RecordFailure()
	if s := u.State(); s != Open {
		t.Fatalf("state = %d, want %d after tripping", s, Open)
	}
	// Should reject before timeout
	if u.Allow() {
		t.Error("Allow() = true before timeout, want false")
	}
	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)
	if !u.Allow() {
		t.Fatal("Allow() = false after timeout, want true (HalfOpen)")
	}
	if s := u.State(); s != HalfOpen {
		t.Errorf("state after timeout = %d, want %d (HalfOpen)", s, HalfOpen)
	}
}

// TestHalfOpenSuccessRecovers verifies that a successful probe request in
// HALF_OPEN state returns the breaker to CLOSED with failure count reset to 0.
func TestHalfOpenSuccessRecovers(t *testing.T) {
	u := NewUpstreamCircuitBreaker(2, 50*time.Millisecond)
	u.RecordFailure()
	u.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	// Transition to HALF_OPEN
	if !u.Allow() {
		t.Fatal("Allow() = false after timeout, want true")
	}
	// Probe success
	u.RecordSuccess()
	if s := u.State(); s != Closed {
		t.Errorf("state after probe success = %d, want %d (Closed)", s, Closed)
	}
	if n := u.FailureCount(); n != 0 {
		t.Errorf("FailureCount after probe success = %d, want 0", n)
	}
	if !u.Allow() {
		t.Error("Allow() = false after recovery, want true")
	}
}

// TestHalfOpenFailureReopens verifies that a failed probe request in HALF_OPEN
// state returns the breaker to OPEN with failure count set to 1.
func TestHalfOpenFailureReopens(t *testing.T) {
	u := NewUpstreamCircuitBreaker(2, 50*time.Millisecond)
	u.RecordFailure()
	u.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	// Transition to HALF_OPEN
	if !u.Allow() {
		t.Fatal("Allow() = false after timeout, want true")
	}
	// Probe fails
	u.RecordFailure()
	if s := u.State(); s != Open {
		t.Errorf("state after probe failure = %d, want %d (Open)", s, Open)
	}
	if n := u.FailureCount(); n != 1 {
		t.Errorf("FailureCount after probe failure = %d, want 1", n)
	}
	// Should be rejected again
	if u.Allow() {
		t.Error("Allow() = true after probe failure, want false")
	}
}

// TestHalfOpenSingleProbe verifies that only the first Allow() call in HALF_OPEN
// state returns true; subsequent calls return false until the state transitions.
func TestHalfOpenSingleProbe(t *testing.T) {
	u := NewUpstreamCircuitBreaker(2, 50*time.Millisecond)
	u.RecordFailure()
	u.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	// First Allow() transitions to HALF_OPEN and returns true
	if !u.Allow() {
		t.Fatal("Allow() = false on first probe, want true")
	}
	// Second Allow() while HALF_OPEN should be false (probe already used)
	if u.Allow() {
		t.Error("Allow() = true on second HALF_OPEN call, want false")
	}
}

// TestUpstreamSuccessResetsCounter verifies that a single success in CLOSED state resets
// the consecutive failure counter to zero.
func TestUpstreamSuccessResetsCounter(t *testing.T) {
	u := NewUpstreamCircuitBreaker(3, time.Minute)
	// Build up to 2 failures
	u.RecordFailure()
	u.RecordFailure()
	if n := u.FailureCount(); n != 2 {
		t.Fatalf("FailureCount = %d, want 2 before success", n)
	}
	// Success resets counter
	u.RecordSuccess()
	if n := u.FailureCount(); n != 0 {
		t.Fatalf("FailureCount after success = %d, want 0", n)
	}
	// Need 3 more failures to trip
	u.RecordFailure()
	u.RecordFailure()
	u.RecordFailure()
	if s := u.State(); s != Open {
		t.Errorf("state after 3 failures post-reset = %d, want %d (Open)", s, Open)
	}
}

// TestUpstreamCBReset verifies that Reset() force-closes an Open circuit breaker,
// clearing all failure state so the upstream becomes immediately usable.
func TestUpstreamCBReset(t *testing.T) {
	u := NewUpstreamCircuitBreaker(3, time.Minute)
	u.RecordFailure()
	u.RecordFailure()
	u.RecordFailure()
	if s := u.State(); s != Open {
		t.Fatalf("state = %d, want Open before reset", s)
	}

	u.Reset()

	if s := u.State(); s != Closed {
		t.Errorf("state after Reset() = %d, want Closed", s)
	}
	if n := u.FailureCount(); n != 0 {
		t.Errorf("FailureCount after Reset() = %d, want 0", n)
	}
	if !u.Allow() {
		t.Error("Allow() = false after Reset(), want true")
	}
}
