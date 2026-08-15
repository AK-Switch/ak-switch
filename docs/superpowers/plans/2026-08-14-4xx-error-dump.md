# 4xx 错误请求/响应落盘实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 当 4xx 不可重试错误发生时，将请求体 + 响应体 + 上下文元数据自动持久化到 `~/.akswitch/errors/` 目录下，方便用户逐条溯源。

**Architecture:** 在 `crash.go` 中新增 `ErrorLogDir` 常量和 `SetupErrorLogDir()` 辅助函数，复用 `.akswitch` 目录约定。新增 `error_dump.go` 文件，包含 `writeErrorDump()` 独立函数——负责文件名构造、内容格式化、原子写入。修改 `handleNonRetryable` 签名增加 `rectified` 参数，重写为读 body → 写文件 → 转发客户端 → 打日志。

**Tech Stack:** Go 1.26, stdlib (os, path/filepath, time, fmt, encoding/json, strings)

## 全局约束

- 存储路径：`~/.akswitch/errors/`（复用 crash.go 的 CrashLogDir 约定）
- 文件名格式：`{status}-{YYYYMMDD}-{HHMMSS纳秒}-{keyPrefix}.txt`（纳秒避免冲突）
- 触发条件：全部 non-retryable 4xx（400/422/413/414/415/405/406/501）
- 敏感信息：存原始内容（API key 在 Authorization header 中，不在请求体中）
- 写入失败时静默 fallback（slog.Warn 记录，不影响客户端）
- 测试注入临时目录，不触碰真实 `~/.akswitch/`
- 提交前执行 `make check && make test-unit`

---

## 文件结构

- Modify: `internal/server/crash.go` — 新增 `ErrorLogDir` 常量 + `defaultErrorLogDir()` + `SetupErrorLogDir()`
- Create: `internal/server/error_dump.go` — 新增 `sanitizeKeyPrefix()` + `errorDumpFilename()` + `writeErrorDump()`
- Modify: `internal/server/proxy_executor.go` — `handleNonRetryable` 签名加 `rectified` 参数，重写体；`Execute` 调用处传 `rectified`
- Test: `internal/server/crash_test.go`、`internal/server/error_dump_test.go`（新增）、`internal/server/proxy_executor_test.go`

### 关键签名

```go
// crash.go 新增
const ErrorLogDir = "errors"
func defaultErrorLogDir() string                     // ~/.akswitch/errors
func SetupErrorLogDir() string                       // 确保目录存在，返回路径

// error_dump.go 新增
func sanitizeKeyPrefix(keyName string) string                          // 清非法字符取前8，空则 "key"
func errorDumpFilename(status int, ts time.Time, prefix string) string // {status}-{纳秒时间戳}-{prefix}.txt
func writeErrorDump(dir string, ps *ProviderState, keyName, method,
    target string, status, round int, start time.Time,
    reqBody, respBody []byte, rectified bool) error                   // 原子写入，返回错误由调用方处理

// proxy_executor.go 修改
func (px *ProxyExecutor) handleNonRetryable(w http.ResponseWriter, ps *ProviderState, idx int,
    resp *http.Response, start time.Time, method, target string,
    bodyBytes []byte, attempt int, rectified bool)                    // 新增 rectified bool
```

---

### Task 1: crash.go — ErrorLogDir 常量与辅助函数

**Files:**
- Modify: `internal/server/crash.go`（现有常量区，第 11-24 行附近追加）
- Test: `internal/server/crash_test.go`（文件末尾追加）

**Interfaces:**
- Consumes: 现有 `CrashLogDir` 常量（第 12 行，值 `.akswitch`）
- Produces: `ErrorLogDir` 常量、`defaultErrorLogDir() string`、`SetupErrorLogDir() string`——供 Task 2 的 `writeErrorDump()` 使用

- [ ] **Step 1: 写测试 `TestDefaultErrorLogDir`**

在 `internal/server/crash_test.go` 末尾追加：

