//go:build unit

package keypool

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"akswitch/internal/config"
	"github.com/99designs/keyring"
)

// resetMasterKeyForTest resets the sync.Once and cached key for testing.
func resetMasterKeyForTest() {
	masterKey = nil
	masterKeyOnce = sync.Once{}
	masterKeyErr = nil
}

func TestSaveEncrypted_ThenLoadEncrypted(t *testing.T) {
	resetMasterKeyForTest()
	kr := keyring.NewArrayKeyring(nil)
	setTestKeyring(kr)
	defer resetTestKeyring()

	dir := t.TempDir()
	config.ConfigDir = dir
	defer func() { config.ConfigDir = "" }()

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
	kr := keyring.NewArrayKeyring(nil)
	setTestKeyring(kr)
	defer resetTestKeyring()

	dir := t.TempDir()
	config.ConfigDir = dir
	defer func() { config.ConfigDir = "" }()

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
	kr1 := keyring.NewArrayKeyring(nil)
	setTestKeyring(kr1)

	dir := t.TempDir()
	config.ConfigDir = dir
	defer func() { config.ConfigDir = "" }()

	// Save with key A
	store := &KeyStore{Keys: []KeyEntry{{Key: "sk-abc", Name: "key-a"}}}
	if err := SaveEncrypted("wrong-key-test", store); err != nil {
		t.Fatalf("SaveEncrypted: %v", err)
	}

	// Switch to key B
	resetMasterKeyForTest()
	kr2 := keyring.NewArrayKeyring(nil)
	fakeKey := make([]byte, 32)
	for i := range fakeKey {
		fakeKey[i] = byte(i + 1)
	}
	kr2.Set(keyring.Item{Key: "akswitch:master-key", Data: fakeKey})
	setTestKeyring(kr2)

	_, err := LoadEncrypted("wrong-key-test")
	if err == nil {
		t.Error("expected decrypt error with wrong key, got nil")
	}
}

func TestRemoveEncrypted(t *testing.T) {
	resetMasterKeyForTest()
	kr := keyring.NewArrayKeyring(nil)
	setTestKeyring(kr)
	defer resetTestKeyring()

	dir := t.TempDir()
	config.ConfigDir = dir
	defer func() { config.ConfigDir = "" }()

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

func TestRoundTrip_PreservesData(t *testing.T) {
	resetMasterKeyForTest()
	kr := keyring.NewArrayKeyring(nil)
	setTestKeyring(kr)
	defer resetTestKeyring()

	dir := t.TempDir()
	config.ConfigDir = dir
	defer func() { config.ConfigDir = "" }()

	original := &KeyStore{
		Keys: []KeyEntry{
			{Key: "sk-1", Name: "alpha", Disabled: false},
			{Key: "sk-2", Name: "beta", Disabled: true},
			{Key: "sk-3", Name: "", Disabled: false},
		},
	}
	if err := SaveEncrypted("roundtrip", original); err != nil {
		t.Fatalf("SaveEncrypted: %v", err)
	}

	loaded, err := LoadEncrypted("roundtrip")
	if err != nil {
		t.Fatalf("LoadEncrypted: %v", err)
	}
	if len(loaded.Keys) != 3 {
		t.Fatalf("got %d keys, want 3", len(loaded.Keys))
	}
	for i, want := range original.Keys {
		got := loaded.Keys[i]
		if got.Key != want.Key || got.Name != want.Name || got.Disabled != want.Disabled {
			t.Errorf("key[%d]: got {Key:%q Name:%q Disabled:%v}, want {Key:%q Name:%q Disabled:%v}",
				i, got.Key, got.Name, got.Disabled, want.Key, want.Name, want.Disabled)
		}
	}
}

func TestSaveEncrypted_CreatesKeysDir(t *testing.T) {
	resetMasterKeyForTest()
	kr := keyring.NewArrayKeyring(nil)
	setTestKeyring(kr)
	defer resetTestKeyring()

	dir := t.TempDir()
	config.ConfigDir = dir
	defer func() { config.ConfigDir = "" }()

	// keys/ directory should not exist yet
	keysDir := filepath.Join(dir, "keys")
	if _, err := os.Stat(keysDir); !os.IsNotExist(err) {
		t.Fatalf("keys dir already exists at %s", keysDir)
	}

	store := &KeyStore{Keys: []KeyEntry{{Key: "sk-abc"}}}
	if err := SaveEncrypted("dir-test", store); err != nil {
		t.Fatalf("SaveEncrypted: %v", err)
	}

	// keys/ directory should now exist (0700 on Unix; Windows ignores mode)
	info, err := os.Stat(keysDir)
	if err != nil {
		t.Fatalf("keys dir not created: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
		t.Errorf("keys dir perms = %o, want 0700", info.Mode().Perm())
	}
}

func TestSaveEncrypted_AtomicWrite(t *testing.T) {
	resetMasterKeyForTest()
	kr := keyring.NewArrayKeyring(nil)
	setTestKeyring(kr)
	defer resetTestKeyring()

	dir := t.TempDir()
	config.ConfigDir = dir
	defer func() { config.ConfigDir = "" }()

	store := &KeyStore{Keys: []KeyEntry{{Key: "sk-abc"}}}
	if err := SaveEncrypted("atomic-test", store); err != nil {
		t.Fatalf("SaveEncrypted: %v", err)
	}

	// .tmp file should not exist after successful save
	tmpPath := filepath.Join(keysPath(dir, "atomic-test")) + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error(".tmp file still exists after save, atomic write may have failed")
	}
}

// keysPath returns the expected encrypted file path for a provider in the given config dir.
func keysPath(dir, provider string) string {
	return filepath.Join(dir, "keys", provider+".enc")
}
