# Thinking Rectifier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `thinking-proxy.py` 的整流逻辑内嵌到 AK-Switch，作为 per-provider 可配置的 request body 修改功能

**Architecture:** 在 `ProxyExecutor.Execute()` 的 `readRequestBody()` 之后插入整流调用。新增 `ThinkingRectifier` 类型封装 JSON 解析/修改逻辑。配置扩展 `ProviderConfig` 的 `ThinkingMode` + `RectifyThinkingMapTo` 字段，保留 `DisableThinking` 向后兼容。

**Tech Stack:** Go 标准库 (`encoding/json`), 项目现有模式 (ProviderState getter/setter, runtime-config descriptor table)

## Global Constraints

- Tab 缩进（项目强制）
- 错误包装: `fmt.Errorf("函数名: %w", err)`
- 日志: `slog`
- 提交前: `make check && make test-unit`
- 不直接访问 `ProviderState` 私有字段
- 单元测试用 `-tags=unit`

---

### Task 1: 配置结构变更 + 向后兼容

**Files:**
- Modify: `internal/config/config.go:17-46` (ProviderConfig struct)
- Modify: `internal/config/config.go:211-254` (DeepCopy)
- Modify: `internal/config/config.go:264-350` (mergeWithDefaults)
- Modify: `internal/config/config_loader.go:99-126` (LoadAllTomlProviders loop)
- Modify: `internal/config/config.go:90-111` (DefaultProviderConfig)

**Interfaces:**
- Consumes: none (first task)
- Produces: `ProviderConfig.ThinkingMode`, `ProviderConfig.RectifyThinkingMapTo`, `Config.migrateDisableThinking()`

- [ ] **Step 1: Write the failing test** — 暂时跳过（config 结构变更的测试放在后面统一验证）
- [ ] **Step 2: Add new fields to ProviderConfig struct**

In `internal/config/config.go`, add two new fields after `DisableThinking` (line 22):

```go
DisableThinking     bool   `toml:"disable_thinking,omitempty"` // Deprecated: use thinking_mode
ThinkingMode        string `toml:"thinking_mode,omitempty"`        // "default" | "rectify"
RectifyThinkingMapTo string `toml:"rectify_thinking_map_to,omitempty"` // "enabled" | "auto" | "disabled"
```

- [ ] **Step 3: Add migrateDisableThinking method**

Add to `internal/config/config.go` after `mergeDefaults`:

```go
// migrateDisableThinking handles backward compatibility for the deprecated
// DisableThinking field. When DisableThinking is true and ThinkingMode is
// unset, it maps to the new field names.
func (pc *ProviderConfig) migrateDisableThinking() {
	if pc.DisableThinking && pc.ThinkingMode == "" {
		pc.ThinkingMode = "rectify"
		pc.RectifyThinkingMapTo = "enabled"
	}
}
```

- [ ] **Step 4: Call migrateDisableThinking in config_loader.go**

In `internal/config/config_loader.go`, add after line 109 (`p.mergeDefaults()`):

```go
p.migrateDisableThinking()
```

- [ ] **Step 5: Update DeepCopy**

In `internal/config/config.go` DeepCopy method (line 222-223), add the new fields:

```go
DisableThinking:        c.DisableThinking,
ThinkingMode:           c.ThinkingMode,
RectifyThinkingMapTo:   c.RectifyThinkingMapTo,
```

- [ ] **Step 6: Update mergeWithDefaults**

In `internal/config/config.go` mergeWithDefaults (after line 309), add:

```go
if override.ThinkingMode != "" {
    result.ThinkingMode = override.ThinkingMode
}
if override.RectifyThinkingMapTo != "" {
    result.RectifyThinkingMapTo = override.RectifyThinkingMapTo
}
```

- [ ] **Step 7: Run tests**

