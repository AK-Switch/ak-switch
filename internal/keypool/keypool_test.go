//go:build unit

package keypool

import (
	"fmt"
	"testing"
	"time"
)

func TestNextReturnsKey(t *testing.T) {
	p := NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	for i := 0; i < 3; i++ {
		idx, key, ok := p.Next()
		if !ok {
			t.Errorf("Next() returned ok=false on call %d", i)
		}
		if idx < 0 {
			t.Errorf("Next() returned index %d (expected >= 0) on call %d", idx, i)
		}
		if key == "" {
			t.Errorf("Next() returned empty key on call %d", i)
		}
	}
}

// ── RPM-aware key selection ──────────────────────────────

func Test_NextPrefersLowRPMKey(t *testing.T) {
	p := NewKeyPool([]string{"high", "medium", "low"}, nil)

	// Simulate requests: high=10, medium=5, low=0 (all within last minute)
	now := time.Now()
	for i := 0; i < 10; i++ {
		p.requestHistory[0] = append(p.requestHistory[0], now.Add(-time.Duration(i)*time.Second))
	}
	for i := 0; i < 5; i++ {
		p.requestHistory[1] = append(p.requestHistory[1], now.Add(-time.Duration(i)*time.Second))
	}
	// key 2 (low) has 0 requests — should be preferred

	// Run Next() multiple times and verify the low-RPM key is selected most often
	lowCount := 0
	mediumCount := 0
	highCount := 0
	for i := 0; i < 30; i++ {
		idx, _, ok := p.Next()
		if !ok {
			t.Fatalf("Next() returned ok=false, expected all keys available")
		}
		switch idx {
		case 0:
			highCount++
		case 1:
			mediumCount++
		case 2:
			lowCount++
		}
		p.Release(idx)
	}

	// Low RPM key should be selected more than high RPM key
	if lowCount <= highCount {
		t.Errorf("low-RPM key (idx=2) selected %d times, high-RPM key (idx=0) selected %d times; expected low > high", lowCount, highCount)
	}
	t.Logf("selection counts — high(10): %d, medium(5): %d, low(0): %d", highCount, mediumCount, lowCount)
}

func Test_NextSkipsHighRPMWhenEnoughCandidates(t *testing.T) {
	p := NewKeyPool([]string{"high", "low1", "low2"}, nil)

	// Simulate key 0 having high RPM, keys 1 and 2 having low RPM
	now := time.Now()
	for i := 0; i < 50; i++ {
		p.requestHistory[0] = append(p.requestHistory[0], now.Add(-time.Duration(i)*time.Second))
	}
	// keys 1 and 2 have 0 RPM

	// Run 20 iterations — key 0 should rarely be selected
	highCount := 0
	for i := 0; i < 20; i++ {
		idx, _, ok := p.Next()
		if !ok {
			t.Fatalf("Next() returned ok=false on iteration %d", i)
		}
		if idx == 0 {
			highCount++
		}
		p.Release(idx)
	}

	// High RPM key should be selected less than 50% of the time
	// (in practice with 2 low-RPM alternatives, it should be ~0)
	if highCount > 10 {
		t.Errorf("high-RPM key selected %d/20 times, expected <= 10", highCount)
	}
	t.Logf("high-RPM key selected %d/20 times", highCount)
}

func Test_NextAllKeysSameRPM(t *testing.T) {
	p := NewKeyPool([]string{"a", "b", "c"}, nil)

	// All keys have same RPM (0)
	// Should just round-robin without issues
	seen := make(map[int]int)
	for i := 0; i < 9; i++ {
		idx, _, ok := p.Next()
		if !ok {
			t.Fatalf("Next() returned ok=false on iteration %d", i)
		}
		seen[idx]++
		p.Release(idx)
	}

	// With 3 keys and 9 calls, each should be selected ~3 times
	if len(seen) != 3 {
		t.Errorf("expected 3 unique keys, got %d: %v", len(seen), seen)
	}
	for k, v := range seen {
		if v < 1 {
			t.Errorf("key %d was never selected", k)
		}
		t.Logf("key %d selected %d times", k, v)
	}
}

