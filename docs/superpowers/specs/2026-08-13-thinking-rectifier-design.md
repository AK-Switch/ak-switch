# Thinking Rectifier 设计文档

> **Issue**: 为 AK-Switch 增加 request body 整流功能，将上游不支持的 `thinking.type` 值（如 `"adaptive"`）转换为上游接受的值。
> **Status**: 设计完成，待实现
> **Date**: 2026-08-13

## 背景

Claude Code 等客户端发送 `thinking.type: "adaptive"` 给某些上游 API（SenseNova/DeepSeek V4）时会收到 400 错误，因为这些上游只接受 `["enabled", "auto", "disabled"]`。

当前 workaround 是在 AK-Switch 外额外跑一个 `thinking-proxy.py` 做中转，增加了一层不必要的复杂度。将整流逻辑内嵌到 AK-Switch 中，作为 provider 级别的可配置扩展功能。

## 需求

1. **整流能力**: 当 `thinking.type == "adaptive"` 时，自动替换为配置的目标值
2. **per-provider 控制**: 每个 provider 独立开关整流，映射值可配置
3. **向后兼容**: 已有 `disable_thinking = true` 的 TOML 配置继续工作
4. **运行时热更新**: 通过 Admin API 动态修改整流配置
5. **可观测性**: 日志中记录整流统计信息

## 设计

### 1. 配置变更

文件: `internal/config/config.go`

新增字段到 `ProviderConfig`:

```go
ThinkingMode         string `toml:"thinking_mode,omitempty"`        // "default" | "rectify"
RectifyThinkingMapTo string `toml:"rectify_thinking_map_to,omitempty"` // "enabled" | "auto" | "disabled"
```

**向后兼容处理**: 在 TOML 解析后、`mergeDefaults` 之前，检查 `DisableThinking == true` 的 provider，自动设置:
- `ThinkingMode = "rectify"`
- `RectifyThinkingMapTo = "enabled"`

`DisableThinking` 字段保留在 struct 中（不删除），但不再作为主要控制开关。TOML 中仍支持 `disable_thinking = true` 作为旧配置的兼容写法。

**TOML 配置示例:**

```toml
[provider.sensenova]
target_base = "https://api.stepfun.com/v1"
thinking_mode = "rectify"
rectify_thinking_map_to = "enabled"

[provider.deepseek]
target_base = "https://api.deepseek.com/v1"
thinking_mode = "rectify"
rectify_thinking_map_to = "auto"

[provider.openai]
target_base = "https://api.openai.com/v1"
# thinking_mode 不设置 = "default"，不做任何修改
```

**旧配置兼容:**

```toml
[provider.sensenova]
target_base = "https://api.stepfun.com/v1"
disable_thinking = true  # 自动映射为 thinking_mode = "rectify", rectify_thinking_map_to = "enabled"
```

### 2. ProviderState 访问器

文件: `internal/server/router.go`

遵循项目 ProviderState 封装模式，新增 getter/setter:

```go
func (ps *ProviderState) ThinkingMode() string              { return ps.config.ThinkingMode }
func (ps *ProviderState) RectifyThinkingMapTo() string      { return ps.config.RectifyThinkingMapTo }
func (ps *ProviderState) SetThinkingMode(v string)          { ps.config.ThinkingMode = v }
func (ps *ProviderState) SetRectifyThinkingMapTo(v string)  { ps.config.RectifyThinkingMapTo = v }
```

### 3. 整流器核心逻辑

文件: `internal/server/rectifier.go`（新建）

```go
package server

type ThinkingRectifier struct {
    enabled bool
    mapTo   string // "enabled" | "auto" | "disabled"
    stats   RectifierStats
}

type RectifierStats struct {
    Total       int64
    Modified    int64
    Passthrough int64
}

func NewThinkingRectifier(enabled bool, mapTo string) *ThinkingRectifier
func (r *ThinkingRectifier) ShouldRectify() bool
func (r *ThinkingRectifier) Process(body []byte) []byte
func (r *ThinkingRectifier) Stats() RectifierStats
```

