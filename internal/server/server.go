// Package server provides the HTTP server, proxy, and management handlers for AK Switch.
package server

import (
	"akswitch/internal/circuitbreaker"
	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// logLevel is a shared LevelVar used by both stderr and file handlers.
// ApplyLogLevel sets this value, and both handlers reflect the change automatically.
var logLevel slog.LevelVar

// logCompact controls whether stdout uses compact ColorHandler format.
// Set via SetLogFormat before the first ApplyLogLevel call.
var logCompact bool

// logSingleProvider controls whether the provider name is shown in compact mode.
// When true (single provider), the provider name is hidden.
// Set via SetLogFormat before the first ApplyLogLevel call.
var logSingleProvider bool

// fileHandlerWriter is the active lumberjack.Logger for file logging.
// nil when file logging is not configured.
var fileHandlerWriter *lumberjack.Logger

// ProxyEngine holds the HTTP client and circuit breakers for upstream proxy requests.
type ProxyEngine struct {
	client *http.Client
	upCB   *circuitbreaker.UpstreamCircuitBreaker
}

// NewProxyEngine creates a ProxyEngine from config and key count.
func NewProxyEngine(cfg *config.Config, pool *keypool.KeyPool) *ProxyEngine {
	backoffCapSec := cfg.BackoffCapSec
	if backoffCapSec <= 0 {
		backoffCapSec = 120
	}
	backoffMult := cfg.BackoffMultiplier
	if backoffMult <= 0 {
		backoffMult = 2
	}
	upstreamThreshold := cfg.UpstreamCBThreshold
	if upstreamThreshold <= 0 {
		upstreamThreshold = 5
	}
	cbResetSec := cfg.CBResetSec
	if cbResetSec <= 0 {
		cbResetSec = 30
	}
	base := time.Duration(cfg.CooldownSec) * time.Second
	cap_ := time.Duration(backoffCapSec) * time.Second
	pool.ConfigureCBs(base, cap_, backoffMult)

	upCB := circuitbreaker.NewUpstreamCircuitBreaker(
		upstreamThreshold,
		time.Duration(cbResetSec)*time.Second,
	)

	return &ProxyEngine{
		client: &http.Client{
			Timeout: time.Duration(cfg.HTTPTimeoutSec) * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        500,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
				ForceAttemptHTTP2:   true,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},		upCB:   upCB,
	}
}



// SetLogFormat sets the log output format for stdout.
// compact=true enables the compact ColorHandler format.
// singleProvider=true hides the provider name in compact mode.
// Must be called before the first ApplyLogLevel call to take effect.
func SetLogFormat(compact bool, singleProvider bool) {
	logCompact = compact
	logSingleProvider = singleProvider
}

// ApplyLogLevel sets the global slog handler's minimum level based on a string.
// Supported values: "debug", "info", "warn", "error".
// Unknown or empty values default to slog.LevelInfo.
// Updates both the stderr handler and the active file handler (if any).
// The log output format (compact vs default) is controlled by SetLogFormat.
func ApplyLogLevel(level string) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	logLevel.Set(lvl)

	stderrHandler := newHandler(os.Stderr, &logLevel, logCompact, logSingleProvider)
	if fileHandlerWriter != nil {
		fileHandler := slog.NewTextHandler(fileHandlerWriter, &slog.HandlerOptions{Level: &logLevel})
		slog.SetDefault(slog.New(&multiHandler{stderr: stderrHandler, file: fileHandler}))
	} else {
		slog.SetDefault(slog.New(stderrHandler))
	}
}

// InitFileHandler initializes file-based logging with the given path and rotation settings.
// If logFile is empty, this is a no-op (file logging remains disabled).
// It wraps the current default slog handler (stderr) with a multiHandler that also
// writes to the file. Call this AFTER ApplyLogLevel to preserve level synchronization.
// The file and stderr handlers share the same logLevel LevelVar.
func InitFileHandler(logFile string, maxSizeMB, maxAgeDays int) {
	if logFile == "" {
		return
	}
	// Close any existing file handler first
	CloseFileHandler()

	lj := &lumberjack.Logger{
		Filename: logFile,
		MaxSize:  maxSizeMB,
		MaxAge:   maxAgeDays,
		Compress: false,
	}
	fileHandlerWriter = lj

	fileHandler := slog.NewTextHandler(lj, &slog.HandlerOptions{Level: &logLevel})

	// Use a direct stderr handler instead of slog.Default().Handler()
	// to avoid a circular dependency in Go 1.24+:
	// slog.Default().Handler() writes to log.Writer(), which calls
	// slog.Default().Handler() again → deadlock.
	stderrHandler := slog.NewTextHandler(os.Stderr, nil)

	// Wrap both into a multiHandler
	slog.SetDefault(slog.New(&multiHandler{
		stderr: stderrHandler,
		file:   fileHandler,
	}))
	slog.Info("file logging initialized", "path", logFile, "maxSizeMB", maxSizeMB, "maxAgeDays", maxAgeDays)
}

// CloseFileHandler closes the active file handler writer, if any.
func CloseFileHandler() {
	if fileHandlerWriter != nil {
		_ = fileHandlerWriter.Close()
		fileHandlerWriter = nil
	}
}

// keyPrefixes are known API key prefixes to mask in debug logging.
var keyPrefixes = []string{"sk-", "nvapi-"}

// MaskSensitiveData scrubs potential API key patterns from a string for safe debug logging.
// It masks any word-like token starting with a known key prefix by replacing it with "***".
// It also truncates the result to maxLen bytes.
func MaskSensitiveData(data string, maxLen int) string {
	if len(data) > maxLen {
		data = data[:maxLen]
	}
	// Mask known API key prefixes
	result := data
	lower := strings.ToLower(data)
	for _, prefix := range keyPrefixes {
		idx := strings.Index(lower, prefix)
		for idx >= 0 {
			// Find end of token (word boundary)
			end := idx + len(prefix)
			for end < len(result) && (isAlphaNum(result[end]) || result[end] == '-' || result[end] == '_') {
				end++
			}
			if end > idx+len(prefix) {
				result = result[:idx] + "***" + result[end:]
				lower = strings.ToLower(result)
			}
			idx = strings.Index(lower, prefix)
		}
	}
	return result
}

func isAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// RouteEntry represents a single provider's routing info.
type RouteEntry struct {
	Config *config.Config
	Pool   *keypool.KeyPool
	Proxy  *ProxyEngine
}
