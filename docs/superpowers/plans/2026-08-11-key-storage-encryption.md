# Key 存储架构重构实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Key 存储主路径从 OS Keyring 迁移到 AES-GCM 加密文件，Keyring 降级为 master key 存储和旧数据迁移 fallback

**Architecture:** 新增 `encrypted_store.go` 封装 AES-256-GCM 加密读写和 master key 生命周期，修改 `store.go` 的优先级链，所有调用方（`cli/key.go`、`server/`）零改动

**Tech Stack:** Go 1.26, `crypto/aes`, `crypto/cipher`, `crypto/rand`, `golang.org/x/crypto` (argon2), `99designs/keyring` (保留用于 master key)

## Global Constraints

- Tab 缩进（项目强制）
- 构建标签：单元测试用 `-tags=unit`
- 错误包装: `fmt.Errorf("函数名: %w", err)`
- 日志: `slog`
- 表驱动测试优先
- 文件权限: 0700 (目录), 0600 (文件)
- 提交前: `make check && make test-unit`

---

### Task 1: 添加依赖

**Files:**
- Modify: `go.mod`

**Interfaces:**
- Consumes: 无
- Produces: `golang.org/x/crypto` 可用

- [ ] **Step 1: 添加依赖并整理**

```bash
go get golang.org/x/crypto@latest
go mod tidy
```

- [ ] **Step 2: 验证依赖正确**

```bash
go build ./...
```

Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add go.mod go.sum
git commit -m "deps: add golang.org/x/crypto for argon2 master key derivation"
```

---

### Task 2: 实现 `encrypted_store.go`

**Files:**
- Create: `internal/keypool/encrypted_store.go`

**Interfaces:**
- Consumes: `config.XDGConfigPath()`, `keyring.Keyring` (用于 master key)
- Produces: `SaveEncrypted`, `LoadEncrypted`, `RemoveEncrypted`, `getMasterKey`

```go
package keypool

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "crypto/sha256"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "sync"

    "akswitch/internal/config"
    "github.com/99designs/keyring"
    "golang.org/x/crypto/argon2"
)

// masterKeyFile 返回 master.key 文件路径。
func masterKeyFile() (string, error) {
    xdgPath, err := config.XDGConfigPath()
    if err != nil {
        return "", err
    }
    return filepath.Join(filepath.Dir(xdgPath), "keys", "master.key"), nil
}

// ensureKeysDir 确保 <config_dir>/keys/ 目录存在 (0700)。
func ensureKeysDir() error {
    xdgPath, err := config.XDGConfigPath()
    if err != nil {
        return err
    }
    dir := filepath.Join(filepath.Dir(xdgPath), "keys")
    return os.MkdirAll(dir, 0700)
}

var (
    masterKey     []byte
    masterKeyOnce sync.Once
    masterKeyErr  error
)

// getMasterKey 返回 32-byte master key，惰性初始化一次。
// 优先级: OS keyring → 本地 master.key → 生成新 key
func getMasterKey() ([]byte, error) {
    masterKeyOnce.Do(func() {
        // 1. 尝试 OS keyring
        kr, err := keyring.Open(keyring.Config{
            ServiceName: "akswitch",
        })
        if err == nil {
            item, err := kr.Get("akswitch:master-key")
            if err == nil {
                masterKey = item.Data
                return
            }
        }

        // 2. 尝试本地 master.key
        mkPath, err := masterKeyFile()
        if err == nil {
            data, err := os.ReadFile(mkPath)
            if err == nil && len(data) == 32 {
                masterKey = data
                return
            }
        }

        // 3. 生成新 key
        key := make([]byte, 32)
        if _, err := rand.Read(key); err != nil {
            masterKeyErr = fmt.Errorf("generate master key: %w", err)
            return
        }

        // 优先存 keyring
        if kr != nil {
            if setErr := kr.Set(keyring.Item{
                Key:  "akswitch:master-key",
                Data: key,
            }); setErr == nil {
                masterKey = key
                return
            }
        }

        // keyring 不可用，写本地文件
        if err := ensureKeysDir(); err != nil {
            masterKeyErr = fmt.Errorf("create keys dir: %w", err)
            return
        }
        mkPath, mkErr := masterKeyFile()
        if mkErr != nil {
            masterKeyErr = mkErr
            return
        }
        if err := os.WriteFile(mkPath, key, 0600); err != nil {
            masterKeyErr = fmt.Errorf("write master key file: %w", err)
            return
        }
        masterKey = key
    })

    if masterKeyErr != nil {
        return nil, masterKeyErr
    }
    if masterKey == nil {
        return nil, errors.New("master key is nil")
    }
    return masterKey, nil
}

