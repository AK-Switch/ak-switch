# 运行时配置全局默认值 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `[provider.default]` TOML section as global config defaults with three-layer inheritance (struct tag → default → per-provider), plus `--all` CLI flag and `provider=all` API support for batch operations.

**Architecture:** Insert merge step in `LoadAllTomlProviders` that applies `[provider.default]` as base before per-provider config. New pure functions `DeepCopy` and `mergeWithDefaults` handle field-level inheritance. CLI and API layers gain `--all`/`provider=all` for batch operations. Persist-to-default strategy writes to `[provider.default]` section.

**Tech Stack:** Go, go-toml/v2, Cobra CLI

## Global Constraints

- 向后兼容：无 `[provider.default]` 段时行为完全不变
- 只对 9 个运行时配置字段生效（`max_retries`、`http_timeout_sec`、`cooldown_sec`、`backoff_cap_sec`、`backoff_multiplier`、`cb_reset_sec`、`upstream_cb_threshold`、`health_check_interval_sec`、`log_level`）
- `config set --all --persist` 写入 `[provider.default]`，已有显式覆盖的 provider 不受影响
- 每个测试文件必须有 `//go:build unit` 标签
- 提交规范：`类型: 描述`

---

### Task 1: Add DeepCopy + mergeWithDefaults to config.go

**Files:**
- Modify: `internal/config/config.go`

**Interfaces:**
- Produces: `func (c *Config) DeepCopy() *Config` — returns a deep copy of Config
- Produces: `func mergeWithDefaults(base, override *Config) *Config` — merges override into base, non-zero override fields take precedence

- [ ] **Step 1: Add DeepCopy method**

Add after `Sanitized()` method (line ~208):

```go
// DeepCopy returns a deep copy of the Config.
func (c *Config) DeepCopy() *Config {
    keys := make([]string, len(c.Keys))
    copy(keys, c.Keys)
    keyNames := make([]string, len(c.KeyNames))
    copy(keyNames, c.KeyNames)
    return &Config{
        ProviderConfig: ProviderConfig{
            Port:                 c.Port,
            Host:                 c.Host,
            TargetBase:           c.TargetBase,
            AdminToken:           c.AdminToken,
            DisableThinking:      c.DisableThinking,
            GenaiModel:           c.GenaiModel,
            MaxRetries:           c.MaxRetries,
            LogLevel:             c.LogLevel,
            CooldownSec:          c.CooldownSec,
            HTTPTimeoutSec:       c.HTTPTimeoutSec,
            Keys:                 keys,
            KeyNames:             keyNames,
            KeysFile:             c.KeysFile,
            BackoffCapSec:        c.BackoffCapSec,
            BackoffMultiplier:    c.BackoffMultiplier,
            CBResetSec:           c.CBResetSec,
            UpstreamCBThreshold:  c.UpstreamCBThreshold,
            HealthCheckIntervalSec: c.HealthCheckIntervalSec,
            HealthCheckPath:      c.HealthCheckPath,
            HealthCheckTimeoutSec: c.HealthCheckTimeoutSec,
            LogFile:              c.LogFile,
            LogMaxSize:           c.LogMaxSize,
            LogMaxAge:            c.LogMaxAge,
            CalibrationIntervalSec: c.CalibrationIntervalSec,
        },
        RuntimeConfig: RuntimeConfig{
            HTTPTimeoutSec:      c.RuntimeConfig.HTTPTimeoutSec,
            MaxRetries:          c.RuntimeConfig.MaxRetries,
            CooldownSec:         c.RuntimeConfig.CooldownSec,
            BackoffCapSec:       c.RuntimeConfig.BackoffCapSec,
            BackoffMultiplier:   c.RuntimeConfig.BackoffMultiplier,
            CBResetSec:          c.RuntimeConfig.CBResetSec,
            UpstreamCBThreshold: c.RuntimeConfig.UpstreamCBThreshold,
            LogLevel:            c.RuntimeConfig.LogLevel,
        },
    }
}
```

- [ ] **Step 2: Add mergeWithDefaults function**

Add after `mergeDefaults()` method:

```go
// mergeWithDefaults merges override into base, returning a new Config.
// Non-zero fields in override take precedence over base.
// Non-inheritable fields (TargetBase, Keys, KeyNames, Port, Host, etc.)
// are always taken from override regardless of zero value.
//
// The 9 inheritable fields are: MaxRetries, HTTPTimeoutSec, CooldownSec,
// BackoffCapSec, BackoffMultiplier, CBResetSec, UpstreamCBThreshold,
// HealthCheckIntervalSec, LogLevel.
func mergeWithDefaults(base, override *Config) *Config {
    result := base.DeepCopy()
    // Inheritable fields — only override if non-zero
    if override.MaxRetries != 0 {
        result.MaxRetries = override.MaxRetries
    }
    if override.HTTPTimeoutSec != 0 {
        result.HTTPTimeoutSec = override.HTTPTimeoutSec
    }
    if override.CooldownSec != 0 {
        result.CooldownSec = override.CooldownSec
    }
    if override.BackoffCapSec != 0 {
        result.BackoffCapSec = override.BackoffCapSec
    }
    if override.BackoffMultiplier != 0 {
        result.BackoffMultiplier = override.BackoffMultiplier
    }
    if override.CBResetSec != 0 {
        result.CBResetSec = override.CBResetSec
    }
    if override.UpstreamCBThreshold != 0 {
        result.UpstreamCBThreshold = override.UpstreamCBThreshold
    }
    if override.HealthCheckIntervalSec != 0 {
        result.HealthCheckIntervalSec = override.HealthCheckIntervalSec
    }
    if override.LogLevel != "" {
        result.LogLevel = override.LogLevel
    }
    // Non-inheritable fields — always take from override
    result.Port = override.Port
    result.Host = override.Host
    result.TargetBase = override.TargetBase
    result.AdminToken = override.AdminToken
    result.DisableThinking = override.DisableThinking
    result.GenaiModel = override.GenaiModel
    result.Keys = override.Keys
    result.KeyNames = override.KeyNames
    result.KeysFile = override.KeysFile
    result.HealthCheckPath = override.HealthCheckPath
    result.HealthCheckTimeoutSec = override.HealthCheckTimeoutSec
    result.LogFile = override.LogFile
    result.LogMaxSize = override.LogMaxSize
    result.LogMaxAge = override.LogMaxAge
    result.CalibrationIntervalSec = override.CalibrationIntervalSec
    // Sync runtime config
    result.RuntimeConfig.HTTPTimeoutSec = result.HTTPTimeoutSec
    result.RuntimeConfig.MaxRetries = result.MaxRetries
    result.RuntimeConfig.CooldownSec = result.CooldownSec
    result.RuntimeConfig.BackoffCapSec = result.BackoffCapSec
    result.RuntimeConfig.BackoffMultiplier = result.BackoffMultiplier
    result.RuntimeConfig.CBResetSec = result.CBResetSec
    result.RuntimeConfig.UpstreamCBThreshold = result.UpstreamCBThreshold
    result.RuntimeConfig.LogLevel = result.LogLevel
    return result
}
```

- [ ] **Step 3: Write unit tests**

Create `internal/config/config_defaults_test.go`:

