# Independent Issues Batch 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 6 个独立 CLI issues（#286, #282, #281, #314, #307, #306），不触碰 #304/#316 正在修改的文件

**Architecture:** 6 个独立 task，每个改一个子命令或数据结构，互不依赖。顺序：先改命令结构（#307），再增加新命令（#286, #282, #281），再改底层存储（#306），最后改输出格式（#314）

**Tech Stack:** Go 1.26, Cobra CLI, keypool 存储

## Global Constraints

- Tab 缩进（项目强制，gofmt 已配置）
- 遵循项目 CLAUDE.md 中的 ProviderState 封装模式（已确认，本次不涉及 ProviderState）
- 提交前必须执行 `make check && make test-unit`
- 新增代码附带 table-driven test
- 不触碰 `internal/config/field_descriptor.go`、`internal/server/admin_api.go`、`internal/server/proxy_executor.go`、`internal/tokenestimator/*`

---

### Task 1: #307 — rename/update 命令合并

**Files:**
- Modify: `internal/cli/key.go:93-94` (init 注册), `internal/cli/key.go:101-106` (flags 注册), `internal/cli/key.go:302-418` (updateCmd + renameCmd 定义)
- Test: `internal/cli/key_import_test.go` (若有 rename 测试则迁移)

**Interfaces:**
- Consumes: `keypool.LoadKeys(provider)`, `keypool.SaveKeys(provider, store)`, `findKeyIndexByName(store, name)`, `triggerReload()`
- Produces: 修改后的 `keyUpdateCmd`（支持 `RangeArgs(2,3)`），移除 `keyRenameCmd` 和 `keyRenameCmd` 注册

- [ ] **Step 1: 修改 update 命令的 Args 校验**

将 `keyUpdateCmd` 的 `Args: cobra.ExactArgs(3)` 改为 `Args: cobra.RangeArgs(2,3)`：

```go
var keyUpdateCmd = &cobra.Command{
    Use:   "update <provider> <index> [key]",
    Short: "Update an API key at the specified index",
    Long: `Replace an existing API key at the specified index with a new key value.
    ...
    Only --name without [key] renames the key without changing its value.
    Examples:
      akswitch key update sensenova 0 sk-xxxxxxxx
      akswitch key update sensenova 0 --name d1-2
      akswitch key update sensenova d1-2 sk-xxxxxxxx --by-name`,
    Args: cobra.RangeArgs(2, 3),
    ...
}
```

- [ ] **Step 2: 修改 update 命令的 RunE 逻辑**

在 `keyUpdateCmd.RunE` 中，根据 `len(args)` 分支处理：

```go
RunE: func(cmd *cobra.Command, args []string) error {
    provider := args[0]
    store, err := keypool.LoadKeys(provider)
    // ... (existing load/find logic unchanged)

    // Handle optional key value
    if len(args) == 3 {
        newKey := args[2]
        oldMasked := logentry.MaskKey(store.Keys[idx].Key)
        store.Keys[idx].Key = newKey
        fmt.Printf("Updated key [%d] %s -> %s for provider %q\n",
            idx, oldMasked, logentry.MaskKey(newKey), provider)
    }

    // Handle --name (always works, with or without key)
    if cmd.Flags().Changed("name") {
        newName, _ := cmd.Flags().GetString("name")
        oldName := store.Keys[idx].Name
        store.Keys[idx].Name = newName
        fmt.Printf("Renamed key [%d] from %q to %q for provider %q\n",
            idx, oldName, newName, provider)
    }

    // No changes at all? Error
    if len(args) == 2 && !cmd.Flags().Changed("name") {
        return fmt.Errorf("nothing to update: provide a new key value or --name")
    }

    // ... (existing save + reload logic)
}
```

- [ ] **Step 3: 移除 rename 命令注册和定义**

在 `init()` 中：
```go
// 删除这一行:
// keyCmd.AddCommand(keyRenameCmd)
```

删除 `keyRenameCmd.Flags()` 注册行：
```go
// 删除这一行:
// addKeyIndexFlags(keyRenameCmd)
```

删除整个 `keyRenameCmd` 变量定义（`key.go:364-418`）。

- [ ] **Step 4: 更新 keyCmd 的 Long 描述**

将 `keyCmd.Long` 中的 "rename" 相关描述移到 `update` 的 `Long` 中。

