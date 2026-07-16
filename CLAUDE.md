# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 构建

- `go install ./cmd/akswitch/` -> 全局 `akswitch`（`$(go env GOPATH)/bin` 已在 PATH）
- `make test-all` — 全量测试（unit -> integration -> e2e）
- `make release VERSION=v0.x.x` — 打 tag 推送触发 CI Release
- 所有依赖装在项目级，**禁止污染全局**

## 测试

测试按速度分层，用 `//go:build` 标签区分：

| 层级 | 标签 | 上限 | 命令 |
|------|------|------|------|
| 单元 | `unit` | <=1s | `make test-unit` |
| 集成 | `integration` | <=10s | `make test-integration` |
| E2E | `e2e` | <=2m | `make test-e2e` |

**新增测试文件规则：**
1. 先判断所属层级，加对应 `//go:build` 标签
2. CLI 命令测试必须包含输出断言（`assertOutputContains` 或类似）
3. 禁止无输出断言的 `runCommand` 模式（只测不崩不算测完）
4. **边界**：Key <=12 字符时 `MaskKey` 输出 `****`（已在 `utils_test.go` 覆盖）

**测试策略：**
- **主攻方向**：集成验收测试（mock upstream + 真实代理请求），如 `proxy_test.go`
- **测试入口**：所有 CLI 可达路径用 `runCommand()` 或子进程模式（`testhelper.go`）
- **标准**：before/after 对比，不测绝对值快照
- **不写**：mock 掉一切只测 JSON 的 Handler 测试（如 `handlers_test.go`）
- **日志格式测试**：`formatLogLine` 纯函数可在 `unit` 层测试（`log_format_test.go`），无需 HTTP 服务器

## 发版

**时机：** 一个完整的功能或修复 PR 合并到 main 后，觉得"值得用户更新了"就发。

**版本号规则：**
- `v0.x.0` — 新功能（minor）
- `v0.x.1` — bug 修复（patch）
- 当前最新：v0.3.0（旧测试 tag 已清理）

**流程（二选一）：**
- `make release VERSION=v0.4.0`（等价于 `git tag v0.4.0 && git push origin v0.4.0`）
- 或从 GitHub Actions 页面手动触发 `Build & Release` workflow，填入版本号

**两种触发方式均已配置：** 本地 git push 和 GitHub Actions `workflow_dispatch` 均有效。触发后自动构建 8 平台二进制 + SHA256SUMS + 创建 Release。

## 架构

```
cmd/akswitch/main.go           # 入口，ldflags 注入 version
internal/
  cmd/                         # Cobra CLI 命令层
    root.go                    #   根命令 + version 子命令
    start.go                   #   start -> 解析配置 -> 初始化 provider -> 启动代理
    logs.go                    #   logs 命令（formatLogLine 纯函数，--verbose 标志）
    config.go, provider.go,    #   config/provider/key 增删查改 CLI
    key.go, status.go, ...
    selfrestart.go             #   二进制自监控热重启（开发模式）
  server/                      # HTTP 代理 + 管理 API
    proxy.go                   #   反向代理 + key 轮转 + 重试
    handlers.go                #   管理 API handler（config/key/log-level...）
    admin.go                   #   admin token 鉴权
    router.go                  #   ProviderRouter: 多 provider 各自独立端口
    middleware.go              #   敏感 header 过滤、日志
    colorhandler.go            #   slog.ColorHandler（ANSI 彩色 + compact 模式）
    crash.go                   #   panic 恢复
  keypool/                     # API Key 池
    keypool.go                 #   轮转策略（round-robin + cooldown + 禁用）
    crypto.go                  #   AES-256-GCM 加密存储
    store.go                   #   持久化（文件读写 + 加密）
  circuitbreaker/              # 两层熔断器
    key.go                     #   Key 级熔断（限流退避）
    upstream.go                #   上游级熔断（502/503 -> open -> half-open -> close）
  config/                      # TOML 配置加载
    config_toml.go             #   TOML provider 定义
    config_loader.go           #   多源合并（env + TOML）
    config_diff.go             #   热重载 diff 脱敏输出
  logstore/                    # 请求日志环形缓冲区
  metrics/                     # Prometheus 指标导出
  utils/                       # MaskKey 等工具函数
```

