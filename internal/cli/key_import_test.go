//go:build unit

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
)

// ── parseJSONL ────────────────────────────────────────

func TestParseJSONL_Basic(t *testing.T) {
	data := []byte(`{"key": "sk-111", "name": "alpha"}
{"key": "sk-222", "name": "beta"}
`)
	entries, err := parseJSONL(data)
	if err != nil {
		t.Fatalf("parseJSONL failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Key != "sk-111" || entries[0].Name != "alpha" {
		t.Errorf("entry 0 = %+v, want {Key: sk-111, Name: alpha}", entries[0])
	}
	if entries[1].Key != "sk-222" || entries[1].Name != "beta" {
		t.Errorf("entry 1 = %+v, want {Key: sk-222, Name: beta}", entries[1])
	}
}

func TestParseJSONL_WithApiKeyField(t *testing.T) {
	data := []byte(`{"api_key": "sk-111", "api_key_name": "alpha"}
{"api_key": "sk-222", "api_key_name": "beta"}
`)
	entries, err := parseJSONL(data)
	if err != nil {
		t.Fatalf("parseJSONL failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Key != "sk-111" || entries[0].Name != "alpha" {
		t.Errorf("entry 0 = %+v, want {Key: sk-111, Name: alpha}", entries[0])
	}
	if entries[1].Key != "sk-222" || entries[1].Name != "beta" {
		t.Errorf("entry 1 = %+v, want {Key: sk-222, Name: beta}", entries[1])
	}
}

func TestParseJSONL_WithApiKeyPlain(t *testing.T) {
	data := []byte(`{"api_key_plain": "sk-111", "api_key_name": "alpha"}
{"api_key_plain": "sk-222"}
`)
	entries, err := parseJSONL(data)
	if err != nil {
		t.Fatalf("parseJSONL failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Key != "sk-111" || entries[0].Name != "alpha" {
		t.Errorf("entry 0 = %+v, want {Key: sk-111, Name: alpha}", entries[0])
	}
	if entries[1].Key != "sk-222" || entries[1].Name != "" {
		t.Errorf("entry 1 = %+v, want {Key: sk-222, Name: \"\"}", entries[1])
	}
}

func TestParseJSONL_SkipsEmptyLines(t *testing.T) {
	data := []byte(`{"key": "sk-111"}

{"key": "sk-222"}
`)
	entries, err := parseJSONL(data)
	if err != nil {
		t.Fatalf("parseJSONL failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestParseJSONL_InvalidJSON(t *testing.T) {
	data := []byte(`{"key": "sk-111"}
not json
`)
	_, err := parseJSONL(data)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParseJSONL_EmptyData(t *testing.T) {
	_, err := parseJSONL([]byte{})
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
}

func TestParseJSONL_OnlyWhitespace(t *testing.T) {
	_, err := parseJSONL([]byte("\n\n   \n"))
	if err == nil {
		t.Fatal("expected error for whitespace-only data, got nil")
	}
}

// ── dedupEntries ──────────────────────────────────────

func TestDedupEntries_AllNew(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "alpha"},
		{Key: "sk-222", Name: "beta"},
	}
	store := &keypool.KeyStore{Keys: []keypool.KeyEntry{}}
	newEntries, skipped := dedupEntries(entries, store)
	if skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", skipped)
	}
	if len(newEntries) != 2 {
		t.Fatalf("expected 2 new entries, got %d", len(newEntries))
	}
}

func TestDedupEntries_SomeDuplicates(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "alpha"},
		{Key: "sk-222", Name: "beta"},
		{Key: "sk-333", Name: "gamma"},
	}
	store := &keypool.KeyStore{Keys: []keypool.KeyEntry{
		{Key: "sk-111", Name: "existing-alpha"},
		{Key: "sk-333", Name: "existing-gamma"},
	}}
	newEntries, skipped := dedupEntries(entries, store)
	if skipped != 2 {
		t.Errorf("expected 2 skipped, got %d", skipped)
	}
	if len(newEntries) != 1 {
		t.Fatalf("expected 1 new entry, got %d", len(newEntries))
	}
	if newEntries[0].Key != "sk-222" {
		t.Errorf("new entry = %+v, want {Key: sk-222}", newEntries[0])
	}
}

func TestDedupEntries_AllDuplicates(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "alpha"},
		{Key: "sk-222", Name: "beta"},
	}
	store := &keypool.KeyStore{Keys: []keypool.KeyEntry{
		{Key: "sk-111"},
		{Key: "sk-222"},
	}}
	newEntries, skipped := dedupEntries(entries, store)
	if skipped != 2 {
		t.Errorf("expected 2 skipped, got %d", skipped)
	}
	if len(newEntries) != 0 {
		t.Errorf("expected 0 new entries, got %d", len(newEntries))
	}
}

