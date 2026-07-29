//go:build integration

package integration

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

// TestCLI_Root_NoArgs 验证 "akswitch" 无参数的行为。
//
// 当前行为：根命令直接调用 startServer，因无配置而 os.Exit(1) 崩溃。
// 期望行为：显示帮助信息。
// SKIP: 待修复根命令无参数行为后再启用此测试。
func TestCLI_Root_NoArgs(t *testing.T) {
	t.Skip("根命令无参数当前会 os.Exit(1)，需先修复设计缺陷")
	_ = runAkswitch
}

// ── Test: --help ────────────────────────────────
//
// "akswitch --help" 应显示完整的命令列表和用法。
func TestCLI_Root_Help(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	var stdout bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runAkswitch(t, "akswitch", "--help")

	w.Close()
	os.Stdout = oldStdout
	io.Copy(&stdout, r)

	if err != nil {
		t.Fatalf("--help failed: %v", err)
	}

	output := stdout.String()
	assertOutputContains(t, output, []string{
		"Usage:",
		"Available Commands:",
		"start",
		"provider",
		"config",
		"key",
	})
}

// ── Test: version 子命令 ───────────────────────
//
// "akswitch version" 应输出版本信息。
func TestCLI_VersionSubcommand(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	var stdout bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runAkswitch(t, "akswitch", "version")

	w.Close()
	os.Stdout = oldStdout
	io.Copy(&stdout, r)

	if err != nil {
		t.Fatalf("version failed: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "akswitch") {
		t.Errorf("version output should contain 'akswitch', got: %s", output)
	}
}

// ── Test: provider list 格式 ────────────────────
//
// "akswitch provider list" 应以表格形式列出 provider。
func TestCLI_ProviderList_Format(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}

	// Init + add providers
	runAkswitch(t, "akswitch", "config", "init", "-p", xdgPath)
	runAkswitch(t, "akswitch", "provider", "add", "alpha",
		"--target", "https://alpha.test/v1",
		"--port", "9101",
	)
	runAkswitch(t, "akswitch", "provider", "add", "beta",
		"--target", "https://beta.test/v1",
		"--port", "9102",
	)

	var stdout bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runAkswitch(t, "akswitch", "provider", "list")

	w.Close()
	os.Stdout = oldStdout
	io.Copy(&stdout, r)

	if err != nil {
		t.Fatalf("provider list failed: %v", err)
	}

	output := stdout.String()
	assertOutputContains(t, output, []string{
		"Providers (from",
		"NAME",
		"TARGET",
		"PORT",
		"alpha",
		"beta",
	})
}

// ── Test: config view ──────────────────────────
//
// "akswitch config view" 应显示配置详情。
func TestCLI_ConfigView(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}

	runAkswitch(t, "akswitch", "config", "init", "-p", xdgPath)

	var stdout bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runAkswitch(t, "akswitch", "config", "view")

	w.Close()
	os.Stdout = oldStdout
	io.Copy(&stdout, r)

	if err != nil {
		t.Fatalf("config view failed: %v", err)
	}

	output := stdout.String()
	assertOutputContains(t, output, []string{
		"Configuration source:",
		"Port:",
		"Target base URL:",
		"GenAI base URL:",
	})
}

// ── Test: provider add 缺少 --target ────────────
//
// "akswitch provider add <name>" 不带 --target 应报错。
// SKIP: Cobra flag 跨 Execute() 持久化（已知 bug）导致前序测试的
// --target 值残留，无法在进程内测试此路径。
func TestCLI_ProviderAdd_MissingTarget(t *testing.T) {
	t.Skip("Cobra flag 持久化 bug 阻止进程内测试此路径")
	_ = runAkswitch
}

// ── Test: provider add 缺少 --port（第一个 provider）─
//
// 第一个 provider 不带 --port 应报错。
// SKIP: 同上，Cobra flag 持久化 bug。
func TestCLI_ProviderAdd_MissingPort(t *testing.T) {
	t.Skip("Cobra flag 持久化 bug 阻止进程内测试此路径")
	_ = runAkswitch
}

