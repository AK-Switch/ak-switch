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

### 分层

测试按速度分层，用 `//go:build` 标签区分：

| 层级 | 标签 | 上限 | 命令 |
|------|------|------|------|
| 单元 | `unit` | <=1s | `make test-unit` |
| 集成 | `integration` | <=10s | `make test-integration` |
| E2E | `e2e` | <=2m | `make test-e2e` |

### 原则

写测试时，用以下四个原则指导决策，而不是记忆具体规则。

**1. 测试是规格**
测试定义"应该输出什么"，代码是实现。改动输出格式前，先改测试定义新格式，再改代码。
- `TestCompact_Acceptance_SingleProvider` 的断言直接写入期望的日志格式字符串，一眼就知道终端输出长什么样
- `TestCompact_Acceptance_NonRetryableError` 断言 `✗ 429 (    d1-1    )`，明确了错误行的格式布局
- 作为对比：`TestCompact_ProxyRequest` 只检查包含"POST"和"/v1/messages"，没有写出完整格式，就不够规格

**2. 反馈速度决定一切**
测试分层就是为了保证速度，慢的测试等于没有测试。
- 纯函数（`formatLogLine`、`truncateKeyName`、`MaskKey`）在 unit 层测，不需要启动服务器
- CLI 命令解析在 unit 层测（`runCommand` + 输出断言），不需要真实代理
- 集成测试（`proxy_test.go`）mock upstream 做真实代理请求，验证完整流程，上限 10s
- 不写：mock 掉一切只测 JSON 序列化的 Handler 测试（如 `handlers_test.go`）——它既不够快（要启动服务器）也不够真（不经过真实代理逻辑）

**3. 测试服务于变更**
每个测试都应该对应一个"未来有人会改这个"的假设，没有假设就不需要测试。
- `TestXxxCmd_HasYyyFlag` 对应"未来有人会删这个 flag"的假设，与标志代码在同一个 PR 中加入
- `TestCompact_Acceptance_LongKeyName` 对应"未来有人会改 truncate 逻辑"的假设
- `TestCompact_Acceptance_Retry` 对应"未来有人会改重试显示位置"的假设
- `TestMaskKey_ShortKey` 对应"未来有人会改 MaskKey 边界"的假设
- 反过来：`formatLogLine` 不需要测空输入，因为调用方永远不会传空值

**4. 测试是文档**
读测试应该比读代码更快理解功能。测试断言直接写出期望值，而不是"不崩就行"。
- 验收测试剥离 ANSI 码后，断言字符串就是终端输出的真实样子，不需要看 `colorhandler.go` 的格式化逻辑
- before/after 对比优于绝对值快照，因为前者表达的是"变化了什么"，后者是"当前值是什么"（容易过时）
- 负载测试脚本放在 `test/load/`，独立于 unit 测试，因为它们是不同读者的文档

### 策略

- **主攻方向**：集成验收测试（mock upstream + 真实代理请求），如 `proxy_test.go`
- **测试入口**：所有 CLI 可达路径用 `testhelper.go` 中的 `runCommand()` 或子进程模式（`start_cmd_test.go`）
- **build tag**：每个测试文件必须加对应层级的 `//go:build` 标签
- **CLI 测试**：必须包含输出断言，禁止无断言的 `runCommand` 模式

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
        store.go                   #   持久化（keyring + JSON 文件读写）
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
  cli-reference.md             # CLI 命令参考（入口，指向自动生成文档）
  cli/                         # 从代码自动生成的 CLI 命令文档
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

main 分支受保护，禁止直接推送。遵循 GitHub Flow + 原子 commit。

### 通用规则

- **新增 CLI 命令/标志，必须在同一 PR 中写好对应的入口测试**，详见下方 Coder 流程
- 标志注册测试固定模式：`provider_cmd_test.go` 中的 `TestKeyAddCmd_HasNameFlag`（`Flags().Lookup("name")`）
- 新增命令定义测试固定模式：`TestKeyUpdateCmd_Exists`（检查变量非 nil）
- 提交前必须跑完**新测试** + **全量回归**，两步分开执行

### Coder 流程

1. **创建分支** — `git checkout -b feature/xxx main`
2. **实现代码** — 写功能逻辑
3. **写测试** — 在 `provider_cmd_test.go`（或对应文件）中按上述模式添加测试：
   - 新增命令 → 加 `TestXxxCmd_Exists`
   - 新增标志 → 加 `TestXxxCmd_HasYyyFlag`
4. **验证新测试** — `go test -tags=unit -run TestXxx ./internal/cmd/` 确认新测试通过
5. **验证全量** — `make test-all`
6. **手动验收** — `go install` 编译后按改动类型运行对应验证：

   | 改动类型 | 验证命令 |
   |---------|---------|
   | 新增/修改 CLI 标志 | `akswitch <cmd> --help \| grep <flag>` 确认标志可见 |
   | 新增/修改 CLI 命令 | `akswitch <cmd> --help` 确认子命令存在 |
   | 修改日志格式 | `akswitch start --help \| grep log-format` 确认默认值，或 `akswitch start --log-format=default` 切回标准模式 |
   | 逻辑修复 | 用真实场景（启动代理、发送请求）确认修复生效 |

7. **提交 Draft PR** — 标题写明改动内容，不等 CI、不合并、不提 auto-merge
8. **审查 PR** — 调用 review agent 对 PR 进行审查（非 trivial 变更）
9. **决策** — 根据审查结果：
   - 无阻塞问题 → 转为 Ready for Review，设置 auto-merge
   - 有小问题 → 修复后重新推送，审查通过再合并
   - 有大问题 → 停止推进，向用户报告

### 提交后检查

- 合并后 `go install ./cmd/akswitch/` 更新本地二进制

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