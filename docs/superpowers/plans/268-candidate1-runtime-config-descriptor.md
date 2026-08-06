# Candidate 1 — Runtime Config Descriptor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace three switch statements in admin_api.go with a descriptor table, one field definition drives all three dispatch sites

**Architecture:** Define a `runtimeConfigField` struct with `apply` and `persist` callbacks. Each of the 8 fields gets one descriptor entry. `setRuntimeConfigField`, `persistRuntimeConfigField`, `persistRuntimeConfigFieldToDefault` become generic lookup-then-call functions. `log_level` side-effect (ApplyLevel) handled in `handleRuntimeConfigSet`.

**Tech Stack:** Go, table-driven tests, internal/server package

## Global Constraints

- 单文件修改：仅 `internal/server/admin_api.go`
- 零行为变更：API 响应格式、副作用时序不变
- `getRuntimeParams` 不变
- `log_level` 的 `ApplyLevel` 在 `handleRuntimeConfigSet` 中处理，不进入描述符
- 测试标签 `//go:build unit`

---

## Task 1: Write table-driven tests for runtime config fields

**Files:**
- Modify: `internal/server/admin_test.go`

**Interfaces:**
- Consumes: `newTestRouterWithKeys(t, keys)` → `*ProviderRouter`
- Produces: `TestRuntimeConfigField_Apply` test function

- [ ] **Step 1: Write the test**

Append to `admin_test.go`:

```go
func TestRuntimeConfigField_Apply(t *testing.T) {
	pr := newTestRouterWithKeys(t, []string{"sk-key-0"})
	ps := pr.Provider("test")

	origTimeout := ps.Config.HTTPTimeoutSec
	origRetries := ps.Config.MaxRetries
	origCooldown := ps.Config.CooldownSec
	origBackoffCap := ps.Config.BackoffCapSec
	origBackoffMult := ps.Config.BackoffMultiplier
	origCBReset := ps.Config.CBResetSec
	origUpThreshold := ps.Config.UpstreamCBThreshold
	origLogLevel := ps.Config.LogLevel
	defer func() {
		ps.Config.HTTPTimeoutSec = origTimeout
		ps.Config.MaxRetries = origRetries
		ps.Config.CooldownSec = origCooldown
		ps.Config.BackoffCapSec = origBackoffCap
		ps.Config.BackoffMultiplier = origBackoffMult
		ps.Config.CBResetSec = origCBReset
		ps.Config.UpstreamCBThreshold = origUpThreshold
		ps.Config.LogLevel = origLogLevel
	}()

	tests := []struct {
		name    string
		key     string
		value   interface{}
		wantErr bool
		check   func(t *testing.T, ps *ProviderState)
	}{
		{name: "http_timeout_sec valid", key: "http_timeout_sec", value: 30, wantErr: false,
			check: func(t *testing.T, ps *ProviderState) {
				if got := ps.Proxy.client.Timeout; got != 30*time.Second {
					t.Errorf("Timeout = %v, want 30s", got)
				}
			}},
		{name: "http_timeout_sec invalid zero", key: "http_timeout_sec", value: 0, wantErr: true},
		{name: "max_retries valid", key: "max_retries", value: 3, wantErr: false,
			check: func(t *testing.T, ps *ProviderState) {
				if ps.Config.MaxRetries != 3 {
					t.Errorf("MaxRetries = %d, want 3", ps.Config.MaxRetries)
				}
			}},
		{name: "max_retries zero", key: "max_retries", value: 0, wantErr: false},
		{name: "cooldown_sec valid", key: "cooldown_sec", value: 60, wantErr: false,
			check: func(t *testing.T, ps *ProviderState) {
				if ps.Config.CooldownSec != 60 {
					t.Errorf("CooldownSec = %d, want 60", ps.Config.CooldownSec)
				}
			}},
		{name: "backoff_cap_sec valid", key: "backoff_cap_sec", value: 120, wantErr: false,
			check: func(t *testing.T, ps *ProviderState) {
				if ps.Config.BackoffCapSec != 120 {
					t.Errorf("BackoffCapSec = %d, want 120", ps.Config.BackoffCapSec)
				}
			}},
		{name: "backoff_multiplier valid", key: "backoff_multiplier", value: 3.0, wantErr: false,
			check: func(t *testing.T, ps *ProviderState) {
				if ps.Config.BackoffMultiplier != 3.0 {
					t.Errorf("BackoffMultiplier = %f, want 3.0", ps.Config.BackoffMultiplier)
				}
			}},
		{name: "cb_reset_sec valid", key: "cb_reset_sec", value: 45, wantErr: false,
			check: func(t *testing.T, ps *ProviderState) {
				if ps.Config.CBResetSec != 45 {
					t.Errorf("CBResetSec = %d, want 45", ps.Config.CBResetSec)
				}
			}},
		{name: "upstream_cb_threshold valid", key: "upstream_cb_threshold", value: 10, wantErr: false,
			check: func(t *testing.T, ps *ProviderState) {
				if ps.Config.UpstreamCBThreshold != 10 {
					t.Errorf("UpstreamCBThreshold = %d, want 10", ps.Config.UpstreamCBThreshold)
				}
			}},
		{name: "log_level valid debug", key: "log_level", value: "debug", wantErr: false,
			check: func(t *testing.T, ps *ProviderState) {
				if ps.Config.LogLevel != "debug" {
					t.Errorf("LogLevel = %q, want debug", ps.Config.LogLevel)
				}
			}},
		{name: "unknown key", key: "nonexistent", value: "x", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pr.api.setRuntimeConfigField(ps, tc.key, tc.value)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (value=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, ps)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify baseline passes**

Run: `go test -tags=unit -run TestRuntimeConfigField_Apply ./internal/server/ -v`
Expected: PASS (existing three-switch covers all 8 keys)

- [ ] **Step 3: Commit the baseline test**

```bash
git add internal/server/admin_test.go
git commit -m "test: add table-driven test for runtime config field apply"
```

---

## Task 2: Add the descriptor struct and table

**Files:**
- Modify: `internal/server/admin_api.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtimeConfigField` struct, `runtimeConfigFields` var, `lookupRuntimeConfigField` function

- [ ] **Step 1: Add the descriptor table**

In `admin_api.go`, after the existing helper functions (`toInt`, `toFloat64`, `loadKeysFromConfig`), add:

```go
// runtimeConfigField describes a single runtime-configurable field.
// Each field is defined once; apply, persist, and persistToDefault
// are derived from the descriptor.
type runtimeConfigField struct {
	key     string
	apply   func(ps *ProviderState, raw interface{}) (interface{}, error)
	persist func(cfg *config.Config, val interface{})
}

