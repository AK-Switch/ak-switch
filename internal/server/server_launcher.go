// Package server provides the HTTP server, proxy, and management handlers for AK Switch.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
)

// RestartController abstracts self-restart behavior so the server package
// does not import the cli package (avoiding a circular dependency).
// The cli package provides the production implementation; nil means
// self-restart is disabled.
type RestartController interface {
	// Setup starts the binary-watch goroutine. Called before server start.
	Setup(exePath string, sigCh chan os.Signal)
	// ShouldRestart reports whether a restart is warranted (e.g., binary
	// was updated). Queried after graceful shutdown, before Execute.
	ShouldRestart() bool
	// Execute performs the actual restart after graceful shutdown.
	Execute()
}

// ServerLauncher holds the parameters for starting the proxy server
// and orchestrates the full startup lifecycle.
type ServerLauncher struct {
	dashboardHTML  string
	providerFilter string
	startAll       bool
	logFormat      string
	logLevel       string
	devMode        bool
	restartCtrl    RestartController
}

// NewServerLauncher creates a ServerLauncher with the given parameters.
// All parameters are primitive types (string, bool) with no Cobra dependency.
func NewServerLauncher(dashboardHTML, providerFilter, logFormat, logLevel string, startAll, devMode bool) *ServerLauncher {
	return &ServerLauncher{
		dashboardHTML:  dashboardHTML,
		providerFilter: providerFilter,
		startAll:       startAll,
		logFormat:      logFormat,
		logLevel:       logLevel,
		devMode:        devMode,
	}
}

// SetRestartController attaches a RestartController for dev-mode self-restart.
// Pass nil to disable self-restart (production mode).
func (sl *ServerLauncher) SetRestartController(rc RestartController) {
	sl.restartCtrl = rc
}

// Launch orchestrates the full server startup sequence:
//  1. PID file pre-check
//  2. Resolve providers from config
//  3. Dev mode: find available port
//  4. Initialize file logging
//  5. Apply log-level override
//  6. Initialize each provider
//  7. Start the HTTP server
//  8. Write PID file
//  9. Background tasks
//  10. Wait for shutdown signal
func (sl *ServerLauncher) Launch() error {
	// Crash recovery
	defer CrashRecover("Launch")

	// PID pre-check
	if running, pid := sl.checkPidFile(sl.PidFilePath(sl.devMode)); running {
		slog.Error("akswitch is already running", "pid", pid)
		return nil
	}

	// Resolve providers, config, and selection strategy
	router, providers, port, host, shouldStart, err := sl.resolveProviders()
	if err != nil {
		return err
	}

	// Dev mode: find available port
	var devListener net.Listener
	if sl.devMode {
		for i := 0; i < 10; i++ {
			addr := fmt.Sprintf("%s:%d", host, port+i)
			ln, err := net.Listen("tcp", addr)
			if err == nil {
				devListener = ln
				port += i
				break
			}
		}
		if devListener == nil {
			return fmt.Errorf("dev mode: no available port found (tried 10 ports)")
		}
		fmt.Printf("🚧 Dev mode on port %d\n", port)
	}

	// Initialize file logging (from first provider)
	for _, cfg := range providers {
		router.LogManager().InitFileHandler(cfg.LogFile, cfg.LogMaxSize, cfg.LogMaxAge)
		break
	}

	// Apply log-level override before any provider init logging
	if sl.logLevel != "" {
		router.LogManager().ApplyLevel(sl.logLevel)
	}

	// Initialize each provider
	if err := sl.initProviders(router, providers, shouldStart); err != nil {
		return err
	}

	// Start server
	started := len(router.ProviderNames())
	if started == 0 {
		return fmt.Errorf("no providers configured, exiting")
	}
	var startErr error
	if sl.devMode {
		startErr = router.StartWithListener(devListener)
	} else {
		startErr = router.Start(host, port)
	}
	if startErr != nil {
		return fmt.Errorf("failed to start server: %w", startErr)
	}

	// Write PID file
	pidPath, err := sl.writePIDFile()
	if err != nil {
		slog.Warn("failed to write PID file", "error", err)
	}
	defer func() {
		if pidPath != "" {
			if err := os.Remove(pidPath); err != nil && !os.IsNotExist(err) {
				slog.Warn("failed to remove PID file", "error", err)
			}
		}
	}()

	// Background tasks
	router.StartBackgroundTasks()

	// Wait for shutdown
	return sl.waitForShutdown(router)
}

