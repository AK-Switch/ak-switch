//go:build integration

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"akswitch/internal/cli"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
)

// ── Key CRUD Acceptance Tests ─────────────────────────

// TestKeyAdd_AddsKey verifies that "akswitch key add <provider> <key>"
// adds a key to the provider's encrypted key store.
func TestKeyAdd_AddsKey(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	// Init config and add a provider
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	keypool.RemoveKeys("keytest")
	cli.RunCommand(t, "akswitch", "provider", "add", "keytest",
		"--target", "https://keytest.api.com/v1",
		"--port", "9501",
	)

	// Add a key
	cli.RunCommand(t, "akswitch", "key", "add", "keytest", "sk-test-key-12345")

	// Verify key was added via keyring
	store, err := keypool.LoadKeys("keytest")
	if err != nil {
		t.Fatalf("LoadKeys failed: %v", err)
	}
	if store == nil || len(store.Keys) == 0 {
		t.Fatal("no keys found in store after add")
	}
	if store.Keys[0].Key != "sk-test-key-12345" {
		t.Errorf("Key = %q, want %q", store.Keys[0].Key, "sk-test-key-12345")
	}
}

// TestKeyList_ShowsKeys verifies that "akswitch key list <provider>"
// displays the correct key information.
func TestKeyList_ShowsKeys(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	keypool.RemoveKeys("listtest")
	cli.RunCommand(t, "akswitch", "provider", "add", "listtest",
		"--target", "https://listtest.api.com/v1",
		"--port", "9502",
	)

	// Add two keys
	cli.RunCommand(t, "akswitch", "key", "add", "listtest", "sk-list-key-aaaa")
	cli.RunCommand(t, "akswitch", "key", "add", "listtest", "sk-list-key-bbbb")

	// Capture list output
	var stdout bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cli.RunCommand(t, "akswitch", "key", "list", "listtest")

	w.Close()
	os.Stdout = oldStdout
	io.Copy(&stdout, r)

	output := stdout.String()
	if !strings.Contains(output, "listtest") {
		t.Errorf("output missing provider name:\n%s", output)
	}
	if !strings.Contains(output, "...") {
		t.Errorf("output missing masked key:\n%s", output)
	}
	if !strings.Contains(output, "active") {
		t.Errorf("output missing key status:\n%s", output)
	}
}

// TestKeyRemove_RemovesKey verifies that "akswitch key remove <provider> <index>"
// removes the key at the given index.
func TestKeyRemove_RemovesKey(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	keypool.RemoveKeys("removetest")
	cli.RunCommand(t, "akswitch", "provider", "add", "removetest",
		"--target", "https://removetest.api.com/v1",
		"--port", "9503",
	)

	// Add two keys, then remove the first
	cli.RunCommand(t, "akswitch", "key", "add", "removetest", "sk-remove-key-1")
	cli.RunCommand(t, "akswitch", "key", "add", "removetest", "sk-remove-key-2")
	cli.RunCommand(t, "akswitch", "key", "remove", "removetest", "0")

	// Verify key[0] was removed (should now be "sk-remove-key-2")
	store, err := keypool.LoadKeys("removetest")
	if err != nil {
		t.Fatalf("LoadKeys failed: %v", err)
	}
	if len(store.Keys) != 1 {
		t.Fatalf("expected 1 key after remove, got %d", len(store.Keys))
	}
	if store.Keys[0].Key != "sk-remove-key-2" {
		t.Errorf("remaining key = %q, want %q", store.Keys[0].Key, "sk-remove-key-2")
	}
}

// TestKeyDisable_DisablesKey verifies that "akswitch key disable <provider> <index>"
// marks the key as disabled.
func TestKeyDisable_DisablesKey(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	keypool.RemoveKeys("disabletest")
	cli.RunCommand(t, "akswitch", "provider", "add", "disabletest",
		"--target", "https://disabletest.api.com/v1",
		"--port", "9504",
	)

	// Add a key and disable it
	cli.RunCommand(t, "akswitch", "key", "add", "disabletest", "sk-disable-key-1")
	cli.RunCommand(t, "akswitch", "key", "disable", "disabletest", "0")

	// Verify key is disabled
	store, err := keypool.LoadKeys("disabletest")
	if err != nil {
		t.Fatalf("LoadKeys failed: %v", err)
	}
	if len(store.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(store.Keys))
	}
	if !store.Keys[0].Disabled {
		t.Error("key should be disabled but Disabled = false")
	}
}

