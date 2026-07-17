<#
.SYNOPSIS
    清理已完成但残留的 worktree 目录和本地分支。

.DESCRIPTION
    安全标准：只删除远程分支已不存在的 worktree（PR 已合并或 closed 的信号）。
    有 dirty / untracked / unpushed 的 worktree 会被跳过（git 自身机制兜底）。

.PARAMETER DryRun
    预览模式，不执行任何删除。

.EXAMPLE
    .\scripts\cleanup-stale-worktrees.ps1
    .\scripts\cleanup-stale-worktrees.ps1 -DryRun
#>

param(
    [switch]$DryRun = $false
)

$ErrorActionPreference = 'Continue'

# 项目根目录（脚本在 scripts/ 下）
$REPO_ROOT = git rev-parse --show-toplevel 2>$null
if ($LASTEXITCODE -ne 0) {
    Write-Error "此脚本必须在 AK-Switch 项目目录下运行"
    exit 1
}
Set-Location $REPO_ROOT

$WORKTREE_DIR = Join-Path (Join-Path $REPO_ROOT ".claude") "worktrees"
$REMOTE = "origin"

# 计数器
$script:cleanCount = 0
$script:skipCount = 0
$script:errorCount = 0
$script:staleCount = 0
$script:orphanClean = 0
$script:orphanSkip = 0

# 结果收集
$script:results = [System.Collections.ArrayList]@()

# ============================================================
# 辅助函数
# ============================================================

# 获取已注册的 worktree 路径列表
function Get-RegisteredWorktreePaths {
    git worktree list --porcelain | Select-String "^worktree " | ForEach-Object {
        $_ -replace "^worktree ", ""
    }
}

# 判断是否主仓库路径
function Test-IsMainRepo {
    param([string]$Path)
    $pNorm = Resolve-Path $Path -ErrorAction SilentlyContinue
    $mainNorm = Resolve-Path $REPO_ROOT
    return ($pNorm.Path -eq $mainNorm.Path)
}

# 记录结果
function Add-Result {
    param([string]$Type, [string]$Name)
    $script:results.Add("${Type}:${Name}") | Out-Null
}

# ============================================================
# 第一部分：清理非注册的残留目录
# ============================================================
Write-Host "=== 第一部分：检查残留目录（非注册 worktree） ==="

if (Test-Path $WORKTREE_DIR) {
    # 收集已注册的 worktree 路径
    $registeredPaths = @(Get-RegisteredWorktreePaths | ForEach-Object {
        try { (Resolve-Path $_ -ErrorAction Stop).Path } catch { $_ }
    })

    # 检查每个目录
    Get-ChildItem -Path $WORKTREE_DIR -Directory | ForEach-Object {
        $dirPath = $_.FullName
        $name = $_.Name
        $dirNorm = (Resolve-Path $dirPath).Path

        $isRegistered = $false
        foreach ($rp in $registeredPaths) {
            if ($rp -eq $dirNorm) {
                $isRegistered = $true
                break
            }
        }

        if (-not $isRegistered) {
            Write-Host "  残留目录: $name"
            if (-not $DryRun) {
                Remove-Item -Recurse -Force $dirPath -ErrorAction SilentlyContinue
                $script:staleCount++
            } else {
                $script:staleCount++
            }
        }
    }

    if ($script:staleCount -gt 0) {
        Write-Host "  → 共 $($script:staleCount) 个残留目录"
    } else {
        Write-Host "  → 无残留目录"
    }
}

# ============================================================
# 第二部分：检查已注册的 worktree
# ============================================================
Write-Host ""
Write-Host "=== 第二部分：检查已注册的 worktree ==="

# 解析 porcelain 格式，每个 worktree 以空行分隔
$wtList = git worktree list --porcelain
$currentWt = @{}
$processedCount = 0

