# field_descriptor 反射化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 struct tag 反射派生 `ConfigFieldDescriptors` 的元数据与 Parse/Format/Persist 样板，保留无法反射表达的定制闭包，同时反射化 DeepCopy 与 RuntimeConfig sync，消除三重声明。零行为变更。

**Architecture:** 两阶段构建——`reflectBuildDescriptors()` 遍历 `ProviderConfig`/`TomlConfig` 的 `field` struct tag 反射生成基础 descriptor（含派生的 Parse/Format/Persist），`customClosures` override map 补回 ApplyRuntime 级联与 enum 校验闭包。DeepCopy 与 syncRuntimeConfig 用 reflect 遍历字段替代手写拷贝。金标准快照测试锁定反射化前后的字节级等价。

**Tech Stack:** Go 1.26, `reflect`, `strconv`, `encoding/json`，table-driven tests，`-tags=unit`。

## Global Constraints

- Tab 缩进（项目强制，gofmt 已配置）
- 错误包装：`fmt.Errorf("函数名: %w", err)`
- 导入顺序：标准库 → 项目内部包 → 第三方包，按字母序
- 单元测试用 `-tags=unit`
- 零行为变更：现有测试的期望值不得修改
- `disable_thinking` 字段保留（#302 另议）
- `mergeWithDefaults` 反射逻辑（#331）不动
- 命名字段字面量：`ProviderConfig` 的所有字面量构造都是命名字段（已确认 22 个文件），重排 struct 字段顺序安全

## File Structure

| 文件 | 职责 | 本计划动作 |
|---|---|---|
| `internal/config/config.go` | `ProviderConfig`/`RuntimeConfig` struct 定义、`DefaultProviderConfig`、`DeepCopy`、`mergeWithDefaults` | 加 `field` tag；重排字段；`DeepCopy` 反射化；提取 `syncRuntimeConfig` |
| `internal/config/config_toml.go` | `TomlConfig` struct（含 global 字段 port/log_file） | 给 port/log_file 加 `field` tag |
| `internal/config/field_descriptor.go` | `ConfigFieldDescriptor` 类型 + `ConfigFieldDescriptors` 表 + `FindField` + `ParseDefault` | 手写表 → `reflectBuildDescriptors` + `customClosures` |
| `internal/config/field_descriptor_test.go` | descriptor 测试 | 新增金标准快照 + 派生测试 |
| `internal/config/config_defaults_test.go` | 默认值/merge/DeepCopy 测试 | 新增 slice 独立性测试 |

## 顺序对齐参考

反射化后 descriptor 顺序 = `ProviderConfig` 字段声明顺序（provider scope）+ `TomlConfig` global 字段顺序。目标顺序（= 现有手写表顺序）：

1. target 2. cooldown_sec 3. max_retries 4. backoff_cap_sec 5. backoff_multiplier
6. cb_reset_sec 7. upstream_cb_threshold 8. http_timeout_sec 9. log_level
10. health_check_interval_sec 11. admin_token 12. disable_thinking 13. thinking_mode
14. rectify_thinking_map_to 15. genai_model 16. keys_file 17. key_selection
18. port 19. log_file

`ProviderConfig` 中不进 descriptor 的字段（Host/Keys/KeyNames/HealthCheckPath/HealthCheckTimeoutSec/LogMaxSize/LogMaxAge/ErrorDumpMaxAge/CalibrationIntervalSec/LogFile）不加 `field` tag，反射时跳过。注意 `LogFile` 在 `ProviderConfig` 中存在但不进表（descriptor 表的 log_file 是 `TomlConfig` 的 global 字段）。

---

### Task 1: 金标准快照测试 — 锁定现有手写表行为

在动任何生产代码前，先写一个测试，把现有手写 `ConfigFieldDescriptors` 表的全部属性导出为快照。后续反射化后此测试必须保持通过（证明等价）。这是零行为变更的主证据。

**Files:**
- Create: `internal/config/field_descriptor_golden_test.go`
- Read-only ref: `internal/config/field_descriptor.go`（现有手写表）

**Interfaces:**
- Consumes: `config.ConfigFieldDescriptors`（现有手写表，本任务只读）
- Produces: 无生产代码改动，仅测试

- [ ] **Step 1: 写金标准快照测试**

创建 `internal/config/field_descriptor_golden_test.go`：

