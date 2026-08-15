# 代码审查修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 PR #328 代码审查发现的 9 个问题（7 个 PR 引入 + 2 个既有）

**Architecture:** 基于最新 origin/main（031bb67），创建独立分支 fix-328-review，逐个提交 9 项修复。每项修复独立 commit，按复杂度从简到繁排列。

**Tech Stack:** Go 1.26, Cobra CLI, internal/keypool (store)

## Global Constraints

- 每项修复一个独立 commit（AGENTS.md 原子提交规范）
- 提交前执行 `make check && make test-unit`
- 新增代码附带 table-driven test（AGENTS.md）
- 错误包装格式: `fmt.Errorf("函数名: %w", err)`
- Tab 缩进（gofmt 强制）

---

### Task 1: 修复 key disable help 文本

**Files:**
- Modify: `internal/cli/key.go:553`

- [ ] **Step 1: 替换 help 文本**

改动：第 553 行，将 `permanently remove` 描述改为准确的 soft-delete 说明

```go
// 修改前:
		Use 'akswitch key remove' to permanently remove a key.

// 修改后:
		Use 'akswitch key remove' to soft-delete a key (recoverable via 'key restore').
		Use 'akswitch key purge' to permanently remove deleted keys.
```

- [ ] **Step 2: 运行测试验证**

```bash
go test -tags=unit -count=1 -short -run TestAllKeyIndexCommands_HaveByNameFlag ./internal/cli/ -v
```

- [ ] **Step 3: 提交**

```bash
git add internal/cli/key.go
git commit -m "fix: key disable 帮助文本改用软删除描述（F1）"
```

---

### Task 2: 修复 key list 示例格式

**Files:**
- Modify: `internal/cli/key.go:461`

- [ ] **Step 1: 替换示例中的方括号为圆括号**

```go
// 修改前（第 461 行）:
	    [1] sk-****yy  [disabled]

// 修改后:
	    [1] sk-****yy  (disabled)
```

- [ ] **Step 2: 运行测试验证**

```bash
go test -tags=unit -count=1 -short -run TestAllKeyIndexCommands_HaveByNameFlag ./internal/cli/ -v
```

- [ ] **Step 3: 提交**

```bash
git add internal/cli/key.go
git commit -m "fix: key list 示例格式与代码一致（F2）"
```

---

### Task 3: 修复 CSV 列名 key_name 不识别

**Files:**
- Modify: `internal/cli/key.go:1043-1046`
- Test: `internal/cli/key_import_test.go`（已有 TestParseCSV）

- [ ] **Step 1: 在 nameColNames 中添加 "key_name"**

```go
// 修改前（第 1043-1046 行）:
	nameColNames := map[string]bool{
		"name": true, "account_name": true, "username": true,
		"user": true, "account": true, "备注": true,
	}

// 修改后:
	nameColNames := map[string]bool{
		"name": true, "key_name": true, "account_name": true,
		"username": true, "user": true, "account": true, "备注": true,
	}
```

- [ ] **Step 2: 运行测试验证**

```bash
go test -tags=unit -count=1 -short -run TestParseCSV ./internal/cli/ -v
```

- [ ] **Step 3: 提交**

```bash
git add internal/cli/key.go
git commit -m "fix: CSV 解析支持 key_name 列名（F3）"
```

---

### Task 4: 修复 key restore help 文本

**Files:**
- Modify: `internal/cli/key.go:636`

- [ ] **Step 1: 替换 help 文本**

```go
// 修改前（第 636 行）:
		Long: `Restore a soft-deleted API key. The key becomes active again.

// 修改后:
		Long: `Restore a soft-deleted API key. The key is no longer deleted and
	appears in key list again. Use 'key enable' separately if it was disabled.
```

- [ ] **Step 2: 运行测试验证**

```bash
go test -tags=unit -count=1 -short -run 'TestKeyRestoreCmd|TestKeyPurgeCmd' ./internal/cli/ -v
```

- [ ] **Step 3: 提交**

```bash
git add internal/cli/key.go
git commit -m "fix: key restore 帮助文本准确描述行为（F4）"
```

---

### Task 5: 删除测试中误导性 no-op 调用

**Files:**
- Modify: `internal/cli/key_import_test.go:378`

- [ ] **Step 1: 删除 no-op 行**

```go
// 修改前（第 377-378 行）:
		// Reset the --output flag between test cases
		keyExportCmd.Flags().Set("output", "")
		keyExportCmd.Flags().Changed("output")

// 修改后:
		// Reset the --output flag between test cases
		keyExportCmd.Flags().Set("output", "")
```

- [ ] **Step 2: 运行测试验证**

```bash
go test -tags=unit -count=1 -short -run 'TestKeyExportCmd' ./internal/cli/ -v
```

- [ ] **Step 3: 提交**

```bash
git add internal/cli/key_import_test.go
git commit -m "fix: 删除测试中误导性 no-op Changed 调用（F5）"
```

---

### Task 6: key update 拒绝操作已删除 key

**Files:**
- Modify: `internal/cli/key.go:389-404`

- [ ] **Step 1: 添加 Deleted 检查**

在 `keyUpdateCmd.RunE` 中，resolve 索引后、修改前检查 `entry.Deleted`：

