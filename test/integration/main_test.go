//go:build integration

package integration

import (
	"os"
	"strings"
	"testing"

	"akswitch/internal/cli"
	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/server"
	"net/http/httptest"
)

// ── Shared helpers ─────────────────────────────────

// resetAllEnv unsets all config env vars to prevent interference between tests.
func resetAllEnv() {
	for _, k := range []string{
		"PORT", "TARGET_BASE_URL", "ADMIN_TOKEN",
		"DISABLE_THINKING", "GENAI_MODEL", "MAX_RETRIES", "LOG_LEVEL",
		"COOLDOWN_SEC", "API_KEYS", "KEY", "KEY1", "KEY2", "KEY3",
		"KEY4", "KEY5", "KEYA", "KEYB",
		"BACKOFF_CAP_SEC", "BACKOFF_MULTIPLIER", "CB_RESET_SEC",
		"UPSTREAM_CB_THRESHOLD", "KEYS_FILE",
		"HEALTH_CHECK_INTERVAL_SEC", "HEALTH_CHECK_PATH", "HEALTH_CHECK_TIMEOUT_SEC",
	} {
		os.Unsetenv(k)
	}
}

// setupServer creates a mock upstream and an AK Switch test server, returning both.
// The caller must close both servers.
func setupServer(tb testing.TB, upstream *httptest.Server, poolKeys []string, maxRetries, cooldownSec int) *httptest.Server {
	tb.Helper()
	cfg := &config.Config{
		TargetBase:  upstream.URL,
		Port:        0,
		MaxRetries:  maxRetries,
		CooldownSec: cooldownSec,
	}
	pool := keypool.NewKeyPool(poolKeys, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	return httptest.NewServer(pr.Handler())
}

// newTestServer creates a test server with default config.
func newTestServer(keys []string) *httptest.Server {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "",
		Keys:        []string{"key-a", "key-b"},
	}
	pool := keypool.NewKeyPool(keys, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	return httptest.NewServer(pr.Handler())
}

// newServer creates a mock upstream and an AK Switch test server with full config control,
// returning both the ProviderRouter (for accessing provider state) and the test server.
// The caller must close both servers.
func newServer(tb testing.TB, cfg *config.Config, keys []string) (*server.ProviderRouter, *httptest.Server) {
	tb.Helper()
	pool := keypool.NewKeyPool(keys, nil)

	// Apply CB/health check defaults matching NewServerState's fallback logic
	if cfg.CBResetSec <= 0 {
		cfg.CBResetSec = 30
	}
	if cfg.UpstreamCBThreshold <= 0 {
		cfg.UpstreamCBThreshold = 5
	}
	if cfg.BackoffCapSec <= 0 {
		cfg.BackoffCapSec = 120
	}
	if cfg.BackoffMultiplier <= 0 {
		cfg.BackoffMultiplier = 2
	}

	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	return pr, httptest.NewServer(pr.Handler())
}

// runAkswitch is a helper for running CLI commands in tests.
func runAkswitch(t testing.TB, args ...string) error {
	return cli.RunCommand(t, args...)
}

// assertOutputContains asserts that output contains all fragments.
func assertOutputContains(t *testing.T, output string, fragments []string) {
	t.Helper()
	for _, f := range fragments {
		if !strings.Contains(output, f) {
			t.Errorf("output should contain %q:\n%s", f, output)
		}
	}
}

// assertOutputNotContains asserts that output does not contain any fragments.
func assertOutputNotContains(t *testing.T, output string, fragments []string) {
	t.Helper()
	for _, f := range fragments {
		if strings.Contains(output, f) {
			t.Errorf("output should NOT contain %q:\n%s", f, output)
		}
	}
}