func TestDedupEntries_EmptyInput(t *testing.T) {
	newEntries, skipped := dedupEntries(nil, &keypool.KeyStore{Keys: []keypool.KeyEntry{}})
	if skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", skipped)
	}
	if len(newEntries) != 0 {
		t.Errorf("expected 0 new entries, got %d", len(newEntries))
	}
}

// ── autoNumberNames ───────────────────────────────────

func TestAutoNumberNames_UniqueNames(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "alpha"},
		{Key: "sk-222", Name: "beta"},
		{Key: "sk-333", Name: "gamma"},
	}
	result := autoNumberNames(entries, nil)
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	if result[0].Name != "alpha" {
		t.Errorf("entry 0 name = %q, want %q", result[0].Name, "alpha")
	}
	if result[1].Name != "beta" {
		t.Errorf("entry 1 name = %q, want %q", result[1].Name, "beta")
	}
	if result[2].Name != "gamma" {
		t.Errorf("entry 2 name = %q, want %q", result[2].Name, "gamma")
	}
}

func TestAutoNumberNames_DuplicateNames(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "auto-reg"},
		{Key: "sk-222", Name: "auto-reg"},
		{Key: "sk-333", Name: "auto-reg"},
	}
	result := autoNumberNames(entries, nil)
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	if result[0].Name != "auto-reg-1" {
		t.Errorf("entry 0 name = %q, want %q", result[0].Name, "auto-reg-1")
	}
	if result[1].Name != "auto-reg-2" {
		t.Errorf("entry 1 name = %q, want %q", result[1].Name, "auto-reg-2")
	}
	if result[2].Name != "auto-reg-3" {
		t.Errorf("entry 2 name = %q, want %q", result[2].Name, "auto-reg-3")
	}
}

func TestAutoNumberNames_EmptyNames(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "alpha"},
		{Key: "sk-222", Name: ""},
		{Key: "sk-333", Name: "alpha"},
	}
	result := autoNumberNames(entries, nil)
	if result[0].Name != "alpha-1" {
		t.Errorf("entry 0 name = %q, want %q", result[0].Name, "alpha-1")
	}
	if result[1].Name != "" {
		t.Errorf("entry 1 name should be empty, got %q", result[1].Name)
	}
	if result[2].Name != "alpha-2" {
		t.Errorf("entry 2 name = %q, want %q", result[2].Name, "alpha-2")
	}
}

func TestAutoNumberNames_SingleEntry(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "only-one"},
	}
	result := autoNumberNames(entries, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Name != "only-one" {
		t.Errorf("name = %q, want %q", result[0].Name, "only-one")
	}
}

func TestAutoNumberNames_CrossBatch(t *testing.T) {
	// Simulate store with existing numbered entries: auto-reg-1~4 from a previous import
	store := &keypool.KeyStore{
		Keys: []keypool.KeyEntry{
			{Key: "sk-old-1", Name: "auto-reg-1"},
			{Key: "sk-old-2", Name: "auto-reg-2"},
			{Key: "sk-old-3", Name: "auto-reg-3"},
			{Key: "sk-old-4", Name: "auto-reg-4"},
			{Key: "sk-other", Name: "manual-key"},
		},
	}
	// New batch: 3 keys all named "auto-reg"
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "auto-reg"},
		{Key: "sk-222", Name: "auto-reg"},
		{Key: "sk-333", Name: "auto-reg"},
	}
	result := autoNumberNames(entries, store)
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	// Should continue from existing max suffix 4, not restart at 1
	if result[0].Name != "auto-reg-5" {
		t.Errorf("entry 0 name = %q, want %q", result[0].Name, "auto-reg-5")
	}
	if result[1].Name != "auto-reg-6" {
		t.Errorf("entry 1 name = %q, want %q", result[1].Name, "auto-reg-6")
	}
	if result[2].Name != "auto-reg-7" {
		t.Errorf("entry 2 name = %q, want %q", result[2].Name, "auto-reg-7")
	}
}

