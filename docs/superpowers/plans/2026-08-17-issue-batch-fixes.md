# Issue 批量修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 解决 AK-Switch 7 个 open issue，覆盖配置清理、端口统一、熔断器恢复、流式截断处理等。

**Architecture:** 分两批执行。Batch 1 是 4 个独立小修复（不同模块，可并行 PR），Batch 2 是 3 个中等规模修复（熔断器/流式截断，需谨慎）。每个 task TDD + 独立 commit。

**Tech Stack:** Go 1.26 + Cobra CLI + BurntSushi/toml + slog + Prometheus metrics

**Spec:** 本计划的设计决策来自 2026-08-16/17 的 brainstorming 会话（Issue #299/#300 规划讨论）。关键决策：
- #302 直接删 disable_thinking，不做兼容层（单用户，toml.Unmarshal 非 strict 会静默忽略未知字段）
- #308 端口统一到 4000（端口注册表 `~/.agents/port-registry.yaml` 确认）
- #335 所有 key 冷却时返回 HTTP 429（不是 503，让客户端识别为限流）
- #323 删除 `--by-name` flag，位置参数自动判断索引（数字）或名称（非数字）
- #324 验证 `persistFieldToToml` 对 log_level 的持久化路径
- #344 撤销 commit 8b20282，恢复 `StatePermanent`；新增周期探针自动恢复 disabled key
- #340 采纳 OpenRouter 方案：检测终端标记缺失 + 注入 `finish_reason:"error"`/`event:error`；新增 `buffer_mode` 可配置项（默认 false）

## Global Constraints

- Go 1.26 必须安装
- Tab 缩进（gofmt 强制）
- 错误包装：`fmt.Errorf("函数名: %w", err)`
- 日志：`slog` 结构化日志
- Table-driven tests 优先
- 提交前执行 `make check`（lint+vet+fmt）+ `make test-unit`
- 构建标签：单元测试用 `-tags=unit`
- 每个 commit 只做一件事，改完立即提交
- `ProviderState` 字段私有，通过 getter/setter 访问

---

## File Structure

| 文件 | 责任 | 涉及 Task |
|------|------|-----------|
| `internal/config/config.go` | ProviderConfig/TomlConfig/RuntimeConfig 结构体 + DeepCopy + syncRuntimeConfig | 1, 2, 5, 7 |
| `internal/config/config_toml.go` | TomlConfig Port 字段 | 2 |
| `internal/config/config_loader.go` | 配置加载 + migrateDisableThinking 调用 | 1 |
| `internal/circuitbreaker/key.go` | KeyCircuitBreaker RecordFailure/Allow | 6 |
| `internal/server/proxy_executor.go` | 代理执行 + handleRateLimited + streamSSE | 3, 7 |
| `internal/server/lifecycle.go` | StartupKeyProbe + ActiveHealthCheck | 6 |
| `internal/server/router.go` | StartBackgroundTasks | 6 |
| `internal/cli/key.go` | key 子命令 + addKeyIndexFlags + resolveKeyIndex | 4 |
| `internal/cli/config.go` | config set/view + persistFieldToToml | 2, 4, 5 |
| `internal/cli/root.go` | adminPort 常量 | 2 |

---

## Batch 1: 独立小修复（可并行 PR）

### Task 1: 删除废弃的 disable_thinking 字段 (#302)

**Files:**
- Modify: `internal/config/config.go:30` — 删 `DisableThinking` 字段
- Modify: `internal/config/config.go:290-296` — 删 `migrateDisableThinking()` 函数
- Modify: `internal/config/config_loader.go:110` — 删 `p.migrateDisableThinking()` 调用
- Modify: `internal/cli/config.go:265,353,590-591` — 删字段列表项 + getFieldValue case
- Modify: `internal/cli/provider.go:352` — 删 flag 映射 `"disable-thinking": "disable_thinking"`
- Test: `internal/config/config_test.go`, `internal/config/config_defaults_test.go`, `internal/cli/config_cmd_test.go`, `internal/cli/provider_cmd_test.go`

**Interfaces:**
- Produces: `ProviderConfig` 不再有 `DisableThinking` 字段；C2 反射化后 descriptor 自动不再生成 `disable_thinking` 条目（struct tag 删了就消失）

**Why no compatibility layer:** 单用户使用；`toml.Unmarshal` 非 strict 模式会静默忽略 config.toml 里残留的 `disable_thinking = true`，不会报错。

- [ ] **Step 1: 写失败测试 — 确认 DisableThinking 字段已删除**

`internal/config/config_test.go` 中找到引用 `DisableThinking` 的测试（如 line 522, 639, 687），改为断言该字段不存在。最简方式：删除这些断言行。

- [ ] **Step 2: 删除 ProviderConfig.DisableThinking 字段**

`internal/config/config.go:30`，删除整行：
```go
DisableThinking        bool    `toml:"disable_thinking,omitempty" field:"disable_thinking,display:Disable Thinking,scope:provider,default:false"`                // Deprecated: use thinking_mode
```

- [ ] **Step 3: 删除 migrateDisableThinking 函数及其调用**