```bash
make test-unit -tags=unit -count=1 -short ./internal/config/
```

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/config/config_loader.go
git commit -m "feat: add ThinkingMode and RectifyThinkingMapTo config fields"
```

---

### Task 2: ProviderState 访问器

**Files:**
- Modify: `internal/server/router.go:104-120` (getter/setter block)

**Interfaces:**
- Consumes: `ProviderConfig.ThinkingMode`, `ProviderConfig.RectifyThinkingMapTo`
- Produces: `ps.ThinkingMode()`, `ps.RectifyThinkingMapTo()`, `ps.SetThinkingMode()`, `ps.SetRectifyThinkingMapTo()`

- [ ] **Step 1: Add getter/setter methods**

In `internal/server/router.go`, after the last existing setter (line 120):

```go
func (ps *ProviderState) ThinkingMode() string              { return ps.config.ThinkingMode }
func (ps *ProviderState) RectifyThinkingMapTo() string      { return ps.config.RectifyThinkingMapTo }
func (ps *ProviderState) SetThinkingMode(v string)          { ps.config.ThinkingMode = v }
func (ps *ProviderState) SetRectifyThinkingMapTo(v string)  { ps.config.RectifyThinkingMapTo = v }
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/router.go
git commit -m "feat: add ProviderState getter/setter for thinking rectifier config"
```

---

### Task 3: ThinkingRectifier 核心逻辑

**Files:**
- Create: `internal/server/rectifier.go`
- Create: `internal/server/rectifier_test.go`

**Interfaces:**
- Consumes: none
- Produces: `NewThinkingRectifier()`, `(*ThinkingRectifier).ShouldRectify()`, `(*ThinkingRectifier).Process()`, `(*ThinkingRectifier).Stats()`

- [ ] **Step 1: Write the failing tests**

Create `internal/server/rectifier_test.go`:

```go
package server

import (
	"testing"
)

func TestThinkingRectifier_AdaptiveToEnabled(t *testing.T) {
	r := NewThinkingRectifier(true, "enabled")
	body := []byte(`{"model":"gpt-4","thinking":{"type":"adaptive"}}`)
	result := r.Process(body)
	if string(result) == string(body) {
		t.Fatal("expected body to be modified")
	}
	stats := r.Stats()
	if stats.Modified != 1 {
		t.Fatalf("expected 1 modified, got %d", stats.Modified)
	}
	if stats.Total != 1 {
		t.Fatalf("expected 1 total, got %d", stats.Total)
	}
}

func TestThinkingRectifier_AdaptiveToAuto(t *testing.T) {
	r := NewThinkingRectifier(true, "auto")
	body := []byte(`{"thinking":{"type":"adaptive"}}`)
	result := r.Process(body)
	if string(result) == string(body) {
		t.Fatal("expected body to be modified")
	}
	stats := r.Stats()
	if stats.Modified != 1 {
		t.Fatalf("expected 1 modified, got %d", stats.Modified)
	}
}

func TestThinkingRectifier_NonAdaptivePassthrough(t *testing.T) {
	r := NewThinkingRectifier(true, "enabled")
	body := []byte(`{"thinking":{"type":"enabled"}}`)
	result := r.Process(body)
	if string(result) != string(body) {
		t.Fatal("expected body unchanged for non-adaptive type")
	}
	stats := r.Stats()
	if stats.Passthrough != 1 {
		t.Fatalf("expected 1 passthrough, got %d", stats.Passthrough)
	}
}

func TestThinkingRectifier_NoThinkingField(t *testing.T) {
	r := NewThinkingRectifier(true, "enabled")
	body := []byte(`{"model":"gpt-4"}`)
	result := r.Process(body)
	if string(result) != string(body) {
		t.Fatal("expected body unchanged when no thinking field")
	}
	stats := r.Stats()
	if stats.Passthrough != 1 {
		t.Fatalf("expected 1 passthrough, got %d", stats.Passthrough)
	}
}

func TestThinkingRectifier_InvalidJSON(t *testing.T) {
	r := NewThinkingRectifier(true, "enabled")
	body := []byte(`not valid json {{{`)
	result := r.Process(body)
	if string(result) != string(body) {
		t.Fatal("expected original body returned on JSON parse failure")
	}
}

func TestThinkingRectifier_DisabledSkips(t *testing.T) {
	r := NewThinkingRectifier(false, "enabled")
	body := []byte(`{"thinking":{"type":"adaptive"}}`)
	result := r.Process(body)
	if string(result) != string(body) {
		t.Fatal("expected body unchanged when rectifier disabled")
	}
}

