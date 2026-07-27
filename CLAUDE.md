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

### 抓取 SSE 流式数据

设置 debug 级别后，服务器日志中会输出 `sse raw line` 条目，包含原始 SSE 事件内容（`data:` 行）。用于诊断 sensenova 等上游的流式响应格式。

## 日志分析

用增量 checkpoint 模式，禁止全量扫描：
1. 读上次分析的 checkpoint
2. `akswitch logs --since=<checkpoint>` 只拉新日志
3. 分析完成后更新 checkpoint

## Agent 工具

- Issue tracker: `docs/agents/issue-tracker.md`
- Triage 标签: `docs/agents/triage-labels.md`
- 领域文档: `docs/agents/domain.md`