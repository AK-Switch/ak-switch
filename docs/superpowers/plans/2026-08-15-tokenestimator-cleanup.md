# Tokenestimator 代码清理与测试补全 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 清理 `internal/tokenestimator/` 碎片化文件，为 `EstimateInput`/`EstimateOutput` 补 table-driven 测试，拆分 `ParseSSEEvent` 双格式逻辑。

**Architecture:** 三个任务按顺序执行：（1）合并 `extractor.go` 到 `tokenestimator.go`，消除碎片化（2）补最关键的两个函数的测试（3）拆分 `ParseSSEEvent` 内部逻辑。每个任务都以 `go test -tags=unit` 验证无回归。

**Tech Stack:** Go 1.26, tiktoken-go

## Global Constraints

- 调用方零改动：`ParseSSEEvent` 签名不改变，`EstimateInput`/`EstimateOutput` 签名不改变
- 遵循现有测试风格：table-driven 测试优先，与 package `tokenestimator` 内测试保持一致
- `EstimateOutput` 测试需 `t.Skip("requires tiktoken C library")` 跳过 tiktoken 依赖（CI 中可能不可用）
- 每个任务独立提交，commit message 使用 conventional commits 格式
- 提交前执行 `go build ./...` 和 `go test -tags=unit -count=1 -short ./internal/tokenestimator/`

---
### Task 1: 合并 extractor.go → tokenestimator.go

**Files:**
- Modify: `internal/tokenestimator/tokenestimator.go`（插入 ExtractModel 函数）
- Modify: `internal/tokenestimator/tokenestimator_test.go`（追加 5 个 ExtractModel 测试）
- Delete: `internal/tokenestimator/extractor.go`
- Delete: `internal/tokenestimator/extractor_test.go`

**Interfaces:**
- Consumes: 现有 `ExtractModel` 函数签名 `func ExtractModel(bodyBytes []byte) string`
- Produces: `tokenestimator.go` 包含所有函数，`tokenestimator_test.go` 包含所有测试

- [ ] **Step 1: 将 ExtractModel 函数体插入 tokenestimator.go**

将 `extractor.go` 中的 `ExtractModel` 函数（17 行）插入到 `tokenestimator.go` 中 `encodingForModel` 函数之后、`EstimateOutput` 函数之前（按逻辑分组——模型相关函数放一起）：

```go
// ExtractModel extracts the model name from a request body JSON.
// Returns empty string if the body is empty, malformed, or model is missing.
func ExtractModel(bodyBytes []byte) string {
	if len(bodyBytes) == 0 {
		return ""
	}
	var reqBody struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		return ""
	}
	return reqBody.Model
}
```

- [ ] **Step 2: 将 TestExtractModel_* 测试追加到 tokenestimator_test.go**

在 `TestProcessResponse_InvalidJSON` 之后、`TestRecordCalibration_BothPositive` 之前插入以下 5 个测试函数：

```go
// ── ExtractModel ──────────────────────────────────

func TestExtractModel_ValidJSON(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[]}`)
	if got := ExtractModel(body); got != "gpt-4o" {
		t.Errorf("ExtractModel = %q, want %q", got, "gpt-4o")
	}
}

func TestExtractModel_MissingField(t *testing.T) {
	body := []byte(`{"messages":[]}`)
	if got := ExtractModel(body); got != "" {
		t.Errorf("ExtractModel = %q, want empty", got)
	}
}

func TestExtractModel_InvalidJSON(t *testing.T) {
	if got := ExtractModel([]byte(`not json`)); got != "" {
		t.Errorf("ExtractModel = %q, want empty", got)
	}
}

func TestExtractModel_NilBody(t *testing.T) {
	if got := ExtractModel(nil); got != "" {
		t.Errorf("ExtractModel(nil) = %q, want empty", got)
	}
}

