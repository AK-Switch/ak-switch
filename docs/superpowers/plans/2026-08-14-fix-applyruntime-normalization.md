# Fix ApplyRuntime 归一化不一致 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 `thinking_mode` 和 `rectify_thinking_map_to` 两个字段的 `ApplyRuntime` 实现中存储未归一化值的问题

**Architecture:** 两处归一化bug均位于 `internal/config/field_descriptor.go` 的 `ConfigFieldDescriptor.ApplyRuntime` 闭包中。修复模式一致：switch 前提取归一化局部变量，switch 和后续存储均使用该变量。各自对齐对应 `Parse` 函数的语义。

**Tech Stack:** Go 1.26, 仅修改 `internal/config/field_descriptor.go` 两处代码 + 补充测试

## Global Constraints

- 保留 Tab 缩进（项目强制，gofmt 已配置）
- 遵循项目 CLAUDE.md 中的 ProviderState 封装模式（通过 getter/setter）
- 提交前必须执行 `make check && make test-unit`
- 新增代码附带 table-driven test

---

### Task 1: 修复 thinking_mode 和 rectify_thinking_map_to 的 ApplyRuntime 归一化

**Files:**
- Modify: `internal/config/field_descriptor.go:409-421` (thinking_mode ApplyRuntime)
- Modify: `internal/config/field_descriptor.go:454-469` (rectify_thinking_map_to ApplyRuntime)
- Test: `internal/server/admin_test.go:456-487` (追加归一化变体测试用例)
- Delete: `internal/config/norm_check_test.go` (临时演示文件，内容已固化为正式测试)

**Interfaces:**
- Consumes: `ProviderRuntimeState` interface (定义于 `field_descriptor.go`)，提供 `SetThinkingMode(v string)`、`SetRectifyThinkingMapTo(v string)` 等方法
- Produces: 修复后的 `ApplyRuntime` 闭包，归一化行为与对应 `Parse` 函数一致

- [ ] **Step 1: 在 admin_test.go 追加归一化变体测试用例**

在 `admin_test.go` 的 `TestSetRuntimeConfigField` table 中，追加以下测试用例：

```go
{name: "thinking_mode valid RECTIFY normalized", key: "thinking_mode", value: "RECTIFY", wantErr: false,
    check: func(t *testing.T, ps *ProviderState) {
        if ps.ThinkingMode() != "rectify" {
            t.Errorf("ThinkingMode = %q, want rectify", ps.ThinkingMode())
        }
    }},
{name: "thinking_mode valid Default normalized", key: "thinking_mode", value: " Default ", wantErr: false,
    check: func(t *testing.T, ps *ProviderState) {
        if ps.ThinkingMode() != "default" {
            t.Errorf("ThinkingMode = %q, want default", ps.ThinkingMode())
        }
    }},
{name: "rectify DISABLED normalized", key: "rectify_thinking_map_to", value: "DISABLED", wantErr: false,
    check: func(t *testing.T, ps *ProviderState) {
        if ps.RectifyThinkingMapTo() != "" {
            t.Errorf("RectifyThinkingMapTo = %q, want \"\" (normalized)", ps.RectifyThinkingMapTo())
        }
    }},
{name: "rectify Enabled normalized", key: "rectify_thinking_map_to", value: " Enabled ", wantErr: false,
    check: func(t *testing.T, ps *ProviderState) {
        if ps.RectifyThinkingMapTo() != "enabled" {
            t.Errorf("RectifyThinkingMapTo = %q, want enabled", ps.RectifyThinkingMapTo())
        }
    }},
```

插入位置：在 `rectify_thinking_map_to invalid` 用例之后（L487），`unknown key` 用例之前（L488），确保按 test case 逻辑分组。

- [ ] **Step 2: 运行测试确认新用例失败（红）**

Run: `go test -tags=unit -count=1 -short -run TestSetRuntimeConfigField ./internal/server/`
Expected: 4 个新增用例 FAIL（因为 ApplyRuntime 尚未修复）

- [ ] **Step 3: 修复 thinking_mode ApplyRuntime（L409-421）**

将 `field_descriptor.go` 中 `thinking_mode` 的 `ApplyRuntime` 函数：

```go
ApplyRuntime: func(ps any, provider string, value any) (any, error) {
    s, ok := value.(string)
    if !ok {
        return nil, fmt.Errorf("thinking_mode must be a string")
    }
    switch strings.TrimSpace(strings.ToLower(s)) {
    case "default", "rectify":
        ps.(ProviderRuntimeState).SetThinkingMode(s)
        return s, nil
    default:
        return nil, fmt.Errorf("invalid thinking_mode %q, use: default, rectify", s)
    }
},
```

改为：

```go
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
```

- [ ] **Step 4: 修复 rectify_thinking_map_to ApplyRuntime（L454-469）**

将 `field_descriptor.go` 中 `rectify_thinking_map_to` 的 `ApplyRuntime` 函数：

```go
ApplyRuntime: func(ps any, provider string, value any) (any, error) {
    s, ok := value.(string)
    if !ok {
        return nil, fmt.Errorf("rectify_thinking_map_to must be a string")
    }
    switch strings.TrimSpace(strings.ToLower(s)) {
    case "enabled", "auto", "disabled":
        if s == "disabled" {
            s = ""
        }
        ps.(ProviderRuntimeState).SetRectifyThinkingMapTo(s)
        return s, nil
    default:
        return nil, fmt.Errorf("invalid rectify_thinking_map_to %q, use: enabled, auto, disabled", s)
    }
},
```

改为：

```go
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
```

- [ ] **Step 5: 运行测试确认全部通过（绿）**

Run: `go test -tags=unit -count=1 -short -run TestSetRuntimeConfigField ./internal/server/`
Expected: 全部 PASS（包括 4 个新增归一化用例 + 原有 3 个 thinking_mode 用例 + 原有 4 个 rectify 用例）

Run: `go test -tags=unit -count=1 -short ./internal/config/`
Expected: 全部 PASS（包括 TestFindField_AllRegistered 等）

- [ ] **Step 6: 清理临时测试文件**

Run: `rm internal/config/norm_check_test.go`
（该文件仅用于演示，`TestReproNorm` 的验证逻辑已由新增 admin_test.go 用例覆盖）

- [ ] **Step 7: 运行 make check 确保无 lint/vet/fmt 问题**

Run: `make check`
Expected: 无错误输出

- [ ] **Step 8: 提交**

```bash
git add internal/config/field_descriptor.go internal/server/admin_test.go
git rm internal/config/norm_check_test.go
git commit -m "fix: ApplyRuntime 归一化不一致——thinking_mode 和 rectify_thinking_map_to 存储归一化值而非原始输入"
```

- [ ] **Step 9: 提交 spec 和 plan 文档**

```bash
git add docs/superpowers/specs/2026-08-14-fix-applyruntime-normalization-design.md docs/superpowers/plans/2026-08-14-fix-applyruntime-normalization.md
git commit -m "docs: 添加 ApplyRuntime 归一化修复设计文档和实现计划"
```