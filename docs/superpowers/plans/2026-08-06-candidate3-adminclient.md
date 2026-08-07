# Candidate 3 — Extract shared admin API client for CLI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/cli/` 新增 `AdminClient`，消除 6 个文件中的 HTTP 请求样板代码重复，同时将 token 加载从"每次调用读一次 TOML"优化为"每个命令读一次"。

**Architecture:** 在 cli 包内新增 `admin_client.go`，`AdminClient` 结构体封装 host/port/token 检测和 HTTP 发送逻辑。6 个文件中的独立请求代码统一替换为 `NewAdminClient` + `client.Get/Post` 调用。`doRuntimeConfigGet` 保留但内部改用 `AdminClient`。

**Tech Stack:** Go, stdlib `net/http`, 项目内部 `config` 包

## Global Constraints

- Tab 缩进，gofmt 风格
- 错误包装格式: `fmt.Errorf("函数名: %w", err)`
- 单元测试: `go test -tags=unit -count=1 ./...`
- 构建验证: `go build ./...`
- `loadAdminTokenFromConfig` / `loadAdminToken` / `detectServerPort` / `detectServerHost` 保留（其他代码和测试仍使用）
- `doRuntimeConfigGet` 保留（含 config 特定的响应解析逻辑）

---

### Task 1: 创建 AdminClient 核心类型

**Files:**
- Create: `internal/cli/admin_client.go`
- Test: `internal/cli/admin_client_test.go`

**Interfaces:**
- Consumes: `detectServerPort()`, `detectServerHost()`, `loadAdminTokenFromConfig()`, `loadAdminToken(provider)`
- Produces: `AdminClient` struct, `NewAdminClient(timeout, provider)`, `Do(req)`, `Get(path)`, `Post(path, contentType, body)`

```go
package cli

import (
    "fmt"
    "io"
    "net/http"
    "os"
    "time"
)

// AdminClient 封装与 akswitch server 通信的通用逻辑。
// 构造函数读取一次 host/port/token 并缓存，后续请求复用。
type AdminClient struct {
    httpClient *http.Client
    baseURL    string
    token      string
}

// NewAdminClient 创建客户端，自动检测 server 地址并加载 admin token。
// provider 参数用于 provider-scoped token；空字符串表示使用 any-admin-token。
func NewAdminClient(timeout time.Duration, provider string) (*AdminClient, error) {
    port := detectServerPort()
    host := detectServerHost()
    baseURL := fmt.Sprintf("http://%s:%d", host, port)

    token := ""
    var err error
    if provider != "" {
        token, err = loadAdminToken(provider)
    } else {
        token, err = loadAdminTokenFromConfig()
    }
    if err != nil {
        fmt.Fprintf(os.Stderr, "warning: failed to load admin token: %v\n", err)
    }

    return &AdminClient{
        httpClient: &http.Client{Timeout: timeout},
        baseURL:    baseURL,
        token:      token,
    }, nil
}

// Do 发送已构建的请求，自动注入 auth header。
func (c *AdminClient) Do(req *http.Request) (*http.Response, error) {
    if c.token != "" {
        req.Header.Set("X-Admin-Token", c.token)
    }
    return c.httpClient.Do(req)
}

// Get 发送 GET 请求。
func (c *AdminClient) Get(path string) (*http.Response, error) {
    req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
    if err != nil {
        return nil, fmt.Errorf("build request: %w", err)
    }
    return c.Do(req)
}

// Post 发送 POST 请求。
func (c *AdminClient) Post(path, contentType string, body io.Reader) (*http.Response, error) {
    req, err := http.NewRequest(http.MethodPost, c.baseURL+path, body)
    if err != nil {
        return nil, fmt.Errorf("build request: %w", err)
    }
    req.Header.Set("Content-Type", contentType)
    return c.Do(req)
}
```

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
    "errors"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"
)

func TestNewAdminClient_BuildsCorrectBaseURL(t *testing.T) {
    // Server not running — detectServerPort returns default, detectServerHost returns default
    // We just verify the struct is created without error
    client, err := NewAdminClient(5*time.Second, "")
    if err != nil {
        t.Fatalf("NewAdminClient() error = %v", err)
    }
    if client == nil {
        t.Fatal("NewAdminClient() returned nil")
    }
    if client.httpClient == nil {
        t.Fatal("httpClient is nil")
    }
    if client.baseURL == "" {
        t.Fatal("baseURL is empty")
    }
}