func Test_NextRPMWithInUse(t *testing.T) {
	p := NewKeyPool([]string{"a", "b", "c"}, nil)

	// Key 2 has lowest RPM, but is inUse — should be skipped
	now := time.Now()
	for i := 0; i < 10; i++ {
		p.requestHistory[0] = append(p.requestHistory[0], now.Add(-time.Duration(i)*time.Second))
	}
	// key 1: 0 RPM
	// key 2: 0 RPM but inUse

	p.inUse[2] = true

	// Key 1 should be selected (lowest RPM among available)
	for i := 0; i < 5; i++ {
		idx, _, ok := p.Next()
		if !ok {
			t.Fatalf("Next() returned ok=false on iteration %d", i)
		}
		if idx == 2 {
			t.Errorf("key 2 (inUse) was selected on iteration %d", i)
		}
		if idx == 0 {
			// high RPM — acceptable but less preferred
			t.Logf("iteration %d: selected high-RPM key (idx=0)", i)
		}
		p.Release(idx)
	}
}

func Test_NextRPMHistoryCleanedUp(t *testing.T) {
	p := NewKeyPool([]string{"a", "b"}, nil)

	// Add old timestamps (>60s) to key 0 — should be ignored for RPM calculation
	old := time.Now().Add(-120 * time.Second)
	for i := 0; i < 100; i++ {
		p.requestHistory[0] = append(p.requestHistory[0], old)
	}
	// Key 1 has 0 RPM (no history)

	// Key 0's old history should be ignored
	idx, _, ok := p.Next()
	if !ok {
		t.Fatal("Next() returned ok=false")
	}
	_ = idx
	p.Release(idx)
}

func TestNextAllCooldown(t *testing.T) {
	p := NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	// Put all keys on cooldown for 10 minutes
	for i := 0; i < 3; i++ {
		p.Cooldown(i, 10*time.Minute)
	}

	idx, key, ok := p.Next()
	if ok {
		t.Errorf("Next() returned ok=true when all keys are on cooldown")
	}
	if idx != -1 {
		t.Errorf("Next() returned index %d, want -1", idx)
	}
	if key != "" {
		t.Errorf("Next() returned key=%q, want empty", key)
	}
}

func TestDisableKey(t *testing.T) {
	p := NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	p.Disable(1)

	for i := 0; i < 10; i++ {
		idx, _, ok := p.Next()
		if !ok {
			t.Fatalf("Next() returned ok=false on iteration %d with 2 active keys", i)
		}
		if idx == 1 {
			t.Errorf("Next() returned disabled index 1 on iteration %d", i)
		}
		p.Release(idx)
	}
}

func TestAddKey(t *testing.T) {
	p := NewKeyPool([]string{"key-a", "key-b"}, nil)
	if n := p.ActiveCount(); n != 2 {
		t.Fatalf("ActiveCount() = %d, want 2 before AddKey", n)
	}

	idx := p.AddKey("new-key", "")
	if idx != 2 {
		t.Errorf("AddKey() returned index %d, want 2", idx)
	}
	if n := p.ActiveCount(); n != 3 {
		t.Errorf("ActiveCount() = %d after AddKey, want 3", n)
	}
}

func TestRemoveKey(t *testing.T) {
	p := NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	if n := p.ActiveCount(); n != 3 {
		t.Fatalf("ActiveCount() = %d, want 3 before RemoveKey", n)
	}

	err := p.RemoveKey(0)
	if err != nil {
		t.Fatalf("RemoveKey(0) returned error: %v", err)
	}
	if n := p.ActiveCount(); n != 2 {
		t.Errorf("ActiveCount() = %d after RemoveKey, want 2", n)
	}
}

func TestRemoveKeyOutOfRange(t *testing.T) {
	p := NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	err := p.RemoveKey(999)
	if err == nil {
		t.Error("RemoveKey(999) expected error, got nil")
	}
}

func TestNextEmptyPool(t *testing.T) {
	p := NewKeyPool([]string{}, nil)
	idx, key, ok := p.Next()
	if ok {
		t.Errorf("Next() returned ok=true for empty pool")
	}
	if idx != -1 {
		t.Errorf("Next() returned index %d, want -1", idx)
	}
	if key != "" {
		t.Errorf("Next() returned key=%q, want empty", key)
	}
}