```go
//go:build unit

package config

import (
    "testing"
)

func TestMergeWithDefaults_InheritsMissingFields(t *testing.T) {
    base := &Config{
        ProviderConfig: ProviderConfig{
            TargetBase:     "https://default.example.com",
            MaxRetries:     3,
            CooldownSec:    20,
            LogLevel:       "info",
            HTTPTimeoutSec: 30,
        },
    }
    override := &Config{
        ProviderConfig: ProviderConfig{
            TargetBase:  "https://override.example.com",
            MaxRetries:  5,
            CooldownSec: 0, // zero — should inherit from base (20)
        },
    }
    result := mergeWithDefaults(base, override)
    if result.MaxRetries != 5 {
        t.Errorf("MaxRetries = %d, want 5 (overridden)", result.MaxRetries)
    }
    if result.CooldownSec != 20 {
        t.Errorf("CooldownSec = %d, want 20 (inherited)", result.CooldownSec)
    }
    if result.TargetBase != "https://override.example.com" {
        t.Errorf("TargetBase = %q, want overridden value", result.TargetBase)
    }
}

func TestMergeWithDefaults_NoDefault(t *testing.T) {
    // Without a base, mergeWithDefaults should behave like a copy of override
    override := &Config{
        ProviderConfig: ProviderConfig{
            TargetBase:     "https://example.com",
            MaxRetries:     2,
            CooldownSec:    10,
            HTTPTimeoutSec: 15,
            Keys:           []string{"key1"},
        },
    }
    result := mergeWithDefaults(override, override)
    if result.MaxRetries != 2 {
        t.Errorf("MaxRetries = %d, want 2", result.MaxRetries)
    }
    if result.CooldownSec != 10 {
        t.Errorf("CooldownSec = %d, want 10", result.CooldownSec)
    }
    if len(result.Keys) != 1 || result.Keys[0] != "key1" {
        t.Errorf("Keys = %v, want [key1]", result.Keys)
    }
}

func TestMergeWithDefaults_AllInherited(t *testing.T) {
    base := &Config{
        ProviderConfig: ProviderConfig{
            TargetBase:     "https://default.example.com",
            MaxRetries:     3,
            CooldownSec:    20,
            HTTPTimeoutSec: 30,
            BackoffCapSec:  120,
            Keys:           []string{"base-key"},
        },
    }
    override := &Config{
        ProviderConfig: ProviderConfig{
            TargetBase: "https://override.example.com",
            Keys:       []string{"override-key"},
        },
    }
    result := mergeWithDefaults(base, override)
    if result.MaxRetries != 3 {
        t.Errorf("MaxRetries = %d, want 3 (inherited)", result.MaxRetries)
    }
    if result.CooldownSec != 20 {
        t.Errorf("CooldownSec = %d, want 20 (inherited)", result.CooldownSec)
    }
    if result.BackoffCapSec != 120 {
        t.Errorf("BackoffCapSec = %d, want 120 (inherited)", result.BackoffCapSec)
    }
    if result.TargetBase != "https://override.example.com" {
        t.Errorf("TargetBase = %q, want overridden", result.TargetBase)
    }
    if len(result.Keys) != 1 || result.Keys[0] != "override-key" {
        t.Errorf("Keys = %v, want [override-key]", result.Keys)
    }
}

func TestDeepCopy(t *testing.T) {
    original := &Config{
        ProviderConfig: ProviderConfig{
            TargetBase:     "https://example.com",
            MaxRetries:     3,
            Keys:           []string{"key1", "key2"},
            KeyNames:       []string{"primary", "secondary"},
        },
    }
    copy := original.DeepCopy()
    copy.MaxRetries = 99
    copy.Keys[0] = "modified"
    if original.MaxRetries != 3 {
        t.Errorf("original.MaxRetries changed to %d", original.MaxRetries)
    }
    if original.Keys[0] != "key1" {
        t.Errorf("original.Keys[0] changed to %s", original.Keys[0])
    }
}
```

- [ ] **Step 4: Run tests**

