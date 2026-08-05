# 运行时配置全局默认值 + Per-Provider 覆盖

> Issue #241 | 状态: Draft

## 问题

当前运行时配置只支持 per-provider 粒度。要给 N 个 provider 统一改 `max_retries` 需要执行 N 次。缺少全局默认值机制和批量操作能力。

## 方案

在现有配置管道中插入 `[provider.default]` 层，与 struct tag 默认值互补，形成三层覆盖：

```
struct tag defaults          ← 代码里的兜底
    ↓ 被覆盖
[provider.default]           ← 运维配置的全局默认
    ↓ 被覆盖
per-provider [provider.X]    ← 个别 provider 的特例
```

## TOML 层

`config.toml` 新增可选的 `[provider.default]` 段：

```toml
[provider.default]
max_retries = 3
cooldown_sec = 20
log_level = "info"

[sensenova]
target = "https://..."
keys_file = "sensenova.keys"
# max_retries 继承自 default (3)
# cooldown_sec 继承自 default (20)

[claude]
target = "https://..."
keys_file = "claude.keys"
max_retries = 5    # 显式覆盖 default
```

**向后兼容**：无 `[provider.default]` 段时行为与当前完全一致。

**字段范围**：只对 9 个运行时配置字段生效（`max_retries`、`http_timeout_sec`、`cooldown_sec`、`backoff_cap_sec`、`backoff_multiplier`、`cb_reset_sec`、`upstream_cb_threshold`、`health_check_interval_sec`、`log_level`）。`target`、`keys_file`、`admin_token`、`port`、`host` 等非继承字段不参与 merge。

## mergeWithDefaults 函数

新增纯函数，无副作用：

```go
func mergeWithDefaults(base, override *config.Config) *config.Config
```

`base` 来自 `[provider.default]`，`override` 来自 `[provider.X]`。返回新 Config：非零值字段取自 override，其余继承 base。非继承字段（TargetBase、Keys 等）直接覆盖。

**Seam**：唯一的 merge 入口，测试 seam 为传入两个 `*Config` 验证合并结果。

## 加载流程

`LoadAllTomlProviders` 中插入 merge 步骤：

1. 加载 `[provider.default]` → `defaultCfg`（可能为 nil）
2. 对每个 `[provider.X]`：
   - 调用 `mergeDefaults()` 填充 struct tag 默认值
   - 若 `defaultCfg` 存在，调用 `mergeWithDefaults(defaultCfg, providerCfg)`
   - 原有的 Port/Host/LogFile 强制逻辑保持不变

## CLI 层

`config set` 新增 `--all` flag：

| 命令 | 效果 |
|------|------|
| `config set max_retries 3 --all` | 批量设置所有 provider，`--persist` 写入 `[provider.default]` |
| `config list --all` | 显示 default 值 + 每个 provider 的值 |
| `config get max_retries --all` | 显示 default + 每个 provider 的值 |

`--all` 的 resolve 逻辑：遍历 `pm.ProviderNames()` 批量调用 API。

**Persist 策略**：`config set <key> <value> --all --persist` 将值写入 `[provider.default]`。已在 TOML 中显式设置了该字段的 provider 不受影响（保留原有值，仍然覆盖 default）。

## API 层

`handleRuntimeConfigSet` 支持 `provider=all` 参数：

| 请求 | 效果 |
|------|------|
| `POST /api/runtime-config?key=max_retries&value=3` | 当前 provider（现有行为不变） |
| `POST /api/runtime-config?key=max_retries&value=3&provider=all` | 批量设置所有 provider |
| `POST ...&persist=true&provider=all` | 批量设置 + 写入 `[provider.default]` |

`handleRuntimeConfigGet` 支持 `provider=all` 返回全部 provider 的值。

## 热重载

`ReloadConfig` 流程不变。`LoadAllTomlProviders` 内部自动执行 merge，热重载后全局默认值自动生效。

## 测试

- `mergeWithDefaults` 单元测试：继承、覆盖、无 default 时行为不变
- `config set --all` CLI 集成测试
- `config list --all` 输出断言
- 热重载后全局默认生效的集成测试
- 迁移后所有现有测试通过

## 不做的事

- 不改变现有 per-provider 路径的行为
- 不改变 `config.toml` 现有字段名或格式
- 不引入 `[global]` 段（用 `[provider.default]` 替代）
- 不做持久化策略变更（现有 `--persist` 语义不变）
