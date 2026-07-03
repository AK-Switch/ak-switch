# AK Switch 变更日志

> 已完成的里程碑记录。当前功能完整，项目已进入维护模式。

---

## 日志条目增强（PR #36）

- LogEntry 新增 DurationMs/Attempt/Provider 字段
- 重试耗尽路径新增日志记录
- CLI `akswitch logs` 命令新增 provider/attempt/duration/key_name 展示
- 集成测试验证新字段存在 + 重试耗尽日志记录

## 单端口 + 路径路由重构（PR #32）

- 从"一个 provider 一个端口"改为"单端口 + `/{provider}/...` 路径路由"
- 移除 `.env` 配置加载（纯 TOML 模式）
- 移除 `--local` / `--network-only` 参数
- ProviderRouter 替代 InstanceManager，所有 provider 共享一个 HTTP 端口
- 管理 API（`/api/*`、`/health`、`/dashboard` 等）不受路径路由影响
- 代理请求格式：`POST /{provider}/v1/chat/completions`

## CLI 迁移（Spec A + B + C）

- Cobra CLI 框架，单一 `akswitch` 二进制管理所有操作
- TOML 配置格式（`config.toml`），XDG 标准路径
- `akswitch start` 单端口多 provider（ProviderRouter）
- `akswitch provider add | list | remove` — provider 配置管理
- `akswitch key add | list | remove | disable` — Key 加密存储管理
- `akswitch config init | view` — 配置初始化和查看
- `akswitch status | logs | stop` — 运行时状态查询和管理
- `akswitch start --provider <name>` — 单 provider 启动过滤
- `manage.go` 已删除，`.env` 支持已移除

## 代码健康 Sprint（PR #25）

- `reloadHandler` 失败时返回 HTTP 500（原为 200）
- 统一 `maskKey`（CLI 与 API 一致）
- `resetAllEnv` 补齐所有遗漏环境变量
- 删除未使用的 `im.stop` channel
- 清理未使用的 viper 依赖
- `TomlProviderConfig` 补齐 15 个字段

## 关键路径测试覆盖（PR #29 + PR #30）

- `start_cmd_test.go` — 子进程模式测试 `akswitch start` TOML 启动全链路
- `e2e_test.go` — 真实二进制全流程模拟（provider add → proxy → shutdown）
- `docs/internal/critical-paths.md` — 所有 CLI 行为测试覆盖状态
- CLAUDE.md 新增"关键路径覆盖纪律"

## README 重写 + 文档拆分（PR #26）

- README 压回导航页（~50 行），详细文档拆分到 `docs/`
- `docs/cli-reference.md` — CLI 命令参考
- `docs/configuration.md` — TOML 配置说明
- `docs/api.md` — API 端点文档
- `docs/architecture.md` — 熔断器架构
- `docs/deployment.md` — Docker 部署与监控栈
- 研究/分析文档移入 `docs/internal/`