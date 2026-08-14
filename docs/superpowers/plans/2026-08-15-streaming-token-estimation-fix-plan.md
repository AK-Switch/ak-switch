# 流式 Token 估算修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 PR #316 code review 发现的 5 个问题：gofmt 合规、thinking/text 分开估算、测试覆盖补充

**Architecture:** 4 个独立修复任务，按依赖顺序执行。Task 1（gofmt）和 Task 4（测试覆盖）可并行于 Task 2/3。Task 2（ParseSSEEvent 签名扩展）是 Task 3（thinkingBuf 分离）的前置依赖。

**Tech Stack:** Go 1.26, net/http/httptest, bufio, strings

## Global Constraints

- 禁止直接访问 `ProviderState` 私有字段
- Error 包装使用 `fmt.Errorf("函数名: %w", err)`
- 提交前必须执行 `make check` 和 `make test-unit`
- 单元测试使用 `-tags=unit` 构建标签
- Tab 缩进，不引入外部框架
- 使用 `mcp__filesystem__modify_file` 工具进行编辑（`Read`/`Edit` 内置工具已禁用），注意 `\n` 在 replace 参数中会被解释为换行符

---

### Task 1: gofmt 修复 token_tracking_test.go

**Files:**
- Modify: `internal/server/token_tracking_test.go`

**Interfaces:**
- Consumes: none
- Produces: 格式正确的测试文件，`make check` 通过

- [ ] **Step 1: 运行 gofmt 格式化文件**

    执行：`go tool gofmt -w internal/server/token_tracking_test.go`

- [ ] **Step 2: 验证 gofmt 未引入语义变化**

    执行：`git diff internal/server/token_tracking_test.go`
    确认：只改动了缩进/空格/空行，没有代码逻辑变化

- [ ] **Step 3: 运行 `make check` 确认 gofmt 通过**

    执行：`make check`
    预期：gofmt 步骤无报错

- [ ] **Step 4: 单元测试确认通过**

    执行：`go test -tags=unit -count=1 -short -run TestStreamSSE_ ./internal/server/`
    预期：全部 PASS

- [ ] **Step 5: 提交**

    ```bash
    git add internal/server/token_tracking_test.go
    git commit -m "style: gofmt 修复 token_tracking_test.go 缩进和空行问题"
    ```

---

### Task 2: ParseSSEEvent 签名扩展 — 增加 thinkingDelta 返回值

**Files:**
- Modify: `internal/tokenestimator/tokenestimator.go:164-220`
- Modify: `internal/tokenestimator/tokenestimator_test.go:145-258`
- Modify: `internal/server/proxy_executor.go:402-406`

**Interfaces:**
- Consumes: none
- Produces: `func ParseSSEEvent(raw []byte) (outputTokens int, textDelta string, thinkingDelta string)`

**依赖：** 无（不需要等待 Task 1）

- [ ] **Step 1: 运行测试确认当前失败状态（TDD）**

    执行：`go test -tags=unit -count=1 -short -run TestParseSSEEvent ./internal/tokenestimator/`
    预期：当前全部 PASS

- [ ] **Step 2: 修改 ParseSSEEvent 签名，增加 thinkingDelta 返回值**

    `internal/tokenestimator/tokenestimator.go` 第 164 行：
    修改前：`func ParseSSEEvent(raw []byte) (outputTokens int, textDelta string)`
    修改后：`func ParseSSEEvent(raw []byte) (outputTokens int, textDelta string, thinkingDelta string)`

    修改 `content_block_delta` case（line 203-208）：
    ```go
    case "content_block_delta":
        if result.Delta != nil {
            textDelta += result.Delta.Text
            textDelta += result.Delta.PartialJSON
            thinkingDelta += result.Delta.Thinking  // 从 textDelta 移到 thinkingDelta
        }
    ```

    修改 `content_block_start` case（line 209-213）：
    ```go
    case "content_block_start":
        if result.ContentBlock != nil {
            textDelta += result.ContentBlock.Text
            thinkingDelta += result.ContentBlock.Thinking  // 从 textDelta 移到 thinkingDelta
        }
    ```

    `message_delta` 和 OpenAI 格式不变化。

