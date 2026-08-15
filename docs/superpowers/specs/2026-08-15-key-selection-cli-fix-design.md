# Key Selection CLI 修复设计

## 背景

PR #322 实现了 `polling`/`random` 两种 key 选择策略，支持的完整链路为：

```
TOML 配置 → config.Config.KeySelection → router.go NewProviderState → pool.SetSelectionMode()
```

但 CLI 交互层有 4 个遗漏，导致用户无法通过命令行正确配置和查看 key 选择策略。

## 问题

### P1: Reload 不重新应用 SelectionMode

`provider_manager.go:ReloadConfig()` 在 reload 时创建新的 `KeyPool`（第 119 行），但未调用 `SetSelectionMode`。新建的 pool 在 `NewKeyPool` 中硬编码为 `KeySelectionPolling`（`keypool.go:59`），导致 reload 后所有 provider 的模式被重置为 `polling`。

**影响：** `akswitch config set key_selection random <provider>` 能正确写入 TOML，但 `akswitch reload` 不会生效。用户必须 `stop` + `start` 完整重启。

**修复：** `ReloadConfig` 中新建 pool 后补一行 `SetSelectionMode` 调用。

### P2: `getFieldValue` 缺少 `key_selection` case

`config.go:getFieldValue()` 的 switch 语句（第 566-599 行）硬编码了所有支持的 key，但没有 `key_selection` 的 case。所有未列出的 key 走 fallback `ParseDefault(fd)`，永远返回 `"polling"`。

**影响：** `akswitch config get key_selection <provider>` 和 `akswitch config list` 永远显示 `polling`，无法查看实际配置值。

**修复：** 在 switch 中加 `case "key_selection": return p.KeySelection, nil`。

### P3: `config view` 未显示 key_selection

`configViewCmd`（第 153-186 行）硬编码了所有要打印的字段列表，逐行 `fmt.Printf("  Field: %s\n", ...)` 输出。`key_selection` 不在其中。

**影响：** `akswitch config view` 看不到 key 选择策略。

**修复：** 补一行 `fmt.Printf("  Key selection mode: %s\n", sanitized.KeySelection)`。

### P4: 帮助文本未列出 key_selection

`configGetCmd` 和 `configSetCmd` 的 Long 描述中 Valid keys 列表（第 269-271 行、第 353-354 行）未包含 `key_selection`。

**影响：** 用户不知道可以通过 `config set key_selection` 来配置。

**修复：** 两处帮助文本各加 `key_selection`。

## 方案

所有修复均为"补遗漏"，不改变现有语义，不涉及偏移冲突。

| 问题 | 文件 | 改动 |
|------|------|------|
| P1 | `internal/server/provider_manager.go` | 第 119 行后加 `existing.pool.SetSelectionMode(...)` |
| P2 | `internal/cli/config.go` | 第 597 行前加 `case "key_selection"` |
| P3 | `internal/cli/config.go` | 第 167 行附近加一行输出 |
| P4 | `internal/cli/config.go` | 第 269-271 行和第 353-354 行加 `key_selection` |

## 向后兼容

- 所有修复均为现有功能的补全，不改变任何现有行为
- `key_selection` 默认值保持 `polling`，未配置的现有配置不受影响
- 不修改 `RuntimeEditable`、`ReadOnly` 等描述符属性
- `config get` 修复只影响显示，不影响存储

## 测试

1. 单元测试：`go test -tags=unit -short ./internal/cli/ ./internal/server/`
2. 手动验证：`akswitch config set key_selection random <provider>` → `akswitch reload` → 确认生效
3. 手动验证：`akswitch config get key_selection <provider>` → 显示正确值