`internal/config/config.go:290-296` 删除整个 `migrateDisableThinking` 函数。
`internal/config/config_loader.go:110` 删除 `p.migrateDisableThinking()` 行。

- [ ] **Step 4: 删除 CLI 侧引用**

`internal/cli/config.go`：
- line 265, 353：从字段列表中删除 `disable_thinking` 项
- line 590-591：删除 `case "disable_thinking": return p.DisableThinking, nil`

`internal/cli/provider.go:352`：删除 `"disable-thinking": "disable_thinking",` 映射。

- [ ] **Step 5: 清理测试文件中的 DisableThinking 引用**

搜索所有测试中的 `DisableThinking` 引用，删除相关断言：
```bash
grep -rn "DisableThinking\|disable_thinking\|disable-thinking" --include="*_test.go" internal/
```
逐文件清理。注意 `config_defaults_test.go:240,284,362-377` 的继承测试可能需要整个删除或改写为 thinking_mode 的等价测试。

- [ ] **Step 6: 运行测试验证**

```bash
go test -tags=unit -count=1 -short ./internal/config/ ./internal/cli/
make check
```
Expected: PASS，无编译错误。

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_loader.go internal/cli/config.go internal/cli/provider.go internal/config/config_test.go internal/config/config_defaults_test.go internal/cli/config_cmd_test.go internal/cli/provider_cmd_test.go
git commit -m "refactor: 删除废弃的 disable_thinking 字段及相关代码 (#302)"
```

### Task 2: 端口统一 8080 → 4000 (#308)

**Files:**
- Modify: `internal/config/config.go:37` — `default:"8080"` → `default:"4000"`
- Modify: `internal/config/config.go:95` — `Port: 8080` → `Port: 4000`
- Modify: `internal/config/config_toml.go:13` — `default:8080` → `default:4000`
- Modify: `internal/cli/root.go:11` — `adminPort = 8080` → `adminPort = 4000`
- Modify: `internal/cli/config.go:94` — `Port: 8080` → `Port: 4000`
- Test: `internal/config/config_test.go`, `internal/config/config_defaults_test.go`, `internal/cli/config_cmd_test.go`, `internal/cli/provider_cmd_test.go`, `internal/cli/status_cmd_test.go`
- Modify: `test/load/README.md` — 端口示例 8080 → 4000

**Interfaces:**
- Produces: 全局默认端口从 8080 改为 4000。用户 config.toml 里已有的 `port = 4000` 不受影响（显式配置优先于默认值）。

**Why 4000:** 端口注册表 `~/.agents/port-registry.yaml` 明确 `ak-switch → proxy → 4000`。用户实际 config.toml 也用 4000。代码默认值与实际使用脱节，本次统一。

- [ ] **Step 1: 写失败测试 — 断言默认端口为 4000**

`internal/config/config_test.go`，找到 `cfg.Port != 8080` 之类断言，改为：
```go
if cfg.Port != 4000 {
    t.Errorf("Port = %d, want default 4000", cfg.Port)
}
```
对 `config_defaults_test.go:119` `port = 8080` 测试 toml 内容，改为 `port = 4000`，对应断言也改。

- [ ] **Step 2: 运行测试验证失败**

```bash
go test -tags=unit -count=1 -short -run TestLoad ./internal/config/
```
Expected: FAIL（默认值还是 8080）。

- [ ] **Step 3: 修改 config.go 默认值**

`internal/config/config.go:37`：
```go
Port                   int      `toml:"port" default:"4000"`
```
`internal/config/config.go:95`（DefaultTomlConfig 或类似）：
```go
Port:                   4000,
```

- [ ] **Step 4: 修改 config_toml.go 的 field tag**

`internal/config/config_toml.go:13`：
```go
Port            int                `toml:"port" field:"port,display:Port,scope:global,default:4000,readonly"`
```

- [ ] **Step 5: 修改 CLI 侧 adminPort 常量和默认值**

`internal/cli/root.go:11`：
```go
adminPort = 4000
```
`internal/cli/config.go:94`：
```go
Port: 4000,
```

- [ ] **Step 6: 更新所有测试中的 8080 断言**

```bash
grep -rn "8080" --include="*_test.go" internal/
```
逐个改为 4000。注意 `status_cmd_test.go:117-118` 的 `localhost:8080` URL 也要改。

- [ ] **Step 7: 更新文档**

`test/load/README.md` 中 `http://localhost:8080` → `http://localhost:4000`。

- [ ] **Step 8: 运行测试验证通过**

```bash
go test -tags=unit -count=1 -short ./internal/config/ ./internal/cli/
make check
```
Expected: PASS。

- [ ] **Step 9: Commit**

```bash
git add internal/config/config.go internal/config/config_toml.go internal/cli/root.go internal/cli/config.go internal/config/config_test.go internal/config/config_defaults_test.go internal/cli/config_cmd_test.go internal/cli/provider_cmd_test.go internal/cli/status_cmd_test.go test/load/README.md
git commit -m "refactor: 默认端口从 8080 统一为 4000 (#308)"
```

### Task 3: 所有 key 冷却时返回 429 (#335)

