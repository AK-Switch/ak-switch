# Issue #277 — Code Review Findings: Fix Plan

## Scope

修复外部代码审查发现的 7 个代码问题，分 3 个 PR 串行合入。

## PR 1 — Go 版本降级到 1.24

### 变更

- `go.mod`: `go 1.26.0` → `go 1.24`
- `Dockerfile`: `FROM golang:1.26-alpine` → `FROM golang:1.24-alpine`

### 理由

审查者依赖列表中无任何包要求 Go 1.26，1.26 不是广泛稳定部署版本，企业 CI 环境通常使用 1.22/1.23 LTS。

### 风险

仅版本声明变更，不涉及代码逻辑。Go 1.24 是当前最新稳定版（LTS），与 1.26 语法兼容。

### 验证

- `go build ./...` 正常编译
- `make check && make test-unit` 通过

---

## PR 2 — 安全加固

### 2.1 Admin Token 时序攻击修复

**位置:** `internal/server/router.go:156-158`

**问题:** `==` 字符串比较存在 timing side-channel，攻击者可通过响应时间差异逐字符推断 token。

**修复:**

```go
import "crypto/subtle"

func (ps *ProviderState) CheckAdminToken(token string) bool {
    if ps.config.AdminToken == "" {
        return false
    }
    return subtle.ConstantTimeCompare([]byte(ps.config.AdminToken), []byte(token)) == 1
}
```

关键: `subtle.ConstantTimeCompare` 保证比较时间与输入无关，即使 token 为空也不提前返回（`== ""` 检查移到函数内部，时间特征一致）。

### 2.2 Admin API 空 Token 拒绝访问

**位置:** `internal/server/admin_api.go:846-849`

**问题:** `checkAnyAdminToken` 在没有 provider 配置 AdminToken 时直接放行，管理接口裸奔。

**修复:**

```go
func (api *AdminAPI) checkAnyAdminToken(w http.ResponseWriter, r *http.Request) bool {
    // ... 现有逻辑 ...
    if !hasAnyToken {
        http.Error(w, "admin token not configured", http.StatusForbidden)
        return false
    }
    if !matched {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return false
    }
    return true
}
```

行为变更: 任何情况下访问 Admin API 都必须提供有效 token。旧行为（无 token 配置时放行）移除。

### 2.3 Key 文件权限收紧

**位置:**

- `internal/keypool/store.go:89` — SaveFullStore
- `internal/keypool/store.go:115` — SaveKeys (file fallback)
- `internal/keypool/store.go:150` — backup 写入
- `internal/config/config_toml.go:53` — SaveTomlConfig

**问题:** `os.WriteFile(path, data, 0644)` 导致其他用户可读 API Key 文件。

**修复:** 所有写入改为 `0600`（owner only）。

```go
os.WriteFile(path, data, 0600)
```

注意: 测试文件中已有的 `0644` 测试用例保持不动，因为测试 tmp 目录不需要严格权限。

### 验证

- `go build ./...` + `make test-unit` 通过
- `admin_token_test.go` 现有测试覆盖 `CheckAdminToken` 和 `checkAnyAdminToken` 的主要路径
- 新增测试: 空 token 时 `checkAnyAdminToken` 返回 false + 403

---

## PR 3 — 健壮性修复

### 3.1 FirstProvider() 排序 Bug

**位置:** `internal/server/provider_manager.go:64-72`

**问题:** 注释声称"返回字母排序第一个"，但 `for _, ps := range pm.providers` 在 Go map 上是随机序。

**修复:**

```go
func (pm *ProviderManager) FirstProvider() *ProviderState {
    pm.mu.RLock()
    defer pm.mu.RUnlock()

    names := pm.ProviderNames()
    if len(names) == 0 {
        return nil
    }
    return pm.providers[names[0]]
}
```

`ProviderNames()` 已对 key 排序（确认: 检查实现），直接取第一个即可。

### 3.2 ProviderManager.ForEach 快照迭代

**位置:** `internal/server/provider_manager.go:74-81`

**问题:** 持 RLock 期间执行外部 callback，callback 可能慢/再调用 ProviderManager 导致死锁。

**修复:**

```go
func (pm *ProviderManager) ForEach(fn func(name string, ps *ProviderState)) {
    pm.mu.RLock()
    providers := make(map[string]*ProviderState, len(pm.providers))
    for name, ps := range pm.providers {
        providers[name] = ps
    }
    pm.mu.RUnlock()

    for name, ps := range providers {
        fn(name, ps)
    }
}
```

锁只保护快照复制阶段，迭代在解锁后进行。

### 3.3 buildTargetURL 用 URL 解析

**位置:** `internal/server/proxy_handler.go:52-60`

**问题:** 字符串拼接 + 硬截 `path[3:]`，`TargetBase` 结尾 `/v10` + path `/v1/chat` 会误匹配。

**修复:**

```go
import (
    "net/url"
    "strings"
)

func buildTargetURL(cfg *config.Config, path, rawQuery string) string {
    base, _ := url.Parse(cfg.TargetBase)
    // Remove "/v1" prefix from path if base already ends with it
    if strings.HasSuffix(base.Path, "/v1") && strings.HasPrefix(path, "/v1") {
        path = path[len("/v1"):]
    }
    base.Path = strings.TrimRight(base.Path, "/") + path
    if rawQuery != "" {
        base.RawQuery = rawQuery
    }
    return base.String()
}
```

用 `url.Parse` 解析 TargetBase，只检查 `base.Path` 而非整个 URL，消除误匹配。

### 3.4 Docker HEALTHCHECK 命令修复

**位置:** `Dockerfile:29-30`

**问题:** `wget -s` 不是所有 busybox 版本支持。

**修复:**

```dockerfile
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O - http://localhost:3000/health || exit 1
```

添加 `-O -` 输出到 stdout + `|| exit 1` 确保失败时容器标记为 unhealthy。

### 验证

- `go build ./...` + `make test-unit` 通过
- `provider_manager_test.go` 和 `proxy_handler_test.go` 已有相关测试
- 新增: `FirstProvider()` 在有序 map 上返回第一个的测试

---

## 总体验证

每个 PR 独立合入，每个 PR 通过后执行:

```
make check && make test-unit
```

全部 3 个 PR 合入后执行全量测试:

```
make test-all
```

## 执行顺序

1. PR 1（Go 版本） → 合入
2. PR 2（安全加固） → 合入
3. PR 3（健壮性修复） → 合入
