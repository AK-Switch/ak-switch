# CLI 参考

CLI 的完整用法和所有子命令通过 `akswitch --help` 和 `akswitch <command> --help` 查看。
`--help` 是 CLI 文档的第一来源，始终与代码一致。

## 架构概览

单一 `akswitch` 二进制管理所有操作（类 `git` 设计）。无全局标志，每个子命令有各自的标志。

### 启动顺序

1. 读取 `config.toml`
2. 逐个加载 provider 配置和 Key
3. 绑定端口启动 HTTP 服务
4. 启动后台 goroutine（热重载、指标刷新、主动健康检查）
5. 等待中断信号 → 优雅关闭所有实例

### 停止流程

1. 读取 `akswitch.pid` 文件获取 PID
2. Windows：`taskkill` 发送关闭信号；Unix：发送 `os.Interrupt`
3. 轮询等待进程退出（最长 10 秒，每 500ms 检查一次）
4. 进程退出后删除 PID 文件

### provider list 输出示例

```
Providers (from /home/user/.config/akswitch/config.toml):
  NAME        TARGET                                            PORT
  nvidia      https://integrate.api.nvidia.com/v1               3001  (default)
  sensenova   https://api.sensenova.com/v1                      3001
```

### provider info 输出示例

```
Provider: agnes
  Status:  running  →  http://100.65.183.101:4000
  CB:      closed
  Requests: 730 (success: 420, failed: 310)

  Config:
    Target:  https://apihub.agnes-ai.com/v1
    Port:    4000

  Tuning:
    Max retries:        2
    Cooldown:           15s

  Health check:
    Interval:  30s
    Path:      /health
    Timeout:   5s

  Keys:
    Total: 31  Active: 31  Disabled: 0
    [0] sk-s...0l8U  (active)  name: Google
```

### Key 存储

- Key 存储在系统 keyring 中，回退到 JSON 文件
- 可用 `--insecure-storage` 标志以明文存储（CI/一次性环境）

### status 输出示例

```
Server: http://127.0.0.1:4000
Status: ok
PROVIDER       KEYS  CB_STATE
nvidia         6     closed
sensenova      6     closed
Requests: 2588 (success: 2577, failed: 11)
Active keys: 12, Cooling: 0, Disabled: 0
Uptime: 32559s
```

实例无响应时显示错误信息，不崩溃。

### logs 输出示例

默认格式（`--verbose` 带完整 method/URL）：

```
=== Provider "nvidia" (port 3001) ===
  [12:00:00.000] 200 (nvidia, key: nvap...xxxx, 342ms)
  [12:00:01.000] 429 (nvidia, key: nvap...yyyy, 12ms)
```