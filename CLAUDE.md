## 构建

- `go install ./cmd/akswitch/` → 全局 `akswitch`（`%USERPROFILE%\go\bin\` 已在 PATH）
- 所有依赖装在项目级，**禁止污染全局**

## 工作流

- main 分支受保护，禁止直接推送
- 执行改动前创建功能分支
- 遵循 GitHub Flow + 原子 commit
- 提交 PR 后在前台等 CI 绿

### 提交前检查清单（强制）

声明"完成"前必须逐条核查。**跳过任意一条 = 任务未完成。**

1. **[测试]** — 新增 CLI 命令/标志 → 对应 CLI 入口测试已写？
2. **[测试]** — `go test ./...` 全量通过？
3. **[手动验收]** — `go install` 后用真实二进制验证了行为？
4. **[提交]** — 在正确的分支？提交信息清晰？

### 提交后检查（强制）

5. **[CI]** — 前台等到 CI 绿？

### 任务收尾清理（PR 合并后/确定不合并后执行）

**这条规则不是说给人类听的，是说给 AI 自己听的。** AI 每次会话独立，必须靠规则而不是记忆来收尾。

PR 合并后（或确定不合并后），按以下顺序执行，**少一步算任务没完成**：

6. **[分支清理·本地]** — `git branch -d <分支>`（squash merge 导致 git 判不准时用 `-D`）
7. **[分支清理·远程]** — `git push origin --delete <分支>`
8. **[Issue 关闭]** — 如果有关联的 Issue，关闭并说明原因
9. **[worktree 清理]** — 如果使用了 EnterWorktree，退出时选择 **remove**（除非该分支后续还要继续工作）
10. **[main 同步]** — `git checkout main && git pull --rebase`

**例外：** 分支还有 OPEN 的 PR 或未完成的工作 → 保留，不要删。

**为什么 AI 需要这个规则：** AI 的"完成"定义天然太窄——代码改了、测试过了、PR 创建了 → AI 认为任务结束了。但分支还留在原地，日积月累就变成 15 个本地分支、29 个远程分支。这个清单把"收尾"变成"任务完成"的一部分，而不是可选的后续步骤。

## 测试策略

- **主攻方向**：集成验收测试（mock upstream + 真实代理请求），如 `proxy_test.go`
- **测试入口**：所有 CLI 可达路径用 `runCommand()` 或子进程模式
- **标准**：before/after 对比，不测绝对值快照
- **不写**：mock 掉一切只测 JSON 的 Handler 测试（如 `handlers_test.go`）
- **边界**：Key ≤12 字符时 `MaskKey` 输出 `****`

## 项目定位

akswitch 只专注于单 provider 内的 API key 轮转，不重复造 ccswitch 的轮子。

## 测试规范

测试按速度分层，用 //go:build 标签区分：

- `unit`: ≤1s，纯逻辑无 IO
- `integration`: ≤10s，CLI 命令 + mock HTTP
- `e2e`: ≤2m，子进程 + 端口绑定

### 新增测试文件规则

1. 先判断所属层级，加对应 `//go:build` 标签
2. CLI 命令测试必须包含输出断言（`assertOutputContains` 或类似）
3. 禁止无输出断言的 `runCommand` 模式（只测不崩不算测完）

## Agent skills

### Issue tracker

Issue 使用 GitHub Issues 跟踪。外部 PR 不作为 triage 流程的需求来源。详见 `docs/agents/issue-tracker.md`。

### Triage 标签

五个标准角色全部使用默认标签名。详见 `docs/agents/triage-labels.md`。

### 领域文档

单上下文布局。详见 `docs/agents/domain.md`。