**Files:**
- Modify: `internal/server/proxy_executor.go:86-95` — `no available keys` 分支改为返回 429
- Test: `internal/server/proxy_executor_test.go`（若无则新建）

**Interfaces:**
- Produces: 当 provider 的所有 key 都在冷却中（`AnyActive()==true` 但 `SelectKey()` 返回 false）时，立即返回 HTTP 429，不再 `sleep 1s → continue` 死循环。

**Why 429 not 503:** 429 是标准限流语义，客户端（Claude Code）会识别为"限流"并触发自动重试（最多 10 次）。503 偏向"服务不可用"，客户端倾向"稍后重试"而非"切换"。这也与 #340 的策略 1 一致（流开始前返回 429 触发客户端重试）。

**当前行为（要改的）：**
```go
idx, key, ok := pool.SelectKey()
if !ok {
    if !pool.AnyActive() {
        px.writeAllKeysExhausted(w, ps, r.Method, start)  // 503，保留
        return
    }
    slog.Warn("no available keys this round, all cooling", ...)
    time.Sleep(time.Second)   // ← 死循环根源
    continue
}
```

- [ ] **Step 1: 写失败测试 — 所有 key 冷却时返回 429**

`internal/server/proxy_executor_test.go`（若文件不存在则创建，带 `//go:build unit` 标签）：

```go
//go:build unit

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAllKeysCooling_Returns429 验证：所有 key 都在冷却中时，
// 代理立即返回 429，不再 sleep 循环。
func TestAllKeysCooling_Returns429(t *testing.T) {
	// 构造一个所有 key 都在冷却的 pool
	// 调用 ProxyExecutor 处理请求
	// 断言响应状态码为 429
	// 断言响应时间 < 2s（证明没有 sleep 循环）
	t.Skip("具体实现依赖 ProxyExecutor 的测试构造方式，实现时补全")
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
go test -tags=unit -count=1 -short -run TestAllKeysCooling ./internal/server/
```
Expected: FAIL 或 Skip。

- [ ] **Step 3: 修改 proxy_executor.go 的 no available keys 分支**

`internal/server/proxy_executor.go:86-95`，将"all cooling"分支改为返回 429：

```go
idx, key, ok := pool.SelectKey()
if !ok {
    if !pool.AnyActive() {
        px.writeAllKeysExhausted(w, ps, r.Method, start)
        return
    }
    // 所有 key 都在冷却中：立即返回 429，让客户端识别为限流并重试
    slog.Warn("all keys cooling, returning 429", "provider", ps.Name(), "round", round, "max", ps.MaxRetries())
    writeProxyError(w, http.StatusTooManyRequests, ErrorAllKeysCooling,
        fmt.Sprintf("%s 所有 API Key 均在冷却中，请稍后重试", ps.Name()))
    px.recordProxyMetrics(r.Method, "429", "", start)
    return
}
```

- [ ] **Step 4: 新增 ErrorAllKeysCooling 常量**

在 `internal/server/proxy_executor.go` 的错误常量区（搜 `ErrorUpstreamError` / `ErrorAllKeysInvalid` 附近）添加：
```go
ErrorAllKeysCooling = "all_keys_cooling"
```

- [ ] **Step 5: 补全测试实现**

根据 ProxyExecutor 的实际构造方式补全 Step 1 的测试。参考已有测试（如 `TestProxyExecutor_*` 或 `writeAllKeysExhausted` 相关测试）的构造方式。

- [ ] **Step 6: 运行测试验证通过**

