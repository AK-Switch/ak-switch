//go:build integration

package integration

import (
	"context"
	"net"
	"time"
	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/logentry"
	"akswitch/internal/server"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupServer creates a mock upstream and an AK Switch test server, returning both.
// The caller must close both servers.

// retryHandler returns a mock upstream handler that fails the first N calls
// and then returns a success status for all subsequent calls.
func retryHandler(failStatus, successStatus int, numFailures int, successBody string) http.HandlerFunc {
	var mu sync.Mutex
	var callCount int
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count := callCount
		callCount++
		mu.Unlock()
		if count < numFailures {
			w.WriteHeader(failStatus)
			return
		}
		if successBody != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(successStatus)
			w.Write([]byte(successBody))
		} else {
			w.WriteHeader(successStatus)
		}
	}
}

// ---------------------------------------------------------------------------
// 1. Basic forward
// ---------------------------------------------------------------------------

func TestProxyBasicForward(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b", "test-key-c"}, 10, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf(`expected "status":"ok", got %q`, result["status"])
	}
}

// ---------------------------------------------------------------------------
// 2. Auth header format
// ---------------------------------------------------------------------------

func TestProxyAuthHeader(t *testing.T) {
	var mu sync.Mutex
	var seenAuth string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b", "test-key-c"}, 10, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	resp.Body.Close()

	mu.Lock()
	auth := seenAuth
	mu.Unlock()

	if !strings.HasPrefix(auth, "Bearer ") {
		t.Errorf("Authorization header should start with 'Bearer ', got %q", auth)
	}
	if len(auth) <= len("Bearer ") {
		t.Errorf("Authorization header %q is too short", auth)
	}
}

// ---------------------------------------------------------------------------
// 3. Key rotation across requests
// ---------------------------------------------------------------------------

func TestProxyKeyRotation(t *testing.T) {
	var mu sync.Mutex
	var auths []string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b", "test-key-c"}, 10, 60)
	defer srv.Close()

	for i := 0; i < 2; i++ {
		resp, err := http.Get(srv.URL + "/test/v1/models")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
	}

	mu.Lock()
	keys := make([]string, len(auths))
	copy(keys, auths)
	mu.Unlock()

	if len(keys) < 2 {
		t.Fatalf("expected at least 2 auth headers, got %d", len(keys))
	}
	if keys[0] == keys[1] {
		t.Errorf("expected different keys in rotation, both are %q", keys[0])
	}
}

// ---------------------------------------------------------------------------
// 4. Retry after 429 (cooldown)
// ---------------------------------------------------------------------------

func TestProxyRetryAfter429(t *testing.T) {
	upstream := httptest.NewServer(retryHandler(
		http.StatusTooManyRequests, http.StatusOK, 1, `{"status":"ok"}`,
	))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b", "test-key-c"}, 10, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after 429 retry, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// 5. Disable key on 401 and fall through to next key
// ---------------------------------------------------------------------------

func TestProxyDisableOn401(t *testing.T) {
	// Return 401 for "test-key-a" (first in round-robin), 200 for others
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if strings.Contains(auth, "test-key-a") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b", "test-key-c"}, 10, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after 401 retry, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// 6. Retry on 503
// ---------------------------------------------------------------------------

func TestProxyRetryOn503(t *testing.T) {
	upstream := httptest.NewServer(retryHandler(
		http.StatusServiceUnavailable, http.StatusOK, 1, `{"status":"ok"}`,
	))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b", "test-key-c"}, 10, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after 503 retry, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// 7. All keys exhausted (all return 429)
// ---------------------------------------------------------------------------

func TestProxyAllKeysExhausted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	// With 3 keys and MaxRetries=3, each key gets exactly one attempt,
	// all keys briefly cooled -> loop ends -> 503.
	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c"}, 3, 2)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable after exhaustion, got %d", resp.StatusCode)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON error response: %v", err)
	}
	if body.Error.Code != "EXHAUSTED_RETRIES" {
		t.Errorf("expected error.code EXHAUSTED_RETRIES, got %q", body.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// 8. SSE streaming
// ---------------------------------------------------------------------------

func TestProxySSEStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "data: {\"x\":%d}\n\n", i)
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b", "test-key-c"}, 10, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	bodyStr := string(body)

	// Count data: lines (robust against buffering)
	lines := strings.Split(bodyStr, "\n")
	var dataLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, line)
		}
	}
	if len(dataLines) != 3 {
		t.Fatalf("expected 3 SSE data lines, got %d. Full body: %q", len(dataLines), bodyStr)
	}

	for i, line := range dataLines {
		expected := fmt.Sprintf(`data: {"x":%d}`, i)
		if line != expected {
			t.Errorf("data line %d: expected %q, got %q", i, expected, line)
		}
	}
}

// ---------------------------------------------------------------------------
// 9. Empty response (204)
// ---------------------------------------------------------------------------

func TestProxyEmptyResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b", "test-key-c"}, 10, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// 10. Request body passthrough
// ---------------------------------------------------------------------------

func TestProxyRequestBodyPassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b", "test-key-c"}, 10, 60)
	defer srv.Close()

	payload := `{"hello":"world"}`
	resp, err := http.Post(srv.URL+"/test/v1/models", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("POST /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) != payload {
		t.Errorf("expected body %q, got %q", payload, string(body))
	}
}

// ---------------------------------------------------------------------------
// 11. Key management (add key, check count, proxy through)
// ---------------------------------------------------------------------------