- [ ] **Step 3: 更新所有 ParseSSEEvent 调用者**

    **`tokenestimator_test.go`：**
    - 所有 `ParseSSEEvent` 调用处改为接收 3 个返回值
    - 测试 `thinkingDelta` 返回值的断言：
      - `TestParseSSEEvent_ThinkingDelta`：`_, delta` → `_, _, delta`，验证 `delta == "Let me think step by step"`
      - `TestParseSSEEvent_ContentBlockStartThinking`：同上
      - `TestParseSSEEvent_ThinkingAndTextDelta`：`_, delta` → `_, textDelta, thinkingDelta`，验证 `textDelta == "answer"` 且 `thinkingDelta == "reasoning"`
      - `TestParseSSEEvent_ThinkingAndTextCombined`：`tokens, delta := ParseSSEEvent` → `tokens, textDelta, thinkingDelta := ParseSSEEvent`，`accumulated += delta` → `accumulated += textDelta + thinkingDelta`

    **`proxy_executor.go`：**
    - 第 405 行：`tokens, delta := tokenestimator.ParseSSEEvent(...)` → `tokens, textDelta, thinkingDelta := tokenestimator.ParseSSEEvent(...)`
    - 第 406 行：`outputBuf.WriteString(delta)` → 暂保持 `outputBuf.WriteString(textDelta + thinkingDelta)`，Task 3 会分离 thinkingBuf

- [ ] **Step 4: 运行测试确认通过**

    执行：`go test -tags=unit -count=1 -short ./internal/tokenestimator/`
    预期：全部 PASS

    执行：`go test -tags=unit -count=1 -short ./internal/server/`
    预期：全部 PASS

- [ ] **Step 5: 提交**

    ```bash
    git add internal/tokenestimator/tokenestimator.go internal/tokenestimator/tokenestimator_test.go internal/server/proxy_executor.go
    git commit -m "refactor: ParseSSEEvent 返回 thinkingDelta，与 textDelta 分离"
    ```

---

### Task 3: streamSSEAndEstimateTokens thinkingBuf 分离

**Files:**
- Modify: `internal/server/proxy_executor.go:371-427`

**Interfaces:**
- Consumes: `ParseSSEEvent(raw []byte) (outputTokens int, textDelta string, thinkingDelta string)` from Task 2
- Produces: fallback 路径只对 outputBuf 估算，不再包含 thinking 文本

**依赖：** 等待 Task 2 完成

- [ ] **Step 1: 增加 thinkingBuf 变量**

    `proxy_executor.go` 第 374 行附近：
    修改前：`var outputBuf strings.Builder`
    修改后：`var outputBuf, thinkingBuf strings.Builder`

- [ ] **Step 2: 调用 ParseSSEEvent 后分别写入**

    第 405-406 行：
    修改前：
    ```go
    tokens, textDelta, thinkingDelta := tokenestimator.ParseSSEEvent([]byte(raw))
    outputBuf.WriteString(textDelta + thinkingDelta)
    ```
    修改后：
    ```go
    tokens, textDelta, thinkingDelta := tokenestimator.ParseSSEEvent([]byte(raw))
    outputBuf.WriteString(textDelta)
    thinkingBuf.WriteString(thinkingDelta)
    ```

- [ ] **Step 3: fallback 路径只对 outputBuf 估算**

    第 424 行：
    修改前：`outputTokens = tokenestimator.EstimateOutput(outputBuf.String(), model)`
    修改后：保持不变（但 outputBuf 现在只包含 textDelta，不再含 thinkingDelta）

- [ ] **Step 4: 运行测试确认通过**

    执行：`go test -tags=unit -count=1 -short ./internal/server/`
    预期：全部 PASS

- [ ] **Step 5: 验证 thinking/text 分离效果**

    执行：`go test -tags=unit -count=1 -short -run TestStreamSSE_ThinkingAndTextCombined ./internal/tokenestimator/ -v`
    预期：`accumulated text` 包含 "Let me think step by stepHere is the answer"
    确认：thinking 和 text 分别在不同的 buffer 中累积

- [ ] **Step 6: 提交**

    ```bash
    git add internal/server/proxy_executor.go
    git commit -m "fix: streamSSEAndEstimateTokens thinkingBuf 分离，fallback 只估算 text 部分"
    ```

---

### Task 4: 测试覆盖补充 — respBodySize 错误场景

**Files:**
- Modify: `internal/server/token_tracking_test.go`

**Interfaces:**
- Consumes: `streamSSEAndEstimateTokens(w, resp, bodyBytes, model) (int, int, int64)` 函数
- Produces: 3 个新子测试覆盖写入错误、`\r\n` 结尾、空流场景

