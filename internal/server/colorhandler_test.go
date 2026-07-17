//go:build unit

package server

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// Compile-time check: ColorHandler implements slog.Handler
var _ slog.Handler = (*ColorHandler)(nil)

func TestNewHandler_NonTTY_ReturnsTextHandler(t *testing.T) {
	var buf bytes.Buffer
	h := newHandler(&buf, slog.LevelInfo, false)
	if _, ok := h.(*slog.TextHandler); !ok {
		t.Errorf("expected *slog.TextHandler, got %T", h)
	}
}

func TestColorHandler_OutputContainsANSICodes(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:     slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:    &buf,
		addSource: false,
	}
	logger := slog.New(handler)
	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "\033[") {
		t.Errorf("expected ANSI escape codes in output, got: %q", output)
	}
}

func TestNewHandler_NOCOLOR_ReturnsTextHandler(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	h := newHandler(os.Stderr, slog.LevelInfo, false)
	if _, ok := h.(*slog.TextHandler); !ok {
		t.Errorf("expected *slog.TextHandler, got %T", h)
	}
}

func TestColorHandler_AllLevels(t *testing.T) {
	levels := []slog.Level{
		slog.LevelDebug,
		slog.LevelInfo,
		slog.LevelWarn,
		slog.LevelError,
	}

	for _, lvl := range levels {
		t.Run(lvl.String(), func(t *testing.T) {
			var buf bytes.Buffer
			handler := &ColorHandler{
				inner:     slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
				writer:    &buf,
				addSource: false,
			}
			logger := slog.New(handler)

			// Log at the specified level with a unique message
			msg := "test at " + lvl.String()
			switch lvl {
			case slog.LevelDebug:
				logger.Debug(msg)
			case slog.LevelInfo:
				logger.Info(msg)
			case slog.LevelWarn:
				logger.Warn(msg)
			case slog.LevelError:
				logger.Error(msg)
			}

			output := buf.String()
			if !strings.Contains(output, "\033[") {
				t.Errorf("expected ANSI codes for level %s, got: %q", lvl, output)
			}
			if !strings.Contains(output, msg) {
				t.Errorf("expected message %q in output, got: %q", msg, output)
			}
		})
	}
}

// ── Compact mode tests ──────────────────────────────

func TestCompact_ProxyRequest(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:     slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:    &buf,
		addSource: false,
		compact:   true,
	}
	logger := slog.New(handler)
	logger.Info("proxy request", "provider", "sensenova", "method", "POST",
		"url", "https://token.sensenova.cn/v1/messages?beta=true", "body_size", 392394)

	output := buf.String()
	// Compact mode handles "proxy request" with a compact format
	if !strings.Contains(output, "→ POST") {
		t.Errorf("compact request should contain → POST, got: %q", output)
	}
	if !strings.Contains(output, "/v1/messages?beta=true") {
		t.Errorf("compact request should contain URL path, got: %q", output)
	}
	if strings.Contains(output, "INFO") {
		t.Errorf("compact request should not contain level label INFO, got: %q", output)
	}
}

func TestCompact_ProxySuccess(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:     slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:    &buf,
		addSource: false,
		compact:   true,
	}
	logger := slog.New(handler)
	logger.Info("proxy success", "provider", "sensenova", "method", "POST",
		"url", "https://token.sensenova.cn/v1/messages", "status", 200,
		"key_index", 3, "key_name", "d1-1", "retry", 0,
		"duration_ms", 1234, "ttfb_ms", 567,
		"request_body_size", 102400, "response_body_size", 51200)

	output := buf.String()
	if !strings.Contains(output, "200") {
		t.Errorf("compact success should contain status 200, got: %q", output)
	}
	if !strings.Contains(output, "sensenova") {
		t.Errorf("compact success should contain provider name, got: %q", output)
	}
	if !strings.Contains(output, "POST") {
		t.Errorf("compact success should contain method, got: %q", output)
	}
	if !strings.Contains(output, "/v1/messages") {
		t.Errorf("compact success should contain URL path, got: %q", output)
	}
	if !strings.Contains(output, "key: d1-1") {
		t.Errorf("compact success should contain key name, got: %q", output)
	}
	if !strings.Contains(output, "ttfb=567ms") {
		t.Errorf("compact success should contain ttfb, got: %q", output)
	}
	if !strings.Contains(output, "total=1.2s") {
		t.Errorf("compact success should contain total time, got: %q", output)
	}
	if strings.Contains(output, "retry") {
		t.Errorf("compact success with retry=0 should not show retry, got: %q", output)
	}
}

