# AK-Switch — AGENTS.md

Go 语言 API key 轮转代理，嵌入 CC Switch 作为 provider，用于多 key 之间的负载均衡和熔断。

## Build & Test

| 命令 | 用途 |
|------|------|
| `go install ./cmd/akswitch/` | 编译安装到 `$(go env GOPATH)/bin` |
| `go mod verify` | 确认依赖完整性 |
| `make build` | 编译到 `bin/` |
| `make test-unit` | 单元测试（标签 `unit`，上限 `<=1s`） |
| `make test-integration` | 集成测试（标签 `integration`，mock upstream，上限 `<=10s`） |
| `make test-e2e` | E2E 测试（标签 `e2e`，需真实上游，上限 `<=2m`） |
| `make test-all` | 全量测试（unit → integration → e2e） |

```bash
# 单测
go test -tags=unit -count=1 -short ./internal/...

# 集成
go test -tags=integration -count=1 -race ./test/integration/

# E2E
go test -tags=e2e -count=1 -timeout=5m -race ./test/integration/
```

## Project Structure

```
cmd/akswitch/main.go           # 入口
internal/
  cli/                         # Cobra CLI 命令层
    root.go, start.go, config.go, key.go, ...
    selfrestart.go             # 二进制自监控重启
  server/                      # HTTP 代理 + 管理 API
    proxy_executor.go          # 代理生命周期
    proxy_handler.go           # 路径提取 + 请求分发
    server.go, admin.go        # 引擎初始化、管理 API
    colorhandler.go            # ANSI 彩色日志
    logmanager.go              # 日志级别/格式/文件输出
  keypool/                     # API Key 轮转
  circuitbreaker/              # 两层熔断
  config/                      # TOML 配置加载
  metrics/                     # Prometheus 指标
  tracker/                     # Token 校准器
  tokenestimator/              # tiktoken 估算
  logentry/                    # LogEntry 结构体
docs/
  cli/                         # 自动生成的 CLI 文档
  architecture.md, api.md      # 架构设计、API 端点
```

## Architecture

关键模式（详见 [docs/architecture.md](./docs/architecture.md)）：
- **ProviderRouter + ProxyExecutor** — 单端口 `/{provider}/...` 路由，ProxyExecutor 执行完整生命周期
- **两层熔断** — Key 级（429 退避）→ 上游级（502/503 熔断）
- **配置热重载** — 监听 `config.toml` 变更，热更新 key pool，不停机
- **启动 key 探针** — 启动时检测 401/403 自动禁用无效 key

## CLI 标志速查

| 命令 | 标志 | 说明 |
|------|------|------|
| `akswitch start` | `--log-format=default` | stdout 标准模式（默认 `compact`） |
| `akswitch start` | `--provider=NAME` | 只启动指定 provider |
| `akswitch start` | `--all` | 启动所有 provider（默认只启动一个） |
| `akswitch logs` | `--verbose` | 显示完整 method/URL（默认隐藏） |
| `akswitch logs` | `--since=RFC3339` | 只显示此时间后的条目 |
| `akswitch logs` | `--last=N` | 只显示最后 N 条 |

`--log-format=compact` 在 `start.go` 的 `init()` 注册（`startCmd.Flags().String(...)`），通过 `startServer` -> `ApplyLogLevel` 传入 `ColorHandler` 的 `compact` 字段控制日志行格式。

## Code Style & Conventions

- **Go 标准格式** — `gofmt`，tab 缩进
- **错误处理** — 不吞错误，不 panic（`crash.go` 做 panic 恢复）
- **测试分层** — 每个测试文件必须加 `//go:build unit` / `integration` / `e2e` 标签
- **CLI 测试模式** — 新增命令加 `TestXxxCmd_Exists`，新增标志加 `TestXxxCmd_HasYyyFlag`

## CLI Design Rules

- **verb-first** — 所有命令以动词开头（`status`、`config list`、`key add`），而非名词（`switch status`）
- **provider 参数统一位置** — provider 名称始终作为第一个位置参数（`<provider>` 必填或 `[provider]` 可选）。例外：`config get` 和 `config set` 以 key 为主、provider 为上下文，provider 放在最后
- **必填用尖括号，可选用方括号** — `<name>` 表示必填，`[provider]` 表示可选
- **flag 语义一致** — 同含义的 flag 在不同命令中使用相同的名称和类型（如 `--target`、`--cooldown-sec`）
- **无使用场景的字段不保留** — 不添加不使用的配置字段、flag 或路由逻辑
- **CLI 测试覆盖** — 新增命令加 `TestXxxCmd_Exists`，新增标志加 `TestXxxCmd_HasYyyFlag`，参数变化加输出断言测试

## Testing

四个设计原则：
1. **测试即规格** — 测试定义"应该输出什么"，代码是实现。改动输出格式前，先改测试定义新格式。
2. **反馈速度决定分层** — 纯函数在 unit 层测，CLI 解析在 unit 层测，集成 mock upstream 验证完整流程。
3. **测试服务于变更** — 每个测试对应一个"未来有人会改这个"的假设，如 `TestXxxCmd_HasYyyFlag` 对应"未来有人删这个 flag"。
4. **测试即文档** — 断言直接写出期望值，而不是"不崩就行"。

策略：集成验收为主（mock upstream + 真实代理请求），测试入口用 `testhelper.go` 的 `runCommand()`，CLI 测试必须包含输出断言。

## Workflow

