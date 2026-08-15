// Package server provides the HTTP admin API.
// Handlers are split across multiple files:
//   - provider_state.go: AdminAPI struct, routing, key operation factory
//   - auth_handlers.go: auth helpers, log level, config, dashboard, clear handlers
//   - keys_handlers.go: key CRUD handler
//   - logs_handler.go: log streaming handler
//   - health_handlers.go: health, stats, reload, upstream CB handlers
//   - runtime_config.go: runtime config get/set handlers
package server
