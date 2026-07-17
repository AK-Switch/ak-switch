package keypool

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"akswitch/internal/config"
	"github.com/99designs/keyring"
)

// openKeyring is a package-level variable that can be replaced in tests.
var openKeyring = defaultOpenKeyring

func defaultOpenKeyring(cfg keyring.Config) (keyring.Keyring, error) {
	return keyring.Open(cfg)
}

var (
	keyringMu      sync.Mutex
	keyringBackend keyring.Keyring
)

// initKeyring lazily opens the system keyring backend.
// Thread-safe; subsequent calls are no-ops after successful init.
// Tries OS-level keyring first (Tier 1), then falls back to
// the encrypted file backend (Tier 2) for headless environments.
func initKeyring() error {
	keyringMu.Lock()
	defer keyringMu.Unlock()
	if keyringBackend != nil {
		return nil
	}

	// Tier 1: Try OS-level keyring (Keychain / WinCred / SecretService)
	kr, err := openKeyring(keyring.Config{
		ServiceName: "akswitch",
	})
	if err == nil {
		keyringBackend = kr
		return nil
	}
	firstErr := err

	// Tier 2: Fall back to encrypted file backend
	// (headless Linux, CI, WSL without desktop environment, etc.)
	fallbackDir := keyringFallbackDir()
	if err := os.MkdirAll(fallbackDir, 0700); err != nil {
		return fmt.Errorf("open keyring: %w (create fallback dir: %v)", firstErr, err)
	}

	passwordFunc := fallbackPasswordFunc(filepath.Join(fallbackDir, "password"))
	kr, err = openKeyring(keyring.Config{
		ServiceName:      "akswitch",
		AllowedBackends:  []keyring.BackendType{keyring.FileBackend},
		FileDir:          fallbackDir,
		FilePasswordFunc: passwordFunc,
	})
	if err != nil {
		return fmt.Errorf("open keyring: %w (fallback failed: %v)", firstErr, err)
	}

	keyringBackend = kr
	return nil
}

// keyringFallbackDir returns the directory for the encrypted file backend.
// Located at <XDG config dir>/keyring-fallback/.
func keyringFallbackDir() string {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		return filepath.Join(os.TempDir(), "akswitch-keyring")
	}
	return filepath.Join(filepath.Dir(xdgPath), "keyring-fallback")
}

// fallbackPasswordFunc returns a PromptFunc that reads the password from
// the given file, generating a random password on first use.
func fallbackPasswordFunc(passwordFile string) keyring.PromptFunc {
	return func(_ string) (string, error) {
		data, err := os.ReadFile(passwordFile)
		if err == nil {
			return string(data), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read password file: %w", err)
		}
		// Generate a random hex password on first use
		pw := make([]byte, 32)
		if _, err := rand.Read(pw); err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		pwHex := hex.EncodeToString(pw)
		if err := os.WriteFile(passwordFile, []byte(pwHex), 0600); err != nil {
			return "", fmt.Errorf("write password: %w", err)
		}
		return pwHex, nil
	}
}

// setTestKeyring replaces the keyring backend for testing.
// Must be called before the function under test, paired with resetTestKeyring.
func setTestKeyring(kr keyring.Keyring) {
	keyringMu.Lock()
	keyringBackend = kr
	keyringMu.Unlock()
}

// resetTestKeyring clears the keyring backend (for testing).
func resetTestKeyring() {
	keyringMu.Lock()
	keyringBackend = nil
	keyringMu.Unlock()
}

// keyringItemKey returns the keyring key used to store a provider's keys.
func keyringItemKey(provider string) string {
	return "akswitch:" + provider
}

// saveToKeyring saves a provider's KeyStore to the system keyring.
func saveToKeyring(provider string, store *KeyStore) error {
	if err := initKeyring(); err != nil {
		return err
	}
	data, err := json.Marshal(store)
	if err != nil {
		return fmt.Errorf("marshal keystore: %w", err)
	}
	return keyringBackend.Set(keyring.Item{
		Key:  keyringItemKey(provider),
		Data: data,
	})
}

// loadFromKeyring loads a provider's KeyStore from the system keyring.
// Returns (nil, nil) if the provider has no stored keys.
func loadFromKeyring(provider string) (*KeyStore, error) {
	if err := initKeyring(); err != nil {
		return nil, err
	}
	item, err := keyringBackend.Get(keyringItemKey(provider))
	if err != nil {
		if err == keyring.ErrKeyNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("keyring get: %w", err)
	}
	var store KeyStore
	if err := json.Unmarshal(item.Data, &store); err != nil {
		return nil, fmt.Errorf("unmarshal keystore: %w", err)
	}
	if store.Keys == nil {
		store.Keys = []KeyEntry{}
	}
	return &store, nil
}

// removeFromKeyring removes a provider's keys from the system keyring.
func removeFromKeyring(provider string) error {
	if err := initKeyring(); err != nil {
		return err
	}
	return keyringBackend.Remove(keyringItemKey(provider))
}