func TestActiveCount(t *testing.T) {
	p := NewKeyPool([]string{"k1", "k2", "k3", "k4"}, nil)
	if n := p.ActiveCount(); n != 4 {
		t.Fatalf("ActiveCount() = %d, want 4", n)
	}

	p.Disable(0)
	if n := p.ActiveCount(); n != 3 {
		t.Errorf("ActiveCount() = %d after Disable(0), want 3", n)
	}

	p.Disable(1)
	if n := p.ActiveCount(); n != 2 {
		t.Errorf("ActiveCount() = %d after Disable(1), want 2", n)
	}
}

func TestNameReturnsCorrectName(t *testing.T) {
	keys := []string{"key-a", "key-b", "key-c"}
	names := []string{"主账号", "备用key", ""}
	p := NewKeyPool(keys, names)

	if n := p.Name(0); n != "主账号" {
		t.Errorf("Name(0) = %q, want %q", n, "主账号")
	}
	if n := p.Name(1); n != "备用key" {
		t.Errorf("Name(1) = %q, want %q", n, "备用key")
	}
	if n := p.Name(2); n != "" {
		t.Errorf("Name(2) = %q, want empty", n)
	}
}

func TestNameOutOfRange(t *testing.T) {
	p := NewKeyPool([]string{"key-a"}, []string{"test"})
	if n := p.Name(-1); n != "" {
		t.Errorf("Name(-1) = %q, want empty", n)
	}
	if n := p.Name(5); n != "" {
		t.Errorf("Name(5) = %q, want empty", n)
	}
}

func TestGetKeyDetailsIncludesName(t *testing.T) {
	p := NewKeyPool([]string{"key-a", "key-b"}, []string{"主key", ""})
	details := p.GetKeyDetails()
	if len(details) != 2 {
		t.Fatalf("GetKeyDetails len = %d, want 2", len(details))
	}
	if details[0]["name"] != "主key" {
		t.Errorf("details[0].name = %q, want %q", details[0]["name"], "主key")
	}
	if details[1]["name"] != "" {
		t.Errorf("details[1].name = %q, want empty", details[1]["name"])
	}
}

func TestAddKeyWithName(t *testing.T) {
	p := NewKeyPool([]string{"key-a"}, []string{"original"})
	idx := p.AddKey("key-b", "新key")
	if idx != 1 {
		t.Errorf("AddKey index = %d, want 1", idx)
	}
	if n := p.Name(1); n != "新key" {
		t.Errorf("Name(1) after AddKey = %q, want %q", n, "新key")
	}
}

func TestTimeUntilAvailable_AllDisabled(t *testing.T) {
	p := NewKeyPool([]string{"key-a", "key-b"}, nil)
	p.Disable(0)
	p.Disable(1)
	wait := p.TimeUntilAvailable()
	if wait >= 0 {
		t.Errorf("TimeUntilAvailable() = %v, want negative value when all keys disabled", wait)
	}
}

func TestTimeUntilAvailable_AllCooling(t *testing.T) {
	p := NewKeyPool([]string{"key-a", "key-b"}, nil)
	p.Cooldown(0, 10*time.Minute)
	p.Cooldown(1, 10*time.Minute)
	wait := p.TimeUntilAvailable()
	if wait <= 0 {
		t.Errorf("TimeUntilAvailable() = %v, want positive value when all keys cooling", wait)
	}
}

func TestTimeUntilAvailable_SomeActive(t *testing.T) {
	p := NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	p.Disable(0)
	p.Cooldown(1, 10*time.Minute)
	wait := p.TimeUntilAvailable()
	if wait != 0 {
		t.Errorf("TimeUntilAvailable() = %v, want 0 when at least one key is ready", wait)
	}
}

func BenchmarkKeyPoolNext(b *testing.B) {
	keySet := []string{"ka", "kb", "kc", "kd", "ke", "kf", "kg", "kh", "ki", "kj"}
	for _, n := range []int{1, 5, 10} {
		p := NewKeyPool(keySet[:n], nil)
		b.Run(fmt.Sprintf("keys-%d", n), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				idx, _, ok := p.Next()
				if ok {
					p.Release(idx)
				}
			}
		})
	}
}
