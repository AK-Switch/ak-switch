# CLI 参考

CLI 命令文档已从代码自动生成，位于 [`docs/cli/`](cli/) 目录下。

每个命令有独立的 Markdown 文件：

| 命令 | 文档 |
|------|------|
| `akswitch` | [`akswitch.md`](cli/akswitch.md) |
| `akswitch start` | [`akswitch_start.md`](cli/akswitch_start.md) |
| `akswitch stop` | [`akswitch_stop.md`](cli/akswitch_stop.md) |
| `akswitch status` | [`akswitch_status.md`](cli/akswitch_status.md) |
| `akswitch version` | [`akswitch_version.md`](cli/akswitch_version.md) |
| `akswitch config` | [`akswitch_config.md`](cli/akswitch_config.md) |
| `akswitch config init` | [`akswitch_config_init.md`](cli/akswitch_config_init.md) |
| `akswitch config view` | [`akswitch_config_view.md`](cli/akswitch_config_view.md) |
| `akswitch provider` | [`akswitch_provider.md`](cli/akswitch_provider.md) |
| `akswitch provider add` | [`akswitch_provider_add.md`](cli/akswitch_provider_add.md) |
| `akswitch provider list` | [`akswitch_provider_list.md`](cli/akswitch_provider_list.md) |
| `akswitch provider remove` | [`akswitch_provider_remove.md`](cli/akswitch_provider_remove.md) |
| `akswitch provider default` | [`akswitch_provider_default.md`](cli/akswitch_provider_default.md) |
| `akswitch key` | [`akswitch_key.md`](cli/akswitch_key.md) |
| `akswitch key add` | [`akswitch_key_add.md`](cli/akswitch_key_add.md) |
| `akswitch key import` | [`akswitch_key_import.md`](cli/akswitch_key_import.md) |
| `akswitch key list` | [`akswitch_key_list.md`](cli/akswitch_key_list.md) |
| `akswitch key remove` | [`akswitch_key_remove.md`](cli/akswitch_key_remove.md) |
| `akswitch key disable` | [`akswitch_key_disable.md`](cli/akswitch_key_disable.md) |
| `akswitch key enable` | [`akswitch_key_enable.md`](cli/akswitch_key_enable.md) |
| `akswitch key update` | [`akswitch_key_update.md`](cli/akswitch_key_update.md) |
| `akswitch key rename` | [`akswitch_key_rename.md`](cli/akswitch_key_rename.md) |
| `akswitch logs` | [`akswitch_logs.md`](cli/akswitch_logs.md) |

## 维护

新增或修改 CLI 命令/标志后，运行以下命令更新文档：

```bash
make gen-docs
# 或
go run ./tools/gen-cli-docs/
```

CI 会自动检查 `docs/cli/` 是否与代码一致。

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