# Eliminate CLI Duplications (Candidate 2)

> Architecture Review Candidate 2 from [#300](https://github.com/AK-Switch/ak-switch/issues/300)
> Date: 2026-08-15
> Status: Approved

## Problem

Two independent code duplications exist in `internal/cli/` and across the project:

| Duplication | Locations | Lines | Risk |
|---|---|---|---|
| `loadAdminToken` vs `loadAdminTokenFromConfig` | `key.go:647` / `reload.go:44` | ~17 lines each, ~90% identical | Fixing Cobra workaround or token loading logic requires touching two files |
| Log level validation (`"debug","info","warn","error"`) | `field_descriptor.go` (Parse + ApplyRuntime), `loglevel.go:55`, `admin_api.go:108` | 4 independent copies | Adding a new level requires updating 4 places; one miss causes inconsistency |

### Accepted scope correction

Issue #300 claimed `hasCLIFlag`/`getCLIFlagValue`/`flagShortName` were duplicated in `config.go` and `provider.go`. **This is incorrect** — these three functions exist only in `provider.go` (lines 611/631/654). No duplication to eliminate.

## Solution

### 1. Merge `loadAdminToken` and `loadAdminTokenFromConfig`

**Current state (two functions):**

```go
// key.go:647 — specific provider lookup
func loadAdminToken(provider string) (string, error) {
    xdgPath, err := config.XDGConfigPath()
    // ... error handling ...
    if p, ok := tc.Provider[provider]; ok {
        return p.AdminToken, nil
    }
    return "", nil
}

// reload.go:44 — scan all providers
func loadAdminTokenFromConfig() (string, error) {
    xdgPath, err := config.XDGConfigPath()
    // ... error handling ...
    for _, p := range tc.Provider {
        if p.AdminToken != "" {
            return p.AdminToken, nil
        }
    }
    return "", nil
}
```

**Merged function (replaces `loadAdminToken` in `key.go`):**

```go
// loadAdminToken loads an admin token from the TOML config file.
// If provider is non-empty, looks up that specific provider's token.
// If provider is empty, returns the first non-empty token from any provider.
func loadAdminToken(provider string) (string, error) {
    xdgPath, err := config.XDGConfigPath()
    if err != nil {
        return "", err
    }
    tc, err := config.LoadTomlConfig(xdgPath)
    if err != nil {
        if os.IsNotExist(err) {
            return "", nil
        }
        return "", err
    }
    if provider != "" {
        if p, ok := tc.Provider[provider]; ok {
            return p.AdminToken, nil
        }
        return "", nil
    }
    for _, p := range tc.Provider {
        if p.AdminToken != "" {
            return p.AdminToken, nil
        }
    }
    return "", nil
}
```

**Call site change:**

```go
// admin_client.go:29-31 — before
if provider != "" {
    token, err = loadAdminToken(provider)
} else {
    token, err = loadAdminTokenFromConfig()
}

// after — single call
token, err = loadAdminToken(provider)
```

**Placement:** the merged `loadAdminToken` lives in `key.go` (where the original `loadAdminToken` already was). `reload.go`'s copy is deleted.

### 2. Consolidate log level validation

**New function in `internal/config/field_descriptor.go`:**

```go
// IsValidLogLevel reports whether level is a recognized log level.
// Valid levels: debug, info, warn, error.
func IsValidLogLevel(level string) bool {
    switch level {
    case "debug", "info", "warn", "error":
        return true
    }
    return false
}
```

**4 call sites replaced:**

| Location | Before | After |
|---|---|---|
| `field_descriptor.go:312` (Parse) | `switch v { case "debug", "info", "warn", "error": ... }` → `if !IsValidLogLevel(v) { ... }` | Same file, no import change |
| `field_descriptor.go:330` (ApplyRuntime) | `switch v { case "debug", "info", "warn", "error": ... }` → `if !IsValidLogLevel(v) { ... }` | Same file, no import change |
| `loglevel.go:55` | `validLevels := map[string]bool{...}` → `if !config.IsValidLogLevel(level) { ... }` | Add `akswitch/internal/config` import |
| `admin_api.go:108` | `switch body.Level { case "debug", "info", "warn", "error": ... }` → `if !config.IsValidLogLevel(body.Level) { ... }` | Already imports `config` |

## Scope

| File | Action | ±Lines |
|---|---|---|
| `internal/cli/key.go` | Replace `loadAdminToken` with merged version (handles both cases) | ~+5 |
| `internal/cli/reload.go` | Delete `loadAdminTokenFromConfig`; remove unused `config` import | ~-18 |
| `internal/cli/admin_client.go` | `loadAdminTokenFromConfig()` → `loadAdminToken("")` | -4 |
| `internal/config/field_descriptor.go` | Add `IsValidLogLevel`; replace two inline switches | +5 |
| `internal/cli/loglevel.go` | Inline `validLevels` map → `config.IsValidLogLevel` | -2 |
| `internal/server/admin_api.go` | Inline switch → `config.IsValidLogLevel` | -2 |

**Net: ~-40 lines**, zero new dependencies, no API contract changes.

## Risks

| Risk | Mitigation |
|---|---|
| `loadAdminToken` merge changes behavior | Binary identical — same conditional logic, same error handling, same OS path detection |
| `IsValidLogLevel` changes semantics | Pure extraction — the same levels, same logic, only centralized |
| Circular import between `cli` and `config` | `cli` already imports `config`; no reverse dependency |
| server importing `config` for log level | `admin_api.go` already imports `akswitch/internal/config` |