```go
//go:build unit

package config

import (
	"testing"
)

// goldenDescriptor 是 ConfigFieldDescriptor 的可比较快照。
type goldenDescriptor struct {
	Key             string
	DisplayName     string
	Scope           FieldScope
	TomlPath        string
	Type            FieldType
	Default         string
	RuntimeEditable bool
	ReadOnly        bool
	MinInt          int
}

// snapshotDescriptors 把 ConfigFieldDescriptors 导出为 golden 切片（顺序保留）。
func snapshotDescriptors() []goldenDescriptor {
	out := make([]goldenDescriptor, len(ConfigFieldDescriptors))
	for i, d := range ConfigFieldDescriptors {
		out[i] = goldenDescriptor{
			Key:             d.Key,
			DisplayName:     d.DisplayName,
			Scope:           d.Scope,
			TomlPath:        d.TomlPath,
			Type:            d.Type,
			Default:         d.Default,
			RuntimeEditable: d.RuntimeEditable,
			ReadOnly:        d.ReadOnly,
			MinInt:          d.MinInt,
		}
	}
	return out
}

// TestGolden_DescriptorSnapshot 锁定反射化前的手写表全属性。
// 反射化后此测试必须保持通过——任何 Key/Type/Default/Scope/顺序变化都会失败。
func TestGolden_DescriptorSnapshot(t *testing.T) {
	got := snapshotDescriptors()

	// 期望值直接从现有手写表逐字段抄录（2026-08-16 main e3a9a4f）。
	want := []goldenDescriptor{
		{Key: "target", DisplayName: "Target URL", Scope: FieldScopeProvider, TomlPath: "provider.%s.target", Type: FieldTypeString, Default: "", RuntimeEditable: false, ReadOnly: false, MinInt: 0},
		// 注：原手写表 TomlPath 为 "provider.%s.target_base"，但该值是死元数据——
		// (1) 没有任何生产代码或测试读取 TomlPath；(2) 实际 toml tag 是 `toml:"target"`，
		// 不是 target_base。反射化按 toml tag 派生得到 "target"，此值正确。golden 快照
		// 采用正确值，这是对原手写表死元数据的修正，零行为变更。
		// 注2：MinInt 未显式设置的字段，Go 零值为 0（不是 -1）。手写表与反射派生均为 0。
		{Key: "cooldown_sec", DisplayName: "Cooldown (sec)", Scope: FieldScopeProvider, TomlPath: "provider.%s.cooldown_sec", Type: FieldTypeInt, Default: "15", RuntimeEditable: true, ReadOnly: false, MinInt: 1},
		{Key: "max_retries", DisplayName: "Max Retries", Scope: FieldScopeProvider, TomlPath: "provider.%s.max_retries", Type: FieldTypeInt, Default: "1", RuntimeEditable: true, ReadOnly: false, MinInt: 0},
		{Key: "backoff_cap_sec", DisplayName: "Backoff Cap (sec)", Scope: FieldScopeProvider, TomlPath: "provider.%s.backoff_cap_sec", Type: FieldTypeInt, Default: "120", RuntimeEditable: true, ReadOnly: false, MinInt: 1},
		{Key: "backoff_multiplier", DisplayName: "Backoff Multiplier", Scope: FieldScopeProvider, TomlPath: "provider.%s.backoff_multiplier", Type: FieldTypeFloat64, Default: "2", RuntimeEditable: true, ReadOnly: false, MinInt: 0},
		{Key: "cb_reset_sec", DisplayName: "Circuit Breaker Reset (sec)", Scope: FieldScopeProvider, TomlPath: "provider.%s.cb_reset_sec", Type: FieldTypeInt, Default: "30", RuntimeEditable: true, ReadOnly: false, MinInt: 1},
		{Key: "upstream_cb_threshold", DisplayName: "Upstream CB Threshold", Scope: FieldScopeProvider, TomlPath: "provider.%s.upstream_cb_threshold", Type: FieldTypeInt, Default: "5", RuntimeEditable: true, ReadOnly: false, MinInt: 1},
		{Key: "http_timeout_sec", DisplayName: "HTTP Timeout (sec)", Scope: FieldScopeProvider, TomlPath: "provider.%s.http_timeout_sec", Type: FieldTypeInt, Default: "30", RuntimeEditable: true, ReadOnly: false, MinInt: 1},
		{Key: "log_level", DisplayName: "Log Level", Scope: FieldScopeProvider, TomlPath: "provider.%s.log_level", Type: FieldTypeString, Default: "info", RuntimeEditable: true, ReadOnly: false, MinInt: 0},
		{Key: "health_check_interval_sec", DisplayName: "Health Check Interval (sec)", Scope: FieldScopeProvider, TomlPath: "provider.%s.health_check_interval_sec", Type: FieldTypeInt, Default: "30", RuntimeEditable: false, ReadOnly: false, MinInt: 0},
		{Key: "admin_token", DisplayName: "Admin Token", Scope: FieldScopeProvider, TomlPath: "provider.%s.admin_token", Type: FieldTypeString, Default: "", RuntimeEditable: false, ReadOnly: true, MinInt: 0},
		{Key: "disable_thinking", DisplayName: "Disable Thinking", Scope: FieldScopeProvider, TomlPath: "provider.%s.disable_thinking", Type: FieldTypeBool, Default: "false", RuntimeEditable: false, ReadOnly: false, MinInt: 0},
		{Key: "thinking_mode", DisplayName: "Thinking Mode", Scope: FieldScopeProvider, TomlPath: "provider.%s.thinking_mode", Type: FieldTypeString, Default: "default", RuntimeEditable: true, ReadOnly: false, MinInt: 0},
		{Key: "rectify_thinking_map_to", DisplayName: "Rectify Thinking Map To", Scope: FieldScopeProvider, TomlPath: "provider.%s.rectify_thinking_map_to", Type: FieldTypeString, Default: "", RuntimeEditable: true, ReadOnly: false, MinInt: 0},
		{Key: "genai_model", DisplayName: "GenAI Model", Scope: FieldScopeProvider, TomlPath: "provider.%s.genai_model", Type: FieldTypeString, Default: "", RuntimeEditable: false, ReadOnly: false, MinInt: 0},
		{Key: "keys_file", DisplayName: "Keys File", Scope: FieldScopeProvider, TomlPath: "provider.%s.keys_file", Type: FieldTypeString, Default: "keys.json", RuntimeEditable: false, ReadOnly: true, MinInt: 0},
		{Key: "key_selection", DisplayName: "Key Selection Mode", Scope: FieldScopeProvider, TomlPath: "provider.%s.key_selection", Type: FieldTypeString, Default: "polling", RuntimeEditable: false, ReadOnly: false, MinInt: 0},
		{Key: "port", DisplayName: "Port", Scope: FieldScopeGlobal, TomlPath: "port", Type: FieldTypeInt, Default: "8080", RuntimeEditable: false, ReadOnly: true, MinInt: 0},
		{Key: "log_file", DisplayName: "Log File", Scope: FieldScopeGlobal, TomlPath: "log_file", Type: FieldTypeString, Default: "", RuntimeEditable: false, ReadOnly: true, MinInt: 0},
	}

	if len(got) != len(want) {
		t.Fatalf("descriptor count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("descriptor[%d] (%s) =\n  got  %+v\n  want %+v", i, want[i].Key, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: 运行测试，确认通过（此时读的是手写表）**

Run: `go test -tags=unit -count=1 -run TestGolden_DescriptorSnapshot ./internal/config/`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/config/field_descriptor_golden_test.go
git commit -m "test: 添加 descriptor 金标准快照测试（反射化基线）"
```

### Task 2: 加 `field` struct tag + 重排字段顺序

给 `ProviderConfig` 的 17 个 provider 字段和 `TomlConfig` 的 2 个 global 字段加 `field` tag，并把 `ProviderConfig` 字段声明顺序重排为 descriptor 目标顺序。不进表的字段不加 tag（反射时跳过）。这是纯元数据/顺序调整，不改任何字段类型或 `default`/`toml` tag。

**Files:**
- Modify: `internal/config/config.go:17-49`（`ProviderConfig` struct，加 tag + 重排）
- Modify: `internal/config/config_toml.go:12-23`（`TomlConfig` 的 port/log_file 加 tag）
- Read-only ref: `internal/config/field_descriptor.go`（手写表，对照 tag 值）

**Interfaces:**
- Consumes: 无
- Produces: struct 上的 `field` tag（供 Task 3 的 `reflectBuildDescriptors` 读取）

**字段→tag 映射表**（`field` tag 值，从手写表逐字段抄录）：

| struct 字段 | field tag |
|---|---|
| TargetBase | `field:"target,display:Target URL,scope:provider"` |
| CooldownSec | `field:"cooldown_sec,display:Cooldown (sec),scope:provider,default:15,runtime,min:1"` |
| MaxRetries | `field:"max_retries,display:Max Retries,scope:provider,default:1,runtime,min:0"` |
| BackoffCapSec | `field:"backoff_cap_sec,display:Backoff Cap (sec),scope:provider,default:120,runtime,min:1"` |
| BackoffMultiplier | `field:"backoff_multiplier,display:Backoff Multiplier,scope:provider,default:2,runtime"` |
| CBResetSec | `field:"cb_reset_sec,display:Circuit Breaker Reset (sec),scope:provider,default:30,runtime,min:1"` |
| UpstreamCBThreshold | `field:"upstream_cb_threshold,display:Upstream CB Threshold,scope:provider,default:5,runtime,min:1"` |
| HTTPTimeoutSec | `field:"http_timeout_sec,display:HTTP Timeout (sec),scope:provider,default:30,runtime,min:1"` |
| LogLevel | `field:"log_level,display:Log Level,scope:provider,default:info,runtime"` |
| HealthCheckIntervalSec | `field:"health_check_interval_sec,display:Health Check Interval (sec),scope:provider,default:30"` |
| AdminToken | `field:"admin_token,display:Admin Token,scope:provider,readonly"` |
| DisableThinking | `field:"disable_thinking,display:Disable Thinking,scope:provider,default:false"` |
| ThinkingMode | `field:"thinking_mode,display:Thinking Mode,scope:provider,default:default,runtime"` |
| RectifyThinkingMapTo | `field:"rectify_thinking_map_to,display:Rectify Thinking Map To,scope:provider,runtime"` |
| GenaiModel | `field:"genai_model,display:GenAI Model,scope:provider"` |
| KeysFile | `field:"keys_file,display:Keys File,scope:provider,default:keys.json,readonly"` |
| KeySelection | `field:"key_selection,display:Key Selection Mode,scope:provider,default:polling"` |