```bash
go test -tags=unit -count=1 -short -run TestAllKeysCooling ./internal/server/
go test -tags=unit -count=1 -short ./internal/server/
make check
```
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add internal/server/proxy_executor.go internal/server/proxy_executor_test.go
git commit -m "fix: 所有 key 冷却时返回 429 而非 sleep 循环 (#335)"
```

### Task 4: 删除 --by-name flag，自动判断索引/名称 (#323)

**Files:**
- Modify: `internal/cli/key.go:78-82` — 改造 `addKeyIndexFlags`（删除 `--by-name` flag）
- Modify: `internal/cli/key.go:804+` — 改造 `resolveKeyIndex` 为自动判断
- Modify: 6 个子命令的 Long 文本和 Example（update/cooldown/remove/disable/enable/restore）
- Test: `internal/cli/key_import_test.go`, `internal/cli/provider_cmd_test.go`

**Interfaces:**
- Produces: key 子命令的位置参数 `<index>` 改为 `<key-id>`，自动判断：能 `strconv.Atoi` → 索引；不能 → 名称查找。
- 删除 `--by-name` flag（破坏性变更，单用户可接受）。

**设计依据：** Docker `container rm <container>` 同样模式——位置参数接受 ID 或 name，自动判断。我们的 key 索引永远是数字，名称永远是非数字字符串，无歧义。

- [ ] **Step 1: 写失败测试 — 自动判断索引和名称**

`internal/cli/key_import_test.go`，找到 `--by-name` 测试（如 line 991, 1075, 1167），改为不带 flag：

```go
// 原来带 --by-name
args: []string{"akswitch", "key", "update", provider, "key-b", "sk-update-2-by-name", "--by-name"},
// 改为自动判断（"key-b" 不是数字 → 按名称）
args: []string{"akswitch", "key", "update", provider, "key-b", "sk-update-2-by-name"},
```

对索引场景保持原样（数字仍按索引）：
```go
args: []string{"akswitch", "key", "update", provider, "0", "sk-xxx"},
```

- [ ] **Step 2: 运行测试验证失败**

```bash
go test -tags=unit -count=1 -short -run TestKeyUpdate ./internal/cli/
```
Expected: FAIL（`--by-name` flag 已删，但代码还在读 flag）。

- [ ] **Step 3: 改造 resolveKeyIndex 为自动判断**

`internal/cli/key.go` 找到 `resolveKeyIndex` 函数（约 line 804）。原逻辑读 `--by-name` flag，改为：

```go
// resolveKeyIndex 解析 key-id：数字 → 索引；非数字 → 按名称查找。
func resolveKeyIndex(cmd *cobra.Command, store *keypool.KeyStore, keyID string) (int, error) {
    if idx, err := strconv.Atoi(keyID); err == nil {
        // 数字 → 按索引
        if idx < 0 || idx >= len(store.Keys) {
            return 0, fmt.Errorf("key index %d out of range (0-%d)", idx, len(store.Keys)-1)
        }
        return idx, nil
    }
    // 非数字 → 按名称查找
    return findKeyIndexByName(store, keyID)
}
```

注意：原 `resolveKeyIndex` 可能签名不同（带 provider 参数加载 store）。保留原有 store 加载逻辑，只改判断部分。

- [ ] **Step 4: 删除 addKeyIndexFlags 中的 --by-name flag**

`internal/cli/key.go:78-82`：
```go
// addKeyIndexFlags 注册 key-index 命令的标准 flag。
// --by-name 已删除：位置参数自动判断索引（数字）或名称（非数字）。
func addKeyIndexFlags(cmd *cobra.Command) {
    // 保留 --name flag（用于重命名），删除 --by-name
}
```
如果 `addKeyIndexFlags` 只注册 `--by-name`，整个函数可删，调用点（line 102-117）一并删除。

- [ ] **Step 5: 改造 6 个子命令的 RunE 逻辑**

每个使用 `cmd.Flags().GetBool("by-name")` 的命令（update/cooldown/remove/disable/enable/restore）改为直接调用 `resolveKeyIndex(cmd, store, args[1])`，不再分支。

`keyUpdateCmd` (line 376-380) 示例改造：
```go
// 原来
if byName, _ := cmd.Flags().GetBool("by-name"); byName {
    idx, err = findKeyIndexByName(store, args[1])
} else {
    idx, err = strconv.Atoi(args[1])
}
// 改为
idx, err = resolveKeyIndex(cmd, store, args[1])
```

- [ ] **Step 6: 更新 Long 文本和 Example**

6 个命令的 `Use` 字段 `<index>` → `<key-id>`，删除 `--by-name` 相关 Example。例如 `keyUpdateCmd` (line 352, 360-363)：
```go
Use:   "update <provider> <key-id> [key]",
Long: `Replace an existing API key at the specified index or name with a new key value,
or rename it without changing the value.

<key-id> can be a numeric index (e.g. 0) or a key name (e.g. d1-2).
The system auto-detects: numbers are treated as indexes, non-numeric strings as names.

Examples:
  akswitch key update sensenova 0 sk-xxxxxxxxxxxxxxxx
  akswitch key update sensenova 0 --name d1-2
  akswitch key update sensenova d1-2 sk-xxxxxxxxxxxxxxxx`,
