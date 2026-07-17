# 熔断器架构

AK Switch 采用**两层熔断器架构**，分别处理 Key 级和上游级故障。

## 架构概览

```text
请求进入
  │
  ▼
┌─────────────────────────────────────┐
│ UpstreamCircuitBreaker              │  ← 502/503/网络错误影响此层
│  CLOSED → OPEN → HALF_OPEN → CLOSED │
└────────────┬────────────────────────┘
             │ 允许请求
             ▼
┌─────────────────────────────────────┐
│ KeyPool (轮询选择可用 Key)          │  ← 跳过冷却/禁用中的 Key
└────────────┬────────────────────────┘
             │ 选中一个 Key
             ▼
┌─────────────────────────────────────┐
│ KeyCircuitBreaker (每个 Key 一个)    │  ← 429 影响此层
│  CLOSED → OPEN → PERMA              │
└────────────┬────────────────────────┘
             │ 通过
             ▼
        上游请求
```

## KeyCircuitBreaker（Key 级）

每个 API Key 对应一个独立熔断器。只跟踪 429（限流）和 401/403（鉴权）响应。

### 状态转移

```
               429 触发
CLOSED ──────────────────→ OPEN (退避冷却)
  │                          │
  │ 请求成功 (2xx/3xx)       │ 冷却时间耗尽
  │ ←────────────────────────┘
  │                           │
  │                           │ 退避达到 BACKOFF_CAP_SEC
  │                           │ 或收到 401/403
  │                           ▼
  │                        PERMA (永久禁用)
  │                        不再恢复
  └─────────────────────────────────────────
```

| 状态 | 行为 |
|------|------|
| **CLOSED** | Key 可用，直接通过 |
| **OPEN** | Key 进入指数退避冷却，公式 `base × multiplier^attempt + jitter`（jitter = 0~50% 随机）。冷却期内 Key 不会被选中 |
| **PERMA** | 永久禁用，不再自动恢复。退避值达到 `BACKOFF_CAP_SEC` 时触发（判定日额度耗尽），401/403 直接进入 |

Key 冷却/禁用后，请求自动换下一个可用 Key。所有 Key 都不可用时返回 503。

## UpstreamCircuitBreaker（上游级）

跟踪上游服务的 502/503 和网络错误。**为个熔断器不惩罚 Key** — 上游问题不影响 Key 状态。

### 状态转移

```
                       连续达阈值
CLOSED ──────────────────────────────→ OPEN
  │                                      │
  │ 请求成功                              │ CB_RESET_SEC 超时
  │ ←────────────────────────────────────┘
  │                                      │
  │                                      ▼
  │                                   HALF_OPEN
  │                                      │
  │                              ┌───────┴───────┐
  │                              │               │
  │                          探测成功          探测失败
  │                              │               │
  │                              ▼               ▼
  │                           CLOSED           OPEN
  └────────────────────────────────────────────────────
```

| 状态 | 行为 |
|------|------|
| **CLOSED** | 正常，请求直通 |
| **OPEN** | 请求直接跳过上游，不发送。等待 `CB_RESET_SEC` 后进入 HALF_OPEN |
| **HALF_OPEN** | 允许单次探测请求。成功→CLOSED，失败→OPEN（再次等待 `CB_RESET_SEC`） |

## HTTP 状态码响应矩阵

| 状态码 | Key 处理 | 上游熔断器 | 重试行为 |
|--------|---------|-----------|---------|
| 429 | 指数退避 + 可能 PERMA | 不记录 | 重试下一个 Key |
| 401/403 | 直接 PERMA + Disable | 不记录 | 重试下一个 Key |
| 502/503 | 不惩罚 | 记录失败 | 重试同一或其他 Key |
| 网络错误 | 不惩罚 | 记录失败 | 重试 |
| 其他 4xx | 不惩罚 | 不记录 | 直接返回，不重试 |
| 2xx/3xx | 重置 CLOSED | 记录成功 | 返回响应 |

## 全冷却时的行为

当所有 Key 都处于冷却状态时：

1. `TimeUntilAvailable()` 返回最短等待时长
2. 等待时长为 `最短冷却时间 + 随机 jitter (0~500ms)`
3. 避免多个请求同时恢复引起的 thundering herd


## ProviderRouter 架构

AK Switch 从 v0.3 开始采用**单端口多 Provider** 架构，通过路径路由区分不同 provider。

### 请求路由

