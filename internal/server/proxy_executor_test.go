//go:build unit

package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	px, _, _ := newProxyExecutor(t)

	w := httptest.NewRecorder()
	resp := newHTTPResponse(http.StatusBadRequest, `{"error":"bad request"}`)

	px.handleNonRetryable(w, ps, 0, resp, testStartTime(), "GET", "http://upstream/", []byte(`{"error":"bad request"}`), 0)

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

	px.handleSuccess(w, ps, 0, resp, testStartTime(), "POST", "http://upstream/v1/chat", body, 0, false)

	if w.Code != http.StatusOK {
		t.Errorf("response status = %d, want 200", w.Code)
	}
}

// ── recordProxyMetrics ──────────────────────────────

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

// ── SSE stream parsing ──────────────────────────────

func TestStreamSSEAndEstimateTokens_EmptyStream(t *testing.T) {
	body := []byte("data: [DONE]\n\n")
	resp := newHTTPResponse(http.StatusOK, string(body))
	w := httptest.NewRecorder()

	inputTokens, outputTokens, _ := streamSSEAndEstimateTokens(w, resp, body, "gpt-4")

	if inputTokens < 0 || outputTokens < 0 {
		t.Errorf("unexpected negative tokens: input=%d output=%d", inputTokens, outputTokens)
	}
}

// ── Thinking Rectifier integration ──────────────────────

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
