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
	"akswitch/internal/logstore"
	"akswitch/internal/tracker"
	akswitchmetrics "akswitch/internal/metrics"

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

// PersistKeys saves the current key pool state to the keys file.
// Called after key mutations through the management API.
func (ps *ProviderState) PersistKeys() {
	if ps.Config.KeysFile == "" {
		return
	}
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
	if err := keypool.SaveFullStore(ps.Config.KeysFile, &keypool.KeyStore{Keys: entries}); err != nil {
		slog.Error("failed to persist keys", "path", ps.Config.KeysFile, "error", err)
	}
}

// ProviderRouter manages a single-port HTTP server with path-based provider routing.
type ProviderRouter struct {
	mu              sync.RWMutex
	proxy           *http.Server
	listener        net.Listener
	providers       map[string]*ProviderState
	logs            *logstore.LogStore
	startTime       time.Time
	metrics         *akswitchmetrics.Metrics
	metricsRegistry *prometheus.Registry
	dashboardHTML   string
	stop            chan struct{}
	wg              sync.WaitGroup
	mux             *http.ServeMux // cached mux for Handler()
	muxOnce         sync.Once
	calibrator      *tracker.Calibrator // per-model token estimation calibration

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
		logs:            logstore.New(10000),
		startTime:       time.Now(),
		metrics:         m,
		metricsRegistry: reg,
		dashboardHTML:   dashboardHTML,
		stop:            make(chan struct{}),
		calibrator:      tracker.NewCalibrator(15),
	}
	pr.logs.OnAppend = func(prevLen, newLen, maxLen int) {
		pr.metrics.LogStoreEntries.Inc()
		if dropped := (prevLen + 1) - newLen; dropped > 0 {
			pr.metrics.LogStoreDropped.Add(float64(dropped))
		}
		pr.metrics.LogStoreFillRatio.Set(float64(newLen) / float64(maxLen))
	}

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
	mux.HandleFunc("POST /api/reload", pr.reloadHandler)
	mux.HandleFunc("/api/log-level", pr.logLevelHandler)
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
	close(pr.stop)
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

// StartBackgroundTasks launches background goroutines (metrics refresh, active health check)
// for each registered provider.
func (pr *ProviderRouter) StartBackgroundTasks() {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	for _, ps := range pr.providers {
		p := ps // capture
		pr.wg.Add(1)
		go func() {
			defer pr.wg.Done()
			RefreshKeyPoolMetrics(pr.metrics, p.Pool, p.Name, pr.stop)
		}()

		pr.wg.Add(1)
		go func() {
			defer pr.wg.Done()
			ActiveHealthCheck(p.Config, p.Proxy, pr.metrics, p, pr.stop)
		}()
	}

	// Router-level uptime gauge
	pr.wg.Add(1)
	go func() {
		defer pr.wg.Done()
		uptimeTicker := time.NewTicker(10 * time.Second)
		defer uptimeTicker.Stop()
		for {
			select {
			case <-pr.stop:
				return
			case <-uptimeTicker.C:
				pr.metrics.UptimeSeconds.Set(time.Since(pr.startTime).Seconds())
			}
		}
	}()
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