# CLAUDE.md

@AGENTS.md

## 工作流

main 分支受保护，禁止直接推送。遵循 GitHub Flow + 原子 commit。

### Coder 流程

1. **创建分支** — `git checkout -b feature/xxx main`
2. **实现代码** — 写功能逻辑
3. **写测试** — 按 AGENTS.md 中的测试模式添加
4. **验证新测试** — `go test -tags=unit -run TestXxx ./internal/cli/`
5. **验证全量** — `make test-all`
6. **手动验收** — 按改动类型运行对应验证
7. **提交 Draft PR** — 标题写明改动内容，不等 CI
8. **审查 PR** — 调用 review agent 审查（非 trivial 变更）
9. **决策** — 根据审查结果：无阻塞 → Ready for Review + auto-merge；有小问题 → 修复后重推；有大问题 → 报告

### 提交后

- 合并后 `go install ./cmd/akswitch/` 更新本地二进制

## 调试指南

### 设置日志级别

运行时通过 API 设置（无需重启）：
```bash
curl -X POST http://localhost:4000/api/log-level \
  -H "Content-Type: application/json" \
  -d '{"level":"debug"}'      # 设为 debug
curl -X POST ... -d '{"level":"info"}'  # 恢复 info
```

### Dev 模式调试

`akswitch start --dev` 启动独立实例，不干扰生产。典型用途：
- 查看 stdout 日志（生产实例的日志不可见）
- 抓取 SSE 原始数据（配合 debug 级别）
- 测试新功能

```bash
akswitch start --dev --provider=sensenova
# 默认端口被占时自动递增，如 4001/4002
```

> **注意：** `--dev` 实例与生产实例共享全局 `slog.Default()`，日志级别会互相影响。

### 抓取 SSE 流式数据

设置 debug 级别后，服务器日志中会输出 `sse raw line` 条目，包含原始 SSE 事件内容（`data:` 行）。用于诊断 sensenova 等上游的流式响应格式。

## 日志分析

用增量 checkpoint 模式，禁止全量扫描：
1. 读上次分析的 checkpoint
2. `akswitch logs --since=<checkpoint>` 只拉新日志
3. 分析完成后更新 checkpoint

## Token 计量

Token 估算基于 tiktoken，在 `internal/tokenestimator/` 中实现。

**流程：** 请求发送前用 tiktoken 估算 input_tokens → 响应到达后从 SSE 事件提取 output_tokens（或估算）→ Calibrator 在滑动窗口中比较估算值与实际值。

**限制：** sensenova 的 Anthropic 流式响应不返回 `usage.output_tokens`（始终为 0），所有 token 均基于 tiktoken 估算，精度 ±10-20%。Calibrator 收不到训练数据，修正系数恒为 1.0。非流式请求返回实际 token 值，可用于校准。

详见 `internal/tracker/calibration.go` 和 `docs/architecture.md`。

## CC Switch 关系

[CC Switch](https://github.com/AK-Switch/cc-switch) 是上游 API 的聚合网关，提供统一格式的 Anthropic 兼容接口。AK-Switch 将 CC Switch 作为一个 provider 嵌入，在其之上做多 key 之间的负载均衡和熔断。

**分工：**
- **CC Switch** — 上游聚合、协议转换、多 provider 路由
- **AK-Switch** — 单 provider 内的 API key 轮转、熔断、Token 计量

AK-Switch 不重复造 CC Switch 的轮子，专注于 key 池管理。

## Agent 工具

- Issue tracker: `docs/agents/issue-tracker.md`
- Triage 标签: `docs/agents/triage-labels.md`
- 领域文档: `docs/agents/domain.md`