```

- [ ] **Step 7: 清理测试中的 --by-name 断言**

```bash
grep -rn "by-name\|byName" --include="*_test.go" internal/cli/
```
`provider_cmd_test.go:45,97,133,140` 的 `--by-name` flag 存在性断言要删除或改为验证自动判断。

- [ ] **Step 8: 运行测试验证通过**

```bash
go test -tags=unit -count=1 -short ./internal/cli/
make check
```
Expected: PASS。

- [ ] **Step 9: Commit**

```bash
git add internal/cli/key.go internal/cli/key_import_test.go internal/cli/provider_cmd_test.go
git commit -m "refactor: 删除 --by-name flag，位置参数自动判断索引/名称 (#323)"
```

---

## Batch 2: 中等规模修复

### Task 5: 验证并修复 log_level 持久化 (#324)

**Files:**
- Investigate: `internal/cli/config.go:436-452` — `configSetCmd` RunE 流程
- Investigate: `internal/cli/config.go:506-540` — `persistFieldToToml` 实现
- Investigate: `internal/server/` — admin API runtime config endpoint（是否只改内存）
- Modify: 视调查结果而定
- Test: `internal/cli/config_cmd_test.go`

**Interfaces:**
- Produces: `akswitch config set <provider> log_level <value>` 应同时改运行时内存 + 写回 config.toml，重启后保持。

**已知事实：**
1. `configSetCmd` (line 436-452)：先 `applyRuntimeField`（改内存），再 `if !runtimeOnly` 调 `persistFieldToToml`（写 TOML）。默认 `runtimeOnly=false`，应该会写 TOML。
2. 用户 config.toml 第 34 行 `[provider.sensenova] log_level = 'debug'` 持续存在，用户说改不回来。
3. 用户日志显示 zhuansj2 key 在循环——但那是 #344 的问题，与 log_level 持久化无关。

**待验证的假设：**
- A. `config set sensenova log_level info` 实际没写 TOML（persistFieldToToml 有 bug）
- B. 用户用了 admin API `/api/runtime-config`（只改内存，不写 TOML）
- C. `log_level` 的 Persist closure（反射生成的 makePersist）写错了字段

- [ ] **Step 1: 写验证测试 — config set log_level 持久化到 TOML**

`internal/cli/config_cmd_test.go`，添加测试：
```go
func TestConfigSet_LogLevel_PersistsToToml(t *testing.T) {
    // 用临时 config.toml
    // 调用 persistFieldToToml("test", logLevelDescriptor, "info")
    // 重新 LoadTomlConfig
    // 断言 tc.Provider["test"].LogLevel == "info"
}
```

- [ ] **Step 2: 运行测试，观察是否通过**

```bash
go test -tags=unit -count=1 -short -run TestConfigSet_LogLevel ./internal/cli/
```
- 若 PASS：persistFieldToToml 路径正常，问题在别处（假设 B：admin API）。转 Step 3。
- 若 FAIL：persistFieldToToml 有 bug，转 Step 4。

- [ ] **Step 3: 检查 admin API runtime config endpoint**

搜索 `internal/server/` 中的 runtime config handler：
```bash
grep -rn "runtime-config\|RuntimeConfig\|setRuntimeConfig" internal/server/
```
确认 admin API 改 log_level 时是否调用了持久化逻辑。若只改内存不写 TOML，则：
- 在 admin API handler 中追加 `persistFieldToToml` 调用，或
- 在文档/AGENTS.md 中明确"admin API 改动不持久，需用 CLI config set 持久化"

- [ ] **Step 4: 修复 persistFieldToToml 的 bug（若 Step 2 FAIL）**

根据测试失败的具体原因修复。常见可能：
- `log_level` 的 Persist closure 写错 struct field（反射 makePersist 的 FieldByName 拼写）
- `tc.Provider[provider]` 为 nil 时未初始化（line 524-528 已有初始化，确认无遗漏）

- [ ] **Step 5: 补充测试覆盖**

- 索引/名称场景（验证 #323 的自动判断）
- nil provider 的边界
- log_level 之外的其他 runtime-editable 字段（cooldown_sec 等）也测试持久化

- [ ] **Step 6: 运行测试验证通过**

```bash
go test -tags=unit -count=1 -short ./internal/cli/ ./internal/server/
make check
```
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add internal/cli/config.go internal/cli/config_cmd_test.go internal/server/*.go
git commit -m "fix: log_level config set 持久化到 TOML (#324)"
```

### Task 6: 恢复熔断器永久禁用 + 周期探针自动恢复 (#344)

**Files:**
- Modify: `internal/circuitbreaker/key.go:60-65` — 恢复 `StatePermanent`（撤销 8b20282）
- Modify: `internal/circuitbreaker/key_test.go` — 适配测试
- Modify: `internal/server/lifecycle.go:238-282` — 把 `StartupKeyProbe` 改造为周期任务 `PeriodicKeyProbe`
- Modify: `internal/server/router.go` — `StartBackgroundTasks` 启动周期探针
- Test: `internal/circuitbreaker/key_test.go`, `internal/server/lifecycle_test.go`

**Interfaces:**
- Produces:
  - `KeyCircuitBreaker.RecordFailure()` 达到 backoffCap 时设为 `StatePermanent`（不再自动恢复）
  - 新增 `PeriodicKeyProbe(pool, target, interval, stop)` 周期探测 Permanent key，200 → Enable

**设计依据：**
- 日志证据：zhuansj2 持续 6 小时循环 429（297 次），8b20282 的"长冷却 + attempt=0 重置"导致配额耗尽的 key 永远在循环。
- New-API 验证：`ChannelStatusAutoDisabled` 持久化 + `PassiveRecovery` 定时测试任务自动恢复——业界成熟做法。
- 探针用 GET /models（复用 StartupKeyProbe 的逻辑），200 → Enable 给机会，429 → 保持禁用，401/403 → 永久死了。

**撤销 8b20282 的关键改动：**

`internal/circuitbreaker/key.go:60-65`，当前：
```go
if raw >= k.backoffCap {
    k.state = Open
    k.cooldownUntil = time.Now().Add(k.backoffCap)
    k.attempt = 0
    return k.backoffCap
}
```
改为（恢复 8b20282 之前的逻辑）：
```go
if raw >= k.backoffCap {
    k.state = Permanent
    k.trippedReason = "quota_exhausted"
    return k.backoffCap
}
```
**删除 `k.attempt = 0`**——这是死循环的根源。

- [ ] **Step 1: 写失败测试 — 达到 cap 后变为 Permanent**

