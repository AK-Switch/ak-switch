package keypool

import (
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"akswitch/internal/circuitbreaker"
	"akswitch/internal/utils"
)

// KeyPool is a thread-safe, round-robin key pool with cooldown, disable, and request-tracking support.
type KeyPool struct {
	counter        uint64
	keys           []string
	names          []string
	cbs            []*circuitbreaker.KeyCircuitBreaker
	requestHistory [][]time.Time // timestamps of requests in the last 60s per key
	lastUsed       []time.Time
	inUse          []bool // tracks keys currently reserved by a goroutine (prevents concurrent selection)
	mu             sync.RWMutex
}

// NewKeyPool creates a KeyPool from slices of API keys and optional names.
// names may be nil or shorter than keys — unnamed keys get empty string.
func NewKeyPool(keys []string, names []string) *KeyPool {
	n := make([]string, len(keys))
	for i := range keys {
		if i < len(names) {
			n[i] = names[i]
		}
	}
	cbs := make([]*circuitbreaker.KeyCircuitBreaker, len(keys))
	for i := range keys {
		cbs[i] = circuitbreaker.NewKeyCircuitBreaker(0, 0, 0)
	}
	return &KeyPool{
		counter:        rand.Uint64(),
		keys:           keys,
		names:          n,
		cbs:            cbs,
		requestHistory: make([][]time.Time, len(keys)),
		lastUsed:       make([]time.Time, len(keys)),
		inUse:          make([]bool, len(keys)),
	}
}

// Keys returns a copy of all keys in the pool.
func (p *KeyPool) Keys() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]string, len(p.keys))
	copy(result, p.keys)
	return result
}

// Len returns the number of keys in the pool.
func (p *KeyPool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.keys)
}

// Name returns the name of a key by index, or empty string if index is out of range.
func (p *KeyPool) Name(idx int) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if idx < 0 || idx >= len(p.names) {
		return ""
	}
	return p.names[idx]
}

// TimeUntilAvailable returns the shortest duration until any key becomes available,
// or -1 if all keys are disabled. Returns 0 if at least one key is ready.
func (p *KeyPool) TimeUntilAvailable() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var soonest time.Duration = -1
	for _, cb := range p.cbs {
		if cb.State() == circuitbreaker.StatePermanent {
			continue
		}
		if cb.Allow() {
			return 0
		}
		if rem := cb.CooldownRemaining(); rem > 0 {
			if soonest < 0 || rem < soonest {
				soonest = rem
			}
		}
	}
	return soonest
}

// Next returns the next available and unreserved key in round-robin order.
// Returns index, key, and ok=false if none available.
// The selected key is marked as in-use; caller must call Release(idx) when done.
func (p *KeyPool) Next() (int, string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.keys)
	if n == 0 {
		return -1, "", false
	}
	start := int((atomic.AddUint64(&p.counter, 1) - 1) % uint64(n))
	for i := 0; i < n; i++ {
		idx := (start + i) % n
		if p.cbs[idx].Allow() && !p.inUse[idx] {
			p.inUse[idx] = true
			return idx, p.keys[idx], true
		}
	}
	return -1, "", false
}

// RequestsInLastMinute returns the number of requests made by a key in the last 60 seconds.
// Caller must hold at least RLock.
func (p *KeyPool) RequestsInLastMinute(idx int) int {
	cutoff := time.Now().Add(-60 * time.Second)
	count := 0
	for _, t := range p.requestHistory[idx] {
		if t.After(cutoff) {
			count++
		}
	}
	return count
}

