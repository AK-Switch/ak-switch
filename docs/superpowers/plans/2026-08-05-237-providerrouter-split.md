# ProviderRouter 职责拆分实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 ProviderRouter 从 God Object 拆分为 ProviderManager + AdminAPI + ServerLifecycle + 精简 ProviderRouter 四个模块

**Architecture:** 通过 ProviderLookup 接口定义 AdminAPI 对 provider 数据的窄依赖；ProviderRouter 实现该接口并持有三个子模块的引用；外部 API（server_launcher.go 调用的方法）签名完全不变。

**Tech Stack:** Go (same project), 纯重构（不改变外部行为）

## Global Constraints

- 外部 API 不变：server_launcher.go 调用的方法签名和调用方不动
- 所有代码在 `internal/server/` 包内进行拆分
- 测试标签不变（`//go:build unit`）
- 使用 tab 缩进，gofmt 格式
- 每次任务完成后运行相关测试验证

## 文件结构

```
internal/server/
  provider_manager.go   (NEW)    — ProviderManager：provider 增删查改 + reload
  admin_api.go          (NEW)    — ProviderLookup 接口 + AdminAPI：管理 handler
  provider_lookup.go    (MODIFY) — ProviderRouter 实现 ProviderLookup 接口（委托给 ProviderManager）
  admin.go              (MODIFY) — handler 迁移到 AdminAPI.RegisterRoutes，仅保留方法体
  lifecycle.go          (MODIFY) — 提取 ServerLifecycle，保留 BackgroundTaskManager 等独立函数
  router.go             (MODIFY) — ProviderRouter 精简为外部门面，持有 3 个子模块引用
  admin_test.go         (MODIFY) — handler 访问路径调整为 pr.adminAPI.xxxHandler
  server_launcher.go    (UNCHANGED) — 不修改
  server_launcher_test.go (UNCHANGED) — 不修改
```

---

### Task 1: 创建 provider_manager.go

**Files:**
- Create: `internal/server/provider_manager.go`
- Test: `internal/server/admin_test.go` (existing, no changes yet)

**Interfaces:**
- Consumes: `config.Config`, `keypool.KeyPool`, `ProviderState`, `LogManager`
- Produces: `ProviderManager` struct + methods

- [ ] **Step 1: 创建 provider_manager.go**

创建 `ProviderManager` 结构体，包含原来在 `ProviderRouter` 中的 provider 状态管理逻辑：

```go
package server

import (
	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"log/slog"
	"sync"
)

// ProviderManager manages the lifecycle and state of all providers.
type ProviderManager struct {
	mu          sync.RWMutex
	providers   map[string]*ProviderState
	startTime   time.Time
	dashboardHTML string
}

// NewProviderManager creates a new ProviderManager.
func NewProviderManager(dashboardHTML string) *ProviderManager {
	return &ProviderManager{
		providers:     make(map[string]*ProviderState),
		startTime:     time.Now(),
		dashboardHTML: dashboardHTML,
	}
}

// AddProvider registers a new provider.
func (pm *ProviderManager) AddProvider(name string, cfg *config.Config, pool *keypool.KeyPool) error {
	ps := NewProviderState(name, cfg, pool, pm.dashboardHTML, cfg.KeysFile)
	pm.mu.Lock()
	pm.providers[name] = ps
	pm.mu.Unlock()
	return nil
}

// Provider returns the ProviderState with the given name.
func (pm *ProviderManager) Provider(name string) *ProviderState {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.providers[name]
}

// ProviderNames returns sorted provider names.
func (pm *ProviderManager) ProviderNames() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	names := make([]string, 0, len(pm.providers))
	for name := range pm.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LookupProvider returns a provider by name.
func (pm *ProviderManager) LookupProvider(name string) *ProviderState {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.providers[name]
}

// FirstProvider returns the first (alphabetically) provider.
func (pm *ProviderManager) FirstProvider() *ProviderState {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, ps := range pm.providers {
		return ps
	}
	return nil
}

// ForEach iterates over all providers.
func (pm *ProviderManager) ForEach(fn func(name string, ps *ProviderState)) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for name, ps := range pm.providers {
		fn(name, ps)
	}
}

// ReloadConfig updates providers from new config, preserving disabled state.
func (pm *ProviderManager) ReloadConfig(providers map[string]*config.Config, logManager *LogManager) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for name, cfg := range providers {
		keys, keyNames := loadKeysFromConfig(name, cfg)
		if len(keys) > 0 {
			cfg.Keys = keys
			cfg.KeyNames = keyNames
		}

		if existing, ok := pm.providers[name]; ok {
			oldPool := existing.Pool
			var disabledNames []string
			for i := 0; i < oldPool.Len(); i++ {
				if oldPool.IsDisabled(i) {
					n, _ := oldPool.Name(i)
					disabledNames = append(disabledNames, n)
				}
			}
			existing.Config = cfg
			existing.Pool = keypool.NewKeyPool(cfg.Keys, cfg.KeyNames)
			existing.Pool.ConfigureCBs(
				time.Duration(cfg.CooldownSec)*time.Second,
				time.Duration(cfg.BackoffCapSec)*time.Second,
				cfg.BackoffMultiplier,
			)
			for _, dn := range disabledNames {
				for i := 0; i < existing.Pool.Len(); i++ {
					n, _ := existing.Pool.Name(i)
					if n == dn {
						_ = existing.Pool.Disable(i)
					}
				}
			}
			logManager.ApplyLevel(cfg.LogLevel)
		} else {
			pool := keypool.NewKeyPool(cfg.Keys, cfg.KeyNames)
			ps := NewProviderState(name, cfg, pool, pm.dashboardHTML, cfg.KeysFile)
			logManager.ApplyLevel(cfg.LogLevel)
			pm.providers[name] = ps
		}
	}
	slog.Info("config reloaded", "providers", len(pm.providers))
}
```