// runtimeConfigFields is the single source of truth for all runtime
// config fields. Adding a new field requires exactly one entry here.
var runtimeConfigFields = []runtimeConfigField{
	{
		key: "http_timeout_sec",
		apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
			v, err := toInt(raw)
			if err != nil || v < 1 {
				return nil, fmt.Errorf("http_timeout_sec must be a positive integer")
			}
			ps.Proxy.client.Timeout = time.Duration(v) * time.Second
			ps.Config.HTTPTimeoutSec = v
			return v, nil
		},
		persist: func(cfg *config.Config, val interface{}) {
			v, _ := toInt(val)
			cfg.HTTPTimeoutSec = v
		},
	},
	{
		key: "max_retries",
		apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
			v, err := toInt(raw)
			if err != nil || v < 0 {
				return nil, fmt.Errorf("max_retries must be a non-negative integer")
			}
			ps.Config.MaxRetries = v
			return v, nil
		},
		persist: func(cfg *config.Config, val interface{}) {
			v, _ := toInt(val)
			cfg.MaxRetries = v
		},
	},
	{
		key: "cooldown_sec",
		apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
			v, err := toInt(raw)
			if err != nil || v < 1 {
				return nil, fmt.Errorf("cooldown_sec must be a positive integer")
			}
			ps.Config.CooldownSec = v
			ps.Pool.ConfigureCBs(
				time.Duration(v)*time.Second,
				time.Duration(ps.Config.BackoffCapSec)*time.Second,
				ps.Config.BackoffMultiplier,
			)
			return v, nil
		},
		persist: func(cfg *config.Config, val interface{}) {
			v, _ := toInt(val)
			cfg.CooldownSec = v
		},
	},
	{
		key: "backoff_cap_sec",
		apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
			v, err := toInt(raw)
			if err != nil || v < 1 {
				return nil, fmt.Errorf("backoff_cap_sec must be a positive integer")
			}
			ps.Config.BackoffCapSec = v
			ps.Pool.ConfigureCBs(
				time.Duration(ps.Config.CooldownSec)*time.Second,
				time.Duration(v)*time.Second,
				ps.Config.BackoffMultiplier,
			)
			return v, nil
		},
		persist: func(cfg *config.Config, val interface{}) {
			v, _ := toInt(val)
			cfg.BackoffCapSec = v
		},
	},
	{
		key: "backoff_multiplier",
		apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
			v, err := toFloat64(raw)
			if err != nil || v < 1.0 {
				return nil, fmt.Errorf("backoff_multiplier must be a number >= 1.0")
			}
			ps.Config.BackoffMultiplier = v
			ps.Pool.ConfigureCBs(
				time.Duration(ps.Config.CooldownSec)*time.Second,
				time.Duration(ps.Config.BackoffCapSec)*time.Second,
				v,
			)
			return v, nil
		},
		persist: func(cfg *config.Config, val interface{}) {
			v, _ := toFloat64(val)
			cfg.BackoffMultiplier = v
		},
	},
	{
		key: "cb_reset_sec",
		apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
			v, err := toInt(raw)
			if err != nil || v < 1 {
				return nil, fmt.Errorf("cb_reset_sec must be a positive integer")
			}
			ps.Proxy.upCB.SetResetTimeout(time.Duration(v) * time.Second)
			ps.Config.CBResetSec = v
			return v, nil
		},
		persist: func(cfg *config.Config, val interface{}) {
			v, _ := toInt(val)
			cfg.CBResetSec = v
		},
	},
	{
		key: "upstream_cb_threshold",
		apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
			v, err := toInt(raw)
			if err != nil || v < 1 {
				return nil, fmt.Errorf("upstream_cb_threshold must be a positive integer")
			}
			ps.Proxy.upCB.SetThreshold(v)
			ps.Config.UpstreamCBThreshold = v
			return v, nil
		},
		persist: func(cfg *config.Config, val interface{}) {
			v, _ := toInt(val)
			cfg.UpstreamCBThreshold = v
		},
	},
	{
		key: "log_level",
		apply: func(ps *ProviderState, raw interface{}) (interface{}, error) {
			s, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("log_level must be a string")
			}
			v := strings.TrimSpace(strings.ToLower(s))
			switch v {
			case "debug", "info", "warn", "error":
				ps.Config.LogLevel = v
				return v, nil
			}
			return nil, fmt.Errorf("invalid log level, use: debug, info, warn, error")
		},
		persist: func(cfg *config.Config, val interface{}) {
			v, _ := val.(string)
			cfg.LogLevel = v
		},
	},
}

