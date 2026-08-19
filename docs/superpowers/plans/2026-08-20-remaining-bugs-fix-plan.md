# 3 个遗留 Bug 修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 3 个遗留 Bug（#355 死循环、#348 token=0、#317/#346 轮转连续选中 429 key）

**Architecture:** 6 个文件的小范围改动，分 3 个独立 Task，每个 Task 可独立测试和提交。Task 间无依赖。

**Tech Stack:** Go 1.26 + stdlib only

**Spec:** `docs/superpowers/specs/2026-08-20-remaining-bugs-fix-design.md`

## Global Constraints

- 每个 Task 必须包含 table-driven test
- 提交前执行 `make check && make test-unit`
- 遵循 AGENTS.md 中 ProviderState 封装模式（getter/setter 访问字段）
- 使用 `fmt.Errorf("函数名: %w", err)` 包装错误

---

### Task 1: 修复 #355 — disable key 被 config reload 重新激活

**File:** `internal/server/provider_manager.go`, `internal/keypool/keypool.go`

**Root cause chain:**
1. `ReloadConfig` 从旧池读 disabled 名字 → 创建新池 → 按名字重新禁用
2. 此方法对无名 key（Name=""）失效，key 被重新激活
3. `ConfigureCBs` 重建 CB 时用 `RecordPerma("preserved")` 覆盖 `trippedReason`
4. `PeriodicKeyProbe` 的 guard 错误地允许 `"preserved"` 通过，导致 auth-rejected/manual 的 key 被恢复

**Interfaces:**
- Consumes: `keypool.LoadDisabledNames(name, cfg) []string` — 已存在，从 store 读 disabled 名字
- Consumes: `keypool.KeyPool.Disable(idx)` — 已存在
- Produces: `keypool.KeyPool.ConfigureCBs` 保留原 `trippedReason`

- [ ] **Step 1: 修改 `ReloadConfig` 改用 `LoadDisabledNames` 代替旧池读取**

当前 `provider_manager.go:109-117` 从旧池读 disabled 名字，改为从 `keypool.LoadDisabledNames(name, cfg)` 读取，这样无名 key 也被正确禁用。

```go
// 当前 (lines 109-133):
if existing, ok := pm.providers[name]; ok {
    oldPool := existing.pool
    var disabledNames []string
    for i := 0; i < oldPool.Len(); i++ {
        if oldPool.IsDisabled(i) {
            n, _ := oldPool.Name(i)
            disabledNames = append(disabledNames, n)
        }
    }
    existing.config = cfg
    existing.pool = keypool.NewKeyPool(cfg.Keys, cfg.KeyNames)
    existing.pool.SetSelectionMode(keypool.KeySelectionMode(cfg.KeySelection))
    existing.ConfigurePoolCBs(
        time.Duration(cfg.CooldownSec)*time.Second,
        time.Duration(cfg.BackoffCapSec)*time.Second,
        cfg.BackoffMultiplier,
    )
    for _, dn := range disabledNames {
        for i := 0; i < existing.pool.Len(); i++ {
            n, _ := existing.pool.Name(i)
            if n == dn {
                _ = existing.pool.Disable(i)
            }
        }
    }
```

改为：

```go
// 修改后：
if existing, ok := pm.providers[name]; ok {
    existing.config = cfg
    existing.pool = keypool.NewKeyPool(cfg.Keys, cfg.KeyNames)
    existing.pool.SetSelectionMode(keypool.KeySelectionMode(cfg.KeySelection))
    existing.ConfigurePoolCBs(
        time.Duration(cfg.CooldownSec)*time.Second,
        time.Duration(cfg.BackoffCapSec)*time.Second,
        cfg.BackoffMultiplier,
    )
    // 从 store 读取 disabled 名字，而非从旧池（旧池索引可能过期）
    for _, dn := range keypool.LoadDisabledNames(name, cfg) {
        for i := 0; i < existing.pool.Len(); i++ {
            n, _ := existing.pool.Name(i)
            if n == dn {
                _ = existing.pool.Disable(i)
            }
        }
    }
    logManager.ApplyLevel(cfg.LogLevel)
}
```

