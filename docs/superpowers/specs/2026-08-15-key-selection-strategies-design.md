# Key Selection Strategies — Design

为 AK-Switch 添加可配置的 API Key 选择策略，替代当前仅依赖 `AvailableKeys()` 轮转偏移的硬编码行为。

## 1. 背景

Issue #317 报告了 API Key 轮转异常：同一个 key 被连续选中直到 429 限流。根因分析确认：

- `Execute()` 使用 `AvailableKeys()`，返回所有可用 key 的列表，按 `counter` 偏移旋转
- 多个并发请求拿到的列表顺序相同，都先尝试第一个 key
- `Next()` 中的 RPM 最低优先逻辑在 `Execute()` 路径上未被使用
- RPM 仅统计成功请求，导致 429 越多的 key 看起来越"空闲"

## 2. 策略定义

新增 `KeySelectionMode` 类型，支持两种模式：

```go
type KeySelectionMode string

const (
    KeySelectionPolling KeySelectionMode = "polling"  // 默认，严格顺序轮转
    KeySelectionRandom  KeySelectionMode = "random"   // 随机选择
)
```

`smart` 模式暂不实现，仅保留代码结构占位。

## 3. 配置

### TOML 配置

在 `[[provider]]` 配置块中新增 `key_selection` 字段：

```toml
[[provider]]
name = "sensenova"
target = "https://api.sensenova.cn/v1"
key_selection = "polling"  # "polling" | "random"（默认 "polling"）
```

### ProviderConfig 结构体

在 `internal/config/config.go` 的 `ProviderConfig` 中新增字段：

```go
KeySelection string `toml:"key_selection,omitempty" default:"polling"`
```

### 字段描述符

在 `internal/config/field_descriptor.go` 的 `ConfigFieldDescriptors` 中新增条目：

- Key: `key_selection`
- Scope: provider
- Type: string
- Default: `"polling"`
- RuntimeEditable: false（启动时配置，重启生效）
- 校验：只能为 `"polling"` 或 `"random"`

## 4. KeyPool 变更

### 新增类型和常量

在 `internal/keypool/keypool.go` 中新增：

```go
type KeySelectionMode string

const (
    KeySelectionPolling KeySelectionMode = "polling"
    KeySelectionRandom  KeySelectionMode = "random"
)
```

### KeyPool 结构体新增字段

```go
type KeyPool struct {
    // ... 现有字段 ...
    selectionMode KeySelectionMode  // 选择策略
    pollingIndex  uint64            // polling 模式下的轮转索引
}
```

### 新增方法

```go
// SetSelectionMode 设置 key 选择策略
func (p *KeyPool) SetSelectionMode(mode KeySelectionMode)

// SelectKey 根据当前策略返回一个可用 key
// 每次只返回一个 key，由调用方处理失败重试
func (p *KeyPool) SelectKey() (int, string, bool)
```

### SelectKey 实现

**polling 模式：**

```go
func (p *KeyPool) SelectKey() (int, string, bool) {
    p.mu.Lock()
    defer p.mu.Unlock()

    n := len(p.keys)
    if n == 0 {
        return -1, "", false
    }

    // 从 pollingIndex 开始，找到第一个可用 key
    start := int(p.pollingIndex % uint64(n))
    for i := 0; i < n; i++ {
        idx := (start + i) % n
        if p.cbs[idx].Allow() {
            p.pollingIndex = uint64(idx+1) % uint64(n)
            return idx, p.keys[idx], true
        }
    }
    return -1, "", false
}
```

**random 模式：**

```go
func (p *KeyPool) SelectKey() (int, string, bool) {
    p.mu.RLock()
    defer p.mu.RUnlock()

    // 收集所有可用 key
    var available []int
    for i, cb := range p.cbs {
        if cb.Allow() {
            available = append(available, i)
        }
    }
    if len(available) == 0 {
        return -1, "", false
    }

    // 随机选一个
    idx := available[rand.Intn(len(available))]
    return idx, p.keys[idx], true
}
```

