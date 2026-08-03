# Config 结构体重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split Config into ProviderConfig (embedded) + RuntimeConfig (standalone type), preserving backward compatibility

**Architecture:** Config embeds ProviderConfig (all TOML/programmatic fields). RuntimeConfig is a standalone type for runtime-config endpoint grouping. DefaultConfig/mergeDefaults/Validate/Sanitized move to their respective types. Config retains compatibility wrappers.

**Tech Stack:** Go, reflection for mergeDefaults

## Global Constraints

- `go vet` clean, all existing tests pass after migration
- No changes to public API signatures (LoadAllTomlProviders, LoadTomlConfig, SaveTomlConfig)
- No changes to config.toml format
- `Config.FieldName` access pattern unchanged for all callers
- `//go:build unit` tag on all test files

---

### Task 1: Split Config struct and move methods in config.go

**Files:**
- Modify: `internal/config/config.go` (entire file)
- Test: `internal/config/config_test.go` (no changes needed yet)

**Interfaces:**
- Consumes: Current Config struct layout, DefaultConfig(), mergeDefaults(), Validate(), Sanitized()
- Produces: `ProviderConfig` struct, `RuntimeConfig` struct, `DefaultProviderConfig()`, `DefaultRuntimeConfig()`, `ProviderConfig.Validate()`, `RuntimeConfig.Validate()`, `Config.Validate()`, `ProviderConfig.mergeDefaults()`

- [ ] **Step 1: Read current config.go to confirm exact field layout**

Run: `wc -l internal/config/config.go`
Expected: ~203 lines

- [ ] **Step 2: Write the struct split**

Replace the `Config` struct with `ProviderConfig` struct, then `Config` embedding `ProviderConfig`, then `RuntimeConfig` as standalone type.

```go
type ProviderConfig struct {
    Port                 int      `toml:"port" default:"8080"`
    Host                 string   `toml:"host,omitempty" default:"127.0.0.1"`
    TargetBase           string   `toml:"target"`
    AdminToken           string   `toml:"admin_token,omitempty"`
    DisableThinking      bool     `toml:"disable_thinking,omitempty"`
    GenaiModel           string   `toml:"genai_model,omitempty"`
    MaxRetries           int      `toml:"max_retries" default:"1"`
    LogLevel             string   `toml:"log_level,omitempty" default:"info"`
    CooldownSec          int      `toml:"cooldown_sec" default:"15"`
    HTTPTimeoutSec       int      `toml:"http_timeout_sec" default:"30"`
    Keys                 []string `toml:"-"`
    KeyNames             []string `toml:"-"`
    KeysFile             string   `toml:"keys_file,omitempty" default:"keys.json"`
    BackoffCapSec        int      `toml:"backoff_cap_sec" default:"120"`
    BackoffMultiplier    float64  `toml:"backoff_multiplier" default:"2"`
    CBResetSec           int      `toml:"cb_reset_sec" default:"30"`
    UpstreamCBThreshold  int      `toml:"upstream_cb_threshold" default:"5"`
    HealthCheckIntervalSec int    `toml:"health_check_interval_sec" default:"30"`
    HealthCheckPath       string  `toml:"-" default:"/health"`
    HealthCheckTimeoutSec int     `toml:"-" default:"5"`
    LogFile    string `toml:"log_file,omitempty"`
    LogMaxSize int    `toml:"log_max_size" default:"100"`
    LogMaxAge  int    `toml:"log_max_age" default:"7"`
    CalibrationIntervalSec int `toml:"calibration_interval_sec" default:"3600"`
}

type Config struct {
    ProviderConfig
}

type RuntimeConfig struct {
    HTTPTimeoutSec      int     `toml:"http_timeout_sec"`
    MaxRetries          int     `toml:"max_retries"`
    CooldownSec         int     `toml:"cooldown_sec"`
    BackoffCapSec       int     `toml:"backoff_cap_sec"`
    BackoffMultiplier   float64 `toml:"backoff_multiplier"`
    CBResetSec          int     `toml:"cb_reset_sec"`
    UpstreamCBThreshold int     `toml:"upstream_cb_threshold"`
    LogLevel            string  `toml:"log_level,omitempty"`
}
```

- [ ] **Step 3: Add DefaultProviderConfig and DefaultRuntimeConfig**

```go
func DefaultProviderConfig() *ProviderConfig {
    return &ProviderConfig{
        Port: 8080, Host: "127.0.0.1", MaxRetries: 1, LogLevel: "info",
        CooldownSec: 15, HTTPTimeoutSec: 30, BackoffCapSec: 120,
        BackoffMultiplier: 2, CBResetSec: 30, UpstreamCBThreshold: 5,
        HealthCheckIntervalSec: 30, HealthCheckPath: "/health",
        HealthCheckTimeoutSec: 5, KeysFile: "keys.json",
        LogMaxSize: 100, LogMaxAge: 7, CalibrationIntervalSec: 3600,
    }
}

func DefaultRuntimeConfig() *RuntimeConfig {
    return &RuntimeConfig{
        HTTPTimeoutSec: 30, MaxRetries: 1, CooldownSec: 15,
        BackoffCapSec: 120, BackoffMultiplier: 2, CBResetSec: 30,
        UpstreamCBThreshold: 5, LogLevel: "info",
    }
}
```

