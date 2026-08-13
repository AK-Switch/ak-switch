# Task 6 Report: Integration Tests

## Status: DONE

## Commits
- `c3ed3e1` — test: add rectifier integration and runtime-config tests

## Tests
Command: `go test -tags=unit -count=1 -short ./internal/server/`
Result: **PASS** (13.182s)

New tests verified individually:
- `TestExecute_ThinkingRectifier_Enabled` — PASS (0.01s)
- `TestRuntimeConfigField_Apply/thinking_mode_valid_rectify` — PASS
- `TestRuntimeConfigField_Apply/thinking_mode_valid_default` — PASS
- `TestRuntimeConfigField_Apply/thinking_mode_invalid` — PASS
- `TestRuntimeConfigField_Apply/rectify_thinking_map_to_valid_enabled` — PASS
- `TestRuntimeConfigField_Apply/rectify_thinking_map_to_valid_auto` — PASS
- `TestRuntimeConfigField_Apply/rectify_thinking_map_to_invalid` — PASS

## Changes
### `internal/server/proxy_executor_test.go`
- Added `TestExecute_ThinkingRectifier_Enabled`: End-to-end test that creates a backend `httptest.Server`, configures a provider with `ThinkingMode="rectify"` and `RectifyThinkingMapTo="enabled"`, sends a request with `"thinking":{"type":"adaptive"}`, and verifies the proxy returns 200 (confirming the rectifier converted the body before forwarding).

### `internal/server/admin_test.go`
- Added `origThinkingMode`/`origRectifyMapTo` save/restore to the `TestRuntimeConfigField_Apply` defer block
- Added 6 new table-driven cases:
  - `thinking_mode` valid: "rectify", "default"
  - `thinking_mode` invalid: rejects unknown value
  - `rectify_thinking_map_to` valid: "enabled", "auto"
  - `rectify_thinking_map_to` invalid: rejects unknown value

## Concerns
- The brief specified `px.Execute(ps, w, req, time.Now())` but the actual signature is `px.Execute(w, req, ps)`. Adapted accordingly.
- The brief used `config.DefaultProviderConfig()` + `NewProviderState(cfg, nil)` which don't match the test helpers. Used `newTestProviderState` + direct config field overrides instead, consistent with existing test patterns.
- No `bytes` import was needed — `strings.NewReader` suffices for request bodies.
