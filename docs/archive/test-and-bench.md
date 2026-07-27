# AK Switch 测试与基准数据

## 测试分布（265 tests，全部活跃）

| 文件 | 测试数 | 类型 |
|------|--------|------|
| `internal/config/config_test.go` | 51 | 单元测试 |
| `proxy_test.go` | 38 | **集成验收测试** |
| `handlers_test.go` | 26 | Handler 测试 |
| `internal/keypool/*_test.go` | 40 | 单元测试 |
| `internal/circuitbreaker/*_test.go` | 19 | 单元测试 |
| `internal/server/*_test.go` | 20 | 单元/集成测试 |
| `metrics_verification_test.go` | 6 | **集成验收测试** |
| `healthcheck_test.go` | 5 | **集成验收测试** |
| `start_cmd_test.go` | 4 | **集成验收测试** |
| `provider_cmd_test.go` | 5 | **集成验收测试** |
| `key_cmd_test.go` | 5 | **集成验收测试** |
| `e2e_test.go` | 1 | **集成验收测试** |
| `graceful_shutdown_test.go` | 3 | **集成验收测试** |
| `docker_compose_test.go` | 5 | **集成验收测试** |
| `config_cmd_test.go` | 3 | **集成验收测试** |
| `logstore_test.go` + `internal/logstore/*_test.go` | 9 | Handler + 单元 |
| `integration_test.go` | 0 | **已清空**（`.env` 测试随 `.env` 移除） |
| **总计** | **265** | |

## 压测基线

| 场景 | 结果 |
|------|------|
| 50 QPS 冒烟测试 | 100% 成功，p99 15.8ms |
| 500 QPS 全量压测 | ⚠️ 32% 成功，p99 34s（i5 笔记本饱和） |
| 200 QPS 中等负载 | ⚠️ 1.6% 成功（大量超时） |

## Docker 验证

- `docker compose config` ✅ 语法通过
- CI Docker build ✅ 已在 go.yml 中配置

## 熔断器验证

- KeyCircuitBreaker: 429 触发指数退避，401/403 永久禁用 ✅
- UpstreamCircuitBreaker: 5xx 触发熔断，304 恢复 ✅
- 上游错误不惩罚 Key（设计正确） ✅