- [ ] **Step 5: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestKey ./internal/cli/
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/key.go
git commit -m "refactor: 合并 rename 命令到 update（#307）"
```

---

### Task 2: #286 — key export 子命令

**Files:**
- Modify: `internal/cli/key.go:86-113` (init 注册), `key.go` 末尾新增 exportCmd 定义
- Test: `internal/cli/key_import_test.go` 追加 export 测试

**Interfaces:**
- Consumes: `keypool.LoadKeys(provider)`, `keypool.KeyStore`
- Produces: `keyExportCmd` 子命令注册到 `keyCmd`

- [ ] **Step 1: 在 init() 中注册 export 命令**

在 `keyCmd.AddCommand(keyUpstreamCBResetCmd)` 之前添加：

```go
keyCmd.AddCommand(keyExportCmd)
keyExportCmd.Flags().StringP("output", "o", "", "Write to file instead of stdout")
```

- [ ] **Step 2: 定义 keyExportCmd**

```go
var keyExportCmd = &cobra.Command{
    Use:   "export <provider>",
    Short: "Export API keys to stdout or a file",
    Long: `Export all API keys for a provider as JSON.

    By default, prints to stdout. Use --output to write to a file.
    Keys are decrypted automatically (supports encrypted storage).

    Examples:
      akswitch key export nvidia
      akswitch key export nvidia --output keys.json`,
    Args: cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        provider := args[0]
        outputPath, _ := cmd.Flags().GetString("output")

        store, err := keypool.LoadKeys(provider)
        if err != nil {
            return fmt.Errorf("failed to load keys for %q: %w", provider, err)
        }
        if store == nil || len(store.Keys) == 0 {
            return fmt.Errorf("no keys found for provider %q", provider)
        }

        data, err := json.MarshalIndent(store, "", "  ")
        if err != nil {
            return fmt.Errorf("failed to serialize keys: %w", err)
        }

        if outputPath != "" {
            // Check if file exists
            if _, err := os.Stat(outputPath); err == nil {
                fmt.Fprintf(os.Stderr, "WARNING: %s already exists, overwriting\n", outputPath)
            }
            if err := os.WriteFile(outputPath, data, 0600); err != nil {
                return fmt.Errorf("failed to write %q: %w", outputPath, err)
            }
            fmt.Printf("Exported %d keys for provider %q to %s\n", len(store.Keys), provider, outputPath)
        } else {
            fmt.Println(string(data))
        }
        return nil
    },
}
```

- [ ] **Step 3: 编写 export 测试（追加到 key_import_test.go）**

```go
func TestKeyExportCmd(t *testing.T) {
    tests := []struct {
        name    string
        args    []string
        wantErr bool
    }{
        {name: "missing provider", args: []string{}, wantErr: true},
        {name: "empty provider", args: []string{"nonexistent"}, wantErr: true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cmd := keyExportCmd
            cmd.SetArgs(tt.args)
            err := cmd.Execute()
            if (err != nil) != tt.wantErr {
                t.Errorf("export error = %v, wantErr = %v", err, tt.wantErr)
            }
        })
    }
}
```

- [ ] **Step 4: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestKeyExportCmd ./internal/cli/
```

- [ ] **Step 5: Commit**

```bash
git add internal/cli/key.go
git commit -m "feat: 添加 key export 子命令（#286）
```

---

### Task 3: #282 — key import --create flag

**Files:**
- Modify: `internal/cli/key.go:196-300` (keyImportCmd.RunE)
- Modify: `internal/cli/key.go:97-99` (init 中 flags 注册)

**Interfaces:**
- Consumes: `config.LoadTomlConfig(path)`, `config.XDGConfigPath()`, `config.TomlConfig`, `keypool.LoadKeys`, `keypool.SaveKeys`
- Produces: `--create` 和 `--target` flag 注册 + 自动创建逻辑

- [ ] **Step 1: 注册 --create 和 --target flag**

在 `init()` 的 `keyImportCmd` flag 注册区域追加：

```go
keyImportCmd.Flags().StringP("target", "t", "", "Upstream target base URL (required with --create when provider is missing)")
keyImportCmd.Flags().Bool("create", false, "Auto-create the provider if it doesn't exist")
```

- [ ] **Step 2: 在 import 逻辑中插入 provider 自动创建**

在 `keyImportCmd.RunE` 中，`keypool.LoadKeys(provider)` 之前插入检查：

