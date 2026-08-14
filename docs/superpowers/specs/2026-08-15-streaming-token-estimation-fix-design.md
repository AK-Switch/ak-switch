# 流式响应 Token 估算修复：code review 问题整改

## 背景

PR #316（流式响应 response_body_size 追踪 + ParseSSEEvent 支持 thinking_delta）的 code review 发现了 5 个问题，涉及 gofmt 合规、设计取舍和测试覆盖。本 spec 覆盖全部 5 个问题的修复方案。

## 修复清单

### 1. gofmt 修复（Finding 1、2、5）

**文件：** `internal/server/token_tracking_test.go`

**问题：**
- `TestStreamSSE_InputJsonDelta` 函数体缩进损坏（column 0 与 3-4 tabs 混用）
- import 块使用双 tab 缩进
- import 块后缺少空行

**修复：** 执行 `go tool gofmt -w internal/server/token_tracking_test.go`，一次性修复所有格式问题。

### 2. thinking/text 分开估算（Finding 3）

**动机：** 当前 `ParseSSEEvent` 将 thinking 和 text 文本合并到同一个 `textDelta` 返回值中。当 `message_delta.usage.output_tokens` 缺失时，fallback 路径 `EstimateOutput(outputBuf.String(), model)` 对 thinking + text 联合估算。thinking（推理链）通常远长于最终回复，导致 output_tokens 严重高估，进而影响校准（calibration）和指标。

**改动：**

#### `internal/tokenestimator/tokenestimator.go`

`ParseSSEEvent` 签名增加 `thinkingDelta` 返回值：

```go
func ParseSSEEvent(raw []byte) (outputTokens int, textDelta string, thinkingDelta string)
```

- `type == "content_block_delta"`：`delta.thinking` 写入 `thinkingDelta`，`delta.text` 和 `delta.partial_json` 仍写入 `textDelta`
- `type == "content_block_start"`：`content_block.thinking` 写入 `thinkingDelta`，`content_block.text` 仍写入 `textDelta`
- `type == "message_delta"`：`usage.output_tokens` 仍写入 `outputTokens`（不改变）
- `type == "openai"`（无 `type` 字段）：`choices[].delta.content` 仍写入 `textDelta`

#### `internal/server/proxy_executor.go`

`streamSSEAndEstimateTokens` 增加 `thinkingBuf`：

```go
var outputBuf, thinkingBuf strings.Builder
```

调用 `ParseSSEEvent` 后分别写入：

```go
tokens, textDelta, thinkingDelta := tokenestimator.ParseSSEEvent([]byte(raw))
outputBuf.WriteString(textDelta)
thinkingBuf.WriteString(thinkingDelta)
if tokens > 0 {
    apiOutputTokens = tokens
}
```

fallback 路径只对 `outputBuf` 估算：

```go
outputTokens = tokenestimator.EstimateOutput(outputBuf.String(), model)
```

### 3. 测试覆盖补充（Finding 4）

**文件：** `internal/server/token_tracking_test.go`

`TestStreamSSE_RespBodySize` 新增三个子测试：

| 子测试 | 场景 | 断言 |
|--------|------|------|
| `部分写入` | `w.Write` 返回 error 后流中断 | `respBodySize` 为部分写入字节数，非 0 且非完整 |
| `\r\n 结尾` | SSE 数据行以 `\r\n` 结尾 | `respBodySize` 包含 `\r` 的 1 字节 |
| `空流` | 空响应体 | `respBodySize` 为 0 |

注：部分写入测试需要一个模拟 `ResponseWriter`，在特定次数后返回 error。使用自定义 `http.ResponseWriter` 包装器实现。

## 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/tokenestimator/tokenestimator.go` | 修改 | `ParseSSEEvent` 签名增加 `thinkingDelta` 返回值 |
| `internal/tokenestimator/tokenestimator_test.go` | 修改 | 适配新签名的测试用例 |
| `internal/server/proxy_executor.go` | 修改 | `streamSSEAndEstimateTokens` 增加 `thinkingBuf` |
| `internal/server/token_tracking_test.go` | 修改 | gofmt + 新增测试 |

## 验证

提交前执行：

```bash
make check && make test-unit
```

- `make check` 必须通过（gofmt + vet + lint 无报错）
- 单元测试全部通过