// ── Test: provider remove 不存在的 provider ─────
//
// 移除不存在的 provider 应报错。
func TestCLI_ProviderRemove_NotFound(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "config", "init", "-p", xdgPath)

	err = runAkswitch(t, "akswitch", "provider", "remove", "nonexistent")
	if err == nil {
		t.Fatal("expected error for removing nonexistent provider, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should contain 'not found', got: %v", err)
	}
}

// ── Test: provider default 不存在的 provider ────
//
// 将默认 provider 设为不存在的名称应报错。
func TestCLI_ProviderDefault_NotFound(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "config", "init", "-p", xdgPath)

	err = runAkswitch(t, "akswitch", "provider", "default", "nonexistent")
	if err == nil {
		t.Fatal("expected error for default with nonexistent provider, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should contain 'not found', got: %v", err)
	}
}

// ── Test: provider add 重复 ─────────────────────
//
// 添加重名的 provider 应报错。
func TestCLI_ProviderAdd_Duplicate(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "config", "init", "-p", xdgPath)
	runAkswitch(t, "akswitch", "provider", "add", "dup",
		"--target", "https://dup.com/v1",
		"--port", "9201",
	)

	err = runAkswitch(t, "akswitch", "provider", "add", "dup",
		"--target", "https://dup2.com/v1",
		"--port", "9202",
	)
	if err == nil {
		t.Fatal("expected error for duplicate provider, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should contain 'already exists', got: %v", err)
	}
}

// ── Test: provider add --default ────────────────
//
// "akswitch provider add <name> --default" 应设置 DefaultProvider。
func TestCLI_ProviderAdd_DefaultFlag(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}

	err = runAkswitch(t, "akswitch", "provider", "add", "primary",
		"--target", "https://primary.test/v1",
		"--port", "9501",
		"--default",
	)
	if err != nil {
		t.Fatalf("provider add --default failed: %v", err)
	}

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if tc.DefaultProvider != "primary" {
		t.Errorf("DefaultProvider = %q, want %q", tc.DefaultProvider, "primary")
	}
}

// ── Test: provider default <name> ───────────────
//
// "akswitch provider default <name>" 应设置默认 provider。
func TestCLI_ProviderDefault_SetsDefault(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}

	runAkswitch(t, "akswitch", "provider", "add", "alpha",
		"--target", "https://alpha.test/v1",
		"--port", "9501",
	)
	err = runAkswitch(t, "akswitch", "provider", "default", "alpha")
	if err != nil {
		t.Fatalf("provider default failed: %v", err)
	}

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if tc.DefaultProvider != "alpha" {
		t.Errorf("DefaultProvider = %q, want %q", tc.DefaultProvider, "alpha")
	}
}

// ── Test: provider remove 默认 provider ─────────
//
// 移除当前默认 provider 时，config 中的 default_provider 应被清除。
func TestCLI_ProviderRemove_DefaultProvider(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}

	// 添加并设为默认
	runAkswitch(t, "akswitch", "provider", "add", "primary",
		"--target", "https://primary.test/v1",
		"--port", "9501",
		"--default",
	)

	// 验证默认已设置
	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if tc.DefaultProvider != "primary" {
		t.Fatalf("DefaultProvider should be 'primary', got %q", tc.DefaultProvider)
	}

	// 删除
	runAkswitch(t, "akswitch", "provider", "remove", "primary")

	// 验证 default_provider 已被清除
	tc, err = config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig after remove failed: %v", err)
	}
	if tc.DefaultProvider != "" {
		t.Errorf("DefaultProvider (%q) should have been cleared after removing the default provider", tc.DefaultProvider)
	}
}

