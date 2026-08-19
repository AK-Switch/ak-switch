//go:build unit

package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	akswitchmetrics "akswitch/internal/metrics"
	"akswitch/internal/tracker"
)

// ── Helpers ─────────────────────────────────────────

func testStartTime() time.Time { return time.Now() }

func newTestProviderState(t *testing.T, name string, keys []string) *ProviderState {
	t.Helper()
	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:             "http://localhost:19999",
			Port:                   0,
			MaxRetries:             3,
			CooldownSec:            60,
			UpstreamCBThreshold:    5,
			CBResetSec:             30,
			HealthCheckIntervalSec: 30,
			HealthCheckPath:        "/health",
			HealthCheckTimeoutSec:  5,
		},
	}
	pool := keypool.NewKeyPool(keys, nil)
	pr := NewProviderRouter("")
	pr.AddProvider(name, cfg, pool)
	return pr.Provider(name)
}

func newProxyExecutor(t *testing.T) (*ProxyExecutor, *akswitchmetrics.Metrics, func()) {
	t.Helper()
	reg, m := akswitchmetrics.NewRegistry()
	cal := tracker.NewCalibrator(10)
	return NewProxyExecutor(m, cal), m, func() { _ = reg }
}

func newHTTPResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// ── categorizeError ─────────────────────────────────

func TestCategorizeError_NonRetryableCodes(t *testing.T) {
	codes := map[int]ErrorCategory{
		http.StatusBadRequest:            CatNonRetryable,
		http.StatusMethodNotAllowed:      CatNonRetryable,
		http.StatusNotAcceptable:         CatNonRetryable,
		http.StatusRequestEntityTooLarge: CatNonRetryable,
		http.StatusUnprocessableEntity:   CatNonRetryable,
		http.StatusNotImplemented:        CatNonRetryable,
	}
	for code, want := range codes {
		got := categorizeError(code, nil)
		if got != want {
			t.Errorf("categorizeError(%d, nil) = %d, want %d", code, got, want)
		}
	}
}

func TestCategorizeError_RetryableCodes(t *testing.T) {
	codes := map[int]ErrorCategory{
		http.StatusTooManyRequests:     CatRetryable,
		http.StatusBadGateway:          CatRetryable,
		http.StatusServiceUnavailable:  CatRetryable,
		http.StatusGatewayTimeout:      CatRetryable,
		http.StatusInternalServerError: CatRetryable,
		http.StatusUnauthorized:        CatRetryable,
		http.StatusForbidden:           CatRetryable,
		http.StatusConflict:            CatRetryable,
		http.StatusRequestTimeout:      CatRetryable,
	}
	for code, want := range codes {
		got := categorizeError(code, nil)
		if got != want {
			t.Errorf("categorizeError(%d, nil) = %d, want %d", code, got, want)
		}
	}
}

func TestCategorizeError_ClientAbortContextCanceled(t *testing.T) {
	err := context.Canceled
	if cat := categorizeError(0, err); cat != CatClientAbort {
		t.Errorf("categorizeError(0, context.Canceled) = %d, want CatClientAbort", cat)
	}
}

func TestCategorizeError_GenericNetworkErrorIsRetryable(t *testing.T) {
	err := errors.New("connection refused")
	if cat := categorizeError(0, err); cat != CatRetryable {
		t.Errorf("categorizeError(0, connection refused) = %d, want CatRetryable", cat)
	}
}

// ── handleRateLimited ───────────────────────────────

func TestHandleRateLimited_SingleKeyNotExhausted(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a"})
	px, _, _ := newProxyExecutor(t)

	w := httptest.NewRecorder()
	resp := newHTTPResponse(http.StatusTooManyRequests, `{"error":"rate limit"}`)

	result := px.handleRateLimited(w, ps, 0, resp, testStartTime(), "GET", "http://upstream/", nil)

	if result {
		t.Error("single key rate-limited should not return true")
	}
}

func TestHandleRateLimited_DisabledKeyReturnsTrue(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a"})
	px, _, _ := newProxyExecutor(t)

	// First call: puts key in Open/Cooling state (not yet disabled)
	w1 := httptest.NewRecorder()
	resp1 := newHTTPResponse(http.StatusTooManyRequests, `{"error":"rate limit"}`)
	px.handleRateLimited(w1, ps, 0, resp1, testStartTime(), "GET", "http://upstream/", nil)

	// Manually disable the key to simulate exhaustion
	_ = ps.pool.Disable(0)

	// Second call: key is disabled, ActiveCount=0 → should return true with 503
	w2 := httptest.NewRecorder()
	resp2 := newHTTPResponse(http.StatusTooManyRequests, `{"error":"rate limit"}`)
	result := px.handleRateLimited(w2, ps, 0, resp2, testStartTime(), "GET", "http://upstream/", nil)

	if !result {
		t.Error("exhausted provider should return true")
	}
	if w2.Code != http.StatusServiceUnavailable {
		t.Errorf("response status = %d, want 503", w2.Code)
	}
}

