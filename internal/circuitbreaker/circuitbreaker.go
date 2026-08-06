// Package circuitbreaker implements circuit breaker patterns for upstream services and API keys.
package circuitbreaker

// State represents the common state of a circuit breaker.
type State int

const (
	Closed    State = iota // Normal operation, allow requests.
	Open                   // Tripped, fail fast or cool down.
	HalfOpen               // Probing, allow one probe request (upstream-specific).
	Permanent              // Permanently disabled (key-specific).
)

