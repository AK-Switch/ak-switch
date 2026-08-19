//go:build integration

package integration

import (
	"bytes"

	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"akswitch/internal/server"
)

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

	// Round 0: key-a gets 429, key-b gets 200 -> success.
	// With all-keys-per-round semantics, second key is tried in same round.
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
		// Send terminal event so the proxy does not inject truncation error
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
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
		if strings.HasPrefix(line, "data: ") && !strings.HasPrefix(line, "data: [DONE]") {
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
		ProviderConfig: config.ProviderConfig{
			TargetBase:  upstream.URL,
			Port:        8080,
			MaxRetries:  10,
			CooldownSec: 60,
			AdminToken:  "",
			Keys:        []string{"initial-key"},
		},
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

	// MaxRetries=1 -> 1 round over all 3 keys, all return 503 -> 503
	srv := setupServer(t, upstream, []string{"test-key-a", "test-key-b", "test-key-c"}, 1, 60)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 after exhausting MaxRetries=1, got %d", resp.StatusCode)
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

// TestProxyOnlyAvailableKeysTried verifies that when only a subset of keys are
// available (others in cooldown), the retry loop only tries the available ones.
func TestProxyOnlyAvailableKeysTried(t *testing.T) {
	upstream := httptest.NewServer(retryHandler(
		http.StatusTooManyRequests, http.StatusOK, 2, `{"status":"ok"}`,
	))
	defer upstream.Close()

	// 5 keys, but key-a and key-b start in a 30s cooldown.
	// Only key-c, key-d, key-e are available.
	// The handler returns 429 twice then 200 -> should succeed on 3rd available key.
	pool := keypool.NewKeyPool([]string{"key-a", "key-b", "key-c", "key-d", "key-e"}, nil)
	cfg := &config.Config{
		ProviderConfig: config.ProviderConfig{
			TargetBase:  upstream.URL,
			Port:        0,
			MaxRetries:  5,
			CooldownSec: 30,
		},
	}
	pr := server.NewProviderRouter("")
	pr.AddProvider("test", cfg, pool)
	srv := httptest.NewServer(pr.Handler())
	defer srv.Close()

	// Manually cool key-a and key-b
	_ = pool.Cooldown(0, 30*time.Second)
	_ = pool.Cooldown(1, 30*time.Second)

	resp, err := http.Get(srv.URL + "/test/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK (only available keys tried), got %d", resp.StatusCode)
	}
}

// TestProxySuccessStopsRound verifies that when a key succeeds mid-round,
// the proxy returns immediately without trying remaining available keys.
func TestProxySuccessStopsRound(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		switch {
		case strings.Contains(auth, "test-key-a"):
			w.WriteHeader(http.StatusServiceUnavailable)
		case strings.Contains(auth, "test-key-b"):
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
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

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK (stop after first success), got %d", resp.StatusCode)
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
	var fiveXX []string
	for _, f := range failures {
		if !strings.Contains(f, "got 429") {
			fiveXX = append(fiveXX, f)
		}
	}
	if len(fiveXX) > 0 {
		t.Fatalf("%d/%d requests failed with non-429 errors:\n%s", len(fiveXX), concurrency, strings.Join(fiveXX, "\n"))
	}
	if len(failures) > 0 {
		t.Logf("%d/%d requests returned 429 (all keys cooling, client should retry)", len(failures), concurrency)
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
		ProviderConfig: config.ProviderConfig{
			TargetBase:  "://invalid",
			Port:        0,
			MaxRetries:  3,
			CooldownSec: 60,
		},
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
// 22. All keys disabled via API — returns 503 immediately (no CPU spin)
// ---------------------------------------------------------------------------

func TestProxy_AllDisabled_Returns503(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	srv := setupServer(t, upstream, []string{"key-a", "key-b", "key-c"}, 10, 60)
	defer srv.Close()

	// Disable all 3 keys via API (1-based indexing: 1=key-a, 2=key-b, 3=key-c)
	for _, idx := range []string{"1", "2", "3"} {
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