**依赖：** 无（不依赖 Task 2/3 的语义变化，但需要等待 Task 1 的 gofmt 修复避免冲突）

- [ ] **Step 1: 在 TestStreamSSE_RespBodySize 中新增 3 个子测试**

    **子测试 1：部分写入场景**

    使用自定义 `http.ResponseWriter` 包装器：
    ```go
    type failAfterWriter struct {
        http.ResponseWriter
        writeCount int
        failAfter  int
    }

    func (w *failAfterWriter) Write(b []byte) (int, error) {
        w.writeCount++
        if w.writeCount > w.failAfter {
            return 0, io.ErrUnexpectedEOF
        }
        return w.ResponseWriter.Write(b)
    }
    ```

    测试代码：
    ```go
    t.Run("write_error_mid_stream", func(t *testing.T) {
        inner := httptest.NewRecorder()
        w := &failAfterWriter{ResponseWriter: inner, failAfter: 1}
        sseData := "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
            "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n"
        respBody := io.NopCloser(strings.NewReader(sseData))
        resp := &http.Response{Body: respBody, Header: make(http.Header)}
        resp.Header.Set("Content-Type", "text/event-stream")

        _, _, respBodySize := streamSSEAndEstimateTokens(w, resp, nil, "")

        if respBodySize <= 0 {
            t.Errorf("respBodySize = %d, want > 0 (partial write)", respBodySize)
        }
        if respBodySize >= int64(len(sseData)) {
            t.Errorf("respBodySize = %d, want < %d (should be truncated)", respBodySize, len(sseData))
        }
    })
    ```

    **子测试 2：\r\n 结尾**
    ```go
    t.Run("crlf_line_endings", func(t *testing.T) {
        w := httptest.NewRecorder()
        // SSE data with \r\n line endings
        sseData := "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\r\n\r\n"
        respBody := io.NopCloser(strings.NewReader(sseData))
        resp := &http.Response{Body: respBody, Header: make(http.Header)}
        resp.Header.Set("Content-Type", "text/event-stream")

        // Expected: scanner keeps \r in text, w.Write writes line + "\n"
        // So "data: ...\r" + "\n" = "data: ...\r\n" per line
        // The blank line is "\r" + "\n" = "\r\n"
        // Expected: len("data: {json}\r") + 1 + len("\r") + 1 = len("data: {json}\r\n") + len("\r\n")
        expectedLine := "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\r"
        expectedBlank := "\r"
        expectedSize := len(expectedLine) + 1 + len(expectedBlank) + 1

        _, _, respBodySize := streamSSEAndEstimateTokens(w, resp, nil, "")

        if respBodySize != int64(expectedSize) {
            t.Errorf("respBodySize = %d, want %d (\\r\\n endings)", respBodySize, expectedSize)
        }
    })
    ```

    **子测试 3：空流**
    ```go
    t.Run("empty_stream", func(t *testing.T) {
        w := httptest.NewRecorder()
        respBody := io.NopCloser(strings.NewReader(""))
        resp := &http.Response{Body: respBody, Header: make(http.Header)}
        resp.Header.Set("Content-Type", "text/event-stream")

        _, _, respBodySize := streamSSEAndEstimateTokens(w, resp, nil, "")

        if respBodySize != 0 {
            t.Errorf("respBodySize = %d, want 0 for empty stream", respBodySize)
        }
    })
    ```

- [ ] **Step 2: 运行测试确认通过**

    执行：`go test -tags=unit -count=1 -short -run TestStreamSSE_RespBodySize ./internal/server/ -v`
    预期：包含 4 个 PASS（原测试 + 3 个子测试）

- [ ] **Step 3: 提交**

    ```bash
    git add internal/server/token_tracking_test.go
    git commit -m "test: respBodySize 错误场景测试覆盖（写入失败、\\r\\n、空流）"
    ```

---

## 验证

按顺序执行：

```bash
make check              # gofmt + vet + lint 全部通过
go test -tags=unit -count=1 -short ./internal/tokenestimator/   # 全部 PASS
go test -tags=unit -count=1 -short ./internal/server/           # 全部 PASS
```

## 回滚方案

如果任一任务导致测试失败：
1. `git checkout -- <file>` 回退该任务的修改
2. 确认 `make check && make test-unit` 恢复通过
3. 重新执行该任务