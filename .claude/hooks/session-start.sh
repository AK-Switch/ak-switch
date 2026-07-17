#!/bin/sh
# session-start.sh — 每次 Claude 启动时同步主目录到最新
#
# 流程：
# 1. 先切到 main 分支（不管之前被留在什么分支上）
# 2. 再拉取最新代码
#
# 安全：--ff-only 保证只进行快进合并，有冲突时直接失败。
# 所有输出走 stdout，让 Claude 能感知执行结果。

# 先切到 main，再拉取（&& 确保切成功后才拉取）
output=$(git checkout main 2>&1 && git pull --ff-only origin main 2>&1)
echo "$output"
exit 0