```go
func TestDefaultErrorLogDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	got := defaultErrorLogDir()
	want := filepath.Join(tmp, ".akswitch", "errors")
	if got != want {
		t.Errorf("defaultErrorLogDir() = %q, want %q", got, want)
	}
}

func TestSetupErrorLogDir_CreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	dir := SetupErrorLogDir()
	if !strings.HasSuffix(dir, filepath.Join(".akswitch", "errors")) {
		t.Errorf("SetupErrorLogDir() = %q, want suffix %q", dir, filepath.Join(".akswitch", "errors"))
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("error log dir not created: %s", dir)
	}
}
```

需要的新 imports：`path/filepath`（已有）、`strings`、`os`（检查现有 crash_test.go 头部——已有 os、path/filepath、testing，缺 strings 则加上）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -tags=unit -count=1 -short -run 'TestDefaultErrorLogDir|TestSetupErrorLogDir' ./internal/server/`
Expected: FAIL，编译错误 "undefined: defaultErrorLogDir"

- [ ] **Step 3: 实现 crash.go 辅助函数**

在 `internal/server/crash.go` 的 `SetupCrashLogDir()` 之后追加：

```go
// ErrorLogDir is the subdirectory under the user's home/config dir for error dumps.
const ErrorLogDir = "errors"

// defaultErrorLogDir returns the default error dump directory
// (~/.akswitch/errors), matching CrashLogDir's home-based convention.
func defaultErrorLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ErrorLogDir
	}
	return filepath.Join(home, CrashLogDir, ErrorLogDir)
}

// SetupErrorLogDir ensures the error dump directory exists.
// Returns the directory path.
func SetupErrorLogDir() string {
	dir := defaultErrorLogDir()
	_ = os.MkdirAll(dir, 0755)
	return dir
}
```

（`os`、`path/filepath` 已在 crash.go 头部 import）

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -tags=unit -count=1 -short -run 'TestDefaultErrorLogDir|TestSetupErrorLogDir' ./internal/server/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/crash.go internal/server/crash_test.go
git commit -m "feat: add error dump directory helpers (ErrorLogDir under ~/.akswitch/errors)"
```

---

### Task 2: error_dump.go — sanitizeKeyPrefix + errorDumpFilename + writeErrorDump

**Files:**
- Create: `internal/server/error_dump.go`
- Test: `internal/server/error_dump_test.go`

**Interfaces:**
- Consumes: Task 1 的 `ProviderState` getter（`ps.Name()`、`ps.ThinkingMode()`、`ps.RectifyThinkingMapTo()`）
- Produces: `sanitizeKeyPrefix(keyName string) string`、`errorDumpFilename(status int, ts time.Time, prefix string) string`、`writeErrorDump(dir string, ps *ProviderState, keyName, method, target string, status, round int, start time.Time, reqBody, respBody []byte, rectified bool) error`——供 Task 3 的 `handleNonRetryable` 使用

- [ ] **Step 1: 写测试 `error_dump_test.go`**

创建 `internal/server/error_dump_test.go`：

```go
//go:build unit

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizeKeyPrefix_KeepsAlnum(t *testing.T) {
	got := sanitizeKeyPrefix("联通-735s4pv")
	if got != "735s4pv" {
		t.Errorf("sanitizeKeyPrefix(联通-735s4pv) = %q, want 735s4pv", got)
	}
}

func TestSanitizeKeyPrefix_TruncatesTo8(t *testing.T) {
	got := sanitizeKeyPrefix("abcdefghijklmn")
	if len(got) > 8 {
		t.Errorf("sanitizeKeyPrefix long = %q, len > 8", got)
	}
}

func TestSanitizeKeyPrefix_EmptyFallback(t *testing.T) {
	if got := sanitizeKeyPrefix("联通 中文！！"); got != "key" {
		t.Errorf("sanitizeKeyPrefix(only-cjk) = %q, want key", got)
	}
}

func TestErrorDumpFilename_Format(t *testing.T) {
	ts := time.Date(2026, 8, 14, 20, 21, 48, 123456789, time.UTC)
	got := errorDumpFilename(400, ts, "735s4pv")
	if !strings.HasPrefix(got, "400-20260814-202148") || !strings.HasSuffix(got, "-735s4pv.txt") {
		t.Errorf("errorDumpFilename = %q, want prefix/suffix match", got)
	}
}

func TestWriteErrorDump_WritesFileWithBodies(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a"})
	ps.SetThinkingMode("rectify")
	ps.SetRectifyThinkingMapTo("auto")
	dir := t.TempDir()

	err := writeErrorDump(dir, ps, "key-a", "POST", "/v1/messages", 400, 2,
		time.Now(), []byte(`{"thinking":{"type":"adaptive"}}`), []byte(`{"error":"bad"}`), true)
	if err != nil {
		t.Fatalf("writeErrorDump: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 file in %s, got %v (err=%v)", dir, entries, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"Provider:  test",
		"Key:       key-a",
		"URL:       /v1/messages",
		"Status:    400",
		"Round:     2",
		`Rectifier: enabled (mapTo: auto)`,
		"Rectified: true",
		`{"thinking":{"type":"adaptive"}}`,
		`{"error":"bad"}`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("dump missing %q in:\n%s", want, content)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -tags=unit -count=1 -short -run 'TestSanitizeKeyPrefix|TestErrorDumpFilename|TestWriteErrorDump' ./internal/server/`
