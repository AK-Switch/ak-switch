# Independent Issues Batch 1 — CLI Key Management & UX

> 日期: 2026-08-15
> 状态: 设计已确认，待实现
> 关联 Discussion: #318

## 背景

当前有 3 个 Open PR 在并行开发（#304 合并 Runtime Config Descriptor、#316 流式 token 修复、#284 Dependabot 依赖更新）。为避免冲突，本批次只处理与这些 PR 修改范围**完全隔离**的独立 issues。

所有改动集中在 `internal/cli/` 和 `internal/keypool/`（#306 涉及），不触碰已开 PR 的修改文件（`field_descriptor.go`、`admin_api.go`、`proxy_executor.go`、`tokenestimator.go`）。

## 范围

| Issue | 标题 | 领域 |
|-------|------|------|
| #286 | key export 子命令 | CLI key 命令增强 |
| #282 | key import --create flag | CLI key 命令增强 |
| #281 | key import CSV 格式 | CLI key 命令增强 |
| #314 | config view 输出不完整 | CLI UX |
| #307 | rename/update 命令合并 | CLI UX |
| #306 | 误删 API key 保护（软删除） | CLI UX + keypool |
| #309 | key.json 存储说明文档 | 文档（已取消，项目不做文档） |

## 设计

### #286 — key export 子命令

**位置:** `internal/cli/key.go`

新增 `keyExportCmd` 子命令：

```
akswitch key export <provider> [--output file]
```

- `keypool.LoadKeys(provider)` 加载（兼容加密存储，自动解密）
- 无 `--output` → 输出 JSON 到 stdout（`json.MarshalIndent(store)`）
- 有 `--output` → 写入文件（权限 0600），文件已存在时提示确认覆盖
- 无 provider 参数时遍历所有 provider 导出（可选增强，本期不做）
- 注册到 `keyCmd` 子树

### #282 — key import --create flag

**位置:** `internal/cli/key.go` 的 `keyImportCmd`

新增两个 flag：

```go
keyImportCmd.Flags().StringP("target", "t", "", "Upstream target base URL (required with --create when provider is missing)")
keyImportCmd.Flags().Bool("create", false, "Auto-create the provider if it doesn't exist")
```

逻辑：
1. 导入前检查 provider 是否存在于 config.toml
2. 存在 → 正常导入（幂等，不重复创建）
3. 不存在且 `--create` 且 `--target` 提供 → 自动创建 provider（等价于 `akswitch provider add <name> --target <url>`）
4. 不存在且 `--create` 但无 `--target` → 报错提示需提供 `--target`
5. 不存在且无 `--create` → 现有行为（加载 provider keys 失败报错）

### #281 — key import CSV 格式

**位置:** `internal/cli/key.go` 的 `parseKeyEntries`

新增 CSV 解析分支 `parseCSV`，在 JSON 尝试失败后调用。

解析规则：
1. 跳过 `#` 开头的注释行
2. 检查第一行是否为**表头**（包含已知列名，如 `key`/`api_key`/`token`/`secret`/`name`/`account_name`/`username` 等）
   - 是表头 → 按表头列名映射构建 `KeyEntry`
   - 不是表头 → 按位置回退推断
3. 位置回退规则：
   - 1 列 → 该列是 key
   - 2 列 → 第一列 name，第二列 key（与 issue 示例一致）
   - 3+ 列 → 报错，提示"无法推断列映射，请使用带表头的 CSV 并用已知列名"
4. 表头但对应列缺失 → 报错提示

实现方式：简单逗号分割（不使用 `encoding/csv` 完整解析器，避免过度设计；若发现带引号场景再升级）。

**安全规则：** 若推断结果无法确定 key 列（如所有列都有值但猜不出），报错而不是静默导入错误列。

### #314 — config view 输出不完整

**位置:** `internal/cli/config.go` 的 `configViewCmd`

将 `configViewCmd` 的硬编码 `fmt.Printf` 列表改为遍历 `config.ConfigFieldDescriptors`：

```go
for _, fd := range config.ConfigFieldDescriptors {
    if fd.Scope != config.FieldScopeProvider { continue }
    val, _ := getFieldValue(tc, name, &fd)
    fmt.Printf("  %-30s %s\n", fd.DisplayName+":", maskSensitiveValue(&fd, val))
}
```

- 参考 `configListCmd`（config.go:251）的现有实现
- 保持 `Sanitized()` 的 key 打码逻辑（`maskSensitiveValue`）
- 修复缺失字段：`thinking_mode`、`rectify_thinking_map_to`、`http_timeout_sec` 等自动出现
- 移除 `DisableThinking` 硬编码行（已被 `thinking_mode` 取代）

### #307 — rename/update 命令合并

**位置:** `internal/cli/key.go`

1. `keyUpdateCmd` 参数改为可选：`Args: cobra.RangeArgs(2,3)`
2. 只传 2 个参数（provider + index）时：
   - 必须带 `--name`（否则无操作无意义，报错提示）
   - 等价于旧 `rename` 行为：只改 name，key 值不变
3. 移除 `keyRenameCmd` 定义和注册
4. 更新 `keyCmd` 的 `Long` 描述，移除 rename 相关行
5. 更新相关测试（`key_import_test.go` 中若有 rename 测试则迁移）

### #306 — 误删 API key 保护（软删除）

**位置:** `internal/keypool/store.go` + `internal/cli/key.go`

**数据结构:**

```go
type KeyEntry struct {
    Key      string `json:"key"`
    Name     string `json:"name,omitempty"`
    Disabled bool   `json:"disabled,omitempty"`
    Deleted  bool   `json:"deleted,omitempty"`  // 新增
}
```

**行为变更:**

| 操作 | 现在 | 改后 |
|------|------|------|
| `key remove` | 从数组删除，不可恢复 | 标记 `Deleted=true` |
| `key list` | 显示所有 | 隐藏 Deleted=true |
| `key list --all` | 无 | 显示 Deleted 标记 `[deleted]` |
| `key restore <provider> <idx>` | ❌ | 设置 `Deleted=false` |
| `key purge <provider>` | ❌ | 永久删除所有 Deleted=true 的 key |

**关键决策:**
- `LoadKeysFromStore()`（→ `KeyPool`）过滤掉 `Deleted=true` 的 key，避免路由池加载已删除 key
- `LoadKeys()`（CLI 用）返回完整 store 包括 Deleted，CLI 才能恢复
- 存储兼容：新字段 `omitempty`，旧文件无该字段不报错
- 与 `Disabled` 区分：Disabled 在路由池中但不可用（可 enable 恢复），Deleted 不在路由池中

## 验证

每项实现后运行:

1. `make check` — lint + vet + fmt
2. `go test -tags=unit -count=1 ./internal/cli/` — CLI 包单测
3. `go test -tags=unit -count=1 ./internal/keypool/` — keypool 包单测（#306）

## 涉及文件清单

- `internal/cli/key.go` — #286, #282, #281, #307, #306(CLI 侧)
- `internal/cli/config.go` — #314
- `internal/cli/key_import_test.go` — CSV/export/restore 测试
- `internal/keypool/store.go` — #306(Deleted 字段)
- `internal/keypool/keypool.go` — #306(过滤 Deleted)
- `docs/key-storage.md` — #309

## 不做的事

- 不触碰 `internal/config/field_descriptor.go`、`internal/server/admin_api.go`、`internal/server/proxy_executor.go`、`internal/tokenestimator/*`（#304/#316 修改中）
- 不处理 #299/#300 的其他 Candidates（#300 Candidate 2 等已分配他人）
- 不处理 #302/#305/#311 等与 #304/#316 冲突的 issues