---
name: remote-monitoring
description: |
  Manage AK-Switch's remote monitoring infrastructure (Prometheus, Grafana, nssm, port forwarding).
  Trigger when the user asks about: server metrics, monitoring stack, Prometheus targets, Grafana dashboards,
  remote service management (start/stop Prometheus or Grafana), port forwarding setup, WSL portproxy,
  nssm service operations, checking metrics endpoint, or any question about the remote monitoring environment.
  Also triggers when the user says "监控" or mentions the remote machine's monitoring setup.
  This skill handles the application layer; connection to the remote machine (SSH, SMB, Tailscale) is
  handled by [[tailscale-ssh]].
---

# AK-Switch Remote Monitoring

## 架构概览

```
┌─ 本机 ({{本机TailscaleIP}}) ──────────────────┐
│  akswitch (port 4000)  —─ curl ──┐          │
│  /metrics 端点                   │          │
└───────────────────────────────────┼──────────┘
                                    │ Tailscale
┌─ 远程 Windows ({{远程TailscaleIP}}) ──────────┐
│  ┌─ WSL2 Ubuntu 22.04 ────────────▼───────┐  │
│  │  Prometheus (端口 9090)                  │  │
│  │    └─ 每 15s scrape metrics              │  │
│  │  Grafana (端口 3000)                     │  │
│  │    └─ 数据源 → localhost:9090             │  │
│  └──────────────────────────────────────────┘  │
│  端口转发: 0.0.0.0:3000 → WSL:3000             │
│  nssm 服务: Prometheus (手动启动)               │
└────────────────────────────────────────────────┘
```

## 变量发现指南

本 skill 中使用 `{{变量名}}` 占位符表示实际环境中需要替换的值。以下是发现这些值的方法：

| 变量 | 如何发现 |
|------|---------|
| `{{本机TailscaleIP}}` | 运行 `tailscale ip -4`，或查看 `akswitch status` 输出的绑定地址，或检查 `~/.akswitch/config.toml` 的 `host` 字段 |
| `{{远程TailscaleIP}}` | 运行 `ssh remote-laptop "tailscale ip -4"`，或查看 Tailscale 管理后台 |
| `{{用户目录}}` | 本机：`$env:USERPROFILE`（Windows）；远程：`echo ~` |

> 如果 SSH 到远程机器时不使用 `remote-laptop` 别名，请先通过 [[tailscale-ssh]] 确认正确的 SSH 连接方式。

## 快速参考

### 关键地址

| 项目 | 值 | 如何发现 |
|------|-----|---------|
| 本机 Tailscale IP | `{{本机TailscaleIP}}` | 运行 `tailscale ip -4` 或查看 `akswitch status` 输出的绑定地址 |
| 远程 Tailscale IP | `{{远程TailscaleIP}}` | `ssh remote-laptop "tailscale ip -4"` |
| SSH 别名 | `remote-laptop` | 已配置在 `~/.ssh/config` |
| 本地 akswitch 端口 | 4000 | `~/.akswitch/config.toml` 中的 `port` 字段 |
| Prometheus 端口 | 9090（WSL 内） | 固定配置，无需查找 |
| Grafana 端口 | 3000（通过 Tailscale 访问） | 固定配置，无需查找 |
| Metrics 端点 | `http://{{本机TailscaleIP}}:4000/metrics` | 用 `{{本机TailscaleIP}}` 替换 |
| Grafana 面板 | `http://{{远程TailscaleIP}}:3000` | 用 `{{远程TailscaleIP}}` 替换 |

### 服务管理命令速查

```bash
# ── 远程 Prometheus ──
ssh remote-laptop "net start Prometheus"    # 启动
ssh remote-laptop "net stop Prometheus"     # 停止
ssh remote-laptop "sc query Prometheus"     # 查看状态
ssh remote-laptop "sc stop Prometheus && sc start Prometheus"  # 重启（替代 nssm restart）

# ── 远程 Grafana ──
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- systemctl start grafana-server"
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- systemctl stop grafana-server"
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- systemctl status grafana-server"

# ── 端口转发（WSL 重启后执行）──
ssh remote-laptop "schtasks /run /tn \"WSL2 Port Forwarding\""

# ── 验证 Prometheus 抓取状态 ──
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- curl -s http://localhost:9090/api/v1/targets"

# ── 验证 Grafana 进程存活 ──
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- systemctl is-active grafana-server"

# ── 本地 metrics 检查 ──
curl -s http://{{本机TailscaleIP}}:4000/metrics | head -20
akswitch status
```