Expected: FAIL，编译错误 "undefined: sanitizeKeyPrefix"

- [ ] **Step 3: 实现 `error_dump.go`**

创建 `internal/server/error_dump.go`：

```go
package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sanitizeKeyPrefix reduces a key name to a filesystem-safe prefix:
// keeps [A-Za-z0-9_-], truncates to 8 chars, falls back to "key".
func sanitizeKeyPrefix(keyName string) string {
	var b strings.Builder
	for _, r := range keyName {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	prefix := b.String()
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	if prefix == "" {
		return "key"
	}
	return prefix
}

// errorDumpFilename builds the dump filename: {status}-{YYYYMMDD}-{HHMMSS纳秒}-{keyPrefix}.txt.
// Nanosecond precision avoids collisions between concurrent requests.
func errorDumpFilename(status int, ts time.Time, prefix string) string {
	return fmt.Sprintf("%d-%s%09d-%s.txt", status, ts.Format("20060102-150405"), ts.Nanosecond(), prefix)
}

// writeErrorDump persists a non-retryable 4xx request/response pair to dir.
// Writes atomically (temp file + rename) so a partially-written dump is
// never observed mid-flight. Returns an error; the caller decides how to log it.
func writeErrorDump(dir string, ps *ProviderState, keyName, method, target string, status, round int, start time.Time, reqBody, respBody []byte, rectified bool) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("writeErrorDump: mkdir %s: %w", dir, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Time:      %s\n", start.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "Provider:  %s\n", ps.Name())
	fmt.Fprintf(&b, "Key:       %s\n", keyName)
	fmt.Fprintf(&b, "URL:       %s\n", target)
	fmt.Fprintf(&b, "Status:    %d\n", status)
	fmt.Fprintf(&b, "Round:     %d\n", round)
	fmt.Fprintf(&b, "Duration:  %.1fs\n", time.Since(start).Seconds())
	if ps.ThinkingMode() == "rectify" {
		fmt.Fprintf(&b, "Rectifier: enabled (mapTo: %s)\n", ps.RectifyThinkingMapTo())
	} else {
		fmt.Fprintf(&b, "Rectifier: default\n")
	}
	fmt.Fprintf(&b, "Rectified: %t\n\n", rectified)

	fmt.Fprintf(&b, "--- Request Body ---\n")
	b.Write(reqBody)
	b.WriteString("\n\n--- Response Body ---\n")
	b.Write(respBody)
	b.WriteString("\n")

	path := filepath.Join(dir, errorDumpFilename(status, start, sanitizeKeyPrefix(keyName)))

	// Atomic write: temp file + rename (Windows rename fails if target exists).
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("writeErrorDump: write temp: %w", err)
	}
	_ = os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("writeErrorDump: rename: %w", err)
	}
	return nil
}
```

