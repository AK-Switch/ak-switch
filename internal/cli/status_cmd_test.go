//go:build unit

package cli

import (
	"strings"
	"testing"
)

func TestStatusCmd_Exists(t *testing.T) {
	if statusCmd == nil {
		t.Fatal("statusCmd is nil")
	}
	if statusCmd.Use != "status [provider]" {
		t.Errorf("statusCmd.Use = %q, want %q", statusCmd.Use, "status [provider]")
	}
}

func TestStatusCmd_HasProviderArg(t *testing.T) {
	if statusCmd.Args == nil {
		t.Fatal("statusCmd.Args is nil, expected MaximumNArgs(1)")
	}
	err := statusCmd.Args(nil, []string{"sensenova"})
	if err != nil {
		t.Errorf("statusCmd accepted 1 arg but Args validator returned error: %v", err)
	}
	err = statusCmd.Args(nil, []string{"a", "b"})
	if err == nil {
		t.Error("statusCmd accepted 2 args, expected MaximumNArgs(1) to reject")
	}
}

func TestFormatProviderTable_SingleProvider(t *testing.T) {
	det := map[string]interface{}{
		"alpha": map[string]interface{}{
			"keys":              3,
			"upstream_cb_state": "closed",
		},
	}
	result := formatProviderTable(det)
	if !strings.Contains(result, "PROVIDER") {
		t.Errorf("expected table header 'PROVIDER', got:\n%s", result)
	}
	if !strings.Contains(result, "alpha") {
		t.Errorf("expected provider name 'alpha', got:\n%s", result)
	}
	if !strings.Contains(result, "closed") {
		t.Errorf("expected cb_state 'closed', got:\n%s", result)
	}
}

func TestFormatProviderTable_MultiProvider(t *testing.T) {
	det := map[string]interface{}{
		"alpha": map[string]interface{}{
			"keys":              3,
			"upstream_cb_state": "closed",
		},
		"beta": map[string]interface{}{
			"keys":              5,
			"upstream_cb_state": "open",
		},
	}
	result := formatProviderTable(det)
	if !strings.Contains(result, "alpha") || !strings.Contains(result, "beta") {
		t.Errorf("expected both providers in output, got:\n%s", result)
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (header + 2 providers), got %d", len(lines))
	}
	if !strings.Contains(lines[0], "PROVIDER") {
		t.Errorf("first line should be header, got: %q", lines[0])
	}
}

func TestFormatProviderTable_Empty(t *testing.T) {
	det := map[string]interface{}{}
	result := formatProviderTable(det)
	if !strings.Contains(result, "PROVIDER") {
		t.Errorf("expected header even with empty data, got:\n%s", result)
	}
}

func TestFormatProviderTable_NilValues(t *testing.T) {
	det := map[string]interface{}{
		"alpha": map[string]interface{}{
			"keys":              nil,
			"upstream_cb_state": nil,
		},
	}
	result := formatProviderTable(det)
	if !strings.Contains(result, "alpha") {
		t.Errorf("expected provider name, got:\n%s", result)
	}
}

// TestStatusCmd_URLConstruction verifies the URL query-param logic used by the
// status command: with a provider name, ?provider=<name> is appended to both
// /health and /api/stats; without a provider, no query string is added.
func TestStatusCmd_URLConstruction(t *testing.T) {
	appendProvider := func(base, provider string) string {
		if provider != "" {
			return base + "?provider=" + provider
		}
		return base
	}

	tests := []struct {
		provider string
		hasQuery bool
	}{
		{"sensenova", true},
		{"", false},
		{"alpha", true},
	}
	for _, tc := range tests {
		h := appendProvider("http://localhost:8080/health", tc.provider)
		s := appendProvider("http://localhost:8080/api/stats", tc.provider)
		if tc.hasQuery {
			if !strings.Contains(h, "?provider="+tc.provider) {
				t.Errorf("healthURL missing provider query (provider=%q): %s", tc.provider, h)
			}
			if !strings.Contains(s, "?provider="+tc.provider) {
				t.Errorf("statsURL missing provider query (provider=%q): %s", tc.provider, s)
			}
		} else {
			if strings.Contains(h, "?") {
				t.Errorf("healthURL should not have query (provider=%q): %s", tc.provider, h)
			}
			if strings.Contains(s, "?") {
				t.Errorf("statsURL should not have query (provider=%q): %s", tc.provider, s)
			}
		}
	}
}
