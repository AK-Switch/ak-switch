# CLI 设计重构：统一配置管理

**Issue**: #280
**日期**: 2026-08-13
**状态**: 设计阶段

## 背景

AK-Switch 当前 CLI 存在两套并行的配置修改入口，导致用户困惑且维护成本高：

- `provider update` — 改 TOML 文件，13 个硬编码 flag
- `config set` — 改 server 运行时内存，默认不持久化

同一字段（`max_retries`、`cooldown_sec`、`http_timeout_sec`）在两个命令里都能改，但行为和持久化语义完全不同。此外：

- `config list/get/set` 挂载在 `config` 子命令下，实际操作的却是 server 运行时内存，不是 `config.toml`——命名严重误导
- `key upstream-cb-reset` 挂在 `key` 下但操作的是 provider 级别的 circuit breaker
- `config set` 默认不持久化，server 重启后值丢失，有生产事故风险
- `provider add` 生成的 TOML 充满 `port = 0`、`http_timeout_sec = 0` 等零值噪音

审计范围覆盖全部 11 个 CLI 源文件，其余 6 个命令（`start`、`stop`、`reload`、`status`、`usage`、`log-level`）的意图与实际操作匹配，无需调整。

## 目标

1. `config` 命令统一管理所有配置字段——启动配置 + 运行时参数
2. `provider` 命令只管 provider 实体生命周期（add / remove / list / info）
3. 一个 descriptor table 驱动所有字段的读写，消除硬编码
4. `config set` 默认持久化，消除"改了但重启丢失"的陷阱

## 设计

### 1. 扩展 RuntimeConfigDescriptorTable 为 ConfigDescriptorTable

PR #270 已实现 `RuntimeConfigDescriptorTable`（在 `config.go` 中），驱动运行时参数的 get/set/list。扩展方案：

```
现有（PR #270 已有）:
  RuntimeConfigDescriptorTable → 覆盖 runtime config 的 8 个字段

扩展:
  ConfigDescriptorTable → 包含启动配置字段 + 运行时字段
                         启动配置字段运行时不可改（只读/需 reload）
                         运行时字段可即时生效
```

扩展字段列表（从 `provider update` 的 13 个 flag 中提取）：

| 字段 | 作用域 | TOML 路径 | 类型 | 默认值 | 运行时可改 |
|------|--------|-----------|------|--------|-----------|
| `target` | provider | `provider[<name>].target` | string | — | 是 |
| `cooldown_sec` | provider | `provider[<name>].cooldown_sec` | int | 60 | 是 |
| `max_retries` | provider | `provider[<name>].max_retries` | int | 3 | 是 |
| `backoff_cap_sec` | provider | `provider[<name>].backoff_cap_sec` | int | 300 | 是 |
| `backoff_multiplier` | provider | `provider[<name>].backoff_multiplier` | float | 2.0 | 是 |
| `cb_reset_sec` | provider | `provider[<name>].cb_reset_sec` | int | 60 | 是 |
| `upstream_cb_threshold` | provider | `provider[<name>].upstream_cb_threshold` | int | 5 | 是 |
| `http_timeout_sec` | provider | `provider[<name>].http_timeout_sec` | int | 30 | 是 |
| `health_check_interval_sec` | provider | `provider[<name>].health_check_interval_sec` | int | 30 | 是 |
| `admin_token` | provider | `provider[<name>].admin_token` | string | — | 否 |
| `disable_thinking` | provider | `provider[<name>].disable_thinking` | bool | false | 是 |
| `genai_model` | provider | `provider[<name>].genai_model` | string | — | 是 |
| `keys_file` | provider | `provider[<name>].keys_file` | string | — | 否 |
| `port` | 全局 | `port` | int | 4000 | 否 |
| `log_file` | 全局 | `log_file` | string | — | 否 |
| `admin_token` | 全局 | `admin_token` | string | — | 否 |

`config set` 不带 `--all` 时必须提供 provider 参数（provider 级字段）或不提供（全局字段），通过作用域自动判断。运行时不可改的字段标记为 `readonly: true`，`config set` 拒绝写入并提示"需修改 TOML 文件后 reload"。

已有的 8 个运行时字段保留在同一个 table 中，不重复定义。

### 2. `config` 命令语义重定义