`internal/circuitbreaker/key_test.go`，找到 `TestReachesCap` 或类似测试（8b20282 提到适配过），改为：
```go
func TestRecordFailure_ReachesCap_Permanent(t *testing.T) {
    cb := NewKeyCircuitBreaker(60*time.Second, 120*time.Second, 2.0)
    // 连续 RecordFailure 直到达到 cap
    for i := 0; i < 10; i++ {
        cb.RecordFailure()
    }
    if cb.State() != Permanent {
        t.Errorf("State = %v, want Permanent after reaching cap", cb.State())
    }
    // Permanent 后 Allow() 应返回 false
    if cb.Allow() {
        t.Error("Allow() should return false when Permanent")
    }
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
go test -tags=unit -count=1 -short -run TestRecordFailure_ReachesCap ./internal/circuitbreaker/
```
Expected: FAIL（当前实现是 Open + 长冷却）。

- [ ] **Step 3: 修改 RecordFailure 恢复 Permanent**

`internal/circuitbreaker/key.go:60-65` 按上述改为 `StatePermanent` + `trippedReason`，删除 `k.attempt = 0`。

- [ ] **Step 4: 运行测试验证通过**

```bash
go test -tags=unit -count=1 -short ./internal/circuitbreaker/
```
Expected: PASS。注意可能有其他测试（如 TestCB_QuotaExhausted）需要适配。

- [ ] **Step 5: 写周期探针的测试**

`internal/server/lifecycle_test.go`，添加：
```go
func TestPeriodicKeyProbe_ReenablesRecoveredKey(t *testing.T) {
    // 构造一个有 Permanent key 的 pool
    // 启动 PeriodicKeyProbe（短 interval，如 100ms）
    // mock 上游返回 200
    // 等待探针执行
    // 断言 key 被 Enable
}
```

- [ ] **Step 6: 实现 PeriodicKeyProbe**

`internal/server/lifecycle.go`，基于 `StartupKeyProbe` (line 238-282) 改造为周期任务：

```go
// PeriodicKeyProbe 周期探测 Permanent/Disabled 的 key，配额恢复则 Enable。
// 探针发 GET /models：200 → Enable；429 → 保持禁用；401/403 → 保持禁用（key 失效）。
func PeriodicKeyProbe(pool *keypool.KeyPool, target string, interval time.Duration, stop <-chan struct{}) {
    client := &http.Client{Timeout: 3 * time.Second}
    probeURL := strings.TrimRight(target, "/") + "/models"
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-stop:
            return
        case <-ticker.C:
            for i := 0; i < pool.Len(); i++ {
                if pool.CBState(i) != circuitbreaker.Permanent {
                    continue
                }
                keyName, _ := pool.Name(i)
                req, err := http.NewRequest("GET", probeURL, nil)
                if err != nil {
                    continue
                }
                key := pool.Keys()[i]
                req.Header.Set("Authorization", "Bearer "+key)
                resp, err := client.Do(req)
                if err != nil {
                    slog.Debug("periodic key probe network error", "key_name", keyName, "error", err)
                    continue
                }
                if resp.StatusCode == http.StatusOK {
                    pool.Reset(i)  // 或 Enable(i)
                    slog.Info("key re-enabled by periodic probe", "key_name", keyName)
                }
                resp.Body.Close()
            }
        }
    }
}
```

注意：需要确认 `KeyPool` 是否有 `CBState(i)` 和 `Reset(i)`/`Enable(i)` 方法。`Reset` 在 8b20282 中已添加（`KeyCircuitBreaker.Reset`），`KeyPool.Enable` 也已添加。可能需要新增 `KeyPool.CBState(i)` 或用 `pool.IsDisabled(i)`。

- [ ] **Step 7: 在 StartBackgroundTasks 启动周期探针**

`internal/server/router.go` 的 `StartBackgroundTasks` (line 253)，为每个 provider 启动探针：
```go
for _, ps := range pr.providers {
    pool := ps.Pool()
    target := ps.Config().TargetBase
    go PeriodicKeyProbe(pool, target, 5*time.Minute, pr.stop)
}
```
探针间隔 5 分钟（既不频繁打上游，配额恢复后最多 5 分钟自动启用）。

- [ ] **Step 8: 运行全部测试**

```bash
go test -tags=unit -count=1 -short ./internal/circuitbreaker/ ./internal/server/
make check
make test-unit
```
Expected: PASS。

- [ ] **Step 9: Commit**

```bash
git add internal/circuitbreaker/key.go internal/circuitbreaker/key_test.go internal/server/lifecycle.go internal/server/lifecycle_test.go internal/server/router.go
git commit -m "fix: 恢复熔断器永久禁用 + 周期探针自动恢复配额 (#344)"
```

### Task 7: 流式截断检测 + 错误事件注入 + buffer_mode 可配置 (#340)