func TestAdminClient_Do_InjectsAuthHeader(t *testing.T) {
    receivedToken := ""
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        receivedToken = r.Header.Get("X-Admin-Token")
        w.WriteHeader(http.StatusOK)
    }))
    defer srv.Close()

    // Override detect functions to point to test server
    // We create client manually for this test
    client := &AdminClient{
        httpClient: &http.Client{Timeout: 5 * time.Second},
        baseURL:    srv.URL,
        token:      "test-token-123",
    }

    req, _ := http.NewRequest(http.MethodGet, srv.URL+"/test", nil)
    _, err := client.Do(req)
    if err != nil {
        t.Fatalf("Do() error = %v", err)
    }
    if receivedToken != "test-token-123" {
        t.Errorf("token = %q, want %q", receivedToken, "test-token-123")
    }
}

func TestAdminClient_Get_AppendsPath(t *testing.T) {
    var gotPath string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotPath = r.URL.Path
        w.WriteHeader(http.StatusOK)
    }))
    defer srv.Close()

    client := &AdminClient{
        httpClient: &http.Client{Timeout: 5 * time.Second},
        baseURL:    srv.URL,
        token:      "",
    }

    _, err := client.Get("/api/health")
    if err != nil {
        t.Fatalf("Get() error = %v", err)
    }
    if gotPath != "/api/health" {
        t.Errorf("path = %q, want %q", gotPath, "/api/health")
    }
}

func TestAdminClient_Do_ReturnsErrorForNilRequest(t *testing.T) {
    client := &AdminClient{
        httpClient: &http.Client{Timeout: 5 * time.Second},
        baseURL:    "http://example.com",
        token:      "",
    }
    _, err := client.Do(nil)
    if err == nil {
        t.Error("Do(nil) expected error, got nil")
    }
}