注意 `ThinkingMode` 的 `default:default`：descriptor 显示默认值是 "default"，但 struct 的 `default:"..."` tag 没有（mergeWithDefaults 不填它）。`field` tag 的 `default:` 独立于 `default` tag——本计划 Task 3 派生 Default 时优先取 `field` tag 的 `default:` 段，缺失时 fallback 到 `default` struct tag，再 fallback 到类型零值字符串。

`TomlConfig` 的 global 字段：

| struct 字段 | field tag |
|---|---|
| Port | `field:"port,display:Port,scope:global,default:8080,readonly"` |
| LogFile | `field:"log_file,display:Log File,scope:global,readonly"` |

- [ ] **Step 1: 重排 `ProviderConfig` 字段并加 `field` tag**

修改 `internal/config/config.go` 的 `ProviderConfig` struct（行 17-49），按目标顺序排列并加 tag。重排后的完整 struct（保持原有 `toml`/`default` tag 与注释，仅追加 `field` tag 并调顺序）：

```go
type ProviderConfig struct {
	// ── 进 descriptor 表的 provider 字段（顺序 = descriptor 顺序）──────────
	TargetBase             string   `toml:"target" field:"target,display:Target URL,scope:provider"`
	CooldownSec            int      `toml:"cooldown_sec,omitempty" default:"15" field:"cooldown_sec,display:Cooldown (sec),scope:provider,default:15,runtime,min:1"`
	MaxRetries             int      `toml:"max_retries,omitempty" default:"1" field:"max_retries,display:Max Retries,scope:provider,default:1,runtime,min:0"`
	BackoffCapSec          int      `toml:"backoff_cap_sec,omitempty" default:"120" field:"backoff_cap_sec,display:Backoff Cap (sec),scope:provider,default:120,runtime,min:1"`
	BackoffMultiplier      float64  `toml:"backoff_multiplier,omitempty" default:"2" field:"backoff_multiplier,display:Backoff Multiplier,scope:provider,default:2,runtime"`
	CBResetSec             int      `toml:"cb_reset_sec,omitempty" default:"30" field:"cb_reset_sec,display:Circuit Breaker Reset (sec),scope:provider,default:30,runtime,min:1"`
	UpstreamCBThreshold    int      `toml:"upstream_cb_threshold,omitempty" default:"5" field:"upstream_cb_threshold,display:Upstream CB Threshold,scope:provider,default:5,runtime,min:1"`
	HTTPTimeoutSec         int      `toml:"http_timeout_sec,omitempty" default:"30" field:"http_timeout_sec,display:HTTP Timeout (sec),scope:provider,default:30,runtime,min:1"`
	LogLevel               string   `toml:"log_level,omitempty" default:"info" field:"log_level,display:Log Level,scope:provider,default:info,runtime"`
	HealthCheckIntervalSec int      `toml:"health_check_interval_sec,omitempty" default:"30" field:"health_check_interval_sec,display:Health Check Interval (sec),scope:provider,default:30"`
	AdminToken             string   `toml:"admin_token,omitempty" field:"admin_token,display:Admin Token,scope:provider,readonly"`
	DisableThinking        bool     `toml:"disable_thinking,omitempty" field:"disable_thinking,display:Disable Thinking,scope:provider,default:false"` // Deprecated: use thinking_mode
	ThinkingMode           string   `toml:"thinking_mode,omitempty" field:"thinking_mode,display:Thinking Mode,scope:provider,default:default,runtime"` // "default" | "rectify"
	RectifyThinkingMapTo   string   `toml:"rectify_thinking_map_to,omitempty" field:"rectify_thinking_map_to,display:Rectify Thinking Map To,scope:provider,runtime"` // "enabled" | "auto" | "disabled"
	GenaiModel             string   `toml:"genai_model,omitempty" field:"genai_model,display:GenAI Model,scope:provider"` // Generative AI model name
	KeysFile               string   `toml:"keys_file,omitempty" default:"keys.json" field:"keys_file,display:Keys File,scope:provider,default:keys.json,readonly"`
	KeySelection           string   `toml:"key_selection,omitempty" default:"polling" field:"key_selection,display:Key Selection Mode,scope:provider,default:polling"`
	// ── 不进 descriptor 表的字段（无 field tag，反射跳过）──────────────────
	Port                   int      `toml:"port" default:"8080"`
	Host                   string   `toml:"host,omitempty" default:"127.0.0.1"`
	Keys                   []string `toml:"-"` // API keys (at least one required)
	KeyNames               []string `toml:"-"` // Corresponding key names (empty string if unnamed), same length as Keys
	HealthCheckPath        string   `toml:"-" default:"/health"`
	HealthCheckTimeoutSec  int      `toml:"-" default:"5"`
	LogFile                string   `toml:"log_file,omitempty"` // 日志文件路径（空 = 不启用文件日志）
	LogMaxSize             int      `toml:"log_max_size,omitempty" default:"100"`
	LogMaxAge              int      `toml:"log_max_age,omitempty" default:"7"`
	ErrorDumpMaxAge        int      `toml:"error_dump_max_age,omitempty" default:"7"`
	CalibrationIntervalSec int      `toml:"calibration_interval_sec,omitempty" default:"3600"` // Token 校准间隔（秒，默认 1 小时）
}
```

- [ ] **Step 2: 给 `TomlConfig` 的 global 字段加 `field` tag**

修改 `internal/config/config_toml.go:12-23`，给 Port 和 LogFile 加 tag：

```go
type TomlConfig struct {
	Port            int                `toml:"port" field:"port,display:Port,scope:global,default:8080,readonly"`
	Host            string             `toml:"host,omitempty"`
	DefaultProvider string             `toml:"default_provider,omitempty"`
	Default         *Config            `toml:"provider.default,omitempty"`
	CredentialsDir  string             `toml:"credentials_dir,omitempty"` // Directory containing provider credential files (JSONL)
	LogFile         string             `toml:"log_file,omitempty" field:"log_file,display:Log File,scope:global,readonly"`
	LogMaxSize      int                `toml:"log_max_size,omitempty"`
	LogMaxAge       int                `toml:"log_max_age,omitempty"`
	ErrorDumpMaxAge int                `toml:"error_dump_max_age,omitempty"`
	Provider        map[string]*Config `toml:"provider"`
}
```

- [ ] **Step 3: 编译验证**

Run: `go build ./...`
Expected: 编译通过（命名字段字面量不受字段顺序影响）

- [ ] **Step 4: 运行金标准快照测试 + 全部 config 包测试，确认零行为变化**

