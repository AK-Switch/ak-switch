# AK Switch — API Key 轮转代理

[![Go Version](https://img.shields.io/badge/Go-1.26-blue)](https://go.dev)
[![Tests](https://img.shields.io/github/actions/workflow/status/bigmanBass666/akswitch/go.yml?branch=main&label=tests)](https://github.com/bigmanBass666/akswitch/actions)
[![Release](https://img.shields.io/github/v/release/bigmanBass666/akswitch)](https://github.com/bigmanBass666/akswitch/releases)

> 专注单 provider 内 API Key 的智能轮转与熔断，与 [ccswitch](https://github.com/farion1231/cc-switch) 互补。
> ccswitch 负责 provider 级路由与故障转移，AK Switch 负责 provider 内 key 级轮转与限流处理。

---

## 快速开始

```bash
# 编译安装
go install ./cmd/akswitch/

# 初始化配置
akswitch config init

# 添加 provider 和 key
akswitch provider add nvidia --target https://integrate.api.nvidia.com/v1 --port 3001
akswitch key add nvidia nvapi-xxxxxxxxxxxx

# 启动（默认 compact 日志格式）
akswitch start
```

输出示例：

```
[00:00.001] → POST /v1/chat/completions (0KB)
[00:02.345] 200 nvidia (key: nvap...xxxx)
[00:02.346] → POST /v1/chat/completions (2KB)
[00:05.123] 429 nvidia (key: nvap...yyyy)
[00:05.678] → POST /v1/chat/completions (2KB)
[00:07.890] 200 nvidia (key: nvap...zzzz)
```

## 核心功能

- **Key 轮转** — 多 Key 轮询、429 指数退避、401/403 永久禁用、自动恢复
- **RPM 感知选择** — 自动选择请求率最低的 Key，均衡负载
- **两层熔断** — Key 级（限流退避） + 上游级（502/503 熔断 + 半开探测）
- **单端口多 provider** — 一个端口、一个进程管理多个 provider（`/{provider}/...` 路径路由）
- **Token 用量追踪** — 流式/非流式请求的 input/output tokens 提取与校准
- **CLI 管理** — `provider` / `key` 增删查改，`status / logs / stop` 运行时管理
- **加密存储** — API Key 以 AES-256-GCM 加密存储，可选 OS Keyring
- **配置热重载** — TOML 配置修改自动生效，支持 diff 脱敏输出
- **Dashboard** — 内置 Web 实时面板（`/dashboard`）
- **Prometheus 指标** — 开箱即用的监控栈（AK Switch + Prometheus + Grafana）
- **compact 日志** — 终端彩色精简日志输出，支持 `--log-format=default` 切换

## 文档

| 文档 | 说明 |
|------|------|
| [CLI 参考](docs/cli-reference.md) | 所有子命令、标志位、使用示例 |
| [配置说明](docs/configuration.md) | TOML 配置、所有可用字段 |
| [API 文档](docs/api.md) | 代理端点、管理 API、错误码 |
| [熔断器架构](docs/architecture.md) | 两层熔断器状态转移、响应矩阵 |
| [部署指南](docs/deployment.md) | Docker、监控栈、自定义端口 |
| [变更日志](CHANGELOG.md) | 版本历史与里程碑 |

## 项目定位

AK Switch **不做** provider 级路由、请求整流、响应变换（ccswitch 已成熟）。
AK Switch **只做** 单 provider 内多 Key 的智能轮转、限流处理、自动熔断。

## License

[MIT](LICENSE)