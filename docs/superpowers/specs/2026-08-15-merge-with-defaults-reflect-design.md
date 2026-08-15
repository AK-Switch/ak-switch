# mergeWithDefaults 反射化改造设计

## 背景

当前 `mergeWithDefaults(base, override *Config)` 在 `config.go` 中通过手写 28 个 if 语句逐字段继承全局默认值（`[provider.default]` 段）到各 provider 上。这个手写清单需要与 struct 定义保持同步，每次新增字段都可能遗漏。

**真实案例：** `KeySelection` 字段被漏掉，导致 `[provider.default]` 段写 `key_selection = "random"` 不生效，必须逐个 provider 显式配置（PR #325 修复的就是这个 CLI 问题，但根因是 `mergeWithDefaults` 漏字段）。`CalibrationIntervalSec` 和 `ErrorDumpMaxAge` 也曾有过类似遗漏（后补上的）。

## 目标

用反射替换手写 28 个 if 语句，使字段继承自动覆盖 `ProviderConfig` 所有字段，新增字段只需加 struct tag 即可自动生效。

## 设计

### 核心机制

`mergeWithDefaults` 保持签名不变，内部改为反射遍历 `ProviderConfig` 的字段：

```go
func mergeWithDefaults(base, override *Config) *Config {
    result := base.DeepCopy()

    baseVal := reflect.ValueOf(&base.ProviderConfig).Elem()
    overrideVal := reflect.ValueOf(&override.ProviderConfig).Elem()
    resultVal := reflect.ValueOf(&result.ProviderConfig).Elem()

    for i := 0; i < baseVal.NumField(); i++ {
        field := baseVal.Type().Field(i)
        if _, excluded := mergeExcludeFields[field.Name]; excluded {
            continue
        }
        o := overrideVal.Field(i)
        r := resultVal.Field(i)
        if !o.IsZero() {
            r.Set(o)
        }
    }

    // Sync runtime config (unchanged)
    result.RuntimeConfig.HTTPTimeoutSec = result.HTTPTimeoutSec
    result.RuntimeConfig.MaxRetries = result.MaxRetries
    // ... (same as today)

    return result
}
```

### 排除列表

三个字段不继承：

```go
var mergeExcludeFields = map[string]struct{}{
    "TargetBase": {},
    "Keys":       {},
    "KeyNames":   {},
}
```

- `TargetBase` — provider 专属，每个上游不同 URL，全局段设置无意义
- `Keys` / `KeyNames` — API keys，provider 专属；且 `toml:"-"`，用户在 TOML 中写不进

### 零值语义

`reflect.Value.IsZero()` 判断零值：字符串 `""`、数值 `0`、bool `false`、切片 `nil`/空都是零值 → 不覆盖。与当前手写版行为完全一致。

**已知限制：** bool `false` 是零值，当 provider 想覆盖回 `false` 时（如 `disable_thinking = false` 覆盖全局的 `true`），不会生效。这是手写版就存在的语义，保持现状不做扩展。

### 合并顺序

```go
// config_loader.go (LoadAllTomlProviders) — 顺序不变
p = mergeWithDefaults(defaultCfg, p)  // 全局段 → provider
p.mergeDefaults()                      // default tag 填零值（独立于 mergeWithDefaults）
```

### 测试

在 `config_defaults_test.go` 中新增 table-driven 测试：

| 用例 | 验证 |
|------|------|
| 已覆盖字段行为不变 | 每个现有字段逐字段断言，反射版结果 == 手写版 |
| KeySelection 继承 | `[provider.default]` 设 `key_selection = "random"`，provider 未设 → random |
| 排除字段不继承 | 全局段设 `target`/`keys` → provider 不被污染 |
| 覆盖优先级 | 全局设值 + provider 设不同值 → provider 赢 |
| 零值不覆盖 | 全局设值 + provider 未设 → 继承全局值 |
| RuntimeConfig 同步 | 合并后 RuntimeConfig 字段与 ProviderConfig 对应字段一致 |

### 迁移兼容

- 现有 config.toml 格式不变
- `[provider.default]` 段语义不变，但补齐了 `key_selection` 的继承——用户在此段写 `key_selection = "random"` 现在会正确生效
- 调用方（`config_loader.go`）零改动
- 不涉及网络协议、API 契约、序列化格式变更

## 不做的

- 不引入多级作用域（全局 → 项目 → provider 等——YAGNI）
- 不解决 bool 零值覆盖回 false 的问题（保持现状）
- 不修改 `mergeDefaults()`（反射填 default tag 的机制独立运行）
- 不改 `field_descriptor.go`（描述符表与合并逻辑分离，保持各自职责）

## 文件变动

| 文件 | 改动 |
|------|------|
| `internal/config/config.go` | 替换 `mergeWithDefaults` 实现（28 行 if → 反射版）；新增 `mergeExcludeFields` 排除表 |
| `internal/config/config_defaults_test.go` | 新增 table-driven 测试（见测试表） |