// resolveProviders handles config detection, TOML loading, and the four-choice
// provider selection strategy (--provider > --all > default_provider > first alphabetically).
func (sl *ServerLauncher) resolveProviders() (router *ProviderRouter, providers map[string]*config.Config, port int, host string, shouldStart func(name string) bool, err error) {
	// Detect config source
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		slog.Error("config detection failed", "error", err)
		return nil, nil, 0, "", nil, fmt.Errorf("config detection failed: %w", err)
	}

	// Load providers from TOML
	providers, err = config.LoadAllTomlProviders(xdgPath)
	if err != nil {
		slog.Error("failed to load providers from TOML", "error", err)
		return nil, nil, 0, "", nil, fmt.Errorf("failed to load providers from TOML: %w", err)
	}
	if len(providers) == 0 {
		slog.Error("no providers found in TOML config")
		return nil, nil, 0, "", nil, fmt.Errorf("no providers found in TOML config")
	}

	// Create ProviderRouter
	router = NewProviderRouter(sl.dashboardHTML)
	port = config.FindServerPort(xdgPath)
	host = config.FindServerHost(xdgPath)

	// 四选一：--provider > --all > default_provider > 第一个 provider（字母序）
	switch {
	case sl.providerFilter != "":
		shouldStart = func(name string) bool { return name == sl.providerFilter }
	case sl.startAll:
		shouldStart = func(name string) bool { return true }
	case config.DefaultProviderName != "":
		shouldStart = func(name string) bool { return name == config.DefaultProviderName }
	default:
		names := slices.Sorted(maps.Keys(providers))
		first := names[0]
		slog.Info("default_provider 未配置，默认使用第一个 provider", "provider", first)
		shouldStart = func(name string) bool { return name == first }
	}
	return router, providers, port, host, shouldStart, nil
}

// initProviders registers each selected provider into the router,
// loading their API keys and applying log level configuration.
func (sl *ServerLauncher) initProviders(router *ProviderRouter, providers map[string]*config.Config, shouldStart func(name string) bool) error {
	// Determine if only one provider is active (hide provider name in compact mode)
	startedCount := 0
	for name := range providers {
		if shouldStart(name) {
			startedCount++
		}
	}
	logCompact := sl.logFormat == "compact"
	router.LogManager().SetFormat(logCompact, startedCount <= 1)

	for name, cfg := range providers {
		if !shouldStart(name) {
			slog.Debug("skipping provider", "name", name)
			continue
		}

		router.LogManager().ApplyLevel(cfg.LogLevel)

		// Load API keys from encrypted store or env
		keys, keyNames := sl.loadKeysForProvider(name, cfg)
		cfg.Keys = keys
		cfg.KeyNames = keyNames

		if err := cfg.Validate(); err != nil {
			slog.Error("invalid provider config", "provider", name, "error", err)
			continue
		}
		pool := keypool.NewKeyPool(keys, keyNames)
		if err := router.AddProvider(name, cfg, pool); err != nil {
			slog.Error("failed to add provider", "provider", name, "error", err)
			continue
		}

		// Restore permanently disabled keys from persisted store
		for _, dn := range keypool.LoadDisabledNames(name, cfg) {
			for i := 0; i < pool.Len(); i++ {
				n, _ := pool.Name(i)
				if n == dn {
					_ = pool.Disable(i)
					slog.Info("restored disabled key", "provider", name, "key_index", i, "key_name", dn)
				}
			}
		}
		slog.Info("provider configured",
			"name", name,
			"keys", len(keys),
			"target", cfg.TargetBase,
		)
	}

	// Warn if filter was set but no provider matched
	if sl.providerFilter != "" {
		found := false
		for _, n := range router.ProviderNames() {
			if n == sl.providerFilter {
				found = true
				break
			}
		}
		if !found {
			slog.Warn("no provider matched --provider filter", "provider", sl.providerFilter)
		}
	}
	return nil
}

