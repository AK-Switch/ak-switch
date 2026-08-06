# Candidate 2 — ProviderState Encapsulation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Encapsulate ProviderState's exported fields (Name, Config, Pool, Proxy) behind methods, enabling future internal changes without touching all call sites

**Architecture:** Three independent layers — (1) Name + Config getters, (2) Config setters + AdminToken protection, (3) Pool/Proxy proxy methods. Each layer is a self-contained commit with zero behavior change.

**Tech Stack:** Go 1.23+, internal/server package

## Global Constraints

- Zero behavior change — every method is a field access forwarding layer
- Config setters replace direct writes; getters replace direct reads
- `AdminToken` is NEVER exposed raw — only `CheckAdminToken(token string) bool` and `HasAdminToken() bool`
- All tests pass (`make test-all`) after each task
- Field names change to lowercase (`name`, `config`, `pool`, `proxy`); methods provide the public API

---

## File Map

| File | Role | Changes |
|------|------|---------|
| `internal/server/router.go` | ProviderState definition + methods | Rename fields lowercase, add all getters/setters |
| `internal/server/proxy_executor.go` | Proxy execution logic | Replace `ps.Config.*` reads, `ps.Name` → `ps.Name()`, `ps.Proxy.*` → methods |
| `internal/server/admin_api.go` | Admin API handlers | Replace all `ps.Config.*`, `ps.Pool.*`, `ps.Proxy.*` access |
| `internal/server/admin_test.go` | Admin API tests | Replace `ps.Config.*` reads/writes in test helpers |
| `internal/server/proxy_executor_test.go` | Proxy executor tests | Replace `ps.Pool.*` and `ps.Config` access |
| `internal/server/provider_manager.go` | Provider config reload | Replace `existing.Pool.*` and `existing.Config` access |
| `internal/server/lifecycle.go` | Health check lifecycle | Replace `ps.Name` reads |

---

### Task 1: ProviderState Name + Config Getters

**Goal:** Add getter methods for Name and all Config fields. Replace all direct field reads with method calls across 7 files.

**Files:**
- Modify: `internal/server/router.go:21-62` — rename fields lowercase + add getters
- Modify: `internal/server/proxy_executor.go` — `ps.Config.*` → getters, `ps.Name` → `ps.Name()`
- Modify: `internal/server/admin_api.go` — `ps.Config.*` → getters in getRuntimeParams + checkAdminToken, `ps.Name` → `ps.Name()`
- Modify: `internal/server/admin_test.go:366-440` — test reads use getters
- Modify: `internal/server/proxy_executor_test.go:123-181` — `ps.Config` → getters
- Modify: `internal/server/provider_manager.go` — `existing.Config.*` → getters
- Modify: `internal/server/lifecycle.go:217-233` — `ps.Name` → `ps.Name()`

**Step 1: Rename fields + add getters in router.go**

In `NewProviderState` (line 33-39), change struct field names from `Name`/`Config`/`Pool`/`Proxy` to `name`/`config`/`pool`/`proxy` in the composite literal.

In `PersistKeys` (line 52-62), change all field access to lowercase: `ps.Pool.Keys()` → `ps.pool.Keys()`, `ps.Pool.Name(i)` → `ps.pool.Name(i)`, `ps.Pool.IsDisabled(i)` → `ps.pool.IsDisabled(i)`, `ps.Name` → `ps.name`.

Add getter methods after `PersistKeys`:

```go
func (ps *ProviderState) Name() string                { return ps.name }
func (ps *ProviderState) HTTPTimeoutSec() time.Duration { return ps.config.HTTPTimeoutSec }
func (ps *ProviderState) MaxRetries() int             { return ps.config.MaxRetries }
func (ps *ProviderState) CooldownSec() int            { return ps.config.CooldownSec }
func (ps *ProviderState) BackoffCapSec() int          { return ps.config.BackoffCapSec }
func (ps *ProviderState) BackoffMultiplier() float64  { return ps.config.BackoffMultiplier }
func (ps *ProviderState) CBResetSec() int             { return ps.config.CBResetSec }
func (ps *ProviderState) UpstreamCBThreshold() int    { return ps.config.UpstreamCBThreshold }
func (ps *ProviderState) LogLevel() string            { return ps.config.LogLevel }
func (ps *ProviderState) GenaiModel() string          { return ps.config.GenaiModel }
func (ps *ProviderState) CalibrationIntervalSec() int { return ps.config.CalibrationIntervalSec }
func (ps *ProviderState) TargetBase() float64         { return ps.config.TargetBase }

func (ps *ProviderState) HasAdminToken() bool         { return ps.config.AdminToken != "" }
func (ps *ProviderState) CheckAdminToken(token string) bool {
    return ps.config.AdminToken != "" && ps.config.AdminToken == token
}
```

