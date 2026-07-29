//go:build unit

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"akswitch/internal/config"
	"path/filepath"
)

func TestStatusCmd_Exists(t *testing.T) {
	if statusCmd == nil {
		t.Fatal("expected statusCmd to be defined")
	}
}

func TestStatusCmd_Use(t *testing.T) {
	if statusCmd.Use != "status [provider]" {
		t.Errorf("statusCmd.Use = %q, want %q", statusCmd.Use, "status [provider]")
	}
}

func runStatusCmd(t *testing.T, args []string, handler http.HandlerFunc) string {
	t.Helper()

	ts := httptest.NewServer(handler)
	defer ts.Close()

	_, portStr, _ := net.SplitHostPort(ts.Listener.Addr().String())

	tmpDir := t.TempDir()
	tomlPath := filepath.Join(tmpDir, "config.toml")
	content := fmt.Sprintf("port = %s\nhost = \"127.0.0.1\"\n\n[provider]\n[provider.test]\ntarget = \"http://example.com\"\ngenai = \"http://example.com\"\n", portStr)
	if err := os.WriteFile(tomlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	oldConfigDir := config.ConfigDir
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = oldConfigDir })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	oldStderr := os.Stderr
	os.Stderr = w

	cmdArgs := append([]string{"status"}, args...)
	rootCmd.SetArgs(cmdArgs)
	err = rootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	if err != nil {
		return "ERROR: " + err.Error()
	}
	return buf.String()
}

func TestStatusCmd_NoProvider_ShowsAll(t *testing.T) {
	output := runStatusCmd(t, []string{}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy",
				"details": map[string]interface{}{
					"alpha": map[string]interface{}{
						"keys":              3,
						"upstream_cb_state": "closed",
					},
					"beta": map[string]interface{}{
						"keys":              2,
						"upstream_cb_state": "closed",
					},
				},
			})
		}
	})

	if !strings.Contains(output, "alpha") {
		t.Errorf("expected output to contain 'alpha', got:\n%s", output)
	}
	if !strings.Contains(output, "beta") {
		t.Errorf("expected output to contain 'beta', got:\n%s", output)
	}
}

func TestStatusCmd_WithProvider_FiltersOutput(t *testing.T) {
	output := runStatusCmd(t, []string{"alpha"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy",
				"details": map[string]interface{}{
					"alpha": map[string]interface{}{
						"keys":              3,
						"upstream_cb_state": "closed",
					},
					"beta": map[string]interface{}{
						"keys":              2,
						"upstream_cb_state": "open",
					},
				},
			})
		}
	})

	if !strings.Contains(output, "alpha") {
		t.Errorf("expected output to contain 'alpha', got:\n%s", output)
	}
	if strings.Contains(output, "beta") {
		t.Errorf("expected output NOT to contain 'beta' when filtering by alpha, got:\n%s", output)
	}
}

func TestStatusCmd_WithUnknownProvider_ReturnsError(t *testing.T) {
	output := runStatusCmd(t, []string{"nonexistent"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  "healthy",
				"details": map[string]interface{}{},
			})
		}
	})

	if !strings.Contains(output, "not found") {
		t.Errorf("expected error output to contain 'not found', got:\n%s", output)
	}
}

func TestStatusCmd_WithProvider_FiltersStatsURL(t *testing.T) {
	var statsPath string
	output := runStatusCmd(t, []string{"alpha"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy",
				"details": map[string]interface{}{
					"alpha": map[string]interface{}{
						"keys":              3,
						"upstream_cb_state": "closed",
					},
					"beta": map[string]interface{}{
						"keys":              2,
						"upstream_cb_state": "open",
					},
				},
			})
		}
		if r.URL.Path == "/api/stats" {
			statsPath = r.RequestURI
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"total_requests":     100,
				"successful_requests": 95,
				"failed_requests":     5,
				"active_keys":        3,
				"cooling_keys":       0,
				"disabled_keys":      1,
				"uptime_seconds":     3600,
				"provider":           "alpha",
			})
		}
	})

	if !strings.Contains(statsPath, "provider=alpha") {
		t.Errorf("expected stats request to include ?provider=alpha, got path: %s", statsPath)
	}
	if !strings.Contains(output, "alpha") {
		t.Errorf("expected output to mention alpha stats, got:\n%s", output)
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
	// Verify header is before data (table structure)
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
	// Should still produce a header row
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