```bash
go test -tags=unit -count=1 ./internal/config/
```

Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_defaults_test.go
git commit -m "feat: add DeepCopy and mergeWithDefaults for config inheritance"
```

---
### Task 2: Add `[provider.default]` to TOML loading

**Files:**
- Modify: `internal/config/config_toml.go:12-21` (TomlConfig struct)
- Modify: `internal/config/config_loader.go:60-125` (LoadAllTomlProviders)

**Interfaces:**
- Consumes: `mergeWithDefaults` from Task 1
- Produces: `[provider.default]` is loaded and applied as base for all providers

- [ ] **Step 1: Add Default field to TomlConfig**

```go
type TomlConfig struct {
    Port              int                  `toml:"port"`
    Host              string               `toml:"host,omitempty"`
    DefaultProvider   string               `toml:"default_provider,omitempty"`
    Default           *Config              `toml:"provider.default"`
    CredentialsDir    string               `toml:"credentials_dir,omitempty"`
    LogFile           string               `toml:"log_file,omitempty"`
    LogMaxSize        int                  `toml:"log_max_size"`
    LogMaxAge         int                  `toml:"log_max_age"`
    Provider          map[string]*Config   `toml:"provider"`
}
```

- [ ] **Step 2: Modify LoadAllTomlProviders to apply defaults**

Replace the loop body at lines 92-113 with:

```go
defaultCfg := tc.Default
for name, p := range tc.Provider {
    if p == nil {
        p = &Config{ProviderConfig: *DefaultProviderConfig()}
    } else {
        p.mergeDefaults()
    }
    if defaultCfg != nil {
        // Apply [provider.default] as base, p overrides non-zero fields
        p = mergeWithDefaults(defaultCfg, p)
    }
    p.Port = port
    // Top-level host used as fallback when provider-level host is empty
    if p.Host == "" {
        p.Host = host
    }
    // Top-level log fields override per-provider log fields
    if tc.LogFile != "" {
        p.LogFile = tc.LogFile
    }
    if tc.LogMaxSize > 0 {
        p.LogMaxSize = tc.LogMaxSize
    }
    if tc.LogMaxAge > 0 {
        p.LogMaxAge = tc.LogMaxAge
    }
    result[name] = p
}
```

- [ ] **Step 3: Write unit test**

Add to `internal/config/config_defaults_test.go`:

```go
func TestLoadAllTomlProviders_WithDefaultSection(t *testing.T) {
    toml := `
port = 8080

[provider.default]
max_retries = 3
cooldown_sec = 20
log_level = "warn"

[sensenova]
target = "https://api.sensenova.com/v1"
keys_file = "sensenova.keys"

[claude]
target = "https://api.anthropic.com/v1"
max_retries = 5
`
    tmpFile := writeTempToml(t, toml)
    defer os.Remove(tmpFile)

    providers, err := LoadAllTomlProviders(tmpFile)
    if err != nil {
        t.Fatalf("LoadAllTomlProviders failed: %v", err)
    }

    // sensenova inherits max_retries=3, cooldown_sec=20, log_level="warn" from default
    s := providers["sensenova"]
    if s.MaxRetries != 3 {
        t.Errorf("sensenova.MaxRetries = %d, want 3 (inherited)", s.MaxRetries)
    }
    if s.CooldownSec != 20 {
        t.Errorf("sensenova.CooldownSec = %d, want 20 (inherited)", s.CooldownSec)
    }
    if s.LogLevel != "warn" {
        t.Errorf("sensenova.LogLevel = %q, want \"warn\" (inherited)", s.LogLevel)
    }
    if s.TargetBase != "https://api.sensenova.com/v1" {
        t.Errorf("sensenova.TargetBase = %q", s.TargetBase)
    }

    // claude overrides max_retries=5, inherits cooldown_sec=20, log_level="warn"
    c := providers["claude"]
    if c.MaxRetries != 5 {
        t.Errorf("claude.MaxRetries = %d, want 5 (overridden)", c.MaxRetries)
    }
    if c.CooldownSec != 20 {
        t.Errorf("claude.CooldownSec = %d, want 20 (inherited)", c.CooldownSec)
    }
    if c.LogLevel != "warn" {
        t.Errorf("claude.LogLevel = %q, want \"warn\" (inherited)", c.LogLevel)
    }
}

func TestLoadAllTomlProviders_WithoutDefaultSection(t *testing.T) {
    // No [provider.default] — behavior unchanged
    toml := `
port = 9090

[sensenova]
target = "https://api.sensenova.com/v1"
max_retries = 3
`
    tmpFile := writeTempToml(t, toml)
    defer os.Remove(tmpFile)

    providers, err := LoadAllTomlProviders(tmpFile)
    if err != nil {
        t.Fatalf("LoadAllTomlProviders failed: %v", err)
    }
    s := providers["sensenova"]
    if s.MaxRetries != 3 {
        t.Errorf("MaxRetries = %d, want 3", s.MaxRetries)
    }
    if s.Port != 9090 {
        t.Errorf("Port = %d, want 9090", s.Port)
    }
}
```

- [ ] **Step 4: Run tests**

```bash
go test -tags=unit -count=1 ./internal/config/
```

Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config_toml.go internal/config/config_loader.go internal/config/config_defaults_test.go
git commit -m "feat: add [provider.default] TOML section support in LoadAllTomlProviders"
```

---
### Task 3: Add `provider=all` to runtime config API

**Files:**
- Modify: `internal/server/admin_api.go`

**Interfaces:**
- Consumes: `api.pm.ProviderNames()`, `api.pm.LookupProvider()` — existing ProviderLookup methods
- Produces: `GET/POST /api/runtime-config?provider=all` batch support

- [ ] **Step 1: Modify handleRuntimeConfigGet for `provider=all`**

At line 636, after the `if key != ""` block, add an `else if pName == "all"` branch:

