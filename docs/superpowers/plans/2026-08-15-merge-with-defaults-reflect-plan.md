# mergeWithDefaults 反射化改造 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用反射替换 `mergeWithDefaults` 的 28 行手写 if，使 `[provider.default]` 段自动继承所有字段，新增字段只需加 struct tag。

**Architecture:** 反射遍历 `ProviderConfig` 所有字段，用 `reflect.Value.IsZero()` 判断是否覆盖。排除 3 个 provider 专属字段（TargetBase、Keys、KeyNames）。RuntimeConfig 同步逻辑不变。

**Tech Stack:** Go 1.26, reflect 标准库

## Global Constraints

- `mergeWithDefaults` 签名不变：`func mergeWithDefaults(base, override *Config) *Config`
- 零值语义与手写版一致：`IsZero()` 判断，bool false 不覆盖
- 排除列表只含 `TargetBase`、`Keys`、`KeyNames`
- 调用方 `config_loader.go` 零改动
- 提交前通过 `make check && make test-unit`

---

### Task 1: 添加测试（TDD RED）

**Files:**
- Modify: `internal/config/config_defaults_test.go`

**Interfaces:**
- Consumes: `mergeWithDefaults(base, override *Config) *Config` — 签名不变
- Produces: 锁定当前行为的回归测试 + 暴露 KeySelection 缺陷的失败测试

- [ ] **Step 1: 添加 KeySelection 继承测试**

在 `config_defaults_test.go` 末尾添加（在 `TestLoadAllTomlProviders_WithoutDefaultSection` 之后）：

```go
func TestMergeWithDefaults_KeySelection(t *testing.T) {
    // 验证 KeySelection 从全局段继承（当前手写版漏这个字段，会失败）
    base := &Config{
        ProviderConfig: ProviderConfig{
            KeySelection: "random",
        },
    }
    override := &Config{
        ProviderConfig: ProviderConfig{
            TargetBase: "https://example.com",
        },
    }
    result := mergeWithDefaults(base, override)
    if result.KeySelection != "random" {
        t.Errorf("KeySelection = %q, want %q (inherited from default)", result.KeySelection, "random")
    }
}
```

- [ ] **Step 2: 添加全字段回归测试**

在 `TestMergeWithDefaults_KeySelection` 之后添加：

```go
func TestMergeWithDefaults_AllFieldsInherit(t *testing.T) {
    // base 模拟 [provider.default] 段，override 只设必要字段
    // 验证所有非排除字段都从 base 继承
    // Base 也携带排除字段的"垃圾值"——如果排除失效，这些值会被错误继承
    base := &Config{
        ProviderConfig: ProviderConfig{
            Port:                   9090,
            Host:                   "0.0.0.0",
            MaxRetries:             3,
            HTTPTimeoutSec:         60,
            CooldownSec:            20,
            BackoffCapSec:          240,
            BackoffMultiplier:      3,
            CBResetSec:             60,
            UpstreamCBThreshold:    10,
            HealthCheckIntervalSec: 10,
            LogLevel:               "warn",
            AdminToken:             "global-token",
            DisableThinking:        true,
            ThinkingMode:           "rectify",
            RectifyThinkingMapTo:   "enabled",
            GenaiModel:             "claude-opus-4",
            KeysFile:               "global.keys",
            KeySelection:           "random",
            HealthCheckPath:        "/healthz",
            HealthCheckTimeoutSec:  10,
            LogFile:                "/var/log/akswitch.log",
            LogMaxSize:             200,
            LogMaxAge:              30,
            ErrorDumpMaxAge:        14,
            CalibrationIntervalSec: 7200,
            // 排除字段设置垃圾值，验证不会泄漏到 result
            TargetBase: "https://should-not-inherit.example.com",
            Keys:       []string{"should-not-inherit-key"},
            KeyNames:   []string{"should-not-inherit-name"},
        },
    }
    override := &Config{
        ProviderConfig: ProviderConfig{
            TargetBase: "https://api.example.com/v1",
        },
    }
    result := mergeWithDefaults(base, override)

    // 验证继承字段全部来自 base
    tests := []struct {
        name string
        got  any
        want any
    }{
        {"Port", result.Port, 9090},
        {"Host", result.Host, "0.0.0.0"},
        {"MaxRetries", result.MaxRetries, 3},
        {"HTTPTimeoutSec", result.HTTPTimeoutSec, 60},
        {"CooldownSec", result.CooldownSec, 20},
        {"BackoffCapSec", result.BackoffCapSec, 240},
        {"BackoffMultiplier", result.BackoffMultiplier, 3.0},
        {"CBResetSec", result.CBResetSec, 60},
        {"UpstreamCBThreshold", result.UpstreamCBThreshold, 10},
        {"HealthCheckIntervalSec", result.HealthCheckIntervalSec, 10},
        {"LogLevel", result.LogLevel, "warn"},
        {"AdminToken", result.AdminToken, "global-token"},
        {"DisableThinking", result.DisableThinking, true},
        {"ThinkingMode", result.ThinkingMode, "rectify"},
        {"RectifyThinkingMapTo", result.RectifyThinkingMapTo, "enabled"},
        {"GenaiModel", result.GenaiModel, "claude-opus-4"},
        {"KeysFile", result.KeysFile, "global.keys"},
        {"KeySelection", result.KeySelection, "random"},
        {"HealthCheckPath", result.HealthCheckPath, "/healthz"},
        {"HealthCheckTimeoutSec", result.HealthCheckTimeoutSec, 10},
        {"LogFile", result.LogFile, "/var/log/akswitch.log"},
        {"LogMaxSize", result.LogMaxSize, 200},
        {"LogMaxAge", result.LogMaxAge, 30},
        {"ErrorDumpMaxAge", result.ErrorDumpMaxAge, 14},
        {"CalibrationIntervalSec", result.CalibrationIntervalSec, 7200},
    }
    for _, tc := range tests {
        if tc.got != tc.want {
            t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
        }
    }

    // 验证排除字段的行为
    // TargetBase: override 设了真实值 → 覆盖生效，且 base 垃圾值不泄漏
    // Keys/KeyNames: override 未设 → result 必须为空，base 垃圾值绝不泄漏
    if result.TargetBase != "https://api.example.com/v1" {
        t.Errorf("TargetBase = %q, want override value", result.TargetBase)
    }
    if len(result.Keys) != 0 {
        t.Errorf("Keys = %v, want empty (base garbage must not leak)", result.Keys)
    }
    if len(result.KeyNames) != 0 {
        t.Errorf("KeyNames = %v, want empty (base garbage must not leak)", result.KeyNames)
    }
}
```

