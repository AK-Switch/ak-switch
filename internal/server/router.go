package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"akswitch/internal/config"
	"akswitch/internal/keypool"

	akswitchmetrics "akswitch/internal/metrics"
	"akswitch/internal/tracker"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ProviderState holds runtime state for a single provider in the routing table.
type ProviderState struct {
	Name   string
	Config *config.Config
	Pool   *keypool.KeyPool
	Proxy  *ProxyEngine

	// Health check state (protected by healthMu)
	healthMu            sync.RWMutex
	lastHealthCheckTime time.Time
	lastHealthCheckOK   bool

	// Per-provider management state
	dashboardHTML string
	keysFile      string
}

// NewProviderState creates a fully initialized ProviderState for a single provider.
func NewProviderState(name string, cfg *config.Config, pool *keypool.KeyPool, dashboardHTML string, keysFile string) *ProviderState {
	proxy := NewProxyEngine(cfg, pool)

	return &ProviderState{
		Name:          name,
		Config:        cfg,
		Pool:          pool,
		Proxy:         proxy,
		dashboardHTML: dashboardHTML,
		keysFile:      keysFile,
	}
}

// LastHealthCheck returns the timestamp and result of the most recent active health check.
func (ps *ProviderState) LastHealthCheck() (time.Time, bool) {
	ps.healthMu.RLock()
	defer ps.healthMu.RUnlock()
	return ps.lastHealthCheckTime, ps.lastHealthCheckOK
}

// SetLastHealthCheck records the result of an active health check probe.
func (ps *ProviderState) SetLastHealthCheck(ok bool) {
	ps.healthMu.Lock()
	defer ps.healthMu.Unlock()
	ps.lastHealthCheckTime = time.Now()
	ps.lastHealthCheckOK = ok
}

// PersistKeys saves the current key pool state (including disabled state)
// to the system keyring. Called after key mutations through the management API
// and proxy flow (401/403 disable).
func (ps *ProviderState) PersistKeys() {
	keys := ps.Pool.Keys()
	entries := make([]keypool.KeyEntry, len(keys))
	for i := range keys {
		n, _ := ps.Pool.Name(i)
		entries[i] = keypool.KeyEntry{
			Key:      keys[i],
			Name:     n,
			Disabled: ps.Pool.IsDisabled(i),
		}
	}
	if err := keypool.SaveKeys(ps.Name, &keypool.KeyStore{Keys: entries}); err != nil {
		slog.Error("failed to persist keys to keyring", "provider", ps.Name, "error", err)
	}
}

// ProviderRouter manages a single-port HTTP server with path-based provider routing.
type ProviderRouter struct {
	mu        sync.RWMutex
	proxy     *http.Server
	listener  net.Listener
	providers map[string]*ProviderState

	startTime       time.Time
	metrics         *akswitchmetrics.Metrics
	metricsRegistry *prometheus.Registry
	dashboardHTML   string
	taskManager     *BackgroundTaskManager
	wg              sync.WaitGroup // for HTTP server goroutine only
	mux             *http.ServeMux // cached mux for Handler()
	muxOnce         sync.Once
	calibrator      *tracker.Calibrator // per-model token estimation calibration
	logManager      *LogManager
	proxyExecutor   *ProxyExecutor
	api             *AdminAPI

	// Key operation handlers (initialized via keyOperationHandler factory)
	disableKeyHandler  http.HandlerFunc
	enableKeyHandler   http.HandlerFunc
	cooldownKeyHandler http.HandlerFunc
	deleteKeyHandler   http.HandlerFunc
}

// NewProviderRouter creates a new ProviderRouter.
func NewProviderRouter(dashboardHTML string) *ProviderRouter {
	reg, m := akswitchmetrics.NewRegistry()
	pr := &ProviderRouter{
		providers:       make(map[string]*ProviderState),
		startTime:       time.Now(),
		metrics:         m,
		metricsRegistry: reg,
		dashboardHTML:   dashboardHTML,
		calibrator:      tracker.NewCalibrator(15),
		logManager:      NewLogManager(),
	}
	pr.taskManager = NewBackgroundTaskManager(m)

	// Initialize proxy executor with shared dependencies
	pr.proxyExecutor = NewProxyExecutor(m, pr.calibrator)

	// Initialize key operation handlers via factory function
	pr.disableKeyHandler = pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Disable(idx)
	})
	pr.enableKeyHandler = pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.Enable(idx)
	})
	pr.cooldownKeyHandler = pr.keyOperationHandler(func(pool *keypool.KeyPool, cfg *config.Config, idx int) error {
		return pool.Cooldown(idx, time.Duration(cfg.CooldownSec)*time.Second)
	})
	pr.deleteKeyHandler = pr.keyOperationHandler(func(pool *keypool.KeyPool, _ *config.Config, idx int) error {
		return pool.RemoveKey(idx)
	})

	// Initialize AdminAPI — pass self as ProviderLookup
	pr.api = NewAdminAPI(pr, pr.logManager, dashboardHTML, pr.startTime)

	return pr
}

// AddProvider creates a new ProviderState with the given name, config, and key pool.
func (pr *ProviderRouter) AddProvider(name string, cfg *config.Config, pool *keypool.KeyPool) error {
	ps := NewProviderState(name, cfg, pool, pr.dashboardHTML, cfg.KeysFile)
	pr.mu.Lock()
	pr.providers[name] = ps
	pr.mu.Unlock()
	return nil
}