```go
    if key != "" {
        names := api.pm.ProviderNames()
        for _, name := range names {
            ps := api.pm.LookupProvider(name)
            params := api.getRuntimeParams(ps)
            val, ok := params[key]
            if !ok {
                continue
            }
            respondJSON(w, http.StatusOK, map[string]interface{}{
                "provider": name,
                "key":      key,
                "value":    val,
            })
            return
        }
        respondJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unknown key %q", key)})
        return
    }

    if pName == "all" {
        result := make(map[string]map[string]interface{})
        api.pm.ForEach(func(name string, ps *ProviderState) {
            result[name] = api.getRuntimeParams(ps)
        })
        respondJSON(w, http.StatusOK, result)
        return
    }

    respondJSON(w, http.StatusOK, result)
```

- [ ] **Step 2: Extract setRuntimeConfigField from handleRuntimeConfigSet**

Replace the inline switch in `handleRuntimeConfigSet` with a call to a new method:

```go
// setRuntimeConfigField applies a runtime config change to a provider's
// in-memory config and runtime state. Returns the new value for the response.
func (api *AdminAPI) setRuntimeConfigField(ps *ProviderState, key string, value interface{}) interface{} {
    switch key {
    case "http_timeout_sec":
        v, _ := toInt(value)
        if v < 1 { return nil }
        ps.Proxy.client.Timeout = time.Duration(v) * time.Second
        ps.Config.HTTPTimeoutSec = v
        return v
    case "max_retries":
        v, _ := toInt(value)
        if v < 0 { return nil }
        ps.Config.MaxRetries = v
        return v
    case "cooldown_sec":
        v, _ := toInt(value)
        if v < 1 { return nil }
        ps.Config.CooldownSec = v
        ps.Pool.ConfigureCBs(
            time.Duration(v)*time.Second,
            time.Duration(ps.Config.BackoffCapSec)*time.Second,
            ps.Config.BackoffMultiplier,
        )
        return v
    case "backoff_cap_sec":
        v, _ := toInt(value)
        if v < 1 { return nil }
        ps.Config.BackoffCapSec = v
        ps.Pool.ConfigureCBs(
            time.Duration(ps.Config.CooldownSec)*time.Second,
            time.Duration(v)*time.Second,
            ps.Config.BackoffMultiplier,
        )
        return v
    case "backoff_multiplier":
        v, _ := toFloat64(value)
        if v < 1.0 { return nil }
        ps.Config.BackoffMultiplier = v
        ps.Pool.ConfigureCBs(
            time.Duration(ps.Config.CooldownSec)*time.Second,
            time.Duration(ps.Config.BackoffCapSec)*time.Second,
            v,
        )
        return v
    case "cb_reset_sec":
        v, _ := toInt(value)
        if v < 1 { return nil }
        ps.Proxy.upCB.SetResetTimeout(time.Duration(v) * time.Second)
        ps.Config.CBResetSec = v
        return v
    case "upstream_cb_threshold":
        v, _ := toInt(value)
        if v < 1 { return nil }
        ps.Proxy.upCB.SetThreshold(v)
        ps.Config.UpstreamCBThreshold = v
        return v
    case "log_level":
        raw, _ := value.(string)
        v := strings.TrimSpace(strings.ToLower(raw))
        switch v {
        case "debug", "info", "warn", "error":
            api.logManager.ApplyLevel(v)
            ps.Config.LogLevel = v
            return v
        }
        return nil
    }
    return nil
}
```

Refactor `handleRuntimeConfigSet` to use `setRuntimeConfigField`:

```go
// In handleRuntimeConfigSet, replace the switch block with:
newValue := api.setRuntimeConfigField(ps, body.Key, body.Value)
if newValue == nil {
    respondJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unknown key %q", body.Key)})
    return
}
```

- [ ] **Step 3: Add provider=all handling to handleRuntimeConfigSet**

After the single-provider `setRuntimeConfigField` call, add:

```go
if pName == "all" {
    var names []string
    api.pm.ForEach(func(name string, ps *ProviderState) {
        api.setRuntimeConfigField(ps, body.Key, body.Value)
        names = append(names, name)
    })
    if persist {
        _ = api.persistRuntimeConfigFieldToDefault(body.Key, newValue)
    }
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "key":       body.Key,
        "value":     newValue,
        "persisted": persist,
        "providers": names,
    })
    return
}
```

