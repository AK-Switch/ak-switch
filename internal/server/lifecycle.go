package server

import (
	"net/http"
	"time"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	akswitchmetrics "akswitch/internal/metrics"
)

// RefreshKeyPoolMetrics periodically updates the keypool gauge metrics.
func RefreshKeyPoolMetrics(metrics *akswitchmetrics.Metrics, pool *keypool.KeyPool, stop <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			metrics.RefreshKeyPoolGauge(pool)
		}
	}
}

// ActiveHealthCheck periodically probes the upstream endpoint and updates
// the upstream circuit breaker state based on the response.
func ActiveHealthCheck(cfg *config.Config, proxy *ProxyEngine, metrics *akswitchmetrics.Metrics, hcState *ServerState, stop <-chan struct{}) {
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
			metrics.HealthCheckDuration.Observe(dur.Seconds())

			if err == nil && resp.StatusCode < 500 {
				resp.Body.Close()
				upCB.RecordSuccess()
				hcState.SetLastHealthCheck(true)
				metrics.HealthCheckProbes.WithLabelValues("ok").Inc()
			} else {
				if err == nil {
					resp.Body.Close()
				}
				upCB.RecordFailure()
				hcState.SetLastHealthCheck(false)
				metrics.HealthCheckProbes.WithLabelValues("fail").Inc()
			}

			metrics.UpstreamCBState.Set(float64(upCB.State()))
		}
	}
}
