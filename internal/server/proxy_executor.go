package server

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"akswitch/internal/circuitbreaker"
	akswitchmetrics "akswitch/internal/metrics"
	"akswitch/internal/tokenestimator"
	"akswitch/internal/tracker"
)

// ProxyExecutor handles the core proxy request lifecycle: key selection, upstream
// forwarding, response dispatch, retry logic, and token estimation.
// It is owned by ProviderRouter and receives per-request state via ProviderState.
type ProxyExecutor struct {
	metrics    *akswitchmetrics.Metrics
	calibrator *tracker.Calibrator
}

// NewProxyExecutor creates a ProxyExecutor with the given dependencies.
func NewProxyExecutor(metrics *akswitchmetrics.Metrics, calibrator *tracker.Calibrator) *ProxyExecutor {
	return &ProxyExecutor{
		metrics:    metrics,
		calibrator: calibrator,
	}
}

// Execute runs the full proxy request lifecycle for a single provider.
// It handles key selection, upstream request, response dispatch, and retries.
func (px *ProxyExecutor) Execute(w http.ResponseWriter, r *http.Request, ps *ProviderState) {
	pool := ps.pool
	client := ps.client
	upCB := ps.UpstreamCB()

	start := time.Now()

	bodyBytes, err := readRequestBody(w, r)
	if err != nil {
		px.recordProxyMetrics(r.Method, "4xx", "", start)
		return
	}

	var rectified bool

	if ps.ThinkingMode() == "rectify" {
		bodyBytes, rectified = NewThinkingRectifier(true, ps.RectifyThinkingMapTo()).Process(bodyBytes)
	}

	// Build target URL
	target := buildTargetURL(ps.config, r.URL.Path, r.URL.RawQuery)

	if auth := r.Header.Get("Authorization"); auth != "" {
		maskedAuth := func() string {
			if len(auth) > 12 {
				return auth[:7] + "..." + auth[len(auth)-4:]
			}
			return "****"
		}()
		bodyPreview := ""
		if len(bodyBytes) > 0 {
			preview := string(bodyBytes)
			if len(preview) > 1024 {
				preview = preview[:1024]
			}
			bodyPreview = MaskSensitiveData(preview, 1024)
		}
		slog.Debug("proxy request debug", "provider", ps.Name(), "method", r.Method, "path", r.URL.Path, "auth", maskedAuth, "body_size", len(bodyBytes), "body_preview", bodyPreview)
	}

	pool.AdvanceCounter()
	for round := 0; round < ps.MaxRetries(); round++ {

		if !upCB.Allow() {
			slog.Warn("upstream circuit breaker open, backing off", "provider", ps.Name(), "round", round, "max", ps.MaxRetries())
			time.Sleep(time.Second)
			continue
		}

		available := pool.AvailableKeys()
		if len(available) == 0 {
			if !pool.AnyActive() {
				px.writeAllKeysExhausted(w, ps, r.Method, start)
				return
			}
			slog.Warn("no available keys this round, all cooling", "provider", ps.Name(), "round", round, "max", ps.MaxRetries())
			time.Sleep(time.Second)
			continue
		}

		for _, idx := range available {

			keyName, _ := pool.Name(idx)

			req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(bodyBytes))
			if err != nil {
				px.metrics.UpstreamErrors.WithLabelValues("network").Inc()
				writeProxyError(w, http.StatusInternalServerError, ErrorUpstreamError, "failed to build upstream request")
				px.recordProxyMetrics(r.Method, "5xx", "", start)
				return
			}
			copyHeaders(req.Header, r.Header)
			req.Header.Set("Authorization", "Bearer "+pool.Keys()[idx])

			resp, err := client.Do(req)
			if err != nil {
				pool.Release(idx)
				switch categorizeError(0, err) {
				case CatClientAbort:
					slog.Debug("client aborted request", "provider", ps.Name(), "key_index", idx, "key_name", keyName, "error", err)
					return
				default:
					slog.Warn("key network error", "provider", ps.Name(), "key_index", idx, "key_name", keyName, "error", err)
					px.metrics.UpstreamErrors.WithLabelValues("network").Inc()
					upCB.RecordFailure()
					continue
				}
			}
			ttfb := time.Since(start)

			// ── Response status dispatch ──
			switch {
			case resp.StatusCode == http.StatusTooManyRequests:
				if px.handleRateLimited(w, ps, idx, resp, start, r.Method, target, bodyBytes) {
					return
				}
				continue

			case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
				if px.handleAuthRejected(w, ps, idx, resp, start, r.Method, target, bodyBytes) {
					return
				}
				continue

			case resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable:
				px.handleServerError(ps, idx, resp, round)
				continue

			case (resp.StatusCode >= 400 && resp.StatusCode < 500) || categorizeError(resp.StatusCode, nil) == CatNonRetryable:
				px.handleNonRetryable(w, ps, idx, resp, start, r.Method, target, bodyBytes, round, rectified)
				return

			case resp.StatusCode >= 500:
				px.handleServerError(ps, idx, resp, round)
				continue

			default:
				px.handleSuccess(w, ps, idx, resp, start, ttfb, r.Method, target, bodyBytes, round, rectified)
				return
			}
		}
	}

	writeProxyError(w, http.StatusServiceUnavailable, ErrorExhaustedRetries, fmt.Sprintf("%s 重试已耗尽，所有 Key 无响应", ps.Name()))

	px.metrics.RetryCount.WithLabelValues(ps.Name()).Add(float64(ps.MaxRetries()))
	slog.Warn("proxy retry exhausted",
		"provider", ps.Name(),
		"method", r.Method,
		"url", target,
		"status", 503,
		"retry", ps.MaxRetries(),
		"rounds", ps.MaxRetries(),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	px.recordProxyMetrics(r.Method, "5xx", "", start)
}

