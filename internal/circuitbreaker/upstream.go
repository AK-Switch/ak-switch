// Package circuitbreaker implements circuit breaker patterns for upstream services and API keys.
package circuitbreaker

import (
	"sync"
	"time"
)

// UpstreamCircuitBreaker protects the upstream from request flooding when it is unhealthy.
// It tracks consecutive failures and opens the circuit when a threshold is reached.
// After a reset timeout, it allows a single probe request to test recovery.
type UpstreamCircuitBreaker struct {
	mu             sync.Mutex
	state          State
	failureCount   int
	threshold      int
	resetTimeout   time.Duration
	openedAt       time.Time
	halfOpenProbed bool
}

// NewUpstreamCircuitBreaker creates a new UpstreamCircuitBreaker with the given failure threshold
// and reset timeout. Initial state is CLOSED.
func NewUpstreamCircuitBreaker(threshold int, resetTimeout time.Duration) *UpstreamCircuitBreaker {
	return &UpstreamCircuitBreaker{
		state:        Closed,
		threshold:    threshold,
		resetTimeout: resetTimeout,
	}
}

// RecordFailure records an upstream failure (e.g., 502/503).
//   - In CLOSED state, it increments the consecutive failure counter. If the counter reaches the
//     threshold, the circuit transitions to OPEN and the openedAt timestamp is recorded.
//   - In HALF_OPEN state (failed probe), the circuit returns to OPEN with failureCount reset to 1.
func (u *UpstreamCircuitBreaker) RecordFailure() time.Duration {
	u.mu.Lock()
	defer u.mu.Unlock()

	switch u.state {
	case Closed:
		u.failureCount++
		if u.failureCount >= u.threshold {
			u.state = Open
			u.openedAt = time.Now()
		}
	case HalfOpen:
		u.failureCount = 1
		u.state = Open
		u.openedAt = time.Now()
	}
	// In Open state, RecordFailure is a no-op.
	return 0
}

// RecordSuccess records an upstream success.
// It resets the consecutive failure counter and returns the circuit to CLOSED.
func (u *UpstreamCircuitBreaker) RecordSuccess() {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.failureCount = 0
	u.state = Closed
}

// Allow checks whether a request should be allowed to pass through.
//   - CLOSED: always returns true.
//   - OPEN: returns true if the reset timeout has elapsed (transitions to HALF_OPEN and marks the
//     probe as used); otherwise returns false.
//   - HALF_OPEN: returns true for the first probe call, false for subsequent calls until the
//     state transitions again.
func (u *UpstreamCircuitBreaker) Allow() bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	switch u.state {
	case Closed:
		return true
	case Open:
		if time.Now().After(u.openedAt.Add(u.resetTimeout)) {
			u.state = HalfOpen
			u.halfOpenProbed = true
			return true
		}
		return false
	case HalfOpen:
		if !u.halfOpenProbed {
			u.halfOpenProbed = true
			return true
		}
		return false
	default:
		return false
	}
}

// State returns the current circuit breaker state.
func (u *UpstreamCircuitBreaker) State() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

// FailureCount returns the current consecutive failure count.
func (u *UpstreamCircuitBreaker) FailureCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.failureCount
}

// SetThreshold updates the failure threshold at runtime.
// The new threshold takes effect immediately; the current failure count is preserved.
func (u *UpstreamCircuitBreaker) SetThreshold(t int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.threshold = t
}

// SetResetTimeout updates the reset timeout at runtime.
// The new timeout takes effect immediately; the next open→half-open transition uses it.
func (u *UpstreamCircuitBreaker) SetResetTimeout(d time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.resetTimeout = d
}

// Reset force-closes the circuit breaker, clearing all failure state.
// Used by the admin API to manually recover from a tripped upstream CB.
func (u *UpstreamCircuitBreaker) Reset() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.failureCount = 0
	u.state = Closed
	u.halfOpenProbed = false
}

// AuthFailCount returns 0 — upstream CB does not track auth failures.
// This method satisfies the CircuitBreaker interface for callers that
// aggregate auth-failure counts across key and upstream CBs.
func (u *UpstreamCircuitBreaker) AuthFailCount() int { return 0 }
