//go:build unit

package keypool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"akswitch/internal/config"
	"github.com/99designs/keyring"
)

func setupMockKeyring(t *testing.T) {
	t.Helper()
	setTestKeyring(keyring.NewArrayKeyring(nil))
	t.Cleanup(resetTestKeyring)
}

func TestSaveKeys_ThenLoadKeys(t *testing.T) {
	setupMockKeyring(t)

	store := &KeyStore{
		Keys: []KeyEntry{
			{Key: "sk-key-1", Name: "prod"},
			{Key: "sk-key-2", Name: "staging", Disabled: true},
			{Key: "sk-key-3"},
		},
	}

	if err := SaveKeys("test-provider", store); err != nil {
		t.Fatalf("SaveKeys: %v", err)
	}

	loaded, err := LoadKeys("test-provider")
	if err != nil {
		t.Fatalf("LoadKeys: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadKeys returned nil, want store")
	}
	if len(loaded.Keys) != 3 {
		t.Fatalf("got %d keys, want 3", len(loaded.Keys))
	}

	// Verify round-trip data integrity
	if loaded.Keys[0].Key != "sk-key-1" || loaded.Keys[0].Name != "prod" || loaded.Keys[0].Disabled {
		t.Errorf("entry 0 mismatch: %+v", loaded.Keys[0])
	}
	if loaded.Keys[1].Key != "sk-key-2" || loaded.Keys[1].Name != "staging" || !loaded.Keys[1].Disabled {
		t.Errorf("entry 1 mismatch: %+v", loaded.Keys[1])
	}
	if loaded.Keys[2].Key != "sk-key-3" || loaded.Keys[2].Name != "" || loaded.Keys[2].Disabled {
		t.Errorf("entry 2 mismatch: %+v", loaded.Keys[2])
	}
}

func TestLoadKeys_NotFound(t *testing.T) {
	setupMockKeyring(t)

	store, err := LoadKeys("nonexistent-provider")
	if err != nil {
		t.Fatalf("LoadKeys: %v", err)
	}
	if store != nil {
		t.Errorf("got store %+v, want nil", store)
	}
}

func TestSaveKeys_Overwrite(t *testing.T) {
	setupMockKeyring(t)

	// Save initial keys
	initial := &KeyStore{Keys: []KeyEntry{{Key: "old-key"}}}
	if err := SaveKeys("provider-a", initial); err != nil {
		t.Fatalf("SaveKeys initial: %v", err)
	}

	// Overwrite with new keys
	updated := &KeyStore{Keys: []KeyEntry{{Key: "new-key-1"}, {Key: "new-key-2"}}}
	if err := SaveKeys("provider-a", updated); err != nil {
		t.Fatalf("SaveKeys updated: %v", err)
	}

	loaded, err := LoadKeys("provider-a")
	if err != nil {
		t.Fatalf("LoadKeys: %v", err)
	}
	if len(loaded.Keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(loaded.Keys))
	}
	if loaded.Keys[0].Key != "new-key-1" {
		t.Errorf("key[0] = %q, want %q", loaded.Keys[0].Key, "new-key-1")
	}
}

func TestSaveKeys_MultipleProviders(t *testing.T) {
	setupMockKeyring(t)

	providerA := &KeyStore{Keys: []KeyEntry{{Key: "key-a"}}}
	providerB := &KeyStore{Keys: []KeyEntry{{Key: "key-b-1"}, {Key: "key-b-2"}}}

	if err := SaveKeys("provider-a", providerA); err != nil {
		t.Fatalf("SaveKeys provider-a: %v", err)
	}
	if err := SaveKeys("provider-b", providerB); err != nil {
		t.Fatalf("SaveKeys provider-b: %v", err)
	}

	loadedA, err := LoadKeys("provider-a")
	if err != nil {
		t.Fatalf("LoadKeys provider-a: %v", err)
	}
	if len(loadedA.Keys) != 1 || loadedA.Keys[0].Key != "key-a" {
		t.Errorf("provider-a: got %+v, want [key-a]", loadedA.Keys)
	}

	loadedB, err := LoadKeys("provider-b")
	if err != nil {
		t.Fatalf("LoadKeys provider-b: %v", err)
	}
	if len(loadedB.Keys) != 2 {
		t.Errorf("provider-b: got %d keys, want 2", len(loadedB.Keys))
	}
}