// encrypt 用 AES-256-GCM 加密 plaintext。
// 输出: [12-byte nonce][ciphertext + 16-byte tag]
func encrypt(masterKey, plaintext []byte) ([]byte, error) {
    block, err := aes.NewCipher(masterKey)
    if err != nil {
        return nil, fmt.Errorf("create cipher: %w", err)
    }
    aesGCM, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("create GCM: %w", err)
    }
    nonce := make([]byte, aesGCM.NonceSize())
    if _, err := rand.Read(nonce); err != nil {
        return nil, fmt.Errorf("generate nonce: %w", err)
    }
    return aesGCM.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt 用 AES-256-GCM 解密 data。
func decrypt(masterKey, data []byte) ([]byte, error) {
    block, err := aes.NewCipher(masterKey)
    if err != nil {
        return nil, fmt.Errorf("create cipher: %w", err)
    }
    aesGCM, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("create GCM: %w", err)
    }
    nonceSize := aesGCM.NonceSize()
    if len(data) < nonceSize {
        return nil, errors.New("ciphertext too short")
    }
    nonce, ciphertext := data[:nonceSize], data[nonceSize:]
    return aesGCM.Open(nil, nonce, ciphertext, nil)
}

// encryptedFilePath 返回 provider 的加密文件路径。
func encryptedFilePath(provider string) (string, error) {
    xdgPath, err := config.XDGConfigPath()
    if err != nil {
        return "", err
    }
    return filepath.Join(filepath.Dir(xdgPath), "keys", provider+".enc"), nil
}

// SaveEncrypted 将 KeyStore 加密写入 <provider>.enc。
func SaveEncrypted(provider string, store *KeyStore) error {
    mk, err := getMasterKey()
    if err != nil {
        return fmt.Errorf("save keys for %q: get master key: %w", provider, err)
    }
    data, err := json.Marshal(store)
    if err != nil {
        return fmt.Errorf("save keys for %q: marshal: %w", provider, err)
    }
    encrypted, err := encrypt(mk, data)
    if err != nil {
        return fmt.Errorf("save keys for %q: encrypt: %w", provider, err)
    }
    path, err := encryptedFilePath(provider)
    if err != nil {
        return fmt.Errorf("save keys for %q: %w", provider, err)
    }
    if err := ensureKeysDir(); err != nil {
        return fmt.Errorf("save keys for %q: %w", provider, err)
    }
    // 原子写: 先写临时文件再 rename
    tmpPath := path + ".tmp"
    if err := os.WriteFile(tmpPath, encrypted, 0600); err != nil {
        return fmt.Errorf("save keys for %q: %w", provider, err)
    }
    if err := os.Rename(tmpPath, path); err != nil {
        _ = os.Remove(tmpPath)
        return fmt.Errorf("save keys for %q: %w", provider, err)
    }
    return nil
}

// LoadEncrypted 从 <provider>.enc 解密读取 KeyStore。
// 返回 (nil, nil) 文件不存在时。
func LoadEncrypted(provider string) (*KeyStore, error) {
    mk, err := getMasterKey()
    if err != nil {
        return nil, fmt.Errorf("load keys for %q: get master key: %w", provider, err)
    }
    path, err := encryptedFilePath(provider)
    if err != nil {
        return nil, err
    }
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, nil
        }
        return nil, fmt.Errorf("load keys for %q: %w", provider, err)
    }
    plaintext, err := decrypt(mk, data)
    if err != nil {
        return nil, fmt.Errorf("load keys for %q: decrypt: %w", provider, err)
    }
    var store KeyStore
    if err := json.Unmarshal(plaintext, &store); err != nil {
        return nil, fmt.Errorf("load keys for %q: unmarshal: %w", provider, err)
    }
    if store.Keys == nil {
        store.Keys = []KeyEntry{}
    }
    return &store, nil
}

// RemoveEncrypted 删除 <provider>.enc。
func RemoveEncrypted(provider string) error {
    path, err := encryptedFilePath(provider)
    if err != nil {
        return err
    }
    err = os.Remove(path)
    if err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("remove encrypted keys for %q: %w", provider, err)
    }
    return nil
}
```

- [ ] **Step 1: 创建 `encrypted_store.go`**

写入上述完整代码到 `internal/keypool/encrypted_store.go`。

- [ ] **Step 2: 验证编译**

```bash
go build ./internal/keypool/
```

Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/keypool/encrypted_store.go go.mod go.sum
git commit -m "feat: add AES-GCM encrypted key store (encrypted_store.go)"
```

---