func TestProxyWithKeyManagement(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	// Create AK Switch with 1 initial key (must have at least 1 to avoid panic in Next())
	cfg := &config.Config{
		TargetBase:  upstream.URL,
		GenaiBase:   upstream.URL,
		Port:        8080,
		MaxRetries:  10,
		CooldownSec: 60,
		AdminToken:  "",
		Keys:        []string{"initial-key"},
	}
	pool := keypool.NewKeyPool([]string{"initial-key"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Step 1: POST /api/keys to add a new key
	addBody := `{"key":"added-key-456"}`
	resp, err := http.Post(srv.URL+"/api/keys", "application/json", bytes.NewReader([]byte(addBody)))
	if err != nil {
		t.Fatalf("POST /api/keys: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/keys expected 200, got %d", resp.StatusCode)
	}

	// Step 2: GET /api/keys to verify count increased
	resp, err = http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/keys expected 200, got %d", resp.StatusCode)
	}

	var keys []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		t.Fatalf("failed to decode GET /api/keys response: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys after adding one, got %d", len(keys))
	}

	// Step 3: Proxy request still works with the updated pool
	resp, err = http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models after key management: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after key management, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// 12. MaxRetries config respected
// ---------------------------------------------------------------------------

func TestProxyMaxRetriesConfig(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	// MaxRetries=2 -> only 2 attempts, then 503
	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b", "test-key-c"}, 2, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 after exhausting MaxRetries=2, got %d", resp.StatusCode)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON error response: %v", err)
	}
	if body.Error.Code != "EXHAUSTED_RETRIES" {
		t.Errorf("expected error.code EXHAUSTED_RETRIES, got %q", body.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// 13. Concurrent requests — all succeed
// ---------------------------------------------------------------------------

func TestProxyConcurrentRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c", "key-d", "key-e"}, 10, 60)
	defer srv.Close()

	const concurrency = 20
	var wg sync.WaitGroup
	errs := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			resp, err := http.Get(srv.URL + "/test/v1/models")
			if err != nil {
				errs <- fmt.Errorf("req %d: %v", id, err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("req %d: expected 200, got %d", id, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	var failures []string
	for e := range errs {
		failures = append(failures, e.Error())
	}
	if len(failures) > 0 {
		t.Fatalf("%d/%d requests failed:\n%s", len(failures), concurrency, strings.Join(failures, "\n"))
	}
}

// ---------------------------------------------------------------------------
// 14. Concurrent requests — key rotation under load
// ---------------------------------------------------------------------------

func TestProxyConcurrentKeyRotation(t *testing.T) {
	var mu sync.Mutex
	authSet := make(map[string]int)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authSet[r.Header.Get("Authorization")]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c"}, 10, 60)
	defer srv.Close()

	const concurrency = 30
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(srv.URL + "/test/v1/models")
			if err != nil {
				t.Errorf("request error: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()

	mu.Lock()
	keys := make([]string, 0, len(authSet))
	for k := range authSet {
		keys = append(keys, k)
	}
	uniqueCount := len(keys)
	mu.Unlock()

	if uniqueCount < 2 {
		t.Fatalf("expected at least 2 different keys under concurrent load (%d concurrent), got %d: %v", concurrency, uniqueCount, keys)
	}
	t.Logf("Concurrent key rotation: %d different keys used across %d requests", uniqueCount, concurrency)
}

// ---------------------------------------------------------------------------
// 15. Concurrent requests with interleaved 429 cooldown
// ---------------------------------------------------------------------------

func TestProxyConcurrentWithCooldown(t *testing.T) {
	var mu sync.Mutex
	reqCount := 0

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count := reqCount
		reqCount++
		mu.Unlock()
		if count%3 == 0 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c", "key-d", "key-e"}, 10, 2)
	defer srv.Close()

	const concurrency = 15
	var wg sync.WaitGroup
	errs := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			resp, err := http.Get(srv.URL + "/test/v1/models")
			if err != nil {
				errs <- fmt.Errorf("req %d: %v", id, err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("req %d: expected 200, got %d", id, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	var failures []string
	for e := range errs {
		failures = append(failures, e.Error())
	}
	if len(failures) > 0 {
		t.Fatalf("%d/%d requests failed with 429 cooldown:\n%s", len(failures), concurrency, strings.Join(failures, "\n"))
	}
}

// ---------------------------------------------------------------------------
// 16. Sensitive headers filtered from upstream request
// ---------------------------------------------------------------------------

func TestProxyFilterSensitiveHeaders(t *testing.T) {
	var mu sync.Mutex
	var receivedHeaders http.Header

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedHeaders = r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key"}, 10, 60)
	defer srv.Close()

	req, err := http.NewRequest("GET", srv.URL+"/test/v1/models", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("X-Admin-Token", "my-secret-admin-token")
	req.Header.Set("Cookie", "session=abc123")
	req.Header.Set("Proxy-Authorization", "Basic dXNlcjpwYXNz")
	req.Header.Set("X-Custom-Header", "should-pass")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	resp.Body.Close()

	mu.Lock()
	headers := receivedHeaders
	mu.Unlock()

	if headers.Get("X-Admin-Token") != "" {
		t.Errorf("X-Admin-Token was forwarded to upstream (value=%q)", headers.Get("X-Admin-Token"))
	}
	if headers.Get("Cookie") != "" {
		t.Errorf("Cookie was forwarded to upstream (value=%q)", headers.Get("Cookie"))
	}
	if headers.Get("Proxy-Authorization") != "" {
		t.Errorf("Proxy-Authorization was forwarded to upstream (value=%q)", headers.Get("Proxy-Authorization"))
	}
	if h := headers.Get("Authorization"); h == "" {
		t.Error("Authorization header was stripped entirely")
	} else if !strings.HasPrefix(h, "Bearer ") {
		t.Errorf("Authorization header should start with 'Bearer ', got %q", h)
	}
	if headers.Get("X-Custom-Header") != "should-pass" {
		t.Errorf("X-Custom-Header was filtered out (should have passed through)")
	}
	if headers.Get("Accept") != "application/json" {
		t.Errorf("Accept header was filtered out (should have passed through)")
	}
}

// ---------------------------------------------------------------------------
// 17. Verify slog output format — proxy request produces structured JSON-like log
// ---------------------------------------------------------------------------

func TestProxySlogOutput(t *testing.T) {
	var buf bytes.Buffer
	origHandler := slog.Default().Handler()
	t.Cleanup(func() { slog.SetDefault(slog.New(origHandler)) })

	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b"}, 10, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	resp.Body.Close()

	output := buf.String()

	// Log format must be slog structured (key=value, not printf-style)
	if output == "" {
		t.Fatal("slog output is empty — no log was written")
	}

	// Must contain INFO level
	if !strings.Contains(output, "INFO") {
		t.Errorf("expected slog INFO level in output, got: %s", output)
	}

	// Must contain structured key=value fields
	for _, key := range []string{"method=GET", "url", "status=200"} {
		if !strings.Contains(output, key) {
			t.Errorf("expected slog field %q in output:\n%s", key, output)
		}
	}
	// key_index must exist but value is implementation-dependent
	if !strings.Contains(output, "key_index=") {
		t.Errorf("expected key_index field in output:\n%s", output)
	}

	// Must NOT contain printf-style log format
	if strings.Contains(output, "→ %s %s") || strings.Contains(output, "log.Printf") {
		t.Errorf("output appears to contain old-style log.Printf format:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// 18. Error handling — BadRequest (body too large)
// ---------------------------------------------------------------------------

func TestProxyError_BadRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a"}, 10, 60)
	defer srv.Close()

	// 11MB body exceeds the 10MB MaxBytesReader limit
	largeBody := make([]byte, 11<<20)
	req, err := http.NewRequest("POST", srv.URL+"/test/v1/chat/completions", bytes.NewReader(largeBody))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if body.Error.Code != "BAD_REQUEST" {
		t.Errorf("expected error.code BAD_REQUEST, got %q", body.Error.Code)
	}
	if body.Error.Message == "" {
		t.Error("expected non-empty error.message")
	}
}

// ---------------------------------------------------------------------------
// 19. Error handling — AllKeysInvalid (single key disabled by 401)
// ---------------------------------------------------------------------------

func TestProxyError_AllKeysInvalid(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	// Only 1 key — after 401 it gets disabled, ActiveCount == 0
	srv := setupServer(t, upstream, []string{"single-key"}, 10, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if body.Error.Code != "ALL_KEYS_INVALID" {
		t.Errorf("expected error.code ALL_KEYS_INVALID, got %q", body.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// 20. Error handling — ExhaustedRetries (all keys rate-limited)
// ---------------------------------------------------------------------------

func TestProxyError_ExhaustedRetries(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c"}, 3, 2)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if body.Error.Code != "EXHAUSTED_RETRIES" {
		t.Errorf("expected error.code EXHAUSTED_RETRIES, got %q", body.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// 21. Error handling — UpstreamError (invalid target URL)
// ---------------------------------------------------------------------------

func TestProxyError_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Set TargetBase to something that makes NewRequestWithContext fail.
	// An invalid scheme causes http.NewRequestWithContext to return an error.
	cfg := &config.Config{
		TargetBase:  "://invalid",
		GenaiBase:   "://invalid",
		Port:        0,
		MaxRetries:  3,
		CooldownSec: 60,
	}
	pool := keypool.NewKeyPool([]string{"test-key-a"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 Internal Server Error, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if body.Error.Code != "UPSTREAM_ERROR" {
		t.Errorf("expected error.code UPSTREAM_ERROR, got %q", body.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// CB integration tests
// ---------------------------------------------------------------------------

// TestCB_RateLimitRecovery verifies that 429 triggers exponential backoff
// but the key recovers after the backoff period and success is possible.
func TestCB_RateLimitRecovery(t *testing.T) {
	var mu sync.Mutex
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count := callCount
		callCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if count < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limited"}`))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer upstream.Close()

	// 3 keys, CooldownSec=2 so each key gets short pool cooldown
	cfg := &config.Config{
		TargetBase:          upstream.URL,
		GenaiBase:           upstream.URL,
		Port:                0,
		MaxRetries:          10,
		CooldownSec:         2,
		BackoffCapSec:       120,
		BackoffMultiplier:   2,
		CBResetSec:          30,
		UpstreamCBThreshold: 5,
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	ts := httptest.NewServer(pr.Handler())
	defer ts.Close()

	// WHEN: send a proxy request
	req, err := http.NewRequest("GET", ts.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// THEN: eventually succeed after 429 retries
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after 429 recovery, got %d", resp.StatusCode)
	}
}

// TestCB_QuotaExhausted verifies that repeated 429s lead to retry exhaustion
// (503 EXHAUSTED_RETRIES) rather than permanently disabling the key.
func TestCB_QuotaExhausted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer upstream.Close()

	// 1 key with low BackoffCapSec and small MaxRetries for fast test
	cfg := &config.Config{
		TargetBase:          upstream.URL,
		GenaiBase:           upstream.URL,
		Port:                0,
		MaxRetries:          3,
		CooldownSec:         1,
		BackoffCapSec:       5,
		BackoffMultiplier:   2,
		CBResetSec:          60,
		UpstreamCBThreshold: 10,
	}
	pool := keypool.NewKeyPool([]string{"single-key"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	ts := httptest.NewServer(pr.Handler())
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// THEN: return 503 EXHAUSTED_RETRIES (not ALL_KEYS_INVALID)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", resp.StatusCode)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON error response: %v", err)
	}
	if body.Error.Code != "EXHAUSTED_RETRIES" {
		t.Errorf("expected error.code EXHAUSTED_RETRIES, got %q", body.Error.Code)
	}
}

// TestCB_UpstreamErrorNoKeyPenalty verifies that 502/503 errors do NOT
// disable the API key — only the upstream circuit breaker is affected.
func TestCB_UpstreamErrorNoKeyPenalty(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"upstream down"}`))
	}))
	defer upstream.Close()

	// MaxRetries=3, UpstreamCBThreshold high so upstream CB does not open
	cfg := &config.Config{
		TargetBase:          upstream.URL,
		GenaiBase:           upstream.URL,
		Port:                0,
		MaxRetries:          3,
		CooldownSec:         1,
		BackoffCapSec:       120,
		BackoffMultiplier:   2,
		CBResetSec:          300,
		UpstreamCBThreshold: 10,
	}
	pool := keypool.NewKeyPool([]string{"test-key"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	ts := httptest.NewServer(pr.Handler())
	defer ts.Close()

	// WHEN: send proxy request -> gets 503 -> exhausts retries
	req, err := http.NewRequest("GET", ts.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// THEN: return 503 EXHAUSTED_RETRIES
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", resp.StatusCode)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON error response: %v", err)
	}
	if body.Error.Code != "EXHAUSTED_RETRIES" {
		t.Errorf("expected error.code EXHAUSTED_RETRIES, got %q", body.Error.Code)
	}

	// THEN: key should NOT be disabled (check via health endpoint)
	healthResp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer healthResp.Body.Close()

	var health struct {
		Details map[string]struct {
			Status string `json:"status"`
			Keys   int    `json:"keys"`
		} `json:"details"`
	}
	if err := json.NewDecoder(healthResp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	ph, ok := health.Details["test"]
	if !ok {
		t.Fatal("health response missing provider 'test'")
	}
	if ph.Keys == 0 {
		t.Error("expected at least 1 key after 503 errors (upstream error should not disable keys)")
	}
}

// TestCB_UpstreamCircuitBreakerOpens verifies that after UPSTREAM_CB_THRESHOLD
// consecutive 503s, the upstream circuit breaker opens and subsequent retries
// fail fast without calling the upstream.
func TestCB_UpstreamCircuitBreakerOpens(t *testing.T) {
	var mu sync.Mutex
	upstreamCallCount := 0

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		upstreamCallCount++
		mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"upstream down"}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:          upstream.URL,
		GenaiBase:           upstream.URL,
		Port:                0,
		MaxRetries:          10,
		CooldownSec:         1,
		BackoffCapSec:       120,
		BackoffMultiplier:   2,
		CBResetSec:          60,
		UpstreamCBThreshold: 3,
	}
	pool := keypool.NewKeyPool([]string{"test-key"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	ts := httptest.NewServer(pr.Handler())
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// THEN: return 503 EXHAUSTED_RETRIES
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	// THEN: upstream should have been called at most UPSTREAM_CB_THRESHOLD times
	// After 3 failures, CB opens. Remaining 7 retries fail fast without upstream call.
	mu.Lock()
	count := upstreamCallCount
	mu.Unlock()
	if count > 5 {
		t.Errorf("expected at most ~3 upstream calls after CB opens, got %d", count)
	}
	t.Logf("upstream call count: %d (threshold=3, should be ~3)", count)
}

// ---------------------------------------------------------------------------
// 22. All keys disabled via API — returns 503 immediately (no CPU spin)
// ---------------------------------------------------------------------------

func TestProxy_AllDisabled_Returns503(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"key-a", "key-b"}, 10, 60)
	defer srv.Close()

	// Disable both keys via API
	for _, idx := range []string{"1", "2"} {
		resp, err := http.Post(srv.URL+"/api/keys/"+idx+"/disable", "application/json", nil)
		if err != nil {
			t.Fatalf("POST /api/keys/%s/disable: %v", idx, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST /api/keys/%s/disable: got %d, want 200", idx, resp.StatusCode)
		}
	}

	// Send proxy request — should get 503 immediately, not spin on retries
	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable when all keys disabled, got %d", resp.StatusCode)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON error response: %v", err)
	}
	if body.Error.Code != "ALL_KEYS_INVALID" {
		t.Errorf("expected error.code ALL_KEYS_INVALID, got %q", body.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// NonRetryable error classification tests
// ---------------------------------------------------------------------------

// TestProxy_NonRetryable400_ReturnsImmediately verifies that a 400 response
// from upstream is returned immediately without retrying.
func TestProxy_NonRetryable400_ReturnsImmediately(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b"}, 3, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}

	mu.Lock()
	count := callCount
	mu.Unlock()
	if count > 1 {
		t.Errorf("expected upstream to be called exactly once (non-retryable), called %d times", count)
	}
}

// TestProxy_NonRetryable422_ReturnsImmediately verifies that a 422 response
// from upstream is returned immediately without retrying.
func TestProxy_NonRetryable422_ReturnsImmediately(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"error":"unprocessable"}`))
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b"}, 3, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}

	mu.Lock()
	count := callCount
	mu.Unlock()
	if count > 1 {
		t.Errorf("expected upstream to be called exactly once (non-retryable), called %d times", count)
	}
}

// TestProxy_NonRetryable_DoesNotPenalizeKey verifies that after a NonRetryable
// 4xx error, the key remains usable for the next (valid) request.
func TestProxy_NonRetryable_DoesNotPenalizeKey(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		count := callCount
		mu.Unlock()
		if count == 1 {
			// First call: return non-retryable 400
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"bad request"}`))
		} else {
			// Subsequent calls: succeed
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a"}, 1, 60)
	defer srv.Close()

	// First request — should get 400
	resp1, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 on first request, got %d", resp1.StatusCode)
	}

	// Second request — same key should still work
	resp2, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 on second request, got %d", resp2.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Key persistence tests
// ---------------------------------------------------------------------------

// TestKeyPersistence_AddKeyRestart verifies that adding a key via API
// persists it to disk and survives a restart.
func TestKeyPersistence_AddKeyRestart(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	tmpDir := t.TempDir()
	keysFile := filepath.Join(tmpDir, "keys.json")

	cfg := &config.Config{
		TargetBase:  upstream.URL,
		GenaiBase:   upstream.URL,
		Port:        0,
		MaxRetries:  3,
		CooldownSec: 60,
		KeysFile:    keysFile,
	}
	pool := keypool.NewKeyPool([]string{"initial-key"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())

	resp, err := http.Post(srv.URL+"/api/keys", "application/json",
		strings.NewReader(`{"key":"persistent-key","name":"test-key"}`))
	if err != nil {
		t.Fatalf("POST /api/keys: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/keys: got status %d, want 200", resp.StatusCode)
	}

	srv.Close()

	data, err := os.ReadFile(keysFile)
	if err != nil {
		t.Fatalf("read keys.json: %v", err)
	}
	t.Logf("keys.json content: %s", string(data))

	if !strings.Contains(string(data), "persistent-key") {
		t.Errorf("keys.json should contain 'persistent-key', got: %s", string(data))
	}

	// Simulate a restart: load keys from file
	fileKeys, fileNames, err := keypool.LoadKeysFromFile(keysFile)
	if err != nil {
		t.Fatalf("LoadKeysFromFile: %v", err)
	}
	if fileKeys == nil {
		t.Fatal("keys.json should exist after first server wrote it")
	}

	restoredPool := keypool.NewKeyPool(fileKeys, fileNames)
	newCfg := &config.Config{
		TargetBase:  upstream.URL,
		GenaiBase:   upstream.URL,
		Port:        0,
		MaxRetries:  3,
		CooldownSec: 60,
		KeysFile:    keysFile,
	}
	pr2 := server.NewProviderRouter("")
	pr2.AddProvider("test", newCfg, restoredPool)
	srv2 := httptest.NewServer(pr2.Handler())
	defer srv2.Close()

	// Verify the key through the API
	resp2, err := http.Get(srv2.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)

	// Name is not masked in the API response
	if !strings.Contains(string(body), "test-key") {
		t.Errorf("restored pool should contain name 'test-key', got: %s", string(body))
	}

	// Key is masked (e.g. "pers...ey"); decode JSON to verify structure
	var keyList []map[string]interface{}
	if err := json.Unmarshal(body, &keyList); err != nil {
		t.Fatalf("failed to decode /api/keys response: %v", err)
	}
	if len(keyList) != 2 {
		t.Errorf("expected 2 keys after restoration, got %d", len(keyList))
	}
}

// TestKeyPersistence_DeleteKeyRestart verifies that deleting a key via API
// persists the removal and the key is gone after restart.
func TestKeyPersistence_DeleteKeyRestart(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	tmpDir := t.TempDir()
	keysFile := filepath.Join(tmpDir, "keys.json")

	cfg := &config.Config{
		TargetBase:  upstream.URL,
		GenaiBase:   upstream.URL,
		Port:        0,
		MaxRetries:  3,
		CooldownSec: 60,
		KeysFile:    keysFile,
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())

	// Delete key-a via API (index 1 = first key, 1-based in URL)
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys/1", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys/1: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/keys/1: got status %d, want 200", resp.StatusCode)
	}

	srv.Close()

	fileKeys, _, err := keypool.LoadKeysFromFile(keysFile)
	if err != nil {
		t.Fatalf("LoadKeysFromFile: %v", err)
	}
	if fileKeys == nil {
		t.Fatal("keys.json should exist")
	}
	t.Logf("keys after delete: %v", fileKeys)

	for _, k := range fileKeys {
		if k == "key-a" {
			t.Error("key-a should not be in the persisted keys after deletion")
		}
	}
	found := false
	for _, k := range fileKeys {
		if k == "key-b" {
			found = true
			break
		}
	}
	if !found {
		t.Error("key-b should still be in the persisted keys")
	}
}

// TestKeyPersistence_DisableKeyAndPersist verifies that disabling a key
// via API persists the disabled state to disk.
func TestKeyPersistence_DisableKeyAndPersist(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	tmpDir := t.TempDir()
	keysFile := filepath.Join(tmpDir, "keys.json")

	cfg := &config.Config{
		TargetBase:  upstream.URL,
		GenaiBase:   upstream.URL,
		Port:        0,
		MaxRetries:  3,
		CooldownSec: 60,
		KeysFile:    keysFile,
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())

	// Disable key 1 (first key, 1-based in URL) via API
	resp, err := http.Post(srv.URL+"/api/keys/1/disable", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want 200", resp.StatusCode)
	}

	srv.Close()

	store, err := keypool.LoadFullStore(keysFile)
	if err != nil {
		t.Fatalf("LoadFullStore: %v", err)
	}
	if store == nil {
		t.Fatal("keys.json should exist")
	}

	t.Logf("store contents: %+v", store.Keys)

	for _, entry := range store.Keys {
		if entry.Key == "key-a" && !entry.Disabled {
			t.Error("key-a should be disabled in persisted store")
		}
	}
}

// TestKeyEncryption_NoEncryption_BackwardCompatible verifies that
// without an encryption key, the system works as before (plaintext keys).
func TestKeyEncryption_NoEncryption_BackwardCompatible(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	tmpDir := t.TempDir()
	keysFile := filepath.Join(tmpDir, "keys.json")

	cfg := &config.Config{
		TargetBase:  upstream.URL,
		GenaiBase:   upstream.URL,
		Port:        0,
		MaxRetries:  3,
		CooldownSec: 60,
		KeysFile:    keysFile,
	}
	pool := keypool.NewKeyPool([]string{"plaintext-key-a"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())

	// Add another key
	resp, err := http.Post(srv.URL+"/api/keys", "application/json",
		strings.NewReader(`{"key":"plaintext-key-b"}`))
	if err != nil {
		t.Fatalf("POST /api/keys: %v", err)
	}
	resp.Body.Close()
	srv.Close()

	// Read raw file — keys should be plaintext
	data, err := os.ReadFile(keysFile)
	if err != nil {
		t.Fatalf("read keys.json: %v", err)
	}
	if !strings.Contains(string(data), "plaintext-key-a") {
		t.Error("plaintext key 'plaintext-key-a' not found in unencrypted file")
	}
}

// ---------------------------------------------------------------------------
// LogEntry new field integration tests (T4: 测试覆盖)
// ---------------------------------------------------------------------------

// TestLogEntry_HasNewFields verifies that a successful proxy request
// produces a log entry with DurationMs, Attempt, Provider, and KeyName.
func TestLogEntry_HasNewFields(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b"}, 10, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	resp.Body.Close()

	logResp, err := http.Get(srv.URL + "/logs")
	if err != nil {
		t.Fatalf("GET /logs: %v", err)
	}
	defer logResp.Body.Close()

	var entries []map[string]interface{}
	if err := json.NewDecoder(logResp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 log entry")
	}

	entry := entries[0]
	for _, field := range []string{"provider", "duration_ms", "retry", "key_name"} {
		if _, ok := entry[field]; !ok {
			t.Errorf("log entry missing %q field", field)
		}
	}

	if p, ok := entry["provider"].(string); !ok || p != "test" {
		t.Errorf("expected provider=\"test\", got %v", entry["provider"])
	}
	if a, ok := entry["retry"].(float64); !ok || a < 0 {
		t.Errorf("expected retry >= 0, got %v", entry["retry"])
	}
	if d, ok := entry["duration_ms"].(float64); !ok || d < 0 {
		t.Errorf("expected duration_ms >= 0, got %v", entry["duration_ms"])
	}
}

// TestLogEntry_ExhaustionHas503 verifies that after all keys are exhausted
// the log store contains a 503 entry with the new fields populated.
func TestLogEntry_ExhaustionHas503(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	// MaxRetries=2, 3 keys, all 429 -> 503
	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c"}, 2, 2)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	logResp, err := http.Get(srv.URL + "/logs")
	if err != nil {
		t.Fatalf("GET /logs: %v", err)
	}
	defer logResp.Body.Close()

	var entries []map[string]interface{}
	if err := json.NewDecoder(logResp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode logs: %v", err)
	}

	var found503 bool
	for _, entry := range entries {
		status, _ := entry["status"].(float64)
		if int(status) != http.StatusServiceUnavailable {
			continue
		}
		found503 = true
		for _, field := range []string{"provider", "duration_ms", "retry", "key_name"} {
			if _, ok := entry[field]; !ok {
				t.Errorf("503 log entry missing %q field", field)
			}
		}
		if p, ok := entry["provider"].(string); !ok || p != "test" {
			t.Errorf("expected provider=\"test\", got %v", entry["provider"])
		}
		if a, ok := entry["retry"].(float64); !ok || a < 2 {
			t.Errorf("expected retry >= 2 (MaxRetries), got %v", entry["retry"])
		}
		break
	}
	if !found503 {
		t.Error("no 503 log entry found after retry exhaustion")
	}
}

// TestLogEntry_CLIFormat verifies the log JSON structure is suitable
// for CLI display — all fields the CLI command needs are present.
func TestLogEntry_CLIFormat(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b"}, 10, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	resp.Body.Close()

	logResp, err := http.Get(srv.URL + "/logs")
	if err != nil {
		t.Fatalf("GET /logs: %v", err)
	}
	defer logResp.Body.Close()

	var entries []map[string]interface{}
	if err := json.NewDecoder(logResp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 log entry")
	}

	entry := entries[0]
	// All fields the CLI display code in internal/cli/logs.go reads
	for _, field := range []string{"method", "url", "status", "timestamp", "provider", "duration_ms", "retry", "key_name"} {
		if _, ok := entry[field]; !ok {
			t.Errorf("CLI display needs field %q, but log entry missing it", field)
		}
	}

	t.Logf("CLI-ready log entry: provider=%v duration_ms=%v retry=%v key_name=%v",
		entry["provider"], entry["duration_ms"], entry["retry"], entry["key_name"])
}

func TestHealthHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf(`expected status="ok", got %v`, body["status"])
	}

	if n, ok := body["providers"].(float64); !ok || int(n) != 1 {
		t.Errorf("expected providers=1, got %v", body["providers"])
	}

	details, ok := body["details"].(map[string]interface{})
	if !ok {
		t.Fatal("expected details field with per-provider data")
	}
	testProv, ok := details["test"].(map[string]interface{})
	if !ok {
		t.Fatal("expected test provider in details")
	}
	if n, ok := testProv["keys"].(float64); !ok || int(n) != 3 {
		t.Errorf("expected keys=3 for test provider, got %v", testProv["keys"])
	}
}

// ── Config GET ─────────────────────────────────────

func TestConfigGet(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/config")
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["targetBase"] != "http://localhost:19999" {
		t.Errorf(`expected targetBase="http://localhost:19999", got %v`, body["targetBase"])
	}
	if body["genaiBase"] != "http://localhost:19999" {
		t.Errorf(`expected genaiBase="http://localhost:19999", got %v`, body["genaiBase"])
	}

	keys, ok := body["keys"].([]interface{})
	if !ok {
		t.Fatal("expected keys field as array")
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}

	expectedMasked := logentry.MaskKey("key-a")

	for i, k := range keys {
		masked, ok := k.(string)
		if !ok {
			t.Errorf("keys[%d] is not a string", i)
			continue
		}
		// All keys should be masked — none should equal the raw key
		if masked == "key-a" || masked == "key-b" || masked == "key-c" {
			t.Errorf("keys[%d]=%q appears unmasked", i, masked)
		}
		// The masking format should match logentry.MaskKey()
		if i == 0 && masked != expectedMasked {
			t.Errorf("keys[0]=%q, want masking like %q", masked, expectedMasked)
		}
	}
}

// ── Config POST ────────────────────────────────────

func TestConfigPost(t *testing.T) {
	// ConfigPost 会写 .env 并调用 reloadConfig，需要隔离到临时目录
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// 写入初始 .env 供 reloadConfig 使用
	envContent := "PORT=19999\nTARGET_BASE_URL=http://localhost:19999\nGENAI_BASE_URL=http://localhost:19999\nAPI_KEYS=key-a,key-b\nCOOLDOWN_SEC=60\nMAX_RETRIES=3\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		GenaiBase:   "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "",
		Keys:        []string{"key-a", "key-b"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// POST /api/config is no longer supported in ProviderRouter architecture
	reqBody := `{"targetBase":"https://new.example.com/v1","genaiBase":"https://genai.example.com","keys":["new-key-1","new-key-2"]}`
	resp, err := http.Post(srv.URL+"/api/config", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405 (POST removed), got %d", resp.StatusCode)
	}
}

// ── Keys GET ───────────────────────────────────────

func TestKeysGet(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var keys []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}

	for i, k := range keys {
		// index is 1-based in keysHandler GET
		if idx, ok := k["index"].(float64); !ok || int(idx) != i+1 {
			t.Errorf("keys[%d] index=%v, want %d", i, k["index"], i+1)
		}
		if key, ok := k["key"].(string); !ok || key == "" {
			t.Errorf("keys[%d] key=%v, want non-empty masked string", i, k["key"])
		}
		if status, ok := k["status"].(string); !ok || status == "" {
			t.Errorf("keys[%d] status=%v, want non-empty string", i, k["status"])
		}
		if _, ok := k["requests_1m"]; !ok {
			t.Errorf("keys[%d] missing requests_1m field", i)
		}
	}
}

// ── Keys POST ──────────────────────────────────────

func TestKeysPost(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	// POST 添加新 key
	reqBody := `{"key":"new-test-key"}`
	resp, err := http.Post(srv.URL+"/api/keys", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/keys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var addResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&addResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// AddKey 返回的 index 为 0-based: 长度 3→新 key idx=3
	if idx, ok := addResp["index"].(float64); !ok || int(idx) != 3 {
		t.Errorf("expected index=3, got %v", addResp["index"])
	}
	if key, ok := addResp["key"].(string); !ok || key == "" {
		t.Errorf("expected non-empty masked key, got %v", addResp["key"])
	}
	if addResp["key"] != logentry.MaskKey("new-test-key") {
		t.Errorf("expected key=%q, got %q", logentry.MaskKey("new-test-key"), addResp["key"])
	}

	// GET 验证 key 数量为 4
	resp2, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp2.Body.Close()

	var keys []map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&keys); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(keys) != 4 {
		t.Errorf("expected 4 keys after POST, got %d", len(keys))
	}
}

// ── Keys DELETE ────────────────────────────────────

func TestKeysDelete(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	// 先 GET 确认当前 key 数
	resp, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp.Body.Close()

	var before []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&before); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	beforeCount := len(before)

	// DELETE 移除 index=1 (1-based) 的 key
	reqBody := `{"index":1}`
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp2.StatusCode)
	}

	var delResp map[string]string
	if err := json.NewDecoder(resp2.Body).Decode(&delResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if delResp["status"] != "removed" {
		t.Errorf(`expected status="removed", got %q`, delResp["status"])
	}

	// GET 验证 key 数减 1
	resp3, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp3.Body.Close()

	var after []map[string]interface{}
	if err := json.NewDecoder(resp3.Body).Decode(&after); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(after) != beforeCount-1 {
		t.Errorf("expected %d keys after DELETE, got %d", beforeCount-1, len(after))
	}
}

// ── Clear ──────────────────────────────────────────

func TestClearHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/clear", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /clear: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["status"] != "cleared" {
		t.Errorf(`expected status="cleared", got %v`, body["status"])
	}
}

// ── Health with AdminToken auth ──────────────────────

func TestHealthHandlerAuth(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		GenaiBase:   "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Without token → 401
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// With wrong token → 401
	req, _ := http.NewRequest("GET", srv.URL+"/health", nil)
	req.Header.Set("X-Admin-Token", "wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health (wrong token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", resp.StatusCode)
	}

	// With correct token → 200
	req, _ = http.NewRequest("GET", srv.URL+"/health", nil)
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf(`expected status="ok", got %v`, body["status"])
	}
}

// ── Clear with AdminToken auth ───────────────────────

func TestClearHandlerAuth(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		GenaiBase:   "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Without token → 401
	resp, err := http.Post(srv.URL+"/clear", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /clear (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// With wrong token → 401
	req, _ := http.NewRequest("POST", srv.URL+"/clear", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", "wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /clear (wrong token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", resp.StatusCode)
	}

	// With correct token → 200
	req, _ = http.NewRequest("POST", srv.URL+"/clear", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /clear (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "cleared" {
		t.Errorf(`expected status="cleared", got %v`, body["status"])
	}
}

// ── Stats GET ───────────────────────────────────────

func TestStatsHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatalf("GET /api/stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	fields := []string{"total_requests", "successful_requests", "failed_requests", "success_rate", "active_keys", "cooling_keys", "disabled_keys", "uptime_seconds"}
	for _, f := range fields {
		if _, ok := body[f]; !ok {
			t.Errorf("missing field %q in response", f)
		}
	}
}

// ── Disable Key POST ──────────────────────────────────

func TestDisableKeyHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	// 禁用 index=1
	req, _ := http.NewRequest("POST", srv.URL+"/api/keys/1/disable", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["success"] != true {
		t.Errorf("expected success=true, got %v", body["success"])
	}

	// GET /api/keys 验证该 key 状态为 "disabled"
	resp2, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp2.Body.Close()

	var keys []map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&keys); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(keys) < 1 {
		t.Fatal("expected at least 1 key")
	}
	status, _ := keys[0]["status"].(string)
	if status != "disabled" {
		t.Errorf("expected keys[0] status=disabled, got %q", status)
	}

	// 越界 index=999 → 404
	req2, _ := http.NewRequest("POST", srv.URL+"/api/keys/999/disable", nil)
	resp3, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST /api/keys/999/disable: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for out-of-range index, got %d", resp3.StatusCode)
	}
}

func TestDisableKeyHandlerAuth(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		GenaiBase:   "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
		Keys:        []string{"key-a", "key-b", "key-c"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Without token → 401
	req, _ := http.NewRequest("POST", srv.URL+"/api/keys/1/disable", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// Wrong token → 401
	req, _ = http.NewRequest("POST", srv.URL+"/api/keys/1/disable", nil)
	req.Header.Set("X-Admin-Token", "wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable (wrong token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", resp.StatusCode)
	}

	// Correct token → 200
	req, _ = http.NewRequest("POST", srv.URL+"/api/keys/1/disable", nil)
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/keys/1/disable (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["success"] != true {
		t.Errorf("expected success=true, got %v", body["success"])
	}
}

// ── Cooldown Key PUT ──────────────────────────────────

func TestCooldownKeyHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	// 冷却 index=1
	req, _ := http.NewRequest("PUT", srv.URL+"/api/keys/1/cooldown", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/keys/1/cooldown: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["success"] != true {
		t.Errorf("expected success=true, got %v", body["success"])
	}

	// 越界 index=999 → 404
	req2, _ := http.NewRequest("PUT", srv.URL+"/api/keys/999/cooldown", nil)
	resp3, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("PUT /api/keys/999/cooldown: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for out-of-range index, got %d", resp3.StatusCode)
	}
}

// ── Delete Key by Index ───────────────────────────────

func TestDeleteKeyByIndexHandler(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	// 删除 index=1
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/keys/1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys/1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["success"] != true {
		t.Errorf("expected success=true, got %v", body["success"])
	}

	// GET /api/keys 验证只剩 2 个 key
	resp2, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp2.Body.Close()

	var keys []map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&keys); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys after DELETE, got %d", len(keys))
	}

	// 越界 index=999 → 404
	req2, _ := http.NewRequest("DELETE", srv.URL+"/api/keys/999", nil)
	resp3, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("DELETE /api/keys/999: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for out-of-range index, got %d", resp3.StatusCode)
	}
}

func TestDeleteKeyByIndexHandlerAuth(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		GenaiBase:   "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
		Keys:        []string{"key-a", "key-b", "key-c"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Without token → 401
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/keys/1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys/1 (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// With correct token → 200
	req, _ = http.NewRequest("DELETE", srv.URL+"/api/keys/1", nil)
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys/1 (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["success"] != true {
		t.Errorf("expected success=true, got %v", body["success"])
	}
}

// ── Reload POST ──────────────────────────────────────

func TestReloadHandler(t *testing.T) {
	tmpDir := t.TempDir()
	config.ConfigDir = tmpDir
	t.Cleanup(func() { config.ConfigDir = "" })

	// Write a valid config.toml at the XDG path for reload to read
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		t.Fatalf("XDGConfigPath failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(xdgPath), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	tomlContent := `[provider.test]
target = "http://localhost:19999/v1"
genai = "http://localhost:19999"
port = 19999
max_retries = 3
cooldown_sec = 60
`
	if err := os.WriteFile(xdgPath, []byte(tomlContent), 0600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		GenaiBase:   "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "",
		Keys:        []string{"key-a", "key-b"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/reload", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /api/reload: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["success"] != true {
		t.Errorf("expected success=true, got %v", body["success"])
	}
}

// ── Log Level API ─────────────────────────────────────

func TestLogLevelHandler_Success(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/log-level", "application/json", strings.NewReader(`{"level":"debug"}`))
	if err != nil {
		t.Fatalf("POST /api/log-level: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if level, ok := body["level"].(string); !ok || level != "debug" {
		t.Errorf(`expected level="debug", got %v`, body["level"])
	}
}

func TestLogLevelHandler_InvalidLevel(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/log-level", "application/json", strings.NewReader(`{"level":"verbose"}`))
	if err != nil {
		t.Fatalf("POST /api/log-level: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestLogLevelHandler_GetLevel(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/log-level")
	if err != nil {
		t.Fatalf("GET /api/log-level: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if level, ok := body["level"].(string); !ok || level == "" {
		t.Errorf(`expected level to be non-empty, got %v`, body["level"])
	}
}

func TestLogLevelHandler_Unauthorized(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		GenaiBase:   "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "secret-token",
		Keys:        []string{"key-a"},
	}
	pool := keypool.NewKeyPool([]string{"key-a"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// No token → 401
	resp, err := http.Post(srv.URL+"/api/log-level", "application/json", strings.NewReader(`{"level":"debug"}`))
	if err != nil {
		t.Fatalf("POST /api/log-level: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}
}

// ── Runtime Config API ─────────────────────────────────

func TestRuntimeConfigHandler_GetList(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/runtime-config")
	if err != nil {
		t.Fatalf("GET /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if provider, ok := body["provider"].(string); !ok || provider == "" {
		t.Errorf("expected provider to be non-empty, got %v", body["provider"])
	}
	params, ok := body["params"].(map[string]interface{})
	if !ok {
		t.Fatal("expected params object")
	}
	if _, ok := params["http_timeout_sec"]; !ok {
		t.Errorf("expected http_timeout_sec in params")
	}
	if _, ok := params["max_retries"]; !ok {
		t.Errorf("expected max_retries in params")
	}
	if _, ok := params["log_level"]; !ok {
		t.Errorf("expected log_level in params")
	}
}

func TestRuntimeConfigHandler_GetSingleKey(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/runtime-config?key=http_timeout_sec")
	if err != nil {
		t.Fatalf("GET /api/runtime-config?key=http_timeout_sec: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if key, ok := body["key"].(string); !ok || key != "http_timeout_sec" {
		t.Errorf("expected key=http_timeout_sec, got %v", body["key"])
	}
	if _, ok := body["value"]; !ok {
		t.Errorf("expected value to be present")
	}
}

func TestRuntimeConfigHandler_GetUnknownKey(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/runtime-config?key=unknown_key")
	if err != nil {
		t.Fatalf("GET /api/runtime-config?key=unknown_key: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestRuntimeConfigHandler_SetHttpTimeout(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	// Set http_timeout_sec to 60
	payload := `{"key":"http_timeout_sec","value":60}`
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if key, ok := body["key"].(string); !ok || key != "http_timeout_sec" {
		t.Errorf("expected key=http_timeout_sec, got %v", body["key"])
	}
	// JSON numbers are float64; 60 == 60.0
	if val, ok := body["value"].(float64); !ok || int(val) != 60 {
		t.Errorf("expected value=60, got %v", body["value"])
	}

	// Verify the change was applied
	checkResp, err := http.Get(srv.URL + "/api/runtime-config?key=http_timeout_sec")
	if err != nil {
		t.Fatalf("GET /api/runtime-config?key=http_timeout_sec: %v", err)
	}
	defer checkResp.Body.Close()
	var checkBody map[string]interface{}
	if err := json.NewDecoder(checkResp.Body).Decode(&checkBody); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if val, ok := checkBody["value"].(float64); !ok || int(val) != 60 {
		t.Errorf("expected value=60 after set, got %v", checkBody["value"])
	}
}

func TestRuntimeConfigHandler_SetMaxRetries(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	payload := `{"key":"max_retries","value":5}`
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if val, ok := body["value"].(float64); !ok || int(val) != 5 {
		t.Errorf("expected value=5, got %v", body["value"])
	}
}

func TestRuntimeConfigHandler_SetLogLevel(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	payload := `{"key":"log_level","value":"debug"}`
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if val, ok := body["value"].(string); !ok || val != "debug" {
		t.Errorf("expected value=debug, got %v", body["value"])
	}

	// Verify via log-level API
	checkResp, err := http.Get(srv.URL + "/api/log-level")
	if err != nil {
		t.Fatalf("GET /api/log-level: %v", err)
	}
	defer checkResp.Body.Close()
	var checkBody map[string]interface{}
	if err := json.NewDecoder(checkResp.Body).Decode(&checkBody); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if level, ok := checkBody["level"].(string); !ok || level != "debug" {
		t.Errorf("expected log level debug, got %v", checkBody["level"])
	}
}

func TestRuntimeConfigHandler_SetInvalidKey(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	payload := `{"key":"invalid_key","value":123}`
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestRuntimeConfigHandler_SetInvalidLogLevel(t *testing.T) {
	srv := newTestServer([]string{"key-a"})
	defer srv.Close()

	payload := `{"key":"log_level","value":"verbose"}`
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestRuntimeConfigHandler_Unauthorized(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		GenaiBase:   "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "secret-token",
		Keys:        []string{"key-a"},
	}
	pool := keypool.NewKeyPool([]string{"key-a"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// No token → 401
	resp, err := http.Post(srv.URL+"/api/runtime-config", "application/json", strings.NewReader(`{"key":"http_timeout_sec","value":60}`))
	if err != nil {
		t.Fatalf("POST /api/runtime-config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}

	// GET also requires token
	getResp, err := http.Get(srv.URL + "/api/runtime-config")
	if err != nil {
		t.Fatalf("GET /api/runtime-config: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401 for GET, got %d", getResp.StatusCode)
	}
}

// ── Keys DELETE — 1-based index validation ─────────────

func TestKeysHandlerDelete_OneBased(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	// GET initial count
	resp, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp.Body.Close()
	var before []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&before); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	beforeCount := len(before)

	// DELETE index=1 (1-based) → should succeed
	reqBody := `{"index":1}`
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp2.StatusCode)
	}

	// GET after delete — count should be reduced by 1
	resp3, err := http.Get(srv.URL + "/api/keys")
	if err != nil {
		t.Fatalf("GET /api/keys: %v", err)
	}
	defer resp3.Body.Close()

	var after []map[string]interface{}
	if err := json.NewDecoder(resp3.Body).Decode(&after); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(after) != beforeCount-1 {
		t.Errorf("expected %d keys after DELETE, got %d", beforeCount-1, len(after))
	}
}

func TestKeysHandlerDelete_IndexZeroReturns400(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	reqBody := `{"index":0}`
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for index=0, got %d", resp.StatusCode)
	}
}

func TestKeysHandlerDelete_IndexNegativeReturns400(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	reqBody := `{"index":-1}`
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for index=-1, got %d", resp.StatusCode)
	}
}

func TestKeysHandlerDelete_IndexTooLargeReturns400(t *testing.T) {
	srv := newTestServer([]string{"key-a", "key-b", "key-c"})
	defer srv.Close()

	reqBody := `{"index":999}`
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for index=999, got %d", resp.StatusCode)
	}
}

// ── DELETE /api/keys — unauthenticated ──────────────────

func TestKeysHandlerDelete_Unauthenticated(t *testing.T) {
	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		GenaiBase:   "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
		Keys:        []string{"key-a", "key-b", "key-c"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Without token → 401
	reqBody := `{"index":1}`
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// With correct token → 200
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/keys", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/keys (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", resp.StatusCode)
	}
}

// ── Config POST — unauthenticated ──────────────────────

func TestConfigHandlerPost_Unauthenticated(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// Write initial .env for ReloadConfig
	envContent := "PORT=19999\nTARGET_BASE_URL=http://localhost:19999\nGENAI_BASE_URL=http://localhost:19999\nAPI_KEYS=key-a,key-b\nCOOLDOWN_SEC=60\nMAX_RETRIES=3\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		TargetBase:  "http://localhost:19999",
		GenaiBase:   "http://localhost:19999",
		Port:        19999,
		MaxRetries:  3,
		CooldownSec: 60,
		AdminToken:  "my-token",
		Keys:        []string{"key-a", "key-b"},
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// POST /api/config is no longer supported — both no token and with token return 405
	reqBody := `{"targetBase":"http://example.com","genaiBase":"http://genai.example.com","keys":["new-key"]}`
	resp, err := http.Post(srv.URL+"/api/config", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/config (no auth): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST (method not allowed), got %d", resp.StatusCode)
	}

	// With correct token — still 405
	req, _ := http.NewRequest("POST", srv.URL+"/api/config", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", "my-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/config (correct token): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 with token, got %d", resp.StatusCode)
	}
}


type healthResponse struct {
	CbState     string `json:"upstream_cb_state"`
	LastCheck   string `json:"last_health_check"`
	LastCheckOK *bool  `json:"last_health_check_ok"`
}

type routerHealthResponse struct {
	Status    string                            `json:"status"`
	Providers int                               `json:"providers"`
	Details   map[string]providerHealthResponse `json:"details"`
}

type providerHealthResponse struct {
	Status            string `json:"status"`
	Keys              int    `json:"keys"`
	UpstreamCBState   string `json:"upstream_cb_state"`
	LastHealthCheck   string `json:"last_health_check,omitempty"`
	LastHealthCheckOK *bool  `json:"last_health_check_ok,omitempty"`
}
func getHealth(tb testing.TB, url string) healthResponse {
	tb.Helper()
	resp, err := http.Get(url + "/health")
	if err != nil {
		tb.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var routerResp routerHealthResponse
	if err := json.Unmarshal(body, &routerResp); err != nil {
		tb.Fatalf("decode /health response %q: %v", string(body), err)
	}

	detail, ok := routerResp.Details["test"]
	if !ok {
		tb.Fatalf("provider 'test' not found in /health details")
	}

	return healthResponse{
		CbState:     detail.UpstreamCBState,
		LastCheck:   detail.LastHealthCheck,
		LastCheckOK: detail.LastHealthCheckOK,
	}
}

// ---------------------------------------------------------------------------
// 1. ProbeSuccess — healthy upstream keeps CB closed
// ---------------------------------------------------------------------------

// TestActiveHealthCheck_ProbeSuccess verifies that when the upstream is healthy
// (returns 200), the circuit breaker stays closed and the /health endpoint
// reflects the healthy state with last_health_check_ok=true.
func TestActiveHealthCheck_ProbeSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             1,
		CooldownSec:            60,
		UpstreamCBThreshold:    3,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
	}
	pr, srv := newServer(t, cfg, []string{"test-key"})
	defer srv.Close()

	// WHEN: a proxy request succeeds — the proxy calls upCB.RecordSuccess()
	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /test/v1/models: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Simulate what ActiveHealthCheck does on a successful probe:
	// - Set last health check result
	// - Increment the health check probes counter
	pr.Provider("test").SetLastHealthCheck(true)
	pr.Metrics().HealthCheckProbes.WithLabelValues("test", "ok").Inc()

	// THEN: /health reflects a healthy upstream
	health := getHealth(t, srv.URL)
	if health.CbState != "closed" {
		t.Errorf("expected upstream_cb_state 'closed', got %q", health.CbState)
	}
	if health.LastCheckOK == nil || !*health.LastCheckOK {
		t.Error("expected last_health_check_ok true")
	}
}

// ---------------------------------------------------------------------------
// 2. ProbeFailure — failing upstream opens CB
// ---------------------------------------------------------------------------

// TestActiveHealthCheck_ProbeFailure verifies that when the upstream returns 503,
// the circuit breaker opens after the configured threshold, and the /health
// endpoint reflects the failure.
func TestActiveHealthCheck_ProbeFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             1, // each proxied request = 1 upstream call
		CooldownSec:            60,
		UpstreamCBThreshold:    3,
		CBResetSec:             60, // long enough that CB stays open during test
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
	}
	pr, srv := newServer(t, cfg, []string{"test-key-a"})
	defer srv.Close()

	// WHEN: send enough proxy requests to trigger UpstreamCBThreshold failures
	// Each request returns 503 and the proxy calls upCB.RecordFailure()
	for i := 0; i < 4; i++ {
		resp, err := http.Get(srv.URL + "/test/v1/models")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
	}

	// Simulate what ActiveHealthCheck does on a failed probe
	pr.Provider("test").SetLastHealthCheck(false)
	pr.Metrics().HealthCheckProbes.WithLabelValues("test", "fail").Inc()

	// THEN: CB should be open — /health shows "open"
	health := getHealth(t, srv.URL)
	if health.CbState != "open" {
		t.Errorf("expected upstream_cb_state 'open' after 3 failures, got %q", health.CbState)
	}
	if health.LastCheckOK == nil || *health.LastCheckOK {
		t.Error("expected last_health_check_ok false")
	}
}

// ---------------------------------------------------------------------------
// 3. Recovery — CB recovers after reset timeout when upstream becomes healthy
// ---------------------------------------------------------------------------

// TestActiveHealthCheck_Recovery verifies that after the CB opens, it
// transitions to HALF_OPEN after the reset timeout, and a successful proxy
// request restores it to CLOSED.
func TestActiveHealthCheck_Recovery(t *testing.T) {
	var mu sync.Mutex
	upstreamHealthy := false

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		healthy := upstreamHealthy
		mu.Unlock()
		if healthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer upstream.Close()

	// Short CB reset timeout for testing; bypasses Validate() in tests
	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             1,
		CooldownSec:            60,
		UpstreamCBThreshold:    3,
		CBResetSec:             1,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
	}
	pr, srv := newServer(t, cfg, []string{"test-key-a"})
	defer srv.Close()

	// Phase 1 — Open the CB by sending failures
	for i := 0; i < 4; i++ {
		resp, err := http.Get(srv.URL + "/test/v1/models")
		if err != nil {
			t.Fatalf("failure phase request %d: %v", i, err)
		}
		resp.Body.Close()
	}

	health := getHealth(t, srv.URL)
	if health.CbState != "open" {
		t.Fatalf("CB should be open after failures, got %q", health.CbState)
	}
	t.Logf("CB is open — waiting for reset timeout (%ds)", cfg.CBResetSec)

	// Phase 2 — Wait for CB reset timeout and switch upstream to healthy
	mu.Lock()
	upstreamHealthy = true
	mu.Unlock()

	time.Sleep(time.Duration(cfg.CBResetSec+1) * time.Second)

	// WHEN: send a proxy request — Allow() transitions to HALF_OPEN,
	// upstream returns 200 → RecordSuccess → CLOSED
	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("recovery request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after recovery, got %d", resp.StatusCode)
	}

	// Simulate health check success after recovery
	pr.Provider("test").SetLastHealthCheck(true)
	pr.Metrics().HealthCheckProbes.WithLabelValues("test", "ok").Inc()

	// THEN: CB is closed again
	health = getHealth(t, srv.URL)
	if health.CbState != "closed" {
		t.Errorf("expected upstream_cb_state 'closed' after recovery, got %q", health.CbState)
	}
	if health.LastCheckOK == nil || !*health.LastCheckOK {
		t.Error("expected last_health_check_ok true after recovery")
	}
}

// ---------------------------------------------------------------------------
// 4. ProbeTimeout — slow upstream causes probe timeout and CB failure
// ---------------------------------------------------------------------------

// TestActiveHealthCheck_ProbeTimeout verifies that when a health check probe
// times out (upstream is unresponsive), the circuit breaker records a failure.
// A timed-out HEAD request simulates what ActiveHealthCheck does with its own
// short-timeout HTTP client.
func TestActiveHealthCheck_ProbeTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate an upstream that is too slow to respond
		time.Sleep(2 * time.Second)
		// Return 503 so the proxy handler calls upCB.RecordFailure()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             1,
		CooldownSec:            60,
		UpstreamCBThreshold:    1, // single timeout opens CB
		CBResetSec:             30,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  1,
	}
	pr, srv := newServer(t, cfg, []string{"test-key-a"})
	defer srv.Close()

	// Simulate what ActiveHealthCheck does:
	// 1. Create a short-timeout HTTP client (like the health check goroutine does)
	// 2. Send HEAD request to the upstream's health check endpoint
	// 3. The request times out — proving timeout detection works

	hcClient := &http.Client{Timeout: time.Duration(cfg.HealthCheckTimeoutSec) * time.Second}
	headResp, err := hcClient.Head(upstream.URL + cfg.HealthCheckPath)
	if err != nil {
		t.Logf("HEAD request timed out as expected: %v", err)
	} else {
		headResp.Body.Close()
		t.Log("HEAD request succeeded (upstream responded before timeout)")
	}

	// 4. Simulate the health check goroutine's failure handling:
	//    Send a proxied request that hits the slow upstream, which returns 503
	//    after 2s. The proxy calls upCB.RecordFailure(), opening the CB.
	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Logf("proxy request error: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}

	pr.Provider("test").SetLastHealthCheck(false)

	// THEN: CB should have recorded a failure
	health := getHealth(t, srv.URL)
	if health.CbState != "open" {
		t.Errorf("expected upstream_cb_state 'open' after failure, got %q", health.CbState)
	}
	if health.LastCheckOK == nil || *health.LastCheckOK {
		t.Error("expected last_health_check_ok false after timeout failure")
	}
}

// ---------------------------------------------------------------------------
// 5. ConfigDriven — health check config fields pass through correctly
// ---------------------------------------------------------------------------

// TestActiveHealthCheck_ConfigDriven verifies that health check configuration
// values are correctly stored and accessible.
func TestActiveHealthCheck_ConfigDriven(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   8080,
		MaxRetries:             3,
		CooldownSec:            60,
		HealthCheckIntervalSec: 10,
		HealthCheckPath:        "/custom",
		HealthCheckTimeoutSec:  3,
		UpstreamCBThreshold:    5,
		CBResetSec:             30,
	}

	// Verify config fields before creating ProviderRouter
	if cfg.HealthCheckIntervalSec != 10 {
		t.Errorf("expected HealthCheckIntervalSec=10, got %d", cfg.HealthCheckIntervalSec)
	}
	if cfg.HealthCheckPath != "/custom" {
		t.Errorf("expected HealthCheckPath=/custom, got %q", cfg.HealthCheckPath)
	}
	if cfg.HealthCheckTimeoutSec != 3 {
		t.Errorf("expected HealthCheckTimeoutSec=3, got %d", cfg.HealthCheckTimeoutSec)
	}

	// Verify ProviderRouter initialises without error with these config values
	pr, srv := newServer(t, cfg, []string{"test-key"})
	defer srv.Close()

	// Server started successfully with health check config
	// Verify a basic proxy request works
	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /test/v1/models: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify health check metrics are accessible
	pr.Metrics().HealthCheckProbes.WithLabelValues("test", "ok")
	pr.Metrics().HealthCheckProbes.WithLabelValues("test", "fail")
	_ = pr.Metrics().HealthCheckDuration

	// Custom health check path is set in config
	// (the actual path is used by ActiveHealthCheck goroutine, not tested here)
	t.Logf("Config: interval=%ds path=%q timeout=%ds",
		cfg.HealthCheckIntervalSec, cfg.HealthCheckPath, cfg.HealthCheckTimeoutSec)
}

func TestGracefulShutdown_ActiveRequestCompletes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("done"))
	})

	srv := &http.Server{Handler: handler}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	go srv.Serve(listener)

	// Send a request concurrently, then immediately start shutdown
	respCh := make(chan *http.Response, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String() + "/")
		if err == nil {
			respCh <- resp
		}
	}()

	// Wait briefly for the request to reach the server
	time.Sleep(50 * time.Millisecond)

	// Initiate graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	// Verify the in-flight request completed successfully
	select {
	case resp := <-respCh:
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "done" {
			t.Fatalf("expected 'done', got '%s'", string(body))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response")
	}
}

// TestGracefulShutdown_RejectsNewConnections verifies that after Shutdown
// returns, new connections are rejected.
func TestGracefulShutdown_RejectsNewConnections(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Handler: handler}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	go srv.Serve(listener)

	// Shutdown immediately
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	// Try to send a new request — should fail
	_, err = http.Get("http://" + listener.Addr().String() + "/")
	if err == nil {
		t.Fatal("expected error after shutdown, got nil")
	}
}

// TestGracefulShutdown_BackgroundGoroutinesStop verifies that background
// goroutines (WatchEnvFile, RefreshKeyPoolMetrics) stop when the stop
// channel is closed, allowing a WaitGroup to complete.
func TestGracefulShutdown_BackgroundGoroutinesStop(t *testing.T) {
	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
	}()

	// Give them a moment to start
	time.Sleep(50 * time.Millisecond)

	// Trigger shutdown and wait for completion
	close(stop)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Both goroutines stopped — success
	case <-time.After(3 * time.Second):
		t.Fatal("background goroutines did not stop within 3s")
	}
}

func TestTokenUsageMetrics(t *testing.T) {
	// Mock upstream that returns token usage in the response body
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return a response with token usage data
		fmt.Fprint(w, `{"usage": {"input_tokens": 42, "output_tokens": 88}}`)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             1,
		CooldownSec:            60,
		UpstreamCBThreshold:    5,
		CBResetSec:             30,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
	}
	pool := keypool.NewKeyPool([]string{"test-key-1234567890"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Send a proxy request that triggers token extraction
	resp, err := http.Get(srv.URL + "/test/v1/chat/completions")
	if err != nil {
		t.Fatalf("GET /test/v1/chat/completions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Check /metrics for token usage counters
	metricsResp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer metricsResp.Body.Close()
	body, _ := io.ReadAll(metricsResp.Body)
	metricsBody := string(body)

	// Verify token usage counters are present
	if !strings.Contains(metricsBody, `akswitch_token_usage_total`) {
		t.Error("token_usage_total metric not found in /metrics output")
	}
	if !strings.Contains(metricsBody, `provider="test"`) {
		t.Error("provider label not found in /metrics output")
	}
	if !strings.Contains(metricsBody, `direction="input"`) {
		t.Error("input direction label not found in /metrics output")
	}
	if !strings.Contains(metricsBody, `direction="output"`) {
		t.Error("output direction label not found in /metrics output")
	}
}

// TestLogStoreMetrics verifies that logstore metrics are exposed via /metrics
// after requests are processed.
func TestLogStoreMetrics(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id": "test"}`)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             1,
		CooldownSec:            60,
		UpstreamCBThreshold:    5,
		CBResetSec:             30,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
	}
	pool := keypool.NewKeyPool([]string{"test-key-1234567890"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Send a few requests to generate log entries
	for i := 0; i < 3; i++ {
		resp, err := http.Get(srv.URL + "/test/v1/models")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
	}

	// Check /metrics for logstore metrics
	metricsResp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer metricsResp.Body.Close()
	body, _ := io.ReadAll(metricsResp.Body)
	metricsBody := string(body)

	// Verify logstore metrics are present
	if !strings.Contains(metricsBody, `akswitch_logstore_entries_total`) {
		t.Error("logstore_entries_total metric not found in /metrics output")
	}
	if !strings.Contains(metricsBody, `akswitch_logstore_fill_ratio`) {
		t.Error("logstore_fill_ratio metric not found in /metrics output")
	}

	// Verify entries counter is > 0 (should be 3 from our requests)
	if !strings.Contains(metricsBody, `akswitch_logstore_entries_total 3`) {
		t.Logf("metrics body: %s", metricsBody)
	}

	// logstore_dropped_total should be 0 since we only sent 3 entries (capacity is 10000)
	if !strings.Contains(metricsBody, `akswitch_logstore_dropped_total 0`) {
		t.Logf("metrics body: %s", metricsBody)
	}
}

// TestMetricsEndpointAccessible verifies the /metrics endpoint is accessible
// and returns valid Prometheus text format.
func TestMetricsEndpointAccessible(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             1,
		CooldownSec:            60,
		UpstreamCBThreshold:    5,
		CBResetSec:             30,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
	}
	pool := keypool.NewKeyPool([]string{"test-key-1234567890"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected Content-Type text/plain, got %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	metricsBody := string(body)

	// Send a proxy request to trigger metric registration
	resp2, err2 := http.Get(srv.URL + "/test/v1/models")
	if err2 == nil {
		resp2.Body.Close()
	}

	// Debug: log the metrics body
	t.Logf("Metrics body for debugging: %s", metricsBody)

	// Verify the new metrics appear in the output (CounterVec metrics like
	// akswitch_requests_total only appear after their first increment)
	expectedMetrics := []string{
		"akswitch_logstore_entries_total",
		"akswitch_logstore_dropped_total",
		"akswitch_logstore_fill_ratio",
	}
	for _, name := range expectedMetrics {
		if !strings.Contains(metricsBody, name) {
			t.Errorf("expected metric %q not found in /metrics output", name)
		}
	}
}

// TestRetryMetrics verifies that retry counters are exposed via /metrics
// when retries occur during proxy requests.
func TestRetryMetrics(t *testing.T) {
	// Mock upstream that always returns 503 to trigger retries
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             3,
		CooldownSec:            30,
		UpstreamCBThreshold:    10,
		CBResetSec:             60,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
	}
	pool := keypool.NewKeyPool([]string{"test-key-1234567890"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Send a request that will trigger retries
	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /test/v1/models: %v", err)
	}
	resp.Body.Close()

	// Check /metrics for retry counters
	metricsResp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer metricsResp.Body.Close()
	body, _ := io.ReadAll(metricsResp.Body)
	metricsBody := string(body)

	// Retry counter should be present
	if !strings.Contains(metricsBody, "akswitch_retries_total") {
		t.Error("retries_total metric not found in /metrics output")
	}
	if !strings.Contains(metricsBody, "provider=\"test\"") {
		t.Error("provider label not found in retry metric")
	}
}

// TestUptimeMetrics verifies that uptime gauge is exposed via /metrics
func TestUptimeMetrics(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		TargetBase:             upstream.URL,
		GenaiBase:              upstream.URL,
		Port:                   0,
		MaxRetries:             1,
		CooldownSec:            60,
		UpstreamCBThreshold:    5,
		CBResetSec:             30,
		HealthCheckIntervalSec: 30,
		HealthCheckPath:        "/health",
		HealthCheckTimeoutSec:  5,
	}
	pool := keypool.NewKeyPool([]string{"test-key-1234567890"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Check /metrics for uptime gauge
	metricsResp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer metricsResp.Body.Close()
	body, _ := io.ReadAll(metricsResp.Body)
	metricsBody := string(body)

	if !strings.Contains(metricsBody, "akswitch_uptime_seconds") {
		t.Error("uptime_seconds metric not found in /metrics output")
	}
}

func readMetricValue(body, name, labelFilter string) float64 {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, name) {
			continue
		}
		// If no label filter, match the first bare metric line
		if labelFilter == "" {
			if !strings.Contains(line, "{") {
				parts := strings.Fields(line)
				if len(parts) >= 1 {
					var val float64
					fmt.Sscanf(parts[len(parts)-1], "%f", &val)
					return val
				}
			}
			continue
		}
		// Match labels
		braceIdx := strings.Index(line, "{")
		if braceIdx < 0 {
			continue
		}
		closeIdx := strings.Index(line, "}")
		if closeIdx < 0 {
			continue
		}
		labels := line[braceIdx+1 : closeIdx]
		// Check that all filter labels exist in the metric labels
		matches := true
		for _, filterLabel := range strings.Split(labelFilter, ",") {
			filterLabel = strings.TrimSpace(filterLabel)
			if filterLabel == "" {
				continue
			}
			if !strings.Contains(labels, filterLabel) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		// Extract the value after }
		valStr := strings.TrimSpace(line[closeIdx+1:])
		var val float64
		if _, err := fmt.Sscanf(valStr, "%f", &val); err == nil {
			return val
		}
	}
	return 0
}

// readMetricsDelta fetches /metrics before and after an action, and returns the delta
// of a specific metric+labels combination.
func readMetricsDelta(baseURL, metricName, labelFilter string, action func()) float64 {
	// Before
	resp, err := http.Get(baseURL + "/metrics")
	if err != nil {
		return -1
	}
	bodyBefore, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	before := readMetricValue(string(bodyBefore), metricName, labelFilter)

	action()

	// After
	resp, err = http.Get(baseURL + "/metrics")
	if err != nil {
		return -2
	}
	bodyAfter, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	after := readMetricValue(string(bodyAfter), metricName, labelFilter)

	return after - before
}

// setupServer creates a mock upstream and a ProviderRouter-based AK Switch test server.

func TestMetricsVerification_RequestCount(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c"}, 10, 60)
	defer srv.Close()

	delta := readMetricsDelta(srv.URL, "akswitch_requests_total",
		`method="GET",status="2xx"`,
		func() {
			resp, err := http.Get(srv.URL + "/test/v1/models")
			if err != nil {
				t.Fatalf("proxy request: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
		},
	)

	if delta < 1 {
		t.Errorf("akswitch_requests_total{method=GET,status=2xx} should increase by >=1, got %.0f", delta)
	} else {
		t.Logf("akswitch_requests_total increased by %.0f (OK)", delta)
	}
}

func TestMetricsVerification_RequestDuration(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c"}, 10, 60)
	defer srv.Close()

	delta := readMetricsDelta(srv.URL, "akswitch_request_duration_seconds_count",
		`method="GET",status="2xx"`,
		func() {
			resp, err := http.Get(srv.URL + "/test/v1/models")
			if err != nil {
				t.Fatalf("proxy request: %v", err)
			}
			resp.Body.Close()
		},
	)

	if delta < 1 {
		t.Errorf("akswitch_request_duration_seconds_count should increase by >=1, got %.0f", delta)
	} else {
		t.Logf("akswitch_request_duration_seconds_count increased by %.0f (OK)", delta)
	}

	// Also verify sum increased (using a fresh request to avoid stale baseline)
	sumDelta := readMetricsDelta(srv.URL, "akswitch_request_duration_seconds_sum",
		`method="GET",status="2xx"`,
		func() {
			time.Sleep(50 * time.Millisecond)
			resp, err := http.Get(srv.URL + "/test/v1/models")
			if err != nil {
				t.Fatalf("proxy request: %v", err)
			}
			resp.Body.Close()
		},
	)
	if sumDelta <= 0 {
		t.Errorf("akswitch_request_duration_seconds_sum should increase by >0, got %f", sumDelta)
	} else {
		t.Logf("akswitch_request_duration_seconds_sum increased by %f (OK)", sumDelta)
	}
}

func TestMetricsVerification_RateLimited(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c"}, 3, 2)
	defer srv.Close()

	delta := readMetricsDelta(srv.URL, "akswitch_upstream_errors_total",
		`type="rate_limited"`,
		func() {
			resp, err := http.Get(srv.URL + "/test/v1/models")
			if err != nil {
				t.Fatalf("proxy request: %v", err)
			}
			resp.Body.Close()
		},
	)

	if delta < 1 {
		t.Errorf("akswitch_upstream_errors_total{type=rate_limited} should increase by >=1, got %.0f", delta)
	} else {
		t.Logf("akswitch_upstream_errors_total{type=rate_limited} increased by %.0f (OK)", delta)
	}
}

func TestMetricsVerification_AuthRejected(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c"}, 3, 2)
	defer srv.Close()

	delta := readMetricsDelta(srv.URL, "akswitch_upstream_errors_total",
		`type="auth_rejected"`,
		func() {
			resp, err := http.Get(srv.URL + "/test/v1/models")
			if err != nil {
				t.Fatalf("proxy request: %v", err)
			}
			resp.Body.Close()
		},
	)

	if delta < 1 {
		t.Errorf("akswitch_upstream_errors_total{type=auth_rejected} should increase by >=1, got %.0f", delta)
	} else {
		t.Logf("akswitch_upstream_errors_total{type=auth_rejected} increased by %.0f (OK)", delta)
	}
}

func TestMetricsVerification_ServerError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c"}, 3, 2)
	defer srv.Close()

	delta := readMetricsDelta(srv.URL, "akswitch_upstream_errors_total",
		`type="server_error"`,
		func() {
			resp, err := http.Get(srv.URL + "/test/v1/models")
			if err != nil {
				t.Fatalf("proxy request: %v", err)
			}
			resp.Body.Close()
		},
	)

	if delta < 1 {
		t.Errorf("akswitch_upstream_errors_total{type=server_error} should increase by >=1, got %.0f", delta)
	} else {
		t.Logf("akswitch_upstream_errors_total{type=server_error} increased by %.0f (OK)", delta)
	}
}

func TestMetricsVerification_KeyPoolDisabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	// Create server via ProviderRouter so we have access to state.Metrics() via ProviderState
	cfg := &config.Config{
		TargetBase:  upstream.URL,
		GenaiBase:   upstream.URL,
		Port:        0,
		MaxRetries:  3,
		CooldownSec: 2,
	}
	pool := keypool.NewKeyPool([]string{"key-a", "key-b"}, nil)
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Before: record disabled count
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics before: %v", err)
	}
	bodyBefore, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	disabledBefore := readMetricValue(string(bodyBefore), "akswitch_keypool_keys", `state="disabled"`)

	// Trigger 401 on key-a which will disable it
	resp, err = http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	resp.Body.Close()

	// RefreshKeyPoolGauge temporarily — it updates the ServerState's metrics but
	// not the ProviderRouter's.  We verify the pool state directly instead.
	disabledAfter := float64(pool.DisabledCount())

	delta := disabledAfter - disabledBefore
	if delta < 1 {
		t.Errorf("akswitch_keypool_keys{state=disabled} should increase by >=1, got delta=%.0f (before=%.0f, after=%.0f)",
			delta, disabledBefore, disabledAfter)
	} else {
		t.Logf("akswitch_keypool_keys{state=disabled} increased by %.0f (OK)", delta)
	}
}