### 兼容性

- `AvailableKeys()` 和 `Next()` 保留不动，仅 `Execute()` 改为使用 `SelectKey()`
- `NewKeyPool()` 默认 `selectionMode = KeySelectionPolling`

## 5. Execute() 变更

`proxy_executor.go` 中的 `Execute()` 方法从使用 `AvailableKeys()` 改为使用 `SelectKey()`：

```go
func (px *ProxyExecutor) Execute(w http.ResponseWriter, r *http.Request, ps *ProviderState) {
    // ... 前置处理不变 ...

    for round := 0; round < ps.MaxRetries(); round++ {
        if !upCB.Allow() {
            time.Sleep(time.Second)
            continue
        }

        // 使用 SelectKey 替代 AvailableKeys 循环
        idx, key, ok := pool.SelectKey()
        if !ok {
            if !pool.AnyActive() {
                px.writeAllKeysExhausted(w, ps, r.Method, start)
                return
            }
            time.Sleep(time.Second)
            continue
        }

        keyName, _ := pool.Name(idx)
        req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(bodyBytes))
        // ... 后续逻辑不变 ...
    }
}
```

**关键变化：**
- 移除 `pool.AdvanceCounter()` 调用
- 移除 `available := pool.AvailableKeys()` 和 `for _, idx := range available` 内层循环
- 替换为 `idx, key, ok := pool.SelectKey()`
- `SelectKey()` 内部处理熔断器过滤，跳过不可用的 key
- `Release(idx)` 逻辑保持不变

## 6. ProviderState 变更

在 `NewProviderState` 中读取配置并设置 key 选择策略：

```go
func NewProviderState(name string, cfg *config.Config, pool *keypool.KeyPool, dash, keysFile string) *ProviderState {
    // ... 现有逻辑 ...
    pool.SetSelectionMode(keypool.KeySelectionMode(cfg.KeySelection))
    // ...
}
```

## 7. 熔断器交互

`SelectKey()` 内部通过 `cb.Allow()` 过滤不可用的 key：

- **Closed 状态** → 放行
- **Open 且冷却已到期** → 放行
- **Open 且冷却中** → 跳过
- **Permanent** → 跳过

polling 模式：如果选中的 key 刚好不可用，自动跳过并尝试下一个 key。
random 模式：先收集所有可用 key 再随机，避免选中不可用的 key。

## 8. 单元测试

### keypool 测试

- `TestKeySelection_Polling` — 验证顺序轮转：连续调用 N 次，每个 key 恰好被选中一次
- `TestKeySelection_Polling_SkipDisabled` — 验证跳过已禁用的 key
- `TestKeySelection_Polling_WrapAround` — 验证轮转回绕到第一个 key
- `TestKeySelection_Random` — 验证随机选择（统计分布）
- `TestKeySelection_AllDisabled` — 验证所有 key 不可用时返回 false

### proxy_executor 测试

- 更新现有测试以适配新的 `SelectKey()` 调用路径
- 验证 `Release()` 在失败后正确释放

## 9. 边界情况

| 场景 | 行为 |
|------|------|
| 所有 key 都在冷却中 | `SelectKey()` 返回 false，`Execute()` 等待后重试 |
| 所有 key 永久禁用 | `AnyActive()` 返回 false，直接返回 503 |
| 单个 key | polling 和 random 行为一致 |
| 高并发 | polling 持有互斥锁，但选择操作是 O(n) 线性扫描，锁时间短 |
| 热重载 | 新配置的 `key_selection` 在 provider 重建时生效 |

## 10. 不纳入范围

- `smart` 模式暂不实现
- `AvailableKeys()` 和 `Next()` 方法保留不动（校准器仍使用 `Next()`）
- 不做运行时动态切换策略（仅 TOML 配置，重启生效）