# Eliminate CLI Duplications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate two independent code duplications: merge `loadAdminToken`/`loadAdminTokenFromConfig` and consolidate log level validation into `config.IsValidLogLevel`.

**Architecture:** Two independent tasks, order-independent. Task 1 merges two near-identical functions in `internal/cli/` into one, removing ~30 lines. Task 2 extracts a shared validation function into `internal/config/` and replaces 4 inline copies, net ~-10 lines.

**Tech Stack:** Go 1.26, standard library only.

## Global Constraints

- No new dependencies — Go stdlib only
- Zero behavior change — pure extraction/merge, no API contract changes
- Tab indentation (project standard, enforced by `gofmt`)
- Commit before each `make check` run
- `os.IsNotExist(err)` must be preserved for the TOML-not-found case (CLI works without server config)

---

### Task 1: Merge `loadAdminToken` and `loadAdminTokenFromConfig`

**Files:**
- Modify: `internal/cli/key.go:645-663` — replace `loadAdminToken` with merged version
- Modify: `internal/cli/reload.go:44-62` — delete `loadAdminTokenFromConfig`; remove unused `akswitch/internal/config` import
- Modify: `internal/cli/admin_client.go:28-32` — simplify call site

**Interfaces:**
- Consumes: `config.XDGConfigPath()`, `config.LoadTomlConfig()`, `os.IsNotExist()`
- Produces: `loadAdminToken(provider string) (string, error)` — single function replacing both originals

- [ ] **Step 1: Replace `loadAdminToken` in key.go with merged version**

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

- [ ] **Step 2: Delete `loadAdminTokenFromConfig` from reload.go**

Remove lines 41-62 (the entire function + its comment). Then remove the unused `"akswitch/internal/config"` import from the import block. Keep `"os"` — it's used by `os.Stderr` in `triggerReload` (line 31).

- [ ] **Step 3: Simplify call site in admin_client.go**

Replace the `if provider != "" { ... } else { ... }` block with a single call:

```go
// Before (lines 28-32):
if provider != "" {
	token, err = loadAdminToken(provider)
} else {
	token, err = loadAdminTokenFromConfig()
}

// After:
token, err = loadAdminToken(provider)
```

- [ ] **Step 4: Build check**

Run: `go build ./internal/cli/`
Expected: success, no errors

- [ ] **Step 5: Unit test**

Run: `go test -tags=unit -count=1 -short ./internal/cli/`
Expected: all tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/cli/key.go internal/cli/reload.go internal/cli/admin_client.go
git commit -m "refactor: 合并 loadAdminToken/loadAdminTokenFromConfig 为一个函数"
```

---

### Task 2: Consolidate log level validation into `config.IsValidLogLevel`

**Files:**
- Modify: `internal/config/field_descriptor.go` — add `IsValidLogLevel` function; update Parse and ApplyRuntime closures for `log_level`
- Modify: `internal/cli/loglevel.go:55-57` — replace inline `validLevels` map
- Modify: `internal/server/admin_api.go:107-108` — replace inline switch
- Test: `internal/config/field_descriptor_test.go` — add `TestIsValidLogLevel` (optional but recommended)

**Interfaces:**
- Consumes: nothing (standalone function, `config` package, no external dependencies)
- Produces: `config.IsValidLogLevel(level string) bool`

- [ ] **Step 1: Add `IsValidLogLevel` to field_descriptor.go**

Add before the `ConfigFieldDescriptors` variable:

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

- [ ] **Step 2: Update `log_level` Parse closure**

In `field_descriptor.go`, replace the `Parse` closure for `log_level`:

```go
// Before (line 309-316):
Parse: func(s string) (any, error) {
	v := strings.TrimSpace(strings.ToLower(s))
	switch v {
	case "debug", "info", "warn", "error":
		return v, nil
	}
	return nil, fmt.Errorf("invalid log level %q, use: debug, info, warn, error", s)
},

// After:
Parse: func(s string) (any, error) {
	v := strings.TrimSpace(strings.ToLower(s))
	if !IsValidLogLevel(v) {
		return nil, fmt.Errorf("invalid log level %q, use: debug, info, warn, error", s)
	}
	return v, nil
},
```

- [ ] **Step 3: Update `log_level` ApplyRuntime closure**

Replace the `ApplyRuntime` closure for `log_level`:

```go
// Before (line 323-335):
ApplyRuntime: func(ps any, provider string, value any) (any, error) {
	s, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("log_level must be a string")
	}
	v := strings.TrimSpace(strings.ToLower(s))
	switch v {
	case "debug", "info", "warn", "error":
		ps.(ProviderRuntimeState).SetLogLevel(v)
		return v, nil
	}
	return nil, fmt.Errorf("invalid log level, use: debug, info, warn, error")
},

// After:
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
```

- [ ] **Step 4: Update loglevel.go (CLI `log-level set`)**

```go
// Before (loglevel.go:54-57):
level := strings.TrimSpace(strings.ToLower(args[0]))
validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
if !validLevels[level] {
	return fmt.Errorf("invalid log level %q, use: debug, info, warn, error", args[0])
}

// After:
level := strings.TrimSpace(strings.ToLower(args[0]))
if !config.IsValidLogLevel(level) {
	return fmt.Errorf("invalid log level %q, use: debug, info, warn, error", args[0])
}
```

Add `"akswitch/internal/config"` to the import block in `loglevel.go`.

- [ ] **Step 5: Update admin_api.go (server `/api/log-level` POST)**

```go
// Before (admin_api.go:106-113):
body.Level = strings.TrimSpace(strings.ToLower(body.Level))
switch body.Level {
case "debug", "info", "warn", "error":
	api.logManager.ApplyLevel(body.Level)
	respondJSON(w, http.StatusOK, map[string]string{"level": body.Level})
default:
	respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log level, use: debug, info, warn, error"})
}

// After:
body.Level = strings.TrimSpace(strings.ToLower(body.Level))
if config.IsValidLogLevel(body.Level) {
	api.logManager.ApplyLevel(body.Level)
	respondJSON(w, http.StatusOK, map[string]string{"level": body.Level})
} else {
	respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log level, use: debug, info, warn, error"})
}
```

Verify `admin_api.go` already imports `akswitch/internal/config` (it does — used by runtime config handler).

- [ ] **Step 6: Add test for IsValidLogLevel**

```go
// Add to field_descriptor_test.go
func TestIsValidLogLevel(t *testing.T) {
	valid := []string{"debug", "info", "warn", "error"}
	for _, l := range valid {
		if !IsValidLogLevel(l) {
			t.Errorf("IsValidLogLevel(%q) = false, want true", l)
		}
	}
	// Case-insensitive normalization is done by callers, not by IsValidLogLevel
	invalid := []string{"", "trace", "fatal", "DEBUG", "INFO"}
	for _, l := range invalid {
		if IsValidLogLevel(l) {
			t.Errorf("IsValidLogLevel(%q) = true, want false", l)
		}
	}
}
```

- [ ] **Step 7: Run tests**

Run: `go test -tags=unit -count=1 -short ./internal/config/ ./internal/cli/ ./internal/server/`
Expected: all tests pass

- [ ] **Step 8: Commit**

```bash
git add internal/config/field_descriptor.go internal/config/field_descriptor_test.go internal/cli/loglevel.go internal/server/admin_api.go
git commit -m "refactor: 提取 IsValidLogLevel 消除 4 处日志级别验证重复"
```

- [ ] **Step 9: Final verification**

Run: `make check && go test -tags=unit -count=1 -short ./internal/...`
Expected: lint passes, vet passes, all unit tests pass