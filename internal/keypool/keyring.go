package keypool

import (
	"encoding/json"
	"fmt"
	"sync"

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
func initKeyring() error {
	keyringMu.Lock()
	defer keyringMu.Unlock()
	if keyringBackend != nil {
		return nil
	}
	kr, err := openKeyring(keyring.Config{
		ServiceName: "akswitch",
	})
	if err != nil {
		return fmt.Errorf("open keyring: %w", err)
	}
	keyringBackend = kr
	return nil
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