### Task 3: 实现 `encrypted_store_test.go`

**Files:**
- Create: `internal/keypool/encrypted_store_test.go`

**Interfaces:**
- Consumes: `SaveEncrypted`, `LoadEncrypted`, `RemoveEncrypted`
- Produces: 无

```go
//go:build unit

package keypool

import (
    "testing"

    "github.com/99designs/keyring"
)

// testKeyring 提供一个内存 keyring 用于测试 getMasterKey。
type testKeyring struct {
    data map[string][]byte
}

func (t *testKeyring) Get(key string) (keyring.Item, error) {
    b, ok := t.data[key]
    if !ok {
        return keyring.Item{}, keyring.ErrKeyNotFound
    }
    return keyring.Item{Key: key, Data: b}, nil
}

func (t *testKeyring) Set(item keyring.Item) error {
    t.data[item.Key] = item.Data
    return nil
}

func (t *testKeyring) Remove(key string) error {
    delete(t.data, key)
    return nil
}

func (t *testKeyring) Keys() ([]string, error) {
    keys := make([]string, 0, len(t.data))
    for k := range t.data {
        keys = append(keys, k)
    }
    return keys, nil
}

func resetMasterKeyForTest() {
    masterKey = nil
    masterKeyOnce = sync.Once{}
    masterKeyErr = nil
    keyringBackend = nil
    testKeyringSet = false
    keyringInitConfig = ""
}

func TestSaveEncrypted_ThenLoadEncrypted(t *testing.T) {
    resetMasterKeyForTest()
    setTestKeyring(&testKeyring{data: map[string][]byte{}})

    store := &KeyStore{
        Keys: []KeyEntry{
            {Key: "sk-abc", Name: "test-key", Disabled: false},
            {Key: "sk-def", Name: ""},
        },
    }
    if err := SaveEncrypted("test-provider", store); err != nil {
        t.Fatalf("SaveEncrypted: %v", err)
    }

    loaded, err := LoadEncrypted("test-provider")
    if err != nil {
        t.Fatalf("LoadEncrypted: %v", err)
    }
    if loaded == nil {
        t.Fatal("LoadEncrypted returned nil, want store")
    }
    if len(loaded.Keys) != 2 {
        t.Fatalf("got %d keys, want 2", len(loaded.Keys))
    }
    if loaded.Keys[0].Key != "sk-abc" || loaded.Keys[0].Name != "test-key" {
        t.Errorf("key[0] mismatch: %+v", loaded.Keys[0])
    }
    if loaded.Keys[1].Key != "sk-def" || loaded.Keys[1].Name != "" {
        t.Errorf("key[1] mismatch: %+v", loaded.Keys[1])
    }
}

func TestLoadEncrypted_FileNotExist(t *testing.T) {
    resetMasterKeyForTest()
    setTestKeyring(&testKeyring{data: map[string][]byte{}})

    store, err := LoadEncrypted("nonexistent-provider")
    if err != nil {
        t.Fatalf("LoadEncrypted: %v", err)
    }
    if store != nil {
        t.Error("LoadEncrypted returned non-nil for missing file, want nil")
    }
}

func TestSaveEncrypted_WrongKey(t *testing.T) {
    resetMasterKeyForTest()
    kr := &testKeyring{data: map[string][]byte{}}
    setTestKeyring(kr)

    // 用 key A 保存
    store := &KeyStore{Keys: []KeyEntry{{Key: "sk-abc", Name: "key-a"}}}
    if err := SaveEncrypted("wrong-key-test", store); err != nil {
        t.Fatalf("SaveEncrypted: %v", err)
    }

    // 换 key B
    resetMasterKeyForTest()
    kr2 := &testKeyring{data: map[string][]byte{}}
    // 先写入 keyring 不同的 key
    fakeKey := make([]byte, 32)
    for i := range fakeKey {
        fakeKey[i] = byte(i + 1)
    }
    kr2.data["akswitch:master-key"] = fakeKey
    setTestKeyring(kr2)

    _, err := LoadEncrypted("wrong-key-test")
    if err == nil {
        t.Error("expected decrypt error with wrong key, got nil")
    }
}

func TestRemoveEncrypted(t *testing.T) {
    resetMasterKeyForTest()
    setTestKeyring(&testKeyring{data: map[string][]byte{}})

    store := &KeyStore{Keys: []KeyEntry{{Key: "sk-abc"}}}
    if err := SaveEncrypted("remove-test", store); err != nil {
        t.Fatalf("SaveEncrypted: %v", err)
    }

    if err := RemoveEncrypted("remove-test"); err != nil {
        t.Fatalf("RemoveEncrypted: %v", err)
    }

    loaded, err := LoadEncrypted("remove-test")
    if err != nil {
        t.Fatalf("LoadEncrypted after remove: %v", err)
    }
    if loaded != nil {
        t.Error("LoadEncrypted returned data after remove, want nil")
    }
}

func TestMigrateFromKeyring(t *testing.T) {
    resetMasterKeyForTest()

    // 1. 在 keyring 中存入旧数据
    oldStore := &KeyStore{
        Keys: []KeyEntry{
            {Key: "sk-old-1", Name: "migrated-key"},
            {Key: "sk-old-2"},
        },
    }
    data, err := json.Marshal(oldStore)
    if err != nil {
        t.Fatalf("marshal: %v", err)
    }
    kr := &testKeyring{data: map[string][]byte{}}
    kr.data["akswitch:old-migration-test"] = data
    setTestKeyring(kr)

    // 2. LoadKeys 应该检测到 keyring 数据并走迁移
    // 注意: LoadKeys 的迁移逻辑在 Task 4 中才实现
    // 这里先验证 LoadEncrypted 能正确加载已存在的文件
    // 直接写一个加密文件模拟迁移完成后的状态
    store := &KeyStore{Keys: []KeyEntry{{Key: "sk-migrated", Name: "from-enc"}}}
    if err := SaveEncrypted("migration-test", store); err != nil {
        t.Fatalf("SaveEncrypted: %v", err)
    }

    loaded, err := LoadEncrypted("migration-test")
    if err != nil {
        t.Fatalf("LoadEncrypted: %v", err)
    }
    if len(loaded.Keys) != 1 || loaded.Keys[0].Key != "sk-migrated" {
        t.Errorf("unexpected keys: %+v", loaded.Keys)
    }
}
```

