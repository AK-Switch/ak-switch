# PR #287 Code Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 6 issues identified in PR #287 code review

**Architecture:** All 6 fixes are surgical edits to two files: `internal/cli/provider.go` (issues 1, 2, 4, 5) and `internal/cli/config.go` (issues 3, 6). No new files, no architectural changes.

**Tech Stack:** Go 1.26, Cobra CLI, table-driven tests

## Global Constraints

- Tab indentation (gofmt enforced)
- Error wrapping: `fmt.Errorf("func: %w", err)`
- Table-driven tests preferred
- Import order: stdlib → internal → third-party, alphabetical
- No `panic()` in tests
- Tests use `-tags=unit`

---

### Task 1: Fix `backoff_multiplier` range validation

**Files:**
- Modify: `internal/cli/provider.go:371-380`
- Test: `internal/cli/provider_cmd_test.go:188-222`

**Interfaces:**
- Consumes: `config.FindField()`, `hasCLIFlag()`, `getCLIFlagValue()` from provider.go
- Produces: Updated range check that aligns with server-side `admin_api.go:1048`

**Background:** The server's `initRuntimeConfigDescriptors` in `admin_api.go:1048` enforces `backoff_multiplier >= 1.0` (`v < 1.0` returns error). The CLI's range check at `provider.go:371-380` only enforces `>= -1` for both int and float64. This means `--backoff-multiplier 0.5` passes CLI validation, writes to TOML, then the server silently rejects it on reload.

Fix: Change the `float64` case minimum from `-1` to `1`, matching the server-side requirement.

- [ ] **Step 1: Write the failing test**

The existing test `TestProviderUpdateCmd_BackoffMultiplierRangeValidation` at `provider_cmd_test.go:188` tests `< -1` for float64. Change it to test `< 1.0`:

```go
func TestProviderUpdateCmd_BackoffMultiplierRangeValidation(t *testing.T) {
	tmpDir := t.TempDir()

	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{TargetBase: "http://localhost:11434"}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup save config: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	origArgs := os.Args
	os.Args = []string{"akswitch", "provider", "update", "test", "--backoff-multiplier", "0.5"}
	defer func() { os.Args = origArgs }()

	cmd := providerUpdateCmd
	cmd.SetArgs([]string{"test", "--backoff-multiplier", "0.5"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for backoff_multiplier = 0.5, got nil")
	}
	if !strings.Contains(err.Error(), "must be >= 1") {
		t.Errorf("expected '>= 1' error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=unit -count=1 -short -run TestProviderUpdateCmd_BackoffMultiplierRangeValidation ./internal/cli/`
Expected: FAIL — `TestProviderUpdateCmd_BackoffMultiplierRangeValidation` fails because error message says "must be >= -1" not "must be >= 1"

- [ ] **Step 3: Implement the fix**

In `provider.go:376-379`, change the `float64` case:

```go
// old:
case float64:
	if v < -1 {
		return fmt.Errorf("--%s must be >= -1", flagName)
	}

// new:
case float64:
	if v < 1 {
		return fmt.Errorf("--%s must be >= %g", flagName, 1.0)
	}
```