## 配置参考

### `~/.akswitch/config.toml`

```toml
port = 4000                    # 监听端口
host = "{{本机TailscaleIP}}"        # 绑定到 Tailscale IP（远程 Prometheus 可访问）
log_file = '{{用户目录}}/akswitch/akswitch.log'
log_max_size = 100
log_max_age = 7

[provider.xxx]
target = 'https://api.xxx.com/v1'
cooldown_sec = 15
max_retries = 2
http_timeout_sec = 60
```

> `host` 字段默认为 `127.0.0.1`。设为 Tailscale IP 后远程 Prometheus 才能抓取 metrics。本机通过 `127.0.0.1:4000` 仍然可访问。

## 组件详情

### 1. Prometheus（远程 WSL）

| 配置项 | 值 |
|--------|-----|
| 管理方式 | nssm Windows 服务（手动启动） |
| 服务名 | Prometheus |
| 启动 | `net start Prometheus` |
| 停止 | `net stop Prometheus` |
| 状态 | `sc query Prometheus`（注意：PAUSED 不一定表示挂了，见 nssm 已知问题） |
| 重启 | 不要用 `nssm restart`，用 `sc stop Prometheus && sc start Prometheus` |
| 配置 | `/opt/prometheus/prometheus.yml` |
| 数据 | `/opt/prometheus/data` |

**配置文件内容：**
```yaml
global:
  scrape_interval: 15s
scrape_configs:
  - job_name: akswitch
    static_configs:
      - targets: ['{{本机TailscaleIP}}:4000']
    metrics_path: /metrics
```

**目标检查：** `curl http://localhost:9090/api/v1/targets` → `health: "up"` 表示正常。

### 2. Grafana（远程 WSL）

| 配置项 | 值 |
|--------|-----|
| 版本 | 13.1.0（apt 安装） |
| 管理 | systemd（已禁用开机自启） |
| 访问 | `http://{{远程TailscaleIP}}:3000`（匿名，无需登录） |
| 数据源 | Prometheus（`http://localhost:9090`） |

**注意：必须先启动 Prometheus，再启动 Grafana。**

**⚠️ SSH 会话风险：** 通过 `ssh remote-laptop "wsl -d Ubuntu2204 -u root -- systemctl start grafana-server"` 启动 Grafana 后，SSH 连接断开时 Grafana 可能收到 SIGTERM 而退出。这是因为 SSH 的 WSL 会话结束导致 systemd 终止了 Grafana 进程。**建议启动后立即验证：**
```bash
# 验证 Grafana 是否存活
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- systemctl is-active grafana-server"
# 应输出 "active"
```

### 3. 端口转发（Portproxy）

Grafana 在 WSL2 内部运行，WSL2 的 IP 重启后会变化。端口转发脚本将远程 Windows 的 `0.0.0.0:3000` 转发到 WSL2 的 3000 端口。

**脚本：** `D:\scripts\wsl2-portproxy.ps1`

**触发时机：**
- 登录 Windows 时自动触发（任务计划程序 "WSL2 Port Forwarding"）
- WSL 重启后手动执行：`schtasks /run /tn "WSL2 Port Forwarding"`

### 4. nssm 服务管理器

**安装位置：** `D:\apps\nssm\nssm.exe`（本机和远程相同）

nssm 将 Prometheus（WSL 命令）包装为 Windows 服务。**akswitch 本身不建议注册为 nssm 服务**（二进制自监控热重启与 nssm 冲突）。

**⚠️ 已知问题：nssm 服务显示 PAUSED 但 WSL 进程正常**

`sc query Prometheus` 可能显示 `STATE: 7 PAUSED`，但这**不表示 Prometheus 挂了**。nssm 包装 WSL 进程时，Windows 服务状态无法正确反映 WSL 内进程的真实状态。这是 nssm 的限制，不影响功能。

**判断 Prometheus 是否真的在运行的方法：**
```bash
# 方法一：检查 WSL 内进程
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- ps aux | grep prometheus"

# 方法二：直接验证端口是否响应
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- curl -s -o /dev/null -w '%{http_code}' http://localhost:9090/-/ready"
# 应返回 200

# 方法三：检查抓取目标状态
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- curl -s http://localhost:9090/api/v1/targets | grep -o '\"health\":\"[^\"]*\"'"
# 应返回 "health":"up"
```

**⚠️ 已知问题：PAUSED 状态下 nssm restart 失效**

当服务处于 PAUSED 状态时，`nssm restart Prometheus` 会报错 `Unexpected status SERVICE_PAUSED`。此时应使用 `sc` 命令替代：
```bash
# 不要用 nssm restart，改用 sc
ssh remote-laptop "sc stop Prometheus && sc start Prometheus"
```

### 5. 本地 akswitch 热重载

```bash
go install ./cmd/akswitch/   # 编译安装新版本 → akswitch 自动检测并优雅重启
```

## 常见问题

### 浏览器打不开 `http://{{远程TailscaleIP}}:3000`

1. 检查 Grafana 是否在 WSL 中运行：`systemctl status grafana-server`
2. 检查端口转发规则：`netsh interface portproxy show all`
3. 检查防火墙规则：`netsh advfirewall firewall show rule name="WSL2 Port Forwarding (AKSwitch)"`
4. **检查系统代理设置** — 如果本机开启了 Karing 等代理软件（TUN 模式或 HTTP 代理），代理可能拦截了 Tailscale IP 流量，返回 502 错误。解决方法：
   - 验证方法：`curl -s -o /dev/null -w '%{http_code}' http://{{远程TailscaleIP}}:3000` 返回 200 但浏览器打不开，说明是代理问题
   - 在 Windows 代理例外列表（`Internet Options → Connections → LAN Settings → Advanced → Exceptions`）中添加 `100.*`（Tailscale 使用 `100.64.0.0/10` CGNAT 段）
   - 或在 Karing 的 TUN 配置中添加 `route_exclude_address: ["{{远程TailscaleIP}}/32"]`（已在 tailscale-ssh skill 中记录）

### Prometheus targets 显示 `down`

1. 确认 akswitch 在运行：`akswitch status`
2. 确认 akswitch 绑定 IP 正确：启动日志显示 `addr={{本机TailscaleIP}}:4000`
3. 从远程 WSL 直接测试：`curl -s http://{{本机TailscaleIP}}:4000/metrics`
4. 检查 Prometheus 日志：`/opt/prometheus/data/prometheus.log`

### 本机重启后

```bash
akswitch start
```

### WSL 重启后

```bash
net start Prometheus
systemctl start grafana-server
schtasks /run /tn "WSL2 Port Forwarding"

# 验证所有服务正常运行
ssh remote-laptop "sc query Prometheus | findstr STATE"              # 应显示 RUNNING 或 PAUSED（见 nssm 已知问题说明）
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- systemctl is-active grafana-server"  # 应输出 active
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- curl -s -o /dev/null -w '%{http_code}' http://localhost:9090/-/ready"  # 应返回 200
curl -s -o /dev/null -w '%{http_code}' http://{{远程TailscaleIP}}:3000                # 应返回 302
```

## 文件位置清单

| 文件 | 位置 |
|------|------|
| akswitch 二进制 | `$(go env GOPATH)/bin/akswitch.exe` |
| 项目配置 | `~/.akswitch/config.toml` |
| PID 文件 | `~/.akswitch/akswitch.pid` |
| 日志文件 | `{{用户目录}}/akswitch/akswitch.log` |
| 远程：端口转发脚本 | `D:\scripts\wsl2-portproxy.ps1` |
| 远程：Prometheus 配置 | `/opt/prometheus/prometheus.yml` |
| 远程：Prometheus 二进制 | `/opt/prometheus/prometheus` |
| 远程：nssm | `D:\apps\nssm\nssm.exe` |
| 远程：Grafana 配置 | `/etc/grafana/grafana.ini` |
| 远程：Grafana 数据源 | `/etc/grafana/provisioning/datasources/prometheus.yml` |
| 远程：Grafana 面板 JSON | `/etc/grafana/provisioning/dashboards/akswitch-overview.json` |