// ── Test: 非法子命令 ──────────────────────────
//
// "akswitch nonexistent" 应给出友好提示。
func TestCLI_InvalidCommand(t *testing.T) {
	cli.ResetConfigEnv()

	var stderr bytes.Buffer
	var stdout bytes.Buffer

	// 捕获 stderr
	oldStderr := os.Stderr
	oldStdout := os.Stdout
	rErr, wErr, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	os.Stderr = wErr
	os.Stdout = wOut

	err := runAkswitch(t, "akswitch", "nonexistent")

	wErr.Close()
	wOut.Close()
	os.Stderr = oldStderr
	os.Stdout = oldStdout
	io.Copy(&stderr, rErr)
	io.Copy(&stdout, rOut)

	// 合并非 stdout 和 stderr
	allOutput := stderr.String() + stdout.String()
	_ = err

	if !strings.Contains(allOutput, "unknown command") &&
		!strings.Contains(allOutput, "nonexistent") {
		t.Errorf("output should mention unknown command, got: %s", allOutput)
	}
}


func TestConfigInit_CreatesFile(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.toml"

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"akswitch", "config", "init", "-p", configPath}

	err := cli.Execute("")
	if err != nil {
		t.Fatalf("config init failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config.toml was not created")
	}

	// Verify file is loadable as valid TOML config
	providersMap, err := config.LoadAllTomlProviders(configPath)
	if err != nil {
		t.Fatalf("created config.toml is not valid: %v", err)
	}
	cfg, ok := providersMap["example-a"]
	if !ok {
		t.Fatal("example-a provider not found in config")
	}
	// Generated config should have example placeholder providers
	if cfg.TargetBase != "https://api.example-a.com/v1" {
		t.Errorf("TargetBase should be set to example-a target, got %q", cfg.TargetBase)
	}
}

// TestConfigView_ShowsConfig verifies that "akswitch config view" prints
// the current configuration from config.toml.
func TestConfigView_ShowsConfig(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"akswitch", "config", "init", "-p", xdgPath}
	if err := cli.Execute(""); err != nil {
		t.Fatalf("config init failed: %v", err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	os.Args = []string{"akswitch", "config", "view"}
	err = cli.Execute("")
	w.Close()
	os.Stdout = oldStdout

	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("config view failed: %v", err)
	}

	output := string(out)
	if !strings.Contains(output, "Configuration source:") {
		t.Error("output missing 'Configuration source:'")
	}
	if !strings.Contains(output, "api.example-a.com") {
		t.Errorf("output missing expected URL: %s", output)
	}
}

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


func TestProviderAdd_CreatesProviderEntry(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	// provider add auto-creates config.toml when it doesn't exist
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}

	// Add a provider
	addArgs := []string{"akswitch", "provider", "add", "test-provider",
		"--target", "https://test.api.com/v1",
		"--port", "9999",
		"--genai", "https://test.api.com",
		"--cooldown-sec", "30",
		"--max-retries", "5",
	}
	cli.RunCommand(t, addArgs...)

	// Verify the config file now contains the provider
	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed after add: %v", err)
	}
	p, ok := tc.Provider["test-provider"]
	if !ok {
		t.Fatal("provider 'test-provider' not found in config after add")
	}
	if p.TargetBase != "https://test.api.com/v1" {
		t.Errorf("Target = %q, want %q", p.TargetBase, "https://test.api.com/v1")
	}
	if tc.Port != 9999 {
		t.Errorf("Port = %d, want 9999", tc.Port)
	}
	if p.GenaiBase != "https://test.api.com" {
		t.Errorf("Genai = %q, want %q", p.GenaiBase, "https://test.api.com")
	}
	if p.CooldownSec != 30 {
		t.Errorf("CooldownSec = %d, want 30", p.CooldownSec)
	}
	if p.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", p.MaxRetries)
	}
}

