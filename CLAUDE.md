# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 构建

- `go install ./cmd/akswitch/` -> 全局 `akswitch`（`$(go env GOPATH)/bin` 已在 PATH）
- `go mod verify` — 确认依赖完整性
- `make test-all` — 全量测试（unit -> integration -> e2e）
- `make release VERSION=v0.x.x` — 打 tag 推送触发 CI Release
- 所有依赖装在项目级，**禁止污染全局**

**单一测试执行：**
```bash
go test -tags=unit -run TestName ./internal/cmd/          # 单元
go test -tags=integration -run TestName -race .             # 集成
go test -tags=e2e -run TestName -timeout=5m -race .         # E2E
```

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
4. **每个 `init()` 中注册的标志，必须在对应 `*_cmd_test.go` 中有 `Lookup` 测试**（如 `TestXxxCmd_HasYyyFlag`），与标志代码在**同一个 PR** 中加入
5. **边界**：Key <=12 字符时 `MaskKey` 输出 `****`（已在 `utils_test.go` 覆盖）

**测试策略：**
- **主攻方向**：集成验收测试（mock upstream + 真实代理请求），如 `proxy_test.go`
- **测试入口**：所有 CLI 可达路径用 `testhelper.go` 中的 `runCommand()` 或子进程模式（`start_cmd_test.go`）
- **标准**：before/after 对比，不测绝对值快照
- **不写**：mock 掉一切只测 JSON 的 Handler 测试（如 `handlers_test.go`）
- **日志格式测试**：`formatLogLine` 纯函数可在 `unit` 层测试（`log_format_test.go`），无需 HTTP 服务器
- **负载测试**：`test/load/` 下有 `akswitch.exe` 压测脚本

## 发版

**时机：** 一个完整的功能或修复 PR 合并到 main 后，觉得"值得用户更新了"就发。

**版本号规则：**
- `v0.x.0` — 新功能（minor）
- `v0.x.1` — bug 修复（patch）
- 当前最新：v0.3.0（旧测试 tag 已清理）

**发版前确认：** CHANGELOG.md 已更新 → `git add CHANGELOG.md && git commit -m "docs: update changelog for v0.x.x"` → 再打 tag

**流程（二选一）：**
- `make release VERSION=v0.4.0`（等价于 `git tag v0.4.0 && git push origin v0.4.0`）
- 或从 GitHub Actions 页面手动触发 `Build & Release` workflow，填入版本号

**两种触发方式均已配置：** 本地 git push 和 GitHub Actions `workflow_dispatch` 均有效。触发后自动构建 8 平台二进制 + SHA256SUMS + 创建 Release。

## 架构

```
cmd/akswitch/main.go           # 入口，ldflags 注入 version，嵌入 dashboard.html
internal/
  cmd/                         # Cobra CLI 命令层
    root.go                    #   根命令 + version 子命令 + detectServerPort()
    start.go                   #   start -> 解析配置 -> 初始化 provider -> 启动代理
    logs.go                    #   logs 命令（formatLogLine 纯函数，--verbose/--since/--last 标志）
    config.go, provider.go,    #   config/provider/key 增删查改 CLI
    key.go, status.go, ...
    selfrestart.go             #   二进制自监控热重启（开发模式）
    testhelper.go              #   runCommand() CLI 测试辅助函数
  server/                      # HTTP 代理 + 管理 API
    proxy.go                   #   错误码/分类定义（categorizeError/writeProxyError）
    proxy_handler.go           #   反向代理 + key 轮转 + 重试 + Token 计量
    handlers.go                #   管理 API handler（config/key/log-level...）
    admin.go                   #   admin token 鉴权
    router.go                  #   ProviderRouter: 单端口 /{provider}/... 路径路由
    middleware.go              #   敏感 header 过滤、日志
    colorhandler.go            #   slog.ColorHandler（ANSI 彩色 + compact 模式）
    crash.go                   #   panic 恢复
    lifecycle.go               #   后台任务（metric ticker、健康检查、启动 key 探针）
    multihandler.go            #   stderr + 文件双写 slog.Handler
    server.go                  #   HTTP 服务器配置
    manager.go                 #   InstanceManager（旧多端口模式，已废弃但保留）
  keypool/                     # API Key 池
    keypool.go                 #   轮转策略（round-robin + cooldown + 禁用）
    crypto.go                  #   AES-256-GCM 加密存储
    store.go                   #   持久化（文件读写 + 加密）
  circuitbreaker/              # 两层熔断器
    key.go                     #   Key 级熔断（限流退避）
    upstream.go                #   上游级熔断（502/503 -> open -> half-open -> close）
  config/                      # TOML 配置加载
    config_toml.go             #   TOML provider 定义 + XDGConfigPath + FindServerPort
    config_loader.go           #   多源合并（env + TOML）
    config_diff.go             #   热重载 diff 脱敏输出
    config_exports.go          #   测试导出（公开 Config 字段供测试包使用）
  logstore/                    # 请求日志环形缓冲区（线程安全，固定容量，支持 SnapshotSince）
  metrics/                     # Prometheus 指标（所有指标统一注册到 router 级 registry）
  utils/                       # LogEntry 结构体 + MaskKey + CopyHeaders
docs/
  api.md                       # API 端点文档
  architecture.md              # 熔断器架构设计文档
  cli-reference.md             # CLI 命令参考
  configuration.md             # 配置说明
  deployment.md                # Docker 部署与监控栈
  internal/                    # 归档设计文档、测试计划、审查报告（已移入 archive）
```

