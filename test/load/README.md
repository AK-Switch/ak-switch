# AK Switch 负载压测

本项目使用 [Vegeta](https://github.com/tsenart/vegeta) 进行 HTTP 负载压测。

## 环境准备

### 安装 Vegeta

```powershell
# winget
winget install vegeta

# 或手动下载: https://github.com/tsenart/vegeta/releases
# 将 vegeta.exe 放到 PATH 中
```

验证安装：

```powershell
vegeta version
```

## 压测脚本

`run-load-test.ps1` 是统一的压测执行脚本，支持多个场景。

### 使用方法

```powershell
# 列出所有可用场景
.\test\load\run-load-test.ps1 -ListScenarios

# 运行正常并发场景（目标默认 http://localhost:8080）
.\test\load\run-load-test.ps1 -Scenario normal-concurrent

# 指定目标地址
.\test\load\run-load-test.ps1 -Scenario normal-concurrent -Target http://localhost:4000

# 通过环境变量设置目标
$env:AKSWITCH_TARGET = 'http://localhost:4000'
.\test\load\run-load-test.ps1 -Scenario normal-concurrent
```

### 参数说明

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-Scenario` | 场景名：`normal-concurrent`, `all-keys-cooldown`, `upstream-flaky` | 必填 |
| `-Target` | 目标代理地址 | `$env:AKSWITCH_TARGET` 或 `http://localhost:8080` |
| `-OutputDir` | 结果输出目录 | `test/load/results/` |
| `-ListScenarios` | 列出可用场景 | - |

### 输出结构

每次压测生成一个带时间戳的目录：

```
test/load/results/
  normal-concurrent-20250101-120000/
    targets.txt      # Vegeta target 文件
    results.bin      # Vegeta 二进制结果
    report.txt       # 文本报告
    report.json      # JSON 报告
    plot.html        # HTML 延迟分布图
    scenario.json    # 场景配置副本
    attack.log       # 攻击日志
    attack.err       # 错误日志
```

## 压测场景

### 1. 正常并发 (`normal-concurrent`)

- **速率**: 500 QPS
- **持续时间**: 60s
- **并发 Workers**: 50
- **目标**: POST /v1/chat/completions
- **预期**: p99 延迟 < 2000ms，零错误率

**前置条件**:
- 代理配置 3+ 个有效 API Key
- 上游正常响应

**执行**:
```powershell
.\test\load\run-load-test.ps1 -Scenario normal-concurrent -Target http://localhost:8080
```

### 2. 全 Key 冷却 (`all-keys-cooldown`)

- **速率**: 100 QPS
- **持续时间**: 120s
- **并发 Workers**: 10
- **目标**: POST /v1/chat/completions
- **预期**: 所有请求返回 429（或自定义降级响应），无崩溃

**前置条件**:
- 代理配置 3 个 Key
- 上游配置为**全部返回 429**

**执行**:
```powershell
.\test\load\run-load-test.ps1 -Scenario all-keys-cooldown -Target http://localhost:8080
```

**模拟方法**: 启动 AK Switch 时配置上游指向一个专门返回 429 的 mock server。

### 3. 上游 429 抖动 (`upstream-flaky`)

- **速率**: 200 QPS
- **持续时间**: 120s
- **并发 Workers**: 20
- **目标**: POST /v1/chat/completions
- **预期**: KeyPool 正确冷却/恢复，无 Key 泄漏，无异常崩溃

**前置条件**:
- 代理配置 3+ 个 Key
- 上游约 50% 请求返回 429

**执行**:
```powershell
.\test\load\run-load-test.ps1 -Scenario upstream-flaky -Target http://localhost:8080
```

## Go Benchmark

除 Vegeta 压测外，项目中还包含 Go 标准 benchmark：

```powershell
# KeyPool 基准测试
go test -bench=BenchmarkKeyPoolNext -benchmem ./internal/keypool/

# Proxy Handler 基准测试
go test -bench=BenchmarkProxy -benchmem .
```

结果示例：
```
BenchmarkKeyPoolNext/keys-1-8      25643052   49.93 ns/op   0 B/op   0 allocs/op
BenchmarkKeyPoolNext/keys-5-8      23899953   49.76 ns/op   0 B/op   0 allocs/op
BenchmarkKeyPoolNext/keys-10-8     23267898   49.96 ns/op   0 B/op   0 allocs/op

BenchmarkProxyRequest-8                7956  165379 ns/op  130393 B/op   201 allocs/op
```

## 校验清单

- [ ] Go benchmark 可运行 (`go test -bench=. ./internal/keypool/`)
- [ ] Vegeta 已安装 (`vegeta version`)
- [ ] 压测脚本语法正确 (`PowerShell -NoProfile -Command "& '.\test\load\run-load-test.ps1' -ListScenarios"`)
- [ ] `go vet ./...` 零警告
- [ ] `go test -race ./...` 全过