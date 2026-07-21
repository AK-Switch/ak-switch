---\r\nname: merger\r\ndisable-model-invocation: true\r\ndescription: AK-Switch 项目 PR 合并编排。管理合并队列、依赖分析、冲突解决、auto-merge 编排。\r\n---

# Merger — AK-Switch 项目 PR 合并管理

## 核心理念

Merger 不是"点按钮"的工具，而是一个**编排角色**。你的工作是：

1. **看清全局** — 所有 Open PR 的改动范围、CI 状态、依赖关系
2. **制定策略** — 确定合并顺序，识别 supersede 关系，标记阻塞
3. **执行编排** — 解决冲突，触发 CI，启用 auto-merge，监控到完成
4. **处理异常** — flaky test、编译失败、依赖冲突
5. **收尾善后** — 合并后拉取最新 main，编译安装新二进制，确保本地环境与远程一致

---

## 工作流\r\n\r\n以下 7 个阶段构成完整的编排流程。每个阶段结束后，回看全局再进入下一阶段。\r\n\r\n### 第一阶段：盘点

```bash
# 1. 列出所有 Open PR
gh pr list --state open --json number,title,headRefName,baseRefName,mergeStateStatus,author

# 2. 逐一检查关键信息
gh pr view <N> --json number,title,headRefName,mergeStateStatus,mergeable,state,isDraft,autoMergeRequest,reviews,statusCheckRollup
```

**关注点：**
- `mergeStateStatus`: `BEHIND`（落后 main）、`DIRTY`（有冲突）、`BLOCKED`（阻塞）、`UNKNOWN`（待计算）
- `isDraft`: Draft 不能开 auto-merge
- `autoMergeRequest`: 是否已开 auto-merge
- `statusCheckRollup`: CI 检查结果
- `mergeable`: `CONFLICTING` 表示有冲突

**Draft 预检：** 如果 `isDraft == true`，先 `gh pr ready <N>` 标记为 Ready，再继续后续阶段。跳过此步会导致后续开 auto-merge 失败。

### 第二阶段：依赖分析

**这是 Merger 最关键的步骤。** 不要假设 PR 可以独立合并。

```bash
# 1. 收集所有 PR 的改动文件
gh pr diff <N> --name-only | sort

# 2. 当两个或多个 PR 改动了同一个文件 → 存在依赖或冲突
# 3. 检查 commit 祖先关系，判断谁依赖谁
git merge-base --is-ancestor <branch-A> <branch-B> && echo "A is ancestor of B"

# 4. 当两个或多个 PR 都改 internal/ 下的 Go 文件时，检查函数级冲突
gh pr diff <N> | grep '^[+-]func ' | sort
```

**依赖类型判断：**

| 模式 | 含义 | 处理 |
|------|------|------|
| PR-A 创建文件 X，PR-B 也改 X | PR-A 是地基，PR-B 依赖它 | PR-A 先合 |
| PR-A 和 PR-B 都定义同名的类型/函数 | 可能 supersede 关系 | 检查实现是否更完整，关闭旧版 |
| PR-A 和 PR-B 改不同文件 | 无依赖，可独立合并 | 顺序不限 |
| PR-A 合入后改变了函数签名，PR-B 的测试用了旧签名 | 隐式依赖 | PR-A 先合，然后修 PR-B |

**区分 supersede 与互补：**
- 两个 PR 都在同一个包定义 `type Calibrator` 但构造器不同 → supersede
- 检查创建时间，新的通常替代旧的
- 对比 diff 的行数、功能覆盖范围

### 第三阶段：合并排序

```
第一批（无依赖）→ 所有独立 PR
第二批（地基）  → 创建被其他 PR 依赖的文件的 PR
第三批（依赖）  → 依赖于地基 PR 的 PR
```

**对于独立 PR：** 可以直接开 auto-merge
**对于有依赖的 PR：** 关掉依赖方的 auto-merge，等地基合并后再开

### 第四阶段：冲突解决

#### 模式 1: add/add conflict
两地同时创建了同名文件，一方已合入 main。
**解决：** `git checkout --theirs -- <file>` 取 main 版本