```go
// 在索引范围检查后、处理可选 key 值前添加:

	if store.Keys[idx].Deleted {
		return fmt.Errorf("key [%d] is deleted, use 'key restore' to recover it first", idx)
	}
```

- [ ] **Step 2: 添加 table-driven test 覆盖已删除 key 拒绝行为**

- [ ] **Step 3: 运行测试验证**

```bash
go test -tags=unit -count=1 -short -run TestKeyUpdateCmd_Behavior ./internal/cli/ -v
```

- [ ] **Step 4: 提交**

```bash
git add internal/cli/key.go internal/cli/key_import_test.go
git commit -m "fix: key update 拒绝操作已删除 key（F6）"
```

---

### Task 7: updateKey 拒绝操作已删除 key（enable/disable）

**Files:**
- Modify: `internal/cli/key.go:31-72`

- [ ] **Step 1: 在 updateKey 中添加 Deleted 检查**

在 `updateKey` 函数的 switch 处理前，检查 `entry.Deleted`：

```go
// 在 switch op 前添加:

	if entry.Deleted {
		return fmt.Errorf("key [%d] is deleted, use 'key restore' to recover it first", idx)
	}
```

- [ ] **Step 2: 添加 table-driven test 覆盖 enable/disable 已删除 key 的拒绝行为**

- [ ] **Step 3: 运行测试验证**

```bash
go test -tags=unit -count=1 -short -run 'TestKeyRestoreCmd|TestKeyPurgeCmd' ./internal/cli/ -v
```

- [ ] **Step 4: 提交**

```bash
git add internal/cli/key.go internal/cli/key_import_test.go
git commit -m "fix: key enable/disable 拒绝操作已删除 key（F7）"
```

---

### Task 8: dedupEntries 排除已删除 key

**Files:**
- Modify: `internal/cli/key.go:950-966`

- [ ] **Step 1: 在构建 existing map 时跳过 Deleted**

```go
// 修改前（第 951-954 行）:
	existing := make(map[string]bool, len(store.Keys))
	for _, e := range store.Keys {
		existing[e.Key] = true
	}

// 修改后:
	existing := make(map[string]bool)
	for _, e := range store.Keys {
		if e.Deleted {
			continue
		}
		existing[e.Key] = true
	}
```

- [ ] **Step 2: 添加或更新 table-driven test 覆盖已删除 key 不被 dedup 参与**

- [ ] **Step 3: 运行测试验证**

```bash
go test -tags=unit -count=1 -short -run 'TestDedupEntries' ./internal/cli/ -v
```

- [ ] **Step 4: 提交**

```bash
git add internal/cli/key.go internal/cli/key_import_test.go
git commit -m "fix: dedupEntries 排除已删除 key（F8）"
```

---

### Task 9: cooldown 索引映射（store→pool）

**Files:**
- Modify: `internal/cli/key.go:433-447`

- [ ] **Step 1: 在 keyCooldownCmd.RunE 中添加 store→pool 索引映射**

```go
// 修改前（第 433-447 行）:
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			idx, err := resolveKeyIndex(cmd, args)
			if err != nil {
				return err
			}

			// Try runtime API
			runtimeErr := callKeyRuntimeAPI(provider, idx, "cooldown")
			if runtimeErr == nil {
				return nil
			}

			return fmt.Errorf("server not running — start akswitch to use runtime cooldown: %w", runtimeErr)
		},

// 修改后:
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			idx, err := resolveKeyIndex(cmd, args)
			if err != nil {
				return err
			}

			// Map store index to pool index (keysFromStore skips Deleted entries)
			store, loadErr := keypool.LoadKeys(provider)
			if loadErr != nil {
				return fmt.Errorf("failed to load keys for %q: %w", provider, loadErr)
			}
			if idx < 0 || idx >= len(store.Keys) {
				return fmt.Errorf("index %d out of range: provider %q has %d keys (valid: 0-%d)",
					idx, provider, len(store.Keys), len(store.Keys)-1)
			}
			if store.Keys[idx].Deleted {
				return fmt.Errorf("key [%d] is deleted, use 'key restore' to recover it first", idx)
			}
			poolIdx := 0
			for i := 0; i < idx; i++ {
				if !store.Keys[i].Deleted {
					poolIdx++
				}
			}
			// Server parseKeyIndex expects 1-based URL index, converts to 0-based internally
			runtimeErr := callKeyRuntimeAPI(provider, poolIdx+1, "cooldown")
			if runtimeErr == nil {
				return nil
			}

			return fmt.Errorf("server not running — start akswitch to use runtime cooldown: %w", runtimeErr)
		},
```

- [ ] **Step 2: 添加 table-driven test 覆盖 cooldown 索引映射**

- [ ] **Step 3: 运行测试验证**

```bash
go test -tags=unit -count=1 -short -run 'TestKeyListCmd_ShowAll|TestKeyUpdateCmd_Behavior' ./internal/cli/ -v
go test -tags=unit -count=1 -short ./internal/cli/
```

- [ ] **Step 4: 提交**

```bash
git add internal/cli/key.go internal/cli/key_import_test.go
git commit -m "fix: key cooldown 索引映射 store→pool（F9）"
```

---

### Task 10: 最终验证

- [ ] **Step 1: 全量验证**

```bash
make check && make test-unit
```

- [ ] **Step 2: 推送到远程**

```bash
git push --force-with-lease origin fix-328-review
```