// TestProviderAdd_DuplicateRejected verifies that adding
// a provider with a duplicate name is rejected.
func TestProviderAdd_DuplicateRejected(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)

	// First add succeeds
	cli.RunCommand(t, "akswitch", "provider", "add", "dup-test",
		"--target", "https://test1.com/v1",
		"--port", "9101",
	)

	// Second add should fail
	err = cli.RunCommand(t, "akswitch", "provider", "add", "dup-test",
		"--target", "https://test2.com/v1",
		"--port", "9102",
	)
	if err == nil {
		t.Fatal("expected error for duplicate provider add, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error message = %q, want it to contain 'already exists'", err.Error())
	}
}

// TestProviderList_ShowsProviders verifies that
// "akswitch provider list" correctly lists configured providers.
func TestProviderList_ShowsProviders(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)

	// Add two providers
	cli.RunCommand(t, "akswitch", "provider", "add", "alpha",
		"--target", "https://alpha.test/v1",
		"--port", "9101",
	)
	cli.RunCommand(t, "akswitch", "provider", "add", "beta",
		"--target", "https://beta.test/v1",
		"--port", "9102",
	)

	// Capture list output
	var stdout bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = cli.RunCommand(t, "akswitch", "provider", "list")

	w.Close()
	os.Stdout = oldStdout
	io.Copy(&stdout, r)

	if err != nil {
		t.Fatalf("provider list failed: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "alpha") {
		t.Errorf("output missing 'alpha':\n%s", output)
	}
	if !strings.Contains(output, "beta") {
		t.Errorf("output missing 'beta':\n%s", output)
	}
	if !strings.Contains(output, "https://alpha.test/v1") {
		t.Errorf("output missing alpha target:\n%s", output)
	}
}

// TestProviderRemove_RemovesEntry verifies that
// "akswitch provider remove <name>" correctly removes a provider.
func TestProviderRemove_RemovesEntry(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)
	cli.RunCommand(t, "akswitch", "provider", "add", "remove-me",
		"--target", "https://remove.test/v1",
		"--port", "9201",
	)

	// Remove it
	cli.RunCommand(t, "akswitch", "provider", "remove", "remove-me")

	// Verify it's gone
	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if _, exists := tc.Provider["remove-me"]; exists {
		t.Error("provider 'remove-me' still exists after remove")
	}
}

// TestProviderRemove_NotFound verifies that removing a
// nonexistent provider returns an error.
func TestProviderRemove_NotFound(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)

	err = cli.RunCommand(t, "akswitch", "provider", "remove", "nonexistent")
	if err == nil {
		t.Fatal("expected error for removing nonexistent provider, got nil")
	}
}

// ── Test: provider add --default ─────────────────────────
//
// "akswitch provider add <name> --default" 应将 DefaultProvider 设为该 provider。
func TestProviderAdd_DefaultFlag(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}

	cli.RunCommand(t, "akswitch", "provider", "add", "primary",
		"--target", "https://primary.test/v1",
		"--port", "9501",
		"--default",
	)

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if tc.DefaultProvider != "primary" {
		t.Errorf("DefaultProvider = %q, want %q", tc.DefaultProvider, "primary")
	}
}

// ── Test: provider default <name> ─────────────────────────
//
// "akswitch provider default <name>" 应正确设置 default_provider。
func TestProviderDefault_SetsDefault(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}

	cli.RunCommand(t, "akswitch", "provider", "add", "alpha",
		"--target", "https://alpha.test/v1",
		"--port", "9501",
	)
	cli.RunCommand(t, "akswitch", "provider", "add", "beta",
		"--target", "https://beta.test/v1",
	)

	cli.RunCommand(t, "akswitch", "provider", "default", "beta")

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if tc.DefaultProvider != "beta" {
		t.Errorf("DefaultProvider = %q, want %q", tc.DefaultProvider, "beta")
	}
}

