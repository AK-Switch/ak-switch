# 4xx 错误请求/响应落盘设计

日期: 2026-08-14
状态: 已批准

## 背景

用户发现 AK-Switch 间或出现 `✗ 400` 错误，但无法溯源：

- `handleNonRetryable`（`internal/server/proxy_executor.go`）是唯一不读取上游响应体的错误处理 handler——其他 handler（429/401/403/5xx）都读取并记录 `body_preview`，唯独 4xx 直接 `io.Copy` 转发客户端，上游错误详情完全丢失
- 请求体（rectified 后）在内存中但从未落盘
- 用户正在验证 thinking rectifier（PR 将 `thinking.type: "adaptive"` 映射为上游接受的取值），400 错误可能来自 rectifier 未覆盖的 shape、非 thinking 字段，或完全无关的原因——没有请求/响应可见性就无法判断

此设计在 GitHub Discussion #312 的基础上扩展：Discussion 提议仅在日志中增加 `body_preview`（1KB、masked），本设计更进一步——将完整请求体 + 响应体 + 上下文元数据持久化到文件，供用户逐条溯源。

## 设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 触发条件 | 全部 non-retryable 4xx（400/422/413/414/415/405/406/501） | 同一个 handler 路径，范围一致性；避免只对 400 特判 |
| 存储格式 | 每错误一个独立文本文件 | 用户工作流是"看到 `✗ 400` → 打开对应文件"；JSONL 需要 grep/tail 处理，独立文件打开即看 |
| 存储位置 | `~/.akswitch/errors/` | 复用 crash.go 已有的 `.akswitch` 目录约定 |
| 清理策略 | 不自动清理 | 400 为偶发事件，清理逻辑属于过度设计 |
| 请求体大小 | 完整写入，不截断 | 400 偶发，写一次几百 KB 成本可忽略；rectifier 调试需要完整请求体，截断可能恰好切掉问题部分 |
| 敏感信息 | 存原始内容 | 本地文件，用于诊断；API key 在 Authorization header 中，不在请求体中 |

## 改动

### `internal/server/crash.go`

新增常量与辅助函数：

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

### `internal/server/proxy_executor.go` — `handleNonRetryable`

重写为：读 body → 写文件 → 转发客户端 → 打日志：

```go
func (px *ProxyExecutor) handleNonRetryable(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, method, target string, bodyBytes []byte, attempt int) {
    defer func() { _ = resp.Body.Close() }()
    keyName, _ := ps.PoolName(idx)

    // Read response body for persistence and logging (client still gets full body)
    body, _ := io.ReadAll(resp.Body)

    copyHeaders(w.Header(), resp.Header)
    w.WriteHeader(resp.StatusCode)
    _, _ = w.Write(body)

    if err := writeErrorDump(ps, keyName, method, target, resp.StatusCode, attempt, start, bodyBytes, body); err != nil {
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

同时新增 `writeErrorDump`：构造文件名 + 人类可读文本内容，原子写入（临时文件 + rename，避免写入一半被读到）。

### 文件名格式

```
{status}-{YYYYMMDD}-{HHMMSS}-{keyPrefix}.txt
```

示例: `400-20260814-202148123456789-735s4pv.txt`

- 纳秒时间戳避免同毫秒并发冲突
- key 前缀 = keyName 清洗非法文件名字符后取前 8 字符

### 文件内容格式

```
Time:      2026-08-14 20:21:48
Provider:  sensenova
Key:       735s4pvyd6us
URL:       /v1/messages?beta=true
Status:    400
Round:     0
Duration:  4.2s
Rectifier: enabled (mapTo: auto)
Rectified: true

--- Request Body ---
{...完整请求体...}

--- Response Body ---
{...完整响应体...}
```

Rectifier 信息从 `ps.ThinkingMode()` / `ps.RectifyThinkingMapTo()` 读取；`rectified` 标志从 `Execute` 传入（需在 `Execute` 中把 `rectified` 传给 `handleNonRetryable`——当前签名没有该参数）。

### 写入失败行为

写文件失败时静默 fallback：slog.Warn 记录错误，不影响请求/响应转发。

## 测试

- `internal/server/proxy_executor_test.go`：
  - `TestExecute_NonRetryable_WritesErrorDump` — 400 时验证文件被创建、内容含请求体与响应体
  - `TestExecute_NonRetryable_ClientStillGetsBody` — 客户端仍收到完整响应体
  - `TestWriteErrorDump_FilenameSanitization` — key 名含非法字符时文件名被清洗
  - 测试注入临时目录（`t.TempDir()`），不触碰真实 `~/.akswitch/`
- `internal/server/crash_test.go`：`SetupErrorLogDir` 返回目录且创建成功

## 未做

- 不自动清理旧文件（400 偶发）
- 不改压缩日志模式渲染（`✗ 400 (key)` 保持不变——文件路径即溯源入口）
- 不提供配置开关（默认开启，行为可预期）