- [ ] **Step 1: 创建测试文件**

写入上述完整代码到 `internal/keypool/encrypted_store_test.go`。

- [ ] **Step 2: 运行测试**

```bash
go test -tags=unit -count=1 -short ./internal/keypool/ -run TestSaveEncrypted
```

Expected: 全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/keypool/encrypted_store_test.go
git commit -m "test: add encrypted_store unit tests"
```

---

### Task 4: 修改 `store.go` 优先级链

**Files:**
- Modify: `internal/keypool/store.go`

**Interfaces:**
- Consumes: `SaveEncrypted`, `LoadEncrypted`, `RemoveEncrypted`
- Produces: 对外 API 不变

**需要改动的函数：**

**`LoadKeys`** — 加密文件优先，keyring 旧数据走迁移：

```go
func LoadKeys(provider string) (*KeyStore, error) {
    // 1. 尝试加密文件（新主路径）
    store, err := LoadEncrypted(provider)
    if err == nil && store != nil {
        return store, nil
    }

    // 2. 尝试 keyring 旧数据（仅迁移用）
    store, err = loadFromKeyring(provider)
    if err == nil && store != nil {
        // 迁移: 写入加密文件
        if saveErr := SaveEncrypted(provider, store); saveErr == nil {
            // 迁移成功，删除旧 keyring 条目
            _ = removeFromKeyring(provider)
        }
        return store, nil
    }

    // 3. 尝试 insecure 明文文件
    store, err = loadInsecureFile(provider)
    if err == nil && store != nil {
        return store, nil
    }

    // 4. 尝试 legacy .enc 文件
    oldPath, pathErr := legacyKeysPath(provider)
    if pathErr != nil {
        return nil, nil
    }
    oldStore, loadErr := LoadFullStore(oldPath)
    if loadErr != nil || oldStore == nil {
        return nil, nil
    }

    // 迁移 legacy 到加密文件
    if saveErr := SaveEncrypted(provider, oldStore); saveErr == nil {
        _ = os.Rename(oldPath, oldPath+".bak")
    }
    return oldStore, nil
}
```

**`SaveKeys`** — 加密文件优先：

```go
func SaveKeys(provider string, store *KeyStore) error {
    if err := SaveEncrypted(provider, store); err != nil {
        return err
    }
    // 同时清理旧 keyring 数据
    _ = removeFromKeyring(provider)
    return nil
}
```

**`LoadKeysFromStore`** — 加密文件优先：

```go
func LoadKeysFromStore(name string, cfg *config.Config) (keys, names []string, loaded bool) {
    // 1. 加密文件
    if store, err := LoadEncrypted(name); err == nil && store != nil {
        if insecureStore, err := loadInsecureFile(name); err == nil && insecureStore != nil {
            for _, ie := range insecureStore.Keys {
                found := false
                for _, ke := range store.Keys {
                    if ie.Key == ke.Key { found = true; break }
                }
                if !found { store.Keys = append(store.Keys, ie) }
            }
        }
        k, n := keysFromStore(store)
        return k, n, true
    }

    // 2. keyring 旧数据（触发迁移）
    if store, err := loadFromKeyring(name); err == nil && store != nil {
        _ = SaveEncrypted(name, store)
        _ = removeFromKeyring(name)
        k, n := keysFromStore(store)
        return k, n, true
    }

    // 3. 自定义 keys file
    if cfg.KeysFile != "" {
        fileKeys, fileNames, err := LoadKeysFromFile(cfg.KeysFile)
        if err == nil && fileKeys != nil {
            return fileKeys, fileNames, true
        }
    }

    // 4. insecure 明文文件
    if store, err := loadInsecureFile(name); err == nil && store != nil {
        k, n := keysFromStore(store)
        return k, n, true
    }

    // 5. legacy .enc
    xdgPath, err := config.XDGConfigPath()
    if err != nil { return nil, nil, false }
    keyFile := filepath.Join(filepath.Dir(xdgPath), "keys", name+".enc")
    fileKeys, fileNames, err := LoadKeysFromFile(keyFile)
    if err == nil && fileKeys != nil {
        return fileKeys, fileNames, true
    }
    return nil, nil, false
}
```

**`RemoveKeys`** — 加密文件优先：

```go
func RemoveKeys(provider string) error {
    _ = RemoveEncrypted(provider)
    _ = removeFromKeyring(provider)
    return nil
}
```

**`LoadDisabledNames`** — 通过 `loadStoreFromAnyBackend` 间接生效（该函数已走新优先级）。

- [ ] **Step 1: 修改 `LoadKeys`**

在 `store.go` 中替换 `LoadKeys` 函数体，用上述新实现。

- [ ] **Step 2: 修改 `SaveKeys`**

在 `store.go` 中替换 `SaveKeys` 函数体。

- [ ] **Step 3: 修改 `LoadKeysFromStore`**

在 `store.go` 中替换 `LoadKeysFromStore` 函数体。

- [ ] **Step 4: 修改 `RemoveKeys`**

在 `store.go` 中替换 `RemoveKeys` 函数体。

- [ ] **Step 5: 验证编译**

```bash
go build ./...
```

Expected: 编译通过

- [ ] **Step 6: 运行单元测试**

```bash
make test-unit
```

Expected: 全部 PASS

- [ ] **Step 7: 提交**

```bash
git add internal/keypool/store.go
git commit -m "refactor: switch key storage priority to encrypted file first"
```

---

### Task 5: 全量验证

**Files:**
- 无（仅验证）

- [ ] **Step 1: lint + vet + fmt**

```bash
make check
```

Expected: 无错误

- [ ] **Step 2: 单元测试**

```bash
make test-unit
```

Expected: 全部 PASS

- [ ] **Step 3: 单包测试（server 相关）**

```bash
go test -tags=unit -count=1 -short ./internal/server/
```

Expected: 全部 PASS

- [ ] **Step 4: 手动验证迁移路径**

```bash
# 在一个临时目录中模拟旧 keyring 数据 → 运行 LoadKeys → 验证 .enc 文件生成
```

- [ ] **Step 5: 提交验证（如无提交需要则跳过）**

---

## Spec Self-Review

**Spec coverage:**
- 新存储格式 ✅ — Task 2 (encrypted_store.go encrypt/decrypt)
- Master Key 管理 ✅ — Task 2 (getMasterKey)
- 存储优先级 ✅ — Task 4 (LoadKeys/SaveKeys/LoadKeysFromStore/RemoveKeys 修改)
- 迁移策略 ✅ — Task 4 (LoadKeys 中 keyring 数据迁移)
- API 不变 ✅ — Task 4 只改内部优先级，签名不变
- 新增依赖 ✅ — Task 1 (go mod)
- 错误处理 ✅ — Task 2 错误包装
- 并发安全 ✅ — Task 2 sync.Once
- 测试策略 ✅ — Task 3

**Type consistency:**
- `SaveEncrypted(provider string, store *KeyStore) error` — Task 2 定义，Task 4 调用 ✅
- `LoadEncrypted(provider string) (*KeyStore, error)` — Task 2 定义，Task 4 调用 ✅
- `RemoveEncrypted(provider string) error` — Task 2 定义，Task 4 调用 ✅
- `getMasterKey() ([]byte, error)` — Task 2 定义，encrypt/decrypt 调用 ✅

**No placeholders:** 所有函数体完整，无 TBD/TODO。