- [ ] **Step 3: 添加覆盖优先级测试**

```go
func TestMergeWithDefaults_OverridePriority(t *testing.T) {
    base := &Config{
        ProviderConfig: ProviderConfig{
            MaxRetries:   3,
            CooldownSec:  20,
            LogLevel:     "info",
            KeySelection: "polling",
            // 排除字段的垃圾值——验证 override 覆盖
            TargetBase: "https://should-not-leak.example.com",
            Keys:       []string{"should-not-leak"},
            KeyNames:   []string{"should-not-leak"},
        },
    }
    override := &Config{
        ProviderConfig: ProviderConfig{
            TargetBase:   "https://api.example.com",
            MaxRetries:   5,
            LogLevel:     "debug",
            KeySelection: "random",
            Keys:         []string{"override-key"},
        },
    }
    result := mergeWithDefaults(base, override)
    if result.MaxRetries != 5 {
        t.Errorf("MaxRetries = %d, want 5 (overridden)", result.MaxRetries)
    }
    if result.CooldownSec != 20 {
        t.Errorf("CooldownSec = %d, want 20 (inherited)", result.CooldownSec)
    }
    if result.LogLevel != "debug" {
        t.Errorf("LogLevel = %q, want \"debug\" (overridden)", result.LogLevel)
    }
    if result.KeySelection != "random" {
        t.Errorf("KeySelection = %q, want \"random\" (overridden)", result.KeySelection)
    }
    // 排除字段：override 非零 → 覆盖生效，base 垃圾值不泄漏
    if result.TargetBase != "https://api.example.com" {
        t.Errorf("TargetBase = %q, want override value", result.TargetBase)
    }
    if len(result.Keys) != 1 || result.Keys[0] != "override-key" {
        t.Errorf("Keys = %v, want [override-key]", result.Keys)
    }
}
```

- [ ] **Step 4: 运行测试验证 KeySelection 测试失败**

```bash
go test -tags=unit -count=1 -short -run TestMergeWithDefaults_KeySelection ./internal/config/
```
Expected: FAIL — `KeySelection = "polling", want "random"`（因为当前手写版漏了 KeySelection）

- [ ] **Step 5: 运行全量测试记录基线**

```bash
go test -tags=unit -count=1 -short -run 'TestMergeWithDefaults' ./internal/config/
```
Expected: 2 个明确失败，1 个可能失败，3 个通过。
- FAIL `TestMergeWithDefaults_KeySelection` — 手写版漏 KeySelection，`KeySelection = "polling", want "random"`
- FAIL `TestMergeWithDefaults_AllFieldsInherit` — 手写版 `result := base.DeepCopy()` 会把 base 的 Keys/KeyNames 垃圾值带入 result，断言 `want empty` 失败（正是要修的行为）
- FAIL `TestMergeWithDefaults_OverridePriority` — 手写版不处理 KeySelection，result.KeySelection 保留 base 的 "polling"，断言 `want "random"` 失败（该测试的 TargetBase 断言会 PASS，因为 override 非零对手写版同样覆盖）
- PASS `TestMergeWithDefaults_InheritsMissingFields` / `_NoDefault` / `_AllInherited` — 手写版回归测试不受影响

- [ ] **Step 6: 暂存未提交的测试文件**

```bash
git add internal/config/config_defaults_test.go
```
先不 commit——Task 2 实现后一起提交。

---

### Task 2: 实现反射版 mergeWithDefaults

