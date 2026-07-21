package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	akswitchmetrics "akswitch/internal/metrics"
)

// RefreshKeyPoolMetrics periodically updates the keypool gauge metrics.
func RefreshKeyPoolMetrics(metrics *akswitchmetrics.Metrics, pool *keypool.KeyPool, providerName string, stop <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			metrics.RefreshKeyPoolGauge(pool, providerName)
		}
	}
}

// ActiveHealthCheck periodically probes the upstream endpoint and updates
// the upstream circuit breaker state based on the response.
func ActiveHealthCheck(cfg *config.Config, proxy *ProxyEngine, metrics *akswitchmetrics.Metrics, ps *ProviderState, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(cfg.HealthCheckIntervalSec) * time.Second)
	defer ticker.Stop()

	hcClient := &http.Client{
		Timeout: time.Duration(cfg.HealthCheckTimeoutSec) * time.Second,
	}

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			target := cfg.TargetBase + cfg.HealthCheckPath
			upCB := proxy.upCB

			start := time.Now()
			resp, err := hcClient.Head(target)
			dur := time.Since(start)

			// Update duration histogram
			metrics.HealthCheckDuration.WithLabelValues(ps.Name).Observe(dur.Seconds())

			if err == nil && resp.StatusCode < 500 {
				resp.Body.Close()
				upCB.RecordSuccess()
				ps.SetLastHealthCheck(true)
				metrics.HealthCheckProbes.WithLabelValues(ps.Name, "ok").Inc()
			} else {
				if err == nil {
					resp.Body.Close()
				}
				upCB.RecordFailure()
				ps.SetLastHealthCheck(false)
				metrics.HealthCheckProbes.WithLabelValues(ps.Name, "fail").Inc()
			}

			metrics.UpstreamCBState.WithLabelValues(ps.Name).Set(float64(upCB.State()))
		}
	}
}

// StartupKeyProbe performs a lightweight health check on each key in the pool during startup.
// Keys that return 401 Unauthorized or 403 Forbidden are automatically disabled.
// The probe uses a short timeout (3 seconds) and does not block startup.
func StartupKeyProbe(pool *keypool.KeyPool, target string) {
	client := &http.Client{Timeout: 3 * time.Second}
	target = strings.TrimRight(target, "/")
	probeURL := target + "/models"
	for i := 0; i < pool.Len(); i++ {
		if pool.IsDisabled(i) {
			continue
		}
		keyName, _ := pool.Name(i)
		// Build a minimal probe request
		req, err := http.NewRequest("GET", probeURL, nil)
		if err != nil {
			slog.Warn("failed to build key probe request", "key_index", i, "key_name", keyName, "error", err)
			continue
		}
		key := pool.Keys()[i]
		req.Header.Set("Authorization", "Bearer "+key)

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("key probe failed (network)", "key_index", i, "key_name", keyName, "error", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			pool.Disable(i)
			slog.Warn("key disabled by startup probe", "key_index", i, "key_name", keyName, "status", resp.StatusCode)
		} else {
			slog.Info("key health check passed", "key_index", i, "key_name", keyName, "status", resp.StatusCode)
		}
	}

	if pool.ActiveCount() == 0 {
		slog.Error("all keys failed health check, server may not function")
	} else if pool.DisabledCount() > 0 {
		slog.Info("startup key probe complete", "active", pool.ActiveCount(), "disabled", pool.DisabledCount())
	} else {
		slog.Info("startup key probe complete", "active", pool.ActiveCount())
	}
}