// TestKeyEnable_EnablesKey verifies that "akswitch key enable <provider> <index>"
// re-enables a previously disabled key.
func TestKeyEnable_EnablesKey(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	keypool.RemoveKeys("enabletest")
	cli.RunCommand(t, "akswitch", "provider", "add", "enabletest",
		"--target", "https://enabletest.api.com/v1",
		"--port", "9506",
	)

	// Add a key, disable it, then enable it
	cli.RunCommand(t, "akswitch", "key", "add", "enabletest", "sk-enable-key-1")
	cli.RunCommand(t, "akswitch", "key", "disable", "enabletest", "0")
	cli.RunCommand(t, "akswitch", "key", "enable", "enabletest", "0")

	// Verify key is enabled again
	store, err := keypool.LoadKeys("enabletest")
	if err != nil {
		t.Fatalf("LoadKeys failed: %v", err)
	}
	if len(store.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(store.Keys))
	}
	if store.Keys[0].Disabled {
		t.Error("key should be enabled but Disabled = true")
	}
}

// TestKeyEnable_InvalidIndex verifies that enabling with an out-of-range
// index returns an error.
func TestKeyEnable_InvalidIndex(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	keypool.RemoveKeys("enableerrtest")
	cli.RunCommand(t, "akswitch", "provider", "add", "enableerrtest",
		"--target", "https://enableerrtest.api.com/v1",
		"--port", "9507",
	)
	cli.RunCommand(t, "akswitch", "key", "add", "enableerrtest", "sk-enable-err-key-1")

	err = cli.RunCommand(t, "akswitch", "key", "enable", "enableerrtest", "999")
	if err == nil {
		t.Fatal("expected error for out-of-range index, got nil")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("error message = %q, want it to contain 'out of range'", err.Error())
	}
}

// TestKeyRemove_InvalidIndex verifies that removing with an out-of-range
// index returns an error.
func TestKeyRemove_InvalidIndex(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	keypool.RemoveKeys("errtest")
	cli.RunCommand(t, "akswitch", "provider", "add", "errtest",
		"--target", "https://errtest.api.com/v1",
		"--port", "9505",
	)
	cli.RunCommand(t, "akswitch", "key", "add", "errtest", "sk-err-key-1")

	err = cli.RunCommand(t, "akswitch", "key", "remove", "errtest", "999")
	if err == nil {
		t.Fatal("expected error for out-of-range index, got nil")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("error message = %q, want it to contain 'out of range'", err.Error())
	}
}

// TestKeyAdd_InsecureStorage verifies that "akswitch key add <provider> <key> --insecure-storage"
// stores the key as plaintext JSON and prints a warning.
func TestKeyAdd_InsecureStorage(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	cli.RunCommand(t, "akswitch", "provider", "add", "insecurtest",
		"--target", "https://insecurtest.api.com/v1",
		"--port", "9510",
	)

	// Capture stderr and stdout
	var stderrBuf, stdoutBuf bytes.Buffer
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	cli.RunCommand(t, "akswitch", "key", "add", "insecurtest", "sk-insecure-test-key", "--insecure-storage")

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	io.Copy(&stdoutBuf, rOut)
	io.Copy(&stderrBuf, rErr)

	stderr := stderrBuf.String()
	if !strings.Contains(stderr, "WARNING") {
		t.Errorf("stderr missing warning:\n%s", stderr)
	}
	if !strings.Contains(stderr, "plaintext") {
		t.Errorf("stderr missing 'plaintext':\n%s", stderr)
	}

	// Verify key is stored (LoadKeys falls back to insecure file)
	store, err := keypool.LoadKeys("insecurtest")
	if err != nil {
		t.Fatalf("LoadKeys failed: %v", err)
	}
	if store == nil || len(store.Keys) == 0 {
		t.Fatal("no keys found after insecure add")
	}
	if store.Keys[0].Key != "sk-insecure-test-key" {
		t.Errorf("Key = %q, want %q", store.Keys[0].Key, "sk-insecure-test-key")
	}
}
// ── Key Import Acceptance Tests ─────────────────────────

