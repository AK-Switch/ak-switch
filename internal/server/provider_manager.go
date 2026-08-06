package server

import (
	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// ProviderManager manages the lifecycle and state of all providers.
type ProviderManager struct {
	mu            sync.RWMutex
	providers     map[string]*ProviderState
	startTime     time.Time
	dashboardHTML string
}

// NewProviderManager creates a new ProviderManager.
func NewProviderManager(dashboardHTML string) *ProviderManager {
	return &ProviderManager{
		providers:     make(map[string]*ProviderState),
		startTime:     time.Now(),
		dashboardHTML: dashboardHTML,
	}
}

// AddProvider registers a new provider.
func (pm *ProviderManager) AddProvider(name string, cfg *config.Config, pool *keypool.KeyPool) error {
	ps := NewProviderState(name, cfg, pool, pm.dashboardHTML, cfg.KeysFile)
	pm.mu.Lock()
	pm.providers[name] = ps
	pm.mu.Unlock()
	return nil
}

// Provider returns the ProviderState with the given name.
func (pm *ProviderManager) Provider(name string) *ProviderState {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.providers[name]
}

// ProviderNames returns sorted provider names.
func (pm *ProviderManager) ProviderNames() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	names := make([]string, 0, len(pm.providers))
	for name := range pm.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LookupProvider returns a provider by name.
func (pm *ProviderManager) LookupProvider(name string) *ProviderState {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.providers[name]
}

// FirstProvider returns the first (alphabetically) provider.
func (pm *ProviderManager) FirstProvider() *ProviderState {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, ps := range pm.providers {
		return ps
	}
	return nil
}

// ForEach iterates over all providers.
func (pm *ProviderManager) ForEach(fn func(name string, ps *ProviderState)) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for name, ps := range pm.providers {
		fn(name, ps)
	}
}

// ReloadConfig updates providers from new config, preserving disabled state.
func (pm *ProviderManager) ReloadConfig(providers map[string]*config.Config, logManager *LogManager) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for name, cfg := range providers {
		keys, keyNames := loadKeysFromConfig(name, cfg)
		if len(keys) > 0 {
			cfg.Keys = keys
			cfg.KeyNames = keyNames
		}

		if existing, ok := pm.providers[name]; ok {
			oldPool := existing.pool
			var disabledNames []string
			for i := 0; i < oldPool.Len(); i++ {
				if oldPool.IsDisabled(i) {
					n, _ := oldPool.Name(i)
					disabledNames = append(disabledNames, n)
				}
			}
			existing.config = cfg
			existing.pool = keypool.NewKeyPool(cfg.Keys, cfg.KeyNames)
			existing.ConfigurePoolCBs(
				time.Duration(cfg.CooldownSec)*time.Second,
				time.Duration(cfg.BackoffCapSec)*time.Second,
				cfg.BackoffMultiplier,
			)
			for _, dn := range disabledNames {
				for i := 0; i < existing.pool.Len(); i++ {
					n, _ := existing.pool.Name(i)
					if n == dn {
						_ = existing.pool.Disable(i)
					}
				}
			}
			logManager.ApplyLevel(cfg.LogLevel)
		} else {
			pool := keypool.NewKeyPool(cfg.Keys, cfg.KeyNames)
			ps := NewProviderState(name, cfg, pool, pm.dashboardHTML, cfg.KeysFile)
			logManager.ApplyLevel(cfg.LogLevel)
			pm.providers[name] = ps
		}
	}
	slog.Info("config reloaded", "providers", len(pm.providers))
}
