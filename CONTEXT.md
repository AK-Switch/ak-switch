# AK Switch

API Key 轮转代理。位于客户端与上游 AI 提供商之间，将请求负载均衡到多个 API Key 上，避免单一 Key 被打满导致 429 限速。

## Language

### 核心概念

**AK Switch**:
API Key 轮转代理。在客户端和上游 AI 提供商之间做反向代理，将请求负荷平均分配到 Provider 内的多个 API Key 上。
_Avoid_: API Key proxy, gateway, router, 反向代理

**CC Switch**:
互补工具。CC Switch 做 Provider 级别的路由和故障转移，AK Switch 做 Provider 内部的 Key 级别轮转。两者配合使用。
_Avoid_: 替代品, 竞争品

**Provider**:
上游 AI API 服务（如 NVIDIA、SenseNova）。每个 Provider 有自己的 base URL 和一组 API Key。
_Avoid_: upstream, service, backend, 上游

**Key**:
上游 AI API 的身份凭证。一个 Provider 下可以配置多个 Key。
_Avoid_: token, secret, credential, 凭证, 令牌

**Key Rotation**:
从 Key Pool 中选出下一个可用 Key 的机制。当前 Key 被限速、鉴权失败或上游不健康时，自动轮转到下一个可用 Key。
_Avoid_: switching, failover, 切换

**Key Pool**:
Key 的集合，通过 round-robin 方式做负载均衡分配，确保每个 Key 被充分利用。
_Avoid_: key list, key set, key group, 列表

### 熔断与保护

**Per-Key 熔断 (Key Circuit Breaker)**:
跟踪单个 Key 的失败状态。429 时进入指数退避冷却，401/403 时永久禁用，冷却次数达到上限后也转为永久禁用。
_Avoid_: key level circuit breaker, key guard

**Upstream 熔断 (Upstream Circuit Breaker)**:
跟踪上游服务的健康状态。502/503 或网络错误达到阈值时熔断，探活请求成功后恢复。独立于 Key 的惩罚逻辑。
_Avoid_: upstream guard, service breaker

**冷却 (Cooldown)**:
Key 被 429 限速后进入的临时不可用状态，使用指数退避（含随机抖动）决定冷却时长。
_Avoid_: penalize, punished, sleep, 惩罚

**永久禁用 (Permanent Disable)**:
Key 因 401/403 鉴权失败，或冷却次数超过上限后，被永久标记为不可用。
_Avoid_: hard disable, banned, 封禁

### 运行时

**热加载 (Hot Reload)**:
不重启进程即可重新加载配置和 Key 的能力，通过 HTTP API 触发。
_Avoid_: hot restart, live reload, 热重启

**自重启 (Self-Restart)**:
Windows 上通过后台 goroutine 监控二进制文件变更，检测到重新编译后自动启动新进程。
_Avoid_: hot restart, auto-restart, 自动重启