```
请求进入 → POST /{provider}/v1/chat/completions
  │
  ├─ ProviderRouter.extractProvider(path)
  │   ├─ 解析 "/nvidia/v1/chat/completions" → provider="nvidia", path="/v1/chat/completions"
  │   └─ 查找 ProviderState
  │
  ├─ 管理 API 路径（/health, /api/*, /logs, /dashboard, /metrics）
  │   └─ 直接由 ProviderRouter 处理，不路由到具体 provider
  │
  └─ 代理请求
      └─ executeProxy → Key 选择 → 上游转发
```

### 管理 API 路由

| 方法 | 路径 | 处理函数 |
|------|------|---------|
| GET | /health | 聚合健康检查（所有 provider） |
| GET | /api/config | 配置查看（?provider= 可选） |
| GET/POST/DELETE | /api/keys | Key 管理（?provider= 可选） |
| POST | /api/keys/{index}/disable | 禁用 Key |
| POST | /api/keys/{index}/enable | 启用 Key |
| PUT | /api/keys/{index}/cooldown | 冷却 Key |
| DELETE | /api/keys/{index} | 删除 Key |
| GET | /api/stats | 请求统计 |
| POST | /api/reload | 热重载配置 |
| POST | /api/log-level | 动态日志级别 |
| GET | /logs | 请求日志 |
| GET | /metrics | Prometheus 指标 |
| GET | /dashboard | Web Dashboard |

## 请求生命周期

一个请求从接收到响应的完整流程：

1. **接收请求** — HTTP 服务接收请求，提取 provider 名称和路径
2. **读取请求体** — 限制最大 10MB，读取 body 用于重试
3. **上游熔断器检查** — `upCB.Allow()` 检查上游是否熔断
4. **Key 选择** — `pool.Next()` 按 RPM 感知算法选择最优 Key
5. **Key 熔断器检查** — 每个 Key 的独立熔断器检查是否在冷却中
6. **构建上游请求** — 设置 Authorization header，转发 headers
7. **执行请求** — `client.Do(req)` 发送到上游
8. **响应处理** — 按状态码分流：
   - 2xx/3xx → 成功，记录 Token 用量
   - 429 → 指数退避冷却，换 Key 重试
   - 401/403 → 永久禁用，换 Key 重试
   - 502/503 → 上游熔断器记录失败，重试
   - 其他 4xx → 直接返回，不重试
9. **日志记录** — 记录请求结果到环形缓冲区
10. **指标记录** — 更新 Prometheus 计数器

## RPM 感知 Key 选择算法

AK Switch 的 Key 选择策略是**RPM 感知 + Round-Robin** 混合算法：

1. 从 Round-Robin 起始位置开始，收集所有当前可用（未熔断、未禁用、未冷却）且未被其他 goroutine 占用的 Key
2. 如果只有一个候选，直接返回
3. 如果有多个候选，选择过去 60 秒内请求数（RPM）最低的 Key

```text
候选 Key 集合:
  Key[0]: RPM=5, 可用
  Key[1]: RPM=12, 可用
  Key[2]: RPM=3, 可用 → 选中（RPM 最低）

平均 RPM 低于 5 时 = round-robin 行为等效
突发流量时 = 自动偏向负载最低的 Key
```

这种设计在低负载时表现如 round-robin（RPM 计数接近），在高负载时自动均衡到最空闲的 Key。

## Token 用量追踪与 Calibrator 校准

### Token 追踪

非流式请求：从响应 body 的 `usage` 字段提取 `input_tokens` 和 `output_tokens`。
流式请求：使用 tiktoken-go 库估算，通过 `cl100k_base` 编码器对输出文本进行编码。

### Calibrator 校准

Calibrator 解决 tiktoken 估算值与上游实际计费值之间的偏差：

1. **非流式请求**作为校准样本：收集估算值 vs 实际值
2. **滑动窗口**跟踪最近 N 个样本的估算/实际比例
3. **中位数比例**作为校准因子，应用于流式请求的估算值
4. 校准比例随样本积累自动收敛

```text
样本收集:
  请求 1: 估算=150, 实际=100 → 比例=1.5
  请求 2: 估算=200, 实际=180 → 比例=1.11
  请求 3: 估算=90,  实际=80  → 比例=1.125
  ...
  中位数比例 → 校准因子

流式请求应用:
  估算值 × 校准因子 → 校准后的估算值
```
