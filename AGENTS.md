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
    root.go, start.go, logs.go # 根命令、启动、日志
    config.go, key.go, ...     # 配置/密钥/Provider 管理
    selfrestart.go             # 二进制自监控重启
  server/                      # HTTP 代理 + 管理 API
    proxy_executor.go          # 代理生命周期（Key 选择→转发→重试→Token 计量）
    proxy_handler.go           # 路径提取 + 请求分发
    server.go, admin.go        # 引擎初始化、管理 API
    colorhandler.go            # ANSI 彩色日志
    logmanager.go              # 日志级别/格式/文件输出
  keypool/                     # API Key 轮转（round-robin + cooldown）
  circuitbreaker/              # 两层熔断（Key 级 + 上游级）
  config/                      # TOML 配置加载
  logstore/                    # 请求日志环形缓冲区
  metrics/                     # Prometheus 指标
  tracker/                     # Token 校准器（Calibrator）
  tokenestimator/              # tiktoken 估算
  logentry/                    # LogEntry 结构体
docs/
  cli/                         # 自动生成的 CLI 文档
  api.md, architecture.md, ... # API 端点、熔断器设计等
```

## Code Style & Conventions

- **Go 标准格式** — `gofmt`，tab 缩进
- **错误处理** — 不吞错误，不 panic（`crash.go` 做 panic 恢复）
- **测试分层** — 每个测试文件必须加 `//go:build unit` / `integration` / `e2e` 标签
- **CLI 测试模式** — 新增命令加 `TestXxxCmd_Exists`，新增标志加 `TestXxxCmd_HasYyyFlag`
- **提交信息** — `类型: 描述`（`feat`/`fix`/`refactor`/`chore`/`docs`）

## Boundaries

- ✅ **Always**：修改 CLI 命令/标志、修复 bug、添加测试、更新文档
- ⚠️ **Ask first**：修改 `internal/server/` 核心逻辑、新增 provider、改数据库/外部服务
- 🚫 **Never**：直接 push 到 main、force push、修改 keys 存储逻辑、提交敏感信息

## Common Pitfalls

- **测试标签**：每个测试文件必须有 build tag，否则 `make test-unit` 会漏掉
- **Windows 路径**：`~` 在 Git Bash 中解析为 `C:\Users\<user>`，数据目录在 `D:\Users\<user>`
- **SSE 流式 token 估算**：sensenova 在 Anthropic 流式响应中不返回 `usage.output_tokens`（始终为 0），所有 token 均基于 tiktoken 估算，精度 ±10-20%。`content_block_delta` 的 `input_json_delta`（工具调用）也需要累积文本，已修复 #144
- **Calibrator 未启用**：sensenova 不返回实际 token 值，校准器无训练数据，修正系数恒为 1.0
- **Dev 模式日志共享**：`--dev` 实例与生产实例共享全局 `slog.Default()`，日志级别会互相影响

## PR / Commit 格式

提交信息：`类型: 描述`（`feat`/`fix`/`refactor`/`chore`/`docs`）

流程：创建分支 → 实现 + 测试 → Draft PR → review → Ready for Review → auto-merge