# Candidate 4 — 合并 ProxyEngine 到 ProviderState Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除 ProxyEngine 薄包装类型，将其 2 个字段（client、upCB）直接合并到 ProviderState，删除空文件 provider_lookup.go

**Architecture:** ProxyEngine 是一个只有 client/upCB 两个字段的包装层，无独立职责。合并后 ProviderState 直接持有这两个字段，proxy proxy methods 的调用路径不变（只是内部字段路径缩短）。无行为变化。

**Tech Stack:** Go 1.23+, internal/server package

## Global Constraints

- 零行为变化 — 每个方法只是缩短字段访问路径
- 提交前运行 `make test-unit` 通过
- 提交前运行 `go build ./...` 通过
- 提交前 grep 确认无残留引用：`grep -r "ProxyEngine" internal/`

---

## File Map

| File | Role | Changes |
|------|------|---------|
| `internal/server/router.go` | ProviderState 定义 + 构造函数 + proxy methods | 改结构体字段、改 NewProviderState、改 proxy methods、改 StartBackgroundTasks |
| `internal/server/proxy_executor.go` | 代理执行逻辑 | 改 Execute 函数中 3 个局部别名 |
| `internal/server/lifecycle.go` | 健康检查生命周期 | 改 StartHealthCheck 签名 + ActiveHealthCheck 中 proxy.upCB → ps.upCB |
| `internal/server/proxy.go` | 代理错误处理 + ProxyEngine 定义 | 删除 ProxyEngine 类型、NewProxyEngine 函数、RouteEntry 类型 |
| `internal/server/provider_lookup.go` | 空文件 | 删除整个文件 |

---

### Task 1: 更新 ProviderState 结构体定义和构造函数

**Files:**
- Modify: `internal/server/router.go:22-40`

**Step 1: 修改 ProviderState 结构体**

将 `proxy *ProxyEngine` 替换为 `client *http.Client` 和 `upCB *circuitbreaker.CircuitBreaker`：

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

**Step 2: 修改 NewProviderState 构造函数**

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

> `circuitbreaker` 和 `http` 的 import 已存在于 `router.go`（line 12 和 line 7），无需添加。

**Step 3: 验证编译**

Run: `go build ./...`
Expected: PASS

**Step 4: 提交**

```bash
git add internal/server/router.go
git commit -m "refactor: replace ProxyEngine field with client and upCB in ProviderState"
```

---

### Task 2: 更新 proxy proxy methods

**Files:**
- Modify: `internal/server/router.go:103-115`

**Step 1: 修改所有 proxy proxy methods**

将 `ps.proxy.client` 替换为 `ps.client`，`ps.proxy.upCB` 替换为 `ps.upCB`：

```go
// Proxy proxy methods — forward to ps.client / ps.upCB
func (ps *ProviderState) SetProxyTimeout(d time.Duration)          { ps.client.Timeout = d }
func (ps *ProviderState) ProxyClientTimeout() time.Duration        { return ps.client.Timeout }
func (ps *ProviderState) ResetUpstreamCB()                         { ps.upCB.Reset() }
func (ps *ProviderState) RecordUpstreamFailure()                   { ps.upCB.RecordFailure() }
func (ps *ProviderState) RecordUpstreamSuccess()                   { ps.upCB.RecordSuccess() }
func (ps *ProviderState) UpstreamCBAllow() bool                    { return ps.upCB.Allow() }
func (ps *ProviderState) SetUpstreamCBResetTimeout(sec int)        { ps.upCB.SetResetTimeout(time.Duration(sec) * time.Second) }

func (ps *ProviderState) UpstreamCBState() circuitbreaker.State    { return ps.upCB.State() }
func (ps *ProviderState) UpstreamCB() circuitbreaker.CircuitBreaker { return ps.upCB }

func (ps *ProviderState) SetUpstreamProxyCBThreshold(n int) { ps.upCB.SetThreshold(n) }
```

**Step 2: 验证编译**

Run: `go build ./...`
Expected: PASS

**Step 3: 提交**

```bash
git add internal/server/router.go
git commit -m "refactor: update proxy methods to use direct client/upCB fields"
```

---

### Task 3: 更新 StartBackgroundTasks 调用

**Files:**
- Modify: `internal/server/router.go:195-206`

**Step 1: 修改 StartBackgroundTasks**

将 `StartHealthCheck` 的第二个参数从 `p.proxy` 改为 `p`（现在第二个参数是 `*ProviderState` 而不是 `*ProxyEngine`）：

```go
func (pr *ProviderRouter) StartBackgroundTasks() {
	pr.pm.ForEach(func(name string, ps *ProviderState) {
		p := ps
		pr.taskManager.StartKeyPoolMetrics(p.pool, p.Name())
		pr.taskManager.StartHealthCheck(p.config, p, p)
		if p.config.GenaiModel != "" {
			interval := time.Duration(p.config.CalibrationIntervalSec) * time.Second
			pr.taskManager.StartCalibrator(pr.calibrator, p.pool, p.config.TargetBase, p.config.GenaiModel, interval)
		}
	})
	pr.taskManager.StartUptimeTicker(time.Now())
}
```

**Step 2: 验证编译**

Run: `go build ./...`
Expected: PASS

**Step 3: 提交**

```bash
git add internal/server/router.go
git commit -m "refactor: pass ProviderState to StartHealthCheck instead of ProxyEngine"
```

---

### Task 4: 更新 proxy_executor.go

**Files:**
- Modify: `internal/server/proxy_executor.go:38-41`

**Step 1: 修改 Execute 函数的局部别名**

