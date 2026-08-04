# #239 CLI start.go 启动逻辑提取 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract server startup logic from `internal/cli/start.go` (355 lines) into a new `ServerLauncher` in `internal/server/server_launcher.go`, reducing CLI layer to ~80 lines.

**Architecture:** `ServerLauncher` struct holds startup parameters as primitives. `Launch()` orchestrates the full sequence. CLI `start.go` only translates flags → parameters → calls `Launch()`.

**Tech Stack:** Go, Cobra CLI (unchanged), existing `server` package

## Global Constraints

- `go vet` clean, all existing tests pass after migration
- No changes to `start` subcommand flags or behavior
- No changes to `selfrestart.go` public API
- No Cobra import in `server` package
- No import cycle (`server` must not import `cli`)

---

### Task 1: Create server_launcher.go with ServerLauncher struct and all migrated functions

**Files:**
- Create: `internal/server/server_launcher.go`
- Modify: `internal/cli/start.go`
- Test: `go test -tags=unit -count=1 ./...`

**Interfaces:**
- Consumes: `config.Config`, `server.ProviderRouter`, `keypool.KeyPool` (all existing)
- Produces: `server.NewServerLauncher()`, `(*ServerLauncher).Launch()`

- [ ] **Step 1: Create server_launcher.go with struct + Launch()**

Write `internal/server/server_launcher.go` with:
- `ServerLauncher` struct (6 fields, all string/bool)
- `NewServerLauncher()` constructor
- `Launch()` method that calls `checkPidFile`, `resolveProviders`, `initProviders`, `writePIDFile`, `waitForShutdown`

The `Launch()` body is the `startServer()` body from `start.go` minus the `defer server.CrashRecover` (that stays in CLI) and minus the `logCompact` flag logic (passes through).

- [ ] **Step 2: Migrate resolveProviders to ServerLauncher**

Move `resolveProviders()` from `start.go` to `server_launcher.go` as `sl.resolveProviders()` method. Signature change: `(dashboardHTML, providerFilter string, startAll bool) (router, providers, port, host, shouldStart, error)`.

The method returns error instead of calling `os.Exit`. `dashboardHTML` is passed as a parameter instead of being a closure variable.

- [ ] **Step 3: Migrate initProviders to ServerLauncher**

Move `initProviders()` from `start.go` to `server_launcher.go` as `sl.initProviders()` method. Signature: `(router *server.ProviderRouter, providers map[string]*config.Config, shouldStart func(string) bool, providerFilter string, logCompact bool) error`.

Add error return for `AddProvider` failures (currently just logs and continues — keep that behavior, return nil).

- [ ] **Step 4: Migrate PID management functions**

Move `writePIDFile()`, `pidFilePath()`, `checkPidFile()` from `start.go` to `server_launcher.go` as methods on `*ServerLauncher`.

- [ ] **Step 5: Migrate loadKeysForProvider to ServerLauncher**

Move `loadKeysForProvider()` to `server_launcher.go` as `sl.loadKeysForProvider()`.

- [ ] **Step 6: Migrate waitForShutdown to ServerLauncher**

Move `waitForShutdown()` to `server_launcher.go` as `sl.waitForShutdown(router *server.ProviderRouter) error`.