- [ ] **Step 4: Add persistRuntimeConfigFieldToDefault**

```go
// persistRuntimeConfigFieldToDefault saves a field to the [provider.default] section.
func (api *AdminAPI) persistRuntimeConfigFieldToDefault(key string, value interface{}) error {
    xdgPath, err := config.XDGConfigPath()
    if err != nil {
        return err
    }
    tc, err := config.LoadTomlConfig(xdgPath)
    if err != nil {
        return err
    }
    if tc.Default == nil {
        tc.Default = &config.Config{}
    }
    switch key {
    case "http_timeout_sec":
        v, _ := toInt(value)
        tc.Default.HTTPTimeoutSec = v
    case "max_retries":
        v, _ := toInt(value)
        tc.Default.MaxRetries = v
    case "cooldown_sec":
        v, _ := toInt(value)
        tc.Default.CooldownSec = v
    case "backoff_cap_sec":
        v, _ := toInt(value)
        tc.Default.BackoffCapSec = v
    case "backoff_multiplier":
        v, _ := toFloat64(value)
        tc.Default.BackoffMultiplier = v
    case "cb_reset_sec":
        v, _ := toInt(value)
        tc.Default.CBResetSec = v
    case "upstream_cb_threshold":
        v, _ := toInt(value)
        tc.Default.UpstreamCBThreshold = v
    case "log_level":
        v, _ := value.(string)
        tc.Default.LogLevel = v
    }
    return config.SaveTomlConfig(tc, xdgPath)
}
```

- [ ] **Step 4: Modify persistRuntimeConfigField for `provider=all`**

Add a new method:

```go
// persistRuntimeConfigFieldToDefault saves a field to [provider.default] section.
func (api *AdminAPI) persistRuntimeConfigFieldToDefault(key string, value interface{}) error {
    xdgPath, err := config.XDGConfigPath()
    if err != nil {
        return err
    }
    tc, err := config.LoadTomlConfig(xdgPath)
    if err != nil {
        return err
    }
    if tc.Default == nil {
        tc.Default = &config.Config{}
    }
    // Set the field on the default config
    switch key {
    case "http_timeout_sec":
        v, _ := toInt(value)
        tc.Default.HTTPTimeoutSec = v
    case "max_retries":
        v, _ := toInt(value)
        tc.Default.MaxRetries = v
    case "cooldown_sec":
        v, _ := toInt(value)
        tc.Default.CooldownSec = v
    case "backoff_cap_sec":
        v, _ := toInt(value)
        tc.Default.BackoffCapSec = v
    case "backoff_multiplier":
        v, _ := toFloat64(value)
        tc.Default.BackoffMultiplier = v
    case "cb_reset_sec":
        v, _ := toInt(value)
        tc.Default.CBResetSec = v
    case "upstream_cb_threshold":
        v, _ := toInt(value)
        tc.Default.UpstreamCBThreshold = v
    case "log_level":
        v, _ := value.(string)
        tc.Default.LogLevel = v
    }
    return config.SaveTomlConfig(tc, xdgPath)
}
```

Update `handleRuntimeConfigSet` to call `persistRuntimeConfigFieldToDefault` when `pName == "all" && persist`.

- [ ] **Step 5: Write tests**

Add to `internal/server/admin_api_test.go` (or create new test functions):

```go
func TestHandleRuntimeConfigGet_ProviderAll(t *testing.T) {
    pr := newTestRouterWithKeys(t, []string{"sk-key-0", "sk-key-1"})
    // Add second provider with different config
    cfg2 := config.DefaultConfig()
    cfg2.MaxRetries = 5
    cfg2.CooldownSec = 30
    pool2 := keypool.NewKeyPool([]string{"sk-key-2"}, nil)
    pr.AddProvider("provider-b", cfg2, pool2)

    w := httptest.NewRecorder()
    r := httptest.NewRequest(http.MethodGet, "/api/runtime-config?provider=all", nil)
    r.Header.Set("X-Admin-Token", "")
    pr.api.runtimeConfigHandler(w, r)

    if w.Code != http.StatusOK {
        t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
    }
    var result map[string]map[string]interface{}
    if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
        t.Fatalf("decode failed: %v", err)
    }
    if _, ok := result["test"]; !ok {
        t.Error("missing provider 'test' in response")
    }
    if _, ok := result["provider-b"]; !ok {
        t.Error("missing provider 'provider-b' in response")
    }
}
```

