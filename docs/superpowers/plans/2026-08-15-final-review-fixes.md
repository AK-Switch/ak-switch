# Final Whole-Branch Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复全分支审查发现的 9 个问题，确保 branch 可安全合并到 main

**Architecture:** 直接在现有 branch 上修复，每个修复点独立原子提交。修复集中在 `internal/cli/config.go`、`internal/cli/key.go`、`internal/cli/key_import_test.go`、`test/integration/cli_integration_test.go` 四个文件。

**Tech Stack:** Go 1.26 + Cobra + table-driven testing

## Global Constraints

- 所有测试必须使用 table-driven pattern
- `google/go-cmp` 不在项目依赖中，测试断言用手动比较
- 项目强制 Tab 缩进，提交前 `make fmt`
- 提交前执行 `make vet` 和 `go test -tags=unit -count=1 -short ./...`
- 集成测试 `TestKeyRemove_RemovesKey` 修复只能在 Docker 环境下验证

---

### Task 1: 修复 `getFieldValue` 缺失 `thinking_mode`/`rectify_thinking_map_to` case

**Files:**
- Modify: `internal/cli/config.go:546-585`

**Interfaces:**
- Consumes: `config.ConfigFieldDescriptors` 中的 `thinking_mode` 和 `rectify_thinking_map_to` 定义
- Produces: `getFieldValue` 函数支持这两个字段的正确读取

- [ ] **Step 1: 添加 `thinking_mode` 和 `rectify_thinking_map_to` case**

在 `getFieldValue` 的 switch 中，`genai_model` case 之前添加：

```go
case "thinking_mode":
    return p.ThinkingMode, nil
case "rectify_thinking_map_to":
    return p.RectifyThinkingMapTo, nil
```

注意：放在 `keys_file` 和 `genai_model` 之间，保持字母序。

- [ ] **Step 2: 运行验证**

```bash
go build ./internal/cli/ && go vet ./internal/cli/
```

- [ ] **Step 3: 提交**

```bash
git add internal/cli/config.go
git commit -m "fix: config view 显示 thinking_mode/rectify_thinking_map_to 实际值（F2）"
```

---

### Task 2: 清理 `KeyRemove` 死代码

**Files:**
- Modify: `internal/cli/key.go:24-28, 58, 71`

**Interfaces:**
- Consumes: `KeyMutation` 枚举类型，`updateKey` 函数
- Produces: 清理后的 `KeyMutation`（仅 `KeyEnable`/`KeyDisable`），`updateKey` 中无 `KeyRemove` 分支

- [ ] **Step 1: 删除 `KeyRemove` 常量**

```go
// 修改前
const (
    KeyEnable KeyMutation = iota
    KeyDisable
    KeyRemove
)

// 修改后
const (
    KeyEnable KeyMutation = iota
    KeyDisable
)
```

- [ ] **Step 2: 删除 `updateKey` 中 `KeyRemove` 的修改 case**

```go
// 删除这段
case KeyRemove:
    store.Keys = append(store.Keys[:idx], store.Keys[idx+1:]...)
```

- [ ] **Step 3: 删除 `updateKey` 中 `KeyRemove` 的输出 case**

```go
// 删除这段
case KeyRemove:
    fmt.Printf("Removed key [%d] %s from provider %q (remaining: %d keys)\n", idx, desc, provider, len(store.Keys))
```

- [ ] **Step 4: 运行验证**

```bash
go build ./internal/cli/ && go vet ./internal/cli/
```

- [ ] **Step 5: 提交**

```bash
git add internal/cli/key.go
git commit -m "refactor: 清理 KeyRemove 死代码（F3）"
```

---

### Task 3: 修复 `maskSensitiveValue` 放过 `keys_file`

**Files:**
- Modify: `internal/cli/config.go:528-541`

- [ ] **Step 1: 修改 `maskSensitiveValue` 函数**

将 `keys_file` 的遮盖逻辑移除，改为与普通字段一样显示实际值：

```go
func maskSensitiveValue(fd *config.ConfigFieldDescriptor, val any) string {
    if fd.Key == "admin_token" {
        if s, ok := val.(string); ok && s != "" {
            return "(set)"
        }
        return "(not set)"
    }
    return fd.Format(val)
}
```

即删除 `keys_file` 的特殊处理，仅保留 `admin_token` 遮盖。

- [ ] **Step 2: 运行验证**

```bash
go build ./internal/cli/ && go vet ./internal/cli/
```

- [ ] **Step 3: 提交**