func TestAdminClient_Post_SetsContentType(t *testing.T) {
    var gotContentType string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotContentType = r.Header.Get("Content-Type")
        w.WriteHeader(http.StatusOK)
    }))
    defer srv.Close()

    client := &AdminClient{
        httpClient: &http.Client{Timeout: 5 * time.Second},
        baseURL:    srv.URL,
        token:      "",
    }

    _, err := client.Post("/api/test", "application/json", nil)
    if err != nil {
        t.Fatalf("Post() error = %v", err)
    }
    if gotContentType != "application/json" {
        t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
    }
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test -tags=unit -count=1 ./internal/cli/ -run TestNewAdminClient -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/admin_client.go internal/cli/admin_client_test.go
git commit -m "feat: add AdminClient for unified CLI HTTP communication"
```

---

### Task 2: 迁移 reload.go — triggerReload

**Files:**
- Modify: `internal/cli/reload.go:17-49`

**Interfaces:**
- Consumes: `NewAdminClient(timeout, "")`, `AdminClient.Get(path)`
- Produces: 无

```go
func triggerReload() bool {
    client, err := NewAdminClient(3*time.Second, "")
    if err != nil {
        return false
    }

    resp, err := client.Get("/api/reload")
    if err != nil {
        return false
    }
    defer func() { _ = resp.Body.Close() }()

    body, _ := io.ReadAll(resp.Body)

    if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
        fmt.Fprintf(os.Stderr, "reload auth failed (HTTP %d): check X-Admin-Token in server config\n", resp.StatusCode)
        return false
    }
    if resp.StatusCode != http.StatusOK {
        fmt.Fprintf(os.Stderr, "reload failed (HTTP %d): %s\n", resp.StatusCode, string(body))
        return false
    }
    return true
}
```

注意：移除了 `io` import 的使用（`io.ReadAll` 仍在 body 读取处），`fmt`、`os`、`net/http` 保留。

- [ ] **Step 1: Edit reload.go — replace triggerReload body**

```go
func triggerReload() bool {
    client, err := NewAdminClient(3*time.Second, "")
    if err != nil {
        return false
    }

    resp, err := client.Get("/api/reload")
    if err != nil {
        return false
    }
    defer func() { _ = resp.Body.Close() }()

    body, _ := io.ReadAll(resp.Body)

    if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
        fmt.Fprintf(os.Stderr, "reload auth failed (HTTP %d): check X-Admin-Token in server config\n", resp.StatusCode)
        return false
    }
    if resp.StatusCode != http.StatusOK {
        fmt.Fprintf(os.Stderr, "reload failed (HTTP %d): %s\n", resp.StatusCode, string(body))
        return false
    }
    return true
}
```

- [ ] **Step 2: Run tests**

Run: `go test -tags=unit -count=1 ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/reload.go
git commit -m "refactor: reload.go triggerReload uses AdminClient"
```

---

### Task 3: 迁移 status.go — statusCmd

**Files:**
- Modify: `internal/cli/status.go`

**Interfaces:**
- Consumes: `NewAdminClient(3*time.Second, "")`, `AdminClient.Get(path)`

```go
RunE: func(cmd *cobra.Command, args []string) error {
    client, err := NewAdminClient(3*time.Second, "")
    if err != nil {
        return err
    }

    providerName := ""
    if len(args) > 0 {
        providerName = args[0]
    }

    // Health check
    healthPath := "/health"
    if providerName != "" {
        healthPath += "?provider=" + providerName
    }
    healthReq, herr := http.NewRequest(http.MethodGet, client.baseURL+healthPath, nil)
    if herr != nil {
        return fmt.Errorf("server not reachable at %s: %w", client.baseURL+healthPath, herr)
    }
    healthResp, err := client.Do(healthReq)
    if err != nil {
        return fmt.Errorf("server not reachable at %s: %w", client.baseURL+healthPath, err)
    }
    defer func() { _ = healthResp.Body.Close() }()
    // ... rest of health handling unchanged

    // Stats — use client.Get directly
    statsPath := "/api/stats"
    if providerName != "" {
        statsPath += "?provider=" + providerName
    }
    statsResp, err := client.Get(statsPath)
    // ... rest unchanged
```

- [ ] **Step 1: Edit status.go — replace client creation and first request**

Replace lines 27-46 with:

```go
    client, err := NewAdminClient(3*time.Second, "")
    if err != nil {
        return err
    }

    providerName := ""
    if len(args) > 0 {
        providerName = args[0]
    }

    healthPath := "/health"
    if providerName != "" {
        healthPath += "?provider=" + url.QueryEscape(providerName)
    }
    healthReq, err := http.NewRequest(http.MethodGet, client.baseURL+healthPath, nil)
    if err != nil {
        return fmt.Errorf("server not reachable at %s: %w", client.baseURL+healthPath, err)
    }
    healthResp, err := client.Do(healthReq)
```

- [ ] **Step 2: Edit status.go — replace stats request (lines 83-97)**

Replace:

```go
    statsURL := fmt.Sprintf("http://%s:%d/api/stats", host, port)
    if providerName != "" {
        statsURL += "?provider=" + providerName
    }
    statsReq, err := http.NewRequest(http.MethodGet, statsURL, nil)
    if err != nil {
        return fmt.Errorf("server not reachable at %s: %w", statsURL, err)
    }
    if token, tokErr := loadAdminTokenFromConfig(); tokErr == nil && token != "" {
        statsReq.Header.Set("X-Admin-Token", token)
    }

    statsResp, err := client.Do(statsReq)
```

With:

```go
    statsPath := "/api/stats"
    if providerName != "" {
        statsPath += "?provider=" + url.QueryEscape(providerName)
    }
    statsResp, err := client.Get(statsPath)
```

- [ ] **Step 3: Run tests**

Run: `go test -tags=unit -count=1 ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cli/status.go
git commit -m "refactor: status.go uses AdminClient for health and stats"
```

---

### Task 4: 迁移 provider.go — providerInfoCmd

**Files:**
- Modify: `internal/cli/provider.go:506-558`

**Interfaces:**
- Consumes: `NewAdminClient(3*time.Second, "")`, `AdminClient.Get(path)`

provider.go 的 providerInfoCmd 有两段 HTTP 请求（health + stats），且当前使用 `if err == nil` 的容错模式（server 不运行时不报错，只打印 "not running"）。迁移时保留此行为。

Replace lines 506-558 with:

```go
            // Runtime status (try server)
            client, clientErr := NewAdminClient(3*time.Second, "")
            if clientErr == nil {
                healthPath := "/health?provider=" + url.QueryEscape(name)
                healthReq, _ := http.NewRequest(http.MethodGet, client.baseURL+healthPath, nil)
                healthResp, herr := client.Do(healthReq)
                if herr == nil {
                    body, _ := io.ReadAll(healthResp.Body)
                    _ = healthResp.Body.Close()
                    if healthResp.StatusCode == http.StatusOK {
                        var healthData map[string]interface{}
                        if json.Unmarshal(body, &healthData) == nil {
                            if details, ok := healthData["details"]; ok {
                                if det, ok2 := details.(map[string]interface{}); ok2 {
                                    if info, ok3 := det[name]; ok3 {
                                        if inf, ok4 := info.(map[string]interface{}); ok4 {
                                            cbState := "unknown"
                                            if cs, ok5 := inf["upstream_cb_state"]; ok5 {
                                                cbState = fmt.Sprintf("%v", cs)
                                            }
                                            fmt.Printf("  Status:  running  →  %s\n", client.baseURL)
                                            fmt.Printf("  CB:      %s\n", cbState)
                                        }
                                    }
                                }
                            }
                        }
                    }

                    // Stats
                    statsPath := "/api/stats?provider=" + url.QueryEscape(name)
                    statsResp, serr := client.Get(statsPath)
                    if serr == nil {
                        statsBody, _ := io.ReadAll(statsResp.Body)
                        _ = statsResp.Body.Close()
                        var stats map[string]interface{}
                        if json.Unmarshal(statsBody, &stats) == nil {
                            fmt.Printf("  Requests: %v (success: %v, failed: %v)\n",
                                stats["total_requests"], stats["successful_requests"], stats["failed_requests"])
                        }
                    }
                }
            }
```

注意：`host` 和 `port` 变量不再需要，因为 `client.baseURL` 包含完整地址。删除对 `detectServerPort()` 和 `detectServerHost()` 的调用。

- [ ] **Step 1: Edit provider.go — replace lines 507-558**

- [ ] **Step 2: Run tests**

Run: `go test -tags=unit -count=1 ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/provider.go
git commit -m "refactor: provider.go providerInfoCmd uses AdminClient"
```

---

### Task 5: 迁移 loglevel.go

**Files:**
- Modify: `internal/cli/loglevel.go`

**Interfaces:**
- Consumes: `NewAdminClient(5*time.Second, "")`, `AdminClient.Get(path)`, `AdminClient.Post(path, contentType, body)`

Replace lines 30-76 with:

```go
    client, err := NewAdminClient(5*time.Second, "")
    if err != nil {
        return fmt.Errorf("server not reachable at %s:%d: %w", detectServerHost(), detectServerPort(), err)
    }

    if len(args) == 0 {
        resp, err := client.Get("/api/log-level")
        if err != nil {
            return fmt.Errorf("server not reachable at %s: %w", client.baseURL, err)
        }
        defer func() { _ = resp.Body.Close() }()
        // ... rest unchanged
    }

    // POST — set log level
    payload := fmt.Sprintf(`{"level":"%s"}`, level)
    resp, err := client.Post("/api/log-level", "application/json", strings.NewReader(payload))
    if err != nil {
        return fmt.Errorf("server not reachable at %s: %w", client.baseURL, err)
    }
    // ... rest unchanged
```

- [ ] **Step 1: Edit loglevel.go — replace client creation and requests**

- [ ] **Step 2: Run tests**

Run: `go test -tags=unit -count=1 ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/loglevel.go
git commit -m "refactor: loglevel.go uses AdminClient"
```

---

### Task 6: 迁移 config.go — configListCmd, configGetCmd, configSetCmd, doRuntimeConfigGet

**Files:**
- Modify: `internal/cli/config.go:182-507`

**Interfaces:**
- Consumes: `NewAdminClient(5*time.Second, "")`, `AdminClient.Get(path)`, `AdminClient.Post(path, contentType, body)`, `AdminClient.Do(req)`

三个命令各创建一个 `&http.Client{Timeout: 5 * time.Second}` + `detectServerHost()` + `detectServerPort()` + `loadAdminTokenFromConfig()` 的样板。`doRuntimeConfigGet` 也接收 `*http.Client` 并重复 auth 逻辑。

**configListCmd** (line 182-211):

```go
    client, err := NewAdminClient(5*time.Second, "")
    if err != nil {
        return fmt.Errorf("server not reachable: %w", err)
    }
    // ... build path
    resp, err := client.Get(path)
    if err != nil {
        return fmt.Errorf("server not reachable: %w", err)
    }
    defer func() { _ = resp.Body.Close() }()
    body, _ := io.ReadAll(resp.Body)
    if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
        return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", resp.StatusCode)
    }
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
    }
    // ... parsing unchanged
