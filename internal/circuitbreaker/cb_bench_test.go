//go:build unit

package circuitbreaker

import (
	"testing"
	"time"
)

func BenchmarkKeyCircuitBreaker_Allow(b *testing.B) {
	cb := NewKeyCircuitBreaker(30*time.Second, 120*time.Second, 2, 3)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Allow()
	}
}

func BenchmarkUpstreamCircuitBreaker_Allow(b *testing.B) {
	cb := NewUpstreamCircuitBreaker(5, 30*time.Second)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Allow()
	}
}
