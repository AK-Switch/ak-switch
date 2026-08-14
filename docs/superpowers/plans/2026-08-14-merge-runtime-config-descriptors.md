# 合并双套 Runtime Config 实现计划

> 根据 spec: `docs/superpowers/specs/2026-08-14-merge-runtime-config-descriptors-design.md`
> 作者: Claude Code (2026-08-14)

**目标:** 将 `internal/server/admin_api.go` 的 `runtimeConfigFields` 表合并到 `internal/config/field_descriptor.go` 的 `ConfigFieldDescriptors`，消除两套 descriptor 的同步隐患，修复 `cooldown_sec` 默认值 desync bug

**架构:** 在 `ConfigFieldDescriptor` 上添加 `ApplyRuntime` 函数字段，server 端在 descriptor entry 中直接赋值。`admin_api.go` 删除独立的 `runtimeConfigFields` 表，所有 runtime config 操作通过 `config.FindField()` + `fd.ApplyRuntime()` 完成。

**技术栈:** Go 1.26, 纯 Go stdlib

## 全局约束

- Tab 缩进（项目强制，gofmt 已配置）
- 错误包装: `fmt.Errorf("函数名: %w", err)`
- 导入: 标准库 → 项目内部包 → 第三方包，按字母序
- `ProviderState` 字段均为私有，必须通过 getter/setter 访问
- 提交前执行 `make check && make test-unit`

---

## 文件变更总览

| 文件 | 操作 | 说明 |
|---|---|---|
| `internal/config/field_descriptor.go` | 修改 | 加 `ApplyRuntime` 字段，10 个 entry 填充，2 个新 entry，fix default |
| `internal/server/admin_api.go` | 修改 | 删除 `runtimeConfigFields` 表 + 更新 3 个 call site |
| `internal/config/field_descriptor_test.go` | 修改 | 更新 `TestFindField_AllRegistered`，加 `ApplyRuntime` 断言 |
| `internal/config/config_test.go` | 修改 | 3 处 "want default 60" → "want default 15" |

---

### Task 1: 在 ConfigFieldDescriptor 添加 ApplyRuntime 字段

**文件:**
- 修改: `internal/config/field_descriptor.go:28-48`

**说明:**
在 `ConfigFieldDescriptor` 结构体的 `Persist` 字段之后添加 `ApplyRuntime` 函数字段。

**Step 1: 修改结构体**

在 `field_descriptor.go` 第 47 行后插入：

```go
	// ApplyRuntime applies a validated value to ProviderState at runtime.
	// Called by the admin API runtime config endpoint.
	// Provider is the target provider name; ps is the target ProviderState.
	// Nil for non-runtime-editable fields.
	ApplyRuntime func(ps *ProviderState, provider string, value any) (any, error)
```

`ProviderState` 是 `internal/server` 包的类型，用作函数字段的参数类型不会造成编译期循环依赖——`config` 包不需要 import `server`，`ApplyRuntime` 在运行时才被赋值。

**Step 2: 运行检查**

```bash
go build ./...
```

预期: 编译通过（新增字段不影响现有代码，所有现有 entry 不设 `ApplyRuntime` 即 nil）

**Step 3: Commit**

```bash
git add internal/config/field_descriptor.go
git commit -m "feat: ConfigFieldDescriptor 添加 ApplyRuntime 字段"
```

---

### Task 2: 为 runtime-editable 字段填充 ApplyRuntime

**文件:**
- 修改: `internal/config/field_descriptor.go:54-218`（provider-scoped section）

**说明:**
为 10 个 runtime-editable 字段添加 `ApplyRuntime`。逻辑直接复用 `admin_api.go` 中 `runtimeConfigFields` 的 `apply` 闭包。

字段映射（Server 侧 key → Descriptor Key）:
- `http_timeout_sec` → 验证 >0 → `SetProxyTimeout` + `SetHTTPTimeoutSec`
- `max_retries` → 验证 >=0 → `SetMaxRetries`
- `cooldown_sec` → 验证 >0 → `SetCooldownSec` + `ConfigurePoolCBs`
- `backoff_cap_sec` → 验证 >0 → `SetBackoffCapSec` + `ConfigurePoolCBs`
- `backoff_multiplier` → 验证 >=1.0 → `SetBackoffMultiplier` + `ConfigurePoolCBs`
- `cb_reset_sec` → 验证 >0 → `SetUpstreamCBResetTimeout` + `SetCBResetSec`
- `upstream_cb_threshold` → 验证 >0 → `SetUpstreamProxyCBThreshold` + `SetUpstreamCBThreshold`
- `log_level` → 验证枚举 → `SetLogLevel`