- [ ] **Step 2: 运行现有测试，确认无回归**

Run: `go test -tags=unit -count=1 ./internal/server/`
Expected: PASS（新文件不影响现有代码）

- [ ] **Step 3: Commit**

```bash
git add internal/server/provider_manager.go
git commit -m "refactor: extract ProviderManager from ProviderRouter (#237)"
```

---

### Task 2: 创建 admin_api.go

**Files:**
- Create: `internal/server/admin_api.go`
- Modify: `internal/server/admin.go` (move handler bodies to AdminAPI)
- Modify: `internal/server/provider_lookup.go` (move checkAdminToken/checkAnyAdminToken)

**Interfaces:**
- Consumes: `ProviderLookup` interface, `LogManager`, config/keypool packages
- Produces: `ProviderLookup` interface, `AdminAPI` struct + `RegisterRoutes`

- [ ] **Step 1: 创建 admin_api.go**

定义 `ProviderLookup` 接口和 `AdminAPI` 结构体，迁移所有 admin handler 方法体：

```go
package server

import (
	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/logentry"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ProviderLookup defines the interface AdminAPI needs to access provider data.
type ProviderLookup interface {
	LookupProvider(name string) *ProviderState
	FirstProvider() *ProviderState
	ForEach(fn func(name string, ps *ProviderState))
	ProviderNames() []string
}

// AdminAPI holds all management API handlers.
type AdminAPI struct {
	pm         ProviderLookup
	logManager *LogManager
}

// NewAdminAPI creates a new AdminAPI.
func NewAdminAPI(pm ProviderLookup, logManager *LogManager) *AdminAPI {
	return &AdminAPI{pm: pm, logManager: logManager}
}

// RegisterRoutes registers all admin API endpoints on the mux.
func (api *AdminAPI) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", api.healthHandler)
	mux.HandleFunc("/logs", api.logsHandler)
	mux.HandleFunc("/dashboard", api.dashboardHandler)
	mux.HandleFunc("/clear", api.clearHandler)
	mux.HandleFunc("/api/config", api.configHandler)
	mux.HandleFunc("/api/keys", api.keysHandler)
	mux.HandleFunc("POST /api/keys/{index}/disable", api.disableKeyHandler)
	mux.HandleFunc("POST /api/keys/{index}/enable", api.enableKeyHandler)
	mux.HandleFunc("PUT /api/keys/{index}/cooldown", api.cooldownKeyHandler)
	mux.HandleFunc("DELETE /api/keys/{index}", api.deleteKeyHandler)
	mux.HandleFunc("GET /api/stats", api.statsHandler)
	mux.HandleFunc("POST /api/stats/reset-upstream-cb", api.upstreamCBResetHandler)
	mux.HandleFunc("POST /api/reload", api.reloadHandler)
	mux.HandleFunc("/api/log-level", api.logLevelHandler)
	mux.HandleFunc("/api/runtime-config", api.runtimeConfigHandler)
	mux.HandleFunc("/sw.js", api.swHandler)
}

// keyOperationHandler creates a handler for key operations.
func (api *AdminAPI) keyOperationHandler(operation func(*keypool.KeyPool, *config.Config, int) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ps, errMsg := api.resolveProvider(r)
		if ps == nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
			return
		}
		if !api.checkAdminToken(w, r, ps.Name) {
			return
		}
		idx, err := parseKeyIndex(r)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		if idx >= len(ps.Pool.Keys()) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
			return
		}
		if err := operation(ps.Pool, ps.Config, idx); err != nil {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		ps.PersistKeys()
		respondJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}
```

