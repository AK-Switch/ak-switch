# Candidate 2 — ProviderState 封装方法

## 问题

`ProviderState` 的四个导出字段（`Name`、`Config`、`Pool`、`Proxy`）被 8 个文件直接读写：

```
router.go        — 启动时读 Config.GenaiModel, Config.CalibrationIntervalSec
provider_manager.go — ReloadConfig 读写 Pool, Config
proxy_executor.go   — Execute, handleRateLimited, handleAuthRejected 等读写 Config, Pool, Proxy
lifecycle.go        — ActiveHealthCheck 读 Name
admin_api.go        — 最密集：configHandler, keysHandler, healthHandler,
                      statsHandler, keyOperationHandler, checkAdminToken,
                      getRuntimeParams, runtimeConfigField descriptors
admin_test.go       — 测试辅助函数读写 Config.*, Pool.*
proxy_executor_test.go — 测试读写 Config, Pool
```

ProviderState 是包内类型（未导出），外部无法直接访问。但包内直接字段访问造成：

- **变更阻力**：未来要给 Config 加 lazy init、给 Pool 做 swap、给 Proxy 做 reconnect，必须改所有调用点
- **不变量无法集中**：`AdminToken` 明文暴露，敏感字段没有访问控制
- **代码扫描困难**：grep `ps\.` 返回 100+ 结果，分不清"合法访问"和"穿透实现"

## 方案

为 ProviderState 添加方法接口，逐步将字段改为小写。分三层实施，每层可独立提交。

### 原则

- **向后兼容**：不删除字段，只加方法。旧字段保留到所有调用点迁移完毕后再移除（或保留为内部兼容字段）
- **方法即接口**：每个方法只暴露"调用者需要什么"，不暴露底层存储细节
- **敏感字段保护**：`Config.AdminToken` 只通过 `CheckAdminToken(token string) bool` 访问，不暴露原始值

### 第一层：Name

`Name` 是纯只读字符串，无副作用。

```go
func (ps *ProviderState) Name() string { return ps.name }
```

调用点变更：`ps.Name` → `ps.Name()`（2 处）

### 第二层：Config

Config 是最复杂的字段，按读写分两组。

**只读 getter（Config 值不可直接替换，只改字段）：**

| 方法 | 读取字段 | 当前调用点 |
|------|---------|-----------|
| `HTTPTimeoutSec() time.Duration` | `Config.HTTPTimeoutSec` | admin_api.go getRuntimeParams, descriptor |
| `MaxRetries() int` | `Config.MaxRetries` | admin_api.go getRuntimeParams, descriptor |
| `CooldownSec() int` | `Config.CooldownSec` | admin_api.go getRuntimeParams, descriptor |
| `BackoffCapSec() int` | `Config.BackoffCapSec` | admin_api.go getRuntimeParams, descriptor |
| `BackoffMultiplier() float64` | `Config.BackoffMultiplier` | admin_api.go getRuntimeParams, descriptor |
| `CBResetSec() int` | `Config.CBResetSec` | admin_api.go getRuntimeParams, descriptor |
| `UpstreamCBThreshold() int` | `Config.UpstreamCBThreshold` | admin_api.go getRuntimeParams, descriptor |
| `LogLevel() string` | `Config.LogLevel` | admin_api.go getRuntimeParams, descriptor |
| `GenaiModel() string` | `Config.GenaiModel` | router.go 启动探针 |
| `CalibrationIntervalSec() int` | `Config.CalibrationIntervalSec` | router.go 启动探针 |
| `AdminToken() string` | `Config.AdminToken` | admin_api.go checkAdminToken |
| `TargetBase() float64` | `Config.TargetBase` | admin_api.go configHandler |

**写 setter（runtime config 热更新路径）：**

| 方法 | 写入字段 | 备注 |
|------|---------|------|
| `SetHTTPTimeoutSec(v time.Duration)` | `Config.HTTPTimeoutSec` | |
| `SetMaxRetries(v int)` | `Config.MaxRetries` | |
| `SetCooldownSec(v int)` | `Config.CooldownSec` | |
| `SetBackoffCapSec(v int)` | `Config.BackoffCapSec` | |
| `SetBackoffMultiplier(v float64)` | `Config.BackoffMultiplier` | |
| `SetCBResetSec(v int)` | `Config.CBResetSec` | |
| `SetUpstreamCBThreshold(v int)` | `Config.UpstreamCBThreshold` | |
| `SetLogLevel(v string)` | `Config.LogLevel` | |

**敏感方法：**

| 方法 | 行为 | 替代 |
|------|------|------|
| `CheckAdminToken(token string) bool` | 比较 token，返回 bool | 替代 `ps.Config.AdminToken == token` |

