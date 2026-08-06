# Candidate 5 — 删除未使用的 CircuitBreaker 接口

**Issue:** #268
**前置条件:** PR #270 已合入（Candidate 2 ProviderState 封装）
**改动量:** 1 个文件修改

---

## 问题

`circuitbreaker.CircuitBreaker` 接口定义了但从未用于多态：

```go
// internal/circuitbreaker/circuitbreaker.go:18-33
type CircuitBreaker interface {
    Allow() bool
    RecordFailure() time.Duration
    RecordSuccess()
    State() State
}
```

该接口只有一处使用：`internal/server/router.go:133` 作为 `PoolCB` 的返回类型：

```go
func (ps *ProviderState) PoolCB(i int) circuitbreaker.CircuitBreaker {
    return ps.pool.CB(i)
}
```

调用方 `proxy_executor.go:195` 只用到 `.State()` 方法，且实际接收的是 `*KeyCircuitBreaker` 具体类型。

项目中已有 `*KeyCircuitBreaker` 和 `*UpstreamCircuitBreaker` 两个具体类型，各自直接使用，从未通过接口多态分发。

**影响范围（`CircuitBreaker` 作为类型使用的位置）：**

| 位置 | 用法 |
|------|------|
| `circuitbreaker.go:20` | 接口定义 |
| `router.go:133` | `PoolCB` 返回类型 |
| `proxy_executor.go:195` | `.State()` 调用（实际类型 `*KeyCircuitBreaker`） |

---

## 设计

将 `PoolCB` 返回类型从 `circuitbreaker.CircuitBreaker` 接口改为 `*circuitbreaker.KeyCircuitBreaker` 具体类型，删除 `CircuitBreaker` 接口。

```go
// router.go 修改前
func (ps *ProviderState) PoolCB(i int) circuitbreaker.CircuitBreaker {
    return ps.pool.CB(i)
}

// router.go 修改后
func (ps *ProviderState) PoolCB(i int) *circuitbreaker.KeyCircuitBreaker {
    return ps.pool.CB(i)
}
```

`proxy_executor.go:195` 不变（`.State()` 在具体类型上一样可用）。

---

## 行为不变保证

| 调用方 | 操作 | 变化 |
|--------|------|------|
| `proxy_executor.go:195` | `.State() == circuitbreaker.Permanent` | 无变化（`.State()` 是具体类型方法） |
| 测试 | `PoolCB` 返回值用于 `.State()` 检查 | 返回类型更具体，不影响方法调用 |

---

## 测试影响

- 所有现有测试不变（无行为变化）
- 运行 `make test-unit` 验证

---

## 实施顺序

1. `router.go:133` — 改返回类型
2. `circuitbreaker.go:18-33` — 删除接口定义
3. `go build ./... && make test-unit` 验证
4. `grep -r "CircuitBreaker" internal/ --include="*.go"` 确认无残留
