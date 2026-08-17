package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"akswitch/internal/circuitbreaker"
	"akswitch/internal/config"
	"akswitch/internal/keypool"
	akswitchmetrics "akswitch/internal/metrics"
	"akswitch/internal/tokenestimator"
	"akswitch/internal/tracker"
)

// ServerLifecycle manages the HTTP server lifecycle: start, shutdown, and route mux.
type ServerLifecycle struct {
	proxy       *http.Server
	listener    net.Listener
	wg          sync.WaitGroup
	taskManager *BackgroundTaskManager
	mux         *http.ServeMux
	muxOnce     sync.Once
}

// NewServerLifecycle creates a ServerLifecycle.
func NewServerLifecycle(taskManager *BackgroundTaskManager) *ServerLifecycle {
	return &ServerLifecycle{taskManager: taskManager}
}

// Handler returns the mux, lazily creating one on first call.
func (sl *ServerLifecycle) Handler() *http.ServeMux {
	sl.muxOnce.Do(func() {
		if sl.mux == nil {
			sl.mux = http.NewServeMux()
		}
	})
	return sl.mux
}

// Start begins listening and serving on the given host:port.
func (sl *ServerLifecycle) Start(host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	mux := sl.Handler()
	sl.proxy = &http.Server{Handler: mux}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind failed: %w", err)
	}
	sl.listener = listener
	sl.wg.Add(1)
	go func() {
		defer sl.wg.Done()
		slog.Info("server started", "addr", listener.Addr().String())
		if err := sl.proxy.Serve(listener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()
	return nil
}

// StartWithListener begins serving on an existing listener.
func (sl *ServerLifecycle) StartWithListener(listener net.Listener) error {
	mux := sl.Handler()
	sl.listener = listener
	sl.proxy = &http.Server{Handler: mux}
	sl.wg.Add(1)
	go func() {
		defer sl.wg.Done()
		slog.Info("server started", "addr", listener.Addr().String())
		if err := sl.proxy.Serve(listener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()
	return nil
}

// Shutdown gracefully stops the server.
func (sl *ServerLifecycle) Shutdown(ctx context.Context) {
	if sl.proxy != nil {
		if err := sl.proxy.Shutdown(ctx); err != nil {
			slog.Error("shutdown error", "error", err)
		} else {
			slog.Info("server shut down")
		}
	}
}

// Stop signals background tasks to stop and waits for server goroutines to finish.
func (sl *ServerLifecycle) Stop() {
	sl.taskManager.Stop()
	sl.wg.Wait()
}

// BackgroundTaskManager manages background goroutines for the proxy server.
// It holds the stop signal and WaitGroup, and provides methods to start
// individual background tasks. Call Stop() to signal all tasks and wait
// for them to complete.
type BackgroundTaskManager struct {
	stop    chan struct{}
	wg      sync.WaitGroup
	metrics *akswitchmetrics.Metrics
}

// NewBackgroundTaskManager creates a BackgroundTaskManager.
func NewBackgroundTaskManager(metrics *akswitchmetrics.Metrics) *BackgroundTaskManager {
	return &BackgroundTaskManager{
		stop:    make(chan struct{}),
		metrics: metrics,
	}
}

// Stop signals all background tasks to stop and waits for them to complete.
func (m *BackgroundTaskManager) Stop() {
	close(m.stop)
	m.wg.Wait()
}

// StopChan returns the stop channel for sharing with external goroutines.
func (m *BackgroundTaskManager) StopChan() <-chan struct{} {
	return m.stop
}

// StartKeyPoolMetrics periodically updates the keypool gauge metrics.
func (m *BackgroundTaskManager) StartKeyPoolMetrics(pool *keypool.KeyPool, providerName string) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		RefreshKeyPoolMetrics(m.metrics, pool, providerName, m.stop)
	}()
}

// StartHealthCheck periodically probes the upstream endpoint and updates
// the upstream circuit breaker state based on the response.
func (m *BackgroundTaskManager) StartHealthCheck(cfg *config.Config, ps *ProviderState) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ActiveHealthCheck(cfg, ps, m.metrics, m.stop)
	}()
}

// StartCalibrator periodically sends tiny non-streaming requests to calibrate
// token estimation. It runs once immediately on start, then at the configured interval.
func (m *BackgroundTaskManager) StartCalibrator(calibrator *tracker.Calibrator, pool *keypool.KeyPool, targetBase, model string, interval time.Duration) {
	if model == "" || interval <= 0 {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		PeriodicCalibrator(calibrator, pool, targetBase, model, interval, m.stop)
	}()
}

