# Troubleshooting Guide

## 运行时日志文件

### 找不到日志文件

运行时日志文件路径由 `config.toml` 的 `log_file` 字段指定。如果未设置，日志仅输出到 stdout（stderr），不写入文件。

**排查步骤：**

1. 检查 `config.toml` 是否有 `log_file` 字段：
   ```bash
   cat ~/.akswitch/config.toml | grep log_file
   ```
2. 查看启动日志中是否输出文件路径：
   ```bash
   grep "file logging initialized" ~/.akswitch/akswitch.log
   ```
3. 如果未设置，日志仅输出到 stderr，可通过 `akswitch start --dev` 查看标准输出。

### `akswitch logs` 看不到日志

`akswitch logs` 从 HTTP API `/api/logs` 读取内存环形缓冲区，**不是**运行时日志文件。

- 它展示的是**请求记录**（每个 HTTP 请求一条），不是运行时日志（slog）
- 运行时日志（slog）输出到文件或 stderr，通过 `akswitch log-level` 控制级别
- 请求日志（LogEntry）始终记录，不受日志级别影响

## 请求日志

### Key 被 401 永久禁用后又成功请求

**现象：** 日志中 key_index=11 先报 401 被 permanently disabled，但紧接着又用同一个 key 请求成功。

**原因：** 竞态条件。

`proxy_executor.go` 中，`pool.Release(idx)` 在 `handleAuthRejected()` 之前执行（第 141 行）。这意味着：

1. 请求 A 使用 key 11 → 上游返回 401
2. `pool.Release(11)` 调用，key 11 被标记为可用
3. 请求 B 通过 `pool.Next()` 选中 key 11（此时它还是可用的）
4. 请求 A 的 `handleAuthRejected()` 调用 `pool.Disable(11)`，key 11 被标记为永久禁用
5. 请求 B 已经选中了 key 11，仍然正常发出并成功

**影响：** 仅影响这一个请求。后续请求 `pool.Next()` 不会再选到 key 11（因为 `Disable()` 已生效）。

**验证方法：** 看日志中请求 B 的 `retry` 字段。如果 `retry=0`，说明是重试循环之外的新请求，确认是竞态条件。

### 冷却时长为 0s

**现象：** 日志中 `duration=0s` 的冷却记录。

**根因：** 断路器创建时参数为 0（`NewKeyCircuitBreaker(0, 0, 0)`），`RecordFailure()` 返回 0 时长。

**可能的原因：**

1. **`ConfigureCBs` 未被调用** — `reloadHandler` 中重建 `KeyPool` 后没有调用 `ConfigureCBs` 给断路器设置正确的冷却参数。见 #175。
2. **启动时序问题** — `NewKeyPool` 创建断路器时未传入配置参数，延迟到 `ConfigureCBs` 才设置。如果 `RecordFailure` 在 `ConfigureCBs` 之前发生，就会返回 0。

**修复状态：** 问题 1 已在 PR #175 修复。问题 2 是启动窗口期的问题，影响极小。

### 大量 429 限流

**现象：** 多个 key 同时出现 `too_many_requests` 429 响应。

**根因：** 上游（如 sensenova）对 API key 的并发限制或速率限制。

**排查步骤：**

1. 检查 `max_retries` 配置（默认 2）：
   ```bash
   akswitch config get max_retries
   ```
2. 检查 `cooldown_sec` 配置（冷却时长，默认 15s）：
   ```bash
   akswitch config get cooldown_sec
   ```
3. 检查 `http_timeout_sec` 配置（默认 30s）：
   ```bash
   akswitch config get http_timeout_sec
   ```
4. 调整 `cooldown_sec` 和 `backoff_multiplier` 来缓解：
   ```bash
   akswitch config set cooldown_sec 60
   akswitch config set backoff_multiplier 2.0
   ```

### 上游 502/503 导致上游断路器打开

**现象：** 所有请求返回 503，日志显示 `upstream circuit breaker open`。

**原因：** 上游连续返回 502/503 超过 `upstream_cb_threshold` 次（默认 5）。

**排查步骤：**

1. 检查上游断路器状态：
   ```bash
   curl http://localhost:4000/api/health
   ```
2. 检查 `upstream_cb_threshold` 和 `cb_reset_sec`：
   ```bash
   akswitch config get upstream_cb_threshold
   akswitch config get cb_reset_sec
   ```
3. 如果上游已恢复，可以手动重置：
   ```bash
   # 调整阈值触发重置，或等待 cb_reset_sec 秒后自动半开
   ```

## 运行时配置

### 运行时参数修改后不生效

**原因：** 运行时配置修改（`akswitch config set`）默认不持久化。

**解决方法：** 加 `--persist` 参数：
```bash
akswitch config set cooldown_sec 60 --persist
```

### 配置热重载后部分状态丢失

**现象：** `reloadHandler` 执行后，key 的冷却状态、禁用状态异常。

**原因：** `reloadHandler` 重建 `KeyPool` 时，只恢复了禁用状态（`disabledNames`），但冷却状态、重试次数、请求计数等运行时状态都丢失了。此外，`ConfigureCBs` 未被调用，断路器使用默认参数（0, 0, 0）。

**修复状态：** `ConfigureCBs` 问题已在 PR #175 修复。

## 系统诊断

### 查看当前所有 key 的状态

```bash
akswitch status
```

输出每个 key 的 index、状态（ready/disabled/cooling）、最近 1 分钟请求数。

### 查看断路器参数

```bash
akswitch config list
```

关键参数：
- `cooldown_sec` — 冷却时长
- `backoff_cap_sec` — 冷却时长上限
- `backoff_multiplier` — 冷却时长增长倍数
- `cb_reset_sec` — 上游断路器重置时间
- `upstream_cb_threshold` — 上游断路器触发阈值

### 查看完整配置

```bash
akswitch config view
```

输出所有 provider 的完整配置（key 已脱敏）。