将 `admin.go` 和 `provider_lookup.go` 中的所有 handler 方法体迁移到 `admin_api.go`，将 `pr.` 替换为 `api.`，将 `pr.mu`/`pr.providers` 的访问替换为 `api.pm` 接口方法调用。

- [ ] **Step 2: 简化 admin.go — 仅保留方法委托**

`admin.go` 中的所有 handler 方法体移到 `admin_api.go` 后，`admin.go` 清空。

- [ ] **Step 3: 简化 provider_lookup.go — 仅保留 resolveProvider**

`checkAdminToken` 和 `checkAnyAdminToken` 移到 `admin_api.go` 后，`provider_lookup.go` 仅保留 `resolveProvider` 函数（被 AdminAPI 使用）。

- [ ] **Step 4: 运行测试**

Run: `go test -tags=unit -count=1 ./internal/server/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin_api.go internal/server/admin.go internal/server/provider_lookup.go
git commit -m "refactor: extract AdminAPI and ProviderLookup interface (#237)"
```

---

### Task 3: 重构 lifecycle.go — 提取 ServerLifecycle

**Files:**
- Modify: `internal/server/lifecycle.go`
- Modify: `internal/server/router.go`

**Interfaces:**
- Consumes: `BackgroundTaskManager`, `ProxyExecutor`, `Calibrator`, `ProviderManager`, `AdminAPI`, `LogManager`
- Produces: `ServerLifecycle` struct

- [ ] **Step 1: 在 lifecycle.go 中创建 ServerLifecycle**

将 `router.go` 中的 Start、StartWithListener、Shutdown、Stop、Handler 迁移到 `ServerLifecycle`：

```go
// ServerLifecycle manages the HTTP server lifecycle (start, shutdown, stop).
type ServerLifecycle struct {
	proxy       *http.Server
	listener    net.Listener
	wg          sync.WaitGroup
	taskManager *BackgroundTaskManager
	mux         *http.ServeMux
	muxOnce     sync.Once
}

func NewServerLifecycle(taskManager *BackgroundTaskManager) *ServerLifecycle {
	return &ServerLifecycle{taskManager: taskManager}
}

func (sl *ServerLifecycle) SetMux(mux *http.ServeMux) {
	sl.mux = mux
}

func (sl *ServerLifecycle) Handler() *http.ServeMux {
	sl.muxOnce.Do(func() {
		if sl.mux == nil {
			sl.mux = http.NewServeMux()
		}
	})
	return sl.mux
}

func (sl *ServerLifecycle) Start(host string, port int) error { ... }
func (sl *ServerLifecycle) StartWithListener(listener net.Listener) error { ... }
func (sl *ServerLifecycle) Shutdown(ctx context.Context) { ... }
func (sl *ServerLifecycle) Stop() { ... }
```

`BackgroundTaskManager`、`ActiveHealthCheck`、`StartupKeyProbe`、`PeriodicCalibrator` 等独立函数保留在 `lifecycle.go` 不变。

- [ ] **Step 2: 运行测试**

Run: `go test -tags=unit -count=1 ./internal/server/`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/server/lifecycle.go internal/server/router.go
git commit -m "refactor: extract ServerLifecycle from ProviderRouter (#237)"
```

---

### Task 4: 重构 router.go — ProviderRouter 精简为外部门面

**Files:**
- Modify: `internal/server/router.go`

**Interfaces:**
- Consumes: `ProviderManager`, `AdminAPI`, `ServerLifecycle`, `LogManager`, `Metrics`, `Calibrator`, `ProxyExecutor`
- Produces: 精简的 `ProviderRouter` 结构体

- [ ] **Step 1: 重写 ProviderRouter 为外部门面**

```go
type ProviderRouter struct {
	pm              *ProviderManager
	adminAPI        *AdminAPI
	lifecycle       *ServerLifecycle
	logManager      *LogManager
	metrics         *akswitchmetrics.Metrics
	metricsRegistry *prometheus.Registry
	calibrator      *tracker.Calibrator
	proxyExecutor   *ProxyExecutor
}

func NewProviderRouter(dashboardHTML string) *ProviderRouter {
	reg, m := akswitchmetrics.NewRegistry()
	pm := NewProviderManager(dashboardHTML)
	calibrator := tracker.NewCalibrator(15)
	logManager := NewLogManager()
	taskManager := NewBackgroundTaskManager(m)
	lifecycle := NewServerLifecycle(taskManager)
	proxyExecutor := NewProxyExecutor(m, calibrator)
	adminAPI := NewAdminAPI(pm, logManager)

	pr := &ProviderRouter{
		pm:              pm,
		adminAPI:        adminAPI,
		lifecycle:       lifecycle,
		logManager:      logManager,
		metrics:         m,
		metricsRegistry: reg,
		calibrator:      calibrator,
		proxyExecutor:   proxyExecutor,
	}
	lifecycle.SetMux(pr.Handler())
	return pr
}

