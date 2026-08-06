# Candidate 4 — 合并 ProxyEngine 到 ProviderState

**Issue:** #268
**前置条件:** PR #270 已合入（descriptor table + ProviderState getters/setters/proxy methods）
**改动量:** 4 个文件修改，1 个文件删除

---

## 问题

`ProxyEngine` 是一个只有 2 个字段的薄包装：

```go
// internal/server/proxy.go
type ProxyEngine struct {
    client *http.Client
    upCB   *circuitbreaker.CircuitBreaker
}
```

它的存在理由是什么？没有。构造函数 `NewProxyEngine` 做实际初始化工作，但完全可以内联到 `NewProviderState` 中。`RouteEntry` 类型在同一文件中但从未被引用。

`provider_lookup.go` 只有 2 行（`package server`），是永远的占位文件。

**影响范围（`ps.proxy.*` 调用点）：**

| 文件 | 行号 | 当前写法 |
|------|------|----------|
| `proxy_executor.go` | 39-41 | `ps.proxy.client`, `ps.proxy.upCB` 局部别名 |
| `proxy_executor.go` | 52 | `ps.config`（不受影响） |
| `lifecycle.go` | 210 | `proxy.upCB` |
| `router.go` | 199 | `p.proxy` 传给 `StartHealthCheck` |
| `router.go` | 104-115 | proxy proxy methods（`ps.proxy.client`, `ps.proxy.upCB`） |

---

## 设计

### 1. ProviderState 增加字段

在 `router.go` 的 `ProviderState` 结构体中，替换 `proxy *ProxyEngine` 为：

```go
type ProviderState struct {
    name          string
    config        *config.Config
    pool          *keypool.KeyPool
    client        *http.Client
    upCB          *circuitbreaker.CircuitBreaker
    healthMu      sync.RWMutex
    lastCheckTime time.Time
    lastCheckOK   bool
    dashboardHTML string
    keysFile      string
}
```

### 2. NewProviderState 内联初始化

删除 `NewProxyEngine` 调用，直接初始化 `client` 和 `upCB`：

```go
func NewProviderState(name string, cfg *config.Config, pool *keypool.KeyPool, dash, keysFile string) *ProviderState {
    upCB := circuitbreaker.New(cfg.UpstreamCBThreshold,
        time.Duration(cfg.CBResetSec)*time.Second)
    return &ProviderState{
        name: name, config: cfg, pool: pool,
        client:        &http.Client{Timeout: time.Duration(cfg.HTTPTimeoutSec) * time.Second},
        upCB:          upCB,
        dashboardHTML: dash, keysFile: keysFile,
    }
}
```

> 注意：`http.Client` 和 `circuitbreaker` 的导入已经在 `router.go` 中（`circuitbreaker` 在 line 12，`http` 在 line 7）。

### 3. Proxy proxy methods 更新

`router.go` 中所有 proxy proxy methods 中的 `ps.proxy.client` → `ps.client`，`ps.proxy.upCB` → `ps.upCB`。

### 4. 更新所有调用点

| 文件 | 改动 |
|------|------|
| `proxy_executor.go:39-41` | `ps.proxy.client` → `ps.client`，`ps.proxy.upCB` → `ps.upCB` |
| `lifecycle.go:210` | `proxy.upCB` → `ps.upCB`（`ActiveHealthCheck` 已有 `ps *ProviderState` 参数） |
| `lifecycle.go:140` | `StartHealthCheck` 签名 `proxy *ProxyEngine` → `ps *ProviderState`，内部调用 `ActiveHealthCheck(cfg, ps, ...)` 而不是 `ActiveHealthCheck(cfg, proxy, ...)` |
| `router.go:199` | `StartHealthCheck(p.config, p.proxy, p)` → `StartHealthCheck(p.config, p, p)` |
| `proxy_executor.go` 其他 `ps.proxy.*` | 同上，全部替换 |

### 5. 删除 ProxyEngine 类型和 provider_lookup.go

- `proxy.go`: 删除 `ProxyEngine` 结构体、`NewProxyEngine` 函数、`RouteEntry` 类型。保留 `ErrorCode`、`ErrorCategory`、`categorizeError`、`writeProxyError`。
- `provider_lookup.go`: 删除整个文件。

### 6. 检查 provider_manager.go

确认无 `ProxyEngine` 或 `ps.proxy` 引用。

---

## 行为不变保证

每个 proxy method 的语义不变：

| 方法 | 前 | 后 |
|------|----|----|
| `SetProxyTimeout(d)` | `ps.proxy.client.Timeout = d` | `ps.client.Timeout = d` |
| `ProxyClientTimeout()` | `return ps.proxy.client.Timeout` | `return ps.client.Timeout` |
| `ResetUpstreamCB()` | `ps.proxy.upCB.Reset()` | `ps.upCB.Reset()` |
| `RecordUpstreamFailure()` | `ps.proxy.upCB.RecordFailure()` | `ps.upCB.RecordFailure()` |
| `RecordUpstreamSuccess()` | `ps.proxy.upCB.RecordSuccess()` | `ps.upCB.RecordSuccess()` |
| `UpstreamCBAllow()` | `return ps.proxy.upCB.Allow()` | `return ps.upCB.Allow()` |
| `SetUpstreamCBResetTimeout(sec)` | `ps.proxy.upCB.SetResetTimeout(...)` | `ps.upCB.SetResetTimeout(...)` |

---

## 测试影响

- 所有现有测试不变（无行为变化）
- `proxy_executor_test.go` 中如有 `ps.proxy.*` 直接访问需同步更新
- 运行 `make test-unit` 验证

---

## 实施顺序

1. `router.go` — 改结构体字段 + 改 `NewProviderState` + 改 proxy methods + 改 `StartBackgroundTasks`
2. `proxy_executor.go` — 改 `Execute` 中 3 个局部别名
3. `lifecycle.go` — 改 `StartHealthCheck` 签名 + `ActiveHealthCheck` 中 `proxy.upCB` → `ps.upCB`
4. `proxy.go` — 删除 `ProxyEngine` 类型、`NewProxyEngine`、`RouteEntry`
5. 删除 `provider_lookup.go`
6. `go build ./...` + `make test-unit` 验证
7. 搜索确认无残留引用：`grep -r "ProxyEngine\|ps\.proxy\|\.proxy\." internal/server/`
