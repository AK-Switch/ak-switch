# Key Selection Strategies Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 AK-Switch 添加 `polling` 和 `random` 两种可配置的 API Key 选择策略，替代当前 `Execute()` 使用 `AvailableKeys()` 的硬编码行为

**Architecture:** 在 `internal/keypool/` 中新增 `KeySelectionMode` 类型和 `SelectKey()` 方法，处理策略选择逻辑；在 `internal/config/` 中新增 `key_selection` 配置字段；在 `internal/server/proxy_executor.go` 中将 `Execute()` 从 `AvailableKeys()` 改为 `SelectKey()`

**Tech Stack:** Go 1.26, standard library (sync/atomic, math/rand)

## Global Constraints

- 所有代码使用 Tab 缩进
- 遵循 Effective Go + Go Code Review Comments
- 新增代码附带 table-driven test
- 提交前执行 `make check && make test-unit`
- 错误包装: `fmt.Errorf("函数名: %w", err)`
- 日志: `slog` (结构化日志)

---
## 文件变更清单

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/keypool/keypool.go` | 修改 | 新增 KeySelectionMode 类型、常量、SetSelectionMode、SelectKey 方法 |
| `internal/keypool/keypool_test.go` | 修改 | 新增 SelectKey 的单元测试 |
| `internal/config/config.go` | 修改 | ProviderConfig 新增 KeySelection 字段 |
| `internal/config/field_descriptor.go` | 修改 | 注册 key_selection descriptor |
| `internal/config/field_descriptor_test.go` | 修改 | 新增 key_selection descriptor 测试 |
| `internal/server/router.go` | 修改 | NewProviderState 中设置 key 选择策略 |
| `internal/server/proxy_executor.go` | 修改 | Execute() 改为使用 SelectKey() |
| `internal/server/proxy_executor_test.go` | 修改 | 适配新的 SelectKey 调用路径 |

---

### Task 1: KeySelectionMode 类型和常量定义

**文件:**
- Modify: `internal/keypool/keypool.go:17-50`（KeyPool 结构体附近）

**接口:**
- Consumes: 无
- Produces: `KeySelectionMode` 类型, `KeySelectionPolling`/`KeySelectionRandom` 常量, `KeyPool` 新增 `selectionMode` 和 `pollingIndex` 字段

- [ ] **Step 1: 在 keypool.go 中新增类型和常量**

在 `package keypool` 声明之后、`KeyPool` 结构体之前添加：

```go
// KeySelectionMode defines how a key is selected from the pool.
type KeySelectionMode string

const (
	KeySelectionPolling KeySelectionMode = "polling"
	KeySelectionRandom  KeySelectionMode = "random"
)
```

- [ ] **Step 2: 在 KeyPool 结构体中新增字段**

```go
type KeyPool struct {
	// ... 现有字段不变 ...
	selectionMode KeySelectionMode // 选择策略
	pollingIndex  uint64           // polling 模式下的轮转索引
}
```

- [ ] **Step 3: 新增 SetSelectionMode 方法**

在 `Release()` 方法（第 355 行）附近新增：

```go
// SetSelectionMode sets the key selection strategy for the pool.
// Safe to call before any requests are processed.
func (p *KeyPool) SetSelectionMode(mode KeySelectionMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selectionMode = mode
}
```

- [ ] **Step 4: 初始化默认值**

在 `NewKeyPool()` 函数中，返回结构体时添加默认值：

```go
return &KeyPool{
	// ... 现有字段 ...
	selectionMode: KeySelectionPolling,
	pollingIndex:   0,
}
```

- [ ] **Step 5: 构建验证**

```bash
go build ./cmd/akswitch/
```
Expected: 编译通过，无错误

- [ ] **Step 6: 提交**

```bash
git add internal/keypool/keypool.go
git commit -m "feat: 新增 KeySelectionMode 类型和常量定义"
```

---

### Task 2: Polling 模式 SelectKey 实现

**文件:**
- Modify: `internal/keypool/keypool.go`（在 `SetSelectionMode` 之后新增）

**接口:**
- Consumes: `KeySelectionPolling`, `KeyPool.pollingIndex`, `KeyPool.cbs[].Allow()`
- Produces: `func (p *KeyPool) SelectKey() (int, string, bool)`

- [ ] **Step 1: 实现 SelectKey 方法（polling 模式）**

在 `SetSelectionMode` 方法之后新增：

```go
// SelectKey returns the next available key according to the configured selection strategy.
// Returns index, key value, and ok=false if no key is available.
// The caller must Release(idx) when the key is no longer needed.
func (p *KeyPool) SelectKey() (int, string, bool) {
	switch p.selectionMode {
	case KeySelectionPolling:
		return p.selectKeyPolling()
	default:
		return p.selectKeyPolling()
	}
}

