# CLI 配置读取命令显示继承值设计

## 背景

`config get`、`config list`、`config view` 三个 CLI 命令目前读取的是**原始 TOML** 配置值，不经过 `[provider.default]` 段的继承合并。这导致 CLI 显示的值 ≠ 服务器运行时实际使用的值，造成用户困惑。

例如在 `[provider.default]` 设置 `key_selection = "random"` 后，`config get key_selection agnes` 显示空值（该 provider 未显式设置），但服务器启动时实际使用了 `"random"`。

## 目标

让三个 CLI 读取命令显示**继承后的最终配置值**，与服务器运行时行为一致。写入命令（`config set`）行为不变。

## 方案

### 核心改动

新增 `getMergedFieldValue` 函数，入参改为 `map[string]*Config`（`LoadAllTomlProviders` 返回的继承后配置），而非 `*TomlConfig`（原始 TOML）。函数体与 `getFieldValue` 的 switch case 一致，仅数据源不同。

```go
// getMergedFieldValue 从 LoadAllTomlProviders 返回的继承后配置中读取字段值。
// 与 getFieldValue 的区别：getFieldValue 读原始 TOML，本函数读继承 default 段后的值。
func getMergedFieldValue(providers map[string]*config.Config, provider string, fd *config.ConfigFieldDescriptor) (any, error) {
	if p, ok := providers[provider]; ok && p != nil {
		switch fd.Key {
		case "target":
			return p.TargetBase, nil
		case "cooldown_sec":
			return p.CooldownSec, nil
		// ... 其余 case 与 getFieldValue 一致
		}
	}
	return config.ParseDefault(fd)
}
```

### 三个命令改动

| 命令 | 当前 | 改后 |
|---|---|---|
| `config view` | `getFieldValue(tc, name, &fd)` | `getMergedFieldValue(providers, name, &fd)` |
| `config get` | `getFieldValue(tc, p, fd)` | `getMergedFieldValue(providers, p, fd)` |
| `config list` | `getFieldValue(tc, name, &fd)` | `getMergedFieldValue(providers, name, &fd)` |

`config view` 已加载 `providers`（line 154 的 `LoadAllTomlProviders`），直接复用。`config get` 和 `config list` 需新增 `LoadAllTomlProviders` 调用。

### 边界情况

- **`[provider.default]` 不存在时**：`getMergedFieldValue` 行为与 `getFieldValue` 一致，fallback 到 `ParseDefault`
- **provider 不存在时**：`providers` 中无此 key，返回 `ParseDefault`
- **global 字段**（port、log_file）：`config view` 的 global 字段仍用 `getGlobalFieldValue`，不受影响
- **`config set`**：写入路径不变，reload 后生效

### 不做的

- 不改 `config set` 写入路径
- 不引入"来源标注"（如 VSCode 的 `Default`/`User` 标签——YAGNI）
- 不改 `getFieldValue` 的签名（保留原始 TOML 读取路径，供潜在其他用途）

## 文件改动

| 文件 | 改动 |
|------|------|
| `internal/cli/config.go` | 新增 `getMergedFieldValue` 函数；三处读取命令调用改为 `getMergedFieldValue` 并补充 `LoadAllTomlProviders` 调用 |

## 测试

- 现有测试保持不变（`getFieldValue` 未改，不受影响）
- 新增 `getMergedFieldValue` 的单元测试：mock 继承后的 providers map，验证各字段返回正确值
- `config view` 现有测试 `TestConfigViewCmd_Exists` 保持不变