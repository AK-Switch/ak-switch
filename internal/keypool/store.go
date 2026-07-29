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
// LoadFullStore loads the complete KeyStore from file (including disabled state).
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
// SaveFullStore writes the complete KeyStore to file.
func SaveFullStore(path string, store *KeyStore) error {

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
	return os.WriteFile(path, data, 0644)
}

// LoadKeys loads a KeyStore for a provider from the system keyring.
// If no keyring data is found, attempts fallback backends in order:
//   1. Insecure plaintext file (<XDG config dir>/keys/<provider>.json)
//   2. Migration from old encrypted file (<XDG config dir>/keys/<provider>.enc)
// Returns (nil, nil) if no stored keys exist in any backend.
func LoadKeys(provider string) (*KeyStore, error) {
	// 1. Try keyring first
	store, err := loadFromKeyring(provider)
	if err != nil {
	} else if store != nil {
		return store, nil
	}

	// 2. Try insecure plaintext file
	store, err = loadInsecureFile(provider)
	if err == nil && store != nil {
		return store, nil
	}

	// 3. Migrate from old encrypted file
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
		src, _ := os.ReadFile(oldPath)
		_ = os.WriteFile(oldPath+".bak", src, 0644)
		_ = os.Remove(oldPath)
		return oldStore, nil
	}

	return nil, nil
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

// LoadKeysFromStore loads API keys for a provider from the configured keys file
// or the standard encrypted store. Returns loaded keys and whether keys were loaded.
func LoadKeysFromStore(name string, cfg *config.Config) (keys, names []string, loaded bool) {
	// 1. Try system keyring first
	if store, err := loadFromKeyring(name); err == nil && store != nil {
		// Also try to load from insecure file and merge keys not in keyring.
		// This ensures keys saved with --insecure-storage are always loaded
		// even when keyring has data.
		if insecureStore, err := loadInsecureFile(name); err == nil && insecureStore != nil {
			for _, insecureEntry := range insecureStore.Keys {
				found := false
				for _, keyringEntry := range store.Keys {
					if insecureEntry.Key == keyringEntry.Key {
						found = true
						break
					}
				}
				if !found {
					store.Keys = append(store.Keys, insecureEntry)
				}
			}
		}
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

	// 3. Fallback: insecure plaintext file
	if store, err := loadInsecureFile(name); err == nil && store != nil {
		k, n := keysFromStore(store)
		return k, n, true
	}

	// 4. Fallback: legacy encrypted file
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
	return removeFromKeyring(provider)
}

// LoadStoreFromKeyring loads a provider's full KeyStore from the system keyring.
// Returns (nil, nil) if the provider has no stored keys.
func LoadStoreFromKeyring(provider string) (*KeyStore, error) {
	return loadFromKeyring(provider)
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

// LoadDisabledNames loads the names of permanently disabled keys for a provider
// from the key store. Returns nil if the store has no entries or no disabled keys.
// Uses the same load priority as LoadKeysFromStore: keyring → keys file → insecure file.
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
// Priority: keyring → keys file → insecure file.
// Returns the first successful result, or nil if all backends fail.
func loadStoreFromAnyBackend(name string, cfg *config.Config) *KeyStore {
	// 1. Try system keyring first
	if store, err := loadFromKeyring(name); err == nil && store != nil {
		return store
	}

	// 2. Fallback: custom keys file
	if cfg.KeysFile != "" {
		store, err := LoadFullStore(cfg.KeysFile)
		if err == nil && store != nil {
			return store
		}
	}

	// 3. Fallback: insecure plaintext file
	store, err := loadInsecureFile(name)
	if err == nil && store != nil {
		return store
	}

	return nil
}
