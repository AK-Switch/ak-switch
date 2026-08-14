# Split admin_api.go — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 940 行的 `admin_api.go` 按职责拆分为 6 个文件，零行为变更

**Architecture:** 纯文件拆分，所有函数保持 `(api *AdminAPI)` receiver，不改变包导出接口。新文件通过同包共享私有类型和方法，外部调用方（`router.go`）零改动。

**Tech Stack:** Go（纯文件重组）

## Global Constraints

- 所有测试必须通过（`go test -tags=unit -count=1 -short ./internal/server/`）
- 零行为变更 — 只改文件布局，不改函数签名、不改逻辑
- Tab 缩进，gofmt 格式化
- 不删除任何函数或字段，只移动位置

## File Structure

| 文件 | 内容 | 源行范围 | 预估行数 |
|------|------|---------|---------|
| `provider_state.go` | ProviderLookup 接口, AdminAPI struct, NewAdminAPI, RegisterRoutes, keyOperationHandler | 16-803 | ~170 |
| `auth_handlers.go` | checkAdminToken, checkAnyAdminToken, resolveProvider, resolveProviderByName, persistRuntimeConfigField, persistRuntimeConfigFieldToDefault + dashboardHandler, clearHandler, logLevelHandler, configHandler | 805-940 + 87-144 | ~170 |
| `keys_handlers.go` | keysHandler | 146-225 | ~80 |
| `logs_handler.go` | logsHandler | 306-448 | ~143 |
| `health_handlers.go` | healthHandler, statsHandler, upstreamCBResetHandler, reloadHandler, swHandler | 81-304 + 450-562 | ~160 |
| `runtime_config.go` | runtimeConfigHandler, handleRuntimeConfigGet, handleRuntimeConfigSet, setRuntimeConfigField, getRuntimeParams | 564-765 | ~200 |

**总行数不变（940），包导出接口不变，router.go 零改动。**

---
### Task 1: 创建 provider_state.go（结构体 + 路由 + 辅助工厂）

**Files:**
- Create: `internal/server/provider_state.go`
- Modify: `internal/server/admin_api.go`（删除行 16-803）

**Interfaces:**
- 本文件定义: `ProviderLookup` 接口, `AdminAPI` struct, `NewAdminAPI`, `RegisterRoutes`, `keyOperationHandler`, `respondJSON`
- 被其他文件引用: 所有 handler 文件引用 `api.*` 方法

- [ ] **Step 1: 创建 `provider_state.go`**

从 `admin_api.go` 提取行 16-803（到 keyOperationHandler 结束），包含：
- ProviderLookup 接口（行 18-25）
- AdminAPI struct（行 30-41）
- NewAdminAPI（行 44-59）
- RegisterRoutes（行 62-79）
- swHandler（行 83-85）
- keyOperationHandler（行 775-803）
- respondJSON 辅助函数（从其他 server 文件确认位置）

```go
package server

import (
	"akswitch/internal/circuitbreaker"
	"akswitch/internal/config"
	"akswitch/internal/logentry"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// ProviderLookup defines the interface AdminAPI needs to access provider data.
type ProviderLookup interface { ... }

// AdminAPI holds all management API handlers.
type AdminAPI struct { ... }

// NewAdminAPI, RegisterRoutes, keyOperationHandler, respondJSON
```

- [ ] **Step 2: 更新 `admin_api.go`**

删除行 16-803，保留行 805-940（auth helpers + persist helpers），并将文件头注释改为：

```go
// Package server provides the HTTP admin API.
// Handlers are split across multiple files:
//   - provider_state.go: AdminAPI struct, routing, key operation factory
//   - auth_handlers.go: auth, config, dashboard, clear, log level handlers
//   - keys_handlers.go: key CRUD handler
//   - logs_handler.go: log streaming handler
//   - health_handlers.go: health, stats, reload, upstream CB handlers
//   - runtime_config.go: runtime config get/set handlers
```

- [ ] **Step 3: 运行测试**

```bash
go test -tags=unit -count=1 -short ./internal/server/
```
Expected: PASS

- [ ] **Step 4: gofmt**

```bash
gofmt -w internal/server/provider_state.go internal/server/admin_api.go
```

- [ ] **Step 5: 提交**

```bash
git add internal/server/provider_state.go internal/server/admin_api.go
git commit -m "refactor: 拆分 admin_api.go — 提取 provider_state.go"
```

### Task 2: 创建 auth_handlers.go

**Files:**
- Create: `internal/server/auth_handlers.go`
- Modify: `internal/server/admin_api.go`（删除剩余 auth + log level + config + dashboard + clear 部分）

从 `admin_api.go` 提取：
- checkAdminToken（行 808-824）
- checkAnyAdminToken（行 829-850）
- resolveProvider（行 854-868）
- resolveProviderByName（行 871-884）
- persistRuntimeConfigField（行 888-916）
- persistRuntimeConfigFieldToDefault（行 920-940）
- logLevelHandler（行 89-117）
- configHandler（行 121-144）
- dashboardHandler（行 452-455）
- clearHandler（行 459-469）

- [ ] **Step 1: 创建 `auth_handlers.go`**