非 runtime-editable 字段（`target`, `admin_token`, `keys_file`, `disable_thinking`, `genai_model`, `health_check_interval_sec`）不加 `ApplyRuntime`。

**注意:** `time` 包的 `time.Duration` 和 `time.Second` 在 `ApplyRuntime` 闭包中使用——`field_descriptor.go` 目前只 import `fmt`, `strconv`, `strings`，需要添加 `"time"` import。

**Step 1: 添加 time import**

在 `field_descriptor.go` 的 import 块中添加 `"time"`。

**Step 2: 填充 8 个现有 runtime-editable 字段的 ApplyRuntime**

以 `cooldown_sec` 为例（第 71-86 行），在 `Persist` 之后添加：

```go
ApplyRuntime: func(ps *ProviderState, provider string, value any) (any, error) {
    v, err := strconv.Atoi(value.(string))
    if err != nil || v < 1 {
        return nil, fmt.Errorf("cooldown_sec must be a positive integer")
    }
    ps.SetCooldownSec(v)
    ps.ConfigurePoolCBs(
        time.Duration(v)*time.Second,
        time.Duration(ps.BackoffCapSec())*time.Second,
        ps.BackoffMultiplier(),
    )
    return v, nil
},
```

`value` 参数类型为 `any`（Server 侧传 `interface{}`），因为 `ApplyRuntime` 是公共函数字段，不能假设调用方传什么类型。在闭包内部用 `value.(string)` 断言，因为 descriptor 的 `Parse` 保证从 HTTP 请求体解析后的值是 string。

其他 7 个字段的逻辑与 `admin_api.go` 中的 `apply` 闭包一致，只是：
- `toInt(raw)` → `strconv.Atoi(value.(string))`
- `toFloat64(raw)` → `strconv.ParseFloat(value.(string), 64)`
- 验证条件完全一致
- setter 调用完全一致

**Step 3: 运行检查**

```bash
go build ./...
```

预期: 编译通过

**Step 4: Commit**

```bash
git add internal/config/field_descriptor.go
git commit -m "feat: 8 个 runtime-editable 字段填充 ApplyRuntime"
```

---

### Task 3: 新增 thinking_mode / rectify_thinking_map_to descriptor entry + fix cooldown_sec default

**文件:**
- 修改: `internal/config/field_descriptor.go`

**说明:**
两个子改动放在同一个 commit 中，因为它们都是 descriptor entry 级别的变更。

#### 3a. 修复 cooldown_sec Default desync

第 76 行:
```go
Default: "60",  →  Default: "15",
```

#### 3b. 新增 thinking_mode entry

在 `disable_thinking` entry 之后、`genai_model` entry 之前插入：

```go
{
    Key:             "thinking_mode",
    DisplayName:     "Thinking Mode",
    Scope:           FieldScopeProvider,
    TomlPath:        "provider.%s.thinking_mode",
    Type:            FieldTypeString,
    Default:         "default",
    RuntimeEditable: true,
    Parse: func(s string) (any, error) {
        v := strings.TrimSpace(strings.ToLower(s))
        switch v {
        case "default", "rectify":
            return v, nil
        }
        return nil, fmt.Errorf("invalid thinking_mode %q, use: default, rectify", s)
    },
    Format:          func(v any) string { return fmt.Sprintf("%v", v) },
    Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
        if c != nil {
            c.ThinkingMode = value.(string)
        }
    },
    ApplyRuntime: func(ps *ProviderState, provider string, value any) (any, error) {
        s, ok := value.(string)
        if !ok {
            return nil, fmt.Errorf("thinking_mode must be a string")
        }
        switch strings.TrimSpace(strings.ToLower(s)) {
        case "default", "rectify":
            ps.SetThinkingMode(s)
            return s, nil
        default:
            return nil, fmt.Errorf("invalid thinking_mode %q, use: default, rectify", s)
        }
    },
},
```

#### 3c. 新增 rectify_thinking_map_to entry

在 `thinking_mode` entry 之后、`genai_model` entry 之前插入：

