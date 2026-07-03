# AK Switch 关键路径覆盖清单

> 所有用户可见行为及其测试覆盖状态。目标：**每个 CLI 行为都有对应的 CLI 入口测试。**

---

## 图例

| 标记 | 含义 |
|------|------|
| ✅ | 有 CLI 入口测试覆盖 |
| ⚠️ | 有测试但未走 CLI 入口（组件级测试） |
| ❌ | 无测试覆盖 |

---

## 路径 1：akswitch start — TOML 多 provider 模式

| # | 步骤 | 覆盖 | 测试文件 |
|---|------|------|---------|
| 1.1 | DetectConfigSource 返回 config.toml 路径 | ⚠️ | `config_test.go` — 仅测了 TOML 解析，没测完整的 DetectConfigSource → LoadAllTomlProviders 链路 |
| 1.2 | LoadAllTomlProviders 读取所有 provider | ⚠️ | `config_test.go` — 只测了解析，没测后续的 key 加载 + validate |
| 1.3 | **loadKeysForProvider 从加密存储找 Key** | ❌ | **不存在** ← 今天的 bug |
| 1.4 | Validate 检查 Key 非空 | ❌ | **不存在** ← 今天的 bug |
| 1.5 | InstanceManager.StartAll 绑定端口 + Serve | ⚠️ | `manager_test.go` — 直接创建 Config+Pool，绕过了所有前面的步骤 |
| 1.6 | 优雅关闭（信号 → Shutdown → Stop） | ⚠️ | `manager_test.go` / `graceful_shutdown_test.go` — 绕过启动流程 |
| 1.7 | 后台任务启动（env watcher、metrics、health check） | ❌ | 不存在 |

## 路径 2：akswitch start — .env 单 provider 模式

| # | 步骤 | 覆盖 | 测试文件 |
|---|------|------|---------|
| 2.1 | server.LoadConfig 读取 .env + Validate | ⚠️ | `integration_test.go` — 子进程模式，但用的是 `server.LoadConfig()` 不是 `cmd.Execute` |
| 2.2 | 单实例 InstanceManager 启动 | ⚠️ | `manager_test.go` — 同上，绕过 |

## 路径 3：akswitch config

| # | 步骤 | 覆盖 | 测试文件 |
|---|------|------|---------|
| 3.1 | `akswitch config init` 生成 config.toml | ✅ | `config_cmd_test.go` — `cmd.Execute` |
| 3.2 | `akswitch config view` 打印配置 | ✅ | `config_cmd_test.go` — `cmd.Execute` |

## 路径 4：akswitch provider

| # | 步骤 | 覆盖 | 测试文件 |
|---|------|------|---------|
| 4.1 | `akswitch provider add` 写入 config.toml | ✅ | `provider_cmd_test.go` |
| 4.2 | `akswitch provider list` 打印 | ✅ | `provider_cmd_test.go` |
| 4.3 | `akswitch provider remove` 删除 | ✅ | `provider_cmd_test.go` |

## 路径 5：akswitch key

| # | 步骤 | 覆盖 | 测试文件 |
|---|------|------|---------|
| 5.1 | `akswitch key add` 写入加密存储 | ✅ | `key_cmd_test.go` |
| 5.2 | `akswitch key list` 打印脱敏 Key | ✅ | `key_cmd_test.go` |
| 5.3 | `akswitch key remove` 删除 Key | ✅ | `key_cmd_test.go` |
| 5.4 | `akswitch key disable` 禁用 Key | ✅ | `key_cmd_test.go` |

## 路径 6：akswitch status / logs / stop

| # | 步骤 | 覆盖 | 测试文件 |
|---|------|------|---------|
| 6.1 | `akswitch status` 查询运行实例 | ❌ | 不存在 |
| 6.2 | `akswitch logs` 查询请求日志 | ❌ | 不存在 |
| 6.3 | `akswitch stop` PID → 信号 → 关闭 | ❌ | 不存在 |

## 路径 7：Proxy 请求转发

| # | 步骤 | 覆盖 | 测试文件 |
|---|------|------|---------|
| 7.1 | HTTP 请求 → 选择 Key → 转发上游 | ✅ | `proxy_test.go` — mock upstream，完整 proxy |
| 7.2 | Key 冷却 → 自动切换下一个 | ✅ | `proxy_test.go` |
| 7.3 | KeyCircuitBreaker 指数退避 | ✅ | `keypool_test.go` / `key_test.go` |
| 7.4 | UpstreamCircuitBreaker 熔断 | ✅ | `upstream_test.go` |

## 路径 8：管理 API

| # | 步骤 | 覆盖 | 测试文件 |
|---|------|------|---------|
| 8.1 | `/api/keys` GET/POST/DELETE | ⚠️ | `handlers_test.go` — 直接创建 ServerState |
| 8.2 | `/api/config` GET/POST | ⚠️ | `handlers_test.go` — 同上 |
| 8.3 | `/api/reload` POST | ⚠️ | `handlers_test.go` — 同上 |
| 8.4 | `/api/stats` GET | ⚠️ | `handlers_test.go` — 同上 |

---

## 当前缺口汇总

| 优先级 | 缺失路径 | 后果 |
|--------|---------|------|
| **P0** | `akswitch start`（TOML 模式）全链路 | 今天的两组 bug 活到了生产环境 |
| P1 | `akswitch status` / `logs` / `stop` | 运行时命令无回归保证 |
| P2 | 管理 API 从 CLI 入口测试 | 当前通过 `NewServerState` 绕过 |

## 纪律

详见 CLAUDE.md「关键路径覆盖纪律」：
- 所有 CLI 命令必须有对应的 CLI 入口测试
- `akswitch start` 通过子进程模式测试
- 其他 CLI 命令通过 `runCommand` 测试
- 纯内部逻辑（算法、数据结构、状态机）是唯一允许没有 CLI 入口测试的例外