```
config init <path?>       → 写 TOML 文件（不变）
config view [provider]    → 读 TOML 文件展示（不变）
config list [provider]    → 列 descriptor table 中所有字段的当前值
                            --all: 显示所有 provider
config get <key> [provider] → 读取单个字段值
                            --all: 显示所有 provider
config set <key> <value> [provider] → 写入字段
                            --all: 应用到所有 provider
                            默认同时改 runtime + 持久化到 TOML
                            --runtime-only: 只改 runtime，不写 TOML（当前默认行为的逆操作）
```

关键变更：
- **`config list/get/set` 不再直接调 server API**。通过 descriptor table 间接操作：对于运行时字段，内部调 server API 然后写 TOML；对于只读字段，拒绝写入。
- **`--persist` flag 移除**（当前 `config set --persist` 语义变为默认行为）。需要"只改 runtime 不持久化"时用 `--runtime-only`。
- `config view` 继续直接读 TOML 文件，不经过 descriptor table。

### 3. `provider update` 内部委托给 descriptor table

保留 `provider update` 的全部 flag 和命令路径（向后兼容），但内部实现改为调用同一个 descriptor table 的 setter。这样：

- 消除硬编码的字段映射
- `provider update` 和 `config set` 走同一套验证和持久化逻辑
- 未来新增字段只需要在 descriptor table 加一条，两处自动生效

`provider update` 的输出静默委托，不输出额外提示。

### 4. `key upstream-cb-reset` 改路径

从 `key upstream-cb-reset` 改为 `provider upstream-cb-reset`，与操作目标一致。保留旧路径作为别名一个版本，输出 deprecation 警告。

### 5. TOML 零值噪音

`provider add` 和 `config set` 写入 TOML 时，跳过零值字段。实现方式：

- 在写 TOML 前过滤 struct，只序列化非零值字段
- 或在 go-toml/v2 encoder 上配置 `OmitEmpty`
- 已有 `default` 值在 `LoadTomlConfig` 的 merge 阶段处理，不会因跳过零值而丢失

### 6. 数据结构

```go
type ConfigFieldDescriptor struct {
    Key            string         // descriptor table 中的 key，如 "cooldown_sec"
    DisplayName    string         // 用户友好的名称
    TomlPath       string         // TOML 中的路径模板。provider 级用 "provider.%s.field"，全局用 "field"
    Type           reflect.Kind   // int / string / bool / float64
    Default        any            // 默认值
    RuntimeEditable bool          // 运行时是否可改
    Validator      func(any) error // 可选的值校验函数
    Setter        func(provider, valueStr string) error // 写入逻辑
    Getter        func(provider string) (any, error)     // 读取逻辑
}
```

`ConfigDescriptorTable` 是 `[]ConfigFieldDescriptor`，由 `init()` 注册。

## 影响范围

### 命令变更

| 变更 | 命令 | 变更类型 |
|------|------|---------|
| 语义调整 | `config list/get/set` | 从调 server API 改为走 descriptor table |
| 默认行为变更 | `config set` | 默认持久化（之前默认不持久化） |
| Flag 移除 | `config set --persist` | 被 `--runtime-only` 替代 |
| 新 Flag | `config set --runtime-only` | 保留"只改 runtime"的能力 |
| 内部重写 | `provider update` | 内部委托 descriptor table |
| 路径变更 | `key upstream-cb-reset` → `provider upstream-cb-reset` | 旧路径保留为别名 + deprecation 警告 |

### 向后兼容

- `provider update` 的所有 flag 不变，用户无感知
- `config set` 默认行为变更会写入 changelog，属于 breaking change 但符合直觉
- `key upstream-cb-reset` 旧路径保留一个版本

## 测试策略

1. **descriptor table 注册测试**：每个字段的 getter/setter 独立测试
2. **config set 持久化测试**：验证写入后 TOML 文件内容 + runtime 值同步更新
3. **config set --runtime-only 测试**：验证只改 runtime 不写 TOML
4. **只读字段拒绝写入测试**：`config set port 5000` 应报错
5. **provider update 委托测试**：验证 `provider update nvidia --cooldown-sec 30` 和 `config set cooldown_sec 30 nvidia` 产生相同结果
6. **TOML 零值过滤测试**：`provider add` 后 TOML 不含零值字段
7. **deprecated 路径测试**：`key upstream-cb-reset` 仍能工作但输出 warning

## 开放问题

无。
