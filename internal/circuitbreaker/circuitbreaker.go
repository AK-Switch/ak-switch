// Package circuitbreaker implements circuit breaker patterns for upstream services and API keys.
package circuitbreaker

import (
	"time"
)

// State represents the common state of a circuit breaker.
type State int

const (
	Closed    State = iota // Normal operation, allow requests.
	Open                   // Tripped, fail fast or cool down.
	HalfOpen               // Probing, allow one probe request (upstream-specific).
	Permanent              // Permanently disabled (key-specific).
)

// CircuitBreaker is the common interface for all circuit breaker implementations.
// Each implementation may have additional methods beyond this interface.
type CircuitBreaker interface {
	// Allow returns true if a request should be allowed through.
	Allow() bool

	// RecordFailure records a failure, transitions state accordingly,
	// and returns the cooldown duration (0 if not applicable).
	RecordFailure() time.Duration

	// RecordSuccess records a success and resets the breaker to closed state.
	RecordSuccess()

	// State returns the current circuit breaker state.
	State() State
}