// 委托方法
func (pr *ProviderRouter) AddProvider(name string, cfg *config.Config, pool *keypool.KeyPool) error {
	return pr.pm.AddProvider(name, cfg, pool)
}
func (pr *ProviderRouter) Provider(name string) *ProviderState {
	return pr.pm.Provider(name)
}
func (pr *ProviderRouter) ProviderNames() []string {
	return pr.pm.ProviderNames()
}
func (pr *ProviderRouter) Handler() *http.ServeMux {
	return pr.lifecycle.Handler()
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
func (pr *ProviderRouter) Metrics() *akswitchmetrics.Metrics {
	return pr.metrics
}
func (pr *ProviderRouter) LogManager() *LogManager {
	return pr.logManager
}

// registerRoutes — 路由编排，保留在 ProviderRouter
func (pr *ProviderRouter) registerRoutes(mux *http.ServeMux) {
	pr.adminAPI.RegisterRoutes(mux)
	mux.HandleFunc("/", pr.proxyHandler)
	mux.Handle("GET /metrics", promhttp.HandlerFor(pr.metricsRegistry, promhttp.HandlerOpts{}))
}

// StartBackgroundTasks — 任务编排，保留在 ProviderRouter
func (pr *ProviderRouter) StartBackgroundTasks() {
	pr.pm.ForEach(func(name string, ps *ProviderState) {
		pr.taskManager.StartKeyPoolMetrics(ps.Pool, name)
		pr.taskManager.StartHealthCheck(ps.Config, ps.Proxy, ps)
		if ps.Config.GenaiModel != "" {
			interval := time.Duration(ps.Config.CalibrationIntervalSec) * time.Second
			pr.taskManager.StartCalibrator(pr.calibrator, ps.Pool, ps.Config.TargetBase, ps.Config.GenaiModel, interval)
		}
	})
	pr.taskManager.StartUptimeTicker(pr.startTime)
}

// proxyHandler, extractProvider — 保留在 ProviderRouter
```

- [ ] **Step 2: 运行测试**

Run: `go test -tags=unit -count=1 ./internal/server/`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/server/router.go
git commit -m "refactor: ProviderRouter as thin facade over ProviderManager, AdminAPI, ServerLifecycle (#237)"
```

---

### Task 5: 更新 admin_test.go — 调整 handler 访问路径

**Files:**
- Modify: `internal/server/admin_test.go`

**Interfaces:**
- Consumes: 现有的 `ProviderRouter` 测试 helper
- Produces: 更新后的测试代码

- [ ] **Step 1: 更新 handler 访问路径**

测试中的 `pr.keyOperationHandler(...)` 改为 `pr.adminAPI.keyOperationHandler(...)`：

```go
// 旧: handler := pr.keyOperationHandler(func(...) { ... })
// 新: handler := pr.adminAPI.keyOperationHandler(func(...) { ... })
```

`newTestRouterWithKeys` 和 `NewProviderRouter` 的调用不变（外部门面 API 不变）。

- [ ] **Step 2: 运行测试**

Run: `go test -tags=unit -count=1 ./internal/server/`
Expected: PASS

- [ ] **Step 3: 运行全量测试**

Run: `make test-all`
Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/server/admin_test.go
git commit -m "test: update admin_test.go handler access paths for #237 split"
```

---

### Task 6: 验证和清理

- [ ] **Step 1: 运行全量测试**

```bash
make test-unit && make test-integration
```
Expected: 全部 PASS

- [ ] **Step 2: gofmt 检查**

```bash
gofmt -w internal/server/provider_manager.go internal/server/admin_api.go internal/server/lifecycle.go internal/server/router.go internal/server/admin.go internal/server/provider_lookup.go
```

- [ ] **Step 3: 检查未使用的 import/variable**

```bash
go vet ./internal/server/
```
Expected: 无 vet 警告

- [ ] **Step 4: 确认外部 API 不变**

检查 `server_launcher.go` 和 `server_launcher_test.go` 无需修改。

- [ ] **Step 5: Commit**

```bash
git add internal/server/provider_manager.go internal/server/admin_api.go
git add internal/server/lifecycle.go internal/server/router.go
git add internal/server/admin.go internal/server/provider_lookup.go internal/server/admin_test.go
git commit -m "refactor: split ProviderRouter into 4 modules (#237)"
```

---

## 测试验证清单

- [ ] `go test -tags=unit -count=1 ./internal/server/` — PASS
- [ ] `go test -tags=integration -count=1 ./test/integration/` — PASS
- [ ] `make test-all` — PASS
- [ ] `go vet ./internal/server/` — 无警告
- [ ] `gofmt -l internal/server/*.go` — 无输出（全部格式化）