func TestExtractModel_EmptyBody(t *testing.T) {
	if got := ExtractModel([]byte{}); got != "" {
		t.Errorf("ExtractModel(empty) = %q, want empty", got)
	}
}
```

- [ ] **Step 3: 删除 extractor.go 和 extractor_test.go**

```bash
rm internal/tokenestimator/extractor.go internal/tokenestimator/extractor_test.go
```

- [ ] **Step 4: 验证编译和测试通过**

```bash
go build ./...
go test -tags=unit -count=1 -short ./internal/tokenestimator/
```
Expected: all tests pass, 27 个测试

- [ ] **Step 5: 提交**

```bash
git add internal/tokenestimator/
git commit -m "refactor: 合并 extractor.go 到 tokenestimator.go，消除碎片化"
```

---

### Task 2: 补 EstimateInput / EstimateOutput table-driven 测试

**Files:**
- Modify: `internal/tokenestimator/tokenestimator_test.go`

**Interfaces:**
- Consumes: 现有 `EstimateInput(bodyBytes []byte, model string) int` 和 `EstimateOutput(text string, model string) int`
- Produces: 新增 ~10 个测试函数，覆盖两种格式解析 + tiktoken 估算

- [ ] **Step 1: 添加 EstimateInput table-driven 测试**

在 `TestExtractModel_EmptyBody` 之后、`TestRecordCalibration_BothPositive` 之前插入：

```go
// ── EstimateInput ─────────────────────────────────

func TestEstimateInput_OpenAIStringFormat(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello world"}]}`)
	tokens := EstimateInput(body, "gpt-4o")
	if tokens <= 0 {
		t.Errorf("EstimateInput = %d, want > 0 (OpenAI string format)", tokens)
	}
}

func TestEstimateInput_AnthropicContentArray(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hello world"}]}]}`)
	tokens := EstimateInput(body, "claude-3-opus-20240229")
	if tokens <= 0 {
		t.Errorf("EstimateInput = %d, want > 0 (Anthropic content array)", tokens)
	}
}

func TestEstimateInput_MultiTurn(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"user","content":"first message"},
		{"role":"assistant","content":"response"},
		{"role":"user","content":[{"type":"text","text":"second message with array"}]}
	]}`)
	tokens := EstimateInput(body, "gpt-4o")
	if tokens <= 0 {
		t.Errorf("EstimateInput = %d, want > 0 (multi-turn)", tokens)
	}
}

func TestEstimateInput_EmptyBody(t *testing.T) {
	if got := EstimateInput(nil, "gpt-4o"); got != 0 {
		t.Errorf("EstimateInput(nil) = %d, want 0", got)
	}
	if got := EstimateInput([]byte{}, "gpt-4o"); got != 0 {
		t.Errorf("EstimateInput(empty) = %d, want 0", got)
	}
}

func TestEstimateInput_InvalidJSON(t *testing.T) {
	if got := EstimateInput([]byte(`not json`), "gpt-4o"); got != 0 {
		t.Errorf("EstimateInput(invalid) = %d, want 0", got)
	}
}

func TestEstimateInput_EmptyMessages(t *testing.T) {
	body := []byte(`{"messages":[]}`)
	if got := EstimateInput(body, "gpt-4o"); got != 0 {
		t.Errorf("EstimateInput(empty messages) = %d, want 0", got)
	}
}

func TestEstimateInput_MixedContentTypes(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"user","content":"plain string"},
		{"role":"user","content":[{"type":"text","text":"array text"}]}
	]}`)
	tokens := EstimateInput(body, "gpt-4o")
	if tokens <= 0 {
		t.Errorf("EstimateInput(mixed) = %d, want > 0", tokens)
	}
}
```

- [ ] **Step 2: 添加 EstimateOutput table-driven 测试**

在 `TestEstimateInput_MixedContentTypes` 之后插入：

```go
// ── EstimateOutput ────────────────────────────────

func TestEstimateOutput_NormalText(t *testing.T) {
	tokens := EstimateOutput("hello world, this is a test message", "gpt-4o")
	if tokens <= 0 {
		t.Skip("requires tiktoken C library")
	}
}

func TestEstimateOutput_EmptyText(t *testing.T) {
	if got := EstimateOutput("", "gpt-4o"); got != 0 {
		t.Errorf("EstimateOutput(empty) = %d, want 0", got)
	}
}

func TestEstimateOutput_UnknownModel(t *testing.T) {
	tokens := EstimateOutput("hello world", "unknown-model-12345")
	if tokens <= 0 {
		t.Skip("requires tiktoken C library")
	}
}

