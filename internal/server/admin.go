package server

import (
	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"fmt"
	"strconv"
)

// ── Package-level helpers (used by admin_api.go) ────────

// toInt converts a JSON-decoded value to int.
func toInt(v interface{}) (int, error) {
	switch val := v.(type) {
	case float64:
		return int(val), nil
	case int:
		return val, nil
	case string:
		return strconv.Atoi(val)
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

// toFloat64 converts a JSON-decoded value to float64.
func toFloat64(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

// loadKeysFromConfig loads API keys for a provider from the configured keys file
// or the standard encrypted store. Returns nil if no keys can be loaded.
func loadKeysFromConfig(name string, cfg *config.Config) (keys, names []string) {
	keys, names, loaded := keypool.LoadKeysFromStore(name, cfg)
	if !loaded {
		return nil, nil
	}
	return keys, names
}