func TestCompact_ProxySuccess_WithRetry(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:     slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:    &buf,
		addSource: false,
		compact:   true,
	}
	logger := slog.New(handler)
	logger.Info("proxy success", "provider", "sensenova", "method", "POST",
		"url", "https://token.sensenova.cn/v1/messages", "status", 200,
		"key_index", 3, "key_name", "d1-1", "retry", 3)

	output := buf.String()
	if !strings.Contains(output, "retry 3") {
		t.Errorf("compact success with retry=3 should show retry 3, got: %q", output)
	}
}

func TestCompact_ProxySuccess_WithBodySizes(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:     slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:    &buf,
		addSource: false,
		compact:   true,
	}
	logger := slog.New(handler)
	logger.Info("proxy success", "provider", "openai", "method", "POST",
		"url", "https://api.openai.com/v1/chat/completions", "status", 200,
		"key_index", 0, "key_name", "sk-xxxx", "retry", 0,
		"duration_ms", 2345, "ttfb_ms", 1234,
		"request_body_size", 362496, "response_body_size", 12288)

	output := buf.String()
	if !strings.Contains(output, "354KB→12KB") {
		t.Errorf("compact success should show body size arrow, got: %q", output)
	}
}

func TestCompact_ProxySuccess_WithTokens(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:     slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:    &buf,
		addSource: false,
		compact:   true,
	}
	logger := slog.New(handler)
	logger.Info("proxy success", "provider", "anthropic", "method", "POST",
		"url", "https://api.anthropic.com/v1/messages", "status", 200,
		"key_index", 0, "key_name", "sk-ant-xxxx", "retry", 0,
		"duration_ms", 5000, "ttfb_ms", 2000,
		"request_body_size", 1024, "response_body_size", 512,
		"input_tokens", 45, "output_tokens", 312)

	output := buf.String()
	if !strings.Contains(output, "tok=45+312") {
		t.Errorf("compact success should show tok=45+312, got: %q", output)
	}
}

func TestCompact_ProxySuccess_StreamingDuration(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:     slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:    &buf,
		addSource: false,
		compact:   true,
	}
	logger := slog.New(handler)
	// Streaming: long TTFB and total duration
	logger.Info("proxy success", "provider", "sensenova", "method", "POST",
		"url", "https://token.sensenova.cn/v1/messages", "status", 200,
		"key_index", 3, "key_name", "d1-1", "retry", 0,
		"duration_ms", 15000, "ttfb_ms", 3000,
		"request_body_size", 100, "response_body_size", 0)

	output := buf.String()
	if !strings.Contains(output, "total=15.0s") {
		t.Errorf("compact success should show long duration 15.0s, got: %q", output)
	}
	if !strings.Contains(output, "ttfb=3.0s") {
		t.Errorf("compact success should show ttfb in seconds 3.0s, got: %q", output)
	}
}

func TestCompact_NonRetryableError(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:     slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:    &buf,
		addSource: false,
		compact:   true,
	}
	logger := slog.New(handler)
	logger.Warn("non-retryable client error", "provider", "sensenova", "method", "POST",
		"url", "https://token.sensenova.cn/v1/messages", "status", 400)

	output := buf.String()
	if !strings.Contains(output, "✗") {
		t.Errorf("compact error should contain ✗, got: %q", output)
	}
	if !strings.Contains(output, "400") {
		t.Errorf("compact error should contain status 400, got: %q", output)
	}
	if !strings.Contains(output, "POST") {
		t.Errorf("compact error should contain method POST, got: %q", output)
	}
	if !strings.Contains(output, "/v1/messages") {
		t.Errorf("compact error should contain URL path, got: %q", output)
	}
}

func TestCompact_OtherMessages(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:     slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:    &buf,
		addSource: false,
		compact:   true,
	}
	logger := slog.New(handler)
	logger.Info("server started", "addr", "127.0.0.1:4000", "providers", 1)

	output := buf.String()
	if !strings.Contains(output, "level=INFO") {
		t.Errorf("non-proxy message should use TextHandler format, got: %q", output)
	}
	if !strings.Contains(output, "server started") {
		t.Errorf("non-proxy message should contain message text, got: %q", output)
	}
}
