//go:build integration

package integration

import (
	"encoding/json"

	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/server"
)

func TestLogEntry_HasNewFields(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	logFile := filepath.Join(t.TempDir(), "test.log")
	cfg := &config.Config{
		TargetBase:  upstream.URL,
		Port:        0,
		MaxRetries:  10,
		CooldownSec: 60,
	}
	pool := keypool.NewKeyPool([]string{"test-key-a", "test-key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.LogManager().InitFileHandler(logFile, 10, 1)
	pr.LogManager().ApplyLevel("debug")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	defer pr.LogManager().CloseFileHandler()
	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	resp.Body.Close()

	logResp, err := http.Get(srv.URL + "/logs")
	if err != nil {
		t.Fatalf("GET /logs: %v", err)
	}
	defer logResp.Body.Close()

	var entries []map[string]interface{}
	if err := json.NewDecoder(logResp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 log entry")
	}

	entry := entries[0]
	for _, field := range []string{"provider", "duration_ms", "retry", "key_name"} {
		if _, ok := entry[field]; !ok {
			t.Errorf("log entry missing %q field", field)
		}
	}

	if p, ok := entry["provider"].(string); !ok || p != "test" {
		t.Errorf("expected provider=\"test\", got %v", entry["provider"])
	}
	if a, ok := entry["retry"].(float64); !ok || a < 0 {
		t.Errorf("expected retry >= 0, got %v", entry["retry"])
	}
	if d, ok := entry["duration_ms"].(float64); !ok || d < 0 {
		t.Errorf("expected duration_ms >= 0, got %v", entry["duration_ms"])
	}
}

// TestLogEntry_ExhaustionHas503 verifies that after all keys are exhausted
// the log store contains a 503 entry with the new fields populated.
func TestLogEntry_ExhaustionHas503(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	// MaxRetries=2, 3 keys, all 429 -> 503
	logFile := filepath.Join(t.TempDir(), "test.log")
	cfg := &config.Config{
		TargetBase:  upstream.URL,
		Port:        0,
		MaxRetries:  2,
		CooldownSec: 2,
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.LogManager().InitFileHandler(logFile, 10, 1)
	pr.LogManager().ApplyLevel("debug")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	defer pr.LogManager().CloseFileHandler()
	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	logResp, err := http.Get(srv.URL + "/logs")
	if err != nil {
		t.Fatalf("GET /logs: %v", err)
	}
	defer logResp.Body.Close()

	var entries []map[string]interface{}
	if err := json.NewDecoder(logResp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode logs: %v", err)
	}

	var found503 bool
	for _, entry := range entries {
		status, _ := entry["status"].(float64)
		if int(status) != http.StatusServiceUnavailable {
			continue
		}
		found503 = true
		for _, field := range []string{"provider", "duration_ms", "retry", "key_name"} {
			if _, ok := entry[field]; !ok {
				t.Errorf("503 log entry missing %q field", field)
			}
		}
		if p, ok := entry["provider"].(string); !ok || p != "test" {
			t.Errorf("expected provider=\"test\", got %v", entry["provider"])
		}
		if a, ok := entry["retry"].(float64); !ok || a < 2 {
			t.Errorf("expected retry >= 2 (MaxRetries), got %v", entry["retry"])
		}
		break
	}
	if !found503 {
		t.Error("no 503 log entry found after retry exhaustion")
	}
}

// TestLogEntry_CLIFormat verifies the log JSON structure is suitable
// for CLI display — all fields the CLI command needs are present.
func TestLogEntry_CLIFormat(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	logFile := filepath.Join(t.TempDir(), "test.log")
	cfg := &config.Config{
		TargetBase:  upstream.URL,
		Port:        0,
		MaxRetries:  10,
		CooldownSec: 60,
	}
	pool := keypool.NewKeyPool([]string{"test-key-a", "test-key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.LogManager().InitFileHandler(logFile, 10, 1)
	pr.LogManager().ApplyLevel("debug")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	defer pr.LogManager().CloseFileHandler()
	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	resp.Body.Close()

	logResp, err := http.Get(srv.URL + "/logs")
	if err != nil {
		t.Fatalf("GET /logs: %v", err)
	}
	defer logResp.Body.Close()

	var entries []map[string]interface{}
	if err := json.NewDecoder(logResp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 log entry")
	}

	entry := entries[0]
	// All fields the CLI display code in internal/cli/logs.go reads
	for _, field := range []string{"method", "url", "status", "timestamp", "provider", "duration_ms", "retry", "key_name"} {
		if _, ok := entry[field]; !ok {
			t.Errorf("CLI display needs field %q, but log entry missing it", field)
		}
	}

	t.Logf("CLI-ready log entry: provider=%v duration_ms=%v retry=%v key_name=%v",
		entry["provider"], entry["duration_ms"], entry["retry"], entry["key_name"])
}