// selectKeyPolling implements the "polling" (round-robin) strategy.
// It starts from the stored pollingIndex and scans forward to find the first available key.
func (p *KeyPool) selectKeyPolling() (int, string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.keys)
	if n == 0 {
		return -1, "", false
	}

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

- [ ] **Step 2: 构建验证**

```bash
go build ./cmd/akswitch/
```
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/keypool/keypool.go
git commit -m "feat: 实现 polling 模式 SelectKey"
```

---

### Task 3: Random 模式 SelectKey 实现

**文件:**
- Modify: `internal/keypool/keypool.go`（在 `selectKeyPolling` 之后新增）

**接口:**
- Consumes: `KeySelectionRandom`, `math/rand`
- Produces: `func (p *KeyPool) selectKeyRandom() (int, string, bool)`

- [ ] **Step 1: 在 SelectKey switch 中添加 random 分支**

```go
func (p *KeyPool) SelectKey() (int, string, bool) {
	switch p.selectionMode {
	case KeySelectionPolling:
		return p.selectKeyPolling()
	case KeySelectionRandom:
		return p.selectKeyRandom()
	default:
		return p.selectKeyPolling()
	}
}
```

- [ ] **Step 2: 实现 selectKeyRandom**

```go
// selectKeyRandom implements the "random" strategy.
// It collects all available keys and picks one uniformly at random.
func (p *KeyPool) selectKeyRandom() (int, string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var available []int
	for i, cb := range p.cbs {
		if cb.Allow() {
			available = append(available, i)
		}
	}
	if len(available) == 0 {
		return -1, "", false
	}
	idx := available[rand.Intn(len(available))]
	return idx, p.keys[idx], true
}
```

- [ ] **Step 3: 构建验证**

```bash
go build ./cmd/akswitch/
```
Expected: 编译通过

- [ ] **Step 4: 提交**

```bash
git add internal/keypool/keypool.go
git commit -m "feat: 实现 random 模式 SelectKey"
```

---

### Task 4: 单元测试 — KeyPool SelectKey

**文件:**
- Modify: `internal/keypool/keypool_test.go`

**接口:**
- 测试 `SelectKey()` 在两种模式下的正确行为

- [ ] **Step 1: 编写 TestSelectKey_Polling** — 验证顺序轮转

```go
func TestSelectKey_Polling(t *testing.T) {
	keys := []string{"key-a", "key-b", "key-c", "key-d"}
	pool := NewKeyPool(keys, nil)
	pool.SetSelectionMode(KeySelectionPolling)

	// 连续调用 4 次，每次应返回不同的 key，顺序为 a,b,c,d
	expected := []string{"key-a", "key-b", "key-c", "key-d"}
	for i, exp := range expected {
		idx, key, ok := pool.SelectKey()
		if !ok {
			t.Fatalf("round %d: SelectKey() returned ok=false, want true", i)
		}
		if key != exp {
			t.Errorf("round %d: got key %q, want %q", i, key, exp)
		}
		pool.Release(idx)
	}

	// 第 5 次应回到 key-a（轮转回绕）
	idx, key, ok := pool.SelectKey()
	if !ok {
		t.Fatal("round 5: SelectKey() returned ok=false, want true")
	}
	if key != "key-a" {
		t.Errorf("round 5: got key %q, want %q", key, "key-a")
	}
	pool.Release(idx)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test -tags=unit -count=1 -short -run TestSelectKey_Polling ./internal/keypool/
```
Expected: FAIL（SelectKey 尚未暴露在测试中）

- [ ] **Step 3: 编写 TestSelectKey_Polling_SkipDisabled** — 验证跳过禁用 key

```go
func TestSelectKey_Polling_SkipDisabled(t *testing.T) {
	keys := []string{"key-a", "key-b", "key-c"}
	pool := NewKeyPool(keys, nil)
	pool.SetSelectionMode(KeySelectionPolling)

	// 禁用 key-b（index 1）
	_ = pool.Disable(1)

	// 轮转应跳过 key-b: a → c → a → c
	expected := []string{"key-a", "key-c", "key-a", "key-c"}
	for i, exp := range expected {
		idx, key, ok := pool.SelectKey()
		if !ok {
			t.Fatalf("round %d: SelectKey() returned ok=false, want true", i)
		}
		if key != exp {
			t.Errorf("round %d: got key %q, want %q", i, key, exp)
		}
		pool.Release(idx)
	}
}
```

- [ ] **Step 4: 编写 TestSelectKey_Polling_AllDisabled** — 验证全部禁用

```go
func TestSelectKey_Polling_AllDisabled(t *testing.T) {
	pool := NewKeyPool([]string{"key-a", "key-b"}, nil)
	pool.SetSelectionMode(KeySelectionPolling)
	_ = pool.Disable(0)
	_ = pool.Disable(1)

	_, _, ok := pool.SelectKey()
	if ok {
		t.Error("SelectKey() on fully disabled pool returned ok=true, want false")
	}
}
```

- [ ] **Step 5: 编写 TestSelectKey_Random** — 验证随机选择

```go
func TestSelectKey_Random(t *testing.T) {
	keys := []string{"key-a", "key-b", "key-c"}
	pool := NewKeyPool(keys, nil)
	pool.SetSelectionMode(KeySelectionRandom)

	// 调用 30 次，统计每个 key 被选中的次数
	counts := make(map[string]int)
	for i := 0; i < 30; i++ {
		idx, key, ok := pool.SelectKey()
		if !ok {
			t.Fatalf("iteration %d: SelectKey() returned ok=false", i)
		}
		counts[key]++
		pool.Release(idx)
	}

	// 每个 key 至少被选中 1 次
	for _, k := range keys {
		if counts[k] == 0 {
			t.Errorf("key %q was never selected in 30 iterations", k)
		}
	}
}
```

- [ ] **Step 6: 运行全部测试**

```bash
go test -tags=unit -count=1 -short -run TestSelectKey ./internal/keypool/
```
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/keypool/keypool_test.go
git commit -m "test: 添加 SelectKey 单元测试（polling + random）"
```

---

### Task 5: TOML 配置字段

**文件:**
- Modify: `internal/config/config.go:17-48`（ProviderConfig 结构体）

**接口:**
- Produces: `ProviderConfig.KeySelection` 字段，`toml:"key_selection,omitempty" default:"polling"`

- [ ] **Step 1: 在 ProviderConfig 中新增字段**

在 `KeyNames` 字段之后（第 31 行）添加：

```go
KeySelection string `toml:"key_selection,omitempty" default:"polling"`
```

- [ ] **Step 2: 在 DefaultConfig 中初始化默认值**

查找 `DefaultConfig()` 函数，添加：

```go
KeySelection: "polling",
```

- [ ] **Step 3: 构建验证**

```bash
go build ./cmd/akswitch/
```
Expected: 编译通过

- [ ] **Step 4: 提交**

```bash
git add internal/config/config.go
git commit -m "feat: 新增 key_selection TOML 配置字段"
```

---

### Task 6: 字段描述符注册

**文件:**
- Modify: `internal/config/field_descriptor.go`（在 ConfigFieldDescriptors 中新增条目）
- Modify: `internal/config/field_descriptor_test.go`（新增测试）

**接口:**
- Consumes: `ProviderConfig.KeySelection`, `FieldScopeProvider`, `FieldTypeString`
- Produces: 注册后的 `key_selection` descriptor

- [ ] **Step 1: 在 field_descriptor.go 中新增 descriptor 条目**

在 `keys_file` descriptor 之后、全局字段之前添加：

```go
{
	Key:             "key_selection",
	DisplayName:     "Key Selection Mode",
	Scope:           FieldScopeProvider,
	TomlPath:        "provider.%s.key_selection",
	Type:            FieldTypeString,
	Default:         "polling",
	RuntimeEditable: false,
	Parse: func(s string) (any, error) {
		v := strings.TrimSpace(strings.ToLower(s))
		switch v {
		case "polling", "random":
			return v, nil
		}
		return nil, fmt.Errorf("invalid key_selection %q, use: polling, random", s)
	},
	Format: func(v any) string { return fmt.Sprintf("%v", v) },
	Persist: func(tc *TomlConfig, provider string, c *Config, value any) {
		if c != nil {
			c.KeySelection = value.(string)
		}
	},
},
```

- [ ] **Step 2: 在 field_descriptor_test.go 中新增测试**

```go
func TestKeySelectionDescriptor(t *testing.T) {
	d := FindField("key_selection")
	if d == nil {
		t.Fatal("FindField('key_selection') returned nil")
	}
	if d.Default != "polling" {
		t.Errorf("default = %q, want 'polling'", d.Default)
	}
	if d.RuntimeEditable {
		t.Error("key_selection should not be runtime editable")
	}

	tests := []struct {
		input string
		want  string
		err   bool
	}{
		{"polling", "polling", false},
		{"random", "random", false},
		{"smart", "", true},
		{"unknown", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		got, err := d.Parse(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("Parse(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
go test -tags=unit -count=1 -short -run TestKeySelectionDescriptor ./internal/config/
```
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/config/field_descriptor.go internal/config/field_descriptor_test.go
git commit -m "feat: 注册 key_selection 字段描述符"
```

---

### Task 7: ProviderState 集成

**文件:**
- Modify: `internal/server/router.go:36-55`（NewProviderState 函数）

**接口:**
- Consumes: `config.KeySelection`, `keypool.KeySelectionMode`, `keypool.SetSelectionMode()`
- Produces: ProviderState 创建时自动设置 key 选择策略

- [ ] **Step 1: 在 NewProviderState 中读取配置并设置**

在 `pool.ConfigureCBs()` 之后、`upCB` 创建之前添加：

```go
// 设置 key 选择策略
pool.SetSelectionMode(keypool.KeySelectionMode(cfg.KeySelection))
```

- [ ] **Step 2: 构建验证**

```bash
go build ./cmd/akswitch/
```
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/server/router.go
git commit -m "feat: 在 ProviderState 初始化时设置 key 选择策略"
```

---

### Task 8: Execute() 改造 — 使用 SelectKey 替代 AvailableKeys

**文件:**
- Modify: `internal/server/proxy_executor.go:78-98`（Execute 方法的核心选择逻辑）

**接口:**
- Consumes: `pool.SelectKey()`, `pool.AnyActive()`
- Produces: 改造后的 Execute 方法，移除 `pool.AdvanceCounter()` 和 `pool.AvailableKeys()` 调用

- [ ] **Step 1: 修改 Execute() 方法**

将第 78-98 行：

```go
pool.AdvanceCounter()
for round := 0; round < ps.MaxRetries(); round++ {
	if !upCB.Allow() {
		slog.Warn("upstream circuit breaker open, backing off", "provider", ps.Name(), "round", round, "max", ps.MaxRetries())
		time.Sleep(time.Second)
		continue
	}

	available := pool.AvailableKeys()
	if len(available) == 0 {
		if !pool.AnyActive() {
			px.writeAllKeysExhausted(w, ps, r.Method, start)
			return
		}
		slog.Warn("no available keys this round, all cooling", "provider", ps.Name(), "round", round, "max", ps.MaxRetries())
		time.Sleep(time.Second)
		continue
	}

	for _, idx := range available {
```

替换为：

```go
for round := 0; round < ps.MaxRetries(); round++ {
	if !upCB.Allow() {
		slog.Warn("upstream circuit breaker open, backing off", "provider", ps.Name(), "round", round, "max", ps.MaxRetries())
		time.Sleep(time.Second)
		continue
	}

	idx, key, ok := pool.SelectKey()
	if !ok {
		if !pool.AnyActive() {
			px.writeAllKeysExhausted(w, ps, r.Method, start)
			return
		}
		slog.Warn("no available keys this round, all cooling", "provider", ps.Name(), "round", round, "max", ps.MaxRetries())
		time.Sleep(time.Second)
		continue
	}
```

- [ ] **Step 2: 更新请求构建部分**

将第 100-110 行中的：

```go
keyName, _ := pool.Name(idx)

req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(bodyBytes))
if err != nil {
	// ...
}
copyHeaders(req.Header, r.Header)
req.Header.Set("Authorization", "Bearer "+pool.Keys()[idx])
```

改为（使用 `key` 变量，避免额外调用 `pool.Keys()`）：

```go
keyName, _ := pool.Name(idx)

req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(bodyBytes))
if err != nil {
	// ...
}
copyHeaders(req.Header, r.Header)
req.Header.Set("Authorization", "Bearer "+key)
```

- [ ] **Step 3: 移除内层 for 循环的结尾花括号**

将内层 `for _, idx := range available {` 的对应结尾花括号删除，确保 `}` 层级正确。

- [ ] **Step 4: 清理未使用的变量**

检查 `available` 变量是否在后续代码中被引用。如果 `AvailableKeys()` 删除后 `available` 不再使用，确保没有编译错误。

- [ ] **Step 5: 构建验证**

```bash
go build ./cmd/akswitch/
```
Expected: 编译通过，无错误

- [ ] **Step 6: 运行单元测试**

```bash
go test -tags=unit -count=1 -short ./internal/server/
```
Expected: PASS（可能需要修复测试中 mock 的期望行为）

- [ ] **Step 7: 提交**

```bash
git add internal/server/proxy_executor.go
git commit -m "refactor: Execute() 改用 SelectKey 替代 AvailableKeys"
```

---

### Task 9: 更新 proxy_executor 测试

**文件:**
- Modify: `internal/server/proxy_executor_test.go`

**接口:**
- 确保现有测试通过新的 SelectKey 路径

- [ ] **Step 1: 运行现有测试，确认哪些需要修改**

```bash
go test -tags=unit -count=1 -short -run TestProxyExecutor ./internal/server/
```
Expected: 记录失败的测试用例

- [ ] **Step 2: 修复失败测试**

现有测试可能使用了 `pool.AvailableKeys()` 或 `pool.AdvanceCounter()` 的间接行为。需要：

1. 检查每个失败的测试，确认其断言逻辑
2. 如果测试创建了 `KeyPool` 并期望特定的 key 顺序，改为适配 `SelectKey()` 的轮转行为
3. 如果测试 mock 了上游响应，确保 `SelectKey()` 返回的 key 与 mock 期望一致

典型修复示例（在测试中设置 selection mode）：

```go
// 在测试的 setup 部分添加
pool.SetSelectionMode(keypool.KeySelectionPolling)
```

- [ ] **Step 3: 运行全部测试确认通过**

```bash
go test -tags=unit -count=1 -short ./internal/server/
```
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/server/proxy_executor_test.go
git commit -m "test: 适配 proxy_executor 测试到 SelectKey 路径"
```

---

### Task 10: 全量验证

**文件:** 无修改

- [ ] **Step 1: 运行 make check**

```bash
make check
```
Expected: lint + vet + fmt 全部通过

- [ ] **Step 2: 运行全量单元测试**

```bash
make test-unit
```
Expected: 全部通过

- [ ] **Step 3: 确认编译产物**

```bash
make build
```
Expected: `bin/akswitch` 编译成功

- [ ] **Step 4: 提交最终清理**

```bash
git add -A
git commit -m "chore: 全量验证通过，key_selection 功能完成"
```

---

## 验证清单

- [ ] `polling` 模式：连续调用 N 次，每个 key 恰好被选中一次，第 N+1 次回到第一个 key
- [ ] `polling` 模式：禁用 key 后自动跳过，轮转索引不受影响
- [ ] `polling` 模式：所有 key 禁用时返回 false
- [ ] `random` 模式：每次随机选择，多次调用后分布均匀
- [ ] `random` 模式：禁用 key 后不会被选中
- [ ] 配置：`key_selection = "polling"` 和 `"random"` 均可解析
- [ ] 配置：无效值报错
- [ ] 配置：`"smart"` 报错
- [ ] 向后兼容：未设置 `key_selection` 时默认使用 `polling`
- [ ] 向后兼容：`AvailableKeys()` 和 `Next()` 方法保留不动
- [ ] 校准器：`lifecycle.go` 中的 `Next()` 调用不受影响