This aligns the CLI validation with `admin_api.go:1048` (`v < 1.0`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags=unit -count=1 -short -run TestProviderUpdateCmd_BackoffMultiplierRangeValidation ./internal/cli/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/provider.go internal/cli/provider_cmd_test.go
git commit -m "fix: backoff_multiplier CLI range check uses >= 1.0 to match server"
```

---

### Task 2: Harden `provider update` field guards (issues 2, 4, 5)

**Files:**
- Modify: `internal/cli/provider.go:333-388`
- Test: `internal/cli/provider_cmd_test.go`

**Interfaces:**
- Consumes: `config.FindField()`, `hasCLIFlag()`, `getCLIFlagValue()`, `fd.ReadOnly`, `fd.RuntimeEditable`
- Produces: Three guard checks added to the field iteration loop

**Background:** Three issues share the same root cause — the field iteration loop in `providerUpdateCmd.RunE` (`provider.go:355-383`) applies all fields without any guard checks. The `config set` command (`config.go:365`) checks `ReadOnly` and `RuntimeEditable` before applying, but `provider update` skips all guards.

- Issue 2: `disable_thinking`/`genai_model` have `RuntimeEditable: false` and silently write to TOML with no effect
- Issue 4: `--target` empty check at line 386 runs after all fields have already been persisted
- Issue 5: `admin_token` has `ReadOnly: true` but `provider update --admin-token xxx` bypasses it

**Fix:**

1. Move `--target` empty check before the field loop (issue 4)
2. Add `fd.ReadOnly` check inside the loop (issue 5) — return error if a ReadOnly field is being updated
3. Add `fd.RuntimeEditable` warning inside the loop (issue 2) — print a warning for non-runtime-editable fields but still persist

- [ ] **Step 1: Write the failing tests**

Add three tests to `provider_cmd_test.go`:

```go
func TestProviderUpdateCmd_ReadOnlyGuard(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"test": {ProviderConfig: config.ProviderConfig{
				TargetBase: "http://localhost:11434",
				AdminToken: "oldtoken",
			}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup save config: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	origArgs := os.Args
	os.Args = []string{"akswitch", "provider", "update", "test", "--admin-token", "newtoken"}
	defer func() { os.Args = origArgs }()

	cmd := providerUpdateCmd
	cmd.SetArgs([]string{"test", "--admin-token", "newtoken"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for ReadOnly field admin_token, got nil")
	}
}

func TestProviderUpdateCmd_TargetEmptyCheckBeforePersist(t *testing.T) {
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
		t.Fatalf("setup save config: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	origArgs := os.Args
	os.Args = []string{"akswitch", "provider", "update", "test", "--cooldown-sec", "30", "--target", ""}
	defer func() { os.Args = origArgs }()

	cmd := providerUpdateCmd
	cmd.SetArgs([]string{"test", "--cooldown-sec", "30", "--target", ""})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for empty --target, got nil")
	}

	// Verify cooldown was NOT persisted (fail-fast before persist)
	loaded, _ := config.LoadTomlConfig(tomlPath)
	if loaded.Provider["test"].CooldownSec != 60 {
		t.Errorf("cooldown should not have changed (expected 60, got %d)", loaded.Provider["test"].CooldownSec)
	}
}

func TestProviderUpdateCmd_NonRuntimeEditableWarning(t *testing.T) {
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
		t.Fatalf("setup save config: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	origArgs := os.Args
	os.Args = []string{"akswitch", "provider", "update", "test", "--disable-thinking", "true"}
	defer func() { os.Args = origArgs }()

	cmd := providerUpdateCmd
	cmd.SetArgs([]string{"test", "--disable-thinking", "true"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error for non-runtime-editable field, got: %v", err)
	}

	// Verify the value was persisted to TOML (the command should still work)
	loaded, _ := config.LoadTomlConfig(tomlPath)
	if !loaded.Provider["test"].DisableThinking {
		t.Error("disable_thinking should have been persisted to TOML")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=unit -count=1 -short -run 'TestProviderUpdateCmd_(ReadOnly|TargetEmpty|NonRuntime)' ./internal/cli/`
Expected: All three tests fail — no guards exist yet

- [ ] **Step 3: Implement the fix**

In `provider.go`, restructure the field loop in `providerUpdateCmd.RunE`:

1. Move the `--target` empty check to **before** the field loop
2. Add `fd.ReadOnly` check at the top of the loop — return error
3. Add `fd.RuntimeEditable` warning inside the loop — print warning but continue

```go
// In providerUpdateCmd.RunE, after loading tc and getting flagToFieldKey:

// Fail-fast: check --target before any field is persisted
if hasCLIFlag("target") {
	prov, _ := tc.Provider[name]
	if prov.TargetBase == "" {
		return fmt.Errorf("--target/-t cannot be empty")
	}
}

for flagName, fieldKey := range flagToFieldKey {
	if !hasCLIFlag(flagName) {
		continue
	}

	fd := config.FindField(fieldKey)
	if fd == nil {
		continue
	}

	// Guard: ReadOnly fields cannot be changed via provider update
	if fd.ReadOnly {
		return fmt.Errorf("%s cannot be changed via provider update — edit the TOML config file and reload", fd.Key)
	}

	valueStr := getCLIFlagValue(flagName)
	parsed, err := fd.Parse(valueStr)
	if err != nil {
		return fmt.Errorf("invalid --%s value: %w", flagName, err)
	}

	switch v := parsed.(type) {
	case int:
		if v < -1 {
			return fmt.Errorf("--%s must be >= -1", flagName)
		}
	case float64:
		if v < 1 {
			return fmt.Errorf("--%s must be >= %g", flagName, 1.0)
		}
	}

	fd.Persist(tc, name, prov, parsed)
	changes++
}
```

Note: Remove the old `--target` check that was after the loop (lines 386-388).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags=unit -count=1 -short -run 'TestProviderUpdateCmd_(ReadOnly|TargetEmpty|NonRuntime)' ./internal/cli/`
Expected: PASS

Also run the full suite to ensure no regressions:
Run: `go test -tags=unit -count=1 -short ./internal/cli/`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/provider.go internal/cli/provider_cmd_test.go
git commit -m "fix: add ReadOnly guard, target validation before persist, non-runtime warning in provider update"
```

---

### Task 3: Fix `config list` and `configGetCmd` help text (issues 3, 6)

**Files:**
- Modify: `internal/cli/config.go:196-208` (config list provider selection)
- Modify: `internal/cli/config.go:230-231` (configGetCmd help text)

**Interfaces:**
- Consumes: `config.ConfigFieldDescriptors` for key list
- Produces: Fixed provider selection logic and updated help text

**Background:** Two related issues in `config.go`:

- Issue 3: `config list` with no args shows ALL providers instead of just the first one. The old behavior (and the command's Long description at line 174-178) says "shows the first (or only) provider" when no args and no `--all`. The current code at line 196-208 falls through to iterate all `names` when `targetProvider == ""`, even without `--all`.
- Issue 6: `configGetCmd` help text at line 230-231 lists only 8 keys but `FindField()` supports all 16 descriptor keys. Seven keys missing: `health_check_interval_sec`, `admin_token`, `disable_thinking`, `genai_model`, `keys_file`, `port`, `log_file`.

- [ ] **Step 1: Write the failing tests**

```go
func TestConfigListCmd_NoArgsShowsFirstProvider(t *testing.T) {
	tmpDir := t.TempDir()
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"alpha": {ProviderConfig: config.ProviderConfig{TargetBase: "http://a.example.com"}},
			"beta":  {ProviderConfig: config.ProviderConfig{TargetBase: "http://b.example.com"}},
		},
	}
	tomlPath := filepath.Join(tmpDir, "config.toml")
	if err := config.SaveTomlConfig(tc, tomlPath); err != nil {
		t.Fatalf("setup save config: %v", err)
	}

	origDir := config.ConfigDir
	defer func() { config.ConfigDir = origDir }()
	config.ConfigDir = tmpDir

	cmd := configListCmd
	cmd.SetArgs([]string{})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("config list should not error when providers exist: %v", err)
	}

	// Should show only the first provider (alpha), not both
	if strings.Contains(output.String(), "Provider: beta") {
		t.Error("config list with no args should show only the first provider, not all providers")
	}
	if !strings.Contains(output.String(), "Provider: alpha") {
		t.Error("config list with no args should show the first provider")
	}
}

func TestConfigGetCmd_HelpTextListsAllKeys(t *testing.T) {
	helpText := configGetCmd.Long
	expectedKeys := []string{
		"http_timeout_sec", "max_retries", "cooldown_sec", "backoff_cap_sec",
		"backoff_multiplier", "cb_reset_sec", "upstream_cb_threshold",
		"health_check_interval_sec", "log_level", "disable_thinking",
		"genai_model", "admin_token", "keys_file",
		"port", "log_file",
	}
	for _, key := range expectedKeys {
		if !strings.Contains(helpText, key) {
			t.Errorf("configGetCmd help text missing key %q", key)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=unit -count=1 -short -run 'TestConfigListCmd_NoArgs|TestConfigGetCmd_HelpText' ./internal/cli/`
Expected: Both tests fail — `config list` shows all providers, help text missing keys

- [ ] **Step 3: Implement the fix**

**Fix 1 — `config list` first provider only (config.go:196-208):**

Change the provider selection logic so that when `targetProvider == ""` and `!all`, only the first provider is shown:

```go
// current:
if all || targetProvider == "" {
	if tc != nil {
		for n := range tc.Provider {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) == 0 && targetProvider == "" {
		return fmt.Errorf("no providers configured")
	}
} else {
	names = []string{targetProvider}
}

// new:
if all {
	if tc != nil {
		for n := range tc.Provider {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("no providers configured")
	}
} else if targetProvider != "" {
	names = []string{targetProvider}
} else {
	// No args, no --all: show the first (or only) provider
	if tc != nil {
		for n := range tc.Provider {
			names = append(names, n)
		}
		sort.Strings(names)
	}
	if len(names) == 0 {
		return fmt.Errorf("no providers configured")
	}
	names = []string{names[0]}
}
```

**Fix 2 — `configGetCmd` help text (config.go:230-231):**

Update the help text to list all 16 valid keys:

```
Valid keys: port, log_file, target, cooldown_sec, max_retries,
backoff_cap_sec, backoff_multiplier, cb_reset_sec, upstream_cb_threshold,
http_timeout_sec, health_check_interval_sec, log_level,
disable_thinking, genai_model, admin_token, keys_file
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags=unit -count=1 -short -run 'TestConfigListCmd_NoArgs|TestConfigGetCmd_HelpText' ./internal/cli/`
Expected: PASS

Also run the full suite:
Run: `go test -tags=unit -count=1 -short ./internal/cli/`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/config.go internal/cli/config_cmd_test.go
git commit -m "fix: config list shows first provider when no args; configGetCmd help lists all keys"
```

---

### Final Verification

After all tasks complete:

- [ ] Run full test suite: `go test -tags=unit -count=1 ./...`
- [ ] Run lint: `make check`
- [ ] Push to remote branch