```

**configGetCmd** (line 272-367):

```go
    client, err := NewAdminClient(5*time.Second, "")
    // ... for --all branch: client.Get(paramsURL)
    // ... for single key: client.Get(paramsURL)
```

**configSetCmd** (line 387-443):

```go
    client, err := NewAdminClient(5*time.Second, "")
    // ... build payload
    resp, err := client.Post(path, "application/json", strings.NewReader(payload))
    // ... response handling unchanged
```

**doRuntimeConfigGet** (line 478-507) — 保留函数，签名改为接受 `*AdminClient`:

```go
func doRuntimeConfigGet(client *AdminClient, baseURL string) (map[string]interface{}, error) {
    req, err := http.NewRequest(http.MethodGet, client.baseURL+baseURL, nil)
    if err != nil {
        return nil, fmt.Errorf("server not reachable: %w", err)
    }
    // auth is already set by NewAdminClient, no need to set header again

    resp, err := client.Do(req)
    // ... rest unchanged
```

注意：`doRuntimeConfigGet` 的调用方 `configGetCmd --all` 分支需要传递 `*AdminClient` 而不是 `*http.Client`。`baseURL` 参数现在只包含 query string（因为 `client.baseURL` 已是 host:port 前缀）。

- [ ] **Step 1: Edit config.go — configListCmd (replace lines 182-211)**

- [ ] **Step 2: Edit config.go — configGetCmd (replace lines 272-340)**

注意 `--all` 分支中 `doRuntimeConfigGet` 的调用改为传入 `*AdminClient`。

- [ ] **Step 3: Edit config.go — configSetCmd (replace lines 387-443)**

- [ ] **Step 4: Edit config.go — doRuntimeConfigGet (update signature and body)**

- [ ] **Step 5: Run tests**

Run: `go test -tags=unit -count=1 ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/config.go
git commit -m "refactor: config.go commands use AdminClient"
```

---

### Task 7: 迁移 key.go — resetUpstreamCB, callKeyRuntimeAPI, keyListRuntime

**Files:**
- Modify: `internal/cli/key.go:566-776`

**Interfaces:**
- Consumes: `NewAdminClient(5*time.Second, provider)`, `AdminClient.Get(path)`, `AdminClient.Post(path, contentType, body)`

注意：`resetUpstreamCB`、`callKeyRuntimeAPI`、`keyListRuntime` 使用 `loadAdminToken(provider)`（provider-scoped token），`NewAdminClient` 传入 provider 参数即可。

**resetUpstreamCB** (line 566-595):

```go
func resetUpstreamCB(provider string) error {
    client, err := NewAdminClient(5*time.Second, provider)
    if err != nil {
        return err
    }
    path := "/api/stats/reset-upstream-cb?provider=" + url.QueryEscape(provider)
    resp, err := client.Post(path, "application/json", nil)
    if err != nil {
        return fmt.Errorf("server not reachable: %w", err)
    }
    defer func() { _ = resp.Body.Close() }()

    body, _ := io.ReadAll(resp.Body)
    if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
        return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", resp.StatusCode)
    }
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
    }
    return nil
}
```

**callKeyRuntimeAPI** (line 676-714):

```go
func callKeyRuntimeAPI(provider string, idx int, operation string) error {
    client, err := NewAdminClient(5*time.Second, provider)
    if err != nil {
        return fmt.Errorf("server not reachable: %w", err)
    }
    path := fmt.Sprintf("/api/keys/%d/%s?provider=%s", idx, operation, url.QueryEscape(provider))

    var reqBody io.Reader
    if operation == "cooldown" {
        reqBody = strings.NewReader(`{}`)
    }

    method := http.MethodPost
    if operation == "cooldown" {
        method = http.MethodPut
    }
    resp, err := client.Do(func() (*http.Request, error) {
        return http.NewRequest(method, client.baseURL+path, reqBody)
    }())
```

等等——`client.Do` 需要 `*http.Request`，不能用 `client.Post`（因为 PUT 方法）。改用 `http.NewRequest` + `client.Do`:

```go
func callKeyRuntimeAPI(provider string, idx int, operation string) error {
    client, err := NewAdminClient(5*time.Second, provider)
    if err != nil {
        return fmt.Errorf("server not reachable: %w", err)
    }

    path := fmt.Sprintf("/api/keys/%d/%s?provider=%s", idx, operation, url.QueryEscape(provider))
    var reqBody io.Reader
    if operation == "cooldown" {
        reqBody = strings.NewReader(`{}`)
    }
    method := http.MethodPost
    if operation == "cooldown" {
        method = http.MethodPut
    }
    req, err := http.NewRequest(method, client.baseURL+path, reqBody)
    if err != nil {
        return fmt.Errorf("server not reachable: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("server not reachable: %w", err)
    }
    defer func() { _ = resp.Body.Close() }()

    body, _ := io.ReadAll(resp.Body)
    if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
        return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", resp.StatusCode)
    }
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
    }
    return nil
}
```

**keyListRuntime** (line 717-776):

```go
func keyListRuntime(provider string) error {
    client, err := NewAdminClient(5*time.Second, provider)
    if err != nil {
        return fmt.Errorf("server not reachable: %w", err)
    }

    path := "/api/keys?provider=" + url.QueryEscape(provider)
    resp, err := client.Get(path)
    if err != nil {
        return fmt.Errorf("server not reachable: %w", err)
    }
    defer func() { _ = resp.Body.Close() }()

    body, _ := io.ReadAll(resp.Body)
    if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
        return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", resp.StatusCode)
    }
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
    }
    // ... JSON parse and display unchanged