main 分支受保护，禁止直接推送。遵循 GitHub Flow + 原子 commit。

1. **创建分支** — `git checkout -b feature/xxx main`
2. **实现代码** — 写功能逻辑
3. **写测试** — 按上方 Testing 原则添加
4. **验证新测试** — `go test -tags=unit -run TestXxx ./internal/cli/`
5. **验证全量** — `make test-all`
6. **手动验收** — 按改动类型运行对应验证
7. **提交 Draft PR** — 标题写明改动内容，不等 CI
8. **审查 PR** — 调用 review agent 审查（非 trivial 变更）
9. **决策** — 无阻塞 → Ready for Review + auto-merge；有小问题 → 修复后重推；有大问题 → 报告

提交后 `go install ./cmd/akswitch/` 更新本地二进制。

## Issue 关闭规范

PR 合并后，对应的 issue 必须关闭。两种方式：

1. **PR 描述中写关闭关键词** — `Closes #N`、`Fixes #N`、`Resolves #N`（GitHub 自动识别并关闭）
2. **PR 合并后发现漏关** — 立即补一条 `Closes #N` 的评论，或手动 `gh issue close N`

**规则：** 一个 PR 只关自己对应的 issue，不要顺手关别的。

## Dev Environment

- Go 1.23+（构建使用 Go 1.26）
- 主要开发平台：Windows
- 跨平台：Linux、macOS（通过 GitHub Actions CI 验证）
- 无需外部依赖（纯 Go 标准库 + Cobra CLI + Prometheus client）

## Boundaries

- **Always**：修改 CLI 命令/标志、修复 bug、添加测试、更新文档
- **Ask first**：修改 `internal/server/` 核心逻辑、新增 provider、改数据库/外部服务
- **Never**：直接 push 到 main、force push、修改 keys 存储逻辑、提交敏感信息

**提交规范**：`类型: 描述`（`feat`/`fix`/`refactor`/`chore`/`docs`）。

**发版**：PR 合并后 `make release VERSION=v0.x.x`，或从 GitHub Actions 触发 `Build & Release` workflow。新功能 `v0.x.0`，bug 修复 `v0.x.1`。

## Common Pitfalls

- **测试标签**：每个测试文件必须有 build tag，否则 `make test-unit` 会漏掉
- **Windows 路径**：`~` 在 Git Bash 中解析为 `C:\Users\<user>`，数据目录在 `D:\Users\<user>`
- **SSE 流式 token 估算**：sensenova 在 Anthropic 流式响应中不返回 `usage.output_tokens`（始终为 0），所有 token 均基于 tiktoken 估算，精度 ±10-20%。`content_block_delta` 的 `input_json_delta`（工具调用）也需要累积文本，已修复 #144
- **Calibrator 未启用**：sensenova 不返回实际 token 值，校准器无训练数据，修正系数恒为 1.0

## Debug 指南

日志位置、格式、LogEntry 字段、运行时配置、常见排查见 [docs/troubleshooting.md](./docs/troubleshooting.md)。

**设置日志级别**（无需重启）：
```bash
curl -X POST http://localhost:4000/api/log-level \
  -H "Content-Type: application/json" \
  -d '{"level":"debug"}'      # 设为 debug
curl -X POST ... -d '{"level":"info"}'  # 恢复 info
```

**Dev 模式**：`akswitch start --dev --provider=sensenova` 启动独立实例（端口自动递增），用于查看 stdout 日志和抓取 SSE 原始数据。注意：`--dev` 实例与生产实例共享全局 `slog.Default()`。

**抓取 SSE 流式数据**：设置 debug 级别后，服务器日志中会输出 `sse raw line` 条目，包含原始 SSE 事件内容（`data:` 行）。

## Token 计量

Token 估算基于 tiktoken，在 `internal/tokenestimator/` 中实现。

**流程：** 请求发送前估算 input_tokens → 响应到达后提取/估算 output_tokens → Calibrator 在滑动窗口中校准。

**限制：** sensenova 的 Anthropic 流式响应不返回 `usage.output_tokens`（始终为 0），所有 token 均基于 tiktoken 估算，精度 ±10-20%。Calibrator 修正系数恒为 1.0。非流式请求返回实际值，可用于校准。

详见 `internal/tracker/calibration.go` 和 [docs/architecture.md](./docs/architecture.md)。

## 文档纪律

每次变更都必须同步更新文档。这是**纪律，不是建议**。

| 变更类型 | 必须同步更新 | 时机 |
|---------|------------|------|
| 新增/修改 CLI 命令或标志 | `docs/cli-reference.md` | 同一 PR |
| 新增/修改配置字段 | `docs/configuration.md` | 同一 PR |
| 新增/修改 API 端点 | `docs/api.md` | 同一 PR |
| 新增功能影响架构 | `docs/architecture.md` | 同一 PR |
| Release notes | GitHub Releases | 发版后 |

**核心原则：** 文档和代码在同一次合并中到达 main。先合并代码后补文档 = 文档永远补不上。

## CC Switch 关系

[CC Switch](https://github.com/AK-Switch/cc-switch) 是上游 API 的聚合网关，提供统一格式的 Anthropic 兼容接口。AK-Switch 将 CC Switch 作为一个 provider 嵌入，在其之上做多 key 之间的负载均衡和熔断。

**分工：**
- **CC Switch** — 上游聚合、协议转换、多 provider 路由
- **AK-Switch** — 单 provider 内的 API key 轮转、熔断、Token 计量