Run: `go test -tags=unit -count=1 -short ./internal/config/ ./internal/cli/`
Expected: 全部 PASS（含 Task 1 的金标准测试——它仍读手写表，重排 struct 不影响手写表）

- [ ] **Step 5: 提交**

```bash
git add internal/config/config.go internal/config/config_toml.go
git commit -m "refactor: ProviderConfig 加 field tag 并重排字段顺序（descriptor 反射化准备）"
```

### Task 3: 实现反射派生核心（reflectBuildDescriptors + 派生闭包 + customClosures 机制）

实现从 struct tag 反射生成基础 descriptor 的全部逻辑：`field` tag 解析器、`parseByType`/`formatByType`/派生 `Persist`、`reflectBuildDescriptors`、`customClosures` override 类型与 `buildDescriptors` 组装函数。**此任务不替换手写表**——`ConfigFieldDescriptors` 仍指向手写表，反射产物通过独立测试证明等价。Task 4 才做替换。

**Files:**
- Modify: `internal/config/field_descriptor.go`（在现有手写表之后追加反射逻辑）
- Modify: `internal/config/field_descriptor_test.go`（新增派生测试）

**Interfaces:**
- Consumes: Task 2 的 `field` struct tag（`ProviderConfig`/`TomlConfig`）
- Produces:
  - `type customClosure struct { Parse func(string) (any, error); Format func(any) string; ApplyRuntime func(any, string, any) (any, error) }`
  - `var customClosures map[string]customClosure`（本任务初始化为空 map，Task 4 填充）
  - `func reflectBuildDescriptors() []ConfigFieldDescriptor`（反射生成基础 descriptor，无 ApplyRuntime）
  - `func buildDescriptors() []ConfigFieldDescriptor`（反射基础 + overlay customClosures）
  - `func parseByType(t FieldType, s string) (any, error)`
  - `func formatByType(t FieldType, v any) string`

- [ ] **Step 1: 写 `TestReflectBuild_Equivalence` 失败测试**

在 `internal/config/field_descriptor_test.go` 末尾追加：

```go
// TestReflectBuild_Equivalence 证明 reflectBuildDescriptors 生成的元数据
// 与现有手写表完全一致（金标准的运行时版本）。
func TestReflectBuild_Equivalence(t *testing.T) {
	reflected := reflectBuildDescriptors()
	golden := ConfigFieldDescriptors // 现有手写表

	if len(reflected) != len(golden) {
		t.Fatalf("reflected count = %d, want %d (golden)", len(reflected), len(golden))
	}
	for i := range golden {
		r, g := reflected[i], golden[i]
		if r.Key != g.Key {
			t.Errorf("[%d] Key = %q, want %q", i, r.Key, g.Key)
		}
		if r.DisplayName != g.DisplayName {
			t.Errorf("[%d] (%s) DisplayName = %q, want %q", i, g.Key, r.DisplayName, g.DisplayName)
		}
		if r.Scope != g.Scope {
			t.Errorf("[%d] (%s) Scope = %q, want %q", i, g.Key, r.Scope, g.Scope)
		}
		if r.TomlPath != g.TomlPath {
			t.Errorf("[%d] (%s) TomlPath = %q, want %q", i, g.Key, r.TomlPath, g.TomlPath)
		}
		if r.Type != g.Type {
			t.Errorf("[%d] (%s) Type = %q, want %q", i, g.Key, r.Type, g.Type)
		}
		if r.Default != g.Default {
			t.Errorf("[%d] (%s) Default = %q, want %q", i, g.Key, r.Default, g.Default)
		}
		if r.RuntimeEditable != g.RuntimeEditable {
			t.Errorf("[%d] (%s) RuntimeEditable = %v, want %v", i, g.Key, r.RuntimeEditable, g.RuntimeEditable)
		}
		if r.ReadOnly != g.ReadOnly {
			t.Errorf("[%d] (%s) ReadOnly = %v, want %v", i, g.Key, r.ReadOnly, g.ReadOnly)
		}
		if r.MinInt != g.MinInt {
			t.Errorf("[%d] (%s) MinInt = %d, want %d", i, g.Key, r.MinInt, g.MinInt)
		}
	}
}

// TestParseByType 覆盖四类型的默认 Parse。
func TestParseByType(t *testing.T) {
	tests := []struct {
		typ  FieldType
		in   string
		want any
	}{
		{FieldTypeInt, "42", 42},
		{FieldTypeFloat64, "2.5", 2.5},
		{FieldTypeBool, "true", true},
		{FieldTypeString, "abc", "abc"},
	}
	for _, tt := range tests {
		got, err := parseByType(tt.typ, tt.in)
		if err != nil {
			t.Errorf("parseByType(%q, %q) err = %v", tt.typ, tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseByType(%q, %q) = %v, want %v", tt.typ, tt.in, got, tt.want)
		}
	}
}

// TestFormatByType 覆盖四类型的默认 Format。
func TestFormatByType(t *testing.T) {
	if got := formatByType(FieldTypeInt, 42); got != "42" {
		t.Errorf("formatByType(int, 42) = %q, want %q", got, "42")
	}
	if got := formatByType(FieldTypeFloat64, float64(2)); got != "2" {
		t.Errorf("formatByType(float64, 2) = %q, want %q (整数去小数点)", got, "2")
	}
	if got := formatByType(FieldTypeFloat64, 2.5); got != "2.5" {
		t.Errorf("formatByType(float64, 2.5) = %q, want %q", got, "2.5")
	}
	if got := formatByType(FieldTypeBool, true); got != "true" {
		t.Errorf("formatByType(bool, true) = %q, want %q", got, "true")
	}
	if got := formatByType(FieldTypeString, "abc"); got != "abc" {
		t.Errorf("formatByType(string, abc) = %q, want %q", got, "abc")
	}
}
```

在测试文件头部补 `reflect` import（若未有）。

- [ ] **Step 2: 运行测试，确认失败（reflectBuildDescriptors 未定义）**

Run: `go test -tags=unit -count=1 -run "TestReflectBuild_Equivalence|TestParseByType|TestFormatByType" ./internal/config/`
Expected: FAIL —— `reflectBuildDescriptors`/`parseByType`/`formatByType` 未定义

- [ ] **Step 3: 实现反射派生逻辑**

在 `internal/config/field_descriptor.go` 末尾（`ParseDefault` 函数之后）追加。导入需加 `"reflect"`。