// ── Test: provider default <name> 不存在 ──────────────────
//
// "akswitch provider default <name>" 对不存在的 provider 应报错。
func TestProviderDefault_NotFound(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	cli.RunCommand(t, "akswitch", "config", "init", "-p", xdgPath)

	err = cli.RunCommand(t, "akswitch", "provider", "default", "nonexistent")
	if err == nil {
		t.Fatal("expected error for default with nonexistent provider, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error message = %q, want it to contain 'not found'", err.Error())
	}
}

// ── Test: provider info 格式（离线模式） ────────
//
// "akswitch provider info <name>" 应输出配置详情和 key 概况。
func TestCLI_ProviderInfo_Format(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}

	// Create a minimal config file directly (avoiding config init global state)
	tc := &config.TomlConfig{
		Port: 8080,
		Provider: map[string]*config.Config{
			"alpha": {
				TargetBase:          "https://alpha.test/v1",
				GenaiBase:           "https://alpha.test",
				CooldownSec:         60,
				MaxRetries:          3,
				BackoffCapSec:       120,
				BackoffMultiplier:   2,
				CBResetSec:          30,
				UpstreamCBThreshold: 5,
				HealthCheckIntervalSec: 30,
				HealthCheckPath:       "/health",
				HealthCheckTimeoutSec:  5,
			},
		},
	}
	if err := config.SaveTomlConfig(tc, xdgPath); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Add keys for the provider
	store := &keypool.KeyStore{
		Keys: []keypool.KeyEntry{
			{Key: "sk-abcdefghijklmn", Name: "key-one"},
			{Key: "sk-uvwxyzabcdefgh", Name: "key-two"},
		},
	}
	if err := keypool.SaveKeysInsecure("alpha", store); err != nil {
		t.Fatalf("SaveKeysInsecure failed: %v", err)
	}

	var stdout bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runAkswitch(t, "akswitch", "provider", "info", "alpha")

	w.Close()
	os.Stdout = oldStdout
	io.Copy(&stdout, r)

	if err != nil {
		t.Fatalf("provider info alpha failed: %v", err)
	}

	output := stdout.String()
	assertOutputContains(t, output, []string{
		"Provider: alpha",
		"Config:",
		"Target:  https://alpha.test/v1",
		"Port:    8080",
		"Tuning:",
		"Max retries:        3",
		"Cooldown:           60s",
		"Backoff cap:        120s",
		"Backoff multiplier: 2.0",
		"CB threshold:       5",
		"CB reset:           30s",
		"Health check:",
		"Interval:  30s",
		"Path:      /health",
		"Timeout:   5s",
		"Keys:",
		"Total: 2  Active: 2  Disabled: 0",
		"name: key-one",
		"name: key-two",
	})
}

// ── Test: provider info 不存在的 provider ────────
//
// "akswitch provider info <name>" 对不存在的 provider 应报错。
func TestCLI_ProviderInfo_NotFound(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "config", "init", "-p", xdgPath)

	err = runAkswitch(t, "akswitch", "provider", "info", "nonexistent")
	if err == nil {
		t.Fatal("expected error for info with nonexistent provider, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should contain 'not found', got: %v", err)
	}
}

// ── provider update 集成测试 ─────────────────────

// TestProviderUpdate_Target 验证 "akswitch provider update <name> --target <url>"
// 能正确修改 provider 的 target 并持久化。
func TestProviderUpdate_Target(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "provider", "add", "upd-target",
		"--target", "https://old.example.com/v1",
		"--port", "9601",
	)

	err = runAkswitch(t, "akswitch", "provider", "update", "upd-target",
		"--target", "https://new.example.com/v1")
	if err != nil {
		t.Fatalf("provider update --target failed: %v", err)
	}

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if tc.Provider["upd-target"].TargetBase != "https://new.example.com/v1" {
		t.Errorf("TargetBase = %q, want %q",
			tc.Provider["upd-target"].TargetBase, "https://new.example.com/v1")
	}
}

