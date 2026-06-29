package server

import (
	"log/slog"
	"net/http"
	"os"
	"time"
)

// WatchEnvFile monitors .env for changes and hot-reloads the configuration.
func WatchEnvFile(state *ServerState, stop <-chan struct{}) {
	var lastMod time.Time
	if info, err := os.Stat(".env"); err == nil {
		lastMod = info.ModTime()
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			info, err := os.Stat(".env")
			if err != nil {
				if os.IsNotExist(err) {
					slog.Info("env deleted, keeping current config")
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}
			if !info.ModTime().After(lastMod) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			lastMod = info.ModTime()
			time.Sleep(100 * time.Millisecond) // debounce

			slog.Info("env changed, reloading")

			state.mu.RLock()
			oldCfg := state.cfg
			state.mu.RUnlock()

			newCfg, newPool, err := ReloadConfig()
			if err != nil {
				slog.Error("env reload failed; keeping previous config", "error", err)
				time.Sleep(10 * time.Millisecond)
				continue
			}

			// Log configuration changes (sensitive fields masked)
			changes := oldCfg.Diff(newCfg)
			if len(changes) > 0 {
				for _, c := range changes {
					slog.Info("config changed", "field", c.Field, "old", c.OldValue, "new", c.NewValue)
				}
			}

			state.mu.Lock()
			state.cfg = newCfg
			state.pool = newPool
			state.mu.Unlock()

			slog.Info("config reloaded", "keys", len(newPool.Keys()), "target", newCfg.TargetBase, "genai", newCfg.GenaiBase)
		}
	}
}

// RefreshKeyPoolMetrics periodically updates the keypool gauge metrics.
func RefreshKeyPoolMetrics(state *ServerState, stop <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			state.mu.RLock()
			pool := state.pool
			state.mu.RUnlock()
			state.metrics.RefreshKeyPoolGauge(pool)
		}
	}
}

// ActiveHealthCheck periodically probes the upstream endpoint and updates
// the upstream circuit breaker state based on the response.
func ActiveHealthCheck(state *ServerState, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(state.cfg.HealthCheckIntervalSec) * time.Second)
	defer ticker.Stop()

	hcClient := &http.Client{
		Timeout: time.Duration(state.cfg.HealthCheckTimeoutSec) * time.Second,
	}

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			state.mu.RLock()
			target := state.cfg.TargetBase + state.cfg.HealthCheckPath
			upCB := state.upCB
			state.mu.RUnlock()

			start := time.Now()
			resp, err := hcClient.Head(target)
			dur := time.Since(start)

			// Update duration histogram
			state.metrics.HealthCheckDuration.Observe(dur.Seconds())

			if err == nil && resp.StatusCode < 500 {
				// Success — upstream is healthy
				resp.Body.Close()
				upCB.RecordSuccess()
				state.SetLastHealthCheck(true)
				state.metrics.HealthCheckProbes.WithLabelValues("ok").Inc()
			} else {
				// Failure
				if err == nil {
					resp.Body.Close()
				}
				upCB.RecordFailure()
				state.SetLastHealthCheck(false)
				state.metrics.HealthCheckProbes.WithLabelValues("fail").Inc()
			}

			// Update CB state gauge
			state.metrics.UpstreamCBState.Set(float64(upCB.State()))
		}
	}
}
