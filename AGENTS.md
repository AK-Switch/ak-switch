# AK-Switch — AGENTS.md

AK-Switch 是一个 AI API Key 代理网关（Go 1.26 + Cobra），为多个 LLM provider 提供统一的 API 接入层，内置两层熔断器、Key 轮询和 Prometheus 指标。单可执行文件，支持 Docker 部署。

## Dev Environment Tips

- 安装依赖: `go mod download`
- 构建: `make build`（输出 `bin/akswitch`）
- Docker 本地运行: `docker compose up -d`（需要 `deployments/docker-compose.yml`）
- Go 1.26 必须安装。验证: `go version`

## Build & Test

| 命令 | 用途 |
|------|------|
| `make build` | 编译二进制 |
| `make fmt` | 格式化源码 (`go fmt`) |
| `make lint` | golangci-lint |
| `make vet` | `go vet ./...` |
| `make check` | lint + vet + fmt（提交前必须执行） |
| `make test-unit` | 单元测试 (`-tags=unit`) |
| `make test-integration` | 集成测试 (`-tags=integration`, 需要 Docker) |
| `make test-e2e` | E2E 测试 (`-tags=e2e`, 仅 Windows) |
| `make test-all` | 全量测试 |

单包测试: `go test -tags=unit -count=1 ./internal/server/`
单测试: `go test -tags=unit -count=1 -run TestName ./internal/server/`

## Project Structure

- `cmd/akswitch/` — 入口 (main.go + 嵌入 dashboard.html)
- `internal/cli/` — Cobra CLI 命令 (key, provider, start, stop, reload, config, status)
- `internal/server/` — HTTP 服务器、反向代理、Admin API、路由、Provider 管理
- `internal/config/` — TOML 配置加载、默认值合并、运行时配置热更新
- `internal/circuitbreaker/` — 两层熔断器 (KeyCircuitBreaker + UpstreamCircuitBreaker)
- `internal/keypool/` — Key 池管理、OS Keyring 持久化、存储抽象
- `internal/metrics/` — Prometheus 指标
- `internal/tracker/` — Token 使用量校准
- `internal/tokenestimator/` — Token 估算
- `internal/logentry/` — 日志条目类型
- `test/integration/` — 集成/E2E 测试
- `docs/` — 架构文档、ADR、设计文档
- `deployments/` — Docker Compose、Grafana、Prometheus 配置
- `web/` — 前端页面模板

## Code Style & Conventions

- Tab 缩进（项目强制，gofmt 已配置）
- 遵循 Effective Go + Go Code Review Comments
- 错误包装: `fmt.Errorf("函数名: %w", err)`
- 日志: `slog` (结构化日志)
- Table-driven tests 优先
- 导入: 标准库 → 项目内部包 → 第三方包，按字母序排列

### ProviderState 封装模式（重要）

`ProviderState` 所有字段均为私有，通过 getter/setter 方法访问：

```go
// ✅ 正确 — 使用 getter
timeout := ps.HTTPTimeoutSec()
ps.SetMaxRetries(3)

// ❌ 错误 — 直接访问字段（字段私有，编译不通过）
// ps.config.MaxRetries = 3
```

新增配置字段时，同步在 `ProviderState` 上添加 getter 和 setter。

## Architecture Notes

```
请求 → UpstreamCircuitBreaker → KeyPool → KeyCircuitBreaker → 上游
```

- **两层熔断器**: UpstreamCB 处理 502/503，KeyCB 处理 429/401/403
- **Key 持久化三级策略**: OS Keyring → 加密文件 → 明文 fallback
- **运行时配置热更新**: 通过 Admin API 修改配置，descriptor table 模式，无需重启
- **Config 结构**: `Config` 内嵌 `ProviderConfig`（启动配置）+ 独立 `RuntimeConfig`（运行时配置）
- 详细架构见 [docs/architecture.md](./docs/architecture.md)

## Boundaries

- ✅ Always: 通过 `ProviderState` getter/setter 访问 Provider 状态，不直接操作字段
- ✅ Always: 提交前执行 `make check && make test-unit`
- ✅ Always: 新增代码附带 table-driven test
- ⚠️ Ask first: 修改 `internal/config/` 结构体定义（影响 TOML 解析和 API 契约）
- ⚠️ Ask first: 修改熔断器状态机逻辑（影响故障恢复行为）
- ⚠️ Ask first: 修改 Dockerfile 基础镜像或暴露端口
- 🚫 Never: 直接访问 `ProviderState` 私有字段
- 🚫 Never: 在 `internal/` 下引入外部框架（保持纯 Go stdlib + 少量已审计依赖）
- 🚫 Never: 提交包含真实 API Key 的 `keys.json` 或 `.env` 文件
- 🚫 Never: 在单元测试中使用 `panic()` 作为错误处理

## Common Pitfalls

- **构建标签**: 单元测试用 `-tags=unit`，集成测试用 `-tags=integration`。裸跑 `go test` 可能跳过有 `//go:build` 约束的测试文件
- **E2E 仅限 Windows**: E2E 测试依赖 Windows 平台特性，Linux CI 上只跑 unit + integration
- **Key 持久化不自动**: `PersistKeys()` 需手动调用，修改 Key 池后不持久化则重启丢失
- **Makefile 用 Tab**: 编辑 Makefile 时确保用 Tab 而非空格缩进（golangci-lint 不检查 Makefile，但 `make` 会报错）
- **Config 字段分两组**: 启动配置在 `ProviderConfig`（TOML 解析），运行时配置在 `RuntimeConfig`（Admin API 热更新）。改配置字段前先确认属于哪组
