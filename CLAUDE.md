# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 构建

```bash
go install ./cmd/akswitch/     # 安装到 $(go env GOPATH)/bin
go build -o akswitch.exe ./cmd/akswitch/  # 本地编译
```

## 架构概览

### 两层熔断器

```
请求 → UpstreamCircuitBreaker (502/503/网络错误) → KeyPool (轮询) → KeyCircuitBreaker (429 退避/401 永久禁用) → 上游
```

- **UpstreamCircuitBreaker** — 跟踪上游 502/503 和网络错误，CLOSED → OPEN → HALF_OPEN 状态机，不影响 Key 健康度
- **KeyCircuitBreaker** — 每个 Key 独立，跟踪 429（指数退避）和 401/403（永久禁用），CLOSED → OPEN → PERMA
- **KeyPool** — 线程安全轮询，跳过冷却/禁用中的 Key，全冷却时等待最短可用时间 + jitter 避免 thundering herd

### 包职责

| 包 | 职责 |
|---|---|
| `internal/server` | HTTP 服务、路由、代理请求处理、管理 API、日志初始化 |
| `internal/keypool` | Key 轮询、熔断器协调、加密存储 (AES-256-GCM) |
| `internal/circuitbreaker` | Key 级和上游级熔断器状态机 |
| `internal/config` | TOML 加载、验证、热重载 diff |
| `internal/cmd` | Cobra CLI 命令：start, stop, status, provider, key, logs, config |
| `internal/logstore` | 内存环形日志缓冲区（固定 10000 条） |
| `internal/metrics` | Prometheus 指标注册 |
| `internal/utils` | MaskKey、LogEntry、CopyHeaders 等工具 |

### 路由（单端口 + 路径前缀）

- `/health` — 健康检查
- `/logs` — 内存日志快照
- `/dashboard` — 内置 Web 面板
- `/api/config` — 配置查看
- `/api/keys` — Key CRUD
- `/api/stats` — 统计
- `/api/reload` — 配置热重载
- `/metrics` — Prometheus
- `/{provider}/...` — 代理请求（路径首段为 provider 名）

## 测试

### 分层标签

```bash
go test -tags=unit -count=1 -short ./internal/...   # ≤1s，纯逻辑无 IO
go test -tags=integration -count=1 -race ./          # ≤10s，CLI + mock HTTP
go test -tags=e2e -count=1 -timeout=5m -race ./      # ≤2m，子进程 + 端口绑定
make test-all                                         # 按 unit → integration → e2e 顺序全量
```

### 测试原则

- 主攻集成验收测试（mock upstream + 真实代理请求）
- CLI 命令测试必须包含输出断言（`assertOutputContains` 或类似）
- 禁止 mock 掉一切只测 JSON 的 Handler 测试
- 边界：Key ≤12 字符时 `MaskKey` 输出 `****`

## 运维排查

### 日志文件

- **主日志**: `C:/Users/86150/AppData/Roaming/akswitch/akswitch.log`（由 `~/.akswitch/config.toml` 的 `log_file` 配置）
- **Crash 日志**: `~/.akswitch/crash.log`（panic 恢复时写入）
- **配置**: `~/.akswitch/config.toml`
- **日志轮转**: lumberjack，默认 100MB 轮转，保留 7 天

### 日志格式 (slog 结构化)

```
time=... level=INFO|WARN|ERROR msg="proxy request|proxy success|key network error|key rate limited" ...
```

### 快速定位问题

```bash
grep "level=ERROR" <log_file>              # 启动失败、端口冲突、shutdown 超时
grep "key rate limited" <log_file>         # 上游限流 429
grep "key network error" <log_file>        # 网络连接问题
grep "key permanently disabled" <log_file> # 401/403 永久禁用
```

## 项目定位

akswitch 只专注于单 provider 内的 API key 轮转，不重复造 ccswitch 的轮子。ccswitch 负责 provider 级路由，akswitch 负责 provider 内 key 级轮转与限流处理。

## 工作流

- main 分支受保护，禁止直接推送
- 执行改动前创建功能分支（`feature/xxx` / `bugfix/xxx` / `docs/xxx`）
- 遵循 GitHub Flow + 原子 commit
- 提交 PR 后在前台等 CI 绿

### 提交前检查清单

1. 新增 CLI 命令/标志 → 对应 CLI 入口测试已写？
2. `go test ./...` 全量通过？
3. `go install` 后用真实二进制验证了行为？
4. 在正确的分支？提交信息清晰？

### 提交后检查

5. 前台等到 CI 绿？