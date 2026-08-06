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
	name          string
	config        *config.Config
	pool          *keypool.KeyPool
	proxy         *ProxyEngine
	healthMu      sync.RWMutex
	lastCheckTime time.Time
	lastCheckOK   bool
	dashboardHTML string
	keysFile      string
}

func NewProviderState(name string, cfg *config.Config, pool *keypool.KeyPool, dash, keysFile string) *ProviderState {
	return &ProviderState{
		name: name, config: cfg, pool: pool,
		proxy:        NewProxyEngine(cfg, pool),
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
	keys := ps.pool.Keys()
	entries := make([]keypool.KeyEntry, len(keys))
	for i := range keys {
		n, _ := ps.pool.Name(i)
		entries[i] = keypool.KeyEntry{Key: keys[i], Name: n, Disabled: ps.pool.IsDisabled(i)}
	}
	if err := keypool.SaveKeys(ps.name, &keypool.KeyStore{Keys: entries}); err != nil {
		slog.Error("persist keys failed", "provider", ps.name, "error", err)
	}
}

func (ps *ProviderState) Name() string                { return ps.name }
func (ps *ProviderState) HTTPTimeoutSec() int             { return ps.config.HTTPTimeoutSec }
func (ps *ProviderState) MaxRetries() int             { return ps.config.MaxRetries }
func (ps *ProviderState) CooldownSec() int            { return ps.config.CooldownSec }
func (ps *ProviderState) BackoffCapSec() int          { return ps.config.BackoffCapSec }
func (ps *ProviderState) BackoffMultiplier() float64  { return ps.config.BackoffMultiplier }
func (ps *ProviderState) CBResetSec() int             { return ps.config.CBResetSec }
func (ps *ProviderState) UpstreamCBThreshold() int    { return ps.config.UpstreamCBThreshold }
func (ps *ProviderState) LogLevel() string            { return ps.config.LogLevel }
func (ps *ProviderState) SetHTTPTimeoutSec(v int)   { ps.config.HTTPTimeoutSec = v }
func (ps *ProviderState) SetMaxRetries(v int)                 { ps.config.MaxRetries = v }
func (ps *ProviderState) SetCooldownSec(v int)                { ps.config.CooldownSec = v }
func (ps *ProviderState) SetBackoffCapSec(v int)              { ps.config.BackoffCapSec = v }
func (ps *ProviderState) SetBackoffMultiplier(v float64)      { ps.config.BackoffMultiplier = v }
func (ps *ProviderState) SetCBResetSec(v int)                 { ps.config.CBResetSec = v }
func (ps *ProviderState) SetUpstreamCBThreshold(v int)        { ps.config.UpstreamCBThreshold = v }
func (ps *ProviderState) SetLogLevel(v string)                { ps.config.LogLevel = v }
func (ps *ProviderState) GenaiModel() string          { return ps.config.GenaiModel }
func (ps *ProviderState) CalibrationIntervalSec() int { return ps.config.CalibrationIntervalSec }
func (ps *ProviderState) TargetBase() string         { return ps.config.TargetBase }

func (ps *ProviderState) HasAdminToken() bool         { return ps.config.AdminToken != "" }
func (ps *ProviderState) CheckAdminToken(token string) bool {
	return ps.config.AdminToken != "" && ps.config.AdminToken == token
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
		pr.taskManager.StartKeyPoolMetrics(p.pool, p.Name())
		pr.taskManager.StartHealthCheck(p.config, p.proxy, p)
		if p.config.GenaiModel != "" {
			interval := time.Duration(p.config.CalibrationIntervalSec) * time.Second
			pr.taskManager.StartCalibrator(pr.calibrator, p.pool, p.config.TargetBase, p.config.GenaiModel, interval)
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
