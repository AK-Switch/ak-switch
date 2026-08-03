# Issue #238: Config 结构体重构 — 设计规范

## Goal

将 `Config` 结构体拆分为 `ProviderConfig`（TOML 可配置字段）和 `RuntimeConfig`（运行时可变参数），`Config` 作为聚合类型保持向后兼容，让字段归属一目了然。

## Architecture

- **ProviderConfig** — 仅包含从 TOML 加载的字段（带 `toml` 标签），以及程序化设置的字段（`toml:"-"`）。加载后不再修改。
- **RuntimeConfig** — runtime-config 端点支持的 8 个字段，运行时可变，无 `toml` 标签。
- **Config** — 嵌入两者（Go 嵌入结构体自动扁平化），所有现有代码 `cfg.FieldName` 访问方式不变。

## Current State

`Config`（`internal/config/config.go`）有 20 个字段，角色不清晰：

- 部分字段同时出现在 TOML 加载和 runtime-config 端点（如 `MaxRetries`、`CooldownSec`），无法一眼看出哪个来源"主导"
- `toml:"-"` 标记的字段（`Keys`、`KeyNames`、`HealthCheckPath`、`HealthCheckTimeoutSec`）混在可配置字段中间
- `mergeDefaults()` 用反射遍历所有字段，`DefaultConfig()` 硬编码所有默认值，`Validate()` 混合验证所有约束

## Design

### Struct Layout

```go
// Config 嵌入 ProviderConfig 和 RuntimeConfig。
// Go 反射会将嵌入字段扁平化，现有 TOML 解析、字段访问均无需改动。
type Config struct {
    ProviderConfig
    RuntimeConfig
}

// ProviderConfig 包含所有从 TOML 加载或程序化设置的字段。
// 加载后不再修改。
type ProviderConfig struct {
    Port                 int      `toml:"port" default:"8080"`
    Host                 string   `toml:"host,omitempty" default:"127.0.0.1"`
    TargetBase           string   `toml:"target"`
    AdminToken           string   `toml:"admin_token,omitempty"`
    DisableThinking      bool     `toml:"disable_thinking,omitempty"`
    GenaiModel           string   `toml:"genai_model,omitempty"`
    MaxRetries           int      `toml:"max_retries" default:"1"`
    LogLevel             string   `toml:"log_level,omitempty" default:"info"`
    CooldownSec          int      `toml:"cooldown_sec" default:"15"`
    HTTPTimeoutSec       int      `toml:"http_timeout_sec" default:"30"`
    Keys                 []string `toml:"-"`                         // API keys
    KeyNames             []string `toml:"-"`                         // 对应 key names
    KeysFile             string   `toml:"keys_file,omitempty" default:"keys.json"`
    BackoffCapSec        int      `toml:"backoff_cap_sec" default:"120"`
    BackoffMultiplier    float64  `toml:"backoff_multiplier" default:"2"`
    CBResetSec           int      `toml:"cb_reset_sec" default:"30"`
    UpstreamCBThreshold  int      `toml:"upstream_cb_threshold" default:"5"`
    HealthCheckIntervalSec int    `toml:"health_check_interval_sec" default:"30"`
    HealthCheckPath       string  `toml:"-" default:"/health"`
    HealthCheckTimeoutSec int     `toml:"-" default:"5"`
    LogFile    string `toml:"log_file,omitempty"`
    LogMaxSize int    `toml:"log_max_size" default:"100"`
    LogMaxAge  int    `toml:"log_max_age" default:"7"`
    CalibrationIntervalSec int `toml:"calibration_interval_sec" default:"3600"`
}

// RuntimeConfig 包含运行时可变参数。
// 通过 /api/runtime-config 端点动态修改，无 TOML 标签。
type RuntimeConfig struct {
    HTTPTimeoutSec      int
    MaxRetries          int
    CooldownSec         int
    BackoffCapSec       int
    BackoffMultiplier   float64
    CBResetSec          int
    UpstreamCBThreshold int
    LogLevel            string
}
```

### DefaultConfig

拆分为两个独立函数，各司其职：

```go
func DefaultProviderConfig() *ProviderConfig {
    return &ProviderConfig{
        Port: 8080, Host: "127.0.0.1", MaxRetries: 1, LogLevel: "info",
        CooldownSec: 15, HTTPTimeoutSec: 30, BackoffCapSec: 120,
        BackoffMultiplier: 2, CBResetSec: 30, UpstreamCBThreshold: 5,
        HealthCheckIntervalSec: 30, HealthCheckPath: "/health",
        HealthCheckTimeoutSec: 5, KeysFile: "keys.json",
        LogMaxSize: 100, LogMaxAge: 7, CalibrationIntervalSec: 3600,
    }
}

func DefaultRuntimeConfig() *RuntimeConfig {
    return &RuntimeConfig{
        HTTPTimeoutSec: 30, MaxRetries: 1, CooldownSec: 15,
        BackoffCapSec: 120, BackoffMultiplier: 2, CBResetSec: 30,
        UpstreamCBThreshold: 5, LogLevel: "info",
    }
}
```