// ── Response Status Handlers ───────────────────────────

// handleRateLimited processes a 429 Too Many Requests response.
// It records the failure, applies cooldown (respecting Retry-After headers),
// and returns true if all keys are exhausted (caller should abort).
// When returning true, the error response has already been written to w.
func (px *ProxyExecutor) handleRateLimited(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, method, target string, bodyBytes []byte) bool {
	defer func() { _ = resp.Body.Close() }()
	pool := ps.pool
	keyName, _ := pool.Name(idx)

	body, _ := io.ReadAll(resp.Body)
	cbCooldown := pool.RecordFailure(idx)
	cooldown := cbCooldown
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			raDuration := time.Duration(secs+2) * time.Second
			if raDuration > cooldown {
				cooldown = raDuration
			}
		}
	}
	_ = pool.Cooldown(idx, cooldown)
	slog.Warn("key rate limited", "provider", ps.Name(), "key_index", idx, "key_name", keyName, "status", resp.StatusCode, "cb_state", fmt.Sprintf("%d", pool.CB(idx).State()), "cb_retry", pool.CB(idx).Attempt(), "body_preview", MaskSensitiveData(string(body), 1024))
	px.metrics.UpstreamErrors.WithLabelValues("rate_limited").Inc()

	if ps.PoolCB(idx).State() == circuitbreaker.Permanent {
		slog.Warn("key quota exhausted, disabling permanently", "provider", ps.Name(), "key_index", idx, "key_name", keyName)
		_ = pool.Disable(idx)
		if ps.PoolActiveCount() == 0 {
			return px.writeAllKeysExhausted(w, ps, method, start)
		}
	}
	return false
}

// handleAuthRejected processes a 401 Unauthorized or 403 Forbidden response.
// It disables the key permanently and returns true if all keys are exhausted.
// When returning true, the error response has already been written to w.
func (px *ProxyExecutor) handleAuthRejected(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, method, target string, bodyBytes []byte) bool {
	defer func() { _ = resp.Body.Close() }()
	pool := ps.pool
	keyName, _ := pool.Name(idx)

	body, _ := io.ReadAll(resp.Body)
	px.metrics.UpstreamErrors.WithLabelValues("auth_rejected").Inc()
	if pool.RecordAuthFailure(idx) {
		_ = pool.Disable(idx)
		ps.PersistKeys()
		slog.Warn("key permanently disabled", "provider", ps.Name(), "key_index", idx, "key_name", keyName, "status", resp.StatusCode, "body_preview", MaskSensitiveData(string(body), 1024))
	} else {
		slog.Warn("key auth failure", "provider", ps.Name(), "key_index", idx, "key_name", keyName, "status", resp.StatusCode, "fail_count", ps.PoolAuthFailCount(idx))
	}
	if ps.PoolActiveCount() == 0 {
		writeProxyError(w, http.StatusServiceUnavailable, ErrorAllKeysInvalid, fmt.Sprintf("%s 所有 Key 已失效或吊销", ps.Name()))
		px.recordProxyMetrics(method, "5xx", "", start)
		return true
	}
	return false
}