// writePIDFile writes the PID file and returns the path for deferred cleanup.
func (sl *ServerLauncher) writePIDFile() (string, error) {
	pidPath := sl.PidFilePath(sl.devMode)
	if err := os.MkdirAll(filepath.Dir(pidPath), 0755); err != nil {
		slog.Warn("failed to create PID file directory", "error", err)
	}
	pidData := []byte(fmt.Sprintf("%d\n", os.Getpid()))
	if err := os.WriteFile(pidPath, pidData, 0644); err != nil {
		slog.Warn("failed to write PID file", "error", err)
		return pidPath, fmt.Errorf("failed to write PID file: %w", err)
	}
	return pidPath, nil
}

// waitForShutdown listens for OS signals, performs graceful shutdown,
// and triggers a self-restart if the binary was updated.
func (sl *ServerLauncher) waitForShutdown(router *ProviderRouter) error {
	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Binary self-monitoring (dev mode)
	if sl.restartCtrl != nil {
		if exePath, err := os.Executable(); err == nil {
			sl.restartCtrl.Setup(exePath, sigCh)
		}
	}

	<-sigCh
	slog.Info("shutting down")

	// Close file logger
	router.LogManager().CloseFileHandler()

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	router.Shutdown(ctx)
	router.Stop()
	slog.Info("server stopped gracefully")

	// Self-monitoring restart (only when binary was updated)
	if sl.restartCtrl != nil && sl.restartCtrl.ShouldRestart() {
		sl.restartCtrl.Execute()
	}

	return nil
}

// loadKeysForProvider loads API keys for a provider from its keys file or env.
func (sl *ServerLauncher) loadKeysForProvider(name string, cfg *config.Config) (keys, names []string) {
	keys, names, loaded := keypool.LoadKeysFromStore(name, cfg)
	if loaded {
		return keys, names
	}

	// Fallback: use configured keys
	keys = cfg.Keys
	names = cfg.KeyNames
	// Persist to keyring for first-time startup
	if len(keys) > 0 {
		entries := make([]keypool.KeyEntry, len(keys))
		for i := range keys {
			name := ""
			if i < len(names) {
				name = names[i]
			}
			entries[i] = keypool.KeyEntry{Key: keys[i], Name: name}
		}
		_ = keypool.SaveKeys(name, &keypool.KeyStore{Keys: entries})
	}
	return keys, names
}

// pidFilePath returns the path to the PID file, located in the config directory.
func (sl *ServerLauncher) PidFilePath(devMode bool) string {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		if devMode {
			return "akswitch-dev.pid"
		}
		return "akswitch.pid"
	}
	if devMode {
		return filepath.Join(filepath.Dir(xdgPath), "akswitch-dev.pid")
	}
	return filepath.Join(filepath.Dir(xdgPath), "akswitch.pid")
}

// checkPidFile reads the PID file and checks if the process is still running.
// Returns (true, pid) if running, (false, 0) otherwise.
func (sl *ServerLauncher) checkPidFile(pidFile string) (bool, int) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false, 0
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false, 0
	}

	if runtime.GOOS == "windows" {
		// Windows: use tasklist to check process existence
		cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH")
		out, err := cmd.Output()
		if err != nil {
			return false, 0
		}
		return strings.Contains(string(out), strconv.Itoa(pid)), pid
	}

	// Unix: signal 0 checks process existence without sending a signal
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, 0
	}
	_ = proc.Release()
	return proc.Signal(syscall.Signal(0)) == nil, pid
}
