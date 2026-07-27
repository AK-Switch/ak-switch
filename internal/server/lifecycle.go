package server

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	akswitchmetrics "akswitch/internal/metrics"
	"akswitch/internal/tokenestimator"
	"akswitch/internal/tracker"
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

// calibrationTarget constructs the upstream messages endpoint URL from the target base.
// Handles the case where TargetBase already ends with "/v1".
func calibrationTarget(targetBase string) string {
	base := strings.TrimRight(targetBase, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

// sendCalibrationRequest sends a single tiny non-streaming request to calibrate
// token estimation. The actual token values from the upstream response are
// recorded in the Calibrator for comparison against tiktoken estimates.
// Errors are logged but do not affect the caller.
func sendCalibrationRequest(calibrator *tracker.Calibrator, pool *keypool.KeyPool, targetBase, model string) {
	if model == "" {
		return
	}

	// Build minimal calibration request body
	body := fmt.Sprintf(`{"model":"%s","max_tokens":1,"messages":[{"role":"user","content":"Hi"}]}`, model)

	// Get a key from the pool, release immediately so it's available for proxy requests
	idx, key, ok := pool.Next()
	if !ok {
		slog.Debug("calibration: no available key, skipping")
		return
	}
	pool.Release(idx)

	// Build the calibration target URL
	calURL := calibrationTarget(targetBase)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", calURL, strings.NewReader(body))
	if err != nil {
		slog.Warn("calibration: failed to create request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("calibration: request failed", "error", err)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("calibration: failed to read response", "error", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		slog.Warn("calibration: non-200 response", "status", resp.StatusCode, "body_preview", string(respBody[:min(len(respBody), 200)]))
		return
	}

	// Extract actual token usage from upstream response
	actualInput, actualOutput := tokenestimator.ExtractTokenUsage(respBody)
	if actualInput == 0 && actualOutput == 0 {
		slog.Debug("calibration: no token usage in response, skipping")
		return
	}

	// Run tiktoken estimation for comparison
	estInput := tokenestimator.EstimateInput([]byte(body), model)
	respText := tokenestimator.ExtractResponseText(respBody)
	estOutput := tokenestimator.EstimateOutput(respText, model)

	// Record calibration samples
	if estInput > 0 && actualInput > 0 {
		calibrator.Record(model, estInput, actualInput)
	}
	if estOutput > 0 && actualOutput > 0 {
		calibrator.Record(model, estOutput, actualOutput)
	}

	slog.Debug("calibration: recorded",
		"model", model,
		"input_estimate", estInput,
		"input_actual", actualInput,
		"output_estimate", estOutput,
		"output_actual", actualOutput,
	)
}

// PeriodicCalibrator periodically sends tiny non-streaming requests to calibrate
// token estimation. It runs once immediately on start, then at the configured interval.
// Calibration runs per-provider — each provider gets its own goroutine.
// Errors are non-blocking: upstream failures are logged but do not affect the main flow.
func PeriodicCalibrator(calibrator *tracker.Calibrator, pool *keypool.KeyPool, targetBase, model string, interval time.Duration, stop <-chan struct{}) {
	if model == "" || interval <= 0 {
		return
	}

	// Run once immediately on startup
	sendCalibrationRequest(calibrator, pool, targetBase, model)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			sendCalibrationRequest(calibrator, pool, targetBase, model)
		}
	}
}
