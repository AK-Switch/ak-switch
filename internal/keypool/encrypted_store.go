package keypool

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"akswitch/internal/config"
	"github.com/99designs/keyring"
)

// masterKeyFile returns the path to the master.key file.
func masterKeyFile() (string, error) {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(xdgPath), "keys", "master.key"), nil
}

// ensureKeysDir ensures the <config_dir>/keys/ directory exists (0700).
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

// getMasterKey returns a 32-byte master key, lazily initialized once.
// Priority: keyring backend (test-injectable) -> local master.key -> generate new key
func getMasterKey() ([]byte, error) {
	masterKeyOnce.Do(func() {
		// 1. Try keyring backend (initKeyring checks test injection + OS keyring)
		if err := initKeyring(); err == nil {
			item, err := keyringBackend.Get("akswitch:master-key")
			if err == nil {
				masterKey = item.Data
				return
			}
		}

		// 2. Try local master.key file
		mkPath, mkErr := masterKeyFile()
		if mkErr != nil {
			masterKeyErr = fmt.Errorf("master key file path: %w", mkErr)
			return
		}
		data, readErr := os.ReadFile(mkPath)
		if readErr == nil {
			if len(data) == 32 {
				masterKey = data
				return
			}
			masterKeyErr = fmt.Errorf("master key file has invalid length %d, want 32", len(data))
			return
		}
		if !os.IsNotExist(readErr) {
			masterKeyErr = fmt.Errorf("read master key file: %w", readErr)
			return
		}

		// 3. Generate a new key
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			masterKeyErr = fmt.Errorf("generate master key: %w", err)
			return
		}

		// Prefer storing in keyring
		if keyringBackend != nil {
			if setErr := keyringBackend.Set(keyring.Item{
				Key:  "akswitch:master-key",
				Data: key,
			}); setErr == nil {
				masterKey = key
				return
			}
		}

		// Keyring unavailable, write to local file
		if err := ensureKeysDir(); err != nil {
			masterKeyErr = fmt.Errorf("create keys dir: %w", err)
			return
		}
		mkPath, mkErr = masterKeyFile()
		if mkErr != nil {
			masterKeyErr = fmt.Errorf("master key file: %w", mkErr)
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

// encrypt encrypts plaintext using AES-256-GCM.
// Output format: [12-byte nonce][ciphertext + 16-byte tag]
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

// decrypt decrypts data using AES-256-GCM.
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

// encryptedFilePath returns the encrypted file path for a given provider.
func encryptedFilePath(provider string) (string, error) {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(xdgPath), "keys", provider+".enc"), nil
}

// SaveEncrypted encrypts and writes the KeyStore to <provider>.enc.
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
	// Atomic write: write to temp file then rename
	// Use Remove + Rename instead of Rename alone because os.Rename
	// silently fails on Windows when the target file already exists.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, encrypted, 0600); err != nil {
		return fmt.Errorf("save keys for %q: %w", provider, err)
	}
	_ = os.Remove(path)
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("save keys for %q: %w", provider, err)
	}
	return nil
}

// LoadEncrypted reads and decrypts the KeyStore from <provider>.enc.
// Returns (nil, nil) when the file does not exist.
func LoadEncrypted(provider string) (*KeyStore, error) {
	mk, err := getMasterKey()
	if err != nil {
		return nil, fmt.Errorf("load keys for %q: get master key: %w", provider, err)
	}
	path, err := encryptedFilePath(provider)
	if err != nil {
		return nil, fmt.Errorf("load keys for %q: %w", provider, err)
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

// RemoveEncrypted deletes the <provider>.enc file.
func RemoveEncrypted(provider string) error {
	path, err := encryptedFilePath(provider)
	if err != nil {
		return fmt.Errorf("remove encrypted keys for %q: %w", provider, err)
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove encrypted keys for %q: %w", provider, err)
	}
	return nil
}