func TestThinkingRectifier_DefaultModeSkips(t *testing.T) {
	r := NewThinkingRectifier(true, "")
	body := []byte(`{"thinking":{"type":"adaptive"}}`)
	result := r.Process(body)
	if string(result) != string(body) {
		t.Fatal("expected body unchanged with empty mapTo")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -tags=unit -count=1 -short -run TestThinkingRectifier ./internal/server/
```

Expected: compilation error `undefined: NewThinkingRectifier`

- [ ] **Step 3: Write minimal implementation**

Create `internal/server/rectifier.go`:

```go
package server

import (
	"encoding/json"
	"log/slog"
)

// ThinkingRectifier modifies request bodies to convert unsupported thinking.type
// values (e.g., "adaptive") to values accepted by the upstream API.
type ThinkingRectifier struct {
	enabled bool
	mapTo   string
	stats   RectifierStats
}

// RectifierStats holds counters for observability.
type RectifierStats struct {
	Total       int64
	Modified    int64
	Passthrough int64
}

// NewThinkingRectifier creates a rectifier with the given configuration.
// If enabled is false, Process() is a no-op passthrough.
// mapTo must be one of: "enabled", "auto", "disabled".
func NewThinkingRectifier(enabled bool, mapTo string) *ThinkingRectifier {
	if enabled && mapTo == "" {
		mapTo = "enabled"
	}
	return &ThinkingRectifier{
		enabled: enabled,
		mapTo:   mapTo,
	}
}

// ShouldRectify returns true if this rectifier is active and has a valid map target.
func (r *ThinkingRectifier) ShouldRectify() bool {
	return r.enabled && r.mapTo != ""
}

// Process examines the JSON body and modifies thinking.type from "adaptive"
// to the configured mapTo value. Returns the original body on any parse/marshal
// failure (safe degradation).
func (r *ThinkingRectifier) Process(body []byte) []byte {
	r.stats.Total++

	if !r.enabled || r.mapTo == "" {
		r.stats.Passthrough++
		return body
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		slog.Warn("thinking rectifier: JSON parse failed, passthrough", "error", err)
		r.stats.Passthrough++
		return body
	}

	thinking, ok := data["thinking"].(map[string]interface{})
	if !ok {
		r.stats.Passthrough++
		return body
	}

	thinkingType, ok := thinking["type"].(string)
	if !ok || thinkingType != "adaptive" {
		r.stats.Passthrough++
		return body
	}

	thinking["type"] = r.mapTo
	modified, err := json.Marshal(data)
	if err != nil {
		slog.Warn("thinking rectifier: JSON marshal failed, passthrough", "error", err)
		r.stats.Passthrough++
		return body
	}

	r.stats.Modified++
	return modified
}

// Stats returns a copy of the current rectifier statistics.
func (r *ThinkingRectifier) Stats() RectifierStats {
	return r.stats
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -tags=unit -count=1 -short -run TestThinkingRectifier ./internal/server/
```

- [ ] **Step 5: Commit**

```bash
git add internal/server/rectifier.go internal/server/rectifier_test.go
git commit -m "feat: add ThinkingRectifier for adaptive thinking.type conversion"
```

---

### Task 4: ProxyExecutor 集成

**Files:**
- Modify: `internal/server/proxy_executor.go:23-34` (ProxyExecutor struct + NewProxyExecutor)
- Modify: `internal/server/proxy_executor.go:45-49` (Execute method, insert rectifier call)
- Modify: `internal/server/router.go:185-203` (NewProviderRouter, pass rectifier)

**Interfaces:**
- Consumes: `NewThinkingRectifier`, `(*ThinkingRectifier).Process()`, `ps.ThinkingMode()`
- Produces: `ProxyExecutor.rectifier` field

- [ ] **Step 1: Add rectifier field to ProxyExecutor struct**

In `internal/server/proxy_executor.go`, modify the struct (line 23-26):

```go
type ProxyExecutor struct {
	metrics    *akswitchmetrics.Metrics
	calibrator *tracker.Calibrator
	rectifier  *ThinkingRectifier
}
```

- [ ] **Step 2: Update NewProxyExecutor to accept rectifier**

In `internal/server/proxy_executor.go`, modify `NewProxyExecutor` (line 29-34):

```go
func NewProxyExecutor(metrics *akswitchmetrics.Metrics, calibrator *tracker.Calibrator, rectifier *ThinkingRectifier) *ProxyExecutor {
	return &ProxyExecutor{
		metrics:    metrics,
		calibrator: calibrator,
		rectifier:  rectifier,
	}
}
```

- [ ] **Step 3: Insert rectifier call in Execute()**

In `internal/server/proxy_executor.go`, after `readRequestBody()` error check (line 49), add:

```go
if err == nil && ps.ThinkingMode() == "rectify" {
    bodyBytes = px.rectifier.Process(bodyBytes)
}
```

So the area around line 45-52 becomes:

```go
bodyBytes, err := readRequestBody(w, r)
if err != nil {
    px.recordProxyMetrics(r.Method, "4xx", "", start)
    return
}

if err == nil && ps.ThinkingMode() == "rectify" {
    bodyBytes = px.rectifier.Process(bodyBytes)
}

target := buildTargetURL(ps.config, r.URL.Path, r.URL.RawQuery)
```

Note: `err == nil` is technically always true here (we'd have returned), but kept for clarity matching the pattern. Actually, since we returned on err != nil, `err` is guaranteed nil here. Simplify to:

```go
if ps.ThinkingMode() == "rectify" {
    bodyBytes = px.rectifier.Process(bodyBytes)
}
```

- [ ] **Step 4: Update NewProviderRouter to create and pass rectifier**

In `internal/server/router.go`, modify `NewProviderRouter` (line 191):

```go
pe := NewProxyExecutor(m, cal, NewThinkingRectifier(false, ""))
```

Wait — the rectifier needs to be per-provider, but `NewProviderRouter` creates a single `ProxyExecutor`. Looking at the architecture, `ProxyExecutor` is shared across all providers but receives `ps` (ProviderState) per request in `Execute()`. The rectifier config comes from `ps.ThinkingMode()`.

The simplest approach: create the rectifier in `Execute()` lazily, or store config in `ProxyExecutor`. But since `ProxyExecutor` is stateless except for metrics, the cleanest approach is to have the rectifier created lazily per-request, or to pass the config through `ProviderState` and check it each time.

Actually, looking at it more carefully — `px.rectifier` should be a default/no-op rectifier, and the per-provider config is checked via `ps.ThinkingMode()` in Execute(). The `Process()` method already checks `r.enabled` internally, so we can either:

1. Create a single `NewThinkingRectifier(false, "")` (disabled) and let `Process()` be a no-op always — then we'd need to reconfigure it per request
2. Check `ps.ThinkingMode()` before calling `Process()` and create a rectifier on the fly

Option 2 is simpler and already shown in the design. Let me refine:

```go
if ps.ThinkingMode() == "rectify" {
    rectifier := NewThinkingRectifier(true, ps.RectifyThinkingMapTo())
    bodyBytes = rectifier.Process(bodyBytes)
}
```

This creates a small rectifier per-request but it's allocation-free in practice (small struct, no heap allocation for the map field). Even simpler: we can make it a method on ProviderState or just inline the logic.

Actually, the cleanest approach is to keep `px.rectifier` as a field but create per-provider rectifiers lazily. But since `ProxyExecutor` is shared and `ps` varies per request, we should just check the config and create on the fly. The overhead is negligible (a few struct field copies).

Let me simplify: remove the rectifier field from ProxyExecutor entirely, and just inline the creation in Execute():

```go
if ps.ThinkingMode() == "rectify" {
    bodyBytes = NewThinkingRectifier(true, ps.RectifyThinkingMapTo()).Process(bodyBytes)
}
```

This is the simplest approach. No state to manage, no field to add to ProxyExecutor.

- [ ] **Step 3 (revised): Insert rectifier call in Execute()**

In `internal/server/proxy_executor.go`, after `readRequestBody()` error check (line 49):

```go
bodyBytes, err := readRequestBody(w, r)
if err != nil {
    px.recordProxyMetrics(r.Method, "4xx", "", start)
    return
}

if ps.ThinkingMode() == "rectify" {
    bodyBytes = NewThinkingRectifier(true, ps.RectifyThinkingMapTo()).Process(bodyBytes)
}

target := buildTargetURL(ps.config, r.URL.Path, r.URL.RawQuery)
```

- [ ] **Step 4: Revert ProxyExecutor struct changes**

No rectifier field needed in ProxyExecutor struct or NewProxyExecutor. Keep the original signature.

- [ ] **Step 5: Verify compilation**

```bash
go build ./...
```

- [ ] **Step 6: Run existing proxy tests**

```bash
go test -tags=unit -count=1 -short ./internal/server/
```

- [ ] **Step 7: Commit**

```bash
git add internal/server/proxy_executor.go
git commit -m "feat: integrate thinking rectifier into proxy request pipeline"
```

---

### Task 5: Admin API runtime-config 支持

**Files:**
- Modify: `internal/server/admin_api.go:752-764` (getRuntimeParams)
- Modify: `internal/server/admin_api.go:954-1098` (runtimeConfigFields)

**Interfaces:**
- Consumes: `ps.ThinkingMode()`, `ps.SetThinkingMode()`, `ps.RectifyThinkingMapTo()`, `ps.SetRectifyThinkingMapTo()`
- Produces: runtime-config keys `thinking_mode`, `rectify_thinking_map_to`

- [ ] **Step 1: Add fields to getRuntimeParams**

In `internal/server/admin_api.go`, add to the `getRuntimeParams` map (line 752-764):

```go
"thinking_mode":             ps.ThinkingMode(),
"rectify_thinking_map_to":   ps.RectifyThinkingMapTo(),
```

- [ ] **Step 2: Add runtime config field descriptors**

In `internal/server/admin_api.go`, add two entries to `runtimeConfigFields` (after the `log_level` entry):

```go
{
    key: "thinking_mode",
    apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
        s, ok := raw.(string)
        if !ok {
            return nil, fmt.Errorf("thinking_mode must be a string")
        }
        switch strings.TrimSpace(strings.ToLower(s)) {
        case "default", "rectify":
            ps.SetThinkingMode(s)
            return s, nil
        default:
            return nil, fmt.Errorf("invalid thinking_mode %q, use: default, rectify", s)
        }
    },
    persist: func(cfg *config.Config, val interface{}) {
        v, _ := val.(string)
        cfg.ThinkingMode = v
    },
},
{
    key: "rectify_thinking_map_to",
    apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
        s, ok := raw.(string)
        if !ok {
            return nil, fmt.Errorf("rectify_thinking_map_to must be a string")
        }
        switch strings.TrimSpace(strings.ToLower(s)) {
        case "enabled", "auto", "disabled":
            ps.SetRectifyThinkingMapTo(s)
            return s, nil
        default:
            return nil, fmt.Errorf("invalid rectify_thinking_map_to %q, use: enabled, auto, disabled", s)
        }
    },
    persist: func(cfg *config.Config, val interface{}) {
        v, _ := val.(string)
        cfg.RectifyThinkingMapTo = v
    },
},
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./...
```

- [ ] **Step 4: Run admin API tests**

```bash
go test -tags=unit -count=1 -short -run TestRuntimeConfig ./internal/server/
```

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin_api.go
git commit -m "feat: add thinking_mode and rectify_thinking_map_to runtime-config keys"
```

---

### Task 6: 集成测试

**Files:**
- Modify: `internal/server/proxy_executor_test.go`
- Modify: `internal/server/admin_api_test.go`

**Interfaces:**
- Consumes: all previous tasks' outputs
- Produces: integration test coverage

- [ ] **Step 1: Add proxy_executor_test for rectifier integration**

In `proxy_executor_test.go`, add a test that verifies the body is modified when `ThinkingMode == "rectify"`:

```go
func TestExecute_ThinkingRectifier(t *testing.T) {
    // Setup: create a ProviderState with ThinkingMode = "rectify"
    // Send a request with thinking.type = "adaptive"
    // Verify the upstream receives thinking.type = "enabled" (or configured mapTo)
    // Verify the client receives the upstream response unchanged
}
```

Note: This test requires an HTTP test server. Follow the existing test patterns in `proxy_executor_test.go`.

- [ ] **Step 2: Add admin_api_test for runtime-config**

In `admin_api_test.go`, add tests for:

1. `POST /api/runtime-config?provider=X` with `{"key": "thinking_mode", "value": "rectify"}`
2. `POST /api/runtime-config?provider=X` with `{"key": "rectify_thinking_map_to", "value": "auto"}`
3. Invalid value rejected with 400

- [ ] **Step 3: Run all tests**

```bash
make test-unit
```

- [ ] **Step 4: Commit**

```bash
git add internal/server/proxy_executor_test.go internal/server/admin_api_test.go
git commit -m "test: add rectifier integration and runtime-config tests"
```

---

### Task 7: 最终验证

- [ ] **Step 1: Full check**

```bash
make check
make test-unit
```

- [ ] **Step 2: Manual config test**

Create a test TOML with `thinking_mode = "rectify"` and verify:
- Config loads without error
- Old `disable_thinking = true` config loads correctly

- [ ] **Step 3: Summary**

All 7 tasks complete. Feature ready for review.