### 第三层：Pool 和 Proxy

Pool 和 Proxy 是复杂类型，ProviderState 不暴露其内部结构，只代理高频操作。

**Pool 代理方法：**

| 方法 | 委托 | 替代直接访问 |
|------|------|-------------|
| `PoolKeys() []string` | `ps.Pool.Keys()` | `ps.Pool.Keys()` |
| `PoolActiveCount() int` | `ps.Pool.ActiveCount()` | `ps.Pool.ActiveCount()` |
| `PoolCoolingCount() int` | `ps.Pool.CoolingCount()` | `ps.Pool.CoolingCount()` |
| `PoolDisabledCount() int` | `ps.Pool.DisabledCount()` | `ps.Pool.DisabledCount()` |
| `PoolName(i int) string` | `ps.Pool.Name(i)` | `ps.Pool.Name(i)` |
| `PoolKeyStatusLabel(i int) string` | `ps.Pool.KeyStatusLabel(i)` | `ps.Pool.KeyStatusLabel(i)` |
| `PoolRequestsInLastMinute(i int) int64` | `ps.Pool.RequestsInLastMinute(i)` | `ps.Pool.RequestsInLastMinute(i)` |
| `PoolCleanupOldRequests() int` | `ps.Pool.CleanupOldRequests()` | `ps.Pool.CleanupOldRequests()` |
| `PoolCB(i int) *circuitbreaker.CircuitBreaker` | `ps.Pool.CB(i)` | `ps.Pool.CB(i)` |
| `PoolIsDisabled(i int) bool` | `ps.Pool.IsDisabled(i)` | `ps.Pool.IsDisabled(i)` |
| `PoolLen() int` | `ps.Pool.Len()` | `ps.Pool.Len()` |
| `ConfigurePoolCBs(cooldown, backoffCap int, multiplier float64)` | `ps.Pool.ConfigureCBs(...)` | 组合操作，减少外部了解 Pool 内部参数 |

**Proxy 代理方法：**

| 方法 | 委托 | 替代直接访问 |
|------|------|-------------|
| `SetProxyTimeout(d time.Duration)` | `ps.Proxy.client.Timeout = d` | `ps.Proxy.client.Timeout = ...` |
| `ResetUpstreamCB()` | `ps.Proxy.upCB.Reset()` | `ps.Proxy.upCB.Reset()` |
| `SetUpstreamCBResetTimeout(sec int)` | `ps.Proxy.upCB.SetResetTimeout(sec)` | `ps.Proxy.upCB.SetResetTimeout(...)` |
| `SetUpstreamCBThreshold(n int)` | `ps.Proxy.upCB.SetThreshold(n)` | `ps.Proxy.upCB.SetThreshold(...)` |
| `UpstreamCBState() circuitbreaker.State` | `ps.Proxy.upCB.State()` | `ps.Proxy.upCB.State()` |

### 与 Candidate 1 的结合

Candidate 1 的 `runtimeConfigField` descriptor 中，apply 闭包当前直接访问 `ps.Config.X` 和 `ps.Proxy.client.Timeout` 等。封装后改为调用方法：

```go
// 之前
apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
    ps.Config.HTTPTimeoutSec = toInt(raw)
    ps.Proxy.client.Timeout = time.Duration(ps.Config.HTTPTimeoutSec) * time.Second
    return ps.Config.HTTPTimeoutSec, nil
},

// 之后
apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
    v := toInt(raw)
    ps.SetHTTPTimeoutSec(v)
    return v, nil
},
```

## 实施顺序

三层各自独立，可以按顺序提交：

1. **Name + Config getters** — 最小改动，只有读取侧
2. **Config setters + AdminToken 保护** — runtime config 路径迁移
3. **Pool/Proxy 代理方法** — 最大改动面，但每个调用点都是机械替换

每层完成后运行 `make test-all` 验证零行为变更。

## 范围

- **修改文件（8 个）**：
  - `internal/server/router.go` — ProviderState 字段改小写 + 方法定义
  - `internal/server/provider_manager.go` — ReloadConfig 改用方法
  - `internal/server/proxy_executor.go` — Execute 等改用方法
  - `internal/server/lifecycle.go` — ActiveHealthCheck 改用 Name()
  - `internal/server/admin_api.go` — 所有直接字段访问改方法调用
  - `internal/server/admin_test.go` — 测试辅助函数改方法
  - `internal/server/proxy_executor_test.go` — 测试改方法
  - `internal/server/admin_api.go` — runtimeConfigField descriptor 迁移（与 Candidate 1 结合）

- **不修改**：`config.Config` 结构体本身、`keypool.KeyPool`、`ProxyEngine`、外部包
- **零行为变更**：所有方法只是字段访问的转发层