```go
// Before: store, err := keypool.LoadKeys(provider)
// Insert:
create, _ := cmd.Flags().GetBool("create")
target, _ := cmd.Flags().GetString("target")

if create {
    // Check if provider exists in config
    xdgPath, err := config.XDGConfigPath()
    if err != nil {
        return fmt.Errorf("failed to determine config path: %w", err)
    }
    tc, err := config.LoadTomlConfig(xdgPath)
    if err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("failed to load config: %w", err)
    }
    providerExists := false
    if tc != nil {
        _, providerExists = tc.Provider[provider]
    }
    if !providerExists {
        if target == "" {
            return fmt.Errorf("--target is required when --create creates a new provider")
        }
        // Add provider to config
        if tc == nil {
            tc = &config.TomlConfig{Provider: make(map[string]config.ProviderConfig)}
        } else if tc.Provider == nil {
            tc.Provider = make(map[string]config.ProviderConfig)
        }
        tc.Provider[provider] = config.ProviderConfig{
            TargetBase: target,
        }
        if err := config.SaveTomlConfig(xdgPath, tc); err != nil {
            return fmt.Errorf("failed to save config with new provider: %w", err)
        }
        fmt.Printf("Created provider %q with target %q\n", provider, target)
    }
}
```

- [ ] **Step 3: 编写 --create 测试**

```go
func TestKeyImportCreateFlag(t *testing.T) {
    // 测试 --create 但无 --target 时报错
    // 测试 --create --target 在 provider 不存在时自动创建
    // 测试 --create 在 provider 已存在时幂等
}
```

