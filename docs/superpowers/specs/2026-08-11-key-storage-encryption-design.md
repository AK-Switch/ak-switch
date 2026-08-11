# Key 存储架构重构设计

**Issue**: #283
**日期**: 2026-08-11
**状态**: 设计阶段

## 背景

当前 AK-Switch 的 Key 存储架构：

```
OS Keyring (主) → 加密文件 (fallback) → 明文 (最后 fallback)
```

已发现的问题：

- Windows Credential Manager 容量限制，批量导入 35 个 key 报错
- 每次 key 读写都是 IPC 调用，key pool 轮询场景下频繁读取影响吞吐量
- Docker 无 D-Bus 会话直接失败，macOS 无头环境不可用
- OS Keyring 为少量用户凭证设计，不是为消费型 API key pool 设计

## 目标

```
加密文件 (主) → OS Keyring (可选，保护加密密钥) → 明文 (fallback)
```

- API keys 本体存储在加密文件中（`keys/<provider>.enc`）
- OS Keyring 仅用于存储加密密钥（master key），而非每个 API key
- 明文文件仅作为无 keyring 环境下的 fallback

## 设计决策

### 为什么 AES-GCM 自研而非继续用 99designs/keyring

行业标准做法是数据库（SQLite/MySQL），但 AK-Switch 的定位是「单可执行文件，零外部依赖」，
SQLite 的 CGO 依赖与之冲突。AES-GCM 加密文件是贴近行业标准且符合项目定位的最优解。

`99designs/keyring` 本身不匹配批量 key pool 场景——它是为少量用户凭证设计的。
把 FileBackend 提升为主路径不能解决根本问题。

### 为什么不引入数据库

- 项目定位：单可执行文件
- 规模：每个 provider 数百个 key，AES-GCM 文件完全够用
- 不需要数据库的查询能力和并发优势

## 新存储格式

### 加密文件

```
<config_dir>/keys/<provider>.enc
  结构: [12-byte nonce][AES-GCM ciphertext + 16-byte tag]
  内容: JSON(KeyStore{Keys: [{Key, Name, Disabled}]})
```

- 每个 provider 一个 `.enc` 文件，整文件读写
- 无 keyring IPC，无条目大小限制
- JSON 结构复用现有 `KeyStore` / `KeyEntry`，零数据结构改动

### Master Key

| 优先级 | 来源 | 说明 |
|--------|------|------|
| 1 | OS Keyring (`akswitch:master-key`) | 首选 |
| 2 | 本地 `keys/master.key` (0600) | 无 keyring 环境兜底 |

- 首次启动生成 32-byte 随机 key
- 优先存 keyring，不可用时写本地文件
- `sync.Once` 保证单次初始化，内存驻留

## 存储优先级

### LoadKeys / LoadKeysFromStore

```
1. 加密文件 (.enc)              ← 新主路径
2. keyring 旧数据 (仅迁移用)     ← 检测到就走迁移
3. insecure 明文文件             ← 已有
4. legacy .enc 文件              ← 已有
```

调用方（`cli/key.go`、`server/router.go`、`server/server_launcher.go`）零改动。

### SaveKeys

```
1. SaveEncrypted (新主路径)
   └─ 失败且用户未显式 --insecure-storage → 返回错误
   └─ 失败但 --insecure-storage → SaveKeysInsecure
2. keyring (保留，后续 PR 评估移除)
```

### RemoveKeys

```
1. RemoveEncrypted
2. removeFromKeyring (旧数据兜底)
```

## 迁移策略

触发条件：LoadKeys 步骤 1 未命中（文件不存在），但步骤 2 命中（keyring 有旧数据）。

迁移动作：
```
旧 keyring JSON 数据 → SaveEncrypted → keyring 旧条目标记 .bak → 删除旧条目
```

一次性完成，后续启动走新路径。全程 best-effort，迁移失败不阻塞启动。

## API 不变

对外函数签名保持不变：

```go
SaveKeys(provider string, store *KeyStore) error
LoadKeys(provider string) (*KeyStore, error)
RemoveKeys(provider string) error
LoadKeysFromStore(name string, cfg *config.Config) (keys, names []string, loaded bool)
```

所有调用方零改动。

## 文件结构

### 新增文件

| 文件 | 职责 |
|------|------|
| `internal/keypool/encrypted_store.go` | AES-GCM 加密读写、master key 管理、迁移逻辑 |
| `internal/keypool/encrypted_store_test.go` | 单元测试 |

### 修改文件

| 文件 | 改动 |
|------|------|
| `internal/keypool/store.go` | SaveKeys 优先写加密文件；LoadKeys/LoadKeysFromStore 优先读加密文件 |
| `go.mod` | 新增 `golang.org/x/crypto` |

### 不动文件

`keypool.go`、`keyring.go`、`cli/key.go`、`server/` 下所有文件 — 零改动。

## 新增依赖

- `golang.org/x/crypto` — Argon2id 密钥派生
- `99designs/keyring` — 保留（用于 master key 的 OS keyring 交互），后续 PR 评估是否移除

## 错误处理

| 场景 | 处理 |
|------|------|
| encrypt 失败 | 向上冒泡，不 fallback 明文（除非 `--insecure-storage`） |
| master key 丢失 | 生成新 key，旧加密文件无法解密 → 走 keyring/insecure fallback |
| 迁移失败 | best-effort，不阻塞启动 |
| 文件权限不足 | 返回错误，用户手动修复 |

## 并发安全

- `masterKey` 包级变量，`sync.Once` 保证单次初始化
- 文件写操作：写临时文件 + `os.Rename` 原子替换
- `KeyPool` 已有锁保护，不涉及新增并发问题

## 测试策略

`encrypted_store_test.go`：
- `TestSaveEncrypted_ThenLoadEncrypted` — round-trip
- `TestLoadEncrypted_FileNotExist` — 返回 nil
- `TestSaveEncrypted_WrongKey` — 换 key 解密失败
- `TestGetMasterKey_GeneratesNew` — 无 keyring 时生成本地文件
- `TestGetMasterKey_ReadsFromKeyring` — 模拟 keyring 返回
- `TestMigrateFromKeyring` — keyring → 加密文件 → keyring 清空

`store_test.go` 现有测试：
- `TestLoadKeysFromStore_*` — 验证新优先级顺序
- `TestSaveKeys_*` — 验证 SaveKeys 走新路径
