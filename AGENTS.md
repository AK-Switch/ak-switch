# AK-Switch — AGENTS.md

API Key 智能轮转代理（Go 1.26）。单 provider 内多 Key 轮询、429 退避、401/403 永久禁用、两层熔断（Key 级 + 上游级）、Token 用量追踪。与 ccswitch 互补（provider 级路由 vs key 级轮转）。

## Dev Environment Tips

| Command | Purpose |
|---------|---------|
| `go build ./cmd/akswitch/` | Build binary |
| `go install ./cmd/akswitch/` | Install to GOPATH/bin |
| `make test-unit` | Run unit tests |
| `make test-integration` | Run integration tests (race detector) |
| `make test-e2e` | Run E2E tests (Windows only) |
| `make lint` | Run golangci-lint |
| `make vet` | Run go vet |
| `make fmt` | Format with gofmt |
| `make check` | Lint + vet + fmt |

**Go 环境**: 需要 Go 1.26+。通过 `go env` 验证版本。

## Build & Test

| Scope | Command |
|-------|---------|
| Build | `go build ./cmd/akswitch/` |
| Unit tests | `go test -tags=unit -count=1 -short ./internal/...` |
| Single test | `go test -tags=unit -count=1 ./internal/server/ -run TestName` |
| Integration | `go test -tags=integration -count=1 -race ./test/integration/` |
| E2E | `go test -tags=e2e -count=1 -timeout=5m -race ./test/integration/` |
| Lint | `golangci-lint run ./...` |
| Vet | `go vet ./...` |
| Format | `go fmt ./...` |

## Project Structure

| Directory | Purpose |
|-----------|---------|
| `cmd/akswitch/` | CLI 入口（main.go + dashboard HTML） |
| `internal/server/` | 核心：HTTP 代理引擎、Admin API、ProviderState、生命周期 |
| `internal/circuitbreaker/` | 两层熔断：Key 级 + 上游级（UpstreamCB） |
| `internal/keypool/` | API Key 池：轮询、RPM 计数、冷却、持久化 |
| `internal/config/` | TOML 配置加载与热重载 |
| `internal/metrics/` | Prometheus 指标 |
| `internal/tokenestimator/` | Token 估算（tiktoken）+ 校准 |
| `internal/tracker/` | 请求追踪与校准器 |
| `internal/cli/` | Cobra CLI 子命令 |
| `internal/logentry/` | 日志条目结构 |
| `test/integration/` | 集成与 E2E 测试 |
| `docs/superpowers/` | 设计文档（specs/ + plans/） |
| `deployments/` | 部署配置（Windows 服务等） |
| `web/` | Dashboard 前端资源 |

## Code Style & Conventions

- **格式化**: `gofmt` 风格，tab 缩进。运行 `make fmt` 或 `go fmt ./...`
- **测试标签**: 单元测试用 `//go:build unit`，集成用 `integration`，E2E 用 `e2e`
- **导入分组**: 标准库 → 第三方 → 本地包，每组间空行
- **错误处理**: 始终检查 error，用 `fmt.Errorf("context: %w", err)` 包装
- **日志**: `slog` 结构化日志，包含 provider/key 等关键字段
- **文件大小**: `internal/server/*.go` 控制在 ~400 行以下。超过时拆分
- **命名**: Go 标准约定。`ProviderState` 方法 API 用大写导出，内部字段小写

## Architecture

```
HTTP Request
  → ProviderRouter (路径路由: /{provider}/...)
    → ProxyExecutor.Execute()
      → KeyPool（Key 选择、RPM 感知）
        → CircuitBreaker（Key 级熔断 + 冷却）
      → UpstreamCircuitBreaker（上游级熔断）
      → HTTP Client → 上游 API
    → Admin API（/admin/* 运行时管理）
      → Runtime Config（热重载，descriptor table 模式）
      → Key 持久化（OS Keyring / 加密文件）
```

**关键类型**: `ProviderState` 封装 provider 运行时状态（name/config/pool/proxy），通过方法 API 暴露。修改 `internal/server/router.go` 了解完整方法列表。

## Boundaries

### Always
- 在 `internal/server/` 内修改代理逻辑、Admin API
- 在 `internal/` 内添加新包
- 在 `cmd/akswitch/` 内修改 CLI 命令
- 在 `docs/superpowers/specs/` 添加设计文档，`docs/superpowers/plans/` 添加实施计划

### Ask First
- 修改 `internal/config/` 配置结构（影响 TOML 序列化与热重载）
- 修改 `internal/circuitbreaker/` 熔断逻辑（影响稳定性）
- 修改 Key 持久化方式（`internal/keypool/keyring.go`）
- 删除或重命名公共方法（破坏 API 兼容性）

### Never
- 将 API Key 明文写入日志（使用 `MaskSensitiveData`）
- 在非 E2E 测试中硬编码真实 Key
- 直接暴露 `AdminToken` 原始值（仅提供 `HasAdminToken()` / `CheckAdminToken()`）
- 修改 `vendor/` 目录（无 vendor 目录，用 go modules）
- 在代理路径中做同步阻塞操作（影响吞吐量）

## Common Pitfalls

- **Windows 路径**: E2E 测试仅在 `windows-latest` 运行。集成测试用 `-race`，在 Linux CI 上执行
- **测试标签**: 单元测试必须带 `//go:build unit` 标签，否则 CI 的 unit job 不会运行它们
- **gofmt 后再提交**: 项目强制 gofmt，提交前运行 `make fmt`
- **ProviderState 字段**: 所有字段已小写化（`name`, `config`, `pool`, `proxy`），外部必须通过方法访问。直接访问小写字段会编译失败
- **配置热重载**: 运行时修改通过 descriptor table 模式，见 `admin_api.go` 的 `runtimeConfigField` 表
- **CircuitBreaker 范围**: Key 级 CB 在 `internal/circuitbreaker/key.go`，上游级 CB 在 `internal/circuitbreaker/upstream.go`，不要混用
