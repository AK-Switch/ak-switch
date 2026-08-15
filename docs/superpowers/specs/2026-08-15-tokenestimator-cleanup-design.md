# Tokenestimator 代码清理与测试补全 — 设计文档

> **Spec 状态**: 待审阅
> **对应**: #300 C5
> **基于**: main 1004f2e

## 概览

对 `internal/tokenestimator/` 包进行代码清理和测试补全，消除碎片化文件，为最复杂的函数补上 table-driven 测试，拆分 `ParseSSEEvent` 的双格式逻辑。

## 任务分解

### 任务 1: 合并 `extractor.go` → `tokenestimator.go`

**现状**
- `extractor.go`（17 行）：仅包含 `ExtractModel` 一个函数，配独立的 `extractor_test.go`（38 行 / 5 个测试）
- `tokenestimator.go`（245 行）：包含 8 个导出函数 + 1 个内部函数
- `tokenestimator_test.go`（343 行）：包含 22 个测试

**操作**
- 将 `ExtractModel` 函数体从 `extractor.go` 移到 `tokenestimator.go`（放在 `encodingForModel` 之后、`EstimateOutput` 之前，按逻辑分组）
- 将 `extractor_test.go` 中的 5 个 `TestExtractModel_*` 测试加入 `tokenestimator_test.go`（放在 `ProcessResponse` 测试组之后）
- **删除** `extractor.go` 和 `extractor_test.go`

**变更后**
- `internal/tokenestimator/` 从 4 个文件 → 2 个文件
- `tokenestimator.go`：262 行，9 个导出函数 + 1 个内部函数
- `tokenestimator_test.go`：381 行，27 个测试

### 任务 2: 补 `EstimateInput` / `EstimateOutput` table-driven 测试

**现状**
- `EstimateInput`（43 行）：解析请求体，支持 OpenAI string 格式和 Anthropic content array 格式，调用 tiktoken 估算 token 数。**零测试。**
- `EstimateOutput`（10 行）：调用 tiktoken 估算文本 token 数。**零测试。**

**测试策略**

`EstimateInput` 的测试覆盖（不依赖 tiktoken，仅测试 JSON 解析逻辑）：

| case | 输入 | 预期 |
|------|------|------|
| OpenAI 单轮对话 | `{"messages":[{"role":"user","content":"hello"}]}`, "gpt-4o" | > 0 |
| Anthropic content array | `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`, "claude-3" | > 0 |
| 多轮对话 | 包含 user + assistant 的消息数组 | 正确累加 |
| 空消息 | `{"messages":[]}` | 0 |
| 空 body | `nil` | 0 |
| 无效 JSON | 非 JSON 字符串 | 0 |
| 混合 content 类型 | 部分 string + 部分 array | 正确累加 |

`EstimateOutput` 的测试策略（需要 tiktoken C 库，仅在 CI 中可用）：

| case | 输入 | 预期 |
|------|------|------|
| 正常文本 | "hello world", "gpt-4o" | > 0（需 tiktoken） |
| 空文本 | "", "gpt-4o" | 0 |
| 长文本 | 1000 字文本, "cl100k_base" | > 0（需 tiktoken） |
| 未知模型 | "hello", "unknown-model" | > 0（fallback 到 cl100k_base） |

**注意**：`EstimateOutput` 依赖 tiktoken 的 C 扩展库，在 `-tags=unit` 下可能不可用。使用 `tiktoken.GetEncoding` 失败时返回 0，所以测试需要 `t.Skip("requires tiktoken C library")` 或通过 `//go:build !unit` 跳过。

### 任务 3: 拆 `ParseSSEEvent` 为两个内部函数

**现状**
- `ParseSSEEvent`（57 行）：一个函数内处理 Anthropic SSE 和 OpenAI SSE 两种格式，5 个代码路径交织
- 已有 9 个测试函数覆盖，测试完整

**操作**
- 保留 `ParseSSEEvent` 导出函数签名不变（`(raw []byte) (outputTokens int, textDelta string, thinkingDelta string)`）
- 提取 `parseAnthropicSSE` 内部函数：处理 `content_block_delta`、`content_block_start`、`message_delta` 三种事件类型
- 提取 `parseOpenAISSE` 内部函数：处理 `choices[].delta.content` 格式
- `ParseSSEEvent` 通过 `type` 字段是否存在判断格式，路由到对应内部函数

**变更后**
```go
func ParseSSEEvent(raw []byte) (outputTokens int, textDelta string, thinkingDelta string) {
    if len(raw) == 0 {
        return 0, "", ""
    }
    // 尝试解析 — 先用 Anthropic 格式（有 type 字段）
    // 路由到 parseAnthropicSSE 或 parseOpenAISSE
}

func parseAnthropicSSE(raw []byte) (outputTokens int, textDelta string, thinkingDelta string) {
    // 处理 content_block_delta / content_block_start / message_delta
}

func parseOpenAISSE(raw []byte) (outputTokens int, textDelta string, thinkingDelta string) {
    // 处理 choices[].delta.content
}
```

**测试**
- 现有 9 个 `TestParseSSEEvent_*` 测试保持不变（测试导出函数，不经内部函数名）
- 不新增内部函数测试（内部函数通过导出函数测试覆盖）

## 验证

| 检查项 | 方式 |
|--------|------|
| 编译通过 | `go build ./...` |
| 现有测试不回归 | `go test -tags=unit -count=1 -short ./internal/tokenestimator/` |
| 新增测试通过 | 同上，预期从 27 个测试增加到 37 个（+5 个 ExtractModel + 4 个 EstimateInput + 1 个 EstimateOutput） |
| CI 全绿 | 全部 5 个 check 通过 |
| 调用方零改动 | `grep -r "tokenestimator\." internal/server/` 确认签名不变 |

## 不做的内容

- ❌ 不修改 `RecordCalibration`（需引入 tracker 依赖，scope 外）
- ❌ 不修改 `ProcessResponse`（已有测试覆盖）
- ❌ 不修改 `ExtractTokenUsage` / `ExtractResponseText`（已有测试覆盖）
- ❌ 不引入新的外部依赖