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
