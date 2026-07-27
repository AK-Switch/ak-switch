# ADR-003: 默认启用 OS Keyring 存储 API Key

**Status**: 提议
**Date**: 2026-07-17
**Author**: /ask-matt
**Related**: `internal/keypool/crypto.go`, `internal/keypool/store.go`, `internal/cmd/key.go`

---

## Context

akswitch 的 API Key 存储目前存在一个安全缺口：

- `akswitch key add` 将 key 写入 `~/.akswitch/keys/<provider>.enc`，**未加密时是明文 JSON**
- 加密需要用户手动设置 `KEYS_ENCRYPTION_KEY` 环境变量——绝大多数用户不会做
- 文件名后缀 `.enc`（encrypted 的缩写）对用户有误导性，里面存的可能是明文
- 市场上主流 CLI 工具（GitHub CLI、Docker CLI、aws-vault）均默认使用 OS 级密码管理器，而非自管加密文件

调研了 10 个主流工具的实践（GitHub CLI、AWS CLI、Docker CLI、gcloud、kubectl、SOPS、age、12-factor app、zalando/go-keyring、99designs/keyring），结论是：

| 层级 | 方案 | 代表工具 |
|------|------|---------|
| Tier 1 | OS 密码管理器（Keychain / WinCred / Secret Service） | Docker Desktop, GitHub CLI, aws-vault |
| Tier 2 | 加密文件 + 密钥托管 | 99designs/keyring 回退 |
| Tier 3 | 明文文件 + 权限保护 | AWS CLI（legacy，被 AWS 官方称为 legacy） |

akswitch 当前在**最底层（Tier 3）**，且没有向用户告知风险。

## Decision

迁移到三层分级架构，默认 Tier 1，无感降级 Tier 2，显式 opt-in Tier 3。

### 具体变更

**引入 `99designs/keyring` Go 库**（aws-vault 同款），替换当前的 `store.go` 读写层：

```
Tier 1（默认）: OS 密码管理器
  macOS  → Keychain
  Windows → Credential Manager (DPAPI)
  Linux   → Secret Service (GNOME/KDE)
  → 自动检测，用户无感，不存额外的密钥文件

Tier 2（回退）: 加密文件
  当系统 keyring 不可用时（如 headless Linux 服务器、CI 环境）：
  → 自动降级到 AES-256-GCM 加密文件
  → 加密密钥由 99designs/keyring 的内部机制自动管理
  → 保留现有 crypto.go 的加解密实现作为底层

Tier 3（显式 opt-in）: 明文文件
  → 新增 --insecure-storage 标志
  → 使用时打印警告："WARNING: API keys will be stored in plaintext"
  → 仅用于 CI/自动化等无法使用 keyring 的场景
```

**移除 `KEYS_ENCRYPTION_KEY` 环境变量**——不再需要用户手动配置加密密钥。

### 不发生变化的

- `akswitch key add/list/remove/disable/enable` 的 CLI 接口不变
- 运行时的 `triggerReload()` → `/api/reload` 热重载链路不变
- 现有的 `crypto.go`（AES-256-GCM）保留，作为加密文件回退的底层实现

### 用户体验变化

```bash
# 之前（手动加密）
KEYS_ENCRYPTION_KEY="xxx" akswitch key add nvidia sk-xxx

# 之后（默认加密——无感）
akswitch key add nvidia sk-xxx
# ✓ 自动存入 Keychain/WinCred/Secret Service
# ✓ 无需任何环境变量

# 明文逃生口
akswitch key add nvidia sk-xxx --insecure-storage
# ⚠ WARNING: API keys will be stored in plaintext
```

## Consequences

### 正面

- 默认安全：新用户首次使用即自动加密，无需配置
- 消除 `KEYS_ENCRYPTION_KEY` 这个泄漏面（环境变量可能被不小心暴露）
- 消除 `.enc` 文件名误导（不再出现明文存 .enc 的情况）
- 对齐行业标准实践
- 兼容 headless 环境（自动降级加密文件）

### 负面

- 新增 Go 依赖：`99designs/keyring`
- Linux 无桌面环境时需要额外密文回退路径（但已有现成方案）
- 已用 `KEYS_ENCRYPTION_KEY` 加密的存量 key 迁移需要过渡方案

### 风险

- `99designs/keyring` 在 Linux 上依赖 D-Bus Secret Service，部分 WSL 环境可能不可用——回退到加密文件即可
- 用户可能依赖 `KEYS_ENCRYPTION_KEY` 环境变量作为"密码"来控制 key 可访问性——移除后需要沟通

## Alternatives Considered

### 保留现状，但强制默认开启加密

- 需要用户自己保存一个 master key，丢了所有 key 全废
- 密钥管理责任转嫁给了用户，不符合"安全默认值"原则

### 学微信，不落盘，从在线服务派生密钥

- 不适合 akswitch 的场景：我们没有在线身份认证系统
- 微信的密钥来自登录态，akswitch 的 key 是用户手动输入的，必须存储

### 使用 `zalando/go-keyring`（更轻量的库）

- 优点是 API 更简单，无 CGO 依赖
- 缺点是没有加密文件回退，headless 环境直接降级到不存在
- 99designs/keyring 的加密文件回退是 aws-vault 验证过的成熟方案

## Related

- Issue #43: feat: akswitch key import 批量导入
- internal/keypool/crypto.go — AES-256-GCM 实现
- internal/keypool/store.go — 当前 JSON 文件存储层
- internal/cmd/reload.go — triggerReload 热重载