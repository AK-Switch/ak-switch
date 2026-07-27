package server

import (
	"log/slog"
	"os"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// LogManager manages the slog logging configuration for the server.
// It encapsulates the log level, format, and file handler state,
// replacing the previous package-level variables.
type LogManager struct {
	level          slog.LevelVar
	compact        bool
	singleProvider bool
	fileWriter     *lumberjack.Logger
}

// NewLogManager creates a new LogManager with default settings.
func NewLogManager() *LogManager {
	return &LogManager{}
}

// SetFormat sets the log output format for stdout.
// compact=true enables the compact ColorHandler format.
// singleProvider=true hides the provider name in compact mode.
func (lm *LogManager) SetFormat(compact bool, singleProvider bool) {
	lm.compact = compact
	lm.singleProvider = singleProvider
}

// ApplyLevel sets the global slog handler's minimum level based on a string.
// Supported values: "debug", "info", "warn", "error".
// Unknown or empty values default to slog.LevelInfo.
// Updates both the stderr handler and the active file handler (if any).
func (lm *LogManager) ApplyLevel(level string) {
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
	lm.level.Set(lvl)

	stderrHandler := newHandler(os.Stderr, &lm.level, lm.compact, lm.singleProvider)
	if lm.fileWriter != nil {
		fileHandler := slog.NewTextHandler(lm.fileWriter, &slog.HandlerOptions{Level: &lm.level})
		slog.SetDefault(slog.New(&multiHandler{stderr: stderrHandler, file: fileHandler}))
	} else {
		slog.SetDefault(slog.New(stderrHandler))
	}
}

// InitFileHandler initializes file-based logging with the given path and rotation settings.
// If logFile is empty, this is a no-op (file logging remains disabled).
func (lm *LogManager) InitFileHandler(logFile string, maxSizeMB, maxAgeDays int) {
	if logFile == "" {
		return
	}
	// Close any existing file handler first
	lm.CloseFileHandler()

	lj := &lumberjack.Logger{
		Filename: logFile,
		MaxSize:  maxSizeMB,
		MaxAge:   maxAgeDays,
		Compress: false,
	}
	lm.fileWriter = lj

	fileHandler := slog.NewTextHandler(lj, &slog.HandlerOptions{Level: &lm.level})

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

// CurrentLevel returns the current log level as a string.
func (lm *LogManager) CurrentLevel() string {
	lvl := lm.level.Level()
	switch lvl {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelInfo:
		return "info"
	case slog.LevelWarn:
		return "warn"
	case slog.LevelError:
		return "error"
	default:
		return "info"
	}
}

// CloseFileHandler closes the active file handler writer, if any.
func (lm *LogManager) CloseFileHandler() {
	if lm.fileWriter != nil {
		_ = lm.fileWriter.Close()
		lm.fileWriter = nil
	}
}