#### 模式 2: 同一函数不同改动
两地改了同一函数的同一区域，一方已合入 main。
**解决：** 分析冲突区域，合并两边的功能代码（不是二选一，是合并）

#### 模式 3: 函数签名变更 + 旧调用方
PR-A 改了函数签名（如返回值从 2 个变 3 个），PR-B 的测试文件用了旧签名。
**根因诊断：** 查看 `streamSSEAndEstimateTokens` 等函数的当前签名
**解决：** 更新测试文件中的调用点，匹配新签名

#### 模式 4: 测试逻辑过时
main 的功能已变化，但旧 PR 的测试还按旧行为断言。
**解决：** 更新测试断言匹配新行为；如果旧 PR 的测试是"期望 fall through"，但新行为已接管，则改为"期望 compact 格式输出"

### 第五阶段：CI 管理与分支状态预检

**核心原则：** auto-merge 只有在 PR 的 `mergeStateStatus` 为可合并状态时才会触发。
如果分支落后 main（`BEHIND`），即使开了 auto-merge 也不会合并——它会一直挂在"等待分支更新"状态。
**必须在开 auto-merge 之前确保分支处于最新状态。**

```bash
# 更新分支触发 CI
gh pr update-branch <N>

# 重跑失败的 CI
gh run rerun <run-id>

# 查看完整状态（含 checks 详情）
gh pr view <N> --json statusCheckRollup,mergeStateStatus,mergeable --jq '{mergeStateStatus, mergeable, checks: [.statusCheckRollup[]? | {name, status, conclusion}]}'
```

**Windows 平台轮询方案（PowerShell，替代 bash 循环）：**
```powershell
$maxAttempts = 20
for ($i = 1; $i -le $maxAttempts; $i++) {
    Start-Sleep -Seconds 30
    $pr = gh pr view <N> --json statusCheckRollup,mergeStateStatus,state --jq '{checks: [.statusCheckRollup[]? | {name, status, conclusion}], mergeState: .mergeStateStatus, state: .state}'
    $prObj = $pr | ConvertFrom-Json
    $completed = ($prObj.checks | Where-Object { $_.status -eq "COMPLETED" }).Count
    $total = $prObj.checks.Count
    if ($prObj.state -eq "MERGED") { Write-Host "✅ PR #N merged!"; break }
    Write-Host "[$($i*30)s] $($prObj.mergeState) — CI: $completed/$total completed"
    $prObj.checks | ForEach-Object { Write-Host "  $($_.name): $($_.status) $($_.conclusion)" }
}
```

**CI 失败分类：**

| 症状 | 常见原因 | 处理 |
|------|---------|------|
| `[build failed]` | 编译错误 | 本地重现并修复 |
| `TestMetricsVerification_*` 失败 | flaky test（timing 敏感） | 见"常见陷阱 4"的鲁棒处理流程 |
| `TestCompact_ProxyRequest` 失败 | 测试期望与新行为不匹配 | 更新测试断言 |
| `TestCrashRecover_*` 失败 | 通常是正常的 | 检查是否预期行为 |

**分支状态预检 —— 开 auto-merge 的前置条件：**\r\n\r\n在开 auto-merge 之前，逐个检查每个 PR 的 `mergeStateStatus` 和 `isDraft`：\r\n\r\n```bash\r\ngh pr view <N> --json mergeStateStatus,mergeable,isDraft --jq '{mergeStateStatus, mergeable, isDraft}'\r\n```\r\n\r\n| 条件 | 处理 |\r\n|------|------|\r\n| `isDraft: true` | 先 `gh pr ready <N>` 标记为 Ready |\r\n| `BEHIND` | 先 `gh pr update-branch <N>` 更新分支，等 CI 通过后再开 auto-merge |\r\n| `DIRTY` | 进入 Phase 4 冲突解决，不能开 auto-merge |\r\n| `BLOCKED` | CI 未通过，等 CI 通过或处理失败原因 |\r\n| `CLEAN` | 可以直接开 auto-merge |\r\n| `UNKNOWN` | 等几秒重新检查（刚 push 时常见） |\r\n
**预检直觉：** 如果 `BEHIND` 是因为刚刚有其他 PR 合并到 main，这是正常现象——更新分支后等 CI 通过即可。
如果 `DIRTY` 或 `CONFLICTING`，不要跳过，必须解决冲突。