- [ ] **Step 4: Update DefaultConfig to delegate**

```go
func DefaultConfig() *Config {
    pc := DefaultProviderConfig()
    return &Config{ProviderConfig: *pc}
}
```

- [ ] **Step 5: Move mergeDefaults to ProviderConfig method**

Change signature from `func mergeDefaults(cfg *Config)` to `func (pc *ProviderConfig) mergeDefaults()`. Update body to use `pc` instead of `cfg`, and `reflect.ValueOf(pc).Elem()`.

- [ ] **Step 6: Update mergeConfig to call new mergeDefaults**

```go
func mergeConfig(cfg *Config) {
    cfg.ProviderConfig.mergeDefaults()
}
```

- [ ] **Step 7: Add ProviderConfig.Validate()**

Extract the TOML-field validation logic from current `Config.Validate()`:

```go
func (pc *ProviderConfig) Validate() error {
    if pc.Port < 1 || pc.Port > 65535 {
        return &ConfigError{Category: "config", Message: fmt.Sprintf("...")}
    }
    if pc.TargetBase == "" {
        return &ConfigError{Category: "config", Message: "..."}
    }
    if len(pc.Keys) == 0 {
        return &ConfigError{Category: "config", Message: "..."}
    }
    if pc.BackoffCapSec < 30 { ... }
    if pc.BackoffMultiplier < 1 { ... }
    if pc.CBResetSec < 5 { ... }
    if pc.UpstreamCBThreshold < 2 { ... }
    if pc.HealthCheckIntervalSec < 5 { ... }
    return nil
}
```

- [ ] **Step 8: Add RuntimeConfig.Validate()**

```go
func (rc *RuntimeConfig) Validate() error {
    if rc.HTTPTimeoutSec < 1 {
        return &ConfigError{Category: "config", Message: fmt.Sprintf("...")}
    }
    return nil
}
```

- [ ] **Step 9: Update Config.Validate() to delegate**

```go
func (c *Config) Validate() error {
    if err := c.ProviderConfig.Validate(); err != nil {
        return err
    }
    return c.RuntimeConfig.Validate()
}
```

- [ ] **Step 10: Update Sanitized to use ProviderConfig fields**

```go
func (c *Config) Sanitized() *Config {
    s := *c
    s.Keys = make([]string, len(c.Keys))
    for i, k := range c.Keys {
        s.Keys[i] = logentry.MaskKey(k)
    }
    s.KeyNames = make([]string, len(c.KeyNames))
    copy(s.KeyNames, c.KeyNames)
    return &s
}
```

No changes needed — `c.Keys` and `c.KeyNames` are promoted from embedded ProviderConfig.

- [ ] **Step 11: Verify compilation**

Run: `go vet ./internal/config/`
Expected: clean

- [ ] **Step 12: Run existing tests**

Run: `go test -tags=unit -count=1 ./internal/config/`
Expected: all pass

- [ ] **Step 13: Commit**

```bash
git add internal/config/config.go
git commit -m "refactor: split Config into ProviderConfig + RuntimeConfig (#238)"
```

---

### Task 2: Update config_loader.go

**Files:**
- Modify: `internal/config/config_loader.go`
- Test: `internal/config/config_loader.go` (no new tests needed)

**Interfaces:**
- Consumes: `DefaultProviderConfig()`, `ProviderConfig.mergeDefaults()`
- Produces: Updated loading logic using new types

- [ ] **Step 1: Update nil provider fallback**

In `LoadAllTomlProviders`, line 94: change `p = DefaultConfig()` to `p = &Config{ProviderConfig: *DefaultProviderConfig()}`

- [ ] **Step 2: Update mergeConfig call**

In `LoadAllTomlProviders`, line 96: change `mergeConfig(p)` to `p.ProviderConfig.mergeDefaults()`

- [ ] **Step 3: Verify compilation**

Run: `go vet ./internal/config/`
Expected: clean

- [ ] **Step 4: Run config tests**

Run: `go test -tags=unit -count=1 ./internal/config/`
Expected: all pass

- [ ] **Step 5: Run full unit test suite**

