//go:build unit

package logentry

import "testing"

func BenchmarkMaskKey(b *testing.B) {
	keys := []string{
		"sk-1234567890abcdef",       // 20 chars → partial mask
		"short",                      // 5 chars → fully masked
		"123456789012",               // 12 chars → boundary (fully masked)
		"1234567890123",              // 13 chars → boundary (partial)
		"very-long-api-key-xxxxxxxx", // long key
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, k := range keys {
			MaskKey(k)
		}
	}
}
