# Candidate 5 — 删除死代码

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** 删除 `internal/server/` 中无调用者的死代码

**Architecture:** 两个零调用者的函数分别删除，零行为变更

**Tech Stack:** Go

## Global Constraints

- 删除前必须确认零调用者（grep + 手动验证）
- 删除后确保 `go build ./...` 和 `go test -tags=unit` 通过
- 不删除 `respondJSON`（helpers.go:11-15）— 有 30+ 调用点
- 删除后检查是否产生孤立 import

---

### Task 1: 删除 headers.go（整文件）

**Files:**
- Delete: `internal/server/headers.go`

**Step 1: 确认零调用者**

Run: `grep -r "copyHeaders" internal/server/`
Expected: 零结果（仅文件自身定义）

**Step 2: 删除文件**

```bash
git rm internal/server/headers.go
```

**Step 3: 验证编译和测试**

Run: `go build ./... && go test -tags=unit -count=1 ./internal/server/`
Expected: PASS

**Step 4: 提交**

```bash
git commit -m "refactor: remove unused copyHeaders function"
```

---

### Task 2: 删除 helpers.go 中的 parseKeyIndex

**Files:**
- Modify: `internal/server/helpers.go`

**Step 1: 确认零调用者**

Run: `grep -r "parseKeyIndex" internal/server/`
Expected: 零结果（仅文件自身定义）

**Step 2: 删除函数**

删除 `helpers.go` 第 17-30 行（`parseKeyIndex` 函数）。

**Step 3: 检查孤立 import**

删除后 `helpers.go` 仅使用 `encoding/json`，确认 `strconv` 和 `fmt` 已不再使用，需要删除这两个 import。

**Step 4: 验证编译和测试**

Run: `go build ./... && go test -tags=unit -count=1 ./internal/server/`
Expected: PASS

**Step 5: 提交**

```bash
git add internal/server/helpers.go
git commit -m "refactor: remove unused parseKeyIndex function"
```

---

### Task 3: 验证整体

Run: `make check && make test-unit`
Expected: All pass
