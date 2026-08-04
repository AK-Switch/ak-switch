# #239 CLI start.go 启动逻辑剥离 — 设计文档

## 问题

`internal/cli/start.go` 包含 355 行代码，承担了服务器启动编排、provider 解析与选择、provider 初始化、PID 文件管理、信号处理与优雅关闭、二进制自监控重启等职责。CLI 命令层不应该包含服务器启动逻辑。

## 目标

将服务器启动逻辑提取到 `server` 包中的 `ServerLauncher`，CLI `start.go` 减少到 ~80 行，仅保留命令解析和 `ServerLauncher` 调用。

## 设计

### 新增 ServerLauncher 结构体

```go
// ServerLauncher encapsulates the full server startup sequence.
// All parameters are primitive types — no Cobra dependency.
type ServerLauncher struct {
    dashboardHTML  string
    providerFilter string
    startAll       bool
    logFormat      string
    logLevel       string
    devMode        bool
}

func NewServerLauncher(dashboardHTML, providerFilter string, startAll, devMode bool, logFormat, logLevel string) *ServerLauncher

func (sl *ServerLauncher) Launch() error
```

`Launch()` 返回 `error`，由 CLI `Run` 函数处理退出逻辑。CLI 层只需：

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

### 迁移的函数

| CLI 函数 | ServerLauncher 方法 | 说明 |
|----------|-------------------|------|
| `startServer()` | `Launch()` | 编排入口，内部调用以下步骤 |
| `resolveProviders()` | `sl.resolveProviders()` | 配置加载 + provider 选择 |
| `initProviders()` | `sl.initProviders()` | provider 注册到 router |
| `writePIDFile()` | `sl.writePIDFile()` | PID 文件写入 |
| `waitForShutdown()` | `sl.waitForShutdown()` | 信号监听 + 优雅关闭 |
| `loadKeysForProvider()` | `sl.loadKeysForProvider()` | key 加载 |
| `pidFilePath()` | `sl.pidFilePath()` | PID 路径计算 |
| `checkPidFile()` | `sl.checkPidFile()` | PID 存在性检查 |

### 不变项

- `selfrestart.go` 保持独立。它通过 PID 路径和全局 `binaryUpdated` 与启动流程交互。
  - `pidFilePath()` 迁移到 `ServerLauncher`，`selfrestart.go` 通过公开访问方式调用
- `start` 子命令的 flag 和行为不变
- `SetupSelfRestart` 和 `ExecRestart` 留在 `cli` 包，不进入 `ServerLauncher`

### 跨包边界

```
cli/start.go                 cli/selfrestart.go
    │                              │
    ├──→ server.ServerLauncher ←──┘
    │       │
    │       ├── config 包（已有）
    │       ├── keypool 包（已有）
    │       └── server.ProviderRouter（已有）
    │
    └── 直接调用 selfrestart.SetupSelfRestart()、selfrestart.ExecRestart()
```

`ServerLauncher` 不导入 `cli` 包（避免循环）。

### 错误处理

`Launch()` 返回 `error`。调用方（CLI `Run`）统一处理 `os.Exit(1)`。内部原有 `os.Exit()` 调用改为返回错误。

### 测试

- `server` 包新增 `server_launcher_test.go`，测试 PID 路径和 PID 检查逻辑（使用临时目录）
- CLI `start.go` 的 flag 解析测试保持不动
- 集成测试保持现有模式

### 实现步骤

1. 创建 `internal/server/server_launcher.go`，定义 `ServerLauncher` 结构体和 `Launch()` 方法
2. 将 8 个函数从 `start.go` 迁移到 `server_launcher.go`
3. 精简 `start.go` 到 ~80 行
4. 调整 `selfrestart.go` 调用 PID 路径函数
5. 验证 `go test -tags=unit ./...` 通过