// ── handleAuthRejected ──────────────────────────────

func TestHandleAuthRejected_DisablesKeyKeepsOthersActive(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a", "key-b"})
	px, _, _ := newProxyExecutor(t)

	w := httptest.NewRecorder()
	resp := newHTTPResponse(http.StatusUnauthorized, `{"error":"invalid key"}`)

	result := px.handleAuthRejected(w, ps, 0, resp, testStartTime(), "GET", "http://upstream/", nil)

	if result {
		t.Error("2-key pool with 1 auth-rejected should not return true (key-b still active)")
	}
	if ps.pool.DisabledCount() != 1 {
		t.Errorf("disabled count = %d, want 1", ps.pool.DisabledCount())
	}
	if ps.pool.ActiveCount() != 1 {
		t.Errorf("active count = %d, want 1", ps.pool.ActiveCount())
	}
}

func TestHandleAuthRejected_AllKeysInvalidReturns503(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a", "key-b"})
	px, _, _ := newProxyExecutor(t)

	for i := range ps.pool.Keys() {
		w := httptest.NewRecorder()
		resp := newHTTPResponse(http.StatusUnauthorized, `{"error":"invalid key"}`)
		px.handleAuthRejected(w, ps, i, resp, testStartTime(), "GET", "http://upstream/", nil)
	}

	w := httptest.NewRecorder()
	resp := newHTTPResponse(http.StatusUnauthorized, `{"error":"invalid key"}`)
	result := px.handleAuthRejected(w, ps, 0, resp, testStartTime(), "GET", "http://upstream/", nil)

	if !result {
		t.Error("all keys auth-rejected should return true")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("response status = %d, want 503", w.Code)
	}
}

// ── handleServerError ───────────────────────────────

func TestHandleServerError_RecordsUpstreamFailure(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a"})
	px, _, _ := newProxyExecutor(t)

	resp := newHTTPResponse(http.StatusBadGateway, `{"error":"upstream"}`)
	px.handleServerError(ps, 0, resp, 0)
}

// ── handleNonRetryable ──────────────────────────────

func TestHandleNonRetryable_PassthroughStatus(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a"})
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	px, _, _ := newProxyExecutor(t)

	w := httptest.NewRecorder()
	resp := newHTTPResponse(http.StatusBadRequest, `{"error":"bad request"}`)

	px.handleNonRetryable(w, ps, 0, resp, testStartTime(), "GET", "http://upstream/", []byte(`{"error":"bad request"}`), 0, false)

	if w.Code != http.StatusBadRequest {
		t.Errorf("response status = %d, want 400", w.Code)
	}
}

// ── handleSuccess ───────────────────────────────────

func TestHandleSuccess_PassthroughBody(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a"})
	px, _, _ := newProxyExecutor(t)

	body := []byte(`{"result":"ok"}`)
	resp := newHTTPResponse(http.StatusOK, string(body))
	w := httptest.NewRecorder()

	px.handleSuccess(w, ps, 0, resp, testStartTime(), 0, "POST", "http://upstream/v1/chat", body, 0, false)

	if w.Code != http.StatusOK {
		t.Errorf("response status = %d, want 200", w.Code)
	}
}

// ── Execute: all keys cooling ─────────────────────────

func TestAllKeysCooling_Returns429(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a", "key-b"})
	pool := ps.pool

	// Put all keys on cooldown (Open state, not Permanent)
	for i := 0; i < 2; i++ {
		pool.Cooldown(i, 10*time.Minute)
	}

	// Verify preconditions: all keys are cooling but not disabled
	if !pool.AnyActive() {
		t.Fatal("AnyActive() should be true (keys are cooling, not disabled)")
	}
	if _, _, ok := pool.SelectKey(); ok {
		t.Fatal("SelectKey() should return false when all keys are cooling")
	}

	px, _, _ := newProxyExecutor(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	px.Execute(w, req, ps)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", w.Code)
	}

	// Verify the error body contains the expected error code
	if !strings.Contains(w.Body.String(), "ALL_KEYS_COOLING") {
		t.Errorf("response body missing error code: %s", w.Body.String())
	}
}