> **注意**：`filename` 用 `start`（请求起始时间）而非 `time.Now()`——保证同一请求的重试轮次生成相同文件名（现有行为：`start` 贯穿整个 `Execute`）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -tags=unit -count=1 -short -run 'TestSanitizeKeyPrefix|TestErrorDumpFilename|TestWriteErrorDump' ./internal/server/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/error_dump.go internal/server/error_dump_test.go
git commit -m "feat: add writeErrorDump for persistent 4xx request/response dumps"
```

---

### Task 3: proxy_executor.go — handleNonRetryable 重写 + Execute 调用更新

**Files:**
- Modify: `internal/server/proxy_executor.go`（`handleNonRetryable` 第 250-263 行；`Execute` 第 145-147 行）
- Test: `internal/server/proxy_executor_test.go`（`TestHandleNonRetryable_PassthroughStatus` 第 211-223 行）

**Interfaces:**
- Consumes: Task 1 的 `SetupErrorLogDir()`、Task 2 的 `writeErrorDump()`
- Produces: 新的 `handleNonRetryable` 签名（新增 `rectified bool` 参数），`Execute` 中传入 `rectified` 变量

- [ ] **Step 1: 更新现有测试 `TestHandleNonRetryable_PassthroughStatus` 适配新签名**

修改 `internal/server/proxy_executor_test.go` 第 218 行调用，补上 `rectified` 参数（该测试验证的是状态码透传，`rectified` 先传 `false`）：

```go
	px.handleNonRetryable(w, ps, 0, resp, testStartTime(), "GET", "http://upstream/", []byte(`{"error":"bad request"}`), 0, false)
```

同一测试里在 `newTestProviderState` 之后加 `t.Setenv` 重定向 home（避免写入真实 `~/.akswitch/`）：

```go
	ps := newTestProviderState(t, "test", []string{"key-a"})
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
```

- [ ] **Step 2: 运行测试确认编译错误（未实现前）**

Run: `go test -tags=unit -count=1 -short -run 'TestHandleNonRetryable' ./internal/server/`
Expected: FAIL，编译错误 "not enough arguments in call to px.handleNonRetryable"

- [ ] **Step 3: 重写 `handleNonRetryable`**

替换 `internal/server/proxy_executor.go` 第 250-263 行：

```go
// handleNonRetryable copies a non-retryable 4xx response through to the client
// without further retry attempts. It also persists the request/response pair
// to ~/.akswitch/errors/ for post-hoc debugging (e.g. rectifier 400s).
func (px *ProxyExecutor) handleNonRetryable(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, method, target string, bodyBytes []byte, attempt int, rectified bool) {
	defer func() { _ = resp.Body.Close() }()
	keyName, _ := ps.PoolName(idx)

	// Read response body for persistence and logging (client still gets full body)
	body, _ := io.ReadAll(resp.Body)

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)

	if err := writeErrorDump(SetupErrorLogDir(), ps, keyName, method, target, resp.StatusCode, attempt, start, bodyBytes, body, rectified); err != nil {
		slog.Warn("failed to write error dump", "provider", ps.Name(), "status", resp.StatusCode, "error", err)
	}

	slog.Warn("non-retryable client error", "provider", ps.Name(), "method", method, "url", target, "status", resp.StatusCode, "key_name", keyName, "body_preview", MaskSensitiveData(string(body), 1024))
	slog.Debug("proxy response debug", "status", resp.StatusCode, "duration_ms", time.Since(start).Seconds()*1000, "retries", attempt+1)
	px.recordProxyMetrics(method, "4xx", fmt.Sprintf("%d", idx), start)
	if attempt > 0 {
		px.metrics.RetryCount.WithLabelValues(ps.Name()).Add(float64(attempt))
	}
}
```

> 注：`io`、`slog`、`fmt`、`time` 已在 proxy_executor.go 头部 import。

- [ ] **Step 4: 更新 `Execute` 中调用处传 `rectified`**

`internal/server/proxy_executor.go` 第 145-147 行，把 `rectified` 加入调用参数：

```go
			case (resp.StatusCode >= 400 && resp.StatusCode < 500) || categorizeError(resp.StatusCode, nil) == CatNonRetryable:
				px.handleNonRetryable(w, ps, idx, resp, start, r.Method, target, bodyBytes, round, rectified)
				return