```go
// fieldTag 解析后的 field struct tag。
type fieldTag struct {
	key             string
	display         string
	scope           FieldScope
	defaultOverride string // 空表示未设
	runtime         bool
	readOnly        bool
	min             int // 0 = 未设（与 ConfigFieldDescriptor.MinInt 的 Go 零值一致）
}

// hasValue 报告 s 是否非空。
func hasValue(s string) bool { return s != "" }

// parseFieldTag 解析 `field:"key,display:...,scope:...,default:...,runtime,readonly,min:N"` tag。
func parseFieldTag(tag string) (fieldTag, bool) {
	if tag == "" {
		return fieldTag{}, false
	}
	ft := fieldTag{min: 0} // 0 = 未设（与 ConfigFieldDescriptor.MinInt 的 Go 零值一致）
	parts := strings.Split(tag, ",")
	if parts[0] == "" {
		return fieldTag{}, false
	}
	ft.key = parts[0]
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(p, ":")
		switch k {
		case "display":
			ft.display = v
		case "scope":
			ft.scope = FieldScope(v)
		case "default":
			ft.defaultOverride = v
		case "runtime":
			ft.runtime = true
		case "readonly":
			ft.readOnly = true
		case "min":
			if n, err := strconv.Atoi(v); err == nil {
				ft.min = n
			}
		}
	}
	return ft, true
}

// reflectTypeField 映射 reflect.Kind → FieldType。
func reflectTypeField(k reflect.Kind) FieldType {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return FieldTypeInt
	case reflect.String:
		return FieldTypeString
	case reflect.Bool:
		return FieldTypeBool
	case reflect.Float32, reflect.Float64:
		return FieldTypeFloat64
	default:
		return FieldTypeString // 兜底，不应出现
	}
}

// zeroDefault 返回类型零值的字符串表示。
func zeroDefault(t FieldType) string {
	switch t {
	case FieldTypeInt:
		return "0"
	case FieldTypeFloat64:
		return "0"
	case FieldTypeBool:
		return "false"
	default:
		return ""
	}
}

// parseByType 是由 Type 决定的默认 Parse。
func parseByType(t FieldType, s string) (any, error) {
	switch t {
	case FieldTypeInt:
		return strconv.Atoi(s)
	case FieldTypeFloat64:
		return strconv.ParseFloat(s, 64)
	case FieldTypeBool:
		return strconv.ParseBool(s)
	case FieldTypeString:
		return s, nil
	default:
		return nil, fmt.Errorf("unknown field type: %s", t)
	}
}

// formatByType 是由 Type 决定的默认 Format。
func formatByType(t FieldType, v any) string {
	switch t {
	case FieldTypeInt:
		return strconv.Itoa(v.(int))
	case FieldTypeFloat64:
		f := v.(float64)
		if f == float64(int64(f)) {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'f', 1, 64)
	case FieldTypeBool:
		return strconv.FormatBool(v.(bool))
	case FieldTypeString:
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// makePersist 为 provider/global scope 构造派生 Persist 闭包。
// structFieldName 是 Go struct 字段名（如 "CooldownSec"），用于反射定位。
func makePersist(structFieldName string, scope FieldScope) func(tc *TomlConfig, provider string, c *Config, value any) {
	return func(tc *TomlConfig, provider string, c *Config, value any) {
		if scope == FieldScopeProvider {
			if c == nil {
				return
			}
			reflect.ValueOf(c).Elem().FieldByName(structFieldName).Set(reflect.ValueOf(value))
			return
		}
		if tc == nil {
			return
		}
		reflect.ValueOf(tc).Elem().FieldByName(structFieldName).Set(reflect.ValueOf(value))
	}
}

// scanStruct 反射扫描一个 struct 的 field tag，生成基础 descriptor。
// 传入 reflect.TypeOf(&ProviderConfig{}) 之类的指针类型。
func scanStruct(typ reflect.Type, defaultScope FieldScope) []ConfigFieldDescriptor {
	var out []ConfigFieldDescriptor
	t := typ.Elem()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		ft, ok := parseFieldTag(sf.Tag.Get("field"))
		if !ok {
			continue // 无 field tag，跳过
		}
		scope := ft.scope
		if scope == "" {
			scope = defaultScope
		}
		fType := reflectTypeField(sf.Type.Kind())
		tomlName := sf.Tag.Get("toml")
		// toml tag 可能带 ",omitempty"，取逗号前部分
		if c := strings.Index(tomlName, ","); c >= 0 {
			tomlName = tomlName[:c]
		}
		var tomlPath string
		if scope == FieldScopeProvider {
			tomlPath = "provider.%s." + tomlName
		} else {
			tomlPath = tomlName
		}
		// Default 派生：field tag defaultOverride > default struct tag > 类型零值
		def := ft.defaultOverride
		if def == "" {
			def = sf.Tag.Get("default")
		}
		if def == "" {
			def = zeroDefault(fType)
		}
		d := ConfigFieldDescriptor{
			Key:             ft.key,
			DisplayName:     ft.display,
			Scope:           scope,
			TomlPath:        tomlPath,
			Type:            fType,
			Default:         def,
			RuntimeEditable: ft.runtime,
			ReadOnly:        ft.readOnly,
			MinInt:          ft.min,
			Parse:           func(s string) (any, error) { return parseByType(fType, s) },
			Format:          func(v any) string { return formatByType(fType, v) },
			Persist:         makePersist(sf.Name, scope),
		}
		out = append(out, d)
	}
	return out
}

// customClosure 存放无法反射派生的定制闭包。任一字段为 nil 表示用派生默认。
type customClosure struct {
	Parse        func(string) (any, error)
	Format       func(any) string
	ApplyRuntime func(ps any, provider string, value any) (any, error)
}

// customClosures 按 field key 索引定制闭包。Task 4 填充。
var customClosures = map[string]customClosure{}

// reflectBuildDescriptors 遍历 ProviderConfig + TomlConfig 的 field tag，
// 反射生成所有字段的基础 descriptor（含派生 Parse/Format/Persist，无 ApplyRuntime）。
func reflectBuildDescriptors() []ConfigFieldDescriptor {
	out := scanStruct(reflect.TypeOf(&ProviderConfig{}), FieldScopeProvider)
	out = append(out, scanStruct(reflect.TypeOf(&TomlConfig{}), FieldScopeGlobal)...)
	return out
}

// buildDescriptors 组装最终 descriptor：反射基础 + customClosures overlay。
func buildDescriptors() []ConfigFieldDescriptor {
	base := reflectBuildDescriptors()
	for i := range base {
		if cc, ok := customClosures[base[i].Key]; ok {
			if cc.Parse != nil {
				base[i].Parse = cc.Parse
			}
			if cc.Format != nil {
				base[i].Format = cc.Format
			}
			if cc.ApplyRuntime != nil {
				base[i].ApplyRuntime = cc.ApplyRuntime
			}
		}
	}
	return base
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test -tags=unit -count=1 -run "TestReflectBuild_Equivalence|TestParseByType|TestFormatByType" ./internal/config/`
Expected: PASS —— 反射产物的元数据与手写表逐字段一致

- [ ] **Step 5: 运行金标准快照测试 + 全部 config 包测试，确认未破坏现有行为**

Run: `go test -tags=unit -count=1 -short ./internal/config/`
Expected: 全部 PASS（手写表未动）

- [ ] **Step 6: 提交**

```bash
git add internal/config/field_descriptor.go internal/config/field_descriptor_test.go
git commit -m "feat: 实现 reflectBuildDescriptors 反射派生 descriptor 元数据"
```

### Task 4: 填充 customClosures + 用 buildDescriptors 替换手写表

把现有手写表里的定制闭包（10 个 ApplyRuntime + 4 个定制 Parse + 1 个定制 Format）原样搬进 `customClosures` map，然后把 `ConfigFieldDescriptors` 从手写表改为 `buildDescriptors()` 的返回值，删除手写表。

