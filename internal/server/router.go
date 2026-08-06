package server

import (
	"context"
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
	"akswitch/internal/tracker"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type ProviderState struct {
	name          string
	config        *config.Config
	pool          *keypool.KeyPool
	client        *http.Client
	upCB          *circuitbreaker.UpstreamCircuitBreaker
	healthMu      sync.RWMutex
	lastCheckTime time.Time
	lastCheckOK   bool
	dashboardHTML string
	keysFile      string
}

func NewProviderState(name string, cfg *config.Config, pool *keypool.KeyPool, dash, keysFile string) *ProviderState {
	backoffCapSec := cfg.BackoffCapSec
	if backoffCapSec <= 0 {
		backoffCapSec = 120
	}
	backoffMult := cfg.BackoffMultiplier
	if backoffMult <= 0 {
		backoffMult = 2
	}
	upstreamThreshold := cfg.UpstreamCBThreshold
	if upstreamThreshold <= 0 {
		upstreamThreshold = 5
	}
	cbResetSec := cfg.CBResetSec
	if cbResetSec <= 0 {
		cbResetSec = 30
	}
	base := time.Duration(cfg.CooldownSec) * time.Second
	cap_ := time.Duration(backoffCapSec) * time.Second
	pool.ConfigureCBs(base, cap_, backoffMult)

	upCB := circuitbreaker.NewUpstreamCircuitBreaker(
		upstreamThreshold,
		time.Duration(cbResetSec)*time.Second,
	)

	return &ProviderState{
		name: name, config: cfg, pool: pool,
		client: &http.Client{
			Timeout: time.Duration(cfg.HTTPTimeoutSec) * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        500,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
				ForceAttemptHTTP2:   true,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		upCB:          upCB,
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
func (ps *ProviderState) SetUpstreamCBThreshold(n int) { ps.config.UpstreamCBThreshold = n }
func (ps *ProviderState) SetLogLevel(v string)                { ps.config.LogLevel = v }
func (ps *ProviderState) GenaiModel() string          { return ps.config.GenaiModel }
func (ps *ProviderState) CalibrationIntervalSec() int { return ps.config.CalibrationIntervalSec }
func (ps *ProviderState) TargetBase() string         { return ps.config.TargetBase }

// Pool proxy methods — forward to ps.pool
func (ps *ProviderState) PoolKeys() []string                          { return ps.pool.Keys() }
func (ps *ProviderState) PoolActiveCount() int                        { return ps.pool.ActiveCount() }
func (ps *ProviderState) PoolCoolingCount() int                       { return ps.pool.CoolingCount() }
func (ps *ProviderState) PoolDisabledCount() int                      { return ps.pool.DisabledCount() }
func (ps *ProviderState) PoolName(i int) (string, error)              { return ps.pool.Name(i) }
func (ps *ProviderState) PoolKeyStatusLabel(i int, now time.Time) string { return ps.pool.KeyStatusLabel(i, now) }
func (ps *ProviderState) PoolRequestsInLastMinute(i int) int               { return ps.pool.RequestsInLastMinute(i) }
func (ps *ProviderState) PoolCleanupOldRequests(i int)                      { ps.pool.CleanupOldRequests(i) }
func (ps *ProviderState) PoolCB(i int) *circuitbreaker.KeyCircuitBreaker { return ps.pool.CB(i) }
func (ps *ProviderState) PoolIsDisabled(i int) bool                   { return ps.pool.IsDisabled(i) }
func (ps *ProviderState) PoolLen() int                                { return ps.pool.Len() }
func (ps *ProviderState) PoolAuthFailCount(idx int) int               { return ps.pool.CB(idx).AuthFailCount() }
func (ps *ProviderState) ConfigurePoolCBs(base, backoffCap time.Duration, multiplier float64) {
	ps.pool.ConfigureCBs(base, backoffCap, multiplier)
}

// Proxy proxy methods — forward to ps.client
func (ps *ProviderState) SetProxyTimeout(d time.Duration)          { ps.client.Timeout = d }
func (ps *ProviderState) ProxyClientTimeout() time.Duration        { return ps.client.Timeout }
func (ps *ProviderState) ResetUpstreamCB()                         { ps.upCB.Reset() }
func (ps *ProviderState) RecordUpstreamFailure()                   { ps.upCB.RecordFailure() }
func (ps *ProviderState) RecordUpstreamSuccess()                   { ps.upCB.RecordSuccess() }
func (ps *ProviderState) UpstreamCBAllow() bool                    { return ps.upCB.Allow() }
func (ps *ProviderState) SetUpstreamCBResetTimeout(sec int)        { ps.upCB.SetResetTimeout(time.Duration(sec) * time.Second) }

func (ps *ProviderState) UpstreamCBState() circuitbreaker.State    { return ps.upCB.State() }
func (ps *ProviderState) UpstreamCB() *circuitbreaker.UpstreamCircuitBreaker { return ps.upCB }

func (ps *ProviderState) SetUpstreamProxyCBThreshold(n int) { ps.upCB.SetThreshold(n) }

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
		pr.taskManager.StartHealthCheck(p.config, p)
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
