#!/bin/sh
# session-start.sh — 每次 Claude 启动时同步主目录到最新
#
# 原理：作为 SessionStart hook 运行，git pull --ff-only 确保主目录
# 的 main 分支与远程同步。所有输出（成功/失败）都走 stdout，
# 让 Claude 能感知执行结果。
#
# 安全：--ff-only 保证只进行快进合并，有冲突时直接失败，不破坏本地。

output=$(git pull --ff-only origin main 2>&1)
echo "$output"
exit 0