```

- [ ] **Step 1: Edit key.go — replace resetUpstreamCB (lines 566-595)**

- [ ] **Step 2: Edit key.go — replace callKeyRuntimeAPI (lines 676-714)**

- [ ] **Step 3: Edit key.go — replace keyListRuntime (lines 717-776)**

- [ ] **Step 4: Run tests**

Run: `go test -tags=unit -count=1 ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/key.go
git commit -m "refactor: key.go key operations use AdminClient"
```

---

### Task 8: 验证构建和完整测试

- [ ] **Step 1: 构建验证**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 2: 单元测试**

Run: `go test -tags=unit -count=1 ./...`
Expected: PASS

- [ ] **Step 3: vet**

Run: `go vet ./...`
Expected: PASS

- [ ] **Step 4: 检查未使用的 import**

确认以下文件没有残留未使用的 import：
- `reload.go`: `io` 仍被 `io.ReadAll` 使用，保留
- `status.go`: `io` 仍被使用，保留
- `provider.go`: `io` 仍被使用，保留
- `loglevel.go`: 检查 `io` 是否仍被使用（GET 分支用了 `io.ReadAll`，保留）
- `config.go`: `io` 仍被使用，保留
- `key.go`: 检查 `net/http` 是否仍被使用（`http.StatusUnauthorized` 等常量仍被使用，保留）

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: verify build and tests pass after AdminClient migration"
```

---

## 不变的内容（验证清单）

迁移完成后确认以下内容未被改动：

- [ ] `doRuntimeConfigGet` 函数签名变为接受 `*AdminClient`（而非删除）
- [ ] `loadAdminTokenFromConfig` 函数保留（`AdminClient` 内部使用）
- [ ] `loadAdminToken(provider)` 函数保留（`AdminClient` 内部使用）
- [ ] `detectServerPort()` 函数保留（`AdminClient` 内部使用，`root_test.go` 测试依赖）
- [ ] `detectServerHost()` 函数保留（`AdminClient` 内部使用）
- [ ] `usage.go` 的 sensenova HTTP 逻辑不动
- [ ] 所有 `*_test.go` 的命令存在性测试不变
- [ ] CLI 输出文本不变