**关键模式：**
- **ProviderRouter** — 单进程管理多个 provider，每个独立端口 + 独立 key pool
- **两层熔断** — Key 级（429 退避）-> 上游级（502/503 熔断），key 级先触发，上游级兜底
- **配置热重载** — 监听 `.env` 变更 -> 计算 diff -> 热更新 key pool，不停机
- **自监控重启** — 开发模式下监控 binary 文件变更，检测到更新后优雅重启
- **双日志通道** — stdout（slog + ColorHandler，运维实时）与 `/logs` API（环形缓冲区，`akswitch logs` 回顾）解耦，互不影响

### 双数据通道

| 通道 | 端点 | 用途 | 适合回答 |
|------|------|------|----------|
| Prometheus 指标 | `GET /metrics` | 聚合趋势 + 告警 | "多少？"、"趋势？" |
| 请求日志 | `GET /logs` | 单请求详情 | "为什么？"、"哪个 key？" |

**所有指标统一注册在 router 级 registry（`pr.metricsRegistry`），通过 `GET /metrics` 暴露。** 每个 provider 的 `NewProviderState` 不再创建独立 registry——历史教训，勿重蹈。

可用指标一览：

| 指标名 | 类型 | labels | 说明 |
|--------|------|--------|------|
| `akswitch_requests_total` | Counter | `method, status, key_index` | 代理请求计数 |
| `akswitch_request_duration_seconds` | Histogram | `method, status` | 请求延迟分布 |
| `akswitch_keypool_keys` | Gauge | `provider, state` | Key 池状态（active/cooling/disabled） |
| `akswitch_upstream_errors_total` | Counter | `type` | 上游错误（network/rate_limited/auth_rejected/server_error） |
| `akswitch_upstream_cb_state` | Gauge | `provider` | 上游熔断器（0=CLOSED/1=OPEN/2=HALF_OPEN） |
| `akswitch_healthcheck_probes_total` | Counter | `provider, status` | 健康检查探针（ok/fail） |
| `akswitch_healthcheck_duration_seconds` | Histogram | `provider` | 健康检查延迟 |

**添加新指标的步骤：**
1. 在 `internal/metrics/metrics.go` 的 `Metrics` 结构体加字段
2. 在 `NewRegistry()` 中用 `factory.New*Vec()` 注册（带 label 定义）
3. 在代码中通过 `pr.metrics.YourMetric` 写入——**不要创建新 registry**

### CLI 标志速查

| 命令 | 标志 | 说明 |
|------|------|------|
| `akswitch start` | `--log-format=compact` | stdout 精简模式（默认 `default`） |
| `akswitch logs` | `--verbose` | 显示完整 method/URL（默认隐藏） |
| `akswitch logs` | `--since=RFC3339` | 只显示此时间后的条目 |
| `akswitch logs` | `--last=N` | 只显示最后 N 条 |

## 工作流

- main 分支受保护，禁止直接推送
- 执行改动前创建功能分支
- 遵循 GitHub Flow + 原子 commit

### 角色分工

**Coder（写代码）**
1. 从 main 创建分支
2. 写代码，确保本地测试通过（`make test-all`）
3. 提交 **Draft PR**（标题写明改动内容）
4. 完成，不等 CI，不合并

**Merger（合并）**
- 仓库已启用 Merge Queue + Auto-merge
- PR 进入 Merge Queue 后自动排队 → CI 测试合并后状态 → 全绿自动合并
- 如需手动操作：`gh pr merge <number> --auto --merge`

### 提交前检查清单（强制）

## 日志分析

**禁止全量扫描。** 用增量 checkpoint 模式：

1. 读上次分析的 checkpoint（在 `memory/log-analysis-jul-2026.md` 或分析报告的 `last_analyzed_at`）
2. `akswitch logs --since=<checkpoint>` 只拉新日志
3. 分析完成后，更新 checkpoint 到最新日志时间戳
4. 将发现追加到上次报告，不重写全量

- `/logs` API 支持 `?since=RFC3339`，CLI 透传 `--since`
- `akswitch logs --verbose` 显示完整 method/URL（默认精简 status + key + duration）
- 首次分析不需要 `--since`（= 全量，建立基线）
- 先看 `/metrics` 聚合数据再扫日志，减少 80% 的手动扫日志需求

## 项目定位

akswitch 只专注于单 provider 内的 API key 轮转，不重复造 ccswitch 的轮子。

## Agent 工具

- Issue tracker: `docs/agents/issue-tracker.md` — GitHub Issues，外部 PR 不参与 triage
- Triage 标签: `docs/agents/triage-labels.md` — 五个标准角色，默认标签名
- 领域文档: `docs/agents/domain.md` — 单上下文布局