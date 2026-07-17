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
// If encryption is enabled (via SetEncryptionKey), Key fields are automatically decrypted.
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

	// Decrypt keys if encryption is enabled
	if EncryptionKeySet() {
		for i := range store.Keys {
			decrypted, err := Decrypt(store.Keys[i].Key)
			if err != nil {
				return nil, fmt.Errorf("decrypt key %d: %w", i, err)
			}
			store.Keys[i].Key = string(decrypted)
		}
	}

	return &store, nil
}

// SaveFullStore writes the complete KeyStore to file.
// If encryption is enabled (via SetEncryptionKey), Key fields are automatically encrypted.
func SaveFullStore(path string, store *KeyStore) error {
	// Encrypt keys if encryption is enabled (work on a copy to avoid mutating the caller's store)
	if EncryptionKeySet() {
		encrypted := make([]KeyEntry, len(store.Keys))
		for i, entry := range store.Keys {
			enc, err := Encrypt([]byte(entry.Key))
			if err != nil {
				return fmt.Errorf("encrypt key %d: %w", i, err)
			}
			encrypted[i] = KeyEntry{
				Key:      enc,
				Name:     entry.Name,
				Disabled: entry.Disabled,
			}
		}
		store = &KeyStore{Keys: encrypted}
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// SaveKeys saves a KeyStore for a provider using the system keyring.
// This is the primary write path; file-based SaveFullStore is retained
// for migration and backward compatibility.
func SaveKeys(provider string, store *KeyStore) error {
	return saveToKeyring(provider, store)
}

// LoadKeys loads a KeyStore for a provider from the system keyring.
// If no keyring data is found, attempts migration from the old encrypted file.
// Returns (nil, nil) if no stored keys exist in any backend.
func LoadKeys(provider string) (*KeyStore, error) {
	// 1. Try keyring first
	store, err := loadFromKeyring(provider)
	if err != nil {
		store = nil
	} else if store != nil {
		return store, nil
	}

	// 2. Migrate from old encrypted file
	oldPath, pathErr := legacyKeysPath(provider)
	if pathErr != nil {
		return nil, nil
	}
	oldStore, loadErr := LoadFullStore(oldPath)
	if loadErr != nil || oldStore == nil {
		return nil, nil
	}

	// Migrate to keyring — best-effort; if it fails, keep old file
	if saveErr := saveToKeyring(provider, oldStore); saveErr == nil {
		os.Rename(oldPath, oldPath+".bak")
		return oldStore, nil
	}

	return nil, nil
}

// legacyKeysPath returns the old file path for a provider's keys.
func legacyKeysPath(provider string) (string, error) {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(xdgPath), "keys", provider+".enc"), nil
}

// LoadKeysFromStore loads API keys for a provider from the configured keys file
// or the standard encrypted store. Returns loaded keys and whether keys were loaded.
func LoadKeysFromStore(name string, cfg *config.Config) (keys, names []string, loaded bool) {
	// 1. Try system keyring first
	if store, err := loadFromKeyring(name); err == nil && store != nil {
		k, n := keysFromStore(store)
		return k, n, true
	}

	// 2. Fallback: custom keys file
	if cfg.KeysFile != "" {
		fileKeys, fileNames, err := LoadKeysFromFile(cfg.KeysFile)
		if err == nil && fileKeys != nil {
			return fileKeys, fileNames, true
		}
	}

	// 3. Fallback: legacy encrypted file
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

// keysFromStore extracts key and name slices from a KeyStore.
func keysFromStore(store *KeyStore) (keys, names []string) {
	keys = make([]string, len(store.Keys))
	names = make([]string, len(store.Keys))
	for i, entry := range store.Keys {
		keys[i] = entry.Key
		names[i] = entry.Name
	}
	return keys, names
}