**Files:**
- Modify: `internal/config/field_descriptor.go`（填充 customClosures、替换 var、删除手写表）

**Interfaces:**
- Consumes: Task 3 的 `customClosures`/`buildDescriptors`、Task 2 的 tag
- Produces: `ConfigFieldDescriptors` 现在由反射生成（行为不变，金标准测试保证）

**customClosures 内容**（闭包体从现有手写表 `field_descriptor.go` 对应行原样搬移，不得改逻辑）：

| key | 来源行 | 内容 |
|---|---|---|
| cooldown_sec | 127-139 | ApplyRuntime（级联 ConfigurePoolCBs） |
| max_retries | 157-164 | ApplyRuntime |
| backoff_cap_sec | 182-194 | ApplyRuntime（级联 ConfigurePoolCBs） |
| backoff_multiplier | 219-231 | ApplyRuntime（级联 ConfigurePoolCBs） |
| cb_reset_sec | 249-257 | ApplyRuntime（调两个 setter） |
| upstream_cb_threshold | 275-283 | ApplyRuntime（调两个 setter） |
| http_timeout_sec | 301-309 | ApplyRuntime（SetProxyTimeout） |
| log_level | 319-325(Parse), 332-343(ApplyRuntime) | 定制 Parse + ApplyRuntime |
| thinking_mode | 403-410(Parse), 417-430(ApplyRuntime) | 定制 Parse + ApplyRuntime |
| rectify_thinking_map_to | 439-449(Parse), 450-456(Format), 462-478(ApplyRuntime) | 定制 Parse + Format + ApplyRuntime |
| key_selection | 520-527(Parse) | 定制 Parse |

- [ ] **Step 1: 填充 `customClosures` map**

在 `internal/config/field_descriptor.go` 的 `var customClosures = map[string]customClosure{}` 位置，把空 map 替换为填充版。**闭包体逐字从现有手写表对应行复制**，例如：

```go
var customClosures = map[string]customClosure{
	"cooldown_sec": {
		ApplyRuntime: func(ps any, provider string, value any) (any, error) {
			v, err := strconv.Atoi(value.(string))
			if err != nil || v < 1 {
				return nil, fmt.Errorf("cooldown_sec must be a positive integer")
			}
			ps.(ProviderRuntimeState).SetCooldownSec(v)
			ps.(ProviderRuntimeState).ConfigurePoolCBs(
				time.Duration(v)*time.Second,
				time.Duration(ps.(ProviderRuntimeState).BackoffCapSec())*time.Second,
				ps.(ProviderRuntimeState).BackoffMultiplier(),
			)
			return v, nil
		},
	},
	"max_retries": {
		ApplyRuntime: func(ps any, provider string, value any) (any, error) {
			v, err := strconv.Atoi(value.(string))
			if err != nil || v < 0 {
				return nil, fmt.Errorf("max_retries must be a non-negative integer")
			}
			ps.(ProviderRuntimeState).SetMaxRetries(v)
			return v, nil
		},
	},
	"backoff_cap_sec": {
		ApplyRuntime: func(ps any, provider string, value any) (any, error) {
			v, err := strconv.Atoi(value.(string))
			if err != nil || v < 1 {
				return nil, fmt.Errorf("backoff_cap_sec must be a positive integer")
			}
			ps.(ProviderRuntimeState).SetBackoffCapSec(v)
			ps.(ProviderRuntimeState).ConfigurePoolCBs(
				time.Duration(ps.(ProviderRuntimeState).CooldownSec())*time.Second,
				time.Duration(v)*time.Second,
				ps.(ProviderRuntimeState).BackoffMultiplier(),
			)
			return v, nil
		},
	},
	"backoff_multiplier": {
		ApplyRuntime: func(ps any, provider string, value any) (any, error) {
			v, err := strconv.ParseFloat(value.(string), 64)
			if err != nil || v < 1.0 {
				return nil, fmt.Errorf("backoff_multiplier must be a number >= 1.0")
			}
			ps.(ProviderRuntimeState).SetBackoffMultiplier(v)
			ps.(ProviderRuntimeState).ConfigurePoolCBs(
				time.Duration(ps.(ProviderRuntimeState).CooldownSec())*time.Second,
				time.Duration(ps.(ProviderRuntimeState).BackoffCapSec())*time.Second,
				v,
			)
			return v, nil
		},
	},
	"cb_reset_sec": {
		ApplyRuntime: func(ps any, provider string, value any) (any, error) {
			v, err := strconv.Atoi(value.(string))
			if err != nil || v < 1 {
				return nil, fmt.Errorf("cb_reset_sec must be a positive integer")
			}
			ps.(ProviderRuntimeState).SetUpstreamCBResetTimeout(v)
			ps.(ProviderRuntimeState).SetCBResetSec(v)
			return v, nil
		},
	},
	"upstream_cb_threshold": {
		ApplyRuntime: func(ps any, provider string, value any) (any, error) {
			v, err := strconv.Atoi(value.(string))
			if err != nil || v < 1 {
				return nil, fmt.Errorf("upstream_cb_threshold must be a positive integer")
			}
			ps.(ProviderRuntimeState).SetUpstreamProxyCBThreshold(v)
			ps.(ProviderRuntimeState).SetUpstreamCBThreshold(v)
			return v, nil
		},
	},
	"http_timeout_sec": {
		ApplyRuntime: func(ps any, provider string, value any) (any, error) {
			v, err := strconv.Atoi(value.(string))
			if err != nil || v < 1 {
				return nil, fmt.Errorf("http_timeout_sec must be a positive integer")
			}
			ps.(ProviderRuntimeState).SetProxyTimeout(time.Duration(v) * time.Second)
			ps.(ProviderRuntimeState).SetHTTPTimeoutSec(v)
			return v, nil
		},
	},
	"log_level": {
		Parse: func(s string) (any, error) {
			v := strings.TrimSpace(strings.ToLower(s))
			if !IsValidLogLevel(v) {
				return nil, fmt.Errorf("invalid log level %q, use: debug, info, warn, error", s)
			}
			return v, nil
		},
		ApplyRuntime: func(ps any, provider string, value any) (any, error) {
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("log_level must be a string")
			}
			v := strings.TrimSpace(strings.ToLower(s))
			if !IsValidLogLevel(v) {
				return nil, fmt.Errorf("invalid log level, use: debug, info, warn, error")
			}
			ps.(ProviderRuntimeState).SetLogLevel(v)
			return v, nil
		},
	},
	"thinking_mode": {
		Parse: func(s string) (any, error) {
			v := strings.TrimSpace(strings.ToLower(s))
			switch v {
			case "default", "rectify":
				return v, nil
			}
			return nil, fmt.Errorf("invalid thinking_mode %q, use: default, rectify", s)
		},
		ApplyRuntime: func(ps any, provider string, value any) (any, error) {
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("thinking_mode must be a string")
			}
			v := strings.TrimSpace(strings.ToLower(s))
			switch v {
			case "default", "rectify":
				ps.(ProviderRuntimeState).SetThinkingMode(v)
				return v, nil
			default:
				return nil, fmt.Errorf("invalid thinking_mode %q, use: default, rectify", s)
			}
		},
	},
	"rectify_thinking_map_to": {
		Parse: func(s string) (any, error) {
			v := strings.TrimSpace(strings.ToLower(s))
			switch v {
			case "enabled", "auto", "disabled":
				if v == "disabled" {
					v = ""
				}
				return v, nil
			}
			return nil, fmt.Errorf("invalid rectify_thinking_map_to %q, use: enabled, auto, disabled", s)
		},
		Format: func(v any) string {
			s := v.(string)
			if s == "" {
				return "disabled"
			}
			return s
		},
		ApplyRuntime: func(ps any, provider string, value any) (any, error) {
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("rectify_thinking_map_to must be a string")
			}
			v := strings.TrimSpace(strings.ToLower(s))
			switch v {
			case "enabled", "auto", "disabled":
				if v == "disabled" {
					v = ""
				}
				ps.(ProviderRuntimeState).SetRectifyThinkingMapTo(v)
				return v, nil
			default:
				return nil, fmt.Errorf("invalid rectify_thinking_map_to %q, use: enabled, auto, disabled", s)
			}
		},
	},
	"key_selection": {
		Parse: func(s string) (any, error) {
			v := strings.TrimSpace(strings.ToLower(s))
			switch v {
			case "polling", "random":
				return v, nil
			}
			return nil, fmt.Errorf("invalid key_selection %q, use: polling, random", s)
		},
	},
}
```