// CleanupOldRequests removes request timestamps older than 60 seconds.
func (p *KeyPool) CleanupOldRequests(idx int) {
	cutoff := time.Now().Add(-60 * time.Second)
	var filtered []time.Time
	for _, t := range p.requestHistory[idx] {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	p.requestHistory[idx] = filtered
}

// Cooldown sets a cooldown on a key for the given duration.
// Returns an error if the index is out of range.
func (p *KeyPool) Cooldown(idx int, d time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx < 0 || idx >= len(p.keys) {
		return fmt.Errorf("key index %d out of range (0-%d)", idx, len(p.keys)-1)
	}
	p.cbs[idx].ForceCooldown(d)
	name := ""
	if idx >= 0 && idx < len(p.names) {
		name = p.names[idx]
	}
	if name != "" {
		slog.Info("key on cooldown", "key_index", idx, "key_name", name, "duration", d)
	} else {
		slog.Info("key on cooldown", "key_index", idx, "duration", d)
	}
	return nil
}

// Disable marks a key as permanently disabled.
// Returns an error if the index is out of range.
func (p *KeyPool) Disable(idx int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx < 0 || idx >= len(p.keys) {
		return fmt.Errorf("key index %d out of range (0-%d)", idx, len(p.keys)-1)
	}
	p.cbs[idx].RecordPerma("manual")
	name := ""
	if idx >= 0 && idx < len(p.names) {
		name = p.names[idx]
	}
	if name != "" {
		slog.Info("key disabled", "key_index", idx, "key_name", name)
	}
	return nil
}

// Enable re-enables a previously disabled key by clearing both the disabled
// flag and any active cooldown. It is safe to call on an already-enabled key.
// Returns an error if the index is out of range.
func (p *KeyPool) Enable(idx int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx < 0 || idx >= len(p.keys) {
		return fmt.Errorf("key index %d out of range (0-%d)", idx, len(p.keys)-1)
	}
					p.cbs[idx].Reset()
		
	name := ""
	if idx >= 0 && idx < len(p.names) {
		name = p.names[idx]
	}
	if name != "" {
		slog.Info("key enabled", "key_index", idx, "key_name", name)
	}
	return nil
}

// IsDisabled returns whether a key is disabled by index.
// Returns false if the index is out of range.
func (p *KeyPool) IsDisabled(idx int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if idx < 0 || idx >= len(p.cbs) {
		return false
	}
	return p.cbs[idx].State() == circuitbreaker.StatePermanent
}
// ConfigureCBs replaces all per-key circuit breakers with new ones configured
// with the given base cooldown, backoff cap, and multiplier.
// Called by NewProxyEngine to synchronize breaker parameters with config.
func (p *KeyPool) ConfigureCBs(base, backoffCap time.Duration, multiplier float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.cbs {
		wasPermanent := p.cbs[i].State() == circuitbreaker.StatePermanent
		p.cbs[i] = circuitbreaker.NewKeyCircuitBreaker(base, backoffCap, multiplier)
		if wasPermanent {
			p.cbs[i].RecordPerma("preserved")
		}
	}
}

// CB returns the KeyCircuitBreaker for a given key index.
// The returned breaker has its own mutex and is safe for concurrent use.
func (p *KeyPool) CB(idx int) *circuitbreaker.KeyCircuitBreaker {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if idx < 0 || idx >= len(p.cbs) {
		return nil
	}
	return p.cbs[idx]
}

// RecordFailure records a rate-limit (429) failure on a key and returns the cooldown duration.
func (p *KeyPool) RecordFailure(idx int) time.Duration {
	cb := p.CB(idx)
	if cb == nil {
		return 0
	}
	return cb.RecordFailure()
}

// RecordAuthFailure records an auth failure (401/403) and returns true if key should be permanently disabled.
func (p *KeyPool) RecordAuthFailure(idx int) bool {
	cb := p.CB(idx)
	if cb == nil {
		return false
	}
	return cb.RecordAuthFailure()
}

// RecordSuccess records a successful proxy response and resets the key's breaker state.
func (p *KeyPool) RecordSuccess(idx int) {
	cb := p.CB(idx)
	if cb == nil {
		return
	}
	cb.RecordSuccess()
}

// Release marks a key as no longer in use, allowing other goroutines to select it.
// Call this after the upstream request using this key completes (success or failure).
// Safe to call with an out-of-range index (no-op).
func (p *KeyPool) Release(idx int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx >= 0 && idx < len(p.inUse) {
		p.inUse[idx] = false
	}
}


// ActiveCount returns the number of non-disabled keys.
func (p *KeyPool) ActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := 0
	for i := range p.keys {
		if p.cbs[i].State() != circuitbreaker.StatePermanent {
			n++
		}
	}
	return n
}

// DisabledCount returns the number of disabled keys.
func (p *KeyPool) DisabledCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := 0
	for _, cb := range p.cbs {
		if cb.State() == circuitbreaker.StatePermanent {
			n++
		}
	}
	return n
}

