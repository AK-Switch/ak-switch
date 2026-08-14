# Fix ApplyRuntime 归一化不一致设计文档

> **修复范围：** `thinking_mode` 和 `rectify_thinking_map_to` 两个字段的 `ApplyRuntime` 实现
> **根因：** `ApplyRuntime` 的 switch 校验使用 `strings.ToLower(s)` 归一化后匹配，但存储时使用原始输入 `s`，导致大小写/空格变体输入绕过校验但仍被存储，下游精确字符串比较时静默失效

## 背景

PR #304 合并了双套 Runtime Config Descriptor 系统，将 `admin_api.go` 中的 `runtimeConfigFields` 表迁移到 `field_descriptor.go` 的 `ConfigFieldDescriptor.ApplyRuntime` 字段。在代码审查中发现两个 `ApplyRuntime` 实现存在归一化不一致问题：

- `thinking_mode`（L409-421）：`switch` 用 `strings.ToLower(s)` 匹配，但 `SetThinkingMode(s)` 存储原始 `s`
- `rectify_thinking_map_to`（L454-469）：`switch` 用 `strings.ToLower(s)` 匹配，但 `if s == "disabled"` 比较原始 `s`

两个字段各自的 `Parse` 函数均正确归一化，因此 CLI 路径正常，只有 Admin API 路径受影响。

## 修复范围

| 字段 | 位置 | 缺陷 | 修复方式 |
|------|------|------|----------|
| `thinking_mode` | L416 | `SetThinkingMode(s)` 存储原始输入 | 改为 `SetThinkingMode(v)`，`v` 为归一化值 |
| `rectify_thinking_map_to` | L461 | `if s == "disabled"` 比较原始输入 | 改为 `if v == "disabled"`，`v` 为归一化值 |

## 修复模式

两处修复使用同一模式——对齐各自 `Parse` 函数的实现：

```go
// 修复前
switch strings.TrimSpace(strings.ToLower(s)) {
case "default", "rectify":
    ps.(ProviderRuntimeState).SetThinkingMode(s)  // 使用原始 s
    return s, nil
}

// 修复后
v := strings.TrimSpace(strings.ToLower(s))
switch v {
case "default", "rectify":
    ps.(ProviderRuntimeState).SetThinkingMode(v)  // 使用归一化 v
    return v, nil
}
```

```go
// 修复前
switch strings.TrimSpace(strings.ToLower(s)) {
case "enabled", "auto", "disabled":
    if s == "disabled" {  // 比较原始 s
        s = ""
    }
    ps.(ProviderRuntimeState).SetRectifyThinkingMapTo(s)
    return s, nil
}

// 修复后
v := strings.TrimSpace(strings.ToLower(s))
switch v {
case "enabled", "auto", "disabled":
    if v == "disabled" {  // 比较归一化 v
        v = ""
    }
    ps.(ProviderRuntimeState).SetRectifyThinkingMapTo(v)
    return v, nil
}
```

## 下游影响分析

| 消费端 | 受影响？ | 说明 |
|--------|---------|------|
| `proxy_executor.go:53` `ps.ThinkingMode() == "rectify"` | 当前静默失效 | 修复后正确匹配 |
| `rectifier.go:28` `r.mapTo == "" \|\| r.mapTo == "disabled"` | 当前大写变体跳过 no-op 检查 | 修复后正确触发 no-op 返回 |
| `getRuntimeParams` 返回值 | 返回未归一化值 | 修复后返回一致值 |
| TOML 持久化 | 写入未归一化值 | 修复后写入一致值 |

## 测试策略

1. **现有测试**：`admin_test.go` 中 `rectify_thinking_map_to` 设为 `"disabled"` 期望返回 `""` 的用例继续通过
2. **新增测试**：在 `field_descriptor_test.go` 中添加归一化变体测试，覆盖 `"DISABLED"`、`"Disabled"`、`" disabled "` 等输入，验证 `ApplyRuntime` 返回与 `Parse` 一致
3. **清理**：删除未跟踪的 `norm_check_test.go`（其内容已固化为正式测试）

## 不在此范围

- 其他 8 个 `ApplyRuntime` 实现的归一化审查（均使用 `strconv` 解析数字或直接赋值，不存在此问题）
- 对 `ConfigFieldDescriptor` 体系本身的架构性修改

## 验收标准

1. `go test -tags=unit -count=1 -short ./internal/config/` 全通过
2. `go test -tags=unit -count=1 -short -run TestFindField_AllRegistered ./internal/config/` 覆盖归一化变体
3. `make check` 通过（lint + vet + fmt）