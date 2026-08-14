//go:build unit

package cli

import (
	"io"
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

// ── parseCSV ────────────────────────────────────────

func TestParseCSV(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    []keypool.KeyEntry
		wantErr bool
	}{
		{
			name: "header key_name",
			data: "key,name\nsk-1,key1\nsk-2,key2",
			want: []keypool.KeyEntry{
				{Key: "sk-1", Name: "key1"},
				{Key: "sk-2", Name: "key2"},
			},
		},
		{
			name: "header api_key_account",
			data: "api_key,account\nsk-xxx,user1",
			want: []keypool.KeyEntry{
				{Key: "sk-xxx", Name: "user1"},
			},
		},
		{
			name: "no header 2 cols",
			data: "my-key,sk-xxx",
			want: []keypool.KeyEntry{
				{Name: "my-key", Key: "sk-xxx"},
			},
		},
		{
			name: "no header 1 col",
			data: "sk-xxx",
			want: []keypool.KeyEntry{
				{Key: "sk-xxx"},
			},
		},
		{
			name: "comment lines",
			data: "# This is a comment\n# generated by BazaarLink\nkey\nsk-xxx",
			want: []keypool.KeyEntry{
				{Key: "sk-xxx"},
			},
		},
		{
			name: "no header 3 cols error",
			data: "a,b,c",
			wantErr: true,
		},
		{
			name: "empty data",
			data: "",
			wantErr: true,
		},
		{
			name: "all comments",
			data: "# comment 1\n# comment 2\n  ",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCSV([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCSV() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("parseCSV() got %d entries, want %d", len(got), len(tt.want))
					return
				}
				for i := range got {
					if got[i].Key != tt.want[i].Key || got[i].Name != tt.want[i].Name {
						t.Errorf("entry[%d] = %+v, want %+v", i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

// ── keyRestoreCmd / keyPurgeCmd ──────────────────────────

var testKeyDir string

func init() {
	// Set up a shared temp config directory with a master.key file so that
	// keypool.SaveKeys / LoadKeys (encrypted storage) work in CLI tests.
	// getMasterKey() caches the key via sync.Once; all tests in this package
	// share the same cached master key.
	// NOTE: do NOT set config.ConfigDir here — that would override
	// AKSWITCH_CONFIG_DIR set by individual tests (e.g. TestKeyImportCreateFlag).
	testKeyDir = tTempDirForPackage()
	keysDir := filepath.Join(testKeyDir, "keys")
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		panic("failed to create keys dir for CLI tests: " + err.Error())
	}
	// Write a 32-byte master key so getMasterKey() reads it from file.
	if err := os.WriteFile(filepath.Join(keysDir, "master.key"), []byte("12345678901234567890123456789012"), 0600); err != nil {
		panic("failed to write master.key for CLI tests: " + err.Error())
	}
}

// tTempDirForPackage creates a temp directory that lives until process exit.
// Go's t.TempDir is per-test and cleaned up after each test, which is too early
// because getMasterKey() caches the master key globally (sync.Once).
func tTempDirForPackage() string {
	dir, err := os.MkdirTemp("", "akswitch-cli-tests-*")
	if err != nil {
		panic("failed to create package temp dir: " + err.Error())
	}
	return dir
}

// setupKeyStore creates a provider with the given keys and soft-deletes the one
// at delIndex (if >= 0). Returns the provider name.
func setupKeyStore(t *testing.T, entries []keypool.KeyEntry, delIndex int) string {
	t.Helper()
	provider := "test-restore-purge"
	store := &keypool.KeyStore{Keys: entries}
	if delIndex >= 0 && delIndex < len(entries) {
		store.Keys[delIndex].Deleted = true
	}
	// Ensure config dir is set (previous test's cleanup may have reset it)
	origConfigDir := config.ConfigDir
	config.ConfigDir = testKeyDir
	t.Cleanup(func() { config.ConfigDir = origConfigDir })
	if err := keypool.SaveKeys(provider, store); err != nil {
		t.Fatalf("SaveKeys: %v", err)
	}
	// Verify the encrypted file exists at the expected path
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath: %v", err)
	}
	encPath := filepath.Join(filepath.Dir(xdgPath), "keys", provider+".enc")
	if _, statErr := os.Stat(encPath); statErr != nil {
		t.Fatalf("encrypted file not found at %s (ConfigDir=%q): %v", encPath, config.ConfigDir, statErr)
	}
	// Verify by reloading
	loaded, loadErr := keypool.LoadKeys(provider)
	if loadErr != nil {
		t.Fatalf("LoadKeys verify after SaveKeys: %v", loadErr)
	}
	if loaded == nil || len(loaded.Keys) != len(entries) {
		t.Fatalf("verify: LoadKeys returned %d keys, want %d (provider=%q dir=%s)",
			len(loaded.Keys), len(entries), provider, config.ConfigDir)
	}
	return provider
}

// runKeyCommand runs a key subcommand with the given os.Args override.
// Returns captured stdout and error.
func runKeyCommand(t *testing.T, osArgs []string) (string, error) {
	t.Helper()
	origArgs := os.Args
	origConfigDir := config.ConfigDir
	os.Args = osArgs
	// Always use testKeyDir so keypool finds the encrypted files written by setupKeyStore.
	// This takes precedence over any AKSWITCH_CONFIG_DIR set by other tests.
	config.ConfigDir = testKeyDir
	t.Cleanup(func() {
		os.Args = origArgs
		config.ConfigDir = origConfigDir
	})

	// Create a pipe to capture stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	oldStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	// Read captured output in background
	done := make(chan string)
	go func() {
		var out strings.Builder
		_, _ = io.Copy(&out, r)
		done <- out.String()
	}()

	subcmd := osArgs[2] // "akswitch", "key", <subcmd>
	// Execute via the command's RunE (skip "akswitch key" prefix)
	var runErr error
	switch subcmd {
	case "restore":
		runErr = keyRestoreCmd.RunE(keyRestoreCmd, osArgs[3:])
	case "purge":
		runErr = keyPurgeCmd.RunE(keyPurgeCmd, osArgs[3:])
	default:
		t.Fatalf("unsupported command: %s", subcmd)
	}

	w.Close()
	out := <-done
	return out, runErr
}

func TestKeyRestoreCmd(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string // returns provider name
		args    []string
		wantErr bool
		wantOut string
	}{
		{
			name: "nonexistent provider",
			setup: func(t *testing.T) string { return "" },
			args:    []string{"akswitch", "key", "restore", "no-such-provider", "0"},
			wantErr: true,
		},
		{
			name: "not deleted",
			setup: func(t *testing.T) string {
				return setupKeyStore(t, []keypool.KeyEntry{
					{Key: "sk-111", Name: "active-key"},
					{Key: "sk-222", Name: "another-key"},
				}, -1)
			},
			args:    []string{"akswitch", "key", "restore", "TEST", "0"},
			wantErr: true,
		},
		{
			name: "restore deleted key",
			setup: func(t *testing.T) string {
				return setupKeyStore(t, []keypool.KeyEntry{
					{Key: "sk-111", Name: "keep-key"},
					{Key: "sk-222", Name: "deleted-key"},
					{Key: "sk-333", Name: "also-keep"},
				}, 1) // soft-delete index 1
			},
			args:    []string{"akswitch", "key", "restore", "TEST", "1"},
			wantErr: false,
			wantOut: "Restored key [1]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := tt.setup(t)
			if provider != "" {
				// Replace placeholder provider name
				for i, a := range tt.args {
					if a == "TEST" {
						tt.args[i] = provider
					}
				}
			}

			out, err := runKeyCommand(t, tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v\noutput: %s", err, tt.wantErr, out)
			}
			if !tt.wantErr && !strings.Contains(out, tt.wantOut) {
				t.Errorf("output = %q, want it to contain %q", out, tt.wantOut)
			}
		})
	}
}

func TestKeyPurgeCmd(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		args    []string
		wantErr bool
		wantOut string
	}{
		{
			name: "no deleted keys",
			setup: func(t *testing.T) string {
				return setupKeyStore(t, []keypool.KeyEntry{
					{Key: "sk-111", Name: "key1"},
					{Key: "sk-222", Name: "key2"},
				}, -1)
			},
			args:    []string{"akswitch", "key", "purge", "TEST"},
			wantErr: false,
			wantOut: "No deleted keys to purge",
		},
		{
			name: "purge deleted keys",
			setup: func(t *testing.T) string {
				return setupKeyStore(t, []keypool.KeyEntry{
					{Key: "sk-111", Name: "keep-1"},
					{Key: "sk-222", Name: "purge-me"},
					{Key: "sk-333", Name: "keep-2"},
				}, 1) // soft-delete index 1
			},
			args:    []string{"akswitch", "key", "purge", "TEST"},
			wantErr: false,
			wantOut: "Purged 1 deleted key(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := tt.setup(t)
			if provider != "" {
				for i, a := range tt.args {
					if a == "TEST" {
						tt.args[i] = provider
					}
				}
			}

			out, err := runKeyCommand(t, tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v\noutput: %s", err, tt.wantErr, out)
			}
			if !tt.wantErr && !strings.Contains(out, tt.wantOut) {
				t.Errorf("output = %q, want it to contain %q", out, tt.wantOut)
			}
		})
	}
}