func TestAutoNumberNames_CrossBatchNilStore(t *testing.T) {
	// Same as TestAutoNumberNames_DuplicateNames but with explicit nil store
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "auto-reg"},
		{Key: "sk-222", Name: "auto-reg"},
		{Key: "sk-333", Name: "auto-reg"},
	}
	result := autoNumberNames(entries, nil)
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	if result[0].Name != "auto-reg-1" {
		t.Errorf("entry 0 name = %q, want %q", result[0].Name, "auto-reg-1")
	}
	if result[1].Name != "auto-reg-2" {
		t.Errorf("entry 1 name = %q, want %q", result[1].Name, "auto-reg-2")
	}
	if result[2].Name != "auto-reg-3" {
		t.Errorf("entry 2 name = %q, want %q", result[2].Name, "auto-reg-3")
	}
}

// ── keyExportCmd ────────────────────────────────────────

func TestKeyExportCmd(t *testing.T) {
	// Use os.Args override (matching provider_cmd_test.go pattern) to avoid
	// contaminating rootCmd.args for other tests that delegate to rootCmd.ExecuteC().
	tests := []struct {
		name    string
		osArgs  []string
		wantErr bool
	}{
		{name: "missing provider", osArgs: []string{"akswitch", "key", "export"}, wantErr: true},
		{name: "empty provider", osArgs: []string{"akswitch", "key", "export", "nonexistent"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			os.Args = tt.osArgs
			defer func() { os.Args = origArgs }()

			cmd := keyExportCmd
			err := cmd.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("export error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// ── keyImportCmd --create flag ──────────────────────────

func TestKeyImportCreateFlag(t *testing.T) {
	t.Run("create without target", func(t *testing.T) {
		origArgs := os.Args
		os.Args = []string{"akswitch", "key", "import", "--create", "test-provider", "sk-key1"}
		defer func() { os.Args = origArgs }()

		err := keyImportCmd.Execute()
		if err == nil {
			t.Fatal("expected error when --create used without --target, got nil")
		}
		if !strings.Contains(err.Error(), "--target") {
			t.Errorf("error = %q, want it to mention --target", err.Error())
		}
	})

	t.Run("create target nonexistent provider", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("AKSWITCH_CONFIG_DIR", tmpDir)

		// Create empty config with no providers
		tc := &config.TomlConfig{Provider: make(map[string]*config.Config)}
		configPath := filepath.Join(tmpDir, "config.toml")
		if err := config.SaveTomlConfig(tc, configPath); err != nil {
			t.Fatalf("failed to save test config: %v", err)
		}

		origArgs := os.Args
		os.Args = []string{"akswitch", "key", "import", "--create", "--target", "https://api.test.com", "--insecure-storage", "test-provider", "sk-key1"}
		defer func() { os.Args = origArgs }()

		err := keyImportCmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify provider was created in config
		loaded, err := config.LoadTomlConfig(configPath)
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}
		p, ok := loaded.Provider["test-provider"]
		if !ok {
			t.Fatal("provider 'test-provider' was not created in config")
		}
		if p.TargetBase != "https://api.test.com" {
			t.Errorf("target = %q, want %q", p.TargetBase, "https://api.test.com")
		}
	})

	t.Run("create with existing provider", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("AKSWITCH_CONFIG_DIR", tmpDir)

		// Create config with existing provider
		tc := &config.TomlConfig{
			Provider: map[string]*config.Config{
				"existing-provider": {ProviderConfig: config.ProviderConfig{TargetBase: "https://original.com"}},
			},
		}
		configPath := filepath.Join(tmpDir, "config.toml")
		if err := config.SaveTomlConfig(tc, configPath); err != nil {
			t.Fatalf("failed to save test config: %v", err)
		}

		origArgs := os.Args
		os.Args = []string{"akswitch", "key", "import", "--create", "--target", "https://api.test.com", "--insecure-storage", "existing-provider", "sk-key1"}
		defer func() { os.Args = origArgs }()

		err := keyImportCmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify provider config is unchanged (original target preserved)
		loaded, err := config.LoadTomlConfig(configPath)
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}
		p, ok := loaded.Provider["existing-provider"]
		if !ok {
			t.Fatal("provider 'existing-provider' should still exist")
		}
		if p.TargetBase != "https://original.com" {
			t.Errorf("target = %q, want %q (should be unchanged)", p.TargetBase, "https://original.com")
		}
	})
}
