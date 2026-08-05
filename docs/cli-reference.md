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

### config 命令

`akswitch config` 管理运行时配置和 `config.toml` 文件。

**初始化：**

```bash
akswitch config init                # 在 XDG 目录生成示例 config.toml
akswitch config init --path ./test.toml  # 指定输出路径
```

**查看：**

```bash
akswitch config view                # 打印当前 config.toml 内容
akswitch config list                # 列出第一个 provider 的运行时参数
akswitch config list sensenova      # 列出指定 provider 的运行时参数
akswitch config list --all          # 列出所有 provider 的运行时参数
akswitch config get http_timeout_sec    # 取第一个 provider 的参数值
akswitch config get log_level sensenova  # 取指定 provider 的参数值
akswitch config get cooldown_sec --all   # 取所有 provider 的参数值
```

**修改（即时生效，不重启）：**

```bash
akswitch config set log_level debug            # 修改第一个 provider 的 log_level
akswitch config set max_retries 5 --persist     # 修改并写入 config.toml
akswitch config set log_level info sensenova    # 修改指定 provider
akswitch config set cooldown_sec 30 --all       # 批量修改所有 provider
akswitch config set max_retries 3 --all --persist  # 批量修改并持久化到 [provider.default]
```

`--all` 批量写入时使用 `--persist` 会写入 `[provider.default]` 段，新添加的 provider 会自动继承这些值。非批量修改的 `--persist` 写入对应 provider 的独立段。

可调参数：`http_timeout_sec`、`max_retries`、`cooldown_sec`、`backoff_cap_sec`、`backoff_multiplier`、`cb_reset_sec`、`upstream_cb_threshold`、`log_level`。

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

### provider usage 输出示例

```
akswitch usage --key sk-xxxxxxxxxxxxxxxx --provider sensenova
Found account: d1-1 (tenant: t-abc123)

Remaining quota:
  deepseek-v4-flash               27.8% [||||||    ]
  sensenova-6.7-flash-lite       100.0% [||||||||||]
  sensenova-u1-fast               100.0% [||||||||||]
```

通过 `--credentials-dir` 指定凭证目录（默认从 `config.toml` 读取 `credentials_dir` 字段）。

### Key 存储

- Key 存储在系统 keyring 中，回退到 JSON 文件
- 可用 `--insecure-storage` 标志以明文存储（CI/一次性环境）

### status 输出示例

`akswitch status` 返回所有 provider 的健康状态和统计。

```
Server: http://127.0.0.1:4000
Status: ok
Providers: 2
PROVIDER       KEYS  CB_STATE
nvidia         6     closed
sensenova      6     closed
Requests: 2588 (success: 2577, failed: 11)
Active keys: 12, Cooling: 0, Disabled: 0
Uptime: 32559s
```

传入可选的 provider 名称可过滤输出：

```
akswitch status sensenova
```

```
Server: http://127.0.0.1:4000
Status: ok
Providers: 1
PROVIDER       KEYS  CB_STATE
sensenova      6     closed
Requests: 1294 (success: 1288, failed: 6)
Active keys: 6, Cooling: 0, Disabled: 0
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