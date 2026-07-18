# AK Switch 变更日志

> 已完成的里程碑记录。当前功能完整，项目已进入维护模式。

---

## v0.1.2（2026-07-19）

### Windows 图标修复
- 修复 icon.ico 尺寸：从 1254×1254 单图缩放为 256/48/32/16 标准多尺寸 ICO
- 修复 CI 中 rsrc 图标嵌入：临时清除 GOOS/GOARCH 后再安装运行 rsrc

### Release Notes 增量
- 替换 `generate_release_notes: true` 为显式 `gh api generate-notes` 调用，支持 `previous_tag` 参数，不再包含全部历史 PR

### Token 计量加固
- 修复流式响应中 `output_tokens` 始终为 0 的问题
- 支持多种 SSE 流式格式（`message_delta`、`content_block_start`、OpenAI 格式）
- 修复 `estimateInputTokens` 对 Anthropic 格式（content 为数组）的兼容性
- Token 计量全面加固（边界情况容错）

### 配置增强
- 移除 `KEYS_ENCRYPTION_KEY` 环境变量，简化密钥管理
- keyring 不可用时自动回退到加密文件存储，新增 `--insecure-storage` 明文逃生口
- `Host` 改为可配置项，支持绑定 Tailscale IP 等非默认地址

### 项目技能
- 添加 `remote-monitoring` 项目级 skill，管理远程监控栈（Prometheus/Grafana/nssm）

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

## 代码仓库规范化（PR #51）

- `.gitattributes` 声明 `* text=auto` + `CLAUDE.md eol=lf`，统一跨平台行尾处理
- CLAUDE.md 添加 `go install` 构建指令，修正行尾
- README.md 测试徽章 URL 修复（OmitNomis → bigmanBass666）
- design-decisions.md 归档 4 项近期完成决策记录
- ColorHandler 日志 msg 与 attrs 粘合修复

## 多项体验改进（PR #52）

- 全 Key 熔断错误提示增加 provider 名（中文消息）
- 日志字段 attempt → retry 重命名，retry=0 时隐藏
- 配置路径从 XDG 改为 `~/.akswitch/`，支持 `AKSWITCH_CONFIG_DIR` 环境变量覆盖
- Key 热加载 — `/api/reload` 重加载 key 文件 + key 命令自动触发

## 日志格式修复 + 紧凑响应时间线（PR #53）

- 修复 ColorHandler 日志 msg 与 attrs 之间缺少空格的问题
- compact 模式增加 TTFB 和总用时，去掉独立请求行
- LogEntry 新增 `TtfbMs` 字段持久化首字用时
- `akswitch logs --compact` 紧凑格式输出

## 校准集成（Calibrator）

- Calibrator 滑动窗口 + 中位数比例校准，覆盖 17 个测试用例
- 挂载到 ProviderRouter，非流式记录样本，流式应用校准比例
- 集成 tiktoken-go 依赖，提取 `estimateInputTokens` / `estimateOutputTokens` 独立函数

## 默认 Provider 改进（PR #56 + #57 + #58 + #59）

- `default_provider` 未设置时默认启动第一个 provider（#56）
- `--provider` 和 `--all` 从 PersistentFlags 改为 LocalFlags，防止跨命令干扰（#57）
- `provider add --default` 和 `provider default <name>` 命令行配置默认 provider（#58）
- 修复 `provider add --default` flag 测试间泄漏，新增测试隔离（#59）