# CLI Config Refactor — Final Review Fixes

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 4 findings from final whole-branch review of the CLI config refactor

**Architecture:** Small targeted fixes to existing files. No new files, no structural changes.

**Tech Stack:** Go, Cobra, go-toml/v2

## Global Constraints

- Tab indentation (gofmt enforced)
- Atomic commits — one commit per fix
- `go test -tags=unit -count=1 ./internal/config/ ./internal/cli/ ./internal/server/` must pass after each task
- No new config field names or descriptor changes

---

### Task 1: Sanitize admin_token output in config get

**Files:**
- Modify: `internal/cli/config.go:486`

**Interfaces:**
- Consumes: `getFieldValue()` returns raw `AdminToken` string
- Produces: `configGetCmd` prints masked value instead of raw

- [ ] **Step 1: Write the failing test**

In `internal/cli/config_cmd_test.go`, add a test that verifies `config get admin_token` does not output the raw token:

```go
func TestConfigGetCmd_AdminTokenMasked(t *testing.T) {
    // Setup: config with a known admin_token
    // Execute: config get admin_token <provider>
    // Assert: output does NOT contain the raw token string
    // Assert: output DOES contain a masked indicator (e.g., "(set)")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=unit -count=1 -short -run TestConfigGetCmd_AdminTokenMasked ./internal/cli/`
Expected: FAIL — output contains raw admin_token

- [ ] **Step 3: Implement the fix**

In `config.go`, after `getFieldValue` returns the value for `admin_token`, mask it before printing:

```go
case "admin_token":
    if rawToken != "" {
        fmt.Println("(set)")
    } else {
        fmt.Println("(not set)")
    }
```

Modify the print path in `configGetCmd` (around line 274) to handle `admin_token` specially:

```go
if key == "admin_token" {
    if rawToken != "" {
        fmt.Println("(set)")
    } else {
        fmt.Println("(not set)")
    }
} else {
    fmt.Println(fd.Format(val))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags=unit -count=1 -short -run TestConfigGetCmd_AdminTokenMasked ./internal/cli/`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go test -tags=unit -count=1 -short ./internal/config/ ./internal/cli/ ./internal/server/`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/config.go internal/cli/config_cmd_test.go
git commit -m "fix: mask admin_token output in config get command"
```

---

### Task 2: Remove duplicate parseDefault, use config.parseDefault

**Files:**
- Modify: `internal/cli/config.go:502-516`

**Interfaces:**
- Consumes: `config.parseDefault` exists in `internal/config/field_descriptor.go:402`
- Produces: `cli/config.go` calls `config.parseDefault` instead of local duplicate

- [ ] **Step 1: Write the failing test**

Existing `TestSaveTomlConfig_OmitZeroValues` and descriptor tests already cover `parseDefault` behavior. Add a test confirming `cli/config.go` delegates to `config.parseDefault`:

```go
func TestConfigGetCmd_UsesSharedParseDefault(t *testing.T) {
    // Verify that getFieldValue for a missing field uses config.parseDefault
    // by checking the returned value matches the descriptor's Default string
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=unit -count=1 -short -run TestConfigGetCmd_UsesSharedParseDefault ./internal/cli/`
Expected: FAIL (local parseDefault may return different result — actually it won't fail since behavior is identical. Skip this test and just verify the change compiles.)

- [ ] **Step 3: Implement the fix**

In `cli/config.go`, replace the local `parseDefault` function with a call to `config.parseDefault`:

```go
// Remove local parseDefault function (lines 502-516)
// Replace calls to parseDefault(fd) with config.parseDefault(fd)
```

- [ ] **Step 4: Run tests to verify**

Run: `go test -tags=unit -count=1 -short ./internal/config/ ./internal/cli/`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/config.go
git commit -m "refactor: use config.parseDefault instead of duplicate in cli/config.go"
```

---

### Task 3: Fix admin_token fallthrough in getFieldValue

**Files:**
- Modify: `internal/cli/config.go:472-476`

**Interfaces:**
- Consumes: `getFieldValue` switch on fd.Key
- Produces: explicit return for empty admin_token instead of implicit fallthrough

- [ ] **Step 1: Add test for empty admin_token**

In `internal/cli/config_cmd_test.go`, add:

```go
func TestGetFieldValue_AdminTokenEmpty(t *testing.T) {
    tc := &config.TomlConfig{
        Provider: map[string]*config.Config{
            "test": {ProviderConfig: config.ProviderConfig{
                AdminToken: "",
            }},
        },
    }
    fd := config.FindField("admin_token")
    val, err := getFieldValue(tc, "test", fd)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    // Should return empty string, not fall through to default
    if val != "" {
        t.Errorf("expected empty string for empty admin_token, got %v", val)
    }
}
```

- [ ] **Step 2: Run test to verify behavior**

Run: `go test -tags=unit -count=1 -short -run TestGetFieldValue_AdminTokenEmpty ./internal/cli/`
Expected: PASS (current behavior already returns empty string via fallthrough, but test documents the expected behavior)

- [ ] **Step 3: Make fallthrough explicit**

In `config.go`, change:

```go
case "admin_token":
    if p.AdminToken != "" {
        return p.AdminToken, nil
    }
```

To:

```go
case "admin_token":
    if p.AdminToken != "" {
        return p.AdminToken, nil
    }
    return "", nil
```

- [ ] **Step 4: Run tests to verify**

Run: `go test -tags=unit -count=1 -short ./internal/cli/`
Expected: ALL PASS (no behavior change)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/config.go internal/cli/config_cmd_test.go
git commit -m "fix: add explicit return for empty admin_token in getFieldValue"
```

---

### Task 4: Verify build passes

**Files:**
- None

- [ ] **Step 1: Full test suite**

Run: `go test -tags=unit -count=1 -short ./...`
Expected: ALL PASS

- [ ] **Step 2: Vet**

Run: `go vet ./...`
Expected: no errors

- [ ] **Step 3: Build**

Run: `make build`
Expected: success, `bin/akswitch` produced

- [ ] **Step 4: Verify no unintended changes**

Run: `git diff --stat`
Expected: only the 4 fix commits' files
