//go:build unit

package server

import (
	"testing"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
)

func TestFirstProvider_ReturnsAlphabeticallyFirst(t *testing.T) {
	pm := NewProviderManager("")
	pm.AddProvider("zebra", config.DefaultConfig(), keypool.NewKeyPool([]string{"key-z"}, nil))
	pm.AddProvider("alpha", config.DefaultConfig(), keypool.NewKeyPool([]string{"key-a"}, nil))
	pm.AddProvider("middle", config.DefaultConfig(), keypool.NewKeyPool([]string{"key-m"}, nil))

	ps := pm.FirstProvider()
	if ps == nil {
		t.Fatal("FirstProvider() returned nil, expected alpha")
	}
	if ps.Name() != "alpha" {
		t.Errorf("FirstProvider() = %q, want alpha", ps.Name())
	}
}

func TestFirstProvider_EmptyManager(t *testing.T) {
	pm := NewProviderManager("")
	if pm.FirstProvider() != nil {
		t.Error("FirstProvider() should return nil for empty manager")
	}
}

func TestReloadConfigPreservesDisabledState(t *testing.T) {
	dir := t.TempDir()
	config.ConfigDir = dir
	defer func() { config.ConfigDir = "" }()

	pm := NewProviderManager("")
	cfg := config.DefaultConfig()
	cfg.Keys = []string{"ak-test-key"}
	cfg.KeyNames = []string{"test-key"}
	pool := keypool.NewKeyPool(cfg.Keys, cfg.KeyNames)
	pm.AddProvider("test", cfg, pool)

	// 禁用 key
	if err := pool.Disable(0); err != nil {
		t.Fatalf("Disable failed: %v", err)
	}
	if !pool.IsDisabled(0) {
		t.Fatal("key should be disabled after Disable")
	}

	// 持久化 disabled 状态到 insecure 文件（供 LoadDisabledNames 读取）
	store := &keypool.KeyStore{
		Keys: []keypool.KeyEntry{
			{Key: "ak-test-key", Name: "test-key", Disabled: true},
		},
	}
	if err := keypool.SaveKeysInsecure("test", store); err != nil {
		t.Fatalf("SaveKeysInsecure failed: %v", err)
	}

	// 模拟 ReloadConfig（相同配置）
	providers := map[string]*config.Config{"test": cfg}
	pm.ReloadConfig(providers, NewLogManager())

	// 验证 key 仍为 disabled
	ps := pm.Provider("test")
	if ps == nil {
		t.Fatal("provider should exist after ReloadConfig")
	}
	if !ps.PoolIsDisabled(0) {
		t.Error("key should still be disabled after ReloadConfig")
	}
}