// handleServerError processes a 502 Bad Gateway or 503 Service Unavailable (or other 5xx) response.
// It logs the error, records metrics, and records an upstream circuit breaker failure.
func (px *ProxyExecutor) handleServerError(ps *ProviderState, idx int, resp *http.Response, attempt int) {
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	pool := ps.pool
	keyName, _ := pool.Name(idx)
	slog.Warn("upstream server error", "provider", ps.Name(), "key_index", idx, "key_name", keyName, "status", resp.StatusCode, "body_preview", MaskSensitiveData(string(body), 1024))
	px.metrics.UpstreamErrors.WithLabelValues("server_error").Inc()
	ps.RecordUpstreamFailure()
}

// handleNonRetryable copies a non-retryable 4xx response through to the client
// without further retry attempts. It also persists the request/response pair
// to ~/.akswitch/errors/ for post-hoc debugging (e.g. rectifier 400s).
func (px *ProxyExecutor) handleNonRetryable(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, method, target string, bodyBytes []byte, attempt int, rectified bool) {
	defer func() { _ = resp.Body.Close() }()
	keyName, _ := ps.PoolName(idx)

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	// Stream body to client while capturing it for the dump (client gets partial data on read error)
	var buf bytes.Buffer
	tee := io.TeeReader(resp.Body, &buf)
	_, _ = io.Copy(w, tee)
	body := buf.Bytes()

	if err := writeErrorDump(SetupErrorLogDir(), ps, keyName, method, target, resp.StatusCode, attempt, start, bodyBytes, body, rectified); err != nil {
		slog.Warn("failed to write error dump", "provider", ps.Name(), "status", resp.StatusCode, "error", err)
	}

	slog.Warn("non-retryable client error", "provider", ps.Name(), "method", method, "url", target, "status", resp.StatusCode, "key_name", keyName, "body_preview", MaskSensitiveData(string(body), 1024))
	slog.Debug("proxy response debug", "status", resp.StatusCode, "duration_ms", time.Since(start).Seconds()*1000, "retries", attempt+1)
	px.recordProxyMetrics(method, "4xx", fmt.Sprintf("%d", idx), start)
	if attempt > 0 {
		px.metrics.RetryCount.WithLabelValues(ps.Name()).Add(float64(attempt))
	}
}

// handleSuccess processes a successful 2xx/3xx response, including streaming
// for SSE and chunked responses. For non-streaming responses, it extracts
// token usage from the response body and records it in the log entry.
func (px *ProxyExecutor) handleSuccess(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, ttfb time.Duration, method, target string, bodyBytes []byte, attempt int, rectified bool) {
	pool := ps.pool
	keyName, _ := pool.Name(idx)
	pool.RecordSuccess(idx)
	ps.RecordUpstreamSuccess()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	var inputTokens, outputTokens int
	var model string
	var responseText string

	// Extract model name from request body for calibration
	model = tokenestimator.ExtractModel(bodyBytes)

	var respBodySize int64

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		inputTokens, outputTokens, respBodySize = streamSSEAndEstimateTokens(w, resp, bodyBytes, model)
	} else {
		// Non-streaming: read body to extract token usage, then write to client
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err == nil {
			inputTokens, outputTokens, responseText = tokenestimator.ProcessResponse(body)
			outputEstimate := tokenestimator.EstimateOutput(responseText, model)
			tokenestimator.RecordCalibration(px.calibrator, model,
				tokenestimator.EstimateInput(bodyBytes, model), inputTokens,
				outputEstimate, outputTokens)
			if outputTokens == 0 && outputEstimate > 0 {
				outputTokens = outputEstimate
			}
			_, _ = w.Write(body)
			respBodySize = int64(len(body))
		}
	}

	// Apply calibration to streaming estimates
	if model != "" && strings.Contains(contentType, "text/event-stream") {
		if outputTokens > 0 {
			outputTokens = px.calibrator.Apply(model, outputTokens)
		}
	}

	pool.IncrementRequestCount(idx)
	if inputTokens > 0 {
		px.metrics.TokenUsage.WithLabelValues(ps.Name(), "input").Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		px.metrics.TokenUsage.WithLabelValues(ps.Name(), "output").Add(float64(outputTokens))
	}
	if attempt > 0 {
		px.metrics.RetryCount.WithLabelValues(ps.Name()).Add(float64(attempt))
	}
	durationMs := time.Since(start).Milliseconds()
	slog.Info("proxy success",
		"provider", ps.Name(),
		"method", method,
		"url", target,
		"status", resp.StatusCode,
		"key_index", idx,
		"key_name", keyName,
		"retry", attempt,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"duration_ms", durationMs,
		"ttfb_ms", ttfb.Milliseconds(),
		"request_body_size", len(bodyBytes),
		"response_body_size", respBodySize,
		"rectified", rectified,
	)
	slog.Debug("proxy response debug", "status", resp.StatusCode, "duration_ms", time.Since(start).Seconds()*1000, "retries", attempt+1)
	px.recordProxyMetrics(method, akswitchmetrics.StatusLabel(resp.StatusCode), fmt.Sprintf("%d", idx), start)
}

