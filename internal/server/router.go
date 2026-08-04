package server

import (
	"context"
	"fmt"
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
	proxy    *http.Server
	listener net.Listener

	pm              *ProviderManager
	api             *AdminAPI
	logManager      *LogManager
	metrics         *akswitchmetrics.Metrics
	metricsRegistry *prometheus.Registry
	calibrator      *tracker.Calibrator
	taskManager     *BackgroundTaskManager
	proxyExecutor   *ProxyExecutor

	wg      sync.WaitGroup
	mux     *http.ServeMux
	muxOnce sync.Once

	disableKeyHandler  http.HandlerFunc
	enableKeyHandler   http.HandlerFunc
	cooldownKeyHandler http.HandlerFunc
	deleteKeyHandler   http.HandlerFunc
}

func NewProviderRouter(dashboardHTML string) *ProviderRouter {
	reg, m := akswitchmetrics.NewRegistry()
	pm := NewProviderManager(dashboardHTML)
	cal := tracker.NewCalibrator(15)
	lm := NewLogManager()
	tm := NewBackgroundTaskManager(m)
	pe := NewProxyExecutor(m, cal)
	api := NewAdminAPI(pm, lm, dashboardHTML, time.Now())

	pr := &ProviderRouter{
		pm: pm, api: api, logManager: lm,
		metrics: m, metricsRegistry: reg, calibrator: cal,
		taskManager: tm, proxyExecutor: pe,
	}
	pr.disableKeyHandler = api.keyOperationHandler(func(p *keypool.KeyPool, _ *config.Config, i int) error { return p.Disable(i) })
	pr.enableKeyHandler = api.keyOperationHandler(func(p *keypool.KeyPool, _ *config.Config, i int) error { return p.Enable(i) })
	pr.cooldownKeyHandler = api.keyOperationHandler(func(p *keypool.KeyPool, c *config.Config, i int) error {
		return p.Cooldown(i, time.Duration(c.CooldownSec)*time.Second)
	})
	pr.deleteKeyHandler = api.keyOperationHandler(func(p *keypool.KeyPool, _ *config.Config, i int) error { return p.RemoveKey(i) })
	return pr
}

func (pr *ProviderRouter) AddProvider(name string, cfg *config.Config, pool *keypool.KeyPool) error {
	return pr.pm.AddProvider(name, cfg, pool)
}

func (pr *ProviderRouter) Start(host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	mux := pr.Handler()
	proxy := &http.Server{Handler: mux}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind failed: %w", err)
	}
	pr.listener = listener
	pr.proxy = proxy
	pr.wg.Add(1)
	go func() {
		defer pr.wg.Done()
		slog.Info("server started", "addr", listener.Addr().String())
		if err := pr.proxy.Serve(listener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()
	return nil
}

func (pr *ProviderRouter) StartWithListener(listener net.Listener) error {
	mux := pr.Handler()
	pr.listener = listener
	pr.proxy = &http.Server{Handler: mux}
	pr.wg.Add(1)
	go func() {
		defer pr.wg.Done()
		slog.Info("server started", "addr", listener.Addr().String())
		if err := pr.proxy.Serve(listener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()
	return nil
}

func (pr *ProviderRouter) registerRoutes(mux *http.ServeMux) {
	pr.api.RegisterRoutes(mux)
	mux.HandleFunc("/", pr.proxyHandler)
	mux.Handle("GET /metrics", promhttp.HandlerFor(pr.metricsRegistry, promhttp.HandlerOpts{}))
}

func (pr *ProviderRouter) Handler() *http.ServeMux {
	pr.muxOnce.Do(func() {
		mux := http.NewServeMux()
		pr.registerRoutes(mux)
		pr.mux = mux
	})
	return pr.mux
}

func (pr *ProviderRouter) Shutdown(ctx context.Context) {
	if pr.proxy != nil {
		if err := pr.proxy.Shutdown(ctx); err != nil {
			slog.Error("shutdown error", "error", err)
		} else {
			slog.Info("server shut down")
		}
	}
}

func (pr *ProviderRouter) Stop() {
	pr.taskManager.Stop()
	pr.wg.Wait()
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