`DefaultConfig()` 保留为兼容方法，内部调用上述两个函数。直接使用者（如 `config_loader.go` 的 nil provider fallback）迁移到 `DefaultProviderConfig()`。

### mergeDefaults

`mergeDefaults` 留在 `ProviderConfig` 上，因为只有它携带 `default` 标签。`RuntimeConfig` 无 `toml`/`default` 标签，不需要默认填充。

`config_loader.go` 中调用 `mergeConfig(p)` → 迁移为 `mergeDefaults(p)`（直接调用，不经过已废弃的 `mergeConfig` 包装）。

### Validate

拆分为两个独立方法：

- `ProviderConfig.Validate()` — 验证 TOML 字段：`Port` 范围、`TargetBase` 非空、`Keys` 非空、`HealthCheckIntervalSec` 最小值
- `RuntimeConfig.Validate()` — 验证运行时约束：`HTTPTimeoutSec >= 1`

`Config.Validate()` 保留，内部依次调用两者。直接使用 `*Config` 的调用方无需改动。

### Sanitized

留在 `Config` 上。Go 嵌入结构体使得 `c.Keys`、`c.KeyNames` 直接可访问。深拷贝逻辑不变。

### config_loader.go 适配

`LoadAllTomlProviders` 的逻辑：

1. TOML unmarshal 到 `TomlConfig`（内部 `map[string]*Config`）—— 不受影响
2. 对 nil provider → `DefaultProviderConfig()` 赋值给 `Config` 的 `ProviderConfig` 嵌入字段
3. `mergeConfig(p)` → `mergeDefaults(p)`（直接调用）
4. 顶层 port/host/log 覆盖 provider 值 —— 字段访问方式不变

### persistRuntimeConfigField

`admin.go` 中的 `persistRuntimeConfigField` 操作 `providerCfg` 的运行时字段（如 `HTTPTimeoutSec`）。由于 RuntimeConfig 是嵌入字段，`providerCfg.HTTPTimeoutSec` 直接可写，无需改动。但 `LoadTomlConfig` 返回的 `TomlConfig` 中 `Provider` map 的 value 是 `*Config`，而 `SaveTomlConfig` 序列化时 `toml` 包会忽略无 `toml` 标签的 RuntimeConfig 字段，所以 persist 行为不变。

### 关键不变项

- `config.toml` 文件格式和字段名
- `LoadAllTomlProviders` / `LoadTomlConfig` / `SaveTomlConfig` 公开签名
- CLI `config` 子命令行为
- `Sanitized()` 输出格式
- `ConfigPayload` 结构
- 所有外部调用方的 `cfg.FieldName` 访问方式

## Implementation Decisions

### 嵌入 vs 组合

选择 **嵌入（embedding）** 而非组合（`ProviderConfig *ProviderConfig`），因为：

- Go 嵌入使字段扁平化，`toml.Unmarshal(data, &cfg)` 直接匹配标签，无需自定义 UnmarshalTOML
- 所有现有代码 `cfg.Port`、`cfg.HTTPTimeoutSec` 不变
- `reflect.TypeOf(cfg).NumField()` 会包含嵌入字段，`mergeDefaults` 的反射逻辑需要适配

### mergeDefaults 适配

当前 `mergeDefaults` 遍历 `Config` 结构体的所有字段（包括嵌入的 ProviderConfig 字段）。拆分后：

- `mergeDefaults` 改为 `ProviderConfig` 的方法
- 反射范围从 `Config` 缩小到 `ProviderConfig`，但效果相同（所有带 `default` 标签的字段都在 ProviderConfig 上）
- 不需要遍历 RuntimeConfig 的字段（没有 `default` 标签，遍历会跳过）

### LogFile / LogMaxSize / LogMaxAge 归属

这三个字段有 `toml` 标签，从 TOML 加载，不在 runtime-config 端点中。放在 **ProviderConfig** 中。

## Testing Decisions

- 迁移后所有现有测试通过
- `DefaultProviderConfig()` 和 `DefaultRuntimeConfig()` 各自添加值断言测试
- `ProviderConfig.Validate()` 和 `RuntimeConfig.Validate()` 各自添加独立的边界值测试
- `Config.Validate()` 保留现有测试（内部委托给拆分后的方法）
- `mergeDefaults` 反射逻辑测试保持原样
- 不新增配置加载的端到端测试

## Out of Scope

- 不改变 `config.toml` 的文件格式或字段名
- 不改变 `LoadTomlConfig`/`SaveTomlConfig` 的公开 API 签名
- 不改变 CLI `config` 子命令的行为
- 不改变 `Sanitized()` 的输出格式
- 不改变 `ConfigPayload` 结构

## Source Files

| 文件 | 改动 |
|------|------|
| `internal/config/config.go` | 拆分 struct、DefaultConfig、Validate、Sanitized |
| `internal/config/config_loader.go` | mergeConfig → mergeDefaults 直接调用，nil provider fallback → DefaultProviderConfig() |
| `internal/server/admin.go` | persistRuntimeConfigField 中 providerCfg 类型不变，无需改动 |