**Step 2: Update proxy_executor.go**

Replace `ps.Config.*` reads with getters. Replace `ps.Name` with `ps.Name()`. Keep `ps.Proxy.*` and `ps.Pool.*` direct access for now (Task 3 handles those).

```go
// Execute function (line 40)
cfg := ps.config  // direct access within package; or use ps.config field
```

Replace all `ps.Config.X` reads → `ps.X()` calls.

**Step 3: Update admin_api.go**

In `getRuntimeParams` (lines 759-766), replace `ps.Config.HTTPTimeoutSec` → `ps.HTTPTimeoutSec()`, etc.

In `checkAdminToken` (lines 818-822):
```go
// Before
if ps.Config.AdminToken == "" {
// After
if !ps.HasAdminToken() {
```
```go
// Before
if ps.Config.AdminToken == token {
// After
if ps.CheckAdminToken(token) {
```

In `checkAnyAdminToken` (lines 839-841), same replacements.

Replace all `ps.Name` reads → `ps.Name()`.

**Step 4: Update admin_test.go**

Lines 366-373: replace `ps.Config.HTTPTimeoutSec` → `ps.HTTPTimeoutSec()` etc.
Lines 375-382: change field writes from `ps.Config.X` → `ps.config.X` (lowercase, still within package).
Lines 394, 401-440: replace `ps.Config.*` reads → getter calls.

**Step 5: Update proxy_executor_test.go**

Lines 123, 137, 145: `ps.Config` → `ps.config`.
Lines 140, 169-173, 181: `ps.Pool.*` → `ps.pool.*`.

**Step 6: Update provider_manager.go**

Replace `existing.Pool.*` → `existing.pool.*`. Replace `existing.Config.*` → `existing.config.*` or getters.

**Step 7: Update lifecycle.go**

Lines 217, 223, 229, 233: `ps.Name` → `ps.name`.

**Step 8: Build and test**

```bash
go build ./...
go test -tags=unit -count=1 -short ./internal/...
```

**Step 9: Commit**

```bash
git add internal/server/router.go internal/server/proxy_executor.go internal/server/admin_api.go internal/server/admin_test.go internal/server/proxy_executor_test.go internal/server/provider_manager.go internal/server/lifecycle.go
git commit -m "refactor: add ProviderState Name + Config getters, migrate call sites"
```

---

### Task 2: ProviderState Config Setters + AdminToken Protection

**Goal:** Add setter methods for Config fields. Replace all direct Config writes with setter calls.

**Files:**
- Modify: `internal/server/router.go` — add 8 setters
- Modify: `internal/server/admin_api.go` — descriptor apply closures + handleRuntimeConfigSet
- Modify: `internal/server/admin_test.go` — restore logic

**Step 1: Add setters to router.go**

```go
func (ps *ProviderState) SetHTTPTimeoutSec(v time.Duration)   { ps.config.HTTPTimeoutSec = v }
func (ps *ProviderState) SetMaxRetries(v int)                 { ps.config.MaxRetries = v }
func (ps *ProviderState) SetCooldownSec(v int)                { ps.config.CooldownSec = v }
func (ps *ProviderState) SetBackoffCapSec(v int)              { ps.config.BackoffCapSec = v }
func (ps *ProviderState) SetBackoffMultiplier(v float64)      { ps.config.BackoffMultiplier = v }
func (ps *ProviderState) SetCBResetSec(v int)                 { ps.config.CBResetSec = v }
func (ps *ProviderState) SetUpstreamCBThreshold(v int)        { ps.config.UpstreamCBThreshold = v }
func (ps *ProviderState) SetLogLevel(v string)                { ps.config.LogLevel = v }
```

**Step 2: Update descriptor apply closures in admin_api.go**

Each descriptor's apply function that writes to `ps.Config.X` uses the setter:

```go
// http_timeout_sec descriptor (line 968)
ps.Config.HTTPTimeoutSec = v  →  ps.SetHTTPTimeoutSec(v)
ps.Config.MaxRetries = v      →  ps.SetMaxRetries(v)
ps.Config.CooldownSec = v     →  ps.SetCooldownSec(v)
ps.Config.BackoffCapSec = v   →  ps.SetBackoffCapSec(v)
ps.Config.BackoffMultiplier = v → ps.SetBackoffMultiplier(v)
ps.Config.CBResetSec = v      →  ps.SetCBResetSec(v)
ps.Config.UpstreamCBThreshold = v → ps.SetUpstreamCBThreshold(v)
ps.Config.LogLevel = v        →  ps.SetLogLevel(v)
```

**Step 3: Update handleRuntimeConfigSet**

Replace any `ps.Config.X = value` writes outside descriptors with setter calls.

**Step 4: Update admin_test.go restore logic**

```go
defer func() {
    ps.SetHTTPTimeoutSec(origTimeout)
    ps.SetMaxRetries(origRetries)
    ps.SetCooldownSec(origCooldown)
    ps.SetBackoffCapSec(origBackoffCap)
    ps.SetBackoffMultiplier(origBackoffMult)
    ps.SetCBResetSec(origCBReset)
    ps.SetUpstreamCBThreshold(origUpThreshold)
    ps.SetLogLevel(origLogLevel)
}()
```

**Step 5: Build and test**

```bash
go build ./...
go test -tags=unit -count=1 -short ./internal/...
make test-all
```

**Step 6: Commit**

```bash
git add internal/server/router.go internal/server/admin_api.go internal/server/admin_test.go
git commit -m "refactor: add ProviderState Config setters, migrate write access"
```

---

### Task 3: ProviderState Pool + Proxy Methods + Call Sites

**Goal:** Add Pool proxy methods and Proxy proxy methods. Replace all direct Pool/Proxy field access.

**Files:**
- Modify: `internal/server/router.go` — add Pool + Proxy methods
- Modify: `internal/server/admin_api.go` — handlers, stats, descriptors
- Modify: `internal/server/admin_test.go` — test assertions
- Modify: `internal/server/proxy_executor.go` — Execute, handle* methods
- Modify: `internal/server/proxy_executor_test.go` — test Pool/Proxy access
- Modify: `internal/server/provider_manager.go` — ReloadConfig Pool access

**Step 1: Add Pool proxy methods to router.go**

```go
func (ps *ProviderState) PoolKeys() []string                          { return ps.pool.Keys() }
func (ps *ProviderState) PoolActiveCount() int                        { return ps.pool.ActiveCount() }
func (ps *ProviderState) PoolCoolingCount() int                       { return ps.pool.CoolingCount() }
func (ps *ProviderState) PoolDisabledCount() int                      { return ps.pool.DisabledCount() }
func (ps *ProviderState) PoolName(i int) (string, error)              { return ps.pool.Name(i) }
func (ps *ProviderState) PoolKeyStatusLabel(i int) string             { return ps.pool.KeyStatusLabel(i) }
func (ps *ProviderState) PoolRequestsInLastMinute(i int) int64        { return ps.pool.RequestsInLastMinute(i) }
func (ps *ProviderState) PoolCleanupOldRequests() int                 { return ps.pool.CleanupOldRequests() }
func (ps *ProviderState) PoolCB(i int) *circuitbreaker.CircuitBreaker { return ps.pool.CB(i) }
func (ps *ProviderState) PoolIsDisabled(i int) bool                   { return ps.pool.IsDisabled(i) }
func (ps *ProviderState) PoolLen() int                                { return ps.pool.Len() }
func (ps *ProviderState) ConfigurePoolCBs(cooldown, backoffCap int, multiplier float64) {
    ps.pool.ConfigureCBs(cooldown, backoffCap, multiplier)
}
```

**Step 2: Add Proxy proxy methods to router.go**