- [ ] **Step 2: 修改 `ConfigureCBs` 保留原 `trippedReason`**

`keypool.go:371-381` 当前用 `RecordPerma("preserved")` 覆盖 reason，改为保留原 reason：

```go
func (p *KeyPool) ConfigureCBs(base, backoffCap time.Duration, multiplier float64) {
    p.mu.Lock()
    defer p.mu.Unlock()
    for i := range p.cbs {
        wasPermanent := p.cbs[i].State() == circuitbreaker.Permanent
        oldReason := p.cbs[i].TrippedReason()
        p.cbs[i] = circuitbreaker.NewKeyCircuitBreaker(base, backoffCap, multiplier)
        if wasPermanent {
            if oldReason != "" {
                p.cbs[i].RecordPerma(oldReason)
            } else {
                p.cbs[i].RecordPerma("preserved")
            }
        }
    }
}
```

- [ ] **Step 3: 写单元测试 — 验证 `ConfigureCBs` 保留 `trippedReason`**

在 `keypool_test.go` 中新增测试：

```go
func TestConfigureCBsPreservesTrippedReason(t *testing.T) {
    pool := NewKeyPool([]string{"key1", "key2"}, []string{"", "named"})
    pool.ConfigureCBs(10*time.Second, 120*time.Second, 2.0)
    pool.Disable(0) // RecordPerma("manual") — 无名 key
    pool.Disable(1) // RecordPerma("manual") — 有名 key

    // 重新配置，应保留 reason
    pool.ConfigureCBs(15*time.Second, 120*time.Second, 2.0)

    if pool.CB(0).TrippedReason() != "manual" {
        t.Errorf("unnamed key trippedReason = %q, want manual", pool.CB(0).TrippedReason())
    }
    if pool.CB(1).TrippedReason() != "manual" {
        t.Errorf("named key trippedReason = %q, want manual", pool.CB(1).TrippedReason())
    }
    if !pool.IsDisabled(0) {
        t.Error("unnamed key should still be disabled after ConfigureCBs")
    }
    if !pool.IsDisabled(1) {
        t.Error("named key should still be disabled after ConfigureCBs")
    }
}
```

- [ ] **Step 4: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestConfigureCBsPreservesTrippedReason ./internal/keypool/
```

- [ ] **Step 5: 写单元测试 — 验证 `ReloadConfig` 保住 disabled 状态**

在 `provider_manager_test.go` 中新增：

```go
func TestReloadConfigPreservesDisabledState(t *testing.T) {
    // 创建 ProviderManager 和 ProviderState
    // 通过 pool.Disable 禁用 key
    // 调用 ReloadConfig（相同配置）
    // 验证 key 仍为 disabled
}
```

- [ ] **Step 6: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestReloadConfigPreservesDisabledState ./internal/server/
```

- [ ] **Step 7: 提交**

```bash
git add internal/server/provider_manager.go internal/keypool/keypool.go internal/keypool/keypool_test.go internal/server/provider_manager_test.go
git commit -m "fix: ReloadConfig 从 store 读 disabled 状态 + ConfigureCBs 保留 trippedReason（#355）"
```

---

### Task 2: 修复 #348 — token 剂量为 0

**File:** `internal/tokenestimator/tokenestimator.go`, `internal/server/proxy_executor.go`

**Root cause:**
1. `parseOpenAISSE` 永远返回 `outputTokens=0`（OpenAI 流式事件不带 usage）
2. `streamSSEAndEstimateTokens` 的 fallback `EstimateOutput` 在 `outputBuf` 为空时返回 0
3. `ExtractTokenUsage` 找不到 `usage` 时返回 (0,0)，且 `responseText` 为空时 `EstimateOutput` 返回 0