// lookupRuntimeConfigField returns the descriptor for key, or nil.
func lookupRuntimeConfigField(key string) *runtimeConfigField {
	for i := range runtimeConfigFields {
		if runtimeConfigFields[i].key == key {
			return &runtimeConfigFields[i]
		}
	}
	return nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/server/`
Expected: PASS

- [ ] **Step 3: Commit the descriptor table**

```bash
git add internal/server/admin_api.go
git commit -m "feat: add runtimeConfigField descriptor table"
```

---

## Task 3: Replace the three switch statements with descriptor lookups

**Files:**
- Modify: `internal/server/admin_api.go`

**Interfaces:**
- Consumes: `runtimeConfigField`, `lookupRuntimeConfigField`
- Produces: refactored `setRuntimeConfigField`, `persistRuntimeConfigField`, `persistRuntimeConfigFieldToDefault`

- [ ] **Step 1: Replace `setRuntimeConfigField` body**

Delete the entire switch body (lines 743–826) and replace with:

```go
func (api *AdminAPI) setRuntimeConfigField(ps *ProviderState, key string, value interface{}) (interface{}, error) {
	f := lookupRuntimeConfigField(key)
	if f == nil {
		return nil, fmt.Errorf("unknown key %q", key)
	}
	return f.apply(ps, value)
}
```

- [ ] **Step 2: Replace `persistRuntimeConfigField` body**

Delete the entire switch body (lines 986–1011) and replace with:

```go
func (api *AdminAPI) persistRuntimeConfigField(ps *ProviderState, key string, value interface{}) error {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		return fmt.Errorf("failed to determine config path: %w", err)
	}
	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}
	if tc.Provider == nil {
		tc.Provider = make(map[string]*config.Config)
	}
	providerCfg, ok := tc.Provider[ps.Name]
	if !ok {
		providerCfg = &config.Config{}
		tc.Provider[ps.Name] = providerCfg
	}
	f := lookupRuntimeConfigField(key)
	if f != nil {
		f.persist(providerCfg, value)
	}
	return config.SaveTomlConfig(tc, xdgPath)
}
```

- [ ] **Step 3: Replace `persistRuntimeConfigFieldToDefault` body**

Delete the entire switch body (lines 1032–1057) and replace with:

```go
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
	f := lookupRuntimeConfigField(key)
	if f != nil {
		f.persist(tc.Default, value)
	}
	return config.SaveTomlConfig(tc, xdgPath)
}
```

- [ ] **Step 4: Handle `log_level` side-effect in `handleRuntimeConfigSet`**

In `handleRuntimeConfigSet`, after `setRuntimeConfigField` succeeds (around line 712), add:

```go
if body.Key == "log_level" {
	api.logManager.ApplyLevel(newValue.(string))
}
```

This applies to both the `pName == "all"` branch (line ~690) and the single-provider branch (line ~708).

- [ ] **Step 5: Run tests**

Run: `go test -tags=unit -run TestRuntimeConfigField ./internal/server/ -v`
Expected: PASS — test validates all 8 fields via descriptor

Run: `go test -tags=unit ./internal/server/ -v`
Expected: PASS — full test suite

Run: `go build ./...`
Expected: PASS

- [ ] **Step 6: Commit the refactor**

```bash
git add internal/server/admin_api.go
git commit -m "refactor: replace three runtime config switches with descriptor table"
```

---

## Task 4: Verify integration tests still pass

- [ ] **Step 1: Run integration tests**

Run: `go test -tags=integration ./test/integration/ -v`
Expected: PASS

- [ ] **Step 2: Run full test suite**

Run: `make test-all`
Expected: All pass

---

Plan complete and saved to `docs/superpowers/plans/268-candidate1-runtime-config-descriptor.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**