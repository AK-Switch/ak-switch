# ProviderRouter 职责拆分设计

> Issue #237 | 状态: Draft

## 当前状态

`ProviderRouter` 是一个 God Object，持有 9+ 项职责：

| 职责 | 当前持有方式 | 涉及代码 |
|------|-------------|---------|
| HTTP 路由管理 | ProviderRouter 方法 | registerRoutes, proxyHandler |
| provider 状态管理 | ProviderRouter 字段 | providers map, AddProvider |
| 指标收集 | ProviderRouter 字段 | metrics, metricsRegistry |
| 日志管理 | ProviderRouter 字段 | logManager |
| token 校准 | ProviderRouter 字段 | calibrator |
| 后台任务编排 | ProviderRouter 字段 | taskManager |
| key 操作处理 | ProviderRouter 方法 | keyOperationHandler |
| 运行时配置 | ProviderRouter 方法 | reloadHandler, runtimeConfigHandler |
| 服务器生命周期 | ProviderRouter 方法 | Start, Shutdown, Stop |

涉及 3 个文件：`router.go`（335 行）、`admin.go`（955 行）、`provider_lookup.go`（85 行）。

## 拆分方案

拆分为 4 个模块，通过 ProviderLookup 接口定义依赖方向：

### ProviderManager

职责：provider 的增删查改和解析

```go
type ProviderManager struct { /* unexported fields */ }

func NewProviderManager(dashboardHTML string) *ProviderManager
func (pm *ProviderManager) AddProvider(name string, cfg *config.Config, pool *keypool.KeyPool) error
func (pm *ProviderManager) Provider(name string) *ProviderState
func (pm *ProviderManager) ProviderNames() []string
func (pm *ProviderManager) ReloadConfig(providers map[string]*config.Config, logManager *LogManager)
```

迁移来源：`router.go` 的 AddProvider、Provider、ProviderNames；`provider_lookup.go` 的 lookupProvider、firstProvider、resolveProvider。

### ProviderLookup 接口

定义 AdminAPI 对 provider 数据的窄依赖：

```go
type ProviderLookup interface {
    Lookup(name string) *ProviderState
    FirstProvider() *ProviderState
    ResolveProvider(r *http.Request) (*ProviderState, string)
    ResolveProviderByName(name string) (*ProviderState, string)
    ProviderNames() []string
    ForEach(fn func(name string, ps *ProviderState))
}
```

### AdminAPI

职责：所有管理 API 端点的 HTTP handler

```go
type AdminAPI struct {
    pm         ProviderLookup
    logManager *LogManager
}

func NewAdminAPI(pm ProviderLookup, logManager *LogManager) *AdminAPI
func (api *AdminAPI) RegisterRoutes(mux *http.ServeMux)
func (api *AdminAPI) keyOperationHandler(operation func(*keypool.KeyPool, *config.Config, int) error) http.HandlerFunc
```

迁移来源：`admin.go` 全部 handler + `provider_lookup.go` 的 checkAdminToken、checkAnyAdminToken。

### ServerLifecycle

职责：HTTP 服务器的启动、关闭和路由缓存

```go
type ServerLifecycle struct {
    proxy        *http.Server
    listener     net.Listener
    wg           sync.WaitGroup
    taskManager  *BackgroundTaskManager
    mux          *http.ServeMux
}

func NewServerLifecycle(taskManager *BackgroundTaskManager) *ServerLifecycle
func (sl *ServerLifecycle) SetMux(mux *http.ServeMux)
func (sl *ServerLifecycle) Handler() *http.ServeMux
func (sl *ServerLifecycle) Start(host string, port int) error
func (sl *ServerLifecycle) StartWithListener(listener net.Listener) error
func (sl *ServerLifecycle) Shutdown(ctx context.Context)
func (sl *ServerLifecycle) Stop()
```

迁移来源：`router.go` 的 Start、StartWithListener、Shutdown、Stop、Handler。

### ProviderRouter (精简)

职责：外部门面，持有三个子模块的引用，外部 API 不变

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
```

委托方法：AddProvider、Provider、ProviderNames、Handler、Start、StartWithListener、Shutdown、Stop、Metrics、LogManager。

保留方法：registerRoutes（路由编排）、StartBackgroundTasks（任务编排）、proxyHandler（代理分发）、extractProvider（路径解析）。

## 设计决策

**1. 外部 API 不变。** `server_launcher.go` 调用 `router.Start()`、`router.AddProvider()` 等，这些方法签名和调用方都不动。拆分只在 `internal/server/` 内部进行。

**2. ProviderRouter 实现 ProviderLookup 接口。** 虽然所有代码在同一个包内，但接口明确了 AdminAPI 的依赖边界——AdminAPI 只能通过接口方法访问 provider 数据。

**3. checkAnyAdminToken 迁移到 AdminAPI。** 该方法只被 admin handler 使用，迁移后 AdminAPI 完整自包含。通过 ProviderLookup.ForEach 迭代 provider 数据。

**4. reloadHandler 迁移到 AdminAPI。** 需要访问 ProviderManager（更新/添加 providers）和 LogManager（ApplyLevel）。新增 ProviderManager.ReloadConfig() 方法封装锁和更新逻辑。

**5. keyOperationHandler 迁移到 AdminAPI。** 该方法创建的 handler 需要 resolveProvider、checkAdminToken，都在 AdminAPI 范围内。

**6. StartBackgroundTasks 保留在 ProviderRouter。** 该方法需要访问 calibrator、metrics、pm（providers），是一个编排方法，适合放在外部门面。

**7. registerRoutes 保留在 ProviderRouter。** 负责路由注册的编排：调用 adminAPI.RegisterRoutes(mux) + 注册 proxy catch-all + 注册 metrics。这是路由拓扑的定义，适合在外部门面。

## 测试影响

- 现有 `admin_test.go` 中的测试创建 `ProviderRouter` 并测试其 handler。通过 `NewProviderRouter` 创建的流程不变（内部创建所有子模块）。
- Handler 访问从 `pr.logLevelHandler(w, r)` 变为 `pr.adminAPI.logLevelHandler(w, r)`。测试在同一个包内，可以访问未导出的 adminAPI 字段。
- 新增 AdminAPI 单元测试：可以通过 mock ProviderLookup 接口测试 handler。

## 实施步骤

1. 创建 `provider_manager.go` — ProviderManager struct + 方法
2. 创建 `admin_api.go` — ProviderLookup 接口 + AdminAPI struct
3. 重构 `provider_lookup.go` — ProviderRouter 的接口实现（委托给 ProviderManager）
4. 重构 `admin.go` — 迁移 handler 到 AdminAPI.RegisterRoutes
5. 重构 `lifecycle.go` — 提取 ServerLifecycle
6. 重构 `router.go` — ProviderRouter 作为精简外部门面
7. 更新 `admin_test.go` — 调整 handler 访问路径
8. 全量测试验证