### 第六阶段：Auto-merge 管理

**开 auto-merge 前，必须确认分支状态预检已通过（Phase 5 已完成）。**

```bash
# 开启（单个）
gh pr merge <N> --squash --auto --subject "<title>"

# 关闭
gh pr merge <N> --disable-auto

# 批量开启（无依赖的 PR）
foreach ($pr in @(1,2,3)) { gh pr merge $pr --squash --auto }
```

**注意事项：**
- `gh pr merge --auto` 成功时无输出（exit code 0），用 `gh pr view <N> --json autoMergeRequest,state --jq '{autoMergeRequest, state}'` 验证：`autoMergeRequest` 不为 null 或 `state == "MERGED"` 均表示生效\r\n- 开了 auto-merge 后如果 PR 状态变为 `DIRTY` → 需要手动解决冲突
- 已开 auto-merge 的 PR 如果被判断为 supersede → 先关 auto-merge 再关闭 PR
- **开 auto-merge 前先检查 `mergeStateStatus`**：如果 `BEHIND`，先更新分支再开，否则开了也合不掉

**BEHIND 状态延迟处理：** `gh pr update-branch` 后 CI 通过但 mergeState 仍是 `BEHIND` 时：
1. 等 10-15 秒让 GitHub 重新计算状态
2. 如果仍是 BEHIND，尝试再次 `gh pr update-branch`
3. 仍然可以开 auto-merge（即使 BEHIND，分支更新后 CI 通过会自动合并）

**CLEAN 状态瞬间合并：** 如果 `mergeStateStatus == CLEAN`，开 auto-merge 后 GitHub 可能立即合并，无需等待。开完后直接验证 `state` 是否为 `MERGED` 即可，不必走轮询流程。

**合并后扫描（横向维护）：**

一个 PR 合并到 main 后，**所有其他 PR 的 `mergeStateStatus` 都可能变为 `BEHIND`**。
不要直接跳到下一件事，必须先扫描所有剩余 PR 的状态：

```bash
# 1. 合并一个 PR 后，扫描所有其他 PR 的 mergeStateStatus
gh pr list --state open --json number,headRefName,mergeStateStatus --jq '.[] | select(.mergeStateStatus == "BEHIND") | "PR #\(.number) is BEHIND"'

# 2. 对每个 BEHIND 的 PR 更新分支（触发 CI 重新运行）
gh pr update-branch <N>

# 3. 等 CI 通过后，已开 auto-merge 的 PR 会自动合并
```

**为什么需要这个步骤：**
auto-merge 开启后，如果 PR 状态变为 `BEHIND`，GitHub 不会自动更新分支，也不会自动合并。
**必须手动 `gh pr update-branch` 来触发 branch update 和 CI 重跑。** 否则 auto-merge 会一直挂在"等待分支更新"状态，直到你手动干预。

**关键判断：** 如果 PR 已经开了 auto-merge，更新分支后 CI 通过，它会自动合并——不需要重新开 auto-merge。

### 第七阶段：Post-merge 本地同步

auto-merge 开启后，合并可能在后台完成。**不要直接跳到下一件事，必须先确认合并完成并同步到本地。**

如果当前在 worktree 中执行合并，Post-merge 同步前先确认 worktree 目录未被自动清理（cleanup hook 可能在合并后触发）。如果目录已不存在，切换到主仓库目录再继续：