```go
{
    Key:             "rectify_thinking_map_to",
    DisplayName:     "Rectify Thinking Map To",
    Scope:           FieldScopeProvider,
    TomlPath:        "provider.%s.rectify_thinking_map_to",
    Type:            FieldTypeString,
    Default:         "",
    RuntimeEditable: true,
    Parse: func(s string) (any, error) {
        v := strings.TrimSpace(strings.ToLower(s))
        switch v {
        case "enabled", "auto", "disabled":
            if v == "disabled" {
                v = ""
            }
            return v, nil
        }
        return nil, fmt.Errorf("invalid rectify_thinking_map_to %q, use: enabled, auto, disabled", s)
    },
    Format: func(v any) string {
        s := v.(string)
        if s == "" {
            return "disabled"
        }
        return s
    },
    Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
        if c != nil {
            c.RectifyThinkingMapTo = value.(string)
        }
    },
    ApplyRuntime: func(ps *ProviderState, provider string, value any) (any, error) {
        s, ok := value.(string)
        if !ok {
            return nil, fmt.Errorf("rectify_thinking_map_to must be a string")
        }
        switch strings.TrimSpace(strings.ToLower(s)) {
        case "enabled", "auto", "disabled":
            ps.SetRectifyThinkingMapTo(s)
            return s, nil
        default:
            return nil, fmt.Errorf("invalid rectify_thinking_map_to %q, use: enabled, auto, disabled", s)
        }
    },
},
```

**注意:** `Parse` 中 `"disabled"` 被映射为空字符串，与 `admin_api.go:1137-1139` 的 persist 逻辑一致。`Format` 中空字符串映射回 `"disabled"` 用于显示。

**Step 1: 修改 field_descriptor.go（三个子改动）**

1. 第 76 行 `"60"` → `"15"`
2. 在 `disable_thinking` entry 后插入 `thinking_mode`
3. 在 `thinking_mode` entry 后插入 `rectify_thinking_map_to`

**Step 2: 运行检查**

```bash
go build ./...
```

预期: 编译通过

**Step 3: Commit**

```bash
git add internal/config/field_descriptor.go
git commit -m "feat: 新增 thinking_mode/rectify_thinking_map_to descriptor + fix cooldown_sec default"
```

---

### Task 4: 删除 admin_api.go 的 runtimeConfigFields 表

**文件:**
- 修改: `internal/server/admin_api.go:943-1153`

**说明:**
删除以下内容：
- `runtimeConfigField` 结构体（第 945-952 行）
- `runtimeConfigFields` 变量（第 954-1143 行）
- `lookupRuntimeConfigField` 函数（第 1145-1153 行）

保留: `getRuntimeParams`（第 753-766 行）— 直接读 `ProviderState` getter，不需要 descriptor。

**Step 1: 删除 runtimeConfigFields 相关代码**

删除第 943 行注释 `// ── Runtime Config Field Descriptors ──────────────────────` 到第 1153 行 `}`（包含 `runtimeConfigField` struct, `runtimeConfigFields` var, `lookupRuntimeConfigField` function）。

**Step 2: 运行检查**

```bash
go build ./...
```

预期: 编译失败（因为 Task 5 还没做，call site 还在引用 `lookupRuntimeConfigField`）

**Step 3: Commit**（这一步单独 commit 删除代码，call site 在 Task 5 更新）

```bash
git add internal/server/admin_api.go
git commit -m "refactor: 删除 admin_api.go 的 runtimeConfigFields 表和 lookupRuntimeConfigField"
```

---

### Task 5: 更新 admin_api.go 的三个 call site

**文件:**
- 修改: `internal/server/admin_api.go`

**说明:**
三个函数使用 `lookupRuntimeConfigField`，替换为 `config.FindField` + `fd.ApplyRuntime`：

#### 5a. setRuntimeConfigField（第 741-750 行）

```go
// Before
func (api *AdminAPI) setRuntimeConfigField(ps *ProviderState, key string, value interface{}) (interface{}, error) {
    f := lookupRuntimeConfigField(key)
    if f == nil {
        return nil, fmt.Errorf("unknown key %q", key)
    }
    return f.apply(ps, value)
}

// After
func (api *AdminAPI) setRuntimeConfigField(ps *ProviderState, key string, value interface{}) (interface{}, error) {
    fd := config.FindField(key)
    if fd == nil || !fd.RuntimeEditable || fd.ApplyRuntime == nil {
        return nil, fmt.Errorf("unknown key %q", key)
    }
    return fd.ApplyRuntime(ps, "", value)
}
```

注意: `value` 从 `interface{}` 传入，`ApplyRuntime` 签名是 `func(ps *ProviderState, provider string, value any)`，直接传递。

#### 5b. persistRuntimeConfigField（第 890-917 行）

这个函数只用到 `f.persist`，不涉及 `apply`。需要替换为 descriptor 的 `Persist`：