```bash
git add internal/cli/config.go
git commit -m "fix: maskSensitiveValue 恢复 keys_file 路径显示（F7）"
```

---

### Task 4: 更新帮助文本

**Files:**
- Modify: `internal/cli/key.go`（`keyImportCmd.Long` 和 `keyCmd.Long`）

- [ ] **Step 1: 在 `keyImportCmd.Long` 追加 CSV 格式说明**

在 `keyImportCmd.Long` 的末尾（JSONL 示例之后），追加：

```
CSV format:
  The --file flag also accepts CSV files. The first row may be a header
  with columns "key_name" and "api_key" (or "api_key_account"). Without
  a header, the file must have one column (API key only) or two columns
  (key name, API key).

  Examples:
    akswitch key import nvidia --file keys.csv
    akswitch key import nvidia --file keys.csv --name "batch"
```

- [ ] **Step 2: 更新 `keyCmd.Long` 追加 export/restore/purge**

在 `keyCmd.Long` 的命令列表中追加 `export`、`restore` 和 `purge`。

- [ ] **Step 3: 运行验证**

```bash
go build ./internal/cli/ && go vet ./internal/cli/
```

- [ ] **Step 4: 提交**

```bash
git add internal/cli/key.go
git commit -m "docs: 更新 key import/help 文本（F8/F9）"
```

---

### Task 5: 补充 `key export` 成功路径测试

**Files:**
- Modify: `internal/cli/key_import_test.go`

**Test structure:**
- 在 `TestKeyExportCmd` 中追加 2 个新用例：
  1. "successful export": 使用 `tmpKeyStore` 创建 provider，添加 key，调用 `key export` 到 `--output` 临时文件，断言文件存在且内容可解析为 JSON 并包含 key
  2. "successful export to stdout": 验证 stdout 输出包含 key 信息

- [ ] **Step 1: 添加成功路径测试用例**

```go
{
    name:     "successful export",
    provider: "testprov",
    args:     []string{"testprov", "--output", filepath.Join(tmpDir, "exported.json")},
    wantErr:  false,
    check: func(t *testing.T, output string) {
        data, err := os.ReadFile(filepath.Join(tmpDir, "exported.json"))
        if err != nil {
            t.Fatalf("failed to read exported file: %v", err)
        }
        var exported keypool.KeyStore
        if err := json.Unmarshal(data, &exported); err != nil {
            t.Fatalf("exported file is not valid JSON: %v", err)
        }
        if len(exported.Keys) == 0 {
            t.Error("exported file contains no keys")
        }
    },
},
```

注意：`TestKeyExportCmd` 使用 `runKeyCommand` 和 `tmpKeyStore`，需要确保 `tmpKeyStore` 中已有 key 数据。检查现有测试框架——`TestKeyExportCmd` 的 `tmpKeyStore` 在 `TestMain` 或 `setup` 中初始化。若需要，在 test function 内自行创建 provider + key 后再导出。

- [ ] **Step 2: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestKeyExportCmd ./internal/cli/ -v
```

- [ ] **Step 3: 提交**

```bash
git add internal/cli/key_import_test.go
git commit -m "test: 补充 key export 成功路径测试（F4）"
```

---

### Task 6: 补充 `key update` 行为测试

**Files:**
- Modify: `internal/cli/key_import_test.go` 或 `internal/cli/provider_cmd_test.go`

**Test: `TestKeyUpdateCmd_Behavior`**

Table-driven test 覆盖：
1. "update name only" — args=2 + `--name newname`，验证 key 值不变，名称更新
2. "update key value" — args=3 `newkeyvalue`，验证 key 值更新
3. "update both" — args=3 `newkey` + `--name newname`，验证两者都更新
4. "nothing to update" — args=2 无 `--name`，验证报错 "nothing to update"
5. "update by name" — `--by-name existingname` + args=3 `newkey`，验证按名称定位

- [ ] **Step 1: 编写测试函数**

按照 `TestKeyRestoreCmd` 的模式，使用 `tmpKeyStore` 和 `runKeyCommand`：

```go
func TestKeyUpdateCmd_Behavior(t *testing.T) {
    tests := []struct {
        name     string
        provider string
        args     []string
        wantErr  bool
        check    func(t *testing.T, output string)
    }{
        {
            name:     "update name only",
            provider: "testprov",
            args:     []string{"testprov", "0", "--name", "renamed-key"},
            wantErr:  false,
            check: func(t *testing.T, output string) {
                if !strings.Contains(output, "renamed-key") {
                    t.Errorf("output missing new name:\n%s", output)
                }
            },
        },
        // ... 更多用例
    }
    // ...
}
```

- [ ] **Step 2: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestKeyUpdateCmd_Behavior ./internal/cli/ -v
```

