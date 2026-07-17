# CLI 参考

单一 `akswitch` 二进制管理所有操作（类 `git` 设计）。

## 全局标志

无全局标志。每个子命令有各自的标志，见各命令说明。

## `akswitch start`

读取 `config.toml`，初始化 Key 池，启动 HTTP 代理服务。

```bash
akswitch start                           # 启动第一个 provider（按名称字母序）
akswitch start --all                     # 启动所有 provider
akswitch start --provider <name>         # 只启动指定 provider
akswitch start --log-format=compact      # 精简日志模式（默认）
akswitch start --log-format=default      # 标准日志模式
```

- 读取 `config.toml` 中所有 `[provider.*]` 段
- 默认启动按名称字母序的第一个 provider（可设 `default_provider` 指定、或 `--all` 启动全部）
- `--all` 强制启动所有 provider（忽略 `default_provider` 设置）
- `--provider <name>` 只启动指定 provider（优先级最高）
- `--log-format` 指定日志格式：`compact`（精简）或 `default`（标准），默认为 `compact`
- 自动写入 `akswitch.pid` 文件，`akswitch stop` 通过此文件发送中断信号

### 启动顺序

1. 读取 `config.toml`
2. 逐个加载 provider 配置和 Key
3. 绑定端口启动 HTTP 服务
4. 启动后台 goroutine（热重载、指标刷新、主动健康检查）
5. 等待中断信号 → 优雅关闭所有实例

## `akswitch config`

```bash
akswitch config init [-p <path>]   # 生成默认 config.toml
akswitch config view                # 打印当前配置
```

`config init` 在 XDG 配置目录生成含两个占位 provider 的示例文件：

| 系统 | 路径 |
|------|------|
| Windows | `%APPDATA%\akswitch\config.toml` |
| Linux | `~/.config/akswitch/config.toml` |
| macOS | `~/Library/Application Support/akswitch/config.toml` |

## `akswitch provider`

```bash
akswitch provider add <name> -t <url> -p <port> [flags]  # 新增 provider
akswitch provider list                                     # 列出所有 provider
akswitch provider remove <name>                            # 删除 provider
akswitch provider default <name>                           # 设置默认 provider
```

### `provider add` 标志

| 标志 | 缩写 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `--target` | `-t` | 是 | — | 上游 API 基础 URL |
| `--port` | `-p` | 首个 provider 必填 | — | HTTP 监听端口（后续 provider 复用） |
| `--genai` | `-g` | 否 | — | GenAI 基础 URL（`/genai/` 路径路由） |
| `--cooldown-sec` | `-c` | 否 | `60` | 429 后 Key 冷却时长（秒） |
| `--max-retries` | `-r` | 否 | `3` | 每次请求的最大重试次数 |
| `--default` | | 否 | `false` | 添加后立即设为默认 provider |

示例：

```bash
# 最小配置
akswitch provider add nvidia --target https://integrate.api.nvidia.com/v1 --port 3001

# 完整配置（port 复用第一个 provider 的）
akswitch provider add sensenova \
  --target https://api.sensenova.com/v1 \
  --cooldown-sec 30 --max-retries 5

# 添加并设为默认
akswitch provider add openai --target https://api.openai.com/v1 --port 3001 --default
```

### `provider list` 输出示例

```
Providers (from /home/user/.config/akswitch/config.toml):
  NAME        TARGET                                            PORT
  nvidia      https://integrate.api.nvidia.com/v1               3001  (default)
  sensenova   https://api.sensenova.com/v1                      3001
```

## `akswitch key`

```bash
akswitch key add <provider> <key> [--name <name>]    # 添加 Key
akswitch key import <provider> [keys...]               # 批量导入 Key（文件/参数/标准输入）
akswitch key list <provider>                           # 列出 Key（脱敏显示）
akswitch key remove <provider> <index>                 # 删除 Key
akswitch key disable <provider> <index>                # 禁用 Key
akswitch key enable <provider> <index>                 # 启用 Key
```

- Key 存储在系统 keyring 中，回退到 JSON 文件
- 可用 `--insecure-storage` 标志以明文存储（CI/一次性环境）

示例：

```bash
# 添加 Key
akswitch key add nvidia nvapi-xxxxxxxxxxxx

# 添加带名称的 Key
akswitch key add nvidia nvapi-yyyyyyyyyyyy --name prod-key

# 列出 Key（脱敏）
akswitch key list nvidia
# Keys for provider "nvidia" (from .../keys/nvidia.enc):
#   [0] nvap...xxxx  (active)
#   [1] nvap...yyyy  (active)  name: prod-key

# 删除 Key[0]
akswitch key remove nvidia 0

# 禁用 Key[1]
akswitch key disable nvidia 1

# 重新启用 Key[1]
akswitch key enable nvidia 1
```

## `akswitch status`

查询所有运行实例的健康状态和统计信息。

```bash
akswitch status
```

输出示例：

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

## `akswitch logs`

读取运行实例的请求日志。

```bash
akswitch logs                         # 显示所有日志
akswitch logs --last=20               # 只显示最近 20 条
akswitch logs --since=2026-07-14T00:00:00Z   # 只显示此时间后的条目
akswitch logs --verbose               # 显示完整 method/URL
akswitch logs --compact               # 精简格式（TTFB、总耗时、body 大小）
```

默认输出格式（`--verbose` 带完整 method/URL）：

```
=== Provider "nvidia" (port 3001) ===
  [12:00:00.000] 200 (nvidia, key: nvap...xxxx, 342ms)
  [12:00:01.000] 429 (nvidia, key: nvap...yyyy, 12ms)
```

## `akswitch stop`

读取 `akswitch.pid` 文件，向运行中的进程发送中断信号，轮询等待进程退出。

```bash
akswitch stop
```

执行流程：

1. 读取 `akswitch.pid` 文件获取 PID
2. Windows：`taskkill` 发送关闭信号；Unix：发送 `os.Interrupt`
3. 轮询等待进程退出（最长 10 秒，每 500ms 检查一次）
4. 进程退出后删除 PID 文件

PID 文件不可用时，打印手动终止命令并报错。

## `akswitch version`

```bash
akswitch version
```