```go
// Before (第 911-913 行)
f := lookupRuntimeConfigField(key)
if f != nil {
    f.persist(providerCfg, value)
}

// After
fd := config.FindField(key)
if fd != nil && fd.Persist != nil {
    fd.Persist(tc, pName, providerCfg, value)
}
```

注意: `ConfigFieldDescriptor.Persist` 签名是 `func(tc *TomlConfig, provider string, c *Config, value any)`，比 `runtimeConfigField.persist` 多两个参数。这里 `tc` 是已加载的 `*TomlConfig`，`pName` 是 provider name。

#### 5c. persistRuntimeConfigFieldToDefault（第 921-940 行）

```go
// Before (第 935-937 行)
f := lookupRuntimeConfigField(key)
if f != nil {
    f.persist(tc.Default, value)
}

// After
fd := config.FindField(key)
if fd != nil && fd.Persist != nil {
    fd.Persist(tc, "", tc.Default, value)
}
```

**Step 1: 更新三个 call site**

按 5a → 5b → 5c 顺序修改。

**Step 2: 运行检查**

```bash
go build ./...
```

预期: 编译通过

**Step 3: 运行测试**

```bash
make test-unit
```

预期: 全部通过

**Step 4: Commit**

```bash
git add internal/server/admin_api.go
git commit -m "refactor: admin_api.go 三个 call site 改用 ConfigFieldDescriptors"
```

---

### Task 6: 更新测试

**文件:**
- 修改: `internal/config/field_descriptor_test.go`
- 修改: `internal/config/config_test.go`（3 处）

#### 6a. field_descriptor_test.go — 更新 TestFindField_AllRegistered

当前测试（第 27-33 行）期望 16 个字段名，新增 `thinking_mode` 和 `rectify_thinking_map_to` 后变成 18 个：

```go
expected := []string{
    "target", "cooldown_sec", "max_retries", "backoff_cap_sec",
    "backoff_multiplier", "cb_reset_sec", "upstream_cb_threshold",
    "http_timeout_sec", "log_level", "thinking_mode",        // 新增
    "rectify_thinking_map_to",                                // 新增
    "health_check_interval_sec",
    "admin_token", "disable_thinking", "genai_model", "keys_file",
    "port", "log_file",
}
```

#### 6b. field_descriptor_test.go — 更新 TestProviderRuntimeInterface_NoCallers（改名为 TestFieldDescriptor_RuntimeEditableConsistency）

当前测试（第 141-159 行）`serverHandled` map 只有 8 个字段，缺少 `thinking_mode` 和 `rectify_thinking_map_to`：

```go
serverHandled := map[string]bool{
    "http_timeout_sec":      true,
    "max_retries":           true,
    "cooldown_sec":          true,
    "backoff_cap_sec":       true,
    "backoff_multiplier":    true,
    "cb_reset_sec":          true,
    "upstream_cb_threshold": true,
    "log_level":             true,
    "thinking_mode":         true,   // 新增
    "rectify_thinking_map_to": true, // 新增
}
```

#### 6c. config_test.go — 修复 3 处 "want default 60" → "want default 15"

位置：
- `config_test.go:457` — `CooldownSec = %d, want default 60`
- `config_test.go:587` — `CooldownSec = %d, want default 60`
- `config_test.go:969` — `CooldownSec = %d, want default 60`

三处都改为 `want default 15`。

**Step 1: 修改 field_descriptor_test.go**

更新 `TestFindField_AllRegistered` 的 expected 列表和 `TestProviderRuntimeInterface_NoCallers` 的 serverHandled map。

**Step 2: 修改 config_test.go**

替换三处 "want default 60" 为 "want default 15"。

**Step 3: 运行测试**

```bash
make test-unit
```

预期: 全部通过

**Step 4: Commit**

```bash
git add internal/config/field_descriptor_test.go internal/config/config_test.go
git commit -m "test: 更新 field_descriptor 和 config 测试（新字段 + cooldown_sec default）"
```

---

## 验证清单

- [ ] `go build ./...` 通过
- [ ] `make check` 通过（lint + vet + fmt）
- [ ] `make test-unit` 通过
- [ ] `config get cooldown_sec` 对未设置字段返回 15（而非 60）
- [ ] `config set thinking_mode` 不再返回 "unknown config key"
- [ ] Admin API `/api/runtime-config` 端点行为不变
- [ ] `field_descriptor.go` 和 `admin_api.go` 中不再有 `runtimeConfigFields` 引用