// Start binds ONE port and starts the HTTP server.
func (pr *ProviderRouter) Start(host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)

	// Use cached mux from Handler()
	mux := pr.Handler()

	pr.mu.Lock()
	defer pr.mu.Unlock()

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port bind failed: %w", err)
	}
	pr.listener = listener
	pr.proxy = &http.Server{Handler: mux}

	pr.wg.Add(1)
	go func() {
		defer pr.wg.Done()
		slog.Info("server started",
			"addr", listener.Addr().String(),
			"providers", len(pr.providers))
		if err := pr.proxy.Serve(listener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	return nil
}

// StartWithListener starts the HTTP server with an already-acquired listener.
// This avoids the Listen→Close→Listen race condition that can occur in dev mode
// when probing for an available port, since the listener is immediately handed
// to the server without any time window for another process to grab the port.
func (pr *ProviderRouter) StartWithListener(listener net.Listener) error {
	mux := pr.Handler()

	pr.mu.Lock()
	defer pr.mu.Unlock()

	pr.listener = listener
	pr.proxy = &http.Server{Handler: mux}

	pr.wg.Add(1)
	go func() {
		defer pr.wg.Done()
		slog.Info("server started",
			"addr", listener.Addr().String(),
			"providers", len(pr.providers))
		if err := pr.proxy.Serve(listener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	return nil
}

// registerRoutes builds the combined mux with all routes.
func (pr *ProviderRouter) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", pr.healthHandler)
	mux.HandleFunc("/logs", pr.logsHandler)
	mux.HandleFunc("/dashboard", pr.dashboardHandler)
	mux.HandleFunc("/clear", pr.clearHandler)
	mux.HandleFunc("/api/config", pr.configHandler)
	mux.HandleFunc("/api/keys", pr.keysHandler)
	mux.HandleFunc("POST /api/keys/{index}/disable", pr.disableKeyHandler)
	mux.HandleFunc("POST /api/keys/{index}/enable", pr.enableKeyHandler)
	mux.HandleFunc("PUT /api/keys/{index}/cooldown", pr.cooldownKeyHandler)
	mux.HandleFunc("DELETE /api/keys/{index}", pr.deleteKeyHandler)
	mux.HandleFunc("GET /api/stats", pr.statsHandler)
	mux.HandleFunc("POST /api/stats/reset-upstream-cb", pr.upstreamCBResetHandler)
	mux.HandleFunc("POST /api/reload", pr.reloadHandler)
	mux.HandleFunc("/api/log-level", pr.logLevelHandler)
	mux.HandleFunc("/api/runtime-config", pr.runtimeConfigHandler)
	mux.Handle("GET /metrics", promhttp.HandlerFor(pr.metricsRegistry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/sw.js", pr.swHandler)
	mux.HandleFunc("/", pr.proxyHandler)
}

// Handler returns the HTTP handler (mux) for use by http.Server, httptest, or Start().
// The mux is built once and cached for the lifetime of the router.
func (pr *ProviderRouter) Handler() *http.ServeMux {
	pr.muxOnce.Do(func() {
		pr.mu.Lock()
		mux := http.NewServeMux()
		pr.registerRoutes(mux)
		pr.mux = mux
		pr.mu.Unlock()
	})
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.mux
}

// Shutdown gracefully shuts down the HTTP server.
func (pr *ProviderRouter) Shutdown(ctx context.Context) {
	pr.mu.RLock()
	srv := pr.proxy
	pr.mu.RUnlock()

	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("server shutdown error", "error", err)
		} else {
			slog.Info("server shut down")
		}
	}
}

// Stop signals all background tasks to stop and waits for all goroutines.
func (pr *ProviderRouter) Stop() {
	pr.taskManager.Stop()
	pr.wg.Wait()
}

// ProviderNames returns the names of all registered providers.
func (pr *ProviderRouter) ProviderNames() []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	names := make([]string, 0, len(pr.providers))
	for name := range pr.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LookupProvider returns the ProviderState with the given name.
func (pr *ProviderRouter) LookupProvider(name string) *ProviderState {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.providers[name]
}

// FirstProvider returns the first (alphabetically) provider.
func (pr *ProviderRouter) FirstProvider() *ProviderState {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	for _, ps := range pr.providers {
		return ps
	}
	return nil
}

// ForEach iterates over all providers.
func (pr *ProviderRouter) ForEach(fn func(name string, ps *ProviderState)) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	for name, ps := range pr.providers {
		fn(name, ps)
	}
}

// Provider returns the ProviderState with the given name, or nil if not found.
func (pr *ProviderRouter) Provider(name string) *ProviderState {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.providers[name]
}

// Metrics returns the router-level Prometheus metrics collector.
func (pr *ProviderRouter) Metrics() *akswitchmetrics.Metrics {
	return pr.metrics
}

// LogManager returns the router-level logging configuration manager.
func (pr *ProviderRouter) LogManager() *LogManager {
	return pr.logManager
}

// StartBackgroundTasks launches background goroutines (metrics refresh, active health check)
// for each registered provider.
func (pr *ProviderRouter) StartBackgroundTasks() {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	for _, ps := range pr.providers {
		p := ps // capture
		pr.taskManager.StartKeyPoolMetrics(p.Pool, p.Name)
		pr.taskManager.StartHealthCheck(p.Config, p.Proxy, p)

		// Start per-provider calibration ticker (if model is configured)
		if p.Config.GenaiModel != "" {
			interval := time.Duration(p.Config.CalibrationIntervalSec) * time.Second
			pr.taskManager.StartCalibrator(pr.calibrator, p.Pool, p.Config.TargetBase, p.Config.GenaiModel, interval)
		}
	}

	// Router-level uptime gauge
	pr.taskManager.StartUptimeTicker(pr.startTime)
}

// extractProvider parses the first path segment as the provider name and returns the rest.
func (pr *ProviderRouter) extractProvider(path string) (providerName, restPath string) {
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", "/"
	}
	if len(parts) == 1 {
		return parts[0], "/"
	}
	return parts[0], "/" + parts[1]
}