- [ ] **Step 3: 提交**

```bash
git add internal/cli/key_import_test.go
git commit -m "test: 补充 key update 行为测试（F5）"
```

---

### Task 7: 补充 `key list --all` 过滤测试

**Files:**
- Modify: `internal/cli/key_import_test.go`

**Test: `TestKeyListCmd_ShowAll`**

在现有测试文件中追加测试，覆盖：
1. "list hides deleted by default" — 软删除一个 key 后，`key list` 默认不显示
2. "list --all shows deleted" — `key list --all` 显示所有 key（含 deleted）
3. "list --all marks deleted status" — 输出中标注 "deleted" 状态

- [ ] **Step 1: 编写测试函数**

使用 `tmpKeyStore` 和 `runKeyCommand`，在 `key add` 后 `key remove`，再 `key list`：

```go
func TestKeyListCmd_ShowAll(t *testing.T) {
    // 先添加两个 key
    runKeyCommand(t, "testprov", "add", "testprov", "sk-key-1")
    runKeyCommand(t, "testprov", "add", "testprov", "sk-key-2")
    // 软删除第一个
    runKeyCommand(t, "testprov", "remove", "testprov", "0")
    
    // 默认 list 只显示 1 个
    output := runKeyCommand(t, "testprov", "list", "testprov")
    if strings.Contains(output, "sk-key-1") {
        t.Error("deleted key should not appear in default list")
    }
    
    // --all 显示 2 个
    output = runKeyCommand(t, "testprov", "list", "testprov", "--all")
    if !strings.Contains(output, "deleted") {
        t.Error("--all list should mark deleted status")
    }
}
```

- [ ] **Step 2: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestKeyListCmd_ShowAll ./internal/cli/ -v
```

- [ ] **Step 3: 提交**

```bash
git add internal/cli/key_import_test.go
git commit -m "test: 补充 key list --all 过滤测试（F6）"
```

---

### Task 8: 修复集成测试适配软删除语义

**Files:**
- Modify: `test/integration/cli_integration_test.go:588-621`

- [ ] **Step 1: 修改 `TestKeyRemove_RemovesKey` 断言**

```go
// 修改前: 断言 len(store.Keys) == 1, 第一个元素是 "sk-remove-key-2"
// 修改后: 断言 len(store.Keys) == 2, Keys[0].Deleted == true, Keys[1].Key == "sk-remove-key-2"

if len(store.Keys) != 2 {
    t.Fatalf("expected 2 keys after soft-delete, got %d", len(store.Keys))
}
if !store.Keys[0].Deleted {
    t.Errorf("key[0] should be marked as deleted")
}
if store.Keys[1].Key != "sk-remove-key-2" {
    t.Errorf("remaining key[1] = %q, want %q", store.Keys[1].Key, "sk-remove-key-2")
}
```

- [ ] **Step 2: 运行验证**

```bash
go vet ./test/integration/
go build -tags=integration ./test/integration/  # 确保编译通过
```

注意：集成测试需要 Docker 环境，本地只验证编译。

- [ ] **Step 3: 提交**

```bash
git add test/integration/cli_integration_test.go
git commit -m "test: 集成测试适配软删除语义（F1）"
```

---

### Task 9: gofmt 格式化 + 最终验证

- [ ] **Step 1: gofmt 格式化**

```bash
make fmt
```

- [ ] **Step 2: 检查格式化产生的变更（如有）并提交**

```bash
git diff --stat
# 如果有变更：
git add -u
git commit -m "chore: gofmt 格式化"
```

- [ ] **Step 3: 最终验证**

```bash
make vet
go test -tags=unit -count=1 -short ./...
```

- [ ] **Step 4: 推送至远程**

```bash
git push origin worktree-spec-independent-issues-batch1
```

---

## Summary

| Task | 改动文件 | 类型 | 验证 |
|------|---------|------|------|
| 1 | `config.go` | 修复 | `go vet` + build |
| 2 | `key.go` | 重构 | `go vet` + build |
| 3 | `config.go` | 修复 | `go vet` + build |
| 4 | `key.go` | 文档 | `go vet` + build |
| 5 | `key_import_test.go` | 测试 | 单测 |
| 6 | `key_import_test.go` | 测试 | 单测 |
| 7 | `key_import_test.go` | 测试 | 单测 |
| 8 | `cli_integration_test.go` | 测试 | 编译 |
| 9 | (gofmt) | 杂务 | 全量单测 |