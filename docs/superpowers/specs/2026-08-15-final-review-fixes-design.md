# Final Whole-Branch Review Fixes — 独立 Issue 批次 1

> 日期: 2026-08-15
> 状态: 设计已确认，待实现
> 背景分支: worktree-spec-independent-issues-batch1

## 背景

独立 Issue 批次 1（#307/#286/#282/#281/#306/#314）共 8 个 commit 已在 `worktree-spec-independent-issues-batch1` 分支完成并通过逐 task review。合并前执行全分支最终审查，从三个维度并行展开（交叉影响、测试完整性、类型安全），共发现 14 个 finding。

本 spec 定义修复范围与取舍，确保修复后可安全合并到 `main`。

## 审查发现汇总

三个审查代理（交叉影响 / 测试完整性 / 类型安全）合并去重后共 14 个 finding，按优先级分组：

| 优先级 | Finding | 来源 |
|--------|---------|------|
| 🔴 严重 | 集成测试 `TestKeyRemove_RemovesKey` 仍假设硬删除，与 #306 软删除冲突 | 交叉影响 + 测试 |
| 🟠 高 | `key export` 仅测错误路径，缺成功路径 | 测试完整性 |
| 🟠 高 | `key update` args=2/args=3 行为无测试 | 测试完整性 |
| 🟡 中 | `getFieldValue` 缺 `thinking_mode`/`rectify_thinking_map_to` case，view/list/get 显示错误默认值 | 类型安全 |
| 🟡 中 | `KeyRemove` 枚举值及 `updateKey` 中两个 case 成为死代码 | 交叉影响 + 类型安全 |
| 🟡 中 | `key export --output` flag 无测试 | 测试完整性 |
| 🟡 中 | `key list --all` 过滤行为无测试 | 测试完整性 |
| 🟡 中 | `keys_file` 路径在 `config view` 被 `maskSensitiveValue` 遮盖为 `(set)` | 交叉影响 |
| 🟢 低 | `key import` 帮助文本未提及 CSV 格式 | 交叉影响 |
| 🟢 低 | `keyCmd.Long` 未列出 export/restore/purge 命令 | 交叉影响 |
| 🟢 低 | `config view` 不再显示单个 key 明细（旧 `Sanitized()` 输出丢失） | 交叉影响 |
| 🟢 低 | `keyExportCmd` 文件存在检查 TOCTOU 竞态 | 交叉影响 |
| 🟢 低 | `key export` 导出包含 `Deleted=true` 的 key | 交叉影响 |
| ℹ️ | `keyCmd.AddCommand` 注册顺序合理 | 交叉影响 |

## 修复范围

### Blockers — 必须修（正确性）

**F1: 集成测试适配软删除** — `test/integration/cli_integration_test.go`

`TestKeyRemove_RemovesKey`（:588-621）断言 `len(store.Keys) == 1` 且 `store.Keys[0].Key == "sk-remove-key-2"`，这是硬删除语义。改为标记删除后，`len` 仍为 2 且 `store.Keys[0].Deleted == true`。

修复：断言改为 `len(store.Keys) == 2` 且 `store.Keys[0].Deleted == true`、`store.Keys[0].Key == "sk-remove-key-1"`；验证剩余 key 仍在 store 中。同步检查 `TestKeyRemove_InvalidIndex`（:725）是否需要调整。

**F2: `getFieldValue` 补齐 case** — `internal/cli/config.go:546-585`

descriptor 中的 `thinking_mode`（默认 `"default"`）和 `rectify_thinking_map_to`（默认 `""`，Format 为 `"disabled"`）没有对应的 `getFieldValue` case，导致 `config view`/`list`/`get` 永远显示默认值。检查 `ProviderConfig` 结构体中的 `ThinkingMode`/`RectifyThinkingMapTo` 字段实际类型并补 case。

**F3: 清理 `KeyRemove` 死代码** — `internal/cli/key.go:24-28, 58, 71`

#306 后 `keyRemoveCmd` 直接软删除，不再调用 `updateKey(args, KeyRemove)`。`KeyMutation` 中 `KeyRemove` 常量及 `updateKey` 中两个 `case KeyRemove:` 分支不可达。