// TestProviderUpdate_NotFound 验证更新不存在的 provider 返回错误。
func TestProviderUpdate_NotFound(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "config", "init", "-p", xdgPath)

	err = runAkswitch(t, "akswitch", "provider", "update", "no-such-provider",
		"--target", "https://example.com/v1")
	if err == nil {
		t.Fatal("expected error for updating nonexistent provider, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should contain 'not found', got: %v", err)
	}
	// Verify the error mentions the config file path
	if !strings.Contains(err.Error(), xdgPath) {
		t.Errorf("error should mention config file path %q, got: %v", xdgPath, err)
	}
}

// TestProviderUpdate_NoFlags 验证不带任何 flag 时返回错误。
// SKIP: Cobra flag 在进程内 Execute() 调用间持久化（Cobra #1398）。
// TODO: 改用子进程模式后移除此 skip。
func TestProviderUpdate_NoFlags(t *testing.T) {
	t.Skip("Cobra flag 持久化 bug 阻止进程内测试此路径")
	_ = runAkswitch
}

// TestProviderUpdate_MultipleFlags 验证多个 flag 可组合修改多个字段。
func TestProviderUpdate_MultipleFlags(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "provider", "add", "multi-flags",
		"--target", "https://old.test/v1",
		"--genai", "https://old-genai.test",
		"--cooldown-sec", "10",
		"--max-retries", "1",
		"--port", "9603",
	)

	err = runAkswitch(t, "akswitch", "provider", "update", "multi-flags",
		"--target", "https://new.test/v1",
		"--cooldown-sec", "99",
		"--max-retries", "7",
		"--backoff-cap-sec", "200",
	)
	if err != nil {
		t.Fatalf("provider update with multiple flags failed: %v", err)
	}

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	p := tc.Provider["multi-flags"]
	if p.TargetBase != "https://new.test/v1" {
		t.Errorf("TargetBase = %q, want %q", p.TargetBase, "https://new.test/v1")
	}
	if p.CooldownSec != 99 {
		t.Errorf("CooldownSec = %d, want 99", p.CooldownSec)
	}
	if p.MaxRetries != 7 {
		t.Errorf("MaxRetries = %d, want 7", p.MaxRetries)
	}
	if p.BackoffCapSec != 200 {
		t.Errorf("BackoffCapSec = %d, want 200", p.BackoffCapSec)
	}
	// Fields not in the update command should be unchanged
	if p.GenaiBase != "https://old-genai.test" {
		t.Errorf("GenaiBase should be unchanged, got: %q", p.GenaiBase)
	}
}

// TestProviderUpdate_DefaultFlag 验证 --default flag 设置默认 provider。
func TestProviderUpdate_DefaultFlag(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "provider", "add", "default-candidate",
		"--target", "https://dc.test/v1",
		"--port", "9604",
	)

	err = runAkswitch(t, "akswitch", "provider", "update", "default-candidate", "--default")
	if err != nil {
		t.Fatalf("provider update --default failed: %v", err)
	}

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if tc.DefaultProvider != "default-candidate" {
		t.Errorf("DefaultProvider = %q, want %q", tc.DefaultProvider, "default-candidate")
	}
}

// TestProviderUpdate_OutputNotMarkedDefault 验证更新已为 default 的 provider
//（不传 --default）时，输出不含 (default) 标记。
func TestProviderUpdate_OutputNotMarkedDefault(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	runAkswitch(t, "akswitch", "provider", "add", "the-default",
		"--target", "https://old.test/v1",
		"--port", "9608",
		"--default",
	)

	var stdout bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runAkswitch(t, "akswitch", "provider", "update", "the-default",
		"--target", "https://new.test/v1")

	w.Close()
	os.Stdout = oldStdout
	io.Copy(&stdout, r)

	if err != nil {
		t.Fatalf("provider update failed: %v", err)
	}
	output := stdout.String()
	if strings.Contains(output, "(default)") {
		t.Errorf("output should not contain (default) when --default was not passed, got:\n%s", output)
	}
	if !strings.Contains(output, "updated") {
		t.Errorf("output should contain 'updated', got:\n%s", output)
	}
}