```

（`rectified` 变量在 `Execute` 第 51-55 行已定义：`var rectified bool`，在 rectifier 分支赋值。）

- [ ] **Step 5: 运行测试确认通过**

Run: `go test -tags=unit -count=1 -short -run 'TestHandleNonRetryable' ./internal/server/`
Expected: PASS

- [ ] **Step 6: 运行全部 server 单测**

Run: `go test -tags=unit -count=1 -short ./internal/server/`
Expected: 全部通过（无回归）

- [ ] **Step 7: Commit**

```bash
git add internal/server/proxy_executor.go internal/server/proxy_executor_test.go
git commit -m "feat: persist request/response dump on non-retryable 4xx in handleNonRetryable"
```

---

### Task 4: Execute 级测试——4xx 落盘 + 客户端仍拿全响应体

**Files:**
- Test: `internal/server/proxy_executor_test.go`（文件末尾追加）

**Interfaces:**
- Consumes: Task 2 的 `writeErrorDump`、Task 3 的 `handleNonRetryable` 新逻辑，现有测试助手 `newTestProviderState`、`newProxyExecutor`
- Produces: 端到端验证（无新生产代码）

- [ ] **Step 1: 写测试**

在 `internal/server/proxy_executor_test.go` 末尾追加：

```go
// ── 4xx error dump ──────────────────────────────────

func TestExecute_NonRetryable_WritesErrorDump(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`))
	}))
	defer backend.Close()

	ps := newTestProviderState(t, "test", []string{"sk-key-0"})
	ps.config.TargetBase = backend.URL
	ps.SetThinkingMode("rectify")
	ps.SetRectifyThinkingMapTo("enabled")

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	px, _, _ := newProxyExecutor(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4","thinking":{"type":"adaptive"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	px.Execute(w, req, ps)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Verify dump file exists with both bodies + metadata.
	errorsDir := filepath.Join(tmpHome, ".akswitch", "errors")
	entries, err := os.ReadDir(errorsDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 dump in %s, got %v (err=%v)", errorsDir, entries, err)
	}
	data, _ := os.ReadFile(filepath.Join(errorsDir, entries[0].Name()))
	content := string(data)
	if !strings.Contains(content, `"thinking":{"type":"adaptive"}`) {
		t.Errorf("dump missing request body:\n%s", content)
	}
	if !strings.Contains(content, `invalid_request_error`) {
		t.Errorf("dump missing response body:\n%s", content)
	}
	if !strings.Contains(content, "Rectified: true") {
		t.Errorf("dump missing rectified flag:\n%s", content)
	}
}

func TestExecute_NonRetryable_ClientStillGetsFullBody(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"full upstream error detail"}`))
	}))
	defer backend.Close()

	ps := newTestProviderState(t, "test", []string{"sk-key-0"})
	ps.config.TargetBase = backend.URL

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	px, _, _ := newProxyExecutor(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	px.Execute(w, req, ps)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if got := w.Body.String(); !strings.Contains(got, "full upstream error detail") {
		t.Errorf("client body lost upstream error, got: %q", got)
	}
}
```

新增 imports（proxy_executor_test.go 头部）：`os`、`path/filepath`（现有 `io`、`strings`、`net/http/httptest` 已用）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -tags=unit -count=1 -short -run 'TestExecute_NonRetryable' ./internal/server/`
Expected: FAIL（先失败再实现是 TDD 关键；若 Task 3 已实现则应直接通过）

> 若按顺序执行 Task 2→3→4，此时测试应已通过——该步验证的是"端到端链路打通"。若 Task 3 尚未实现，此步失败是预期的。

- [ ] **Step 3: 运行全部 server 单测**

Run: `go test -tags=unit -count=1 -short ./internal/server/`
Expected: 全部通过

- [ ] **Step 4: 运行完整验证链**

```bash
go vet ./...
gofmt -l internal/server/
go test -tags=unit -count=1 -short ./internal/...
```

Expected: vet 无输出、gofmt 无文件列出、测试全绿

- [ ] **Step 5: Commit**

```bash
git add internal/server/proxy_executor_test.go
git commit -m "test: verify 4xx error dump persists bodies and client still receives full response"
```