func TestMigration_FromOldEncFile(t *testing.T) {
	setupMockKeyring(t)

	dir := t.TempDir()
	config.ConfigDir = dir
	t.Cleanup(func() { config.ConfigDir = "" })

	// Create old-style encrypted file
	keysDir := filepath.Join(dir, "keys")
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	oldPath := filepath.Join(keysDir, "migrate-provider.enc")

	oldStore := &KeyStore{
		Keys: []KeyEntry{
			{Key: "migrated-key-1", Name: "old-prod"},
			{Key: "migrated-key-2", Disabled: true},
		},
	}
	data, err := json.MarshalIndent(oldStore, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(oldPath, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// LoadKeys should detect old file and migrate
	loaded, err := LoadKeys("migrate-provider")
	if err != nil {
		t.Fatalf("LoadKeys: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadKeys returned nil after migration, want migrated keys")
	}
	if len(loaded.Keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(loaded.Keys))
	}
	if loaded.Keys[0].Key != "migrated-key-1" || loaded.Keys[0].Name != "old-prod" {
		t.Errorf("entry 0 mismatch: %+v", loaded.Keys[0])
	}
	if loaded.Keys[1].Key != "migrated-key-2" || !loaded.Keys[1].Disabled {
		t.Errorf("entry 1 mismatch: %+v", loaded.Keys[1])
	}

	// Old file should be renamed to .bak
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old file still exists at %s, should be removed", oldPath)
	}
	bakPath := oldPath + ".bak"
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		t.Errorf("backup file not found at %s", bakPath)
	}

	// Second load should come from keyring, not file
	loaded2, err := LoadKeys("migrate-provider")
	if err != nil {
		t.Fatalf("LoadKeys second call: %v", err)
	}
	if len(loaded2.Keys) != 2 {
		t.Errorf("second load: got %d keys, want 2", len(loaded2.Keys))
	}
}

func TestMigration_EmptyOldFile(t *testing.T) {
	setupMockKeyring(t)

	dir := t.TempDir()
	config.ConfigDir = dir
	t.Cleanup(func() { config.ConfigDir = "" })

	// The directory exists but no old file → LoadKeys returns nil (no data)
	keysDir := filepath.Join(dir, "keys")
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	store, err := LoadKeys("empty-provider")
	if err != nil {
		t.Fatalf("LoadKeys: %v", err)
	}
	if store != nil {
		t.Errorf("got store %+v, want nil for empty provider", store)
	}
}

func TestLoadKeysFromStore_KeyringPriority(t *testing.T) {
	setupMockKeyring(t)

	dir := t.TempDir()
	config.ConfigDir = dir
	t.Cleanup(func() { config.ConfigDir = "" })

	// Save to keyring first
	krStore := &KeyStore{Keys: []KeyEntry{{Key: "keyring-key"}}}
	if err := SaveKeys("priority-test", krStore); err != nil {
		t.Fatalf("SaveKeys: %v", err)
	}

	// Also create old file with different data
	keysDir := filepath.Join(dir, "keys")
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	oldPath := filepath.Join(keysDir, "priority-test.enc")
	oldStore := &KeyStore{Keys: []KeyEntry{{Key: "file-key"}}}
	oldData, _ := json.MarshalIndent(oldStore, "", "  ")
	os.WriteFile(oldPath, oldData, 0644)

	// LoadKeysFromStore should return keyring data, not file data
	cfg := &config.Config{}
	keys, names, loaded := LoadKeysFromStore("priority-test", cfg)
	if !loaded {
		t.Fatal("LoadKeysFromStore: loaded=false, want true")
	}
	if len(keys) != 1 || keys[0] != "keyring-key" {
		t.Errorf("keys = %v, want [keyring-key]", keys)
	}
	if len(names) != 1 || names[0] != "" {
		t.Errorf("names = %v, want [\"\"]", names)
	}
}

func TestLoadKeysFromStore_CustomFileStillWorks(t *testing.T) {
	setupMockKeyring(t)

	dir := t.TempDir()
	config.ConfigDir = dir
	t.Cleanup(func() { config.ConfigDir = "" })

	// Write a custom keys file
	keysPath := filepath.Join(dir, "my-keys.json")
	store := &KeyStore{
		Keys: []KeyEntry{
			{Key: "custom-file-key"},
		},
	}
	data, _ := json.MarshalIndent(store, "", "  ")
	os.WriteFile(keysPath, data, 0644)

	cfg := &config.Config{KeysFile: keysPath}
	keys, _, loaded := LoadKeysFromStore("test", cfg)
	if !loaded {
		t.Fatal("LoadKeysFromStore: loaded=false, want true")
	}
	if len(keys) != 1 || keys[0] != "custom-file-key" {
		t.Errorf("keys = %v, want [custom-file-key]", keys)
	}
}

