# Error Dump 自动清理 — 设计文档

**日期：** 2026-08-15
**状态：** Draft
**背景：** PR #319（4xx error dump 功能）已合并到 main。该功能在 `handleNonRetryable` 中将每个非重试 4xx 的请求/响应体写入 `~/.akswitch/errors/`，文件名格式 `{status}-{timestamp}-{keyPrefix}.txt`。

## 问题

实测发现，sensenova provider 持续产生 400 错误（`thinking.budget_tokens` 超限），1 小时内积累 214 个文件、145 MB。当前没有任何清理机制，长期运行会耗尽磁盘空间。

`akswitch.log` 已通过 lumberjack 实现按大小/年龄轮转（`log_max_size` 默认 100 MB、`log_max_age` 默认 7 天）。Error dump 是每个 4xx 一个独立文件，不能用 lumberjack（它只管单个持续写入的日志流）。

## 目标

为 error dump 目录提供与 `log_max_age` 语义一致的按年龄清理机制：超过配置天数的 dump 文件自动删除。

## 非目标

- 不做按文件数或总大小裁剪。400 高频产生是根因问题（客户端代码错误），裁剪解决不了根本问题；修复根因后新文件停止产生，旧文件由年龄清理自然消失。
- 不引入定时器 goroutine。
- 不修改 dump 文件名格式或内容结构。

## 设计

### 配置项

在 `ProviderConfig`（`internal/config/config.go`）新增字段，紧邻 `LogMaxAge`：

```go
LogMaxAge       int `toml:"log_max_age,omitempty" default:"7"`
ErrorDumpMaxAge int `toml:"error_dump_max_age,omitempty" default:"7"`
```

- TOML 键：`error_dump_max_age`
- 默认值：7 天
- 0 或负值的处理：清理逻辑内 fallback 到默认 7 天（避免配置为 0 时误删全部文件）

遵循现有约定：`default` struct tag 由 `mergeDefaults()` 在零值时填充，无需手动处理默认值合并。

### 清理函数

在 `internal/server/crash.go`（与 `SetupErrorLogDir` 同处）新增：

```go
// cleanErrorDumps removes error dump files older than maxAgeDays from dir.
// It is best-effort: read or remove failures are logged at warn level, not fatal.
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

### 清理时机

两处触发，覆盖短运行和长运行场景：

1. **启动时** — 在 `SetupErrorLogDir()` 内，`os.MkdirAll` 成功后调用 `cleanErrorDumps(dir, maxAge)`。重启后旧文件立即被清理。

   `SetupErrorLogDir()` 签名需扩展以接收 `maxAge` 参数：
   ```go
   func SetupErrorLogDir(maxAgeDays int) string
   ```

2. **每次写 dump 前** — 在 `writeErrorDump()` 写入新文件前调用 `cleanErrorDumps(dir, maxAge)`。长运行不重启时保持干净。

   `writeErrorDump()` 内部在构造文件名之后、原子写入之前触发清理。

### 配置传递路径

`handleNonRetryable` 在 `proxy_executor.go` 调用 `SetupErrorLogDir()` 和 `writeErrorDump()`。需要把 `ErrorDumpMaxAge` 从 `ProviderState` 传下去：

- `ProviderState` 新增 getter `ErrorDumpMaxAge() int`（遵循现有封装模式）
- `proxy_executor.go` 调用处改为 `SetupErrorLogDir(ps.ErrorDumpMaxAge())`
- `writeErrorDump` 签名新增 `maxAgeDays int` 参数，在构造文件名之后、原子写入之前调用 `cleanErrorDumps(dir, maxAgeDays)`（清理与写入在同一次调用内完成，避免外部重复扫描）

### 不采用定时器

清理在启动 + 每次写前触发，已经足够。不引入 ticker goroutine，避免额外并发复杂度。400 产生频次高时清理及时；400 停止后无需清理。

## 测试

新增到 `internal/server/error_dump_test.go`（或 `crash_test.go`），table-driven：

- `TestCleanErrorDumps_RemovesOldFiles` — 用 `t.TempDir()` 创建两个文件，一个 `ModTime` 设为 10 天前、一个设为当前，验证旧的被删、新的保留
- `TestCleanErrorDumps_ZeroMaxAge` — `maxAgeDays=0` 时 fallback 到 7 天，不误删新文件
- `TestCleanErrorDumps_NoDir` — 目录不存在时静默返回，不报错
- `TestCleanErrorDumps_SkipsSubdirs` — 子目录不被删除

### 已有测试的调整

- `TestSetupErrorLogDir_CreatesDirectory` — `SetupErrorLogDir` 签名变更后，补一个 `maxAge` 参数（传 0 或 7）
- `TestWriteErrorDump_WritesFileWithBodies` — `writeErrorDump` 签名变更后同步更新

## 验证

```bash
make check                    # lint + vet + fmt
make test-unit                # 单元测试
go test -tags=unit -count=1 -short ./internal/server/  # 单包测试
```

## 影响面

- 配置兼容性：新增可选字段，未配置者走默认 7 天，向后兼容
- 运行时行为：启动多一次目录扫描；每次 400 多一次目录扫描（400 本就是异常路径，扫描开销可忽略）
- 不影响 `akswitch.log` 的 lumberjack 轮转