Run: `go test -tags=unit -count=1 ./...`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/config/config_loader.go
git commit -m "refactor: config_loader.go use DefaultProviderConfig and mergeDefaults directly (#238)"
```

---

### Task 3: Add tests for new types and verify integration

**Files:**
- Modify: `internal/config/config_test.go`
- Test: `go test -tags=unit -count=1 ./...`

**Interfaces:**
- Consumes: `DefaultProviderConfig()`, `DefaultRuntimeConfig()`, `ProviderConfig.Validate()`, `RuntimeConfig.Validate()`

- [ ] **Step 1: Write test for DefaultProviderConfig**

```go
func TestDefaultProviderConfig(t *testing.T) {
    pc := DefaultProviderConfig()
    if pc.Port != 8080 { t.Errorf("Port = %d, want 8080", pc.Port) }
    if pc.Host != "127.0.0.1" { t.Errorf("Host = %q, want %q", pc.Host, "127.0.0.1") }
    if pc.TargetBase != "" { t.Errorf("TargetBase should be empty, got %q", pc.TargetBase) }
    if pc.MaxRetries != 1 { t.Errorf("MaxRetries = %d, want 1", pc.MaxRetries) }
    if pc.CooldownSec != 15 { t.Errorf("CooldownSec = %d, want 15", pc.CooldownSec) }
    if pc.HealthCheckPath != "/health" { t.Errorf("HealthCheckPath = %q, want %q", pc.HealthCheckPath, "/health") }
    if pc.CalibrationIntervalSec != 3600 { t.Errorf("CalibrationIntervalSec = %d, want 3600", pc.CalibrationIntervalSec) }
}
```

- [ ] **Step 2: Write test for DefaultRuntimeConfig**

```go
func TestDefaultRuntimeConfig(t *testing.T) {
    rc := DefaultRuntimeConfig()
    if rc.HTTPTimeoutSec != 30 { t.Errorf("HTTPTimeoutSec = %d, want 30", rc.HTTPTimeoutSec) }
    if rc.MaxRetries != 1 { t.Errorf("MaxRetries = %d, want 1", rc.MaxRetries) }
    if rc.CooldownSec != 15 { t.Errorf("CooldownSec = %d, want 15", rc.CooldownSec) }
    if rc.LogLevel != "info" { t.Errorf("LogLevel = %q, want %q", rc.LogLevel, "info") }
}
```

- [ ] **Step 3: Write test for ProviderConfig.Validate() boundary values**

```go
func TestProviderConfig_Validate_PortRange(t *testing.T) {
    pc := DefaultProviderConfig()
    pc.TargetBase = "https://example.com"
    pc.Keys = []string{"key1"}
    tests := []struct{ port int; wantErr bool }{
        {0, true}, {-1, true}, {65536, true}, {8080, false},
    }
    for _, tt := range tests {
        pc.Port = tt.port
        err := pc.Validate()
        if (err != nil) != tt.wantErr {
            t.Errorf("Port=%d: wantErr=%v, got err=%v", tt.port, tt.wantErr, err)
        }
    }
}
```

- [ ] **Step 4: Write test for RuntimeConfig.Validate() boundary values**

```go
func TestRuntimeConfig_Validate_HTTPTimeoutSec(t *testing.T) {
    tests := []struct{ sec int; wantErr bool }{
        {0, true}, {-1, true}, {1, false}, {30, false},
    }
    for _, tt := range tests {
        rc := &RuntimeConfig{HTTPTimeoutSec: tt.sec}
        err := rc.Validate()
        if (err != nil) != tt.wantErr {
            t.Errorf("HTTPTimeoutSec=%d: wantErr=%v, got err=%v", tt.sec, tt.wantErr, err)
        }
    }
}
```

- [ ] **Step 5: Write test for Config backward compatibility**

```go
func TestConfig_BackwardCompatibility(t *testing.T) {
    cfg := DefaultConfig()
    if cfg.Port != 8080 { t.Errorf("Port = %d, want 8080", cfg.Port) }
    if cfg.HTTPTimeoutSec != 30 { t.Errorf("HTTPTimeoutSec = %d, want 30", cfg.HTTPTimeoutSec) }
    if cfg.MaxRetries != 1 { t.Errorf("MaxRetries = %d, want 1", cfg.MaxRetries) }

    cfg.Port = 9090
    cfg.HTTPTimeoutSec = 60
    if cfg.Port != 9090 { t.Error("field mutation broken") }
    if cfg.HTTPTimeoutSec != 60 { t.Error("field mutation broken") }
}
```

- [ ] **Step 6: Run all tests**

Run: `go test -tags=unit -count=1 ./internal/config/`
Expected: all pass (existing + new)

- [ ] **Step 7: Run full unit test suite**

Run: `go test -tags=unit -count=1 ./...`
Expected: all pass

- [ ] **Step 8: Commit**

```bash
git add internal/config/config_test.go
git commit -m "test: add tests for ProviderConfig, RuntimeConfig, and backward compatibility (#238)"
```

---

## Verification

After all tasks:

1. `go test -tags=unit -count=1 ./...` — all pass
2. `go vet ./...` — clean
3. `go mod verify` — clean
4. All existing `Config` field access patterns compile without changes
