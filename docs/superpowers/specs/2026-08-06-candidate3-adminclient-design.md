# Candidate 3 — Extract shared admin API client for CLI

## 问题

CLI 目录下 6 个文件独立实现了相同的 HTTP 请求样板代码：

```
detectServerPort() + detectServerHost() → 构建 URL → &http.Client{} → loadAdminTokenFromConfig() → 发请求 → 401/403 处理
```

重复文件：`reload.go`、`status.go`、`provider.go`、`loglevel.go`、`config.go`、`key.go`

此外，`loadAdminTokenFromConfig` 每次调用都重新读取并解析 TOML 文件。在 `status.go` 单个命令内对同一 config 读取两次。

## 方案

在 `internal/cli/` 新增 `admin_client.go`，定义 `AdminClient` 结构体封装通用通信逻辑。

构造函数 `NewAdminClient(timeout)` 在创建时读取一次 host/port/token 并缓存。

提供 `Do(req)`、`Get(path)`、`Post(path, contentType, body)` 方法，自动注入 `X-Admin-Token` header。

## 新增文件

### `internal/cli/admin_client.go`

- `AdminClient` 结构体：`httpClient`、`baseURL`、`token`
- `NewAdminClient(timeout time.Duration) (*AdminClient, error)`
- `Do(req *http.Request) (*http.Response, error)` — 注入 auth，发送请求
- `Get(path string) (*http.Response, error)`
- `Post(path, contentType string, body io.Reader) (*http.Response, error)`

## 迁移范围

| 文件 | 改动 |
|------|------|
| `reload.go` | `triggerReload()` → `NewAdminClient(3s)` + `client.Get("/api/reload")` |
| `status.go` | 两处请求 → 一个 `NewAdminClient(3s)` |
| `provider.go` | health + stats → 一个 `NewAdminClient(3s)` |
| `loglevel.go` | GET/POST → `NewAdminClient(5s)` |
| `config.go` | 三个 API 调用点 → `NewAdminClient(5s)` |
| `key.go` | `resetUpstreamCB` + `callKeyRuntimeAPI` → `NewAdminClient(5s)` |

## 不变的内容

- `doRuntimeConfigGet` 保留（含 config 特定的响应解析逻辑），内部改用 `AdminClient`
- `loadAdminTokenFromConfig` / `loadAdminToken` / `detectServerPort` / `detectServerHost` 保留
- `usage.go` 的 sensenova 专用 HTTP 逻辑不动（非 admin API）

## 测试

- 新增 `admin_client_test.go`：验证 auth header 注入和响应处理
- 现有命令测试不需要修改（CLI 输出不变）

## 收益

- 消除 6 处 HTTP 样板代码重复
- Token 从"每次调用读一次 TOML"变为"每个命令读一次"
- 新增 CLI 命令时只需 `NewAdminClient` + 调用