**Files:**
- Modify: `internal/server/proxy_executor.go:379-436` — `streamSSEAndEstimateTokens` 增加截断检测 + 错误事件注入
- Modify: `internal/server/proxy_executor.go:277-310` — `handleSuccess` 根据 buffer_mode 选择流式或缓冲
- Modify: `internal/config/config.go` — 新增 `BufferMode bool` 字段（provider-scoped，default:false）
- Modify: `internal/config/config_toml.go` — 若 BufferMode 是全局字段则加 tag
- Test: `internal/server/proxy_executor_test.go`

**Interfaces:**
- Produces:
  - `streamSSEAndEstimateTokens` 追加终端标记检测，截断时注入 `event: error` + `event: message_stop`（Anthropic 格式）
  - 新增 `buffer_mode` 配置项，`true` 时先缓冲完整响应再转发（失败可换 key 重试）

**设计依据：**
- OpenRouter 业界标准：mid-stream 错误以 SSE 事件注入，`finish_reason: "error"`
- OmniRoute 修复（#7699）：只发 `event: error` 不发 `message_stop` 客户端仍认为流不完整，必须追加 `message_stop`
- Claude Code 行为调查：流开始后的错误不会自动重试，但至少显示 `Server error mid-response` 而非静默
- buffer_mode 可配置：牺牲流式实时性换取完整性 + 换 key 重试能力

- [ ] **Step 1: 写失败测试 — 截断时注入 error 事件**

`internal/server/proxy_executor_test.go`，添加：
```go
func TestStreamSSE_Truncated_InjectsErrorEvent(t *testing.T) {
    // 构造一个提前 EOF 的上游响应（无 message_stop）
    // 调用 streamSSEAndEstimateTokens
    // 断言输出包含 `event: error` 和 `event: message_stop`
}

func TestStreamSSE_NormalCompletion_NoErrorEvent(t *testing.T) {
    // 构造正常结束的 SSE 流（有 message_stop）
    // 断言输出不包含 `event: error`
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
go test -tags=unit -count=1 -short -run TestStreamSSE ./internal/server/
```
Expected: FAIL（当前无截断检测）。

- [ ] **Step 3: 改造 streamSSEAndEstimateTokens 增加截断检测**

`internal/server/proxy_executor.go:379-436`，在 scanner 循环中追踪终端标记：

```go
func streamSSEAndEstimateTokens(w http.ResponseWriter, resp *http.Response, bodyBytes []byte, model string) (int, int, int64) {
    defer func() { _ = resp.Body.Close() }()

    var outputBuf strings.Builder
    var respBodySize int64
    var apiOutputTokens int
    var receivedTerminalEvent bool  // 新增：是否收到 message_stop/[DONE]

    scanner := bufio.NewScanner(resp.Body)
    scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
    f, canFlush := w.(http.Flusher)

    for scanner.Scan() {
        line := scanner.Text()
        // 检测终端标记
        if strings.Contains(line, "message_stop") || strings.Contains(line, "[DONE]") {
            receivedTerminalEvent = true
        }
        // ... 原有 write + parse 逻辑 ...
    }

    // 截断检测：流结束但未收到终端标记，且已转发过内容
    if !receivedTerminalEvent && respBodySize > 0 {
        injectTruncationError(w, f, canFlush)
        slog.Warn("stream truncated",
            "model", model,
            "resp_body_size", respBodySize,
            "received_terminal", false,
        )
    }
    // ... 原有 token 返回逻辑 ...
}
```

- [ ] **Step 4: 实现 injectTruncationError 函数**

`internal/server/proxy_executor.go` 新增：
```go
// injectTruncationError 在截断的 SSE 流末尾注入错误事件 + 终止标记。
// Anthropic 格式：event: error + event: message_stop
func injectTruncationError(w http.ResponseWriter, f http.Flusher, canFlush bool) {
    // Anthropic Messages 格式
    errorEvent := "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"upstream stream truncated, please retry\"}}\n\n"
    stopEvent := "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
    w.Write([]byte(errorEvent))
    w.Write([]byte(stopEvent))
    if canFlush {
        f.Flush()
    }
}
```

注意：当前 AK-Switch 主要对接 Anthropic Messages 格式（sensenova 用 /messages 端点）。若需支持 OpenAI Chat 格式，追加 `finish_reason: "error"` chunk——但 YAGNI，先做 Anthropic 格式。

- [ ] **Step 5: 运行截断测试验证通过**

```bash
go test -tags=unit -count=1 -short -run TestStreamSSE ./internal/server/
```
Expected: PASS。

- [ ] **Step 6: 新增 BufferMode 配置字段**

`internal/config/config.go` 的 `ProviderConfig` 结构体，添加：
```go
BufferMode bool `toml:"buffer_mode,omitempty" field:"buffer_mode,display:Buffer Mode,scope:provider,default:false"`
```
（不加 `runtime` tag——启动时决定，不热更新）

- [ ] **Step 7: 写 buffer_mode 测试**

`internal/server/proxy_executor_test.go`：
```go
func TestHandleSuccess_BufferMode_BuffersAndRetries(t *testing.T) {
    // 构造 BufferMode=true 的 provider
    // 第一次上游返回截断流
    // 断言：客户端未收到任何内容（缓冲中）
    // 断言：换 key 重试（retry loop 继续）
    // 第二次上游返回完整流
    // 断言：客户端收到完整内容
}
```