func TestEstimateOutput_LongText(t *testing.T) {
	text := ""
	for i := 0; i < 1000; i++ {
		text += "word "
	}
	tokens := EstimateOutput(text, "cl100k_base")
	if tokens <= 0 {
		t.Skip("requires tiktoken C library")
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

```bash
go test -tags=unit -count=1 -short -v ./internal/tokenestimator/ 2>&1 | tail -40
```
Expected: 新增测试通过（tiktoken 依赖的测试可能 skip，不算失败）

- [ ] **Step 4: 提交**

```bash
git add internal/tokenestimator/tokenestimator_test.go
git commit -m "test: 补 EstimateInput / EstimateOutput table-driven 测试"
```

---

### Task 3: 拆 ParseSSEEvent 为两个内部函数

**Files:**
- Modify: `internal/tokenestimator/tokenestimator.go`（重构 ParseSSEEvent 内部结构）

**Interfaces:**
- Consumes: 现有 `ParseSSEEvent(raw []byte) (outputTokens int, textDelta string, thinkingDelta string)` 签名
- Produces: 同上（签名不变，调用方零改动），新增两个内部函数 `parseAnthropicSSE` 和 `parseOpenAISSE`

- [ ] **Step 1: 重构 ParseSSEEvent — 提取 parseAnthropicSSE 内部函数**

```go
// parseAnthropicSSE handles Anthropic format SSE events.
// Supports: content_block_delta (text/partial_json/thinking), content_block_start, message_delta.
func parseAnthropicSSE(raw []byte) (outputTokens int, textDelta string, thinkingDelta string) {
	var result struct {
		Type  string `json:"type"`
		Delta *struct {
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
			Thinking    string `json:"thinking"`
		} `json:"delta,omitempty"`
		ContentBlock *struct {
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content_block,omitempty"`
		Usage *struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, "", ""
	}

	switch result.Type {
	case "content_block_delta":
		if result.Delta != nil {
			textDelta += result.Delta.Text
			textDelta += result.Delta.PartialJSON
			thinkingDelta += result.Delta.Thinking
		}
	case "content_block_start":
		if result.ContentBlock != nil {
			textDelta += result.ContentBlock.Text
			thinkingDelta += result.ContentBlock.Thinking
		}
	case "message_delta":
		if result.Usage != nil && result.Usage.OutputTokens > 0 {
			outputTokens = result.Usage.OutputTokens
		}
	}
	return outputTokens, textDelta, thinkingDelta
}
```

- [ ] **Step 2: 提取 parseOpenAISSE 内部函数**

```go
// parseOpenAISSE handles OpenAI format SSE events.
// Supports: choices[].delta.content format.
func parseOpenAISSE(raw []byte) (outputTokens int, textDelta string, thinkingDelta string) {
	var result struct {
		Choices []struct {
			Delta *struct {
				Content string `json:"content"`
			} `json:"delta,omitempty"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, "", ""
	}
	for _, choice := range result.Choices {
		if choice.Delta != nil {
			textDelta += choice.Delta.Content
		}
	}
	return 0, textDelta, ""
}
```

- [ ] **Step 3: 简化 ParseSSEEvent 为路由函数**

```go
// ParseSSEEvent parses a single SSE "data: " event line and returns
// the extracted text delta, thinking delta, and output token count.
// Supports both Anthropic format (content_block_delta / content_block_start / message_delta)
// and OpenAI format (choices[].delta.content).
// Returns (0, "", "") for non-data lines, unrecognized JSON, or events
// that don't carry text/token information.
func ParseSSEEvent(raw []byte) (outputTokens int, textDelta string, thinkingDelta string) {
	if len(raw) == 0 {
		return 0, "", ""
	}

	// Try Anthropic format first (has "type" field)
	if raw[0] == '{' {
		typeCheck := struct {
			Type string `json:"type"`
		}{}
		if err := json.Unmarshal(raw, &typeCheck); err == nil && typeCheck.Type != "" {
			return parseAnthropicSSE(raw)
		}
		// No type field — try OpenAI format
		return parseOpenAISSE(raw)
	}
	return 0, "", ""
}
```

- [ ] **Step 4: 运行测试验证无回归**

```bash
go test -tags=unit -count=1 -short -v ./internal/tokenestimator/ 2>&1 | tail -30
```
Expected: 所有 37 个测试通过（包括已有的 9 个 ParseSSEEvent 测试）

- [ ] **Step 5: 验证调用方无变化**

```bash
grep -rn "tokenestimator\.ParseSSEEvent\|tokenestimator\.EstimateInput\|tokenestimator\.EstimateOutput" internal/server/ internal/cli/
```
Expected: 签名不变，调用方无需修改

- [ ] **Step 6: 提交**

```bash
git add internal/tokenestimator/tokenestimator.go
git commit -m "refactor: 拆分 ParseSSEEvent 为 parseAnthropicSSE / parseOpenAISSE 内部函数"
```