- [ ] **Step 4: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestKeyImport ./internal/cli/
```

- [ ] **Step 5: Commit**

```bash
git add internal/cli/key.go
git commit -m "feat: key import 支持 --create flag 自动创建 provider（#282）"
```

---

### Task 4: #281 — key import CSV 格式

**Files:**
- Modify: `internal/cli/key.go:877-908` (parseKeyEntries 加 CSV 分支)
- Modify: `internal/cli/key.go` 末尾新增 `parseCSV` 函数
- Test: `internal/cli/key_import_test.go` 追加 CSV 测试

**Interfaces:**
- Consumes: `keypool.KeyEntry`
- Produces: `parseCSV(data []byte) ([]keypool.KeyEntry, error)` 函数

- [ ] **Step 1: 实现 parseCSV 函数**

在 `key.go` 末尾添加：

```go
// parseCSV parses CSV data into KeyEntry slices.
// Parsing rules:
// 1. Lines starting with '#' are skipped (comments)
// 2. If the first non-comment line contains known column headers,
//    columns are mapped by header name (case-insensitive)
// 3. If no header is detected, positional inference is used:
//    - 1 column → key
//    - 2 columns → name, key
//    - 3+ columns → error
// 4. Leading/trailing whitespace is stripped from each cell
func parseCSV(data []byte) ([]keypool.KeyEntry, error) {
    lines := strings.Split(strings.TrimSpace(string(data)), "\n")
    if len(lines) == 0 {
        return nil, fmt.Errorf("empty CSV data")
    }

    // Known column header names (case-insensitive)
    keyColNames := map[string]bool{
        "key": true, "api_key": true, "api key": true,
        "token": true, "secret": true, "apikey": true,
    }
    nameColNames := map[string]bool{
        "name": true, "account_name": true, "username": true,
        "user": true, "account": true, "备注": true,
    }

    // Skip comment lines
    contentStart := 0
    for contentStart < len(lines) {
        trimmed := strings.TrimSpace(lines[contentStart])
        if trimmed == "" || strings.HasPrefix(trimmed, "#") {
            contentStart++
            continue
        }
        break
    }
    if contentStart >= len(lines) {
        return nil, fmt.Errorf("no data found in CSV (all lines are comments or empty)")
    }

    firstLine := strings.TrimSpace(lines[contentStart])
    cols := strings.Split(firstLine, ",")
    for i := range cols {
        cols[i] = strings.TrimSpace(cols[i])
    }

    // Detect if first line is a header
    isHeader := false
    keyCol := -1
    nameCol := -1
    for _, c := range cols {
        lower := strings.ToLower(c)
        if keyColNames[lower] {
            isHeader = true
            break
        }
        if nameColNames[lower] {
            isHeader = true
            break
        }
    }

    dataStart := contentStart
    if isHeader {
        // Map columns by header name
        for i, c := range cols {
            lower := strings.ToLower(c)
            if keyColNames[lower] {
                keyCol = i
            }
            if nameColNames[lower] {
                nameCol = i
            }
        }
        if keyCol == -1 {
            return nil, fmt.Errorf("CSV has header but no key column found (known names: key, api_key, token, secret)")
        }
        dataStart = contentStart + 1 // skip header row
    }

    // Parse data rows
    var entries []keypool.KeyEntry
    for i := dataStart; i < len(lines); i++ {
        line := strings.TrimSpace(lines[i])
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        cells := strings.Split(line, ",")
        for j := range cells {
            cells[j] = strings.TrimSpace(cells[j])
        }

        if isHeader {
            if keyCol >= len(cells) {
                continue // skip malformed rows
            }
            entry := keypool.KeyEntry{Key: cells[keyCol]}
            if nameCol >= 0 && nameCol < len(cells) {
                entry.Name = cells[nameCol]
            }
            entries = append(entries, entry)
        } else {
            // Positional inference
            switch len(cells) {
            case 1:
                entries = append(entries, keypool.KeyEntry{Key: cells[0]})
            case 2:
                entries = append(entries, keypool.KeyEntry{Name: cells[0], Key: cells[1]})
            default:
                return nil, fmt.Errorf("line %d: cannot infer CSV column mapping with %d columns (no header detected)", i+1, len(cells))
            }
        }
    }

    if len(entries) == 0 {
        return nil, fmt.Errorf("no valid key entries found in CSV data")
    }
    return entries, nil
}
```

- [ ] **Step 2: 在 parseKeyEntries 中加 CSV 分支**

在 `parseKeyEntries` 函数的 JSONL 尝试之后、最后 return 之前，插入 CSV 检测：

```go
// Try CSV (detect by .csv extension or content sniffing)
if len(data) > 0 {
    csvEntries, csvErr := parseCSV(data)
    if csvErr == nil {
        return csvEntries, nil
    }
}
```

- [ ] **Step 3: 编写 CSV 测试**

```go
func TestParseCSV(t *testing.T) {
    tests := []struct {
        name    string
        data    string
        want    []keypool.KeyEntry
        wantErr bool
    }{
        {
            name: "header key_name",
            data: "key,name\nsk-1,key1\nsk-2,key2",
            want: []keypool.KeyEntry{
                {Key: "sk-1", Name: "key1"},
                {Key: "sk-2", Name: "key2"},
            },
        },
        {
            name: "header api_key_account",
            data: "api_key,account\nsk-xxx,user1",
            want: []keypool.KeyEntry{
                {Key: "sk-xxx", Name: "user1"},
            },
        },
        {
            name: "no header 2 cols",
            data: "my-key,sk-xxx",
            want: []keypool.KeyEntry{
                {Name: "my-key", Key: "sk-xxx"},
            },
        },
        {
            name: "no header 1 col",
            data: "sk-xxx",
            want: []keypool.KeyEntry{
                {Key: "sk-xxx"},
            },
        },
        {
            name: "comment lines",
            data: "# This is a comment\n# generated by BazaarLink\nkey\nsk-xxx",
            want: []keypool.KeyEntry{
                {Key: "sk-xxx"},
            },
        },
        {
            name: "no header 3 cols error",
            data: "a,b,c",
            wantErr: true,
        },
        {
            name: "empty data",
            data: "",
            wantErr: true,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := parseCSV([]byte(tt.data))
            if (err != nil) != tt.wantErr {
                t.Errorf("parseCSV() error = %v, wantErr = %v", err, tt.wantErr)
                return
            }
            if !tt.wantErr {
                if diff := cmp.Diff(tt.want, got); diff != "" {
                    t.Errorf("parseCSV() mismatch (-want +got):\n%s", diff)
                }
            }
        })
    }
}
```

- [ ] **Step 4: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestParseCSV ./internal/cli/
```

- [ ] **Step 5: Commit**

```bash
git add internal/cli/key.go
git commit -m "feat: key import 支持 CSV 格式（#281）"
```

---

### Task 5: #306 — 误删 API key 保护（软删除）

**Files:**
- Modify: `internal/keypool/store.go:13-17` (KeyEntry 加 Deleted 字段)
- Modify: `internal/keypool/store.go:285-293` (keysFromStore 过滤 Deleted)
- Modify: `internal/cli/key.go:498-518` (keyRemoveCmd 改为标记删除)
- Modify: `internal/cli/key.go:449-496` (keyListCmd 过滤 Deleted + --all)
- Modify: `internal/cli/key.go:86-113` (init 注册 restore + purge 命令)
- Modify: `internal/cli/key.go` 末尾新增 `keyRestoreCmd` 和 `keyPurgeCmd` 定义
- Test: `internal/cli/key_import_test.go` 追加 restore/purge 测试

