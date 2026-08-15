# Key Selection CLI 修复 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 key_selection CLI 交互层的 4 个遗漏，让用户能通过命令行配置和查看 key 选择策略。

**Architecture:** 4 个独立修复，全部为现有功能的补全。不改变语义，不涉及重构。修改范围仅限 2 个文件。

**Tech Stack:** Go 1.26, Cobra CLI

## Global Constraints

- 不修改 `RuntimeEditable`、`ReadOnly` 等描述符属性
- 不改变现有语义，补遗漏即可
- 提交前必须通过 `make check` 和 `make test-unit`

---

### Task 1: Reload 时重新应用 SelectionMode

**Files:**
- Modify: `internal/server/provider_manager.go:118-119`
- Test: `go test -tags=unit -short -run TestReload ./internal/server/`

**Interfaces:**
- Consumes: `keypool.KeyPool.SetSelectionMode(keypool.KeySelectionMode)` — 已有方法
- Produces: reload 后新建的 pool 继承 TOML 配置的 key_selection 模式

- [ ] **Step 1: 确认修复位置**

`provider_manager.go:ReloadConfig()` 第 119 行新建 pool 后，紧跟 `ConfigurePoolCBs` 之前。需要在这行之后补 `SetSelectionMode` 调用。

- [ ] **Step 2: 实现修复**

在 `provider_manager.go:119` 后补：
```go
existing.pool.SetSelectionMode(keypool.KeySelectionMode(cfg.KeySelection))
```

使 ReloadConfig 中新建 pool 后应用配置的 key 选择策略。

- [ ] **Step 3: 运行测试验证**

```bash
go test -tags=unit -count=1 -short ./internal/server/
```
Expected: 全部通过

- [ ] **Step 4: 提交**

```bash
git add internal/server/provider_manager.go
git commit -m "fix: ReloadConfig 新建 pool 后补 SetSelectionMode 调用"
```

---

### Task 2: getFieldValue 补 key_selection case

**Files:**
- Modify: `internal/cli/config.go:596-598`

**Interfaces:**
- Consumes: `config.Config.KeySelection` 字段
- Produces: `config get key_selection <provider>` 显示实际值而非默认值

- [ ] **Step 1: 实现修复**

在 `getFieldValue` 函数的 switch 中，`case "keys_file"` 之后补：
```go
case "key_selection":
	return p.KeySelection, nil
```

- [ ] **Step 2: 运行测试验证**

```bash
go test -tags=unit -count=1 -short ./internal/cli/
```
Expected: 全部通过

- [ ] **Step 3: 提交**

```bash
git add internal/cli/config.go
git commit -m "fix: getFieldValue 补 key_selection case"
```

---

### Task 3: config view 显示 key_selection

**Files:**
- Modify: `internal/cli/config.go:173-174`

- [ ] **Step 1: 实现修复**

在 `configViewCmd` 的 `Keys file` 输出行之后补：
```go
fmt.Printf("  Key selection mode: %s\n", sanitized.KeySelection)
```

- [ ] **Step 2: 运行测试验证**

```bash
go test -tags=unit -count=1 -short ./internal/cli/
```
Expected: 全部通过

- [ ] **Step 3: 提交**

```bash
git add internal/cli/config.go
git commit -m "fix: config view 显示 key_selection 值"
```

---

### Task 4: 帮助文本加 key_selection

**Files:**
- Modify: `internal/cli/config.go:269-271` 和 `353-354`

- [ ] **Step 1: 确认两处帮助文本的位置**

`configGetCmd.Long` 第 269-271 行的 Valid keys 列表，以及 `configSetCmd.Long` 第 353-354 行的 Valid keys 列表。

- [ ] **Step 2: 实现修复**

两处帮助文本的 Valid keys 列表中，`keys_file` 之后各加 `key_selection`。

- [ ] **Step 3: 运行测试验证**

```bash
go test -tags=unit -count=1 -short ./internal/cli/
```
Expected: 全部通过

- [ ] **Step 4: 提交**

```bash
git add internal/cli/config.go
git commit -m "chore: 帮助文本补 key_selection"
```

---

### Task 5: 全量验证

**Files:** 无修改

- [ ] **Step 1: 运行 make check**

```bash
make check
```
Expected: lint + vet + fmt 全部通过

- [ ] **Step 2: 运行全量单元测试**

```bash
make test-unit
```
Expected: 全部通过

- [ ] **Step 3: 提交最终清理**

```bash
git add -A
git commit -m "chore: 全量验证通过"
```

- [ ] **Step 4: 推送到远程**

```bash
git push origin worktree-fix+key-selection-cli-gaps
```