// StartKeyProbe periodically probes permanently disabled keys and re-enables
// them if their quota has recovered (upstream returns 200).
func (m *BackgroundTaskManager) StartKeyProbe(pool *keypool.KeyPool, targetBase string) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		PeriodicKeyProbe(pool, targetBase, 5*time.Minute, m.stop)
	}()
}

// StartUptimeTicker periodically updates the uptime metric gauge.
func (m *BackgroundTaskManager) StartUptimeTicker(startTime time.Time) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		uptimeTicker := time.NewTicker(10 * time.Second)
		defer uptimeTicker.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-uptimeTicker.C:
				m.metrics.UptimeSeconds.Set(time.Since(startTime).Seconds())
			}
		}
	}()
}

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
func ActiveHealthCheck(cfg *config.Config, ps *ProviderState, metrics *akswitchmetrics.Metrics, stop <-chan struct{}) {
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
			upCB := ps.UpstreamCB()

			start := time.Now()
			resp, err := hcClient.Head(target)
			dur := time.Since(start)

			// Update duration histogram
			metrics.HealthCheckDuration.WithLabelValues(ps.name).Observe(dur.Seconds())

			if err == nil && resp.StatusCode < 500 {
				_ = resp.Body.Close()
				upCB.RecordSuccess()
				ps.SetLastHealthCheck(true)
				metrics.HealthCheckProbes.WithLabelValues(ps.name, "ok").Inc()
			} else {
				if err == nil {
					_ = resp.Body.Close()
				}
				upCB.RecordFailure()
				ps.SetLastHealthCheck(false)
				metrics.HealthCheckProbes.WithLabelValues(ps.name, "fail").Inc()
			}

			metrics.UpstreamCBState.WithLabelValues(ps.name).Set(float64(upCB.State()))
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
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			_ = pool.Disable(i)
			slog.Warn("key disabled by startup probe", "key_index", i, "key_name", keyName, "status", resp.StatusCode)
		} else {
			slog.Info("key health check passed", "key_index", i, "key_name", keyName, "status", resp.StatusCode)
		}
	}

	switch {
	case pool.ActiveCount() == 0:
		slog.Error("all keys failed health check, server may not function")
	case pool.DisabledCount() > 0:
		slog.Info("startup key probe complete", "active", pool.ActiveCount(), "disabled", pool.DisabledCount())
	default:
		slog.Info("startup key probe complete", "active", pool.ActiveCount())
	}
}

// PeriodicKeyProbe periodically probes permanently disabled keys by sending
// GET /models to the upstream. If the upstream returns 200 OK, the key is
// re-enabled (quota has recovered). 429, 401, and 403 responses keep the key
// disabled. Network errors are logged at debug level and do not affect the
// key's state.
func PeriodicKeyProbe(pool *keypool.KeyPool, target string, interval time.Duration, stop <-chan struct{}) {
	client := &http.Client{Timeout: 3 * time.Second}
	probeURL := strings.TrimRight(target, "/") + "/models"
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			for i := 0; i < pool.Len(); i++ {
				if pool.CB(i).State() != circuitbreaker.Permanent {
					continue
				}
				keyName, _ := pool.Name(i)
				key := pool.Keys()[i]
				req, err := http.NewRequest("GET", probeURL, nil)
				if err != nil {
					continue
				}
				req.Header.Set("Authorization", "Bearer "+key)
				resp, err := client.Do(req)
				if err != nil {
					slog.Debug("periodic key probe network error", "key_name", keyName, "error", err)
					continue
				}
				if resp.StatusCode == http.StatusOK {
					_ = pool.Enable(i)
					slog.Info("key re-enabled by periodic probe", "key_name", keyName)
				}
				resp.Body.Close()
			}
		}
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
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("calibration: failed to read response", "error", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		slog.Warn("calibration: non-200 response", "status", resp.StatusCode, "body_preview", string(respBody[:min(len(respBody), 200)]))
		return
	}

	// Extract actual token usage and response text from upstream response
	actualInput, actualOutput, respText := tokenestimator.ProcessResponse(respBody)
	if actualInput == 0 && actualOutput == 0 {
		slog.Debug("calibration: no token usage in response, skipping")
		return
	}

	// Run tiktoken estimation for comparison
	estInput := tokenestimator.EstimateInput([]byte(body), model)
	estOutput := tokenestimator.EstimateOutput(respText, model)

	// Record calibration samples
	tokenestimator.RecordCalibration(calibrator, model, estInput, actualInput, estOutput, actualOutput)

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