**Interfaces:**
- Consumes: `keypool.LoadKeys(provider)`, `keypool.SaveKeys(provider, store)`, `keypool.KeyEntry.Deleted`
- Produces: `keyRestoreCmd`, `keyPurgeCmd` 子命令，修改后的 `keyRemoveCmd` 和 `keyListCmd`

- [ ] **Step 1: KeyEntry 添加 Deleted 字段**

```go
type KeyEntry struct {
    Key      string `json:"key"`
    Name     string `json:"name,omitempty"`
    Disabled bool   `json:"disabled,omitempty"`
    Deleted  bool   `json:"deleted,omitempty"` // 新增
}
```

- [ ] **Step 2: keysFromStore 过滤已删除 key**

```go
func keysFromStore(store *KeyStore) (keys, names []string) {
    for _, entry := range store.Keys {
        if entry.Deleted {
            continue // 已删除的 key 不加载到路由池
        }
        keys = append(keys, entry.Key)
        names = append(names, entry.Name)
    }
    return keys, names
}
```

- [ ] **Step 3: 修改 keyRemoveCmd 为软删除**

```go
var keyRemoveCmd = &cobra.Command{
    Use:   "remove <provider> <index>",
    Short: "Remove (soft delete) an API key by index or name",
    Long: `Mark an API key as deleted at the specified index or matching name.
    ...
    Deleted keys are hidden from 'key list' but can be restored with 'key restore'.
    Use 'key purge' to permanently delete all marked keys.`,
    ...
    RunE: func(cmd *cobra.Command, args []string) error {
        idx, err := resolveKeyIndex(cmd, args)
        if err != nil {
            return err
        }
        // 改为软删除而非硬删除
        store, err := keypool.LoadKeys(args[0])
        if err != nil {
            return fmt.Errorf("failed to load keys for %q: %w", args[0], err)
        }
        if store == nil || idx >= len(store.Keys) {
            return fmt.Errorf("index %d out of range", idx)
        }
        store.Keys[idx].Deleted = true
        if err := keypool.SaveKeys(args[0], store); err != nil {
            return fmt.Errorf("failed to save keys: %w", err)
        }
        desc := logentry.MaskKey(store.Keys[idx].Key)
        if store.Keys[idx].Name != "" {
            desc += fmt.Sprintf(" (name: %s)", store.Keys[idx].Name)
        }
        fmt.Printf("Deleted key [%d] %s for provider %q (use 'key restore' to undo)\n", idx, desc, args[0])
        triggerReload()
        return nil
    },
}
```

- [ ] **Step 4: 修改 keyListCmd 隐藏已删除 key**

在 `keyListCmd.RunE` 中，添加 `--all` flag 判断：

```go
// 在 provider 循环中:
showAll, _ := cmd.Flags().GetBool("all")
for i, entry := range store.Keys {
    if entry.Deleted && !showAll {
        continue
    }
    status := "active"
    if entry.Disabled {
        status = "disabled"
    }
    if entry.Deleted {
        status = "deleted"
    }
    // ... existing format code
}
```

注册 `--all` flag：
```go
keyListCmd.Flags().Bool("all", false, "Show all keys including deleted ones")
```

- [ ] **Step 5: 定义 keyRestoreCmd**

```go
var keyRestoreCmd = &cobra.Command{
    Use:   "restore <provider> <index>",
    Short: "Restore a previously deleted API key",
    Long: `Restore a soft-deleted API key. The key becomes active again.
    Use --by-name to look up a key by its display name.
    Use 'key list --all' to see deleted keys.`,
    Args: cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        idx, err := resolveKeyIndex(cmd, args)
        if err != nil {
            return err
        }
        store, err := keypool.LoadKeys(args[0])
        if err != nil {
            return fmt.Errorf("failed to load keys for %q: %w", args[0], err)
        }
        if store == nil || idx >= len(store.Keys) {
            return fmt.Errorf("index %d out of range", idx)
        }
        if !store.Keys[idx].Deleted {
            return fmt.Errorf("key [%d] is not deleted", idx)
        }
        store.Keys[idx].Deleted = false
        if err := keypool.SaveKeys(args[0], store); err != nil {
            return fmt.Errorf("failed to save keys: %w", err)
        }
        desc := logentry.MaskKey(store.Keys[idx].Key)
        if store.Keys[idx].Name != "" {
            desc += fmt.Sprintf(" (name: %s)", store.Keys[idx].Name)
        }
        fmt.Printf("Restored key [%d] %s for provider %q\n", idx, desc, args[0])
        triggerReload()
        return nil
    },
}
```