```bash
# 0. 确认工作目录有效
if [ ! -d ".git" ]; then
  cd "$(gh repo view --json nameWithOwner --jq '.nameWithOwner' | sed 's/.*\///')" 2>/dev/null || cd $(git rev-parse --show-toplevel 2>/dev/null)
fi

# 1. 轮询等待合并完成（最多等 2 分钟）
for i in $(seq 1 12); do
  state=$(gh pr view <N> --json state --jq '.state')
  if [ "$state" = "MERGED" ]; then
    echo "✅ PR #N 已合并"
    break
  fi
  echo "⏳ 等待合并... ($((i*10))s)"
  sleep 10
done

# 2. 拉取最新 main
git checkout main
git fetch origin

# 尝试快进合并，本地 main 分叉时用 reset 兜底
git merge --ff-only origin/main || {
  echo "本地 main 已分叉，执行 git reset --hard origin/main"
  git reset --hard origin/main
}

# 3. 编译安装最新二进制
go install ./cmd/akswitch/

# 4. 验证安装
akswitch version
```

**AK-Switch 项目特有：**
- 项目 `CLAUDE.md` 明确要求：**PR 合并后 `go install ./cmd/akswitch/` 更新本地二进制**
- 如合并多个 PR，在所有 PR 合并完成后一次性拉取 main + install 即可，不必每合一个就装一次

**验证清单：**

| 步骤 | 命令 | 预期结果 |
|------|------|---------|
| 分支已同步 | `git log --oneline main..origin/main` | 无输出（已是最新） |
| 编译成功 | `go install ./cmd/akswitch/` | 无报错 |
| 二进制可用 | `akswitch version` | 输出版本号 |

---

## 常见陷阱

### 1. 无脑全开 auto-merge
不要一上来就把所有 PR 都开 auto-merge。**必须先做依赖分析。** 否则地基 PR 还没合，依赖 PR 就抢跑，导致冲突。

### 2. 忽略文件行尾问题
AK-Switch 项目使用 CRLF 行尾。在 Windows 上编辑文件时：
- `git diff` / `git show` 在 Bash 中显示 LF 行尾
- 实际文件可能是 CRLF
- 当用 `sed` 或 Python 替换时，CR 可能隐藏在行尾导致匹配失败
- 处理 CRLF 文件时，优先使用 `python3 << 'PYEOF'` 配合二进制模式（`'rb'`/`'wb'`）读写

### 3. flaky test 的鲁棒处理

`TestMetricsVerification_RequestDuration` 等 metrics 验证测试是 timing 敏感的。

**多级处理流程：**

| 层级 | 条件 | 处理 |
|------|------|------|
| 第 1 次失败 | 首次遇到 flaky test | `gh run rerun <run-id> --failed` 重跑 |
| 通过 | 重跑后通过 | 正常继续 |
| 第 2 次失败 | 同一 flaky test 连续 2 次失败 | 检查 CI 日志中具体失败行，确认是纯 timing 偏差还是代码逻辑差异 |
| 纯 timing 偏差 | 测试期望值接近边界值，差值在 10% 以内 | 第三次重跑 |
| 代码逻辑差异 | 测试断言期望的值与实际行为不匹配 | 检查功能代码是否更改了 metrics 行为，考虑更新测试断言 |
| 第 3 次失败 | 连续 3 次失败 | 手动在本地跑一次确认，考虑跳过该测试或标记为 known failure |

**快速判断：** 如果 flaky test 在分支更新后出现（之前已经通过过），且 PR 改动不涉及 metrics 注册、日志格式、handler 逻辑，大概率是纯 timing 偏差，重跑即可。
如果 PR 改动涉及 metrics 相关代码，需要认真检查测试失败原因，不要盲目重跑。

### 4. 忘记清理 worktree
解决冲突后创建的临时 worktree 可能残留。合并完成后清理：
```bash
git worktree remove .claude/worktrees/<name>
```
\r\n
---

## 输出格式

完成合并后，输出以下格式的报告：

```
## ✅ Merger 完成报告

| PR | 状态 | 说明 |
|----|------|------|
| #N | ✅ MERGED | 标题 |
| #N | 🔴 CLOSED | 原因（如被 #N 替代） |

### 处理过程中解决的问题
- 冲突 x: 解决方式

### 当前 main 分支新增内容
- 功能 1
- 功能 2

### 本地二进制更新
- `git pull --ff-only origin main` — ✅ 已同步
- `go install ./cmd/akswitch/` — ✅ 编译成功
- `akswitch version` — 输出: `akswitch version dev`
```