Change `os.Exit` calls to return errors. The `ExecRestart()` call stays (it's a `cli` function).

- [ ] **Step 7: Verify compilation**

Run: `go vet ./internal/server/`
Expected: clean

- [ ] **Step 8: Run existing tests**

Run: `go test -tags=unit -count=1 ./...`
Expected: all pass

- [ ] **Step 9: Commit**

```bash
git add internal/server/server_launcher.go
git commit -m "refactor: extract ServerLauncher from CLI start.go (#239)"
```

---

### Task 2: Simplify start.go to CLI-only

**Files:**
- Modify: `internal/cli/start.go`
- Test: `go test -tags=unit -count=1 ./internal/cli/`

**Interfaces:**
- Consumes: `server.NewServerLauncher()`, `(*ServerLauncher).Launch()`
- Produces: Slim `start.go` (~80 lines)

- [ ] **Step 1: Remove migrated functions from start.go**

Delete `startServer()`, `resolveProviders()`, `initProviders()`, `writePIDFile()`, `waitForShutdown()`, `loadKeysForProvider()`, `pidFilePath()`, `checkPidFile()` from `start.go`.

Remove unused imports: `context`, `net`, `path/filepath`, `runtime`, `strconv`, `syscall`, `time`, `maps`, `slices`, `keypool` (verify after deletion).

Keep: `fmt`, `log/slog`, `os`, `os/exec`, `strings`, `config`, `server`, `cobra`, and `selfrestart` functions.

- [ ] **Step 2: Update startCmd Run function**

```go
Run: func(cmd *cobra.Command, args []string) {
    providerFilter, _ := cmd.Flags().GetString("provider")
    startAll, _ := cmd.Flags().GetBool("all")
    devMode, _ := cmd.Flags().GetBool("dev")
    logFormat, _ := cmd.Flags().GetString("log-format")
    logLevel, _ := cmd.Flags().GetString("log-level")
    sl := server.NewServerLauncher(dashHTML, providerFilter, startAll, devMode, logFormat, logLevel)
    if err := sl.Launch(); err != nil {
        slog.Error("failed to start server", "error", err)
        os.Exit(1)
    }
},
```

- [ ] **Step 3: Verify compilation**

Run: `go vet ./internal/cli/`
Expected: clean

- [ ] **Step 4: Run CLI tests**

Run: `go test -tags=unit -count=1 ./internal/cli/`
Expected: all pass

- [ ] **Step 5: Run full test suite**

Run: `go test -tags=unit -count=1 ./...`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/cli/start.go
git commit -m "refactor: simplify start.go to CLI-only flag translation (#239)"
```

---

### Task 3: Add tests for ServerLauncher

**Files:**
- Create: `internal/server/server_launcher_test.go`
- Test: `go test -tags=unit -count=1 ./internal/server/`

**Interfaces:**
- Consumes: `ServerLauncher`, `pidFilePath()`, `checkPidFile()`

- [ ] **Step 1: Write test for pidFilePath**

```go
func TestServerLauncher_pidFilePath(t *testing.T) {
    sl := &ServerLauncher{}
    path := sl.pidFilePath(false)
    if !strings.HasSuffix(path, "akswitch.pid") {
        t.Errorf("expected akswitch.pid suffix, got %s", path)
    }
}
```

- [ ] **Step 2: Write test for checkPidFile with temp file**

```go
func TestServerLauncher_checkPidFile(t *testing.T) {
    sl := &ServerLauncher{}
    // Non-existent file
    running, pid := sl.checkPidFile("/nonexistent/pid")
    if running || pid != 0 {
        t.Errorf("non-existent file should return (false, 0), got (%v, %d)", running, pid)
    }
    // Write fake PID
    tmpDir := t.TempDir()
    pidFile := filepath.Join(tmpDir, "test.pid")
    os.WriteFile(pidFile, []byte("99999\n"), 0644)
    running, pid = sl.checkPidFile(pidFile)
    if running {
        t.Errorf("PID 99999 should not be running, got running=%v", running)
    }
    if pid != 99999 {
        t.Errorf("expected pid=99999, got %d", pid)
    }
}
```

- [ ] **Step 3: Write test for NewServerLauncher**

```go
func TestNewServerLauncher(t *testing.T) {
    sl := NewServerLauncher("html", "provider1", true, false, "compact", "debug")
    if sl.providerFilter != "provider1" {
        t.Errorf("providerFilter = %q, want %q", sl.providerFilter, "provider1")
    }
    if !sl.startAll {
        t.Error("startAll should be true")
    }
    if sl.logFormat != "compact" {
        t.Errorf("logFormat = %q, want %q", sl.logFormat, "compact")
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go test -tags=unit -count=1 ./internal/server/`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add internal/server/server_launcher_test.go
git commit -m "test: add ServerLauncher tests (#239)"
```

---

## Verification

After all tasks:

1. `go test -tags=unit -count=1 ./...` — all pass
2. `go vet ./...` — clean
3. `go build ./cmd/akswitch/` — clean
4. `internal/cli/start.go` line count < 100