**关键模式：**
- **ProviderRouter** — 单进程单端口管理多个 provider，`/{provider}/...` 路径路由
- **两层熔断** — Key 级（429 退避）-> 上游级（502/503 熔断），key 级先触发，上游级兜底
- **配置热重载** — 监听 `.env` 变更 -> 计算 diff -> 热更新 key pool，不停机
- **自监控重启** — 开发模式下监控 binary 文件变更，检测到更新后优雅重启
- **双日志通道** — stdout（slog + ColorHandler/multiHandler，运维实时）与 `/logs` API（环形缓冲区，`akswitch logs` 回顾）解耦，互不影响
- **config 子包** — config_toml 定义 TOML schema，config_loader 解析多源，config_diff 计算变更，config_exports 暴露类型给测试
- **启动 key 探针** — 启动时对每个 key 发 `/models` 请求，检测到 401/403 自动禁用
- **ProxyEngine** — 每个 ProviderState 持有独立 ProxyEngine（含 HTTP client + upstream CB），proxy_handler.go 驱动全流程

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

### 关键数据结构：LogEntry

`internal/utils/utils.go` 定义 `/logs` API 和 `akswitch logs` 的 JSON schema：

```go
type LogEntry struct {
    Timestamp       string `json:"timestamp"`          // RFC3339
    Key             string `json:"key"`                // 已脱敏（MaskKey）
    KeyIndex        int    `json:"key_index"`
    KeyName         string `json:"key_name"`
    Method          string `json:"method"`
    URL             string `json:"url"`
    Status          int    `json:"status"`
    RequestBodySize int    `json:"request_body_size"`
    DurationMs      int64  `json:"duration_ms"`
    Retries         int    `json:"retry"`
    Provider        string `json:"provider,omitempty"`
    InputTokens     int    `json:"input_tokens,omitempty"`
    OutputTokens    int    `json:"output_tokens,omitempty"`
}
```

### CLI 标志速查

| 命令 | 标志 | 说明 |
|------|------|------|
| `akswitch start` | `--log-format=default` | stdout 标准模式（默认 `compact`） |
| `akswitch start` | `--provider=NAME` | 只启动指定 provider |
| `akswitch start` | `--all` | 启动所有 provider（默认只启动一个） |
| `akswitch logs` | `--verbose` | 显示完整 method/URL（默认隐藏） |
| `akswitch logs` | `--since=RFC3339` | 只显示此时间后的条目 |
| `akswitch logs` | `--last=N` | 只显示最后 N 条 |

`--log-format=compact` 在 `start.go` 的 `init()` 注册（`startCmd.Flags().String(...)`），通过 `startServer` -> `ApplyLogLevel` 传入 `ColorHandler` 的 `compact` 字段控制日志行格式。

## 工作流

- main 分支受保护，禁止直接推送
- 执行改动前创建功能分支
- 遵循 GitHub Flow + 原子 commit

### 角色分工

**Coder（写代码）**
1. 从 main 创建分支
2. 写代码，确保本地测试通过（`make test-all`）
3. 提交 **Draft PR**（标题写明改动内容）
4. 完成——不等 CI，不合并，不提 auto-merge

需要合并时，调用 merger skill（说"你是Merger"）。

1. **[测试]** — 新增 CLI 命令/标志 -> 对应 CLI 入口测试已写？标志注册测试（`Lookup`）在同一个 PR？
2. **[测试]** — `make test-all` 全量通过？
3. **[手动验收]** — `go install` 编译安装后，按改动类型运行对应验证：

   | 改动类型 | 验证命令 |
   |---------|---------|
   | 新增/修改 CLI 标志 | `akswitch <cmd> --help \| grep <flag>` 确认标志可见 |
   | 新增/修改 CLI 命令 | `akswitch <cmd> --help` 确认子命令存在 |
   | 修改日志格式 | `akswitch start --help | grep log-format` 确认默认值，或 `akswitch start --log-format=default` 切回标准模式 |
   | 逻辑修复 | 用真实场景（启动代理、发送请求）确认修复生效 |

4. **[提交]** — 在正确的分支？提交信息清晰？

### 提交后检查

- **[二进制更新]** — PR 合并后 `go install ./cmd/akswitch/` 更新本地二进制

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