func TestKeysFromStore(t *testing.T) {
	store := &KeyStore{
		Keys: []KeyEntry{
			{Key: "k1", Name: "n1"},
			{Key: "k2"},
		},
	}
	keys, names := keysFromStore(store)
	if len(keys) != 2 || keys[0] != "k1" || keys[1] != "k2" {
		t.Errorf("keys = %v, want [k1 k2]", keys)
	}
	if len(names) != 2 || names[0] != "n1" || names[1] != "" {
		t.Errorf("names = %v, want [n1 ]", names)
	}
}

func TestKeyringItemKey(t *testing.T) {
	cases := []struct {
		provider string
		want     string
	}{
		{"nvidia", "akswitch:nvidia"},
		{"openai", "akswitch:openai"},
		{"", "akswitch:"},
	}
	for _, tc := range cases {
		got := keyringItemKey(tc.provider)
		if got != tc.want {
			t.Errorf("keyringItemKey(%q) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}
func TestSaveKeysInsecure_ThenLoadKeys(t *testing.T) {
	store := &KeyStore{
		Keys: []KeyEntry{
			{Key: "sk-insecure-1", Name: "ci-env"},
			{Key: "sk-insecure-2"},
		},
	}

	if err := SaveKeysInsecure("insecure-test", store); err != nil {
		t.Fatalf("SaveKeysInsecure: %v", err)
	}

	// LoadKeys should find the insecure file (keyring not set up)
	loaded, err := LoadKeys("insecure-test")
	if err != nil {
		t.Fatalf("LoadKeys: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadKeys returned nil, want insecure file data")
	}
	if len(loaded.Keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(loaded.Keys))
	}
	if loaded.Keys[0].Key != "sk-insecure-1" || loaded.Keys[0].Name != "ci-env" {
		t.Errorf("entry 0 mismatch: %+v", loaded.Keys[0])
	}
	if loaded.Keys[1].Key != "sk-insecure-2" {
		t.Errorf("entry 1 mismatch: %+v", loaded.Keys[1])
	}
}

func TestLoadKeys_InsecureFileFallback(t *testing.T) {
	// Setup: mock keyring is empty, but insecure file exists
	setupMockKeyring(t)

	store := &KeyStore{Keys: []KeyEntry{{Key: "fallback-key"}}}
	if err := SaveKeysInsecure("fallback-test", store); err != nil {
		t.Fatalf("SaveKeysInsecure: %v", err)
	}

	// LoadKeys should find the insecure file (keyring has no data)
	loaded, err := LoadKeys("fallback-test")
	if err != nil {
		t.Fatalf("LoadKeys: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadKeys returned nil, want insecure fallback data")
	}
	if len(loaded.Keys) != 1 || loaded.Keys[0].Key != "fallback-key" {
		t.Errorf("unexpected data: %+v", loaded.Keys)
	}
}

func TestLoadKeysFromStore_InsecureFileFallback(t *testing.T) {
	store := &KeyStore{Keys: []KeyEntry{{Key: "fromstore-insecure"}}}
	if err := SaveKeysInsecure("fromstore-test", store); err != nil {
		t.Fatalf("SaveKeysInsecure: %v", err)
	}

	cfg := &config.Config{}
	keys, names, loaded := LoadKeysFromStore("fromstore-test", cfg)
	if !loaded {
		t.Fatal("LoadKeysFromStore: loaded=false, want true")
	}
	if len(keys) != 1 || keys[0] != "fromstore-insecure" {
		t.Errorf("keys = %v, want [fromstore-insecure]", keys)
	}
	if len(names) != 1 || names[0] != "" {
		t.Errorf("names = %v, want empty strings", names)
	}
}

func TestSaveKeysInsecure_FileExists(t *testing.T) {
	// Write an insecure file, then overwrite it
	store1 := &KeyStore{Keys: []KeyEntry{{Key: "first-version"}}}
	if err := SaveKeysInsecure("overwrite-test", store1); err != nil {
		t.Fatalf("SaveKeysInsecure first: %v", err)
	}

	store2 := &KeyStore{Keys: []KeyEntry{{Key: "second-version"}}}
	if err := SaveKeysInsecure("overwrite-test", store2); err != nil {
		t.Fatalf("SaveKeysInsecure second: %v", err)
	}

	loaded, err := LoadKeys("overwrite-test")
	if err != nil {
		t.Fatalf("LoadKeys: %v", err)
	}
	if loaded == nil || len(loaded.Keys) != 1 {
		t.Fatalf("expected 1 key, got %+v", loaded)
	}
	if loaded.Keys[0].Key != "second-version" {
		t.Errorf("key = %q, want %q", loaded.Keys[0].Key, "second-version")
	}
}
