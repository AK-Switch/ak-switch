# Candidate 1 — Collapse runtime config field management

## 问题

`admin_api.go` 中三个 switch 语句各自枚举相同的 8 个 runtime config 字段：

- `setRuntimeConfigField` (742–826) — 验证 → 应用值 → 触发副作用
- `persistRuntimeConfigField` (964–1014) — 转换类型 → 写入 provider 段
- `persistRuntimeConfigFieldToDefault` (1018–1060) — 转换类型 → 写入 default 段

加新字段必须三处同时改，且三处 case 顺序不一致，容易漏改。

## 方案

引入 `runtimeConfigField` 描述符结构体，每个字段定义一次。三个 dispatch site 统一遍历描述符表。

```go
type runtimeConfigField struct {
    key     string
    apply   func(ps *ProviderState, raw interface{}) (interface{}, error)
    persist func(cfg *config.Config, val interface{})
}
```

- `apply` — 封装验证 + 副作用（如设置 `ps.Proxy.client.Timeout`），返回转换后的值
- `persist` — 将值写入 `Config` 结构体的对应字段

三个函数改为遍历描述符表的通用实现：

```go
func (api *AdminAPI) setRuntimeConfigField(ps *ProviderState, key string, value interface{}) (interface{}, error) {
    f := lookupRuntimeConfigField(key)
    if f == nil {
        return nil, fmt.Errorf("unknown key %q", key)
    }
    return f.apply(ps, value)
}
```

`persistRuntimeConfigField` 和 `persistRuntimeConfigFieldToDefault` 同样通过 `f.persist` 写入，不再需要独立 switch。

`getRuntimeParams` 保持不变——它只读字段，不需要描述符参与。

### log_level 副作用

`log_level` 的 apply 需要调用 `api.logManager.ApplyLevel`。描述符表为包级 `var`，apply 闭包不直接持有 `api`。处理方式：在 `handleRuntimeConfigSet` 中，set 完成后检测 key 是否为 `log_level`，若是则单独调用 `ApplyLevel`。

## 字段描述符定义

8 个字段的描述符：

| key | apply 副作用 | persist 写入 |
|------|------------|------------|
| `http_timeout_sec` | `ps.Proxy.client.Timeout` | `cfg.HTTPTimeoutSec` |
| `max_retries` | `ps.Config.MaxRetries` | `cfg.MaxRetries` |
| `cooldown_sec` | `ps.Config.CooldownSec` + `ps.Pool.ConfigureCBs` | `cfg.CooldownSec` |
| `backoff_cap_sec` | `ps.Config.BackoffCapSec` + `ps.Pool.ConfigureCBs` | `cfg.BackoffCapSec` |
| `backoff_multiplier` | `ps.Config.BackoffMultiplier` + `ps.Pool.ConfigureCBs` | `cfg.BackoffMultiplier` |
| `cb_reset_sec` | `ps.Proxy.upCB.SetResetTimeout` + `ps.Config.CBResetSec` | `cfg.CBResetSec` |
| `upstream_cb_threshold` | `ps.Proxy.upCB.SetThreshold` + `ps.Config.UpstreamCBThreshold` | `cfg.UpstreamCBThreshold` |
| `log_level` | 无（由 handler 处理副作用） | `cfg.LogLevel` |

## 测试

一个 table-driven 测试遍历所有描述符，验证：

- 有效值通过验证并正确应用副作用
- 无效值返回错误
- persist 正确写入 Config 结构体的对应字段

这覆盖了之前分散在三处的逻辑。

## 范围

- **修改：** `internal/server/admin_api.go`
- **不修改：** 其他文件，API 行为无变更
- **零行为变更：** 请求/响应格式、副作用时序均不变
