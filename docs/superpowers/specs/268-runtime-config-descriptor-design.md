# Candidate 1 — Runtime Config Descriptor Table

## 问题

`internal/server/admin_api.go` 中三个独立的 switch 语句重复实现相同的 8 个 runtime config 字段：

| Switch 位置 | 行号 | 功能 |
|---|---|---|
| `setRuntimeConfigField` | 742–826 | 应用字段值到运行时状态 |
| `persistRuntimeConfigField` | 964–1014 | 持久化单个 provider 的字段 |
| `persistRuntimeConfigFieldToDefault` | 1018–1060 | 持久化到 `[provider.default]` |

三个 switch 的 case 顺序不一致（如 `cooldown_sec` 在 set 中是第 3 个，在 persist 中是第 3 个，在 default 中也是第 3 个——目前一致但维护风险高）。添加新字段需同时修改 3 处。

## 方案：描述符表（Descriptor Table）

定义一个 `runtimeConfigField` 描述符切片，每个字段描述其所有行为，三个 dispatch site 统一遍历。

### 字段定义

每个描述符包含：

- `key` — 配置键名（如 `"http_timeout_sec"`）
- `parse` — 从 `interface{}` 解析为目标类型的函数（返回 `(T, error)`）
- `validate` — 可选的验证函数（返回 error）
- `apply` — 应用副作用（修改运行时状态）
- `setConfig` — 写入 `*config.Config` 字段的 setter

### 8 个字段的描述符

```
http_timeout_sec       → int,     >0,      set ps.Proxy.client.Timeout, set Config.HTTPTimeoutSec
max_retries            → int,     >=0,     —,                          set Config.MaxRetries
cooldown_sec           → int,     >0,      ConfigureCBs,               set Config.CooldownSec
backoff_cap_sec        → int,     >0,      ConfigureCBs,               set Config.BackoffCapSec
backoff_multiplier     → float64, >=1.0,   ConfigureCBs,               set Config.BackoffMultiplier
cb_reset_sec           → int,     >0,      upCB.SetResetTimeout,       set Config.CBResetSec
upstream_cb_threshold  → int,     >0,      upCB.SetThreshold,          set Config.UpstreamCBThreshold
log_level              → string,  in {debug,info,warn,error}, logManager.ApplyLevel, set Config.LogLevel
```

### 副作用分类

| 副作用类型 | 涉及的字段 | 实现位置 |
|---|---|---|
| HTTP client timeout | `http_timeout_sec` | `ps.Proxy.client.Timeout` |
| KeyPool CB 配置 | `cooldown_sec`, `backoff_cap_sec`, `backoff_multiplier` | `ps.Pool.ConfigureCBs(...)` |
| Upstream CB 配置 | `cb_reset_sec`, `upstream_cb_threshold` | `ps.Proxy.upCB.SetResetTimeout/SetThreshold` |
| LogManager | `log_level` | `api.logManager.ApplyLevel()` |
| 纯配置写入 | `max_retries` | 仅 `ps.Config.Xxx = v` |

### 实现结构

```go
type configField struct {
    key       string
    parse     func(interface{}) (interface{}, error)
    validate  func(interface{}) error
    apply     func(*ProviderState, *LogManager, interface{}) error
    setConfig func(*config.Config, interface{})
}

var runtimeConfigFields = []configField{
    {key: "http_timeout_sec", parse: toIntPtr, validate: positiveInt, apply: httpTimeoutApply, setConfig: setHTTPTimeoutSec},
    // ... 7 more
}
```

三个 dispatch site 统一为：

```go
func (api *AdminAPI) setRuntimeConfigField(ps *ProviderState, key string, value interface{}) (interface{}, error) {
    f := findField(runtimeConfigFields, key)
    if f == nil { return nil, fmt.Errorf("unknown key %q", key) }
    v, err := f.parse(value)
    if err != nil { return nil, err }
    if f.validate != nil { if err := f.validate(v); err != nil { return nil, err } }
    if f.apply != nil { if err := f.apply(ps, api.logManager, v); err != nil { return nil, err } }
    f.setConfig(ps.Config, v)
    return v, nil
}
```

`persistRuntimeConfigField` 和 `persistRuntimeConfigFieldToDefault` 同样遍历描述符表写入 TOML 结构。

### 行为不变保证

- 所有 `toInt`/`toFloat64` 逻辑保留，仅从 switch case 移动到描述符的 `parse` 函数
- `log_level` 的 `ApplyLevel` 副作用在 `apply` 函数中处理
- `cooldown_sec`/`backoff_cap_sec`/`backoff_multiplier` 的 `ConfigureCBs` 调用保留
- 错误消息文本不变

### 测试策略

Table-driven 测试：用描述符表驱动生成测试用例。

```go
// 为每个字段生成测试：
// 1. 合法值 → 返回期望值，无错误
// 2. 非法值（类型错误、范围错误）→ 返回错误
// 3. 非法值（枚举值错误如 log_level）→ 返回错误
```

覆盖所有 8 个字段 × 合法/非法/边界值 = ~24 个测试用例，集中在一个 `TestSetRuntimeConfigField` 中。

## 实施范围

仅修改 `internal/server/admin_api.go`，零行为变更。新增 `runtimeConfigField` 类型和 `runtimeConfigFields` 描述符表，重写三个 switch 为统一遍历逻辑。
