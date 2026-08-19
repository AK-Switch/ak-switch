# Bug 修复设计：3 个遗留 Issue

> **日期:** 2026-08-20
> **范围:** `internal/server/provider_manager.go`, `internal/server/proxy_executor.go`, `internal/keypool/store.go`, `internal/keypool/keypool.go`, `internal/tokenestimator/tokenestimator.go`, `internal/circuitbreaker/key.go`, `internal/server/lifecycle.go`

---

## 1. #355 — 死循环（disable key ↔ config reloaded）

### 根因分析

日志显示 `13:35:00.255` 同一毫秒内反复出现 `key disabled` + `config reloaded`，循环周期约 17ms。

**触发链：**

```
CLI key disable
  → store 写入 Disabled=true
  → triggerReload() → POST /api/reload

Server 处理 reload:
  → ReloadConfig (provider_manager.go:98)
    → loadKeysFromConfig → keypool.LoadKeysFromStore → keysFromStore(store.go:287)
      → 只读取 Key 和 Name，完全忽略 Disabled 字段
    → 新建 KeyPool(cfg.Keys, cfg.KeyNames) — 所有 key 初始为活跃
    → 从旧池按名字重新禁用 (lines 126-133)
    → 无名 key (Name="") 静默跳过，重新激活
    → 即使有名字，ConfigureCBs (keypool.go:371) 重建所有熔断器，
      用 RecordPerma("preserved") 覆写 trippedReason
  → 响应 200 OK 返回 CLI
```

**死循环条件：** 当 reload 后 key 被重新激活（无名 key 或 store 索引错位），上游请求打到该 key 返回 401/403（或 429 达 cap），`handleAuthRejected` 再次禁用 key，从外部触发新一轮 reload，循环往复。

**第二个源头：`ConfigureCBs` 破坏熔断器状态。** `ReloadConfig` 第 121 行调用 `ConfigurePoolCBs`，内部 `ConfigureCBs` (keypool.go:371) 每次重建所有 CB，用 `RecordPerma("preserved")` 覆盖原有的 `trippedReason`（如 `quota_exhausted`、`manual`）。这导致 `PeriodicKeyProbe` (lifecycle.go:300) 的 guard：

```go
if reason := pool.CB(i).TrippedReason(); reason != "quota_exhausted" && reason != "preserved" {
    continue  // 跳过 auth-rejected 和 manual 的 key
}
```

被 bypass：`"preserved"` 是允许通过的条件，所以 probe 错误地恢复了 auth-rejected/manual 的 key。

### 修复

#### 修复 A：keysFromStore 读取 Disabled（store.go）

`keysFromStore` 新增返回 `disabled` 切片，`ReloadConfig` 据此设置初始禁用状态。

#### 修复 B：ConfigureCBs 不覆写 trippedReason（keypool.go）

```go
func (p *KeyPool) ConfigureCBs(base, backoffCap time.Duration, multiplier float64) {
    for i := range p.cbs {
        wasPermanent := p.cbs[i].State() == circuitbreaker.Permanent
        oldReason := p.cbs[i].TrippedReason()  // 保存原 reason
        p.cbs[i] = circuitbreaker.NewKeyCircuitBreaker(base, backoffCap, multiplier)
        if wasPermanent {
            p.cbs[i].RecordPerma(oldReason)  // 恢复原 reason，而非写死 "preserved"
        }
    }
}
```

#### 修复 C：ReloadConfig 不重建 KeyPool 当 keys 未变（provider_manager.go，可选优化）

如果 `loadKeysFromConfig` 返回的 keys 与旧池一致，跳过重建，只更新 CB 参数。

---

## 2. #348 — token 剂量为 0

### 根因分析

日志 `tok=89298+0` 是流式 SSE 响应，输出 token 为 0。有两个独立场景：

#### 场景 A：OpenAI 格式 SSE 流

`parseOpenAISSE` (tokenestimator.go:219) **从不返回 token 计数**。OpenAI 流式事件不携带 `usage` 字段，token 只能靠 `EstimateOutput` 估算。如果 `outputBuf` 累积的 text delta 为空（tool call 场景、纯 `thinking` 块），`EstimateOutput` 返回 0。

#### 场景 B：非流式响应无 `usage` 对象

`ExtractTokenUsage` 从 JSON 解析 `usage` 对象，若 payload 无顶层 `usage` 则返回 (0,0)。随后 `handleSuccess` 走 fallback 但若 `responseText` 也为空，`EstimateOutput` 返回 0。

### 修复

#### 修复 A：OpenAI 流式累积估算

在 `parseOpenAISSE` 中增加对 `delta.thinking` 和 `delta.partial_json` 的捕获。`streamSSEAndEstimateTokens` 中，对 `outputBuf` 为空时，用 `respBodySize / 4` 作为粗略估算。

#### 修复 B：非流式 usage fallback 强化

`ExtractTokenUsage` 找不到 `usage` 时，回退到 `ExtractResponseText` + `EstimateOutput`。当前 `handleSuccess` 已有此逻辑，但确保所有 fallback 路径都尝试过。

---

## 3. #317/#346 — 轮转连续选中 429 key

### 根因分析

`selectKeyRandom` (keypool.go:110) 随机选可用 key，但缺少 `Next()` 的"最低 RPM 优先"策略。cooldown 过期后 key 回到可用状态，但上游可能仍然是 429 状态。

### 修复

**方案：** `selectKeyRandom` 也采用 RPM 优先策略——在全部可用 key 中选 RPM 最低的。这保证刚冷却完的 key 不会被立即选中，而是优先选低负载的 key。

---

## 文件改动总览

| 文件 | 改动 | 修复 |
|------|------|------|
| `internal/keypool/store.go` | `keysFromStore` 返回 `disabled` 标志 | #355‑A |
| `internal/keypool/keypool.go` | `ConfigureCBs` 保留原 `trippedReason`；`selectKeyRandom` 增加 RPM 优先 | #355‑B, #317 |
| `internal/server/provider_manager.go` | `ReloadConfig` 从 store 读取 disabled 状态 | #355‑A |
| `internal/server/proxy_executor.go` | `streamSSEAndEstimateTokens` 增加空 outputBuf 兜底 | #348‑A |
| `internal/tokenestimator/tokenestimator.go` | `parseOpenAISSE` 增加 thinking/partial_json 捕获 | #348‑A |
| `internal/circuitbreaker/key.go` | 无改动 | — |
| `internal/server/lifecycle.go` | 无改动 | — |