- [ ] **Step 8: 实现 buffer_mode 的缓冲逻辑**

`internal/server/proxy_executor.go` 的 `handleSuccess` (line 277+)，根据 `ps.BufferMode()` 选择路径：

```go
func (px *ProxyExecutor) handleSuccess(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, ttfb time.Duration, method, target string, bodyBytes []byte, attempt int, rectified bool) {
    pool := ps.pool
    pool.RecordSuccess(idx)
    ps.RecordUpstreamSuccess()

    if ps.BufferMode() {
        // 缓冲模式：先读完整响应，失败则让 retry loop 换 key
        body, err := io.ReadAll(resp.Body)
        resp.Body.Close()
        if err != nil || !isCompleteStream(body) {
            slog.Warn("buffer mode: incomplete response, will retry", ...)
            return  // 不写任何内容给客户端，让 retry loop 继续
        }
        copyHeaders(w.Header(), resp.Header)
        w.WriteHeader(resp.StatusCode)
        w.Write(body)
        return
    }

    // 原有流式逻辑
    copyHeaders(w.Header(), resp.Header)
    w.WriteHeader(resp.StatusCode)
    // ... streamSSEAndEstimateTokens ...
}
```

注意：缓冲模式下"失败不写客户端 + return"会让外层 retry loop 继续——但当前 retry loop 在 `handleSuccess` 后是 `return`（line 152）。需要改为：缓冲模式失败时不 return，而是 continue retry loop。这需要重构 handleSuccess 的返回值（返回 bool 表示是否已提交响应）。

- [ ] **Step 9: 重构 handleSuccess 返回值支持 buffer retry**

`handleSuccess` 改为返回 `bool`（是否已向客户端提交响应）。`proxy_executor.go:151-153` 的调用改为：
```go
default:
    if px.handleSuccess(w, ps, idx, resp, start, ttfb, r.Method, target, bodyBytes, round, rectified) {
        return  // 已提交响应，退出 retry loop
    }
    continue  // 缓冲模式失败，换 key 重试
```

- [ ] **Step 10: 运行全部测试**

```bash
go test -tags=unit -count=1 -short ./internal/server/ ./internal/config/
make check
make test-unit
```
Expected: PASS。

- [ ] **Step 11: Commit**

```bash
git add internal/server/proxy_executor.go internal/server/proxy_executor_test.go internal/config/config.go
git commit -m "feat: 流式截断检测+错误事件注入+buffer_mode 可配置 (#340)"
```

---

## Self-Review

### 1. Spec coverage

- #302 删 disable_thinking → Task 1 ✅
- #308 端口 8080→4000 → Task 2 ✅
- #335 无可用 key 返回 429 → Task 3 ✅
- #323 删 --by-name 自动判断 → Task 4 ✅
- #324 log_level 持久化 → Task 5 ✅（含调查分支）
- #344 熔断器永久禁用 + 探针 → Task 6 ✅
- #340 流式截断 + buffer_mode → Task 7 ✅

未覆盖（明确 defer）：
- #305（已由 #328/#332 修复，只需验证后关闭，不在本计划）
- #314（C1/C2 后可能已修复，验证后关闭）
- #326/#345/#317/#339（讨论/调查类，单独处理）

### 2. Placeholder scan

- Task 3 Step 5、Task 5 Step 4、Task 6 Step 6 的测试实现标注"根据实际构造方式补全"——这是合理的，因为 ProxyExecutor/KeyPool 的测试构造方式需实现时确认。不算 placeholder，是 TDD 的探索步骤。
- 无"TBD"/"TODO"/"add appropriate error handling"等空泛描述。

### 3. Type consistency

- `ErrorAllKeysCooling`（Task 3 新增）在 Task 3 Step 4 定义，Step 3 使用——一致 ✅
- `PeriodicKeyProbe(pool, target, interval, stop)`（Task 6）签名在 Step 6 定义，Step 7 使用——一致 ✅
- `BufferMode` 字段（Task 7）在 Step 6 定义为 `bool`，Step 8 通过 `ps.BufferMode()` 访问——需确认 ProviderState 有 getter（实现时补）✅
- `handleSuccess` 返回值（Task 7 Step 9）从 void 改为 bool，调用点 Step 9 同步改——一致 ✅

### 4. 执行顺序建议

Batch 1（Task 1-4）可并行，不同模块无冲突。Batch 2（Task 5-7）建议串行：
- Task 5 独立（Config/CLI）
- Task 6（circuitbreaker + server）和 Task 7（proxy_executor + config）都改 server，但不同文件，可并行——不过都改 `proxy_executor.go` 的话 Task 3 和 Task 7 有重叠，建议 Task 3 先合。

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-17-issue-batch-fixes.md`. Two execution options:

**1. Subagent-Driven (recommended)** - 每个 task 派一个子代理实现，task 间 review，快速迭代

**2. Inline Execution** - 当前会话内执行，批量执行带 checkpoint

用户已选 Subagent-Driven（SDD）。执行时调用 `superpowers:subagent-driven-development`。
