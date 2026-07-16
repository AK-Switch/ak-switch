package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorGray   = "\033[90m"
	colorWhite  = "\033[97m"
)

// ColorHandler is a slog.Handler wrapper that adds ANSI color to output.
type ColorHandler struct {
	inner     slog.Handler
	writer    io.Writer
	addSource bool
	compact   bool
}

// newHandler creates an appropriate slog.Handler based on the output destination.
// - If NO_COLOR env var is set → plain TextHandler
// - If w is a terminal → ColorHandler (ANSI colored)
// - Otherwise → plain TextHandler
// lvl should be a *slog.LevelVar for dynamic level updates, or a fixed slog.Level.
func newHandler(w io.Writer, lvl slog.Leveler, compact bool) slog.Handler {
	// NO_COLOR convention: https://no-color.org/
	if os.Getenv("NO_COLOR") != "" {
		return slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl})
	}

	// Check if it's a terminal
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return &ColorHandler{
			inner:     slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl, AddSource: true}),
			writer:    w,
			addSource: lvl.Level() <= slog.LevelDebug, // only show caller in debug
			compact:   compact,
		}
	}

	return slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl})
}

// Implement slog.Handler interface

func (h *ColorHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.inner.Enabled(ctx, lvl)
}

func (h *ColorHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.compact {
		return h.handleCompact(ctx, r)
	}

	// Time
	ts := r.Time.Format("15:04:05.000")

	// Level color and label
	var levelColor string
	var levelLabel string
	switch {
	case r.Level >= slog.LevelError:
		levelColor = colorRed
		levelLabel = "ERRO"
	case r.Level >= slog.LevelWarn:
		levelColor = colorYellow
		levelLabel = "WARN"
	case r.Level >= slog.LevelInfo:
		levelColor = colorGreen
		levelLabel = "INFO"
	default:
		levelColor = colorGray
		levelLabel = "DEBU"
	}

	// Message
	msg := r.Message

	// Collect attrs
	var attrs strings.Builder
	r.Attrs(func(a slog.Attr) bool {
		if attrs.Len() > 0 {
			attrs.WriteByte(' ')
		}
		attrs.WriteString(fmt.Sprintf("%s%s%s=%s%v%s",
			colorGray, a.Key, colorReset,
			colorWhite, a.Value.Any(), colorReset))
		return true
	})

	// Add source info for debug
	var source string
	if h.addSource && r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		if f, _ := fs.Next(); f.File != "" {
			// Short file name
			shortFile := f.File
			if idx := strings.LastIndex(shortFile, "/"); idx >= 0 {
				shortFile = shortFile[idx+1:]
			}
			source = fmt.Sprintf("%s %s:%d%s", colorGray, shortFile, f.Line, colorReset)
		}
	}

	// Build suffix (source + attrs) with proper spacing
	attrsStr := attrs.String()
	var suffix string
	switch {
	case source != "" && attrsStr != "":
		suffix = source + " " + attrsStr
	case source != "":
		suffix = source
	case attrsStr != "":
		suffix = attrsStr
	}

	// Build output line with proper spacing between msg and suffix
	line := fmt.Sprintf("%s%s%s %s%s%s %s",
		colorGray, ts, colorReset,
		levelColor, levelLabel, colorReset,
		msg,
	)
	if suffix != "" {
		line += " " + suffix
	}
	line += "\n"
	fmt.Fprint(h.writer, line)

	return nil
}

// handleCompact formats proxy-related log messages in a compact one-line format.
// Falls back to the default handler for non-proxy messages.
func (h *ColorHandler) handleCompact(ctx context.Context, r slog.Record) error {
	ts := r.Time.Format("15:04:05")
	bracketTS := fmt.Sprintf("%s[%s]%s", colorGray, ts, colorReset)

	switch r.Message {
	case "proxy request":
		var method, url string
		var bodySize int64
		r.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "method":
				method = fmt.Sprintf("%v", a.Value.Any())
			case "url":
				url = fmt.Sprintf("%v", a.Value.Any())
			case "body_size":
				bodySize = attrInt64(a)
			}
			return true
		})
		url = compactURL(url)
		sizeStr := formatSizeCompact(bodySize)
		line := fmt.Sprintf("%s %s→ %s %s (%s)%s\n",
			bracketTS, colorGray, method, url, sizeStr, colorReset)
		fmt.Fprint(h.writer, line)
		return nil

	case "proxy success":
		var status int
		var provider, keyName string
		var retry int
		r.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "status":
				status = int(attrInt64(a))
			case "provider":
				provider = fmt.Sprintf("%v", a.Value.Any())
			case "key_name":
				keyName = fmt.Sprintf("%v", a.Value.Any())
			case "retry":
				retry = int(attrInt64(a))
			}
			return true
		})
		keyPart := fmt.Sprintf("key: %s", keyName)
		if retry > 0 {
			keyPart = fmt.Sprintf("%s, retry %d", keyPart, retry)
		}
		line := fmt.Sprintf("%s %s%d%s %s (%s)%s\n",
			bracketTS, colorGreen, status, colorReset, provider, keyPart, colorReset)
		fmt.Fprint(h.writer, line)
		return nil

	case "non-retryable client error":
		var method, url string
		var status int
		r.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "method":
				method = fmt.Sprintf("%v", a.Value.Any())
			case "url":
				url = fmt.Sprintf("%v", a.Value.Any())
			case "status":
				status = int(attrInt64(a))
			}
			return true
		})
		url = compactURL(url)
		line := fmt.Sprintf("%s %s✗ %d %s %s%s\n",
			bracketTS, colorRed, status, method, url, colorReset)
		fmt.Fprint(h.writer, line)
		return nil

	default:
		return h.inner.Handle(ctx, r)
	}
}

// compactURL strips scheme and host from a URL, keeping only the path.
func compactURL(rawURL string) string {
	if idx := strings.Index(rawURL, "://"); idx >= 0 {
		if pathIdx := strings.Index(rawURL[idx+3:], "/"); pathIdx >= 0 {
			return rawURL[idx+3+pathIdx:]
		}
	}
	return rawURL
}

// formatSizeCompact converts bytes to a human-readable string (KB, MB, GB).
func formatSizeCompact(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%dKB", bytes>>10)
	default:
		return "0KB"
	}
}

// attrInt64 extracts an int64 value from a slog.Attr, supporting int, int64, and float64.
func attrInt64(a slog.Attr) int64 {
	switch v := a.Value.Any().(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

func (h *ColorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ColorHandler{
		inner:     h.inner.WithAttrs(attrs),
		writer:    h.writer,
		addSource: h.addSource,
		compact:   h.compact,
	}
}

func (h *ColorHandler) WithGroup(name string) slog.Handler {
	return &ColorHandler{
		inner:     h.inner.WithGroup(name),
		writer:    h.writer,
		addSource: h.addSource,
		compact:   h.compact,
	}
}