```go
func (ps *ProviderState) SetProxyTimeout(d time.Duration)          { ps.proxy.client.Timeout = d }
func (ps *ProviderState) ProxyClientTimeout() time.Duration        { return ps.proxy.client.Timeout }
func (ps *ProviderState) ResetUpstreamCB()                         { ps.proxy.upCB.Reset() }
func (ps *ProviderState) RecordUpstreamFailure()                   { ps.proxy.upCB.RecordFailure() }
func (ps *ProviderState) SetUpstreamCBResetTimeout(sec int)        { ps.proxy.upCB.SetResetTimeout(time.Duration(sec) * time.Second) }
func (ps *ProviderState) SetUpstreamCBThreshold(n int)             { ps.proxy.upCB.SetThreshold(n) }
func (ps *ProviderState) UpstreamCBState() circuitbreaker.State    { return ps.proxy.upCB.State() }
```

**Step 3: Update router.go PersistKeys**

Change `ps.Pool.Keys()` → `ps.pool.Keys()`, `ps.Pool.Name(i)` → `ps.pool.Name(i)`, `ps.Pool.IsDisabled(i)` → `ps.pool.IsDisabled(i)`, `ps.Name` → `ps.name`.

**Step 4: Update proxy_executor.go**

Replace `ps.Proxy.client` → `ps.SetProxyTimeout()` for writes, `ps.ProxyClientTimeout()` for reads.
Replace `ps.Proxy.upCB.*` → corresponding proxy methods.
Replace `ps.Pool.CB(idx)` → `ps.PoolCB(idx)`.
Replace `ps.Pool.Name(idx)` → `ps.PoolName(idx)`.
Replace `ps.Pool.ConfigureCBs(...)` → `ps.ConfigurePoolCBs(...)`.

In Execute (line 40-43), keep `ps.config` and `ps.pool` local aliases — these are read-heavy and direct access within the package is fine.

**Step 5: Update admin_api.go**

Handlers:
- `ps.Pool.Keys()` → `ps.PoolKeys()`
- `ps.Pool.ActiveCount()` → `ps.PoolActiveCount()`
- `ps.Pool.CoolingCount()` → `ps.PoolCoolingCount()`
- `ps.Pool.DisabledCount()` → `ps.PoolDisabledCount()`
- `ps.Pool.ConfigureCBs(...)` → `ps.ConfigurePoolCBs(...)`
- `ps.Proxy.upCB.Reset()` → `ps.ResetUpstreamCB()`
- `ps.Proxy.upCB.SetResetTimeout(...)` → `ps.SetUpstreamCBResetTimeout(...)`
- `ps.Proxy.upCB.SetThreshold(...)` → `ps.SetUpstreamCBThreshold(...)`
- `ps.Proxy.upCB.State()` → `ps.UpstreamCBState()`

Descriptors:
- `ps.Pool.ConfigureCBs(...)` → `ps.ConfigurePoolCBs(...)`
- `ps.Proxy.upCB.SetResetTimeout(...)` → `ps.SetUpstreamCBResetTimeout(...)`
- `ps.Proxy.upCB.SetThreshold(...)` → `ps.SetUpstreamCBThreshold(...)`

**Step 6: Update admin_test.go**

Line 394: `ps.Proxy.client.Timeout` → `ps.ProxyClientTimeout()`.

**Step 7: Update proxy_executor_test.go**

Replace `ps.Pool.*` → `ps.pool.*`.

**Step 8: Update provider_manager.go**

Replace `existing.Pool.ConfigureCBs(...)` → `existing.ConfigurePoolCBs(...)`.
Replace `existing.Pool.Len()` → `existing.pool.Len()`, `existing.Pool.Name(i)` → `existing.pool.Name(i)`, `existing.Pool.Disable(i)` → `existing.pool.Disable(i)`.

**Step 9: Build and test**

```bash
go build ./...
go test -tags=unit -count=1 -short ./internal/...
make test-all
```

**Step 10: Commit**

```bash
git add internal/server/router.go internal/server/proxy_executor.go internal/server/admin_api.go internal/server/admin_test.go internal/server/proxy_executor_test.go internal/server/provider_manager.go
git commit -m "refactor: add ProviderState Pool + Proxy methods, migrate call sites"
```

---

## Validation

After each task:
```bash
go build ./...
go test -tags=unit -count=1 -short ./internal/...
```

After all tasks:
```bash
make test-all
```

Expected: 80+ unit tests pass, zero compilation errors, zero behavior change.

## Post-Implementation

After all three layers are complete, the lowercase fields `name`, `config`, `pool`, `proxy` are unexported and the old exported fields no longer exist. The compiler enforces the encapsulation boundary — any code that tries `ps.Config` will fail to compile.
