# AK Switch 变更日志

> 已完成的里程碑记录。当前功能完整，项目已进入维护模式。

---

## v0.2.0（2026-07-27）

### 架构重构（PR #131）
- 重构 ProxyHandler：提取 `ProxyExecutor` 独立处理代理请求全流程（构建、发送、重试、响应）
- 提取 `TokenEstimator` 为独立子包，支持独立的 Token 估算和校准
- 提取 `LogManager` 封装日志环形缓冲区管理，简化 Server 状态管理
- 移除 `manager.go` 旧多端口模式代码，`ProviderRouter` 全面接管路由
- 合并 `errorclassifier.go` 等零散文件，减少 internal/server 文件数量
- 新增 `helpers.go` 统一辅助函数

### 项目结构规范化（PR #129）
- 重命名 `internal/cmd/` → `internal/cli/`，避免与根目录 `cmd/` 混淆
- 重命名 `alvus-dashboard/` → `web/`，符合 Go 标准布局
- 合并 `grafana/` + `prometheus/` → `deployments/`，统一部署配置
- 审查报告移入 `docs/internal/` 归档
- 修复 `release.yml` 中 ldflags 路径引用

### CLI 文档自动生成（PR #130）
- 新增 `tools/gen-cli-docs/` 工具，从 Cobra 命令树自动生成 Markdown 文档
- `docs/cli/` 下每个命令独立文档，CI 中 `docs-check` 作业验证文档最新
- 删除手写维护的 `docs/cli-reference.md`，从源头消除文档过期

### 开发者模式
- `akswitch start --dev`：端口自动递增 + 独立 PID 文件（`akswitch-dev.pid`）
- 启动时打印 `🚧 Dev mode` 标识

### Key 管理增强
- `key rename` 命令：支持重命名 provider 内的 key
- `key update` 命令：支持原位替换 API key（`--by-name` 标志）
- `key remove/disable/enable` 支持 `--by-name` 标志，统一命令工厂模式
- `key import` 增加 JSONL 支持、去重功能、自动编号和统计输出
- 提取 `KeyPool.validateIndex()` 共享方法，统一 `Name()` 越界行为

### 代码质量
- 修复端口扫描竞态条件：`StartWithListener` 预持有 listener 避免 `Listen→Close→再 Listen`
- 修复审查遗留的 6 个代码质量问题（nil deref、硬编码 host、过时注释等）
- `Config.mergeConfig()` 用反射 + default tag 自动化，新增字段不再遗漏
- 参数化测试替代逐一手写的 flag 注册测试（4 文件 +105/-136 行）
- 移除 `SilenceUsage`，CLI 参数错误时显示用法提示
- 修复 `TestKeyImport_FromFileWithObjects` 顺序依赖
- 修复 `default_provider` 删除时不清除配置的遗留注释（P0-3，已确认修复）

### 依赖与 CI
- 升级 `github.com/pkoukk/tiktoken-go` 从 v0.1.6 到 v0.1.8
- 并行化 CI 管线，去除 e2e 和 docker 对 test 的串行依赖（PR #99）
- 新增 `.github/workflows/go.yml` 独立 CI 工作流

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