foreach ($line in $wtList) {
    if ($line -match "^worktree (.+)") {
        $currentWt.Path = $matches[1]
    } elseif ($line -match "^HEAD (.+)") {
        $currentWt.Head = $matches[1]
    } elseif ($line -match "^branch refs/heads/(.+)") {
        $currentWt.Branch = $matches[1]
    } elseif ($line -eq "detached") {
        $currentWt.Detached = $true
    } elseif ($line -eq "") {
        # 空行 = 一个 worktree 记录结束
        if ($currentWt.ContainsKey("Path") -and -not (Test-IsMainRepo $currentWt.Path)) {

            $path = $currentWt.Path
            $branch = if ($currentWt.ContainsKey("Branch")) { $currentWt.Branch } else { "" }
            $detached = if ($currentWt.ContainsKey("Detached")) { $true } else { $false }
            $name = Split-Path $path -Leaf

            # 检查 dirty / untracked
            $dirtyCount = 0
            $statusOut = git -C $path status --porcelain 2>$null
            if ($LASTEXITCODE -eq 0 -and $statusOut) {
                $dirtyCount = @($statusOut).Count
            }

            # 检查 unpushed
            $unpushedCount = 0
            if (-not [string]::IsNullOrEmpty($branch) -and -not $detached) {
                # 先检查是否有 upstream（避免 git cherry 报错）
                $null = git -C $path rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>$null
                if ($LASTEXITCODE -eq 0) {
                    $cherryOut = git -C $path cherry 2>$null
                    if ($cherryOut) {
                        $unpushedCount = @($cherryOut).Count
                    }
                }
            }

            # 检查远程分支
            $remoteExists = $false
            $remoteReachable = $false

            if (-not [string]::IsNullOrEmpty($branch) -and -not $detached) {
                $lsRemoteOut = git ls-remote --heads $REMOTE $branch 2>$null
                if ($LASTEXITCODE -eq 0 -and ($lsRemoteOut | Select-String "refs/heads/$branch" -Quiet)) {
                    $remoteExists = $true
                    $remoteReachable = $true
                } else {
                    # 第二次 ls-remote 检查远程是否可达
                    $null = git ls-remote --heads $REMOTE 2>$null
                    if ($LASTEXITCODE -eq 0) {
                        $remoteReachable = $true
                    }
                }
            } else {
                # detached HEAD：只检查远程是否可达
                $null = git ls-remote --heads $REMOTE 2>$null
                if ($LASTEXITCODE -eq 0) {
                    $remoteReachable = $true
                }
            }

            # === 决策逻辑 ===
            if ($dirtyCount -gt 0 -or $unpushedCount -gt 0) {
                $reason = ""
                if ($dirtyCount -gt 0) { $reason += " ${dirtyCount}dirty" }
                if ($unpushedCount -gt 0) { $reason += " ${unpushedCount}unpushed" }
                Write-Host "  跳过: $name ($($reason.TrimStart()))"
                Add-Result -Type "S" -Name $name
                $script:skipCount++
            } elseif (-not $remoteReachable) {
                Write-Host "  跳过: $name (远程不可达，无法判断)"
                Add-Result -Type "S" -Name $name
                $script:skipCount++
            } elseif ($remoteExists) {
                Write-Host "  跳过: $name (远程分支 $branch 仍存在)"
                Add-Result -Type "S" -Name $name
                $script:skipCount++
            } else {
                # 远程分支不存在 + 干净 → 可安全删除
                if ($detached) {
                    Write-Host "  清理: $name (detached HEAD, 干净)"
                } else {
                    Write-Host "  清理: $name (分支: $branch)"
                }

                if (-not $DryRun) {
                    # 删除 worktree
                    git worktree remove $path 2>$null
                    if ($LASTEXITCODE -eq 0) {
                        Write-Host "    → worktree 已删除"
                        if (-not [string]::IsNullOrEmpty($branch) -and -not $detached) {
                            git branch -D $branch 2>$null
                            if ($LASTEXITCODE -eq 0) {
                                Write-Host "    → 本地分支已删除: $branch"
                            } else {
                                Write-Host "    → 本地分支删除失败"
                            }
                        }
                        Add-Result -Type "C" -Name $name
                        $script:cleanCount++
                    } else {
                        Write-Host "    → worktree 删除失败"
                        Add-Result -Type "E" -Name $name
                        $script:errorCount++
                    }
                } else {
                    Add-Result -Type "C" -Name $name
                    $script:cleanCount++
                }
            }

            $processedCount++
        }

        # 重置
        $currentWt = @{}
    }
}

# ============================================================
# 第三部分：孤儿 worktree 记录清理
# ============================================================
Write-Host ""
Write-Host "=== 第三部分：清理孤儿 worktree 记录 ==="
if (-not $DryRun) {
    git worktree prune 2>$null | Out-Null
    Write-Host "  → 已完成"
}

# ============================================================
# 第四部分：清理孤儿本地分支（worktree 已删但分支仍残留）
# ============================================================
Write-Host ""
Write-Host "=== 第四部分：清理孤儿本地分支 ==="

# 获取所有已注册 worktree 的分支列表
$activeWtBranches = @()
Get-RegisteredWorktreePaths | ForEach-Object {
    $wtBranch = git -C $_ rev-parse --abbrev-ref HEAD 2>$null
    if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrEmpty($wtBranch) -and $wtBranch -ne "HEAD") {
        $activeWtBranches += $wtBranch
    }
}

# 遍历所有本地 worktree-* 分支
git branch | ForEach-Object {
    $branch = $_ -replace "^\s*\*?\s*", ""  # 去掉空格和 *
    if ($branch -like "worktree-*") {
        # 检查是否有对应的 worktree
        if ($activeWtBranches -contains $branch) {
            return  # 有对应 worktree，跳过
        }

        # 检查远程分支是否存在
        $null = git ls-remote --heads $REMOTE $branch 2>$null
        if ($LASTEXITCODE -eq 0 -and (git ls-remote --heads $REMOTE $branch 2>$null | Select-String "refs/heads/$branch" -Quiet)) {
            Write-Host "  跳过: $branch (远程分支仍存在)"
            $script:orphanSkip++
        } else {
            Write-Host "  清理: $branch"
            if (-not $DryRun) {
                git branch -D $branch 2>$null
                if ($LASTEXITCODE -eq 0) {
                    $script:orphanClean++
                } else {
                    Write-Host "    → 删除失败"
                    $script:orphanSkip++
                }
            } else {
                $script:orphanClean++
            }
        }
    }
}

if ($script:orphanClean -gt 0 -or $script:orphanSkip -gt 0) {
    Write-Host "  → 已清理: $($script:orphanClean), 跳过: $($script:orphanSkip)"
} else {
    Write-Host "  → 无孤儿分支"
}

# ============================================================
# 汇总
# ============================================================
Write-Host ""
Write-Host "=== 汇总 ==="
Write-Host "  已清理 worktree+分支: $($script:cleanCount)"
Write-Host "  已跳过:              $($script:skipCount)"
Write-Host "  残留目录:            $($script:staleCount)"
Write-Host "  孤儿分支（已清理）:  $($script:orphanClean)"
Write-Host "  孤儿分支（已跳过）:  $($script:orphanSkip)"
Write-Host "  出错:                $($script:errorCount)"