// TestKeyImport_FromArgs verifies that "akswitch key import <provider> <key1> <key2>"
// imports multiple keys from command line arguments.
func TestKeyImport_FromArgs(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	keypool.RemoveKeys("importtest")
	cli.RunCommand(t, "akswitch", "provider", "add", "importtest",
		"--target", "https://importtest.api.com/v1",
		"--port", "9520",
	)

	// Import three keys from args
	cli.RunCommand(t, "akswitch", "key", "import", "importtest", "sk-import-1", "sk-import-2", "sk-import-3")

	// Verify all keys were imported
	store, err := keypool.LoadKeys("importtest")
	if err != nil {
		t.Fatalf("LoadKeys failed: %v", err)
	}
	if store == nil || len(store.Keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(store.Keys))
	}
	if store.Keys[0].Key != "sk-import-1" || store.Keys[1].Key != "sk-import-2" || store.Keys[2].Key != "sk-import-3" {
		t.Errorf("keys mismatch: %+v", store.Keys)
	}
}

// TestKeyImport_FromFile verifies that "akswitch key import <provider> --file <path>"
// imports keys from a JSON file.
func TestKeyImport_FromFile(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	keypool.RemoveKeys("fileimporttest")
	cli.RunCommand(t, "akswitch", "provider", "add", "fileimporttest",
		"--target", "https://fileimporttest.api.com/v1",
		"--port", "9521",
	)

	// Write a JSON file with keys
	keysJSON := []byte(`["sk-file-1", "sk-file-2", "sk-file-3"]`)
	keysFile := tmpDir + "/keys.json"
	if err := os.WriteFile(keysFile, keysJSON, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Import from file
	cli.RunCommand(t, "akswitch", "key", "import", "fileimporttest", "--file", keysFile)

	// Verify all keys were imported
	store, err := keypool.LoadKeys("fileimporttest")
	if err != nil {
		t.Fatalf("LoadKeys failed: %v", err)
	}
	if store == nil || len(store.Keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(store.Keys))
	}
	if store.Keys[0].Key != "sk-file-1" || store.Keys[1].Key != "sk-file-2" || store.Keys[2].Key != "sk-file-3" {
		t.Errorf("keys mismatch: %+v", store.Keys)
	}
}

// TestKeyImport_FromFileWithObjects verifies that the JSON format with key objects works.
func TestKeyImport_FromFileWithObjects(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	keypool.RemoveKeys("objimporttest")
	cli.RunCommand(t, "akswitch", "provider", "add", "objimporttest",
		"--target", "https://objimporttest.api.com/v1",
		"--port", "9522",
	)

	// Write a JSON file with key objects (with names)
	keysJSON := []byte(`[{"key": "sk-obj-1", "name": "key-one"}, {"key": "sk-obj-2", "name": "key-two"}]`)
	keysFile := tmpDir + "/keys.json"
	if err := os.WriteFile(keysFile, keysJSON, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Import from file
	cli.RunCommand(t, "akswitch", "key", "import", "objimporttest", "--file", keysFile)

	// Verify keys were imported with names
	store, err := keypool.LoadKeys("objimporttest")
	if err != nil {
		t.Fatalf("LoadKeys failed: %v", err)
	}
	if store == nil || len(store.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(store.Keys))
	}
	// Build a lookup by key to avoid order dependency from LoadKeys
	got := make(map[string]string)
	for _, entry := range store.Keys {
		got[entry.Key] = entry.Name
	}
	if got["sk-obj-1"] != "key-one" {
		t.Errorf("key sk-obj-1: expected name %q, got %q", "key-one", got["sk-obj-1"])
	}
	if got["sk-obj-2"] != "key-two" {
		t.Errorf("key sk-obj-2: expected name %q, got %q", "key-two", got["sk-obj-2"])
	}
}

// TestKeyImport_EmptyInput verifies that importing with no keys returns an error.
func TestKeyImport_EmptyInput(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	keypool.RemoveKeys("emptyimporttest")
	cli.RunCommand(t, "akswitch", "provider", "add", "emptyimporttest",
		"--target", "https://emptyimporttest.api.com/v1",
		"--port", "9523",
	)

	// Write an empty JSON array to a file
	keysJSON := []byte(`[]`)
	keysFile := tmpDir + "/empty.json"
	if err := os.WriteFile(keysFile, keysJSON, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Import empty file should succeed but add no keys
	cli.RunCommand(t, "akswitch", "key", "import", "emptyimporttest", "--file", keysFile)

	store, err := keypool.LoadKeys("emptyimporttest")
	if err != nil {
		t.Fatalf("LoadKeys failed: %v", err)
	}
	if store != nil && len(store.Keys) > 0 {
		t.Errorf("expected no keys, got %d", len(store.Keys))
	}
}