// TestProviderUpdate_AdminToken verifies --admin-token flag sets and clears admin token.
func TestProviderUpdate_AdminToken(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "provider", "add", "admintoken",
		"--target", "https://admintoken.test/v1",
		"--port", "9605",
	)

	// Set admin token
	err = runAkswitch(t, "akswitch", "provider", "update", "admintoken",
		"--admin-token", "secret123")
	if err != nil {
		t.Fatalf("provider update --admin-token set failed: %v", err)
	}

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if tc.Provider["admintoken"].AdminToken != "secret123" {
		t.Errorf("AdminToken = %q, want %q", tc.Provider["admintoken"].AdminToken, "secret123")
	}

	// Clear admin token
	err = runAkswitch(t, "akswitch", "provider", "update", "admintoken",
		"--admin-token", "")
	if err != nil {
		t.Fatalf("provider update --admin-token clear failed: %v", err)
	}

	tc, err = config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if tc.Provider["admintoken"].AdminToken != "" {
		t.Errorf("AdminToken = %q, want empty after clear", tc.Provider["admintoken"].AdminToken)
	}
}

// TestProviderUpdate_FloatFlag verifies --backoff-multiplier float flag.
func TestProviderUpdate_FloatFlag(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "provider", "add", "floatflag",
		"--target", "https://float.test/v1",
		"--port", "9606",
	)

	err = runAkswitch(t, "akswitch", "provider", "update", "floatflag",
		"--backoff-multiplier", "3.5")
	if err != nil {
		t.Fatalf("provider update --backoff-multiplier failed: %v", err)
	}

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if tc.Provider["floatflag"].BackoffMultiplier != 3.5 {
		t.Errorf("BackoffMultiplier = %f, want 3.5", tc.Provider["floatflag"].BackoffMultiplier)
	}
}

// TestProviderUpdate_DisableThinking verifies --disable-thinking bool flag.
func TestProviderUpdate_DisableThinking(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "provider", "add", "think",
		"--target", "https://think.test/v1",
		"--port", "9607",
	)

	// Set disable-thinking to true
	err = runAkswitch(t, "akswitch", "provider", "update", "think", "--disable-thinking")
	if err != nil {
		t.Fatalf("provider update --disable-thinking failed: %v", err)
	}

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	if !tc.Provider["think"].DisableThinking {
		t.Error("DisableThinking should be true")
	}
}

// TestProviderUpdate_ZeroValues verifies that setting int/float fields to zero
// persists correctly (regression test for omitempty stripping zero values).
func TestProviderUpdate_ZeroValues(t *testing.T) {
	cli.ResetConfigEnv()
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	runAkswitch(t, "akswitch", "provider", "add", "zerovals",
		"--target", "https://zerovals.test/v1",
		"--port", "9608",
		"--cooldown-sec", "15",
		"--max-retries", "3",
	)

	// Set all int/float fields to zero
	err = runAkswitch(t, "akswitch", "provider", "update", "zerovals",
		"--cooldown-sec", "0",
		"--max-retries", "0",
		"--backoff-cap-sec", "0",
		"--backoff-multiplier", "0",
	)
	if err != nil {
		t.Fatalf("provider update zero values failed: %v", err)
	}

	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		t.Fatalf("LoadTomlConfig failed: %v", err)
	}
	p := tc.Provider["zerovals"]
	if p.CooldownSec != 0 {
		t.Errorf("CooldownSec = %d, want 0", p.CooldownSec)
	}
	if p.MaxRetries != 0 {
		t.Errorf("MaxRetries = %d, want 0", p.MaxRetries)
	}
	if p.BackoffCapSec != 0 {
		t.Errorf("BackoffCapSec = %d, want 0", p.BackoffCapSec)
	}
	if p.BackoffMultiplier != 0 {
		t.Errorf("BackoffMultiplier = %f, want 0", p.BackoffMultiplier)
	}
}