```go
func (px *ProxyExecutor) Execute(w http.ResponseWriter, r *http.Request, ps *ProviderState) {
	pool := ps.pool
	client := ps.client
	upCB := ps.upCB
```

**Step 2: 验证编译**

Run: `go build ./...`
Expected: PASS

**Step 3: 提交**

```bash
git add internal/server/proxy_executor.go
git commit -m "refactor: use ps.client and ps.upCB instead of ps.proxy.*"
```

---

### Task 5: 更新 lifecycle.go

**Files:**
- Modify: `internal/server/lifecycle.go:140,196`

**Step 1: 修改 StartHealthCheck 签名**

将 `proxy *ProxyEngine` 改为 `ps *ProviderState`，内部转发时传递 `ps` 而不是 `proxy`：

```go
func (m *BackgroundTaskManager) StartHealthCheck(cfg *config.Config, ps *ProviderState, metrics *akswitchmetrics.Metrics) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ActiveHealthCheck(cfg, ps, metrics, m.stop)
	}()
}
```

**Step 2: 修改 ActiveHealthCheck 签名和内部访问**

将 `proxy *ProxyEngine` 参数移除（已有 `ps *ProviderState`），将 `proxy.upCB` 改为 `ps.upCB`：

```go
func ActiveHealthCheck(cfg *config.Config, ps *ProviderState, metrics *akswitchmetrics.Metrics, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(cfg.HealthCheckIntervalSec) * time.Second)
	defer ticker.Stop()

	hcClient := &http.Client{
		Timeout: time.Duration(cfg.HealthCheckTimeoutSec) * time.Second,
	}

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			target := cfg.TargetBase + cfg.HealthCheckPath
			upCB := ps.upCB

			start := time.Now()
			resp, err := hcClient.Head(target)
			dur := time.Since(start)

			metrics.HealthCheckDuration.WithLabelValues(ps.name).Observe(dur.Seconds())

			if err == nil && resp.StatusCode < 500 {
				_ = resp.Body.Close()
				upCB.RecordSuccess()
				ps.SetLastHealthCheck(true)
				metrics.HealthCheckProbes.WithLabelValues(ps.name, "ok").Inc()
			} else {
				if err == nil {
					_ = resp.Body.Close()
				}
				upCB.RecordFailure()
				ps.SetLastHealthCheck(false)
				metrics.HealthCheckProbes.WithLabelValues(ps.name, "fail").Inc()
			}

			metrics.UpstreamCBState.WithLabelValues(ps.name).Set(float64(upCB.State()))
		}
	}
}
```

**Step 3: 验证编译**

Run: `go build ./...`
Expected: PASS

**Step 4: 提交**

```bash
git add internal/server/lifecycle.go
git commit -m "refactor: use ProviderState for health check instead of ProxyEngine"
```

---

### Task 6: 删除 ProxyEngine 类型和 RouteEntry

**Files:**
- Modify: `internal/server/proxy.go`
- Delete: `internal/server/provider_lookup.go`

**Step 1: 修改 proxy.go**

删除以下代码段（保留 ErrorCode、ErrorCategory、categorizeError、writeProxyError）：

1. `ProxyEngine` 结构体定义
2. `NewProxyEngine` 函数
3. `RouteEntry` 类型

保留文件顶部（line 1-58）：

```go
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// ErrorCode represents a machine-readable error category for proxy responses.
type ErrorCode string

const (
	ErrorBadRequest       ErrorCode = "BAD_REQUEST"
	ErrorUpstreamError    ErrorCode = "UPSTREAM_ERROR"
	ErrorAllKeysInvalid   ErrorCode = "ALL_KEYS_INVALID"
	ErrorExhaustedRetries ErrorCode = "EXHAUSTED_RETRIES"
)

// ErrorCategory represents whether an upstream response is retryable or not.
type ErrorCategory int

const (
	CatUnknown      ErrorCategory = iota
	CatRetryable                  // 可换 Key 重试：5xx、429、网络问题
	CatNonRetryable               // 客户端问题：400/422 等，换 Key 也解决不了
	CatClientAbort                // 客户端主动中断：不污染 Key 健康度
)

// categorizeError classifies an upstream HTTP status code (or network error)
// into an ErrorCategory. NonRetryable codes are returned immediately without
// consuming retry attempts and without penalizing key health.
func categorizeError(statusCode int, err error) ErrorCategory {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return CatClientAbort
		}
		return CatRetryable
	}
	switch statusCode {
	case 400, 405, 406, 413, 414, 415, 422, 501:
		return CatNonRetryable
	default:
		return CatRetryable
	}
}

// writeProxyError writes a JSON error response with the given status code and error code.
func writeProxyError(w http.ResponseWriter, status int, code ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"code":    string(code),
			"message": message,
		},
	})
}
```

**Step 2: 删除 provider_lookup.go**

```bash
git rm internal/server/provider_lookup.go
```

**Step 3: 验证编译**

Run: `go build ./...`
Expected: PASS

**Step 4: 提交**

```bash
git add internal/server/proxy.go
git commit -m "refactor: remove ProxyEngine type and RouteEntry dead code"
```

---

### Task 7: 验证全量测试和清理残留引用

**Step 1: 运行单元测试**

Run: `make test-unit`
Expected: PASS

**Step 2: grep 确认无残留引用**

Run: `grep -r "ProxyEngine\|ps\.proxy\|\.proxy\." internal/`
Expected: 无输出（或仅有注释/文档中的提及）

**Step 3: 提交（如有清理）**

```bash
git add .
git commit -m "chore: verify no remaining ProxyEngine references"
```
