# Error Dump 自动清理 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 error dump 文件添加按年龄自动清理机制，通过 `error_dump_max_age` 配置项控制（默认 7 天）。

**Architecture:** 新增 `cleanErrorDumps()` 函数扫描目录并删除超过 cutoff 的文件；`SetupErrorLogDir()` 增加 `maxAgeDays` 参数，启动时触发清理；`writeErrorDump()` 增加 `maxAgeDays` 参数，每次写 dump 前触发清理；`ProviderConfig` 新增 `ErrorDumpMaxAge` 字段，通过 `ProviderState` getter 传递给调用栈。

**Tech Stack:** Go 1.26, stdlib

## Global Constraints

- 遵循 AGENTS.md 中 ProviderState 封装模式：所有字段通过 getter 访问
- 遵循现有 config 默认值约定：`default` struct tag + `DefaultProviderConfig()` + `mergeDefaults()`
- 清理函数 best-effort：读/删失败只打 warn 日志，不抛错
- 零值或负值 fallback 到默认 7 天
- 表驱动测试优先

---

### Task 1: 配置项 + ProviderState getter

**Files:**
- Modify: `internal/config/config.go` — 新增字段 + 默认值
- Modify: `internal/server/router.go` — 新增 ProviderState getter

**Interfaces:**
- Consumes: 现有 `ProviderConfig` 结构体、`DefaultProviderConfig()` 函数、`ProviderState` 模式
- Produces: `ProviderConfig.ErrorDumpMaxAge` 字段、`DefaultProviderConfig()` 中默认值 7、`ps.ErrorDumpMaxAge() int` getter

- [ ] **Step 1: config.go 新增字段**

在 `internal/config/config.go` 的 `ProviderConfig` 结构体中，紧邻 `LogMaxAge` 添加：

```go
LogMaxAge       int `toml:"log_max_age,omitempty" default:"7"`
ErrorDumpMaxAge int `toml:"error_dump_max_age,omitempty" default:"7"`
```

- [ ] **Step 2: DefaultProviderConfig 添加默认值**

```go
LogMaxAge:              7,
ErrorDumpMaxAge:        7,
CalibrationIntervalSec: 3600,
```

- [ ] **Step 3: router.go 新增 getter**

在 `internal/server/router.go` 文件末尾，`ProviderState` getter 区域添加：

```go
func (ps *ProviderState) ErrorDumpMaxAge() int { return ps.config.ErrorDumpMaxAge }
```

- [ ] **Step 4: 验证编译**

```bash
go build ./...
```

- [ ] **Step 5: 提交**

```bash
git add internal/config/config.go internal/server/router.go
git commit -m "feat: add ErrorDumpMaxAge config field and ProviderState getter"
```

---

### Task 2: crash.go — cleanErrorDumps + SetupErrorLogDir 签名变更

**Files:**
- Modify: `internal/server/crash.go` — 新增 `cleanErrorDumps`，修改 `SetupErrorLogDir` 签名
- Modify: `internal/server/crash_test.go` — 更新 `TestSetupErrorLogDir_CreatesDirectory` 调用

**Interfaces:**
- Consumes: `maxAgeDays int` 参数
- Produces: `cleanErrorDumps(dir string, maxAgeDays int)` 函数、`SetupErrorLogDir(maxAgeDays int) string` 新签名

- [ ] **Step 1: 新增 cleanErrorDumps 函数**

添加到 `internal/server/crash.go`，放在 `SetupErrorLogDir` 之后：

```go
// cleanErrorDumps removes error dump files older than maxAgeDays from dir.
// Best-effort: read/remove failures logged at warn level, not fatal.
func cleanErrorDumps(dir string, maxAgeDays int) {
	if maxAgeDays <= 0 {
		maxAgeDays = 7
	}
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				slog.Warn("failed to remove stale error dump", "file", e.Name(), "error", err)
			}
		}
	}
}
```

需要在 crash.go 的 imports 添加 `"log/slog"`。

- [ ] **Step 2: 修改 SetupErrorLogDir 签名**

```go
func SetupErrorLogDir(maxAgeDays int) string {
	dir := defaultErrorLogDir()
	_ = os.MkdirAll(dir, 0755)
	cleanErrorDumps(dir, maxAgeDays)
	return dir
}
```

- [ ] **Step 3: 更新 TestSetupErrorLogDir_CreatesDirectory**

```go
dir := SetupErrorLogDir(7)
```

- [ ] **Step 4: 验证编译**

```bash
go build ./...
```

- [ ] **Step 5: 提交**

```bash
git add internal/server/crash.go internal/server/crash_test.go
git commit -m "feat: add cleanErrorDumps and update SetupErrorLogDir signature"
```

---

### Task 3: error_dump.go — writeErrorDump 清理触发

**Files:**
- Modify: `internal/server/error_dump.go` — 签名新增 `maxAgeDays`，写入前调用 `cleanErrorDumps`

**Interfaces:**
- Consumes: `cleanErrorDumps(dir string, maxAgeDays int)`（Task 2）
- Produces: `writeErrorDump(dir string, ps, keyName, method, target string, status, round int, start time.Time, reqBody, respBody []byte, rectified bool, maxAgeDays int) error` 新签名

- [ ] **Step 1: 修改 writeErrorDump 签名**

```go
func writeErrorDump(dir string, ps *ProviderState, keyName, method, target string, status, round int, start time.Time, reqBody, respBody []byte, rectified bool, maxAgeDays int) error {
```

