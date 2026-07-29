# AK-Switch — AGENTS.md

Go 语言 API key 轮转代理，嵌入 CC Switch 作为 provider，用于多 key 之间的负载均衡和熔断。

## Build & Test

| 命令 | 用途 |
|------|------|
| `go install ./cmd/akswitch/` | 编译安装到 `$(go env GOPATH)/bin` |
| `go mod verify` | 确认依赖完整性 |
| `make test-unit` | 单元测试（标签 `unit`） |
| `make test-integration` | 集成测试（标签 `integration`，mock upstream） |
| `make test-e2e` | E2E 测试（标签 `e2e`，需真实上游） |
| `make test-all` | 全量测试（unit → integration → e2e） |

单测执行：
```bash
go test -tags=unit -run TestName ./internal/cli/          # 单元
go test -tags=integration -run TestName -race ./test/integration/  # 集成
go test -tags=e2e -run TestName -timeout=5m -race ./test/integration/  # E2E
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
docs/
  cli/                         # 自动生成的 CLI 文档
  architecture.md, api.md      # 架构设计、API 端点
```

## Architecture

关键模式（详见 [docs/architecture.md](./docs/architecture.md)）：
- **ProviderRouter + ProxyExecutor** — 单端口 `/{provider}/...` 路由，ProxyExecutor 执行完整生命周期
- **两层熔断** — Key 级（429 退避）→ 上游级（502/503 熔断）
- **配置热重载** — 监听 `.env` 变更，热更新 key pool，不停机
- **启动 key 探针** — 启动时检测 401/403 自动禁用无效 key

## Code Style & Conventions

- **Go 标准格式** — `gofmt`，tab 缩进
- **错误处理** — 不吞错误，不 panic（`crash.go` 做 panic 恢复）
- **测试分层** — 每个测试文件必须加 `//go:build unit` / `integration` / `e2e` 标签
- **CLI 测试模式** — 新增命令加 `TestXxxCmd_Exists`，新增标志加 `TestXxxCmd_HasYyyFlag`

## Testing

测试设计原则：测试即规格、反馈速度决定分层、服务于变更、测试即文档。

详细规范见 [docs/guides/testing.md](./docs/guides/testing.md)。单测命令：
```bash
go test -tags=unit -run TestName ./internal/cli/          # 单元
go test -tags=integration -run TestName -race ./test/integration/  # 集成
go test -tags=e2e -run TestName -timeout=5m -race ./test/integration/  # E2E
```

## Boundaries

- ✅ **Always**：修改 CLI 命令/标志、修复 bug、添加测试、更新文档
- ⚠️ **Ask first**：修改 `internal/server/` 核心逻辑、新增 provider、改数据库/外部服务
- 🚫 **Never**：直接 push 到 main、force push、修改 keys 存储逻辑、提交敏感信息

**提交规范**：`类型: 描述`（`feat`/`fix`/`refactor`/`chore`/`docs`）。PR 合并后 `go install ./cmd/akswitch/` 更新本地二进制。

**发版**：PR 合并后 `git commit` 更新 CHANGELOG → `make release VERSION=v0.x.x`，或从 GitHub Actions 触发 `Build & Release` workflow。新功能 `v0.x.0`，bug 修复 `v0.x.1`。

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

## 日志分析

日志文件为 JSON 格式（每行一条），使用标准工具分析：
- `tail -f <log_file>` — 实时查看日志
- `grep '"proxy success"' <log_file>` — 筛选请求日志
- `jq 'select(.status >= 400)' <log_file>` — 筛选失败请求

## Token 计量

Token 估算基于 tiktoken（`internal/tokenestimator/`），流程和限制详见 [docs/architecture.md](./docs/architecture.md#token-校准)。

sensenova 流式响应不返回 `usage.output_tokens`（始终为 0），Calibrator 修正系数恒为 1.0。非流式请求返回实际 token 值，可用于校准。

## CC Switch 关系

[CC Switch](https://github.com/AK-Switch/cc-switch) 是上游 API 的聚合网关，提供统一格式的 Anthropic 兼容接口。AK-Switch 将 CC Switch 作为一个 provider 嵌入，在其之上做多 key 之间的负载均衡和熔断。

**分工：**
- **CC Switch** — 上游聚合、协议转换、多 provider 路由
- **AK-Switch** — 单 provider 内的 API key 轮转、熔断、Token 计量

## Agent 工具

- Issue tracker: `docs/agents/issue-tracker.md`
- Triage 标签: `docs/agents/triage-labels.md`
- 领域文档: `docs/agents/domain.md`
