# CLI Config Refactor — Bug Fixes Design Spec

**Date:** 2026-08-14
**Author:** Claude Code + wingIsCrazy
**Status:** Approved

## Overview

Fix 8 bugs introduced during the PR #287 CLI refactoring in `internal/cli/config.go` (7 issues) and `internal/cli/provider.go` (1 issue). All issues are regressions from the refactoring that replaced server-API-based config operations with direct TOML manipulation.

## Architecture

Two-file surgical edit. No new files, no new packages. Changes are additive (new validation, better error handling) and corrective (fixing inverted conditions, restoring lost behavior).

### Shared helper

Extract range validation into a shared function, placed near the top of `config.go` (after imports, before command definitions):

```go
func validateFieldRange(fd *config.ConfigFieldDescriptor, valueStr string) error {
    parsed, err := fd.Parse(valueStr)
    if err != nil {
        return fmt.Errorf("invalid --%s value: %w", fd.Key, err)
    }
    switch v := parsed.(type) {
    case int:
        if v < -1 {
            return fmt.Errorf("--%s must be >= -1", fd.Key)
        }
    case float64:
        if v < 1 {
            return fmt.Errorf("--%s must be >= %g", fd.Key, 1.0)
        }
    }
    return nil
}
```

`providerUpdateCmd` replaces its inline range check with a call to this function. `configSetCmd` calls it after its existing `fd.Parse`.

## Execution Order

```
Task 1: config.go 全部修复（#1-#6）— 单一文件，6个独立代码区域
Task 2: provider.go 布尔标志修复（#7）— 独立文件
```

Tasks are ordered by file to minimize context switching. No inter-task dependencies.

## Issues and Fixes

### Issue 1: `--runtime-only` flag inverted condition (config.go:379)

**Root cause:** Condition `!runtimeOnly` is used for both branches. Should be `runtimeOnly` for the runtime-only path.

**Fix:** Change line 379 from `!runtimeOnly` to `runtimeOnly`.

### Issue 2: `config set` lacks range validation (config.go:335)

**Root cause:** `configSetCmd` only calls `fd.Parse(valueStr)` (type check), then persists. `providerUpdateCmd` has range checks (`v < -1` for int, `v < 1` for float64) but `configSetCmd` doesn't.

**Fix:** After Parse, call `validateFieldRange(fd, valueStr)` before proceeding. Reuse the same validation logic as `providerUpdateCmd`.

### Issue 3: TOML loading errors silently swallowed (config.go:192, 267, 285, 364)

**Root cause:** `tc, _ := config.LoadTomlConfig(source)` discards `os.ErrNotExist` and parse errors. Users see `no providers configured` for both empty and broken configs.

**Fix:** Propagate the error instead of ignoring it. `LoadTomlConfig` returns `(*TomlConfig, error)` — return the error to the user.

### Issue 4: Ghost provider entries created (config.go:468+)

**Root cause:** `persistFieldToToml` creates `tc.Provider[name] = &config.Config{}` when the provider doesn't exist, without checking first.

**Fix:** In `configSetCmd`, before the persist loop, check that each target provider exists in `tc.Provider`. Return error for non-existent providers.

### Issue 5: `config get --all` redundant I/O (config.go:291-296)

**Root cause:** `config.XDGConfigPath()` and `config.LoadTomlConfig()` are called inside the per-provider loop.

**Fix:** Load TOML once before the loop, reuse the loaded config.

### Issue 6: Help text missing keys (config.go:315-317)

**Root cause:** Help text lists 8 keys but `FindField()` supports all 16 descriptor keys.

**Fix:** Update help text to list all 16 valid keys.

### Issue 7: Boolean flag `--disable-thinking` broken (provider.go)

**Root cause:** `getCLIFlagValue()` scans `os.Args` for the flag name, then returns `os.Args[i+1]` as the value. For boolean flags like `--disable-thinking` that are the last argument or followed by another flag, `i+1` is out of range or returns the next flag's value.

**Fix:** For fields with `fd.Type == "bool"`, check `cmd.Flags().Changed(flagName)` instead of parsing a value. If changed, use `"true"`; if not changed, skip.

### Issue 8: Closure variable capture hazard (admin_api.go:961) — deferred

**Root cause:** All `apply`/`persist` closures capture loop variable `fd` by reference.

**Fix:** Deferred. Currently safe (closures don't read `fd` at call time). Fix when a future change requires `fd` access inside closures.

### Issue 9: `Persist` closures ignore conversion errors (admin_api.go:980-1118) — deferred

**Root cause:** `v, _ := toInt(val)` / `v, _ := toFloat64(val)` discards conversion errors.

**Fix:** Deferred. Currently safe (server controls input types). Fix if corrupted config becomes a realistic scenario.

### Issue 10: `json.Marshal` error silently discarded (config.go:423) — deferred

**Root cause:** `payloadBytes, _ := json.Marshal(payloadMap)`.

**Fix:** Deferred. Currently safe (int/float64/string/bool all marshal). Fix if new field types are added.

### Issue 11: `io.ReadAll` error silently discarded (config.go:438) — deferred

**Root cause:** `body, _ := io.ReadAll(resp.Body)`.

**Fix:** Deferred. Currently cosmetic.

### Issue 12: `config.DefaultProviderName` side effect (provider.go:408) — deferred

**Root cause:** `config.DefaultProviderName = name` is set before TOML save, not rolled back on failure.

**Fix:** Deferred. Low risk, requires restructuring.

## Testing Strategy

Each fix gets an independent test function following existing patterns:
- `t.TempDir()` + `config.SaveTomlConfig()` for setup
- `os.Args` mutation + `cmd.SetArgs()` for CLI invocation
- Direct value inspection of loaded TOML for persistence verification
- `os.Pipe()` for stdout/stderr capture when needed

## Execution Order

```
Task 1: config.go 全部修复（#1-#6）— 单一文件，6个独立代码区域
Task 2: provider.go 布尔标志修复（#7）— 独立文件
```

Tasks are ordered by file to minimize context switching. No inter-task dependencies.
