---
name: cleanup-worktrees
description: >
  清理已完成但残留的 git worktree。当用户提到 worktree 太多、清理 branch、stale worktree、或 worktree 堆积时触发。
  这个 skill 提供当前项目的 worktree 清理方案说明和手动清理脚本。
---

# Worktree 清理系统

AK-Switch 项目有三层 worktree 清理机制，**所有 AI agent 都应了解并遵循**。

## 三层清理机制

### 1. 自动：cleanupPeriodDays

`.claude/settings.json` 中配置了 `worktree.cleanupPeriodDays: 7`。

- Claude Code **每次启动时**自动扫描 `.claude/worktrees/`
- 删除超过 7 天且**同时满足**以下条件的 worktree：
  - 无未提交的变更
  - 无未跟踪的文件
  - 无未推送的 commit
- 这是兜底机制，不需要手动干预。

### 2. 半自动：post-merge hook

`.git/hooks/post-merge` 中配置了 git hook。

- 每次 `git pull` 或 `git merge` 到 `main`/`master` 分支后**自动触发**
- 运行清理脚本，删除远程分支已不存在的 worktree 和本地分支
- 不需要手动干预，但前提是 hook 文件存在

### 3. 手动：清理脚本

当需要**立即**清理时，运行脚本：

```powershell
.claude\skills\cleanup-worktrees\scripts\cleanup-stale-worktrees.ps1           # 执行清理
.claude\skills\cleanup-worktrees\scripts\cleanup-stale-worktrees.ps1 -DryRun   # 预览，不删除
```

也可以使用 `--dry-run`（或 `-DryRun`）预览。

## 安全机制

脚本有**四层防御**，确保不会误删：

| 层 | 机制 | 保护什么 |
|----|------|---------|
| 1 | 检查远程分支是否存在 | 只删 PR 已合并的 worktree |
| 2 | 检查 dirty / untracked | 保护未完成的工作 |
| 3 | 检查 unpushed commits | 保护未推送的变更 |
| 4 | git 自身安全机制 | `git worktree remove` 拒绝删 dirty 目录 |

## 清理后恢复

如果误删了 worktree，恢复很简单：

```bash
# 分支和 commit 都在，只是目录被删了
git worktree add .claude/worktrees/<name> <branch>
# 或直接切过去
git checkout <branch>
```

## 排查当前状态

```bash
# 查看所有 worktree
git worktree list

# 查看残留目录
ls .claude/worktrees/

# 查看 orphan 分支
git branch | grep worktree-
```

## 依赖

- Git
- PowerShell（脚本用 `.ps1` 编写）

## 注意

- 只需要 `worktree-*` 前缀的分支才是由 Claude Code 自动创建的，其他分支不会被清理
- 脚本只删除 `worktree-*` 分支，不会动 feature / bugfix 分支
- `post-merge hook` 仅当合并到 `main`/`master` 时触发，合并到其他分支不会触发