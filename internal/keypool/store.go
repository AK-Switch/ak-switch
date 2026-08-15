package keypool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"akswitch/internal/config"
)

// KeyEntry represents a persisted key entry with its metadata.
type KeyEntry struct {
	Key      string `json:"key"`
	Name     string `json:"name,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Deleted  bool   `json:"deleted,omitempty"`
}

// KeyStore is a JSON file backed store for API keys.
type KeyStore struct {
	Keys []KeyEntry `json:"keys"`
}

// LoadKeysFromFile reads keys from a JSON file at the given path.
// Returns the keys slice, names slice, and any error.
// If the file does not exist, returns empty slices with nil error.
func LoadKeysFromFile(path string) (keys []string, names []string, err error) {
	store, err := LoadFullStore(path)
	if err != nil {
		return nil, nil, err
	}
	if store == nil {
		return nil, nil, nil
	}
	keys = make([]string, len(store.Keys))
	names = make([]string, len(store.Keys))
	for i, entry := range store.Keys {
		keys[i] = entry.Key
		names[i] = entry.Name
	}
	return keys, names, nil
}

// SaveKeysToFile writes keys to a JSON file at the given path.
// names slice may be nil or shorter than keys.
func SaveKeysToFile(path string, keys []string, names []string) error {
	entries := make([]KeyEntry, len(keys))
	for i, k := range keys {
		name := ""
		if i < len(names) {
			name = names[i]
		}
		entries[i] = KeyEntry{Key: k, Name: name}
	}
	store := &KeyStore{Keys: entries}
	return SaveFullStore(path, store)
}

// LoadFullStore loads the complete KeyStore from file (including disabled state).
// Returns nil store with nil error if the file does not exist.
func LoadFullStore(path string) (*KeyStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var store KeyStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	if store.Keys == nil {
		store.Keys = []KeyEntry{}
	}

	return &store, nil
}

// SaveFullStore writes the complete KeyStore to file.
func SaveFullStore(path string, store *KeyStore) error {

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// SaveKeys saves a KeyStore for a provider as an encrypted file.
// The keyring entry is also removed (migration cleanup).
// Returns the error from SaveEncrypted directly if it fails.
func SaveKeys(provider string, store *KeyStore) error {
	if err := SaveEncrypted(provider, store); err != nil {
		return err
	}
	// 同时清理旧 keyring 数据
	_ = removeFromKeyring(provider)
	return nil
}

// SaveKeysInsecure saves a KeyStore for a provider as a plaintext JSON file.
// WARNING: The keys are NOT encrypted. Only use in CI/disposable environments.
// The file is written to <XDG config dir>/keys/<provider>.json.
func SaveKeysInsecure(provider string, store *KeyStore) error {
	path, err := insecureKeysPath(provider)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create keys dir: %w", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// LoadKeys loads a KeyStore for a provider from the encrypted file (primary).
// If not found or decryption fails, falls back to keyring migration, insecure
// plaintext file, and legacy .enc file in that order. Only returns an error if
// all backends fail or an unrecoverable error occurs.
//
// Returns (nil, nil) if no stored keys exist in any backend.
func LoadKeys(provider string) (*KeyStore, error) {
	// 1. 尝试加密文件（新主路径）
	store, err := LoadEncrypted(provider)
	if err != nil {
		// 解密失败时，尝试作为 legacy 明文 JSON 加载（文件名同为 .enc）
		path, pathErr := encryptedFilePath(provider)
		if pathErr != nil {
			return nil, fmt.Errorf("load keys for %q: %w", provider, pathErr)
		}
		if _, statErr := os.Stat(path); statErr == nil {
			if legacyStore, legacyErr := LoadFullStore(path); legacyErr == nil && legacyStore != nil {
				// 先备份旧文件，再迁移到加密格式
				src, _ := os.ReadFile(path)
				_ = os.WriteFile(path+".bak", src, 0600)
				_ = SaveEncrypted(provider, legacyStore)
				return legacyStore, nil
			}
		}
		// 继续尝试其他回退路径（keyring、insecure、legacy .enc）
		goto fallback
	}
	if store != nil {
		return store, nil
	}

	// 2. 尝试 keyring 旧数据（仅迁移用）
fallback:
	store, err = loadFromKeyring(provider)
	if err != nil {
		return nil, err
	}
	if store != nil {
		// 迁移: 写入加密文件
		if saveErr := SaveEncrypted(provider, store); saveErr == nil {
			// 迁移成功，删除旧 keyring 条目
			_ = removeFromKeyring(provider)
		}
		return store, nil
	}

	// 3. 尝试 insecure 明文文件
	store, err = loadInsecureFile(provider)
	if err != nil {
		return nil, err
	}
	if store != nil {
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

	// 迁移 legacy 到加密文件: 先写入加密文件成功后再备份旧文件
	if err := SaveEncrypted(provider, oldStore); err == nil {
		src, _ := os.ReadFile(oldPath)
		_ = os.WriteFile(oldPath+".bak", src, 0600)
		_ = os.Remove(oldPath)
	}
	return oldStore, nil
}

// insecureKeysPath returns the path for a provider's insecure plaintext key file.
func insecureKeysPath(provider string) (string, error) {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(xdgPath), "keys", provider+".json"), nil
}

// loadInsecureFile loads a KeyStore from a plaintext JSON file.
func loadInsecureFile(provider string) (*KeyStore, error) {
	path, err := insecureKeysPath(provider)
	if err != nil {
		return nil, err
	}
	return LoadFullStore(path)
}

// legacyKeysPath returns the old file path for a provider's keys.
func legacyKeysPath(provider string) (string, error) {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(xdgPath), "keys", provider+".enc"), nil
}

// LoadKeysFromStore loads API keys for a provider from all available backends
// in priority order: encrypted file → keyring → custom keys file → insecure
// plaintext file → legacy .enc file. Returns loaded keys and whether any
// backend had keys.
func LoadKeysFromStore(name string, cfg *config.Config) (keys, names []string, loaded bool) {
	// 1. 加密文件
	if store, err := LoadEncrypted(name); err == nil && store != nil {
		mergeInsecureKeys(name, store)
		k, n := keysFromStore(store)
		return k, n, true
	}

	// 2. keyring 旧数据（触发迁移）
	if store, err := loadFromKeyring(name); err == nil && store != nil {
		// 先合并 insecure 文件中的 key（不重复），再写入加密文件
		mergeInsecureKeys(name, store)
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
	if err != nil {
		return nil, nil, false
	}
	keyFile := filepath.Join(filepath.Dir(xdgPath), "keys", name+".enc")
	fileKeys, fileNames, err := LoadKeysFromFile(keyFile)
	if err == nil && fileKeys != nil {
		return fileKeys, fileNames, true
	}
	return nil, nil, false
}

// RemoveKeys removes a provider's keys from the system keyring.
func RemoveKeys(provider string) error {
	_ = RemoveEncrypted(provider)
	_ = removeFromKeyring(provider)
	return nil
}

// LoadStoreFromKeyring loads a provider's full KeyStore from the system keyring.
// Returns (nil, nil) if the provider has no stored keys.
func LoadStoreFromKeyring(provider string) (*KeyStore, error) {
	return loadFromKeyring(provider)
}

// keysFromStore extracts key and name slices from a KeyStore.
// Deleted entries are skipped so they are not loaded into the routing pool.
func keysFromStore(store *KeyStore) (keys, names []string) {
	for _, entry := range store.Keys {
		if entry.Deleted {
			continue
		}
		keys = append(keys, entry.Key)
		names = append(names, entry.Name)
	}
	return keys, names
}

// mergeInsecureKeys merges keys from the insecure plaintext file into store.
// Keys already present (by value) are not duplicated.
func mergeInsecureKeys(name string, store *KeyStore) {
	insecureStore, err := loadInsecureFile(name)
	if err != nil || insecureStore == nil {
		return
	}
	for _, ie := range insecureStore.Keys {
		found := false
		for _, ke := range store.Keys {
			if ie.Key == ke.Key {
				found = true
				break
			}
		}
		if !found {
			store.Keys = append(store.Keys, ie)
		}
	}
}

// LoadDisabledNames loads the names of permanently disabled keys for a provider
// from the key store. Returns nil if the store has no entries or no disabled keys.
// Uses the same load priority as LoadKeysFromStore: encrypted file → keyring → keys file → insecure file.
func LoadDisabledNames(name string, cfg *config.Config) []string {
	store := loadStoreFromAnyBackend(name, cfg)
	if store == nil {
		return nil
	}
	var disabled []string
	for _, entry := range store.Keys {
		if entry.Disabled {
			disabled = append(disabled, entry.Name)
		}
	}
	return disabled
}

// loadStoreFromAnyBackend tries to load a KeyStore from any available backend.
// Priority: encrypted file → keyring → keys file → insecure file.
// Returns the first successful result, or nil if all backends fail.
func loadStoreFromAnyBackend(name string, cfg *config.Config) *KeyStore {
	// 1. Try encrypted file first (new primary)
	if store, err := LoadEncrypted(name); err == nil && store != nil {
		return store
	}

	// 2. Fallback: keyring (migration happens at LoadKeys level)
	if store, err := loadFromKeyring(name); err == nil && store != nil {
		return store
	}

	// 3. Fallback: custom keys file
	if cfg.KeysFile != "" {
		store, err := LoadFullStore(cfg.KeysFile)
		if err == nil && store != nil {
			return store
		}
	}

	// 4. Fallback: insecure plaintext file
	store, err := loadInsecureFile(name)
	if err == nil && store != nil {
		return store
	}

	return nil
}