```go
package server

import (
	"akswitch/internal/config"
	"akswitch/internal/logentry"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// checkAdminToken, checkAnyAdminToken, resolveProvider,
// resolveProviderByName, persistRuntimeConfigField,
// persistRuntimeConfigFieldToDefault,
// logLevelHandler, configHandler, dashboardHandler, clearHandler
```

- [ ] **Step 2: 更新 `admin_api.go`**

删除已提取的所有函数，仅保留文件头注释。

- [ ] **Step 3: 运行测试 + gofmt + 提交**

```bash
go test -tags=unit -count=1 -short ./internal/server/
gofmt -w internal/server/auth_handlers.go internal/server/admin_api.go
git add internal/server/auth_handlers.go internal/server/admin_api.go
git commit -m "refactor: 拆分 admin_api.go — 提取 auth_handlers.go"
```

### Task 3: 创建 keys_handlers.go

**Files:**
- Create: `internal/server/keys_handlers.go`

从 `admin_api.go` 提取 keysHandler（行 148-225）。

- [ ] **Step 1: 创建 `keys_handlers.go`**

```go
package server

import (
	"akswitch/internal/logentry"
	"encoding/json"
	"net/http"
	"time"
)

func (api *AdminAPI) keysHandler(w http.ResponseWriter, r *http.Request) { ... }
```

- [ ] **Step 2: 更新 `admin_api.go`**

删除 keysHandler（行 148-225）。

- [ ] **Step 3: 运行测试 + gofmt + 提交**

```bash
go test -tags=unit -count=1 -short ./internal/server/
gofmt -w internal/server/keys_handlers.go internal/server/admin_api.go
git add internal/server/keys_handlers.go internal/server/admin_api.go
git commit -m "refactor: 拆分 admin_api.go — 提取 keys_handlers.go"
```

### Task 4: 创建 logs_handler.go

**Files:**
- Create: `internal/server/logs_handler.go`

从 `admin_api.go` 提取 logsHandler（行 308-448）。

- [ ] **Step 1: 创建 `logs_handler.go`**

```go
package server

import (
	"akswitch/internal/logentry"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

func (api *AdminAPI) logsHandler(w http.ResponseWriter, r *http.Request) { ... }
```

- [ ] **Step 2: 更新 `admin_api.go`**

删除 logsHandler（行 308-448）。

- [ ] **Step 3: 运行测试 + gofmt + 提交**

```bash
go test -tags=unit -count=1 -short ./internal/server/
gofmt -w internal/server/logs_handler.go internal/server/admin_api.go
git add internal/server/logs_handler.go internal/server/admin_api.go
git commit -m "refactor: 拆分 admin_api.go — 提取 logs_handler.go"
```

### Task 5: 创建 health_handlers.go

**Files:**
- Create: `internal/server/health_handlers.go`

从 `admin_api.go` 提取：
- swHandler（行 83-85）
- healthHandler（行 229-304）
- statsHandler（行 473-502）
- upstreamCBResetHandler（行 506-529）
- reloadHandler（行 533-562）

- [ ] **Step 1: 创建 `health_handlers.go`**

```go
package server

import (
	"akswitch/internal/circuitbreaker"
	"akswitch/internal/config"
	"log/slog"
	"net/http"
	"time"
)

// swHandler, healthHandler, statsHandler, upstreamCBResetHandler, reloadHandler
```

- [ ] **Step 2: 更新 `admin_api.go`**

删除已提取的函数。

- [ ] **Step 3: 运行测试 + gofmt + 提交**

```bash
go test -tags=unit -count=1 -short ./internal/server/
gofmt -w internal/server/health_handlers.go internal/server/admin_api.go
git add internal/server/health_handlers.go internal/server/admin_api.go
git commit -m "refactor: 拆分 admin_api.go — 提取 health_handlers.go"
```

### Task 6: 创建 runtime_config.go

**Files:**
- Create: `internal/server/runtime_config.go`

从 `admin_api.go` 提取：
- runtimeConfigHandler（行 566-579）
- handleRuntimeConfigGet（行 581-648）
- handleRuntimeConfigSet（行 650-738）
- setRuntimeConfigField（行 743-749）
- getRuntimeParams（行 752-765）

- [ ] **Step 1: 创建 `runtime_config.go`**

```go
package server

import (
	"akswitch/internal/config"
	"encoding/json"
	"fmt"
	"net/http"
)

// runtimeConfigHandler, handleRuntimeConfigGet, handleRuntimeConfigSet,
// setRuntimeConfigField, getRuntimeParams
```

- [ ] **Step 2: 更新 `admin_api.go`**

删除已提取的函数。此时 `admin_api.go` 应只包含：
- 文件包声明 + 导入
- ProviderLookup 接口
- AdminAPI struct
- NewAdminAPI
- RegisterRoutes
- 文件级注释

- [ ] **Step 3: 运行测试 + gofmt + 提交**

```bash
go test -tags=unit -count=1 -short ./internal/server/
gofmt -w internal/server/runtime_config.go internal/server/admin_api.go
git add internal/server/runtime_config.go internal/server/admin_api.go
git commit -m "refactor: 拆分 admin_api.go — 提取 runtime_config.go"
```

---