**Interfaces:**
- Consumes: `tokenestimator.ParseSSEEvent(raw []byte) (outputTokens int, textDelta string, thinkingDelta string)` — 已存在
- Produces: `streamSSEAndEstimateTokens` 在 `outputBuf` 为空时用 `respBodySize/4` 兜底

- [ ] **Step 1: 写测试 — 验证 `parseOpenAISSE` 累积 delta**

在 `tokenestimator_test.go` 中新增：

```go
func TestParseOpenAISSE_AccumulatesDelta(t *testing.T) {
    tests := []struct {
        name string
        raw  []byte
        wantText  string
        wantThink string
    }{
        {
            name: "content delta",
            raw:  []byte(`{"choices":[{"delta":{"content":"Hello"}}]}`),
            wantText: "Hello",
        },
        {
            name: "empty delta",
            raw:  []byte(`{"choices":[{"delta":{}}]}`),
            wantText: "",
        },
        {
            name: "no choices",
            raw:  []byte(`{}`),
            wantText: "",
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            tokens, text, think := parseOpenAISSE(tt.raw) //nolint:all
            if tokens != 0 {
                t.Errorf("outputTokens = %d, want 0", tokens)
            }
            if text != tt.wantText {
                t.Errorf("textDelta = %q, want %q", text, tt.wantText)
            }
            if think != tt.wantThink {
                t.Errorf("thinkingDelta = %q, want %q", think, tt.wantThink)
            }
        })
    }
}
```

- [ ] **Step 2: 修改 `parseOpenAISSE` 增加 `thinking` 和 `partial_json` 捕获**

```go
func parseOpenAISSE(raw []byte) (outputTokens int, textDelta string, thinkingDelta string) {
    var result struct {
        Choices []struct {
            Delta *struct {
                Content     string `json:"content"`
                Thinking    string `json:"thinking"`
                PartialJSON string `json:"partial_json"`
            } `json:"delta,omitempty"`
        } `json:"choices"`
    }
    if err := json.Unmarshal(raw, &result); err != nil {
        return 0, "", ""
    }
    for _, choice := range result.Choices {
        if choice.Delta != nil {
            textDelta += choice.Delta.Content
            textDelta += choice.Delta.PartialJSON
            thinkingDelta += choice.Delta.Thinking
        }
    }
    return 0, textDelta, thinkingDelta
}
```

- [ ] **Step 3: 修改 `streamSSEAndEstimateTokens` 增加空 outputBuf 兜底**

在 `proxy_executor.go:496-505`，当前 fallback 逻辑：

```go
if apiOutputTokens > 0 {
    inputTokens := tokenestimator.EstimateInput(bodyBytes, model)
    return inputTokens, apiOutputTokens, respBodySize
}

// Fall back to tiktoken estimation
outputTokens := tokenestimator.EstimateOutput(outputBuf.String(), model)
inputTokens := tokenestimator.EstimateInput(bodyBytes, model)
return inputTokens, outputTokens, respBodySize
```

改为：

```go
if apiOutputTokens > 0 {
    inputTokens := tokenestimator.EstimateInput(bodyBytes, model)
    return inputTokens, apiOutputTokens, respBodySize
}

// Fall back to tiktoken estimation
outputTokens := tokenestimator.EstimateOutput(outputBuf.String(), model)
if outputTokens == 0 && respBodySize > 0 {
    // 无法估算时的兜底：1 token ≈ 4 bytes
    outputTokens = int(respBodySize / 4)
}
inputTokens := tokenestimator.EstimateInput(bodyBytes, model)
return inputTokens, outputTokens, respBodySize
```

