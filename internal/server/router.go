package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
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

type ProviderState struct {
	Name          string
	Config        *config.Config
	Pool          *keypool.KeyPool
	Proxy         *ProxyEngine
	healthMu      sync.RWMutex
	lastCheckTime time.Time
	lastCheckOK   bool
	dashboardHTML string
	keysFile      string
}

func NewProviderState(name string, cfg *config.Config, pool *keypool.KeyPool, dash, keysFile string) *ProviderState {
	return &ProviderState{
		Name: name, Config: cfg, Pool: pool,
		Proxy:         NewProxyEngine(cfg, pool),
		dashboardHTML: dash, keysFile: keysFile,
	}
}

func (ps *ProviderState) LastHealthCheck() (time.Time, bool) {
	ps.healthMu.RLock()
	defer ps.healthMu.RUnlock()
	return ps.lastCheckTime, ps.lastCheckOK
}
func (ps *ProviderState) SetLastHealthCheck(ok bool) {
	ps.healthMu.Lock()
	defer ps.healthMu.Unlock()
	ps.lastCheckTime = time.Now()
	ps.lastCheckOK = ok
}
func (ps *ProviderState) PersistKeys() {
	keys := ps.Pool.Keys()
	entries := make([]keypool.KeyEntry, len(keys))
	for i := range keys {
		n, _ := ps.Pool.Name(i)
		entries[i] = keypool.KeyEntry{Key: keys[i], Name: n, Disabled: ps.Pool.IsDisabled(i)}
	}
	if err := keypool.SaveKeys(ps.Name, &keypool.KeyStore{Keys: entries}); err != nil {
		slog.Error("persist keys failed", "provider", ps.Name, "error", err)
	}
}

type ProviderRouter struct {
	pm              *ProviderManager
	api             *AdminAPI
	lifecycle       *ServerLifecycle
	logManager      *LogManager
	metrics         *akswitchmetrics.Metrics
	metricsRegistry *prometheus.Registry
	calibrator      *tracker.Calibrator
	taskManager     *BackgroundTaskManager
	proxyExecutor   *ProxyExecutor
}

func NewProviderRouter(dashboardHTML string) *ProviderRouter {
	reg, m := akswitchmetrics.NewRegistry()
	pm := NewProviderManager(dashboardHTML)
	cal := tracker.NewCalibrator(15)
	lm := NewLogManager()
	tm := NewBackgroundTaskManager(m)
	pe := NewProxyExecutor(m, cal)
	lifecycle := NewServerLifecycle(tm)
	api := NewAdminAPI(pm, lm, dashboardHTML, time.Now())

	pr := &ProviderRouter{
		pm: pm, api: api, lifecycle: lifecycle,
		logManager: lm, metrics: m, metricsRegistry: reg,
		calibrator: cal, taskManager: tm, proxyExecutor: pe,
	}
	mux := lifecycle.Handler()
	registerRoutes(pr, mux)
	return pr
}

func (pr *ProviderRouter) AddProvider(name string, cfg *config.Config, pool *keypool.KeyPool) error {
	return pr.pm.AddProvider(name, cfg, pool)
}

func (pr *ProviderRouter) Start(host string, port int) error {
	return pr.lifecycle.Start(host, port)
}

func (pr *ProviderRouter) StartWithListener(listener net.Listener) error {
	return pr.lifecycle.StartWithListener(listener)
}

func (pr *ProviderRouter) Shutdown(ctx context.Context) {
	pr.lifecycle.Shutdown(ctx)
}

func (pr *ProviderRouter) Stop() {
	pr.lifecycle.Stop()
}

func registerRoutes(pr *ProviderRouter, mux *http.ServeMux) {
	pr.api.RegisterRoutes(mux)
	mux.HandleFunc("/", pr.proxyHandler)
	mux.Handle("GET /metrics", promhttp.HandlerFor(pr.metricsRegistry, promhttp.HandlerOpts{}))
}

func (pr *ProviderRouter) Handler() *http.ServeMux {
	return pr.lifecycle.Handler()
}

func (pr *ProviderRouter) ProviderNames() []string { return pr.pm.ProviderNames() }
func (pr *ProviderRouter) LookupProvider(name string) *ProviderState {
	return pr.pm.LookupProvider(name)
}
func (pr *ProviderRouter) FirstProvider() *ProviderState                   { return pr.pm.FirstProvider() }
func (pr *ProviderRouter) ForEach(fn func(name string, ps *ProviderState)) { pr.pm.ForEach(fn) }
func (pr *ProviderRouter) Provider(name string) *ProviderState             { return pr.pm.Provider(name) }

func (pr *ProviderRouter) Metrics() *akswitchmetrics.Metrics { return pr.metrics }
func (pr *ProviderRouter) LogManager() *LogManager           { return pr.logManager }

func (pr *ProviderRouter) StartBackgroundTasks() {
	pr.pm.ForEach(func(name string, ps *ProviderState) {
		p := ps
		pr.taskManager.StartKeyPoolMetrics(p.Pool, p.Name)
		pr.taskManager.StartHealthCheck(p.Config, p.Proxy, p)
		if p.Config.GenaiModel != "" {
			interval := time.Duration(p.Config.CalibrationIntervalSec) * time.Second
			pr.taskManager.StartCalibrator(pr.calibrator, p.Pool, p.Config.TargetBase, p.Config.GenaiModel, interval)
		}
	})
	pr.taskManager.StartUptimeTicker(time.Now())
}

func (pr *ProviderRouter) extractProvider(path string) (string, string) {
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