- [ ] **Step 6: 定义 keyPurgeCmd**

```go
var keyPurgeCmd = &cobra.Command{
    Use:   "purge <provider>",
    Short: "Permanently remove all deleted keys",
    Long: `Remove all soft-deleted API keys permanently. This cannot be undone.`,
    Args: cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        provider := args[0]
        store, err := keypool.LoadKeys(provider)
        if err != nil {
            return fmt.Errorf("failed to load keys for %q: %w", provider, err)
        }
        if store == nil {
            return fmt.Errorf("no keys found for provider %q", provider)
        }

        // Count and remove deleted keys
        var remaining []keypool.KeyEntry
        purged := 0
        for _, entry := range store.Keys {
            if entry.Deleted {
                purged++
                continue
            }
            remaining = append(remaining, entry)
        }
        if purged == 0 {
            fmt.Printf("No deleted keys to purge for provider %q\n", provider)
            return nil
        }

        store.Keys = remaining
        if err := keypool.SaveKeys(provider, store); err != nil {
            return fmt.Errorf("failed to save keys: %w", err)
        }
        fmt.Printf("Purged %d deleted key(s) from provider %q (remaining: %d)\n", purged, provider, len(remaining))
        triggerReload()
        return nil
    },
}
```

- [ ] **Step 7: 在 init() 中注册新命令**

```go
keyCmd.AddCommand(keyRestoreCmd)
keyCmd.AddCommand(keyPurgeCmd)
keyListCmd.Flags().Bool("all", false, "Show all keys including deleted ones")
addKeyIndexFlags(keyRestoreCmd)
```

- [ ] **Step 8: 运行测试**

```bash
go test -tags=unit -count=1 -short ./internal/keypool/ ./internal/cli/
```

- [ ] **Step 9: Commit**

```bash
git add internal/keypool/store.go internal/cli/key.go
git commit -m "feat: 添加软删除保护（#306）— key remove 改为标记删除，新增 restore/purge"
```

---

### Task 6: #314 — config view 输出不完整

**Files:**
- Modify: `internal/cli/config.go:132-190` (configViewCmd.RunE)
- Test: `internal/cli/config_cmd_test.go` 追加 view 测试

**Interfaces:**
- Consumes: `config.ConfigFieldDescriptors`, `config.FieldScopeProvider`, `getFieldValue(tc, name, fd)`, `maskSensitiveValue(fd, val)`, `config.LoadAllTomlProviders(source)`, `config.XDGConfigPath()`
- Produces: 修改后的 `configViewCmd.RunE`（遍历 descriptor 表）

- [ ] **Step 1: 修改 configViewCmd.RunE**

将硬编码的 `fmt.Printf` 列表替换为 descriptor 遍历：

```go
RunE: func(cmd *cobra.Command, args []string) error {
    source, err := config.XDGConfigPath()
    if err != nil {
        return fmt.Errorf("failed to determine XDG config path: %w", err)
    }
    if _, statErr := os.Stat(source); statErr != nil {
        return fmt.Errorf("no configuration file found (looked at %s)", source)
    }

    // Load TOML for field values
    tc, err := config.LoadTomlConfig(source)
    if err != nil {
        return fmt.Errorf("failed to load configuration: %w", err)
    }

    providers, err := config.LoadAllTomlProviders(source)
    if err != nil {
        return fmt.Errorf("failed to load configuration: %w", err)
    }

    fmt.Printf("Configuration source: %s\n", source)
    for name := range providers {
        fmt.Printf("\n--- Provider: %s ---\n", name)
        for _, fd := range config.ConfigFieldDescriptors {
            if fd.Scope != config.FieldScopeProvider {
                continue
            }
            val, _ := getFieldValue(tc, name, &fd)
            fmt.Printf("  %-30s %s\n", fd.DisplayName+":", maskSensitiveValue(&fd, val))
        }
    }
    return nil
},
```

- [ ] **Step 2: 运行测试**

```bash
go test -tags=unit -count=1 -short ./internal/cli/
```

- [ ] **Step 3: Commit**

```bash
git add internal/cli/config.go
git commit -m "fix: config view 改为 descriptor 驱动，修复缺失字段（#314）"
```

---

## 验证

所有 task 完成后：

```bash
make check
make test-unit
```

预期：全部通过，无回归。