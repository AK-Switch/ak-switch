# field_descriptor 反射化（#299 C2 剩余）

> Architecture Review Candidate 2 剩余部分 from [#299](https://github.com/AK-Switch/ak-switch/issues/299)
> 日期: 2026-08-16
> 状态: 已批准

## 背景

`field_descriptor.go`（597 行）是项目 shallow module 的典型示例，对 `ProviderConfig` 已有的信息做了三重声明：

1. `ProviderConfig` struct 字段 — 类型的来源
2. struct tag `toml:"..."` + `default:"..."` — TOML 路径和默认值的来源
3. `ConfigFieldDescriptors` 表 — Key/Type/Default/Scope/Parse/Format/Persist 的手写副本

`mergeWithDefaults` 已在 PR #331 反射化，但 `ConfigFieldDescriptors` 表本身仍是手写副本，且 `DeepCopy`（48 行手写字段拷贝）和 `mergeWithDefaults` 末尾的 `RuntimeConfig` 同步段（7 行手写）也未反射化。新增字段时 `mergeWithDefaults` 自动跟，但 descriptor 表 / DeepCopy / RuntimeConfig sync 三处会漏，无编译期检查。

## 目标

用 struct tag 反射派生 descriptor 的元数据与纯样板闭包（Parse/Format/Persist），消除三重声明。保留无法反射表达的定制闭包（ApplyRuntime 级联、enum 校验 Parse）。零行为变更。

## 不做的事

- 不动 `mergeWithDefaults` 反射逻辑（#331 已完成）
- 不删 `disable_thinking` 字段（#302 另议，breaking change，不进本批）
- 不改任何对外行为（config list/get/set 输出、Admin API runtime-config、TOML 解析）
- 不重构 CLI 侧 `getFieldValue` 的 switch（#336 已处理）

## 现状分类

descriptor 表 19 个字段的内容分三类：

### A 类 — 纯样板，反射可完全替代

- `Parse`：int→`strconv.Atoi`、float→`strconv.ParseFloat`、bool→`strconv.ParseBool`、string→原样。完全由 `Type` 决定。
- `Format`：与 Parse 对称的反向转换。
- `Persist`：把值赋给 `Config`（provider scope）或 `TomlConfig`（global scope）的对应字段。由 struct tag 的字段名反射定位。

### B 类 — 有定制逻辑，反射无法覆盖，保留闭包

- `ApplyRuntime`：cooldown_sec/backoff_cap_sec/backoff_multiplier 级联调用 `ConfigurePoolCBs`；cb_reset_sec 调两个 setter；http_timeout_sec 调 `SetProxyTimeout`。这些是有意义的业务级联，不是类型转换。
- `Parse` 中的 enum 校验：log_level / thinking_mode / rectify_thinking_map_to / key_selection 有 enum 校验，非纯类型转换，需定制 Parse 覆盖默认派生。

### C 类 — 元数据，从 struct tag 派生

Key、TomlPath、Default、RuntimeEditable、ReadOnly、MinInt、Scope、Type。

## 设计

### 1. struct tag 编码元数据

给 `ProviderConfig` 和 `TomlConfig`（global 字段）的 struct 字段加自定义 `field` tag：

```go
type ProviderConfig struct {
    TargetBase  string `toml:"target" field:"target,display:Target URL,scope:provider"`
    CooldownSec int    `toml:"cooldown_sec" field:"cooldown_sec,display:Cooldown (sec),scope:provider,default:15,runtime,min:1"`
    BackoffMultiplier float64 `toml:"backoff_multiplier" field:"backoff_multiplier,display:Backoff Multiplier,scope:provider,default:2,runtime"`
    DisableThinking bool   `toml:"disable_thinking,omitempty" field:"disable_thinking,display:Disable Thinking,scope:provider,default:false"`
    // ...
}
```

`field` tag 语法：`<key>,display:<显示名>,scope:<provider|global>,default:<默认值>[,runtime][,readonly][,min:<n>]`

### 2. 派生规则

| descriptor 字段 | 派生来源 |
|---|---|
| `Key` | `field` tag 第 1 段；缺失则 fallback 到 `toml` tag |
| `Type` | `reflect.Kind`：int→FieldTypeInt，string→FieldTypeString，bool→FieldTypeBool，float64→FieldTypeFloat64 |
| `Default` | `default:` 段；缺失则类型零值的字符串 |
| `RuntimeEditable` | 有 `runtime` 标记 → true |
| `ReadOnly` | 有 `readonly` 标记 → true |
| `MinInt` | `min:` 段；无则 -1 |
| `Scope` | `scope:` 段 |
| `TomlPath` | provider scope → `provider.%s.<toml>`；global scope → `<toml>` |

### 3. Parse/Format/Persist 的派生

- `Parse`/`Format`：由 `Type` 决定，用通用函数 `parseByType`/`formatByType` 替代 19 个闭包。
  - int: `strconv.Atoi` / `strconv.Itoa`
  - float64: `strconv.ParseFloat(s, 64)` / 智能格式化（整数去小数点）
  - bool: `strconv.ParseBool` / `strconv.FormatBool`
  - string: 原样 / `fmt.Sprintf("%v", v)`
- `Persist`：反射定位 struct 字段赋值。
  - provider scope：`reflect.ValueOf(c).Elem().FieldByName(<structField>).Set(...)`，写 `*Config`
  - global scope：写 `*TomlConfig`，由 scope 分流

### 4. 两阶段构建 + override map

```go
// reflectBuildDescriptors 遍历 ProviderConfig + TomlConfig 的 struct tag，
// 反射生成所有字段的基础 descriptor（含派生的 Parse/Format/Persist，无 ApplyRuntime）。
func reflectBuildDescriptors() []ConfigFieldDescriptor { ... }

// customClosures 存放无法反射派生的定制闭包，按 field key 索引。
var customClosures = map[string]customClosure{
    "cooldown_sec": {
        ApplyRuntime: func(ps any, provider string, value any) (any, error) { ... 级联 ConfigurePoolCBs ... },
    },
    "log_level": {
        Parse:       func(s string) (any, error) { ... enum 校验 ... },  // 覆盖默认 Parse
        ApplyRuntime: func(...) { ... },
    },
    "rectify_thinking_map_to": {
        Parse:        func(s string) (any, error) { ... enum 校验 + disabled→"" 映射 ... },
        Format:       func(v any) string { ... ""→"disabled" 显示映射 ... },  // 覆盖默认 Format
        ApplyRuntime: func(...) { ... },
    },
    // max_retries, backoff_cap_sec, backoff_multiplier, cb_reset_sec, upstream_cb_threshold,
    // http_timeout_sec, thinking_mode, key_selection 同理（仅 ApplyRuntime 或 +定制 Parse）
}

var ConfigFieldDescriptors = buildDescriptors()

func buildDescriptors() []ConfigFieldDescriptor {
    base := reflectBuildDescriptors()
    for i := range base {
        if cc, ok := customClosures[base[i].Key]; ok {
            if cc.Parse != nil { base[i].Parse = cc.Parse }
            if cc.Format != nil { base[i].Format = cc.Format }
            if cc.ApplyRuntime != nil { base[i].ApplyRuntime = cc.ApplyRuntime }
        }
    }
    return base
}
```

**关键点：**
- `customClosures` 只含 B 类定制逻辑（10 个 ApplyRuntime + 4 个定制 Parse + 1 个定制 Format）。
- 排序：反射按 struct 字段声明顺序遍历，与现有 descriptor 表顺序一致，保证 `config list` 输出稳定。
- `FindField` 逻辑不变。

### 5. DeepCopy + RuntimeConfig sync 反射化

**DeepCopy：**

```go
func (c *Config) DeepCopy() *Config {
    cp := &Config{}
    reflectCopyStruct(&cp.ProviderConfig, &c.ProviderConfig)
    reflectCopyStruct(&cp.RuntimeConfig, &c.RuntimeConfig)
    return cp
}

// reflectCopyStruct 遍历字段，slice 做 copy，其余值类型直接 Set。
func reflectCopyStruct(dst, src interface{}) {
    dv := reflect.ValueOf(dst).Elem()
    sv := reflect.ValueOf(src).Elem()
    for i := 0; i < sv.NumField(); i++ {
        sf := sv.Field(i)
        if sf.Kind() == reflect.Slice {
            newSlice := reflect.MakeSlice(sf.Type(), sf.Len(), sf.Cap())
            reflect.Copy(newSlice, sf)
            dv.Field(i).Set(newSlice)
        } else {
            dv.Field(i).Set(sf)
        }
    }
}
```

**RuntimeConfig sync：** 当前 `mergeWithDefaults` 末尾 7 行手写把 ProviderConfig 的 8 个字段拷到 RuntimeConfig。两 struct 同名字段一一对应，反射化：

```go
func syncRuntimeConfig(rc *RuntimeConfig, pc *ProviderConfig) {
    rcVal := reflect.ValueOf(rc).Elem()
    pcVal := reflect.ValueOf(pc).Elem()
    for i := 0; i < rcVal.NumField(); i++ {
        fieldName := rcVal.Type().Field(i).Name
        if f := pcVal.FieldByName(fieldName); f.IsValid() {
            rcVal.Field(i).Set(f)
        }
    }
}
```

`mergeWithDefaults` 末尾的 7 行手写替换为 `syncRuntimeConfig(&result.RuntimeConfig, &result.ProviderConfig)`。

**边界处理：**
- slice 深拷贝保证 `Keys`/`KeyNames` 不共享底层数组（行为与原手写 `copy()` 一致）。
- 未导出字段：`ProviderConfig`/`RuntimeConfig` 同在 `config` 包，反射可访问未导出字段。
- `reflectCopyStruct` 只处理值类型和 slice，不递归到指针/嵌套 struct（当前 struct 无指针字段，YAGNI 不加递归）。

## 测试策略

零行为变更的主证据是现有测试全部保持通过（不修改测试期望值）。现有测试：

- `field_descriptor_test.go` — Parse/Format 各类型
- `config_defaults_test.go` — 默认值、mergeWithDefaults、DeepCopy（含 DisableThinking 继承）
- `config_test.go` — TOML 加载、字段解析、DeepCopy 往返
- `config_cmd_test.go` / `field_descriptor_test.go` — ConfigFieldDescriptors 集合和顺序
- CLI 侧 `config_cmd_test.go` — config list/get 输出字段集合

### 新增断言测试

- `TestReflectBuildDescriptors_Completeness` — 反射生成的 descriptor 集合 = 原 19 个 key，顺序一致（防漏字段、防顺序漂移）
- `TestReflectBuildDescriptors_DefaultsMatch` — 每个字段的 Default 与 struct tag `default:` 一致
- `TestDeepCopy_SliceIndependence` — 改副本 slice 不影响原对象（原 TestDeepCopy 没覆盖）
- `TestPersist_Reflection` — Persist 后 Config 字段值正确（覆盖 int/string/bool/float 四类型）

### 等价性金标准测试（关键）

反射化前，用现有手写表导出全部 descriptor 的快照（Key/Type/Default/Scope/RuntimeEditable/MinInt/ReadOnly/TomlPath），存为测试 fixture。反射化后跑同样导出，断言字节级一致。防"语义偏移"的最强保障。

## 风险与缓解

- **struct tag 漏标** → 某字段 descriptor 丢失 → 金标准快照测试捕获
- **reflect.Set 对不可寻址值 panic** → Persist/DeepCopy 在包内操作导出 struct 的指针，寻址安全；测试覆盖
- **global 字段 Persist 写错 struct**（TomlConfig vs Config）→ scope 分流逻辑单测覆盖
- **descriptor 顺序变化导致 config list 输出抖动** → 反射按 struct 字段声明顺序遍历，与现有表顺序一致；金标准快照含顺序断言

## 成功标准

- 现有所有 config 包 + CLI 包单元测试零修改通过
- `field_descriptor.go` 从 597 行降到 ~150 行（反射构建逻辑 + override map）
- `config.go` 的 `DeepCopy` 从 48 行降到 ~5 行
- `mergeWithDefaults` 的 RuntimeConfig sync 从 7 行手写降到 1 行函数调用
- 金标准快照测试通过

## 涉及文件

| 文件 | 动作 | 说明 |
|---|---|---|
| `internal/config/config.go` | 修改 | `ProviderConfig` 加 `field` tag；`DeepCopy` 反射化；`RuntimeConfig` sync 提取为函数 |
| `internal/config/config_toml.go` | 修改 | `TomlConfig` 的 global 字段（port/log_file）加 `field` tag |
| `internal/config/field_descriptor.go` | 重写 | 手写 descriptor 表 → 反射构建 + override map；`ParseDefault` 内部改调 `parseByType`，保留导出签名（CLI 3 处调用） |
| `internal/config/field_descriptor_test.go` | 修改 | 新增金标准快照测试 + 派生测试 |
| `internal/config/config_defaults_test.go` | 修改 | 新增 DeepCopy slice 独立性测试 |

## 顺序一致性约束

反射生成的 descriptor 必须与现有手写表的字段顺序完全一致，因为 `config list` 输出顺序依赖此顺序。现有顺序：

1. target
2. cooldown_sec
3. max_retries
4. backoff_cap_sec
5. backoff_multiplier
6. cb_reset_sec
7. upstream_cb_threshold
8. http_timeout_sec
9. log_level
10. health_check_interval_sec
11. admin_token
12. disable_thinking
13. thinking_mode
14. rectify_thinking_map_to
15. genai_model
16. keys_file
17. key_selection
18. port（global）
19. log_file（global）

反射构建时先遍历 `ProviderConfig` 字段（1-17），再遍历 `TomlConfig` 的 global 字段（18-19），需保证 `ProviderConfig` 的 struct 字段声明顺序与上表一致。若当前声明顺序不同，需在 struct 定义中调整字段声明顺序以匹配（这是必要的顺序对齐，属本设计范围）。