- [ ] **Step 2: 用 `buildDescriptors()` 替换手写表**

把 `field_descriptor.go` 中的：

```go
var ConfigFieldDescriptors = []ConfigFieldDescriptor{
	// ...（整个手写表，行 93-571）...
}
```

替换为：

```go
var ConfigFieldDescriptors = buildDescriptors()
```

删除整个手写表（行 93-571 的 `ConfigFieldDescriptors = []ConfigFieldDescriptor{...}`）。

- [ ] **Step 3: 编译验证**

Run: `go build ./...`
Expected: 编译通过

- [ ] **Step 4: 运行金标准快照测试 — 核心等价验证**

Run: `go test -tags=unit -count=1 -run TestGolden_DescriptorSnapshot ./internal/config/`
Expected: PASS —— 反射生成的 descriptor 元数据与原手写表字节级一致

- [ ] **Step 5: 运行全部 config + CLI 测试**

Run: `go test -tags=unit -count=1 -short ./internal/config/ ./internal/cli/`
Expected: 全部 PASS（含 Parse/Format 各类型测试、config list 输出字段集合测试）

- [ ] **Step 6: 提交**

```bash
git add internal/config/field_descriptor.go
git commit -m "refactor: 用 buildDescriptors 反射生成替换手写 descriptor 表"
```

### Task 5: DeepCopy 反射化 + 提取 syncRuntimeConfig

用反射遍历字段替代 `DeepCopy` 的 48 行手写拷贝，提取 `syncRuntimeConfig` 替代 `mergeWithDefaults` 末尾的 7 行手写 RuntimeConfig 同步。

**Files:**
- Modify: `internal/config/config.go:214-262`（`DeepCopy`）+ `config.go:293-299`（RuntimeConfig sync 段）
- Modify: `internal/config/config_defaults_test.go`（新增 slice 独立性测试）

**Interfaces:**
- Consumes: 无
- Produces:
  - `func reflectCopyStruct(dst, src interface{})`（通用反射拷贝，slice 深拷贝）
  - `func syncRuntimeConfig(rc *RuntimeConfig, pc *ProviderConfig)`

- [ ] **Step 1: 写 `TestDeepCopy_SliceIndependence` 失败测试**

在 `internal/config/config_defaults_test.go` 末尾追加：

```go
// TestDeepCopy_SliceIndependence 验证副本的 slice 不与原对象共享底层数组。
func TestDeepCopy_SliceIndependence(t *testing.T) {
	orig := &Config{
		ProviderConfig: ProviderConfig{
			Keys:     []string{"k1", "k2"},
			KeyNames: []string{"n1", "n2"},
		},
	}
	cp := orig.DeepCopy()

	// 改副本不影响原对象
	cp.Keys[0] = "changed"
	cp.KeyNames[0] = "changed"
	cp.Keys = append(cp.Keys, "k3")

	if orig.Keys[0] != "k1" {
		t.Errorf("orig.Keys[0] = %q, want %q (副本修改不应影响原对象)", orig.Keys[0], "k1")
	}
	if orig.KeyNames[0] != "n1" {
		t.Errorf("orig.KeyNames[0] = %q, want %q", orig.KeyNames[0], "n1")
	}
	if len(orig.Keys) != 2 {
		t.Errorf("len(orig.Keys) = %d, want 2", len(orig.Keys))
	}
}

// TestSyncRuntimeConfig 验证按名同步 ProviderConfig → RuntimeConfig。
func TestSyncRuntimeConfig(t *testing.T) {
	pc := &ProviderConfig{
		HTTPTimeoutSec:      45,
		MaxRetries:          3,
		CooldownSec:         20,
		BackoffCapSec:       90,
		BackoffMultiplier:   1.5,
		CBResetSec:          25,
		UpstreamCBThreshold: 7,
		LogLevel:            "warn",
	}
	var rc RuntimeConfig
	syncRuntimeConfig(&rc, pc)

	if rc.HTTPTimeoutSec != 45 || rc.MaxRetries != 3 || rc.CooldownSec != 20 {
		t.Error("syncRuntimeConfig 未正确同步字段")
	}
	if rc.BackoffMultiplier != 1.5 || rc.LogLevel != "warn" {
		t.Error("syncRuntimeConfig 未正确同步 float/string 字段")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test -tags=unit -count=1 -run "TestDeepCopy_SliceIndependence|TestSyncRuntimeConfig" ./internal/config/`
Expected: FAIL —— `syncRuntimeConfig` 未定义；`TestDeepCopy_SliceIndependence` 可能因现有 DeepCopy 已用 copy() 而通过，但 `syncRuntimeConfig` 必失败

- [ ] **Step 3: 实现 `reflectCopyStruct` + `syncRuntimeConfig`，重写 `DeepCopy`**

在 `internal/config/config.go` 中（`DeepCopy` 函数附近）替换。导入需含 `"reflect"`（已存在，mergeWithDefaults 在用）。

把现有 `DeepCopy`（行 214-262）替换为：