**Process 逻辑:**
1. 尝试 JSON unmarshal body
2. 如果 `body.thinking` 是 object 且 `body.thinking.type == "adaptive"`，替换为 `mapTo` 值
3. JSON marshal 返回
4. 任何解析/序列化失败，返回原始 body 不变（安全降级）

**统计规则:**
- `Total` — 每次 Process 调用
- `Modified` — 实际发生了 adaptive → mapTo 转换
- `Passthrough` — 请求不含 thinking 字段，或 type 不是 adaptive

### 4. 注入点

文件: `internal/server/proxy_executor.go`

在 `Execute()` 方法中，`readRequestBody()` 之后、`buildTargetURL()` 之前插入:

```go
bodyBytes, err := readRequestBody(w, r)
if err != nil {
    px.recordProxyMetrics(r.Method, "4xx", "", start)
    return
}

// Rectify thinking.type before forwarding upstream
if ps.ThinkingMode() == "rectify" {
    bodyBytes = px.rectifier.Process(bodyBytes)
}

target := buildTargetURL(ps.config, r.URL.Path, r.URL.RawQuery)
```

`ProxyExecutor` 需要持有 `rectifier` 引用。在 `NewProxyExecutor` 时创建 per-provider 的 rectifier 实例。

### 5. 运行时热更新

`runtime-config` Admin API 已有 `ProviderState` getter/setter 机制，新增整流字段的 runtime-config key 映射即可:

- `thinking_mode` → `ps.SetThinkingMode(value)`
- `rectify_thinking_map_to` → `ps.SetRectifyThinkingMapTo(value)`

修改 `admin_api.go` 的 runtime-config handler，添加这两个 key 的处理。

### 6. 可观测性

- 通过日志 `slog` 输出整流统计
- 不单独增加 Prometheus metric（当前项目没有为类似功能单独加 metric 的先例）
- 在 `handleSuccess` 的日志中，如果发生了整流，在 key_name 旁附加 `rectified=true` 标记

### 7. 边界情况

| 情况 | 处理 |
|------|------|
| body 不是合法 JSON | 返回原始 body，不报错 |
| body 中无 `thinking` 字段 | 透传，统计 +1 passthrough |
| `thinking` 是 null | 透传 |
| `thinking.type` 已经是 `"enabled"` | 透传，统计 +1 passthrough |
| `thinking.type` 是其他未知值 | 透传，不修改 |
| `RectifyThinkingMapTo` 是非法值 | 降级为 `"enabled"` |
| `ThinkingMode == "default"` | 跳过整流，零开销 |

## 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/config/config.go` | 修改 | 新增字段、backward compat、DeepCopy、mergeWithDefaults |
| `internal/server/router.go` | 修改 | ProviderState getter/setter |
| `internal/server/rectifier.go` | 新建 | 整流核心逻辑 |
| `internal/server/rectifier_test.go` | 新建 | 整流器单元测试 |
| `internal/server/proxy_executor.go` | 修改 | 注入整流调用点 |
| `internal/server/admin_api.go` | 修改 | runtime-config key 映射 |
| `internal/server/admin_api_test.go` | 修改 | runtime-config 测试补充 |

## 测试策略

### rectifier_test.go（7 个 table-driven 测试）

1. `adaptive → enabled` 转换成功
2. `adaptive → auto` 转换成功
3. `adaptive → disabled` 转换成功
4. 非 adaptive 值（如 `"enabled"`）透传不修改
5. 无 `thinking` 字段的请求体透传
6. 非法 JSON 安全降级（返回原始 body）
7. `ThinkingMode == "default"` 跳过整流

### proxy_executor_test.go 补充

1. 整流开启时，请求体中的 adaptive 被替换后转发
2. 整流关闭时，请求体原样转发

## 非目标

- 不做响应体的 thinking 字段修改（Python 原版也只改请求）
- 不做 SSE 流式修改（复杂度高，当前需求不涉及）
- 不做任意 JSON path 的通用修改（只针对 `thinking.type`）
- 不增加 CLI 子命令（通过 TOML + runtime-config 管理已足够）