- [ ] **Step 6: Run tests**

```bash
go test -tags=unit -count=1 ./internal/server/
```

Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/server/admin_api.go internal/server/admin_api_test.go
git commit -m "feat: add provider=all batch support to runtime config API"
```

---
### Task 4: Add `--all` flag to CLI config commands

**Files:**
- Modify: `internal/cli/config.go`

**Interfaces:**
- Consumes: API `?provider=all` from Task 3
- Produces: `config set --all`, `config list --all`, `config get --all`

- [ ] **Step 1: Add `--all` flag registration**

```go
configListCmd.Flags().Bool("all", false, "Show all providers")
configGetCmd.Flags().Bool("all", false, "Show value from all providers")
configSetCmd.Flags().Bool("persist", false, "Persist the change to the config file")
configSetCmd.Flags().Bool("all", false, "Apply to all providers (writes to [provider.default] when used with --persist)")
```

- [ ] **Step 2: Modify config list for `--all`**

When `--all` is set, query `?provider=all` and display each provider's params. If `[provider.default]` exists, show it first:

```go
all, _ := cmd.Flags().GetBool("all")
if all {
    baseURL := fmt.Sprintf("http://%s:%d/api/runtime-config?provider=all", detectServerHost(), detectServerPort())
    // ... fetch and display all providers
}
```

- [ ] **Step 3: Modify config get for `--all`**

When `--all` is set, query `?key=<key>&provider=all` and display all providers' values.

- [ ] **Step 4: Modify config set for `--all`**

When `--all` is set, append `&provider=all` to the request URL. When `--all --persist` is combined, the API writes to `[provider.default]`.

- [ ] **Step 5: Update Use strings**

```go
var configListCmd = &cobra.Command{
    Use:   "list [provider]",
    // ...
}
// No change needed — --all is a flag, not positional arg
```

- [ ] **Step 6: Write CLI tests**

Add to `internal/cli/config_test.go` or create new test:

```go
func TestConfigSetCmd_HasAllFlag(t *testing.T) {
    if !hasFlag(configSetCmd, "all") {
        t.Error("config set missing --all flag")
    }
}

func TestConfigListCmd_HasAllFlag(t *testing.T) {
    if !hasFlag(configListCmd, "all") {
        t.Error("config list missing --all flag")
    }
}
```

- [ ] **Step 7: Run tests**

```bash
go test -tags=unit -count=1 ./internal/cli/
```

Expected: All tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/config.go internal/cli/config_test.go
git commit -m "feat: add --all flag to config CLI commands"
```

---
### Task 5: Update config init with default section example

**Files:**
- Modify: `internal/cli/config.go` (configInitCmd)

- [ ] **Step 1: Add `[provider.default]` to init template**

```go
tc := &config.TomlConfig{
    Port: 8080,
    Provider: map[string]*config.Config{
        "example-a": { /* ... */ },
        "example-b": { /* ... */ },
    },
    Default: &config.Config{
        ProviderConfig: config.ProviderConfig{
            MaxRetries:  2,
            CooldownSec: 15,
            LogLevel:    "info",
        },
    },
}
```

- [ ] **Step 2: Run tests**

```bash
go test -tags=unit -count=1 ./internal/cli/ -run TestConfigInit
```

- [ ] **Step 3: Commit**

```bash
git add internal/cli/config.go
git commit -m "feat: include [provider.default] example in config init"
```

---
### Task 6: Update docs and verify

**Files:**
- Modify: `docs/api.md` — document `provider=all` API
- Modify: `docs/cli-reference.md` or equivalent — document `--all` flag

- [ ] **Step 1: Update API docs**

Add `provider=all` support to the runtime-config endpoint documentation.

- [ ] **Step 2: Update CLI docs**

Add `--all` flag documentation for `config set`, `config get`, `config list`.

- [ ] **Step 3: Run full test suite**

```bash
make test-all
```

Expected: All tests pass (unit + integration).

- [ ] **Step 4: Commit**

```bash
git add docs/api.md docs/cli-reference.md
git commit -m "docs: document provider=all API and --all CLI flag"
```
