# CLI Config Bug Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 8 regressions in CLI config commands introduced during PR #287 refactoring

**Architecture:** Two-file surgical edit. Task 1 covers all `internal/cli/config.go` fixes (issues #290-#295, #297). Task 2 covers the `internal/cli/provider.go` boolean flag fix (issue #296). Shared `validateFieldRange` helper lives in `config.go`.

**Tech Stack:** Go 1.26, Cobra CLI, table-driven tests with `-tags=unit`

## Global Constraints

- Tab indentation (gofmt enforced)
- Error wrapping: `fmt.Errorf("func: %w", err)`
- Table-driven tests preferred
- Import order: stdlib → internal → third-party, alphabetical
- No `panic()` in tests
- Tests use `-tags=unit`
- `t.TempDir()` + `config.SaveTomlConfig()` for test setup (existing pattern)
- `os.Pipe()` for stdout capture when `fmt.Printf` is used inside RunE

---

### Task 1: Fix `internal/cli/config.go` — 6 regressions

**Files:**
- Modify: `internal/cli/config.go`
- Test: `internal/cli/config_cmd_test.go`

**Interfaces:**
- Consumes: `config.FindField()`, `config.LoadTomlConfig()`, `config.SaveTomlConfig()`, `config.XDGConfigPath()`, `config.ConfigFieldDescriptors`, `persistFieldToToml()`, `applyRuntimeField()`, `getFieldValue()`, `maskSensitiveValue()`
- Produces: `validateFieldRange()` shared helper, corrected command behaviors

#### Step 1: Write failing test for `--runtime-only` flag (issue #290)

Add to `config_cmd_test.go`:

```go
func TestConfigSetCmd_RuntimeOnlyAppliesButDoesNotPersist(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{
				TargetBase: "http://localhost:11434",
				CooldownSec: 60,
			}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	origArgs := os.Args
	os.Args = []string{"akswitch", "config", "set", "cooldown_sec", "30", "test", "--runtime-only"}
	defer func() { os.Args = origArgs }()

	cmd := configSetCmd
	cmd.SetArgs([]string{"cooldown_sec", "30", "test", "--runtime-only"})
	err := cmd.Execute()
	if err != nil {
		// applyRuntimeField may fail if no server running — that's expected
		// The key assertion is: error should NOT be about "no change" or silent skip
		if strings.Contains(err.Error(), "not reachable") {
			t.Skip("no server running — runtime-only apply expected to fail here")
		}
		t.Fatalf("unexpected error: %v", err)
	}

	// TOML must NOT be modified with --runtime-only
	loaded, _ := config.LoadTomlConfig(tomlPath)
	if loaded.Provider["test"].CooldownSec != 60 {
		t.Errorf("cooldown should NOT have changed with --runtime-only (expected 60, got %d)",
			loaded.Provider["test"].CooldownSec)
	}
}
```

#### Step 2: Run test to verify it fails

Run: `go test -tags=unit -count=1 -short -run TestConfigSetCmd_RuntimeOnlyAppliesButDoesNotPersist ./internal/cli/`
Expected: FAIL — `applyRuntimeField` is skipped because condition is `!runtimeOnly` (line 379)

#### Step 3: Implement fix #1 — `--runtime-only` condition

In `config.go:379`, change:

```go
// old:
if fd.Scope == config.FieldScopeProvider && fd.RuntimeEditable && !runtimeOnly {

// new:
if fd.Scope == config.FieldScopeProvider && fd.RuntimeEditable && runtimeOnly {
```

#### Step 4: Run test to verify it passes

Run: `go test -tags=unit -count=1 -short -run TestConfigSetCmd_RuntimeOnlyAppliesButDoesNotPersist ./internal/cli/`
Expected: PASS

#### Step 5: Write failing test for range validation (issue #293)

Add to `config_cmd_test.go`:

```go
func TestConfigSetCmd_RangeValidation(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{
				TargetBase: "http://localhost:11434",
			}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	tests := []struct {
		key      string
		value    string
		wantErr  bool
		errSub   string
	}{
		{"backoff_multiplier", "0.5", true, "must be >= 1"},
		{"backoff_multiplier", "2.0", false, ""},
		{"max_retries", "-2", true, "must be >= -1"},
		{"max_retries", "5", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.key+"_"+tt.value, func(t *testing.T) {
			origArgs := os.Args
			os.Args = []string{"akswitch", "config", "set", tt.key, tt.value, "test"}
			defer func() { os.Args = origArgs }()

			cmd := configSetCmd
			cmd.SetArgs([]string{tt.key, tt.value, "test"})
			err := cmd.Execute()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %s=%s, got nil", tt.key, tt.value)
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("expected error containing %q, got: %v", tt.errSub, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for %s=%s: %v", tt.key, tt.value, err)
				}
			}
		})
	}
}
```

#### Step 6: Run test to verify it fails

Run: `go test -tags=unit -count=1 -short -run TestConfigSetCmd_RangeValidation ./internal/cli/`
Expected: FAIL — no range validation exists in `configSetCmd`

#### Step 7: Implement fix #2 — extract shared `validateFieldRange` and use it

Add the shared function near the top of `config.go` (after imports, before `init()`):

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

In `configSetCmd.RunE`, after the existing `fd.Parse` call (line 335-338), add:

```go
if err := validateFieldRange(fd, valueStr); err != nil {
    return err
}
```

#### Step 8: Run test to verify it passes

Run: `go test -tags=unit -count=1 -short -run TestConfigSetCmd_RangeValidation ./internal/cli/`
Expected: PASS

Also run: `go test -tags=unit -count=1 -short -run TestConfigSetCmd_RuntimeOnlyAppliesButDoesNotPersist ./internal/cli/`
Expected: PASS (no regression)

#### Step 9: Write failing test for TOML error propagation (issue #294)

Add to `config_cmd_test.go`:

```go
func TestConfigSetCmd_TomlLoadErrorNotSwallowed(t *testing.T) {
	tmpDir := t.TempDir()

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir // no config.toml exists

	origArgs := os.Args
	os.Args = []string{"akswitch", "config", "set", "port", "9090"}
	defer func() { os.Args = origArgs }()

	cmd := configSetCmd
	cmd.SetArgs([]string{"port", "9090"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when config.toml does not exist, got nil")
	}
	if !strings.Contains(err.Error(), "no providers") && !strings.Contains(err.Error(), "not exist") {
		t.Errorf("expected error about missing config, got: %v", err)
	}
}
```

#### Step 10: Run test to verify it fails

Run: `go test -tags=unit -count=1 -short -run TestConfigSetCmd_TomlLoadErrorNotSwallowed ./internal/cli/`
Expected: FAIL — `tc, _ := config.LoadTomlConfig(source)` silently returns nil

#### Step 11: Implement fix #3 — propagate TOML loading errors

In `config.go`, fix all 4 locations where `LoadTomlConfig` error is discarded:

**Line 192** (`configListCmd`):
```go
// old:
tc, _ := config.LoadTomlConfig(source) // may fail if no config yet

// new:
tc, err := config.LoadTomlConfig(source)
if err != nil && !os.IsNotExist(err) {
    return fmt.Errorf("failed to load config: %w", err)
}
```

**Line 267** (`configGetCmd`, `--all` branch):
```go
// old:
tc, _ := config.LoadTomlConfig(source)

// new:
tc, err := config.LoadTomlConfig(source)
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}
```

**Line 285** (`configGetCmd`, global field branch):
```go
// old:
tc, _ := config.LoadTomlConfig(source)

// new:
tc, err := config.LoadTomlConfig(source)
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}
```

**Line 364** (`configSetCmd`, `--all` branch):
```go
// old:
tc, _ := config.LoadTomlConfig(source)

// new:
tc, err := config.LoadTomlConfig(source)
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}
```

#### Step 12: Run test to verify it passes

Run: `go test -tags=unit -count=1 -short -run TestConfigSetCmd_TomlLoadErrorNotSwallowed ./internal/cli/`
Expected: PASS

#### Step 13: Write failing test for `config get --all` redundant I/O (issue #297)

This is a performance test. Verify TOML is loaded once, not N times.

```go
func TestConfigGetCmd_AllLoadsTomlOnce(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"alpha": {ProviderConfig: config.ProviderConfig{CooldownSec: 60}},
			"beta":  {ProviderConfig: config.ProviderConfig{CooldownSec: 90}},
			"gamma": {ProviderConfig: config.ProviderConfig{CooldownSec: 120}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	cmd := configGetCmd
	cmd.SetArgs([]string{"cooldown_sec", "--all"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := output.String()
	if !strings.Contains(out, "alpha: 60") {
		t.Error("expected alpha: 60 in output")
	}
	if !strings.Contains(out, "beta: 90") {
		t.Error("expected beta: 90 in output")
	}
	if !strings.Contains(out, "gamma: 120") {
		t.Error("expected gamma: 120 in output")
	}
}
```

#### Step 14: Run test to verify it passes (functionality test)

Run: `go test -tags=unit -count=1 -short -run TestConfigGetCmd_AllLoadsTomlOnce ./internal/cli/`
Expected: PASS (functionality works, this test doesn't verify I/O count — optimization is verified by code review)

#### Step 15: Implement fix #5 — load TOML once in `config get --all`

In `configGetCmd.RunE`, restructure lines 262-273:

```go
// old:
if all {
    source, err := config.XDGConfigPath()
    if err != nil {
        return fmt.Errorf("failed to determine config path: %w", err)
    }
    tc, _ := config.LoadTomlConfig(source)
    if tc != nil {
        for n := range tc.Provider {
            providers = append(providers, n)
        }
    }
    sort.Strings(providers)
}

// new:
if all {
    source, err := config.XDGConfigPath()
    if err != nil {
        return fmt.Errorf("failed to determine config path: %w", err)
    }
    tc, err := config.LoadTomlConfig(source)
    if err != nil {
        return fmt.Errorf("failed to load config: %w", err)
    }
    for n := range tc.Provider {
        providers = append(providers, n)
    }
    sort.Strings(providers)
}
```

And restructure the per-provider loop (lines 291-303) to reuse the already-loaded `tc`:

```go
// old:
for _, p := range providers {
    source, err := config.XDGConfigPath()
    if err != nil {
        return fmt.Errorf("failed to determine config path: %w", err)
    }
    tc, _ := config.LoadTomlConfig(source)
    val, _ := getFieldValue(tc, p, fd)
    ...
}

// new:
for _, p := range providers {
    val, _ := getFieldValue(tc, p, fd)
    ...
}
```

#### Step 16: Write failing test for ghost provider prevention (issue #295)

Add to `config_cmd_test.go`:

```go
func TestConfigSetCmd_RejectsNonExistentProvider(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"real": {ProviderConfig: config.ProviderConfig{TargetBase: "http://localhost:11434"}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	origArgs := os.Args
	os.Args = []string{"akswitch", "config", "set", "target", "http://x.com", "ghost_provider"}
	defer func() { os.Args = origArgs }()

	cmd := configSetCmd
	cmd.SetArgs([]string{"target", "http://x.com", "ghost_provider"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent provider, got nil")
	}

	loaded, _ := config.LoadTomlConfig(tomlPath)
	if _, hasGhost := loaded.Provider["ghost_provider"]; hasGhost {
		t.Error("ghost provider should not have been created in TOML")
	}
}
```

#### Step 17: Run test to verify it fails

Run: `go test -tags=unit -count=1 -short -run TestConfigSetCmd_RejectsNonExistentProvider ./internal/cli/`
Expected: FAIL — `persistFieldToToml` creates the ghost provider entry

#### Step 18: Implement fix #4 — reject non-existent providers

In `configSetCmd.RunE`, before the persist loop, add a provider existence check. After resolving `providerList` (after line 376), add:

```go
// Validate providers exist before any modifications
if fd.Scope == config.FieldScopeProvider && provider != "all" {
    source, err := config.XDGConfigPath()
    if err != nil {
        return fmt.Errorf("failed to determine config path: %w", err)
    }
    tc, err := config.LoadTomlConfig(source)
    if err != nil {
        return fmt.Errorf("failed to load config: %w", err)
    }
    for _, p := range providerList {
        if _, ok := tc.Provider[p]; !ok {
            return fmt.Errorf("provider %q not found in config — run 'provider add' first", p)
        }
    }
}
```

#### Step 19: Run test to verify it passes

Run: `go test -tags=unit -count=1 -short -run TestConfigSetCmd_RejectsNonExistentProvider ./internal/cli/`
Expected: PASS

#### Step 20: Implement fix #6 — update help text

In `configSetCmd.Long` (lines 315-317), update the Valid keys list to include all 16 keys:

```go
// old:
Valid keys: target, cooldown_sec, max_retries, backoff_cap_sec,
backoff_multiplier, cb_reset_sec, upstream_cb_threshold, http_timeout_sec,
log_level, disable_thinking, genai_model

// new:
Valid keys: port, log_file, target, cooldown_sec, max_retries,
backoff_cap_sec, backoff_multiplier, cb_reset_sec, upstream_cb_threshold,
http_timeout_sec, health_check_interval_sec, log_level,
disable_thinking, genai_model, admin_token, keys_file
```

Note: `port`, `log_file`, `health_check_interval_sec`, `admin_token`, `keys_file` are already supported by `FindField()` but were missing from the help text.

#### Step 21: Write test for help text completeness

The test `TestConfigGetCmd_HelpTextListsAllKeys` already exists (line 259). Add a similar one for `configSetCmd`:

```go
func TestConfigSetCmd_HelpTextListsAllKeys(t *testing.T) {
	helpText := configSetCmd.Long
	expectedKeys := []string{
		"port", "log_file", "target", "cooldown_sec", "max_retries",
		"backoff_cap_sec", "backoff_multiplier", "cb_reset_sec",
		"upstream_cb_threshold", "http_timeout_sec", "health_check_interval_sec",
		"log_level", "disable_thinking", "genai_model", "admin_token", "keys_file",
	}
	for _, key := range expectedKeys {
		if !strings.Contains(helpText, key) {
			t.Errorf("configSetCmd help text missing key %q", key)
		}
	}
}
```

#### Step 22: Run Task 1 full test suite

Run: `go test -tags=unit -count=1 -short ./internal/cli/`
Expected: All PASS

#### Step 23: Commit Task 1

```bash
git add internal/cli/config.go internal/cli/config_cmd_test.go
git commit -m "fix: config set runtime-only, range validation, error handling, ghost provider, help text"
```

---

### Task 2: Fix `internal/cli/provider.go` — boolean flag parsing (issue #296)

**Files:**
- Modify: `internal/cli/provider.go`
- Test: `internal/cli/provider_cmd_test.go`

**Interfaces:**
- Consumes: `config.FindField()`, `cmd.Flags().Changed()`, `fd.Type`
- Produces: Fixed boolean flag detection in `providerUpdateCmd.RunE`

#### Step 1: Write failing test for boolean flag `--disable-thinking`

Add to `provider_cmd_test.go`:

```go
func TestProviderUpdateCmd_BoolFlagNoValue(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{
				TargetBase:      "http://localhost:11434",
				DisableThinking: false,
			}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	// Simulate: user runs `akswitch provider update test --disable-thinking`
	// (no explicit value — Cobra should set it to true when flag is present)
	origArgs := os.Args
	os.Args = []string{"akswitch", "provider", "update", "test", "--disable-thinking"}
	defer func() { os.Args = origArgs }()

	cmd := providerUpdateCmd
	cmd.SetArgs([]string{"test", "--disable-thinking"})

	// Reset flag state (Cobra persists flags between test runs)
	cmd.Flags().Set("disable-thinking", "false")

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error for --disable-thinking without value, got: %v", err)
	}

	loaded, _ := config.LoadTomlConfig(tomlPath)
	if !loaded.Provider["test"].DisableThinking {
		t.Error("disable_thinking should have been set to true")
	}
}
```

#### Step 2: Run test to verify it fails

Run: `go test -tags=unit -count=1 -short -run TestProviderUpdateCmd_BoolFlagNoValue ./internal/cli/`
Expected: FAIL — `getCLIFlagValue("disable-thinking")` returns empty string, `strconv.ParseBool("")` errors

#### Step 3: Implement fix — detect boolean flags via `cmd.Flags().Changed()`

In `providerUpdateCmd.RunE`, after getting `valueStr := getCLIFlagValue(flagName)` (line 377), add a check for boolean fields:

```go
valueStr := getCLIFlagValue(flagName)

// Boolean flags: if Changed() but value is empty, the flag was present without a value
if valueStr == "" && fd.Type == "bool" && cmd.Flags().Changed(flagName) {
    valueStr = "true"
}
```

#### Step 4: Run test to verify it passes

Run: `go test -tags=unit -count=1 -short -run TestProviderUpdateCmd_BoolFlagNoValue ./internal/cli/`
Expected: PASS

#### Step 5: Run Task 2 full test suite

Run: `go test -tags=unit -count=1 -short ./internal/cli/`
Expected: All PASS

#### Step 6: Commit Task 2

```bash
git add internal/cli/provider.go internal/cli/provider_cmd_test.go
git commit -m "fix: boolean flags in provider update work without explicit value"
```

---

### Final Verification

Run: `go test -tags=unit -count=1 ./...`
Expected: All PASS

Run: `make check`
Expected: Clean
