# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 构建

- `go install ./cmd/akswitch/` → 全局 `akswitch`（`$(go env GOPATH)/bin` 已在 PATH）
- `make test-all` — 全量测试（unit → integration → e2e）
- `make release VERSION=v0.x.x` — 打 tag 推送触发 CI Release
- 所有依赖装在项目级，**禁止污染全局**

## 测试

测试按速度分层，用 `//go:build` 标签区分：

| 层级 | 标签 | 上限 | 命令 |
|------|------|------|------|
| 单元 | `unit` | ≤1s | `make test-unit` |
| 集成 | `integration` | ≤10s | `make test-integration` |
| E2E | `e2e` | ≤2m | `make test-e2e` |

**新增测试文件规则：**
1. 先判断所属层级，加对应 `//go:build` 标签
2. CLI 命令测试必须包含输出断言（`assertOutputContains` 或类似）
3. 禁止无输出断言的 `runCommand` 模式（只测不崩不算测完）
4. **边界**：Key ≤12 字符时 `MaskKey` 输出 `****`（已在 `utils_test.go` 覆盖）

**测试策略：**
- **主攻方向**：集成验收测试（mock upstream + 真实代理请求），如 `proxy_test.go`
- **测试入口**：所有 CLI 可达路径用 `runCommand()` 或子进程模式（`testhelper.go`）
- **标准**：before/after 对比，不测绝对值快照
- **不写**：mock 掉一切只测 JSON 的 Handler 测试（如 `handlers_test.go`）

## 架构

```
cmd/akswitch/main.go           # 入口，ldflags 注入 version
internal/
  cmd/                         # Cobra CLI 命令层
    root.go                    #   根命令 + version 子命令
    start.go                   #   start → 解析配置 → 初始化 provider → 启动代理
    config.go, provider.go,    #   config/provider/key 增删查改 CLI
    key.go, status.go, ...
    selfrestart.go             #   二进制自监控热重启（开发模式）
  server/                      # HTTP 代理 + 管理 API
    proxy.go                   #   反向代理 + key 轮转 + 重试
    handlers.go                #   管理 API handler（config/key/log-level...）
    admin.go                   #   admin token 鉴权
    router.go                  #   ProviderRouter: 多 provider 各自独立端口
    middleware.go              #   敏感 header 过滤、日志
    crash.go                   #   panic 恢复
  keypool/                     # API Key 池
    keypool.go                 #   轮转策略（round-robin + cooldown + 禁用）
    crypto.go                  #   AES-256-GCM 加密存储
    store.go                   #   持久化（文件读写 + 加密）
  circuitbreaker/              # 两层熔断器
    key.go                     #   Key 级熔断（限流退避）
    upstream.go                #   上游级熔断（502/503 → open → half-open → close）
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
- **两层熔断** — Key 级（429 退避）→ 上游级（502/503 熔断），key 级先触发，上游级兜底
- **配置热重载** — 监听 `.env` 变更 → 计算 diff → 热更新 key pool，不停机
- **自监控重启** — 开发模式下监控 binary 文件变更，检测到更新后优雅重启

## 工作流

- main 分支受保护，禁止直接推送
- 执行改动前创建功能分支
- 遵循 GitHub Flow + 原子 commit
- 提交 PR 后在前台等 CI 绿

### 提交前检查清单（强制）

1. **[测试]** — 新增 CLI 命令/标志 → 对应 CLI 入口测试已写？
2. **[测试]** — `make test-all` 全量通过？
3. **[手动验收]** — `go install` 后用真实二进制验证了行为？
4. **[提交]** — 在正确的分支？提交信息清晰？

### 提交后检查（强制）

5. **[CI]** — 前台等到 CI 绿？

## 项目定位

akswitch 只专注于单 provider 内的 API key 轮转，不重复造 ccswitch 的轮子。

## Agent 工具

- Issue tracker: `docs/agents/issue-tracker.md` — GitHub Issues，外部 PR 不参与 triage
- Triage 标签: `docs/agents/triage-labels.md` — 五个标准角色，默认标签名
- 领域文档: `docs/agents/domain.md` — 单上下文布局
