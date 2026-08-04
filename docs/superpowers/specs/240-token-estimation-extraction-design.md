# #240 Token 估算逻辑提取 — 设计文档

## 问题

`proxy_executor.go` 的 `streamSSEAndEstimateTokens`（98 行）内联了完整的 SSE 解析逻辑，与 `tokenestimator` 包已有能力重叠。新增 SSE 格式需要同时修改两个文件，容易遗漏。

## 目标

将 SSE 解析提取到 `tokenestimator` 包，`proxy_executor` 只做循环调度和文本累积。

## 设计

### 新增函数

在 `tokenestimator/tokenestimator.go` 新增：

```go
// ParseSSEEvent parses a single SSE "data: " event line and returns
// the extracted text delta and output token delta.
// Returns (0, "") for non-data lines, unrecognized JSON, or events
// that don't carry text/token information.
// Does not return errors — unrecognized input is silently ignored
// to keep the streaming loop clean.
func ParseSSEEvent(raw []byte) (outputTokens int, textDelta string)
```

**职责：** 单条 SSE 事件 → 提取文本和 token 信息。无状态，纯函数。

**解析的事件类型：**
- Anthropic `content_block_delta` → `delta.text` + `delta.partial_json`
- Anthropic `content_block_start` → `content_block.text`
- Anthropic `message_delta` → `usage.output_tokens`
- OpenAI `choices[].delta.content`

### 精简 streamSSEAndEstimateTokens

`proxy_executor.go` 的循环变为：

```go
for scanner.Scan() {
    line := scanner.Text()
    w.Write([]byte(line + "\n"))

    if strings.HasPrefix(line, "data: ") {
        raw := line[6:]
        tokens, delta := tokenestimator.ParseSSEEvent(raw)
        outputBuf.WriteString(delta)
        if tokens > 0 {
            apiOutputTokens = tokens
        }
    }

    if canFlush { f.Flush() }
}
```

移除 98 行中的 3 个内联 JSON 结构体定义和 switch 分支。

### 依赖关系

- `tokenestimator` 包新增的 `ParseSSEEvent` 仅依赖 `encoding/json`（已有）和 `strings`（已有），零新 import
- `proxy_executor` 新增对 `tokenestimator.ParseSSEEvent` 的调用，无新 import
- `EstimateOutput`、`EstimateInput`、`ExtractTokenUsage`、`ExtractResponseText` 不变

### 测试

| 测试 | 位置 | 覆盖场景 |
|------|------|---------|
| `TestParseSSEEvent_ContentBlockDelta` | `tokenestimator_test.go` | Anthropic text delta |
| `TestParseSSEEvent_ContentBlockStart` | `tokenestimator_test.go` | Anthropic content_block_start |
| `TestParseSSEEvent_MessageDelta` | `tokenestimator_test.go` | message_delta output_tokens |
| `TestParseSSEEvent_OpenAIFormat` | `tokenestimator_test.go` | OpenAI choices delta |
| `TestParseSSEEvent_NonDataLine` | `tokenestimator_test.go` | 非 data: 行返回 (0, "") |
| `TestParseSSEEvent_InvalidJSON` | `tokenestimator_test.go` | 无效 JSON 静默返回 |
| `TestParseSSEEvent_PartialJSON` | `tokenestimator_test.go` | partial_json 字段累积 |
| `streamSSEAndEstimateTokens` 集成测试 | `proxy_executor_test.go` | 迁移后行为不变 |

### 不变项

- `streamSSEAndEstimateTokens` 的函数签名不变
- SSE 事件处理流程不变（写客户端 → 解析 → 累积 → 估算）
- `Calibrator` 调用逻辑不变
- `handleSuccess` 中的非流式路径不变

### 实现步骤

1. 在 `tokenestimator.go` 新增 `ParseSSEEvent` 函数
2. 在 `tokenestimator_test.go` 新增 7 个测试
3. 精简 `streamSSEAndEstimateTokens` 调用新函数
4. 验证 `go test -tags=unit ./internal/tokenestimator/ ./internal/server/` 通过