// ── Proxy Helpers ──────────────────────────────────────

// recordProxyMetrics records request total count and duration metrics.
func (px *ProxyExecutor) recordProxyMetrics(method, statusClass, keyIndex string, start time.Time) {
	px.metrics.RequestsTotal.WithLabelValues(method, statusClass, keyIndex).Inc()
	px.metrics.RequestDuration.WithLabelValues(method, statusClass).Observe(time.Since(start).Seconds())
}

// writeAllKeysExhausted writes the "all keys exhausted" error response and records metrics.
func (px *ProxyExecutor) writeAllKeysExhausted(w http.ResponseWriter, ps *ProviderState, method string, start time.Time) bool {
	writeProxyError(w, http.StatusServiceUnavailable, ErrorAllKeysInvalid, fmt.Sprintf("%s 所有 API Key 已熔断，请稍后重试", ps.Name()))
	px.recordProxyMetrics(method, "5xx", "", start)
	return true
}

// ── Streaming Helpers ──────────────────────────────────

// streamSSEAndEstimateTokens streams SSE events to the client while accumulating
// text for token estimation. Supports multiple SSE formats via tokenestimator.ParseSSEEvent:
//   - Anthropic: content_block_delta (delta.text), content_block_start (content_block.text),
//     message_delta (usage.output_tokens)
//   - OpenAI: choices[].delta.content
//
// After the stream ends, it uses the API's output_tokens from message_delta
// when available, otherwise falls back to tiktoken estimation.
func streamSSEAndEstimateTokens(w http.ResponseWriter, resp *http.Response, bodyBytes []byte, model string) (int, int, int64) {
	defer func() { _ = resp.Body.Close() }()

	var outputBuf strings.Builder
	var respBodySize int64
	var apiOutputTokens int

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	f, canFlush := w.(http.Flusher)

	for scanner.Scan() {
		line := scanner.Text()

		// Debug: log raw SSE lines for token estimation diagnosis
		if strings.HasPrefix(line, "data:") {
			preview := line
			if len(preview) > 300 {
				preview = preview[:300]
			}
			slog.Debug("sse raw line", "preview", preview, "len", len(line))
		}

		// Write to client immediately and track response body size
		if n, err := w.Write([]byte(line + "\n")); err != nil {
			respBodySize += int64(n) // count partial write before breaking
			break
		} else {
			respBodySize += int64(n)
		}

		// Parse data: lines for SSE events
		if strings.HasPrefix(line, "data: ") {
			raw := line[6:]
			tokens, textDelta, _ := tokenestimator.ParseSSEEvent([]byte(raw))
			outputBuf.WriteString(textDelta)
			if tokens > 0 {
				apiOutputTokens = tokens
			}
		}

		if canFlush {
			f.Flush()
		}
	}

	// Use API's output_tokens from message_delta when available (most accurate)
	if apiOutputTokens > 0 {
		inputTokens := tokenestimator.EstimateInput(bodyBytes, model)
		return inputTokens, apiOutputTokens, respBodySize
	}

	// Fall back to tiktoken estimation
	outputTokens := tokenestimator.EstimateOutput(outputBuf.String(), model)
	inputTokens := tokenestimator.EstimateInput(bodyBytes, model)
	return inputTokens, outputTokens, respBodySize
}