- [ ] **Step 2: 在 os.MkdirAll 之后、内容构建之前插入清理**

```go
func writeErrorDump(...) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("writeErrorDump: mkdir %s: %w", dir, err)
	}
	cleanErrorDumps(dir, maxAgeDays)

	var b strings.Builder
	// ... 其余不变
```

- [ ] **Step 3: 更新 error_dump_test.go 的调用**

```go
err := writeErrorDump(dir, ps, "key-a", "POST", "/v1/messages", 400, 2,
    time.Now(), []byte(`{"thinking":{"type":"adaptive"}}`), []byte(`{"error":"bad"}`), true, 7)
```

- [ ] **Step 4: 验证编译**

```bash
go build ./...
```

- [ ] **Step 5: 提交**

```bash
git add internal/server/error_dump.go internal/server/error_dump_test.go
git commit -m "feat: add cleanup trigger to writeErrorDump"
```

---

### Task 4: proxy_executor.go — 传递 ErrorDumpMaxAge

**Files:**
- Modify: `internal/server/proxy_executor.go` — `handleNonRetryable` 调用处传递 `maxAgeDays`

**Interfaces:**
- Consumes: `ps.ErrorDumpMaxAge() int`（Task 1）、`SetupErrorLogDir(maxAgeDays int)`（Task 2）、`writeErrorDump(..., maxAgeDays int)`（Task 3）
- Produces: 无

- [ ] **Step 1: 修改 handleNonRetryable 中的调用**

当前：

```go
if err := writeErrorDump(SetupErrorLogDir(), ps, keyName, method, target, resp.StatusCode, attempt, start, bodyBytes, body, rectified); err != nil {
```

改为：

```go
maxAge := ps.ErrorDumpMaxAge()
if err := writeErrorDump(SetupErrorLogDir(maxAge), ps, keyName, method, target, resp.StatusCode, attempt, start, bodyBytes, body, rectified, maxAge); err != nil {
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

- [ ] **Step 3: 提交**

```bash
git add internal/server/proxy_executor.go
git commit -m "feat: wire ErrorDumpMaxAge through proxy_executor"
```

---

### Task 5: 测试 — cleanErrorDumps 单元测试

**Files:**
- Modify: `internal/server/error_dump_test.go` — 新增 4 个测试

**Interfaces:**
- Consumes: `cleanErrorDumps(dir string, maxAgeDays int)`（Task 2）

- [ ] **Step 1: 新增 TestCleanErrorDumps_RemovesOldFiles**

```go
func TestCleanErrorDumps_RemovesOldFiles(t *testing.T) {
	dir := t.TempDir()

	// Create an old file (10 days ago)
	oldPath := filepath.Join(dir, "old.txt")
	os.WriteFile(oldPath, []byte("old"), 0644)
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(oldPath, oldTime, oldTime)

	// Create a recent file (now)
	newPath := filepath.Join(dir, "new.txt")
	os.WriteFile(newPath, []byte("new"), 0644)

	cleanErrorDumps(dir, 7)

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old file should be removed, stat err: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new file should remain, stat err: %v", err)
	}
}
```

- [ ] **Step 2: 新增 TestCleanErrorDumps_ZeroMaxAge**

```go
func TestCleanErrorDumps_ZeroMaxAge(t *testing.T) {
	dir := t.TempDir()

	// Create a file that is 3 days old (< 7 day default)
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("data"), 0644)
	oldTime := time.Now().AddDate(0, 0, -3)
	os.Chtimes(path, oldTime, oldTime)

	cleanErrorDumps(dir, 0)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("3-day-old file should not be removed with maxAge=0 (fallback to 7), stat err: %v", err)
	}
}
```

- [ ] **Step 3: 新增 TestCleanErrorDumps_NoDir**

```go
func TestCleanErrorDumps_NoDir(t *testing.T) {
	// Should not panic or error when dir doesn't exist
	cleanErrorDumps(filepath.Join(t.TempDir(), "nonexistent"), 7)
}
```

- [ ] **Step 4: 新增 TestCleanErrorDumps_SkipsSubdirs**

```go
func TestCleanErrorDumps_SkipsSubdirs(t *testing.T) {
	dir := t.TempDir()

	subDir := filepath.Join(dir, "sub")
	os.MkdirAll(subDir, 0755)

	// Old file in subdir (should be skipped)
	oldSub := filepath.Join(subDir, "old.txt")
	os.WriteFile(oldSub, []byte("old"), 0644)
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(oldSub, oldTime, oldTime)

	cleanErrorDumps(dir, 7)

	// Subdir itself should still exist
	if _, err := os.Stat(subDir); err != nil {
		t.Errorf("subdir should not be removed, stat err: %v", err)
	}
	// File inside subdir should still exist
	if _, err := os.Stat(oldSub); err != nil {
		t.Errorf("file inside subdir should not be removed, stat err: %v", err)
	}
}
```

- [ ] **Step 5: 运行测试验证**

```bash
go test -tags=unit -count=1 -short -run 'TestCleanErrorDumps' ./internal/server/
```

预期：全部 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/server/error_dump_test.go
git commit -m "test: add cleanErrorDumps tests"
```

---

### Task 6: 全量验证

- [ ] **Step 1: 运行全量检查**

```bash
make check && make test-unit
```

- [ ] **Step 2: 提交如果还有未提交的改动**

```bash
git add -A && git status
```

- [ ] **Step 3: 退出 worktree**

```bash
exit  # 或手动 ExitWorktree
```