func TestAllKeysCooling_NoSleepLoop(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a"})
	pool := ps.pool

	// Put the only key on cooldown
	pool.Cooldown(0, 10*time.Minute)

	px, _, _ := newProxyExecutor(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	start := time.Now()
	px.Execute(w, req, ps)
	elapsed := time.Since(start)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", w.Code)
	}

	// Response must complete in under 2 seconds (no sleep loop)
	if elapsed >= 2*time.Second {
		t.Errorf("response took %v, expected < 2s (no sleep loop)", elapsed)
	}
}

func TestRecordProxyMetrics_Noop(t *testing.T) {
	px, _, _ := newProxyExecutor(t)
	px.recordProxyMetrics("GET", "2xx", "0", testStartTime())
}

// ── writeAllKeysExhausted ───────────────────────────

func TestWriteAllKeysExhausted_Returns503(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a"})
	px, _, _ := newProxyExecutor(t)

	w := httptest.NewRecorder()
	result := px.writeAllKeysExhausted(w, ps, "GET", testStartTime())

	if !result {
		t.Error("writeAllKeysExhausted should return true")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// ── MaskSensitiveData ───────────────────────────────

func TestMaskSensitiveData_LongBodyTruncated(t *testing.T) {
	longBody := strings.Repeat("x", 2000)
	masked := MaskSensitiveData(longBody, 1024)
	if len(masked) != 1024 {
		t.Errorf("masked length = %d, want 1024", len(masked))
	}
}

func TestMaskSensitiveData_ShortBodyUnchanged(t *testing.T) {
	body := "short body"
	masked := MaskSensitiveData(body, 1024)
	if masked != body {
		t.Errorf("short body was modified: %q", masked)
	}
}

// ── buildTargetURL ──────────────────────────────────

func TestBuildTargetURL_BasicPath(t *testing.T) {
	cfg := &config.Config{ProviderConfig: config.ProviderConfig{TargetBase: "https://api.example.com/v1"}}
	req, _ := http.NewRequest("GET", "https://akswitch/test/v1/chat", nil)
	url := buildTargetURL(cfg, req.URL.Path, req.URL.RawQuery)
	if url != "https://api.example.com/v1/test/v1/chat" {
		t.Errorf("buildTargetURL = %q", url)
	}
}

// ── readRequestBody ─────────────────────────────────

func TestReadRequestBody_Success(t *testing.T) {
	body := `{"model":"gpt-4"}`
	req, _ := http.NewRequest("POST", "/", strings.NewReader(body))
	b, err := readRequestBody(nil, req)
	if err != nil {
		t.Fatalf("readRequestBody: %v", err)
	}
	if string(b) != body {
		t.Errorf("body = %q, want %q", string(b), body)
	}
}

func TestReadRequestBody_EmptyBody(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	b, err := readRequestBody(nil, req)
	if err != nil {
		t.Fatalf("readRequestBody: %v", err)
	}
	if len(b) != 0 {
		t.Errorf("expected empty body, got %q", string(b))
	}
}

// ── SSE stream truncation detection ──────────────────

func TestStreamSSE_Truncated_InjectsErrorEvent(t *testing.T) {
	// Construct a truncated SSE stream (no message_stop or [DONE])
	body := "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hello\"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\" world\"}}\n\n"
	resp := newHTTPResponse(http.StatusOK, body)
	w := httptest.NewRecorder()

	streamSSEAndEstimateTokens(w, resp, []byte(`{"model":"claude-3"}`), "claude-3")

	output := w.Body.String()
	if !strings.Contains(output, "event: error") {
		t.Errorf("truncated stream should inject error event, got: %s", output)
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Errorf("truncated stream should inject message_stop event, got: %s", output)
	}
	if !strings.Contains(output, "overloaded_error") {
		t.Errorf("truncated stream should inject overloaded_error, got: %s", output)
	}
}

func TestStreamSSE_NormalCompletion_NoErrorEvent(t *testing.T) {
	// Construct a normal SSE stream ending with message_stop
	body := "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hello\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	resp := newHTTPResponse(http.StatusOK, body)
	w := httptest.NewRecorder()

	streamSSEAndEstimateTokens(w, resp, []byte(`{"model":"claude-3"}`), "claude-3")

	output := w.Body.String()
	if strings.Contains(output, "event: error") {
		t.Errorf("normal stream should not inject error event, got: %s", output)
	}
	if !strings.Contains(output, "message_stop") {
		t.Errorf("normal stream should contain message_stop, got: %s", output)
	}
}

func TestInjectTruncationError_WritesExpectedEvents(t *testing.T) {
	w := httptest.NewRecorder()
	injectTruncationError(w, nil, false)

	output := w.Body.String()
	if !strings.Contains(output, "event: error") {
		t.Errorf("expected event: error, got: %s", output)
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Errorf("expected event: message_stop, got: %s", output)
	}
	if !strings.Contains(output, "overloaded_error") {
		t.Errorf("expected overloaded_error, got: %s", output)
	}
}

// ── BufferMode ────────────────────────────────────────

func TestIsCompleteStream_WithTerminalEvent(t *testing.T) {
	body := []byte("data: hello\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	if !isCompleteStream(body) {
		t.Error("isCompleteStream should return true for body containing message_stop")
	}

	body = []byte("data: hello\n\ndata: [DONE]\n\n")
	if !isCompleteStream(body) {
		t.Error("isCompleteStream should return true for body containing [DONE]")
	}
}

func TestIsCompleteStream_WithoutTerminalEvent(t *testing.T) {
	body := []byte("data: hello\n\ndata: world\n\n")
	if isCompleteStream(body) {
		t.Error("isCompleteStream should return false for body without terminal event")
	}

	body = []byte("")
	if isCompleteStream(body) {
		t.Error("isCompleteStream should return false for empty body")
	}
}

func TestHandleSuccess_BufferMode_ReturnsFalseOnIncomplete(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a", "key-b"})
	ps.config.BufferMode = true
	px, _, _ := newProxyExecutor(t)

	// Incomplete SSE stream (no message_stop, no [DONE])
	body := "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"partial\"}}\n\n"
	resp := newHTTPResponse(http.StatusOK, body)
	resp.Header.Set("Content-Type", "text/event-stream")
	w := httptest.NewRecorder()

	result := px.handleSuccess(w, ps, 0, resp, testStartTime(), 0, "POST", "http://upstream/v1/messages", []byte(`{"model":"claude-3"}`), 0, false)

	if result {
		t.Error("handleSuccess should return false for incomplete stream in buffer mode")
	}
	// Client should not receive any content
	if w.Body.Len() > 0 {
		t.Errorf("client should receive no content on incomplete stream, got: %s", w.Body.String())
	}
}

func TestHandleSuccess_BufferMode_ReturnsTrueOnComplete(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a", "key-b"})
	ps.config.BufferMode = true
	px, _, _ := newProxyExecutor(t)

	// Complete SSE stream (contains message_stop)
	body := "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hello\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	resp := newHTTPResponse(http.StatusOK, string(body))
	resp.Header.Set("Content-Type", "text/event-stream")
	w := httptest.NewRecorder()

	result := px.handleSuccess(w, ps, 0, resp, testStartTime(), 0, "POST", "http://upstream/v1/messages", []byte(`{"model":"claude-3"}`), 0, false)

	if !result {
		t.Error("handleSuccess should return true for complete stream in buffer mode")
	}
	if w.Code != http.StatusOK {
		t.Errorf("response status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "message_stop") {
		t.Errorf("client should receive the complete stream, got: %s", w.Body.String())
	}
}

func TestHandleSuccess_NonBufferMode_ReturnsTrue(t *testing.T) {
	ps := newTestProviderState(t, "test", []string{"key-a"})
	ps.config.BufferMode = false
	px, _, _ := newProxyExecutor(t)

	body := []byte(`{"result":"ok"}`)
	resp := newHTTPResponse(http.StatusOK, string(body))
	w := httptest.NewRecorder()

	result := px.handleSuccess(w, ps, 0, resp, testStartTime(), 0, "POST", "http://upstream/v1/chat", body, 0, false)

	if !result {
		t.Error("handleSuccess should return true in non-buffer mode always")
	}
	if w.Code != http.StatusOK {
		t.Errorf("response status = %d, want 200", w.Code)
	}
}

func TestStreamSSEAndEstimateTokens_EmptyStream(t *testing.T) {
	body := []byte("data: [DONE]\n\n")
	resp := newHTTPResponse(http.StatusOK, string(body))
	w := httptest.NewRecorder()

	inputTokens, outputTokens, _ := streamSSEAndEstimateTokens(w, resp, body, "gpt-4")

	if inputTokens < 0 || outputTokens < 0 {
		t.Errorf("unexpected negative tokens: input=%d output=%d", inputTokens, outputTokens)
	}
}

// ── Execute 4xx error dump ────────────────────────────

func TestExecute_NonRetryable_WritesErrorDump(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`))
	}))
	defer backend.Close()

	ps := newTestProviderState(t, "test", []string{"sk-key-0"})
	ps.config.TargetBase = backend.URL
	ps.SetThinkingMode("rectify")
	ps.SetRectifyThinkingMapTo("enabled")

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	px, _, _ := newProxyExecutor(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4","thinking":{"type":"adaptive"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	px.Execute(w, req, ps)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Verify dump file exists with both bodies + metadata.
	errorsDir := filepath.Join(tmpHome, ".akswitch", "errors")
	entries, err := os.ReadDir(errorsDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 dump in %s, got %v (err=%v)", errorsDir, entries, err)
	}
	data, err := os.ReadFile(filepath.Join(errorsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"thinking":{"type":"enabled"}`) {
		t.Errorf("dump missing request body:\n%s", content)
	}
	if !strings.Contains(content, `invalid_request_error`) {
		t.Errorf("dump missing response body:\n%s", content)
	}
	if !strings.Contains(content, "Rectified: true") {
		t.Errorf("dump missing rectified flag:\n%s", content)
	}
}

func TestExecute_NonRetryable_ClientStillGetsFullBody(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"full upstream error detail"}`))
	}))
	defer backend.Close()

	ps := newTestProviderState(t, "test", []string{"sk-key-0"})
	ps.config.TargetBase = backend.URL

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	px, _, _ := newProxyExecutor(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	px.Execute(w, req, ps)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if got := w.Body.String(); !strings.Contains(got, "full upstream error detail") {
		t.Errorf("client body lost upstream error, got: %q", got)
	}
}

func TestExecute_ThinkingRectifier_Enabled(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"type":"enabled"`) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"choices":[]}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"thinking.type not supported"}`))
	}))
	defer backend.Close()

	ps := newTestProviderState(t, "test", []string{"sk-key-0"})
	ps.config.TargetBase = backend.URL
	ps.SetThinkingMode("rectify")
	ps.SetRectifyThinkingMapTo("enabled")

	px, _, _ := newProxyExecutor(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4","thinking":{"type":"adaptive"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	px.Execute(w, req, ps)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ── TTFB regression ───────────────────────────────────

// TestExecute_TtfbLessThanDuration verifies that ttfb_ms is sampled when the
// upstream response headers arrive (client.Do returns), not after the full
// stream has been consumed. Regression for commit 3479eb8 where ttfb_ms was
// computed with time.Since(start) at the end of handleSuccess, making
// ttfb_ms always equal to duration_ms.
func TestExecute_TtfbLessThanDuration(t *testing.T) {
	// Backend: send headers + flush immediately, then delay the body to
	// simulate a streaming LLM response.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}" + "\n\n"))
	}))
	defer backend.Close()

	ps := newTestProviderState(t, "test", []string{"sk-key-0"})
	ps.config.TargetBase = backend.URL

	px, _, _ := newProxyExecutor(t)

	// Capture slog output to inspect ttfb_ms / duration_ms.
	var logBuf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(orig)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	px.Execute(w, req, ps)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Parse ttfb_ms / duration_ms from the "proxy success" log line.
	ttfb, dur := parseTtfbAndDuration(t, logBuf.String())
	if dur <= ttfb {
		t.Fatalf("ttfb_ms=%d should be strictly less than duration_ms=%d for streaming responses", ttfb, dur)
	}
	if ttfb < 0 || dur < 0 {
		t.Fatalf("failed to parse timing from log: %q", logBuf.String())
	}
}

// parseTtfbAndDuration extracts ttfb_ms and duration_ms from a slog
// TextHandler output line containing "proxy success".
// Returns (-1, -1) when the log line is missing or unparsable.
func parseTtfbAndDuration(t *testing.T, logOutput string) (int64, int64) {
	t.Helper()
	ttfbRe := regexp.MustCompile(`ttfb_ms=(\d+)`)
	durRe := regexp.MustCompile(`duration_ms=(\d+)`)

	var ttfb, dur = int64(-1), int64(-1)
	for _, line := range strings.Split(logOutput, "\n") {
		if !strings.Contains(line, "proxy success") {
			continue
		}
		if m := ttfbRe.FindStringSubmatch(line); m != nil {
			ttfb, _ = strconv.ParseInt(m[1], 10, 64)
		}
		if m := durRe.FindStringSubmatch(line); m != nil {
			dur, _ = strconv.ParseInt(m[1], 10, 64)
		}
	}
	return ttfb, dur
}

func TestExecute_ThinkingRectifier_Disabled(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"type":"adaptive"`) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"choices":[]}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"unexpected thinking.type"}`))
	}))
	defer backend.Close()

	ps := newTestProviderState(t, "test", []string{"sk-key-0"})
	ps.config.TargetBase = backend.URL
	ps.SetThinkingMode("default")

	px, _, _ := newProxyExecutor(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4","thinking":{"type":"adaptive"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	px.Execute(w, req, ps)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
