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
	h := newHandler(&buf, slog.LevelInfo, false, false)
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
	h := newHandler(os.Stderr, slog.LevelInfo, false, false)
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
	if !strings.Contains(output, "POST") {
		t.Errorf("compact request should contain POST, got: %q", output)
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
	if !strings.Contains(output, "(    d1-1    )") {
		t.Errorf("compact success should contain centered key name, got: %q", output)
	}
	if !strings.Contains(output, "ttfb=") || !strings.Contains(output, "567ms") {
		t.Errorf("compact success should contain ttfb=567ms, got: %q", output)
	}
	if !strings.Contains(output, "total=") || !strings.Contains(output, "1.2s") {
		t.Errorf("compact success should contain total=1.2s, got: %q", output)
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
	if !strings.Contains(output, "(    d1-1    ) retry 3") {
		t.Errorf("compact success with retry=3 should show key name and retry, got: %q", output)
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
	if strings.Contains(output, "POST") {
		t.Errorf("compact success should not show method, got: %q", output)
	}
	if strings.Contains(output, "/v1/chat/completions") {
		t.Errorf("compact success should not show URL, got: %q", output)
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
	if !strings.Contains(output, "total=") || !strings.Contains(output, "15.0s") {
		t.Errorf("compact success should show total=15.0s, got: %q", output)
	}
	if !strings.Contains(output, "ttfb=") || !strings.Contains(output, "3.0s") {
		t.Errorf("compact success should show ttfb=3.0s, got: %q", output)
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
	if strings.Contains(output, "POST") {
		t.Errorf("compact error should not contain method, got: %q", output)
	}
	if strings.Contains(output, "/v1/messages") {
		t.Errorf("compact error should not contain URL, got: %q", output)
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
	stripped := stripANSI(output)
	if strings.Contains(stripped, "level=INFO") {
		t.Errorf("non-proxy message should not use TextHandler format, got: %q", stripped)
	}
	if !strings.Contains(stripped, "INFO") {
		t.Errorf("non-proxy message should contain colored level label, got: %q", stripped)
	}
	if !strings.Contains(output, "server started") {
		t.Errorf("non-proxy message should contain message text, got: %q", output)
	}
}

// stripANSI removes ANSI escape codes from a string for test assertions.
func stripANSI(s string) string {
	var result strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\033' {
			// Skip ANSI escape sequence: \033[...m
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		result.WriteByte(s[i])
	}
	return result.String()
}

// ── Acceptance tests ────────────────────────────────────
// These tests validate the full compact log format by stripping ANSI codes
// and checking the plain-text output layout.

func TestCompact_Acceptance_SingleProvider(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:          slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:         &buf,
		addSource:      false,
		compact:        true,
		singleProvider: true,
	}
	logger := slog.New(handler)
	logger.Info("proxy success",
		"provider", "sensenova",
		"method", "POST",
		"url", "https://token.sensenova.cn/v1/messages",
		"status", 200,
		"key_index", 3,
		"key_name", "d1-1",
		"retry", 0,
		"duration_ms", 1234,
		"ttfb_ms", 567,
		"request_body_size", 102400,
		"response_body_size", 51200,
		"input_tokens", 45,
		"output_tokens", 312,
	)

	output := buf.String()
	stripped := stripANSI(output)
	t.Logf("Compact output: %s", stripped)

	// Expected format: [HH:MM:SS] 200 (    d1-1    ) ttfb=567ms total=1.2s 100KB→50KB tok=45+312
	if !strings.HasPrefix(stripped, "[") {
		t.Errorf("output should start with timestamp bracket, got: %q", stripped)
	}
	expected := "200 (    d1-1    ) ttfb=567ms total=1.2s 100KB→50KB tok=45+312"
	if !strings.Contains(stripped, expected) {
		t.Errorf("format mismatch:\n  got:  %s\n  want: %s", stripped, expected)
	}
}

func TestCompact_Acceptance_MultiProvider(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:          slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:         &buf,
		addSource:      false,
		compact:        true,
		singleProvider: false,
	}
	logger := slog.New(handler)
	logger.Info("proxy success",
		"provider", "sensenova",
		"method", "POST",
		"url", "https://token.sensenova.cn/v1/messages",
		"status", 200,
		"key_index", 3,
		"key_name", "d1-1",
		"retry", 0,
		"duration_ms", 1234,
		"ttfb_ms", 567,
		"request_body_size", 102400,
		"response_body_size", 51200,
	)

	output := buf.String()
	stripped := stripANSI(output)

	// Multi-provider: provider name should be visible
	expected := "200 sensenova (    d1-1    )"
	if !strings.Contains(stripped, expected) {
		t.Errorf("multi-provider should show provider name:\n  got:  %s\n  want: %s", stripped, expected)
	}
}

func TestCompact_Acceptance_LongKeyName(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:          slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:         &buf,
		addSource:      false,
		compact:        true,
		singleProvider: true,
	}
	logger := slog.New(handler)
	logger.Info("proxy success",
		"provider", "openai",
		"method", "POST",
		"url", "https://api.openai.com/v1/chat/completions",
		"status", 200,
		"key_index", 0,
		"key_name", "sk-ant-xxxx-long-name",
		"retry", 0,
		"duration_ms", 5000,
		"ttfb_ms", 2000,
		"request_body_size", 1024,
		"response_body_size", 512,
		"input_tokens", 45,
		"output_tokens", 312,
	)

	output := buf.String()
	stripped := stripANSI(output)

	// Long key name should be truncated to 12 chars with middle ...
	if !strings.Contains(stripped, "sk-an...name") {
		t.Errorf("long key name should be truncated to 12 chars, got: %s", stripped)
	}
}

func TestCompact_Acceptance_ErrorWithKeyName(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:          slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:         &buf,
		addSource:      false,
		compact:        true,
		singleProvider: true,
	}
	logger := slog.New(handler)
	logger.Warn("non-retryable client error",
		"provider", "sensenova",
		"method", "POST",
		"url", "https://token.sensenova.cn/v1/messages",
		"status", 429,
		"key_name", "d1-1",
	)

	output := buf.String()
	stripped := stripANSI(output)

	// Error format: [HH:MM:SS] ✗ 429 (    d1-1    )
	if !strings.Contains(stripped, "✗ 429 (    d1-1    )") {
		t.Errorf("error should show ✗, status, and key name, got: %s", stripped)
	}
}

func TestCompact_Acceptance_Retry(t *testing.T) {
	var buf bytes.Buffer
	handler := &ColorHandler{
		inner:          slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		writer:         &buf,
		addSource:      false,
		compact:        true,
		singleProvider: true,
	}
	logger := slog.New(handler)
	logger.Info("proxy success",
		"provider", "sensenova",
		"method", "POST",
		"url", "https://token.sensenova.cn/v1/messages",
		"status", 200,
		"key_index", 3,
		"key_name", "d1-1",
		"retry", 3,
		"duration_ms", 5000,
		"ttfb_ms", 2000,
		"request_body_size", 100,
		"response_body_size", 0,
	)

	output := buf.String()
	stripped := stripANSI(output)

	// Retry format: ... (    d1-1    ) retry 3 ttfb=...
	if !strings.Contains(stripped, "(    d1-1    ) retry 3") {
		t.Errorf("retry should show after key name, got: %s", stripped)
	}
}