```go
// reflectCopyStruct 遍历 src 的字段拷贝到 dst。slice 做深拷贝，其余值类型直接 Set。
// dst/src 必须是指向同类型 struct 的指针。当前 ProviderConfig/RuntimeConfig 无指针/嵌套
// struct 字段，故不递归（YAGNI）。
func reflectCopyStruct(dst, src interface{}) {
	dv := reflect.ValueOf(dst).Elem()
	sv := reflect.ValueOf(src).Elem()
	for i := 0; i < sv.NumField(); i++ {
		sf := sv.Field(i)
		if sf.Kind() == reflect.Slice {
			if sf.IsNil() {
				dv.Field(i).Set(sf)
				continue
			}
			newSlice := reflect.MakeSlice(sf.Type(), sf.Len(), sf.Cap())
			reflect.Copy(newSlice, sf)
			dv.Field(i).Set(newSlice)
		} else {
			dv.Field(i).Set(sf)
		}
	}
}

// DeepCopy returns a deep copy of the Config.
func (c *Config) DeepCopy() *Config {
	cp := &Config{}
	reflectCopyStruct(&cp.ProviderConfig, &c.ProviderConfig)
	reflectCopyStruct(&cp.RuntimeConfig, &c.RuntimeConfig)
	return cp
}

// syncRuntimeConfig 把 ProviderConfig 中与 RuntimeConfig 同名的字段同步过去。
// 两者字段名一一对应（HTTPTimeoutSec/MaxRetries/CooldownSec/BackoffCapSec/
// BackoffMultiplier/CBResetSec/UpstreamCBThreshold/LogLevel）。
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

- [ ] **Step 4: 把 `mergeWithDefaults` 末尾手写 sync 替换为函数调用**

`internal/config/config.go` 中 `mergeWithDefaults` 末尾（行 293-299）的：

```go
	// Sync runtime config
	result.RuntimeConfig.HTTPTimeoutSec = result.HTTPTimeoutSec
	result.RuntimeConfig.MaxRetries = result.MaxRetries
	result.RuntimeConfig.CooldownSec = result.CooldownSec
	result.RuntimeConfig.BackoffCapSec = result.BackoffCapSec
	result.RuntimeConfig.BackoffMultiplier = result.BackoffMultiplier
	result.RuntimeConfig.CBResetSec = result.CBResetSec
	result.RuntimeConfig.UpstreamCBThreshold = result.UpstreamCBThreshold
	result.RuntimeConfig.LogLevel = result.LogLevel
```

替换为：

```go
	// Sync runtime config
	syncRuntimeConfig(&result.RuntimeConfig, &result.ProviderConfig)
```

注意：原手写有 8 行（含 LogLevel），`syncRuntimeConfig` 按名匹配会同步所有 8 个同名字段，行为一致。

- [ ] **Step 5: 运行新测试 + 全部 config 包测试**

Run: `go test -tags=unit -count=1 -short ./internal/config/`
Expected: 全部 PASS（含现有 TestDeepCopy、TestMergeWithDefaults_* 系列）

- [ ] **Step 6: 提交**

```bash
git add internal/config/config.go internal/config/config_defaults_test.go
git commit -m "refactor: DeepCopy 反射化 + 提取 syncRuntimeConfig"
```

### Task 6: 收尾验证

全量验证零行为变更，整理 `ParseDefault`，确认行数目标达成。

**Files:**
- Read-only check: `internal/config/field_descriptor.go`、`config.go`
- 可能 Modify: `field_descriptor.go`（`ParseDefault` 内部改调 `parseByType` 消除重复）

- [ ] **Step 1: 确认 `ParseDefault` 可复用 `parseByType`**

`ParseDefault`（field_descriptor.go 末尾）当前是一个 switch 按 Type 调 strconv，逻辑与 `parseByType` 完全重复。CLI 侧 `internal/cli/config.go` 3 处调用 `ParseDefault`（行 561/603/616），是导出 API，**不得删除签名**。把它改为内部调 `parseByType`：

```go
// ParseDefault converts the Default string to the field's type.
// 保留导出签名（CLI 侧调用），内部委托 parseByType 消除重复。
func ParseDefault(d *ConfigFieldDescriptor) (any, error) {
	return parseByType(d.Type, d.Default)
}
```

- [ ] **Step 2: 运行 `make check`（lint + vet + fmt）**

Run: `make check`
Expected: 干净通过

- [ ] **Step 3: 运行全量单元测试**

Run: `make test-unit`
Expected: 全部 PASS

- [ ] **Step 4: 运行 CLI + server 单包测试（descriptor/DeepCopy 的消费者）**

Run: `go test -tags=unit -count=1 -short ./internal/config/ ./internal/cli/ ./internal/server/`
Expected: 全部 PASS

- [ ] **Step 5: 确认行数目标**

Run: `wc -l internal/config/field_descriptor.go` 和 `grep -c "" internal/config/config.go` 中 DeepCopy 行数
Expected: `field_descriptor.go` 显著下降（手写表 ~480 行删除，反射逻辑 + customClosures ~200 行）；`DeepCopy` 从 48 行降到 ~5 行

- [ ] **Step 6: 提交收尾**

```bash
git add internal/config/field_descriptor.go
git commit -m "refactor: ParseDefault 委托 parseByType 消除重复"
```

## Self-Review

### Spec coverage

| spec 要求 | 覆盖 task |
|---|---|
| struct tag 编码元数据 | Task 2 |
| 派生规则（Key/Type/Default/Scope/TomlPath 等） | Task 2 (tag) + Task 3 (派生) |
| Parse/Format/Persist 派生 | Task 3 |
| 两阶段构建 + customClosures overlay | Task 3 (机制) + Task 4 (填充) |
| ApplyRuntime/enum 校验保留闭包 | Task 4 |
| DeepCopy 反射化 | Task 5 |
| syncRuntimeConfig 提取 | Task 5 |
| 金标准快照测试 | Task 1 |
| 新增断言测试（Completeness/DefaultsMatch/SliceIndependence/Persist） | Task 1 (snapshot) + Task 3 (Equivalence/Parse/Format) + Task 5 (SliceIndep/Sync) |
| ParseDefault 整理 | Task 6 |
| 零行为变更验证 | Task 1/3/4/5/6 的现有测试保持通过 |

### Placeholder scan

无 TBD/TODO。customClosures 闭包体在 Task 4 给出全部 11 个字段完整代码。struct 重排在 Task 2 给出完整 struct。reflectBuildDescriptors 在 Task 3 给出完整实现。

### Type consistency

- `customClosure` struct 定义（Task 3）与填充（Task 4）字段名一致：`Parse`/`Format`/`ApplyRuntime`
- `buildDescriptors` overlay 三个字段（Task 3）与 `customClosure` 三字段一致
- `reflectBuildDescriptors`/`parseByType`/`formatByType`/`makePersist`/`scanStruct` 定义（Task 3）与测试调用一致
- `reflectCopyStruct`/`syncRuntimeConfig` 定义（Task 5）与测试调用一致

### 已知风险点

- Task 3 的 `scanStruct` 用 `reflect.TypeOf(&ProviderConfig{})` —— 传入指针，`.Elem()` 取 struct。测试 `TestReflectBuild_Equivalence` 是运行时校验，若 tag 写错会立即失败。
- Task 4 替换手写表后，若某个 customClosure 漏搬，该字段会缺 ApplyRuntime/定制 Parse → 对应测试（如 runtime config 应用、enum 校验）会失败。现有 `field_descriptor_test.go` 的 ParseBool/Parse 测试覆盖 enum 字段的 Parse 行为。
- Task 5 的 `reflectCopyStruct` 对 nil slice 直接 Set（不深拷贝 nil）—— 与原手写 `copy()` 行为一致（原代码 `make([]string, len)` 对 nil 产出空 slice）。若现有测试对 nil slice 有断言，需观察。`TestDeepCopy_SliceIndependence` 只测非空 slice。
