//go:build unit

package server

import (
	"errors"
	"strings"
	"testing"
)

// FuzzCategorizeError verifies categorizeError never panics on arbitrary
// status codes and error values, and always returns a valid ErrorCategory.
func FuzzCategorizeError(f *testing.F) {
	// Seed corpus: known status codes and errors
	// errStr="" represents nil error; non-empty represents an error message
	for _, code := range []int{0, 200, 400, 401, 403, 429, 500, 502, 503, 999} {
		f.Add(code, "")
	}
	f.Add(0, "context.Canceled")
	f.Add(0, "connection refused")
	f.Add(0, "net/http: timeout")

	f.Fuzz(func(t *testing.T, statusCode int, errStr string) {
		var err error
		if errStr != "" {
			err = errors.New(errStr)
		}
		cat := categorizeError(statusCode, err)
		if cat < CatUnknown || cat > CatClientAbort {
			t.Errorf("categorizeError(%d, %v) = %d: out of range", statusCode, err, cat)
		}
	})
}

// FuzzMaskSensitiveData verifies MaskSensitiveData never panics on arbitrary
// input and always returns a string within the maxLen constraint.
func FuzzMaskSensitiveData(f *testing.F) {
	f.Add("", 1024)
	f.Add("short", 1024)
	f.Add(strings.Repeat("x", 2000), 1024)

	f.Fuzz(func(t *testing.T, data string, maxLen int) {
		if maxLen < 0 {
			maxLen = 0
		}
		result := MaskSensitiveData(data, maxLen)
		if len(result) > maxLen {
			t.Errorf("MaskSensitiveData len=%d, want <= %d", len(result), maxLen)
		}
	})
}