// CoolingCount returns the number of keys currently in cooldown (not disabled, but cooldown not yet expired).
func (p *KeyPool) CoolingCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := 0
	for i := range p.keys {
		cb := p.cbs[i]
		if cb.State() == circuitbreaker.StateOpen && !cb.Allow() {
			n++
		}
	}
	return n
}

// KeyStatusLabel returns a status string for a key (disabled, ready, or cooling).
// Caller must hold at least RLock.
func (p *KeyPool) KeyStatusLabel(i int, now time.Time) string {
	cb := p.cbs[i]
	switch cb.State() {
	case circuitbreaker.StatePermanent:
		return "disabled"
	case circuitbreaker.StateClosed:
		return "ready"
	case circuitbreaker.StateOpen:
		if cb.Allow() {
			return "ready"
		}
		return fmt.Sprintf("cooling(%.0fs)", cb.CooldownRemaining().Seconds())
	default:
		return "unknown"
	}
}

// Status returns a human-readable status string for all keys.
func (p *KeyPool) Status() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := time.Now()
	parts := make([]string, len(p.keys))
	for i := range p.keys {
		parts[i] = fmt.Sprintf("[%d]:%s", i, p.KeyStatusLabel(i, now))
	}
	return strings.Join(parts, " ")
}

// GetKeyDetails returns detailed status for each key in the pool.
func (p *KeyPool) GetKeyDetails() []map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := time.Now()
	details := make([]map[string]interface{}, len(p.keys))
	for i := range p.keys {
		p.CleanupOldRequests(i)
		name := ""
		if i < len(p.names) {
			name = p.names[i]
		}
		keyDetail := map[string]interface{}{
			"index":               i,
			"key":                 utils.MaskKey(p.keys[i]),
			"name":                name,
			"disabled":            p.cbs[i].State() == circuitbreaker.StatePermanent,
			"requests_per_minute": p.RequestsInLastMinute(i),
			"last_used":           p.lastUsed[i].Format(time.RFC3339),
			"cooldown_until":      p.cbs[i].CooldownRemaining().String(),
		}
		keyDetail["status"] = p.KeyStatusLabel(i, now)
		details[i] = keyDetail
	}
	return details
}

// IncrementRequestCount records a request timestamp for a key and updates its lastUsed timestamp.
func (p *KeyPool) IncrementRequestCount(idx int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.CleanupOldRequests(idx)
	p.requestHistory[idx] = append(p.requestHistory[idx], time.Now())
	p.lastUsed[idx] = time.Now()
}

// AddKey appends a new key to the pool and returns its index.
func (p *KeyPool) AddKey(key string, name string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = append(p.keys, key)
	p.names = append(p.names, name)
				p.cbs = append(p.cbs, circuitbreaker.NewKeyCircuitBreaker(0, 0, 0))
		
	p.requestHistory = append(p.requestHistory, []time.Time{})
	p.lastUsed = append(p.lastUsed, time.Time{})
	p.inUse = append(p.inUse, false)
	idx := len(p.keys) - 1
	if name != "" {
		slog.Info("key added to pool", "key_index", idx, "key_name", name)
	} else {
		slog.Info("key added to pool", "key_index", idx)
	}
	return idx
}

// RemoveKey removes a key from the pool by index.
func (p *KeyPool) RemoveKey(idx int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx < 0 || idx >= len(p.keys) {
		return fmt.Errorf("key index %d out of range (0-%d)", idx, len(p.keys)-1)
	}
	name := ""
	if idx < len(p.names) {
		name = p.names[idx]
	}
	p.keys = append(p.keys[:idx], p.keys[idx+1:]...)
	p.names = append(p.names[:idx], p.names[idx+1:]...)
				p.cbs = append(p.cbs[:idx], p.cbs[idx+1:]...)
		
	p.requestHistory = append(p.requestHistory[:idx], p.requestHistory[idx+1:]...)
	p.lastUsed = append(p.lastUsed[:idx], p.lastUsed[idx+1:]...)
	p.inUse = append(p.inUse[:idx], p.inUse[idx+1:]...)
	if name != "" {
		slog.Info("key removed from pool", "key_index", idx, "key_name", name)
	} else {
		slog.Info("key removed from pool", "key_index", idx)
	}
	return nil
}
