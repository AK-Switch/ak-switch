# Merge Dual Runtime Config Descriptor Systems

> Candidate 1 from Architecture Review (#300)
> Date: 2026-08-14
> Status: Approved

## Problem

Two independent descriptor tables exist for the same set of runtime-configurable fields:

| | CLI side (`ConfigFieldDescriptor`) | Server side (`runtimeConfigField`) |
|---|---|---|
| Location | `internal/config/field_descriptor.go` | `internal/server/admin_api.go:948-1143` |
| Entry count | 16 (12 provider + 4 global) | 10 |
| Overlap | 8 runtime-editable fields | Same 8 + 2 server-only |
| Parse | `fd.Parse` closure | `toInt`/`toFloat64` inline |
| Validate | `validateFieldRange` via `MinInt` | Inline in `apply` closure |
| Persist | `fd.Persist` closure → `persistFieldToToml` | `f.persist` closure → direct `*Config` write |

### Confirmed desync bug

`cooldown_sec` default value:
- `field_descriptor.go:76` → `Default: "60"` (used by CLI `config get`)
- `config.go:28` → struct tag `default:"15"` (used by TOML parsing)
- `DefaultProviderConfig()` → returns `15`

`config get cooldown_sec` reports 60 for unset fields; actual runtime value is 15.

### Missing CLI access

`thinking_mode` and `rectify_thinking_map_to` exist only in `runtimeConfigFields`. CLI `config set thinking_mode` returns "unknown config key".

## Solution

Add an `ApplyRuntime` function field to `ConfigFieldDescriptor`. Server initializes it at package init time. CLI and Admin API share the single descriptor table.

### Changes

#### 1. `ConfigFieldDescriptor` struct — add `ApplyRuntime` field

```go
// ApplyRuntime applies a validated value to ProviderState at runtime.
// Provider is the target provider name. Called by the admin API
// runtime config endpoint. Nil for non-runtime fields.
ApplyRuntime func(ps *ProviderState, provider string, value any) (any, error)
```

#### 2. Provider-scoped descriptor entries — populate `ApplyRuntime`

For each of the 10 runtime-editable fields, assign `ApplyRuntime` using the same logic currently in `runtimeConfigFields.apply` closures. Non-runtime fields (`target`, `admin_token`, `keys_file`, `disable_thinking`, `genai_model`, `health_check_interval_sec`) leave it nil.

#### 3. New descriptor entries — `thinking_mode` and `rectify_thinking_map_to`

Add to the provider-scoped section, matching the existing server-side validation:

- `thinking_mode`: accepts `"default"` or `"rectify"`, calls `ps.SetThinkingMode`
- `rectify_thinking_map_to`: accepts `"enabled"`, `"auto"`, `"disabled"`, calls `ps.SetRectifyThinkingMapTo`

#### 4. Fix `cooldown_sec` default desync

`field_descriptor.go:76`: change `Default: "60"` → `Default: "15"`.

#### 5. `admin_api.go` — delete `runtimeConfigFields` table (lines 943-1153)

Replace `setRuntimeConfigField`, `persistRuntimeConfigField`, `persistRuntimeConfigFieldToDefault`:

```go
// Before
f := lookupRuntimeConfigField(key)
if f == nil { return nil, fmt.Errorf("unknown key %q", key) }
return f.apply(ps, value)

// After
fd := config.FindField(key)
if fd == nil || !fd.RuntimeEditable || fd.ApplyRuntime == nil {
    return nil, fmt.Errorf("unknown key %q", key)
}
return fd.ApplyRuntime(ps, providerName, value)
```

Delete: `runtimeConfigField` struct, `runtimeConfigFields` var, `lookupRuntimeConfigField` function (~210 lines).

Keep: `getRuntimeParams` (line 753-766) — reads live `ProviderState` getters, no descriptor needed.

#### 6. Test updates

- `field_descriptor_test.go`: verify `ApplyRuntime` is non-nil for all 10 runtime-editable fields
- Any test that asserts `cooldown_sec` default of 60 → update to 15

## Scope

| File | Action | Lines |
|---|---|---|
| `internal/config/field_descriptor.go` | Add `ApplyRuntime` field + populate 10 entries + add 2 new entries + fix default | +32 |
| `internal/server/admin_api.go` | Delete `runtimeConfigFields` table, update 3 call sites | -210 |
| `internal/config/field_descriptor_test.go` | Update assertions | ±5 |

Net: ~-173 lines, zero new dependencies, no API contract changes.

## Unchanged

- `getRuntimeParams` — hardcoded field read from live state, not descriptor-driven
- `toInt`/`toFloat64` in `internal/server/admin.go` — used elsewhere, keep
- CLI `config list`/`config get`/`config set` — behavior unchanged (same descriptor)
- Admin API `/api/runtime-config` endpoints — behavior unchanged (same logic, different source)

## Risks

| Risk | Mitigation |
|---|---|
| `ApplyRuntime` captures `*ProviderState` — if server type changes, all entries break | Same risk exists today with `runtimeConfigFields.apply` closures; nothing new |
| `ApplyRuntime` function field adds per-entry allocation at init | 10 entries, one-time at startup — negligible |
| Circular import between `config` and `server` packages | `ApplyRuntime` is a function field assigned at runtime; no compile-time dependency |