- [ ] **Step 4: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestParseOpenAISSE ./internal/tokenestimator/
go test -tags=unit -count=1 -short -run TestProxySSEStream ./internal/server/
```

- [ ] **Step 5: 提交**

```bash
git add internal/tokenestimator/tokenestimator.go internal/tokenestimator/tokenestimator_test.go internal/server/proxy_executor.go
git commit -m "fix: OpenAI 流式 token 估算 + 空 outputBuf 兜底（#348）"
```

---

### Task 3: 修复 #317/#346 — selectKeyRandom 连续选中 429 key

**File:** `internal/keypool/keypool.go`

**Root cause:** `selectKeyRandom` 在可用 key 中均匀随机选择，但刚冷却完的 key 可能仍被上游限流。缺少 `Next()` 的 RPM 优先策略。

**Interfaces:**
- Consumes: `keypool.KeyPool.RequestsInLastMinute(idx int) int` — 已存在
- Produces: `selectKeyRandom` 在可用 key 中选 RPM 最低的

- [ ] **Step 1: 写测试 — 验证 `selectKeyRandom` 选 RPM 最低的 key**

在 `keypool_test.go` 中新增：

```go
func TestSelectKeyRandom_PrefersLowRPM(t *testing.T) {
    pool := NewKeyPool(
        []string{"key1", "key2", "key3"},
        []string{"k1", "k2", "k3"},
    )
    pool.ConfigureCBs(10*time.Second, 120*time.Second, 2.0)
    pool.SetSelectionMode(KeySelectionRandom)

    // 模拟 key1 有高 RPM（10次），key2 中 RPM（5次），key3 低 RPM（1次）
    now := time.Now()
    for i := 0; i < 10; i++ {
        pool.requestHistory[0] = append(pool.requestHistory[0], now.Add(-time.Duration(i)*time.Second))
    }
    for i := 0; i < 5; i++ {
        pool.requestHistory[1] = append(pool.requestHistory[1], now.Add(-time.Duration(i)*time.Second))
    }
    for i := 0; i < 1; i++ {
        pool.requestHistory[2] = append(pool.requestHistory[2], now.Add(-time.Duration(i)*time.Second))
    }

    // 运行多次，统计哪个 key 被选中最多
    counts := make(map[int]int)
    for i := 0; i < 100; i++ {
        idx, _, ok := pool.SelectKey()
        if !ok {
            t.Fatal("SelectKey returned false")
        }
        counts[idx]++
    }

    // 绝大部分选择应落在低 RPM 的 key 上
    if counts[0] > counts[2] {
        t.Errorf("key3 (low RPM) should be selected more than key1 (high RPM): counts=%v", counts)
    }
}
```

- [ ] **Step 2: 修改 `selectKeyRandom` 增加 RPM 优先**

```go
func (p *KeyPool) selectKeyRandom() (int, string, bool) {
    p.mu.RLock()
    defer p.mu.RUnlock()

    var available []int
    for i, cb := range p.cbs {
        if cb.Allow() {
            available = append(available, i)
        }
    }
    if len(available) == 0 {
        return -1, "", false
    }

    // 在可用 key 中选 RPM 最低的，而非均匀随机
    // 这保证刚冷却完的 key 不会被立即选中
    bestIdx := available[0]
    bestRPM := p.RequestsInLastMinute(bestIdx)
    for _, idx := range available[1:] {
        rpm := p.RequestsInLastMinute(idx)
        if rpm < bestRPM {
            bestRPM = rpm
            bestIdx = idx
        }
    }
    return bestIdx, p.keys[bestIdx], true
}
```

- [ ] **Step 3: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestSelectKeyRandom_PrefersLowRPM ./internal/keypool/
```

- [ ] **Step 4: 提交**

```bash
git add internal/keypool/keypool.go internal/keypool/keypool_test.go
git commit -m "fix: selectKeyRandom 选 RPM 最低的 key，避免连续选中 429（#317）"
```

---

## Execution Handoff

计划已保存到 `docs/superpowers/plans/2026-08-20-remaining-bugs-fix-plan.md`。两个执行选项：

**1. Subagent-Driven（推荐）** — 每个 Task 派一个子代理实现，独立提交，快速迭代

**2. Inline Execution** — 在本会话中逐个执行，批量提交

你选哪个？