**Files:**
- Modify: `internal/config/config.go:263-366` — 替换 `mergeWithDefaults` 实现
- Test: 跑 Task 1 添加的测试

**Interfaces:**
- Consumes: `ProviderConfig` 结构体字段（`reflect` 遍历）
- Produces: 与手写版行为一致的 `mergeWithDefaults`，自动覆盖所有字段

**实现前提（重要）：** 排除语义是"该字段不从 base 继承"，而**不是**"跳过该字段"。因为 `result := base.DeepCopy()` 已经把所有 base 值拷入 result，如果对排除字段直接 `continue`，则 base 的 TargetBase/Keys/KeyNames 会残留在 result 里，且 provider 自身设置的 TargetBase 也不会被应用。正确实现必须先**清零排除字段**再从 override 应用：

```go
result := base.DeepCopy()
// 先清零排除字段——它们的值绝不应来自 base
result.TargetBase = ""
result.Keys = nil
result.KeyNames = nil
```

之后对排除字段的处理与普通字段相同（override 非零则覆盖）。这样：
- base 不设 TargetBase + provider 设 → provider 的值生效
- base 设 TargetBase（垃圾） + provider 设 → provider 的值覆盖
- base 设 TargetBase（垃圾） + provider 不设 → result.TargetBase = ""（base 的垃圾值不泄漏）

- [ ] **Step 1: 添加排除列表**

在 `mergeWithDefaults` 函数上方添加：

```go
// mergeExcludeFields 列出不应从 [provider.default] 段继承的字段。
// 这些字段是 provider 专属，全局段设置无意义。
// 实现上：mergeWithDefaults 会对这些字段清零后仅接受 override 的非零值。
var mergeExcludeFields = map[string]struct{}{
    "TargetBase": {},
    "Keys":       {},
    "KeyNames":   {},
}
```

- [ ] **Step 2: 替换 mergeWithDefaults 实现**

将 `mergeWithDefaults` 函数体（第 271-366 行）替换为：

```go
func mergeWithDefaults(base, override *Config) *Config {
    result := base.DeepCopy()

    // 排除字段不从 base 继承——清零后仅接受 override 的非零值。
    // 否则 base.DeepCopy() 会把这三个 provider 专属字段误带到 result。
    result.TargetBase = ""
    result.Keys = nil
    result.KeyNames = nil

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

    // 排除字段：显式应用 override 非零值
    // 反射循环跳过了排除字段，但 override 显式设置的值必须生效
    if override.TargetBase != "" {
        result.TargetBase = override.TargetBase
    }
    if len(override.Keys) > 0 {
        result.Keys = override.Keys
    }
    if len(override.KeyNames) > 0 {
        result.KeyNames = override.KeyNames
    }

    // Sync runtime config
    result.RuntimeConfig.HTTPTimeoutSec = result.HTTPTimeoutSec
    result.RuntimeConfig.MaxRetries = result.MaxRetries
    result.RuntimeConfig.CooldownSec = result.CooldownSec
    result.RuntimeConfig.BackoffCapSec = result.BackoffCapSec
    result.RuntimeConfig.BackoffMultiplier = result.BackoffMultiplier
    result.RuntimeConfig.CBResetSec = result.CBResetSec
    result.RuntimeConfig.UpstreamCBThreshold = result.UpstreamCBThreshold
    result.RuntimeConfig.LogLevel = result.LogLevel
    return result
}
```

- [ ] **Step 3: 更新函数注释**

将函数上方的注释（第 263-270 行）替换为：

```go
// mergeWithDefaults merges override into base, returning a new Config.
// Non-zero fields in override take precedence over base.
// Uses reflection to iterate all ProviderConfig fields automatically.
// Fields in mergeExcludeFields are never inherited from base.
```

- [ ] **Step 4: 运行测试验证全部通过**

```bash
go test -tags=unit -count=1 -short -run 'TestMergeWithDefaults' ./internal/config/
```
Expected: 全部通过（包括新加的 KeySelection、AllFieldsInherit、OverridePriority）

- [ ] **Step 5: 运行全量单元测试**

```bash
go test -tags=unit -count=1 -short ./internal/config/
```
Expected: 全部通过

- [ ] **Step 6: 运行 make check**

```bash
make check
```
Expected: lint + vet + fmt 全部通过

- [ ] **Step 7: 提交两个文件**

```bash
git add internal/config/config.go internal/config/config_defaults_test.go
git commit -m "refactor: mergeWithDefaults 反射化改造（替换手写 28 if）"
```

### 验证清单

- [ ] `TestMergeWithDefaults_KeySelection` 通过（KeySelection 从全局段继承）
- [ ] `TestMergeWithDefaults_AllFieldsInherit` 通过（所有 25 个字段正确继承，3 个排除字段不受影响）
- [ ] `TestMergeWithDefaults_OverridePriority` 通过（provider 覆盖优先级）
- [ ] 原有 3 个测试通过（回归保证）
- [ ] `TestLoadAllTomlProviders_WithDefaultSection` 通过（集成测试，加载真实 TOML）
- [ ] `make check` 通过
- [ ] `make test-unit` 通过