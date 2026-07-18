# Grafana (http://100.64.141.0:3000) 无法访问排查步骤

> 排查日期：2026-07-18
> 基于 remote-monitoring skill 架构知识

## 架构回顾

```
Browser → Tailscale → 100.64.141.0:3000
                           ↓
                  远程 Windows (100.64.141.0)
                  端口转发 (portproxy: 0.0.0.0:3000 → WSL:3000)
                           ↓
                  WSL2 Ubuntu 22.04
                  grafana-server (监听 WSL 内 3000 端口)
                           ↓
                  依赖: Prometheus (localhost:9090)
```

每个环节都可能出问题，按从外到内的顺序排查。

---

## 第 1 层：基础网络可达性

**问题：** 本机是否通过 Tailscale 能到达远程机器？

**排查命令：**
```bash
tailscale ping 100.64.141.0
```
或
```bash
ping -n 3 100.64.141.0
```

**预期结果：** 收到回复，延迟在几毫秒到几十毫秒（Tailscale 直连）或略高（中继）。

**如果失败：**
- 本机 Tailscale 是否在线？`tailscale status`
- 远程机器 Tailscale 是否在线？`ssh remote-laptop "tailscale status"`
- 双端是否登录同一个 Tailscale 网络？
- 远程机器是否关机/休眠？

---

## 第 2 层：端口级可达性

**问题：** 端口 3000 是否在远程 Windows 上监听并可达？

**排查命令：** 从本机测试端口是否开放
```bash
# 用 curl 测试
curl -s -o /dev/null -w "%{http_code}" http://100.64.141.0:3000

# 或用 telnet（如果可用）
ssh remote-laptop "curl -s -o /dev/null -w '%{http_code}' http://localhost:3000"
```

**预期结果：** 返回 200 或 302（Grafana 登录/首页重定向）。

**如果从本机访问失败但远程 localhost 成功：** 问题在 portproxy 或防火墙。
**如果从远程 localhost 也失败：** 问题在 WSL 内的 Grafana 服务。

---

## 第 3 层：端口转发规则（Portproxy）

**问题：** Windows 上的端口转发规则是否存在且指向正确的 WSL IP？

**排查命令：** 在远程机器上执行
```bash
ssh remote-laptop "netsh interface portproxy show all"
```

**预期结果：** 看到类似输出
```
监听地址: 0.0.0.0  监听端口: 3000  连接到地址: 172.x.x.x  连接到端口: 3000
```

**关注点：**
- 规则是否存在？
- 如果存在，`连接到地址` 是否与当前 WSL2 的 IP 一致？
- WSL2 重启后 IP 会变化，规则中的 IP 可能已过期

**验证 WSL2 当前 IP：**
```bash
ssh remote-laptop "wsl -d Ubuntu2204 -- ip addr show eth0 | grep inet"
```

**如果规则过期或不存在：** 需要重新运行端口转发脚本
```bash
ssh remote-laptop "schtasks /run /tn \"WSL2 Port Forwarding\""
```

---

## 第 4 层：Windows 防火墙

**问题：** 防火墙是否阻止了端口 3000 的入站流量？

**排查命令：** 在远程机器上执行
```bash
ssh remote-laptop "netsh advfirewall firewall show rule name=\"WSL2 Port Forwarding (AKSwitch)\" verbose"
```

**预期结果：** 看到规则存在，`Enabled: Yes`，`Action: Allow`，`LocalPort: 3000`

**如果规则不存在或被禁用：**
- 可以用 `netsh advfirewall firewall add rule ...` 重新创建
- 临时测试：`netsh advfirewall set allprofiles firewallpolicy allowinbound,allowoutbound`
- 注意：临时关闭防火墙后要恢复

---

## 第 5 层：WSL2 运行状态

**问题：** WSL2 虚拟机是否正常运行？

**排查命令：** 在远程机器上执行
```bash
ssh remote-laptop "wsl -l -v"
```

**预期结果：** 看到 `Ubuntu-22.04` 状态为 `Running`。

**如果状态为 Stopped：**
```bash
ssh remote-laptop "wsl -d Ubuntu2204 -- exit"
```

---

## 第 6 层：Grafana 服务状态

**问题：** grafana-server 是否在 WSL2 内部正常运行？

**排查命令：** 在远程机器上执行
```bash
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- systemctl status grafana-server"
```

**预期结果：** 看到 `Active: active (running)`，且无错误日志。

**如果服务未运行：**
```bash
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- systemctl start grafana-server"
```

**如果服务启动失败：** 查看详细日志
```bash
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- journalctl -u grafana-server --no-pager -n 50"
```

**常见失败原因：**
- Grafana 数据目录权限问题
- Grafana 配置文件语法错误
- 端口 3000 被占用（其他进程占用了 WSL 内的 3000 端口）
- Prometheus 未启动（Grafana 虽然不依赖 Prometheus 启动，但启动后可能报数据源不可用）

---

## 第 7 层：Grafana 监听地址

**问题：** Grafana 是否只监听了 localhost，导致 portproxy 转发后外部无法访问？（注意：portproxy 转发的是 Windows → WSL 的流量，WSL 内 Grafana 监听 localhost 实际上是可行的，但需要确认 Grafana 的 `server_addr` 配置）

**排查命令：** 在 WSL 内检查 Grafana 实际监听情况
```bash
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- ss -tlnp | grep 3000"
```

**预期结果：** 看到 `*:3000` 或 `0.0.0.0:3000` 或 `127.0.0.1:3000`。

**检查 Grafana 配置：**
```bash
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- cat /etc/grafana/grafana.ini | grep -A 2 '^;http_addr'"
```

**如果只监听 127.0.0.1：** 从 WSL 外部无法访问，需要修改 `http_addr` 为 `0.0.0.0` 或留空（默认监听所有接口）。

---

## 第 8 层：Prometheus 依赖（可选）

**问题：** Prometheus 是否在运行？（Grafana 可以独立启动，但数据源不可用可能影响面板展示）

**排查命令：** 在远程机器上执行
```bash
ssh remote-laptop "sc query Prometheus"
```

**预期结果：** `STATE: 4 RUNNING`

**如果 Prometheus 未运行：**
```bash
ssh remote-laptop "net start Prometheus"
```

---

## 排查汇总对照表

| 层 | 检查点 | 成功标志 | 失败常见原因 |
|----|--------|----------|-------------|
| 1 | Tailscale 连通性 | ping 成功 | 远程关机、Tailscale 未登录、中继不可达 |
| 2 | 端口可达性 | curl 返回 HTTP 状态码 | 后续层的问题 |
| 3 | Portproxy 规则 | 规则存在且 IP 匹配 | WSL 重启后 IP 变化、脚本未执行 |
| 4 | Windows 防火墙 | 规则存在且 Allow | 防火墙规则被删除或禁用 |
| 5 | WSL2 状态 | Running | WSL 崩溃、Windows 重启后未启动 |
| 6 | Grafana 服务 | active (running) | 服务未启动、配置错误、端口冲突 |
| 7 | Grafana 监听地址 | 0.0.0.0:3000 | 配置了 http_addr 为 127.0.0.1 |
| 8 | Prometheus | RUNNING | 服务未启动 |

---

## 快速诊断路径

如果想快速缩小范围，按以下顺序：

```bash
# 1. 远程机器是否活着？
tailscale ping 100.64.141.0

# 2. 远程机器上 3000 端口是否在监听？
ssh remote-laptop "curl -s -o /dev/null -w '%%{http_code}' http://localhost:3000"

# 3. 端口转发规则是否正常？
ssh remote-laptop "netsh interface portproxy show all"

# 4. Grafana 是否在 WSL 内运行？
ssh remote-laptop "wsl -d Ubuntu2204 -u root -- systemctl status grafana-server"
```

这四个命令能覆盖 80% 以上的故障场景。