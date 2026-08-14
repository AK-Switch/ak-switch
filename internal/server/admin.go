package server

import (
	"akswitch/internal/config"
	"akswitch/internal/keypool"
)

// loadKeysFromConfig loads API keys for a provider from the configured keys file
// or the standard encrypted store. Returns nil if no keys can be loaded.
func loadKeysFromConfig(name string, cfg *config.Config) (keys, names []string) {
	keys, names, loaded := keypool.LoadKeysFromStore(name, cfg)
	if !loaded {
		return nil, nil
	}
	return keys, names
}