修复：删除 `KeyRemove` 常量、两个 case（switch 的修改和输出部分），保留 `KeyEnable`/`KeyDisable`。注意 `iota` 枚举值——`KeyEnable=0`、`KeyDisable=1`，删除末位 `KeyRemove=2` 不影响既有值。

### Quality — 应该修（测试覆盖）

**F4: `key export` 成功路径测试** — `internal/cli/key_import_test.go`

现有 `TestKeyExportCmd` 只有 2 个错误用例。补充：真实 provider 导出到 `--output` 临时文件，断言文件存在、JSON 可解析、含 key 且格式正确。参照现有 `TestKeyExportCmd` 的 mock 模式（`tmpKeyStore` / `runKeyCommand`）。

**F5: `key update` 行为测试** — `internal/cli/key_import_test.go` 或 `provider_cmd_test.go`

`keyUpdateCmd` 支持 `cobra.RangeArgs(2, 3)`：
- args=2 + `--name`：仅重命名，不改变 key 值
- args=3：变更 key 值
- args=3 + `--name`：同时变更值 + 名称
- args=2 无 `--name`：报 "nothing to update" 错误
- `--by-name`：按名称定位

**F6: `key list --all` 过滤测试** — `internal/cli/key_import_test.go`

软删除后：默认不显示 deleted key；`--all` 显示全部并标注 deleted 状态。参照 `TestKeyRestoreCmd`/`TestKeyPurgeCmd` 的 mock 模式。

### UX — 应该修（小文本/行为）

**F7: `maskSensitiveValue` 放过 `keys_file`** — `internal/cli/config.go:528-541`

`keys_file` 是文件路径，非敏感信息。改为：`keys_file` 有值时显示完整路径，无值显示 `(not set)`。`admin_token` 保持 covered。

**F8: `key import` 帮助文本加 CSV 说明** — `internal/cli/key.go:184-203`

在 `keyImportCmd.Long` 中追加 CSV 格式说明与示例（头行 `key_name,api_key` 或位置列）。

**F9: `keyCmd.Long` 补命令列表** — `internal/cli/key.go`

当前 Long 列出 6 个命令，补充 export/restore/purge。

### 跳过（已确认的取舍）

| Finding | 原因 |
|---------|------|
| `config view` 不显示 key 明细 | `key list` 是查看 key 的正确入口，descriptor 驱动保持纯粹性 |
| export TOCTOU 竞态 | CLI 工具业界惯例，可接受 |
| export 含已删除 key | 完整备份语义，import 后可恢复完整性 |
| `findKeyIndexByName` 含已删除 key | restore 命令正需要查找已删除 key |
| 注册顺序 | 已确认合理 |

## 实现方式

直接在本工作树（`worktree-spec-independent-issues-batch1`）修复，不派 subagent。理由：9 个修复点均已被精确定位（文件 + 行号），人工干预成本低于 subagent 调度；上一轮已遇到 subagent 模型错误与文件损坏问题。

## 验证

```bash
make fmt
make vet                              # go vet ./...
go test -tags=unit -count=1 -short ./...                          # 全量单测
go test -tags=integration -count=1 -run "TestKeyRemove|TestKeyList" ./test/integration/   # 修复的集成测试（需 Docker）
```

集成测试需 Docker 环境——CI 将覆盖，本地若无环境则以单测 + go vet 为准。

## 提交策略

修复按逻辑分组原子提交（遵循 git_rules）：

1. `fix: config view 显示 thinking_mode/rectify_thinking_map_to 实际值（F2）`
2. `refactor: 清理 KeyRemove 死代码（F3）`
3. `fix: maskSensitiveValue 恢复 keys_file 路径显示（F7）`
4. `docs: 更新 key import/help 文本（F8/F9）`
5. `test: 补充 export/update/list 测试覆盖（F4/F5/F6）`
6. `test: 集成测试适配软删除语义（F1）`
7. `chore: gofmt 格式化（如有）`

修复 commit 推送到现有分支，合并后本分支即可创建 PR。