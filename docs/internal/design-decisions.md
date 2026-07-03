# AK Switch 设计决策记录

## 不做的（已评估后排除）

| 方向 | 排除理由 |
|------|----------|
| CD / Release Pipeline | 纯自用，不需要自动发布 |
| 性能优化 | 50 QPS p99 15.8ms 已远超个人需求，瓶颈在上游 rate limit 不在 AK Switch |
| Key 选择策略 | 单 provider 内 Key 间无负载差异 |
| 外部密钥管理（Vault/KMS） | 本地 AES-256-GCM 加密已够安全 |
| 优雅降级（重试队列/半开） | HTTP 代理场景无法暂存请求；Key 切换成本极低，当前熔断器足够 |
| Dashboard 增强 | 已够用，无真实需求 |
| 请求/响应预处理 | ccswitch 已成熟，不重复造轮 |

## 已知约束

- **ccswitch 领域不碰** — 格式化/整流/转发、provider 路由、请求修改、响应变换等 ccswitch 已成熟的功能不重复造轮。详见 `.agents/documents/ccswitch-analysis.md`。
- **WSL2 9p 文件系统不支持 inotify** — 容器内热重载不会触发（不影响裸跑）
- **高并发性能瓶颈** — 100+ QPS 开始饱和，个人场景到不了这个量级