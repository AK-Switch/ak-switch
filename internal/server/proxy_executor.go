package server

import (
	"akswitch/internal/circuitbreaker"
	"akswitch/internal/config"
	akswitchmetrics "akswitch/internal/metrics"
	"akswitch/internal/logentry"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"akswitch/internal/logstore"
	"akswitch/internal/tokenestimator"
	"akswitch/internal/tracker"
)

// ProxyExecutor handles the core proxy request lifecycle: key selection, upstream
// forwarding, response dispatch, retry logic, and token estimation.
// It is owned by ProviderRouter and receives per-request state via ProviderState.
type ProxyExecutor struct {
	metrics    *akswitchmetrics.Metrics
	logs       *logstore.LogStore
	calibrator *tracker.Calibrator
}

// NewProxyExecutor creates a ProxyExecutor with the given dependencies.
func NewProxyExecutor(metrics *akswitchmetrics.Metrics, logs *logstore.LogStore, calibrator *tracker.Calibrator) *ProxyExecutor {
	return &ProxyExecutor{
		metrics:    metrics,
		logs:       logs,
		calibrator: calibrator,
	}
}

// Execute runs the full proxy request lifecycle for a single provider.
// It handles key selection, upstream request, response dispatch, and retries.
func (px *ProxyExecutor) Execute(w http.ResponseWriter, r *http.Request, ps *ProviderState) {
	cfg := ps.Config
	pool := ps.Pool
	client := ps.Proxy.client
	upCB := ps.Proxy.upCB

	start := time.Now()
	var lastKey string
	var lastIdx int
	var lastKeyName string

	bodyBytes, err := readRequestBody(w, r)
	if err != nil {
		px.recordProxyMetrics(r.Method, "4xx", "", start)
		return
	}

	// Build target URL
	target := buildTargetURL(cfg, r.URL.Path, r.URL.RawQuery)

	if auth := r.Header.Get("Authorization"); auth != "" {
		maskedAuth := auth
		if len(auth) > 12 {
			maskedAuth = auth[:7] + "..." + auth[len(auth)-4:]
		} else {
			maskedAuth = "****"
		}
		bodyPreview := ""
		if len(bodyBytes) > 0 {
			preview := string(bodyBytes)
			if len(preview) > 1024 {
				preview = preview[:1024]
			}
			bodyPreview = MaskSensitiveData(preview, 1024)
		}
		slog.Debug("proxy request debug", "provider", ps.Name, "method", r.Method, "path", r.URL.Path, "auth", maskedAuth, "body_size", len(bodyBytes), "body_preview", bodyPreview)
	}

	for attempt := 0; attempt < cfg.MaxRetries; attempt++ {
		if !upCB.Allow() {
			slog.Warn("upstream circuit breaker open, backing off", "provider", ps.Name, "retry", attempt, "max", cfg.MaxRetries)
			time.Sleep(time.Second)
			continue
		}

		idx, key, ok := pool.Next()
		keyName, _ := pool.Name(idx)
		if !ok {
			wait := pool.TimeUntilAvailable()
			if wait < 0 {
				px.writeAllKeysExhausted(w, ps, r.Method, start)
				return
			}
			jitter := time.Duration(rand.Intn(500)) * time.Millisecond
			slog.Warn("all keys cooling", "provider", ps.Name, "wait", (wait+jitter).Round(time.Second), "retry", attempt, "max", cfg.MaxRetries)
			time.Sleep(wait + jitter)
			continue
		}
		lastKey = key
		lastIdx = idx
		lastKeyName, _ = pool.Name(lastIdx)
		if !pool.CB(idx).Allow() {
			pool.Release(idx) // release since we're skipping this key
			remaining := pool.CB(idx).CooldownRemaining()
			if remaining < 0 {
				allPerma := true
				for i := range pool.Keys() {
					if pool.CB(i).State() != circuitbreaker.Permanent {
						allPerma = false
						break
					}
				}
				if allPerma {
					px.writeAllKeysExhausted(w, ps, r.Method, start)
					return
				}
				continue
			}
			if remaining > 0 {
				pool.Cooldown(idx, remaining)
			}
			continue
		}

		req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(bodyBytes))
		if err != nil {
			px.metrics.UpstreamErrors.WithLabelValues("network").Inc()
			writeProxyError(w, http.StatusInternalServerError, ErrorUpstreamError, "failed to build upstream request")
			px.recordProxyMetrics(r.Method, "5xx", "", start)
			return
		}
		copyHeaders(req.Header, r.Header)
		req.Header.Set("Authorization", "Bearer "+key)

		resp, err := client.Do(req)
		ttfb := time.Since(start)
		pool.Release(idx) // done with the key, allow other goroutines to select it
		if err != nil {
			switch categorizeError(0, err) {
			case CatClientAbort:
				slog.Debug("client aborted request", "provider", ps.Name, "key_index", idx, "key_name", keyName, "error", err)
				return
			default:
				slog.Warn("key network error", "provider", ps.Name, "key_index", idx, "key_name", keyName, "error", err)
				px.metrics.UpstreamErrors.WithLabelValues("network").Inc()
				upCB.RecordFailure()
				continue
			}
		}

		// ── Response status dispatch ──
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			px.logs.Append(px.buildLogEntry(ps, key, idx, r.Method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds()))
			if px.handleRateLimited(w, ps, idx, resp, cfg, start, r.Method, target, bodyBytes) {
				return
			}
			continue

		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			px.logs.Append(px.buildLogEntry(ps, key, idx, r.Method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds()))
			if px.handleAuthRejected(w, ps, idx, resp, start, r.Method, target, bodyBytes) {
				return
			}
			continue

		case resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable:
			px.logs.Append(px.buildLogEntry(ps, key, idx, r.Method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds()))
			px.handleServerError(ps, idx, resp, attempt)
			continue

		case resp.StatusCode >= 400 && resp.StatusCode < 500 || categorizeError(resp.StatusCode, nil) == CatNonRetryable:
			px.handleNonRetryable(w, ps, idx, resp, start, r.Method, target, bodyBytes, attempt, key, ttfb)
			return

		case resp.StatusCode >= 500:
			px.logs.Append(px.buildLogEntry(ps, key, idx, r.Method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds()))
			px.handleServerError(ps, idx, resp, attempt)
			continue

		default:
			// 2xx/3xx — success
			px.handleSuccess(w, ps, idx, resp, start, r.Method, target, bodyBytes, attempt, key, ttfb)
			return
		}
	}

	// Retry exhausted
	writeProxyError(w, http.StatusServiceUnavailable, ErrorExhaustedRetries, fmt.Sprintf("%s 重试已耗尽，所有 Key 无响应", ps.Name))
	px.logs.Append(logentry.LogEntry{
		Timestamp:       time.Now().Format(time.RFC3339),
		Key:             lastKey,
		KeyIndex:        lastIdx + 1,
		KeyName:         lastKeyName,
		Method:          r.Method,
		URL:             target,
		Status:          http.StatusServiceUnavailable,
		RequestBodySize: len(bodyBytes),
		DurationMs:      time.Since(start).Milliseconds(),
		Retries:         cfg.MaxRetries,
		Provider:        ps.Name,
	})
	px.metrics.RetryCount.WithLabelValues(ps.Name).Add(float64(cfg.MaxRetries))
	slog.Debug("proxy response debug", "status", 503, "duration_ms", time.Since(start).Seconds()*1000, "retries", cfg.MaxRetries)
	px.recordProxyMetrics(r.Method, "5xx", "", start)
}

// ── Response Status Handlers ───────────────────────────

// handleRateLimited processes a 429 Too Many Requests response.
// It records the failure, applies cooldown (respecting Retry-After headers),
// and returns true if all keys are exhausted (caller should abort).
// When returning true, the error response has already been written to w.
func (px *ProxyExecutor) handleRateLimited(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, cfg *config.Config, start time.Time, method, target string, bodyBytes []byte) bool {
	defer resp.Body.Close()
	pool := ps.Pool
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
	pool.Cooldown(idx, cooldown)
	slog.Warn("key rate limited", "provider", ps.Name, "key_index", idx, "key_name", keyName, "status", resp.StatusCode, "cb_state", fmt.Sprintf("%d", pool.CB(idx).State()), "cb_retry", pool.CB(idx).Attempt(), "body_preview", MaskSensitiveData(string(body), 1024))
	px.metrics.UpstreamErrors.WithLabelValues("rate_limited").Inc()

	if pool.CB(idx).State() == circuitbreaker.Permanent {
		slog.Warn("key quota exhausted, disabling permanently", "provider", ps.Name, "key_index", idx, "key_name", keyName)
		pool.Disable(idx)
		if pool.ActiveCount() == 0 {
			return px.writeAllKeysExhausted(w, ps, method, start)
		}
	}
	return false
}

// handleAuthRejected processes a 401 Unauthorized or 403 Forbidden response.
// It disables the key permanently and returns true if all keys are exhausted.
// When returning true, the error response has already been written to w.
func (px *ProxyExecutor) handleAuthRejected(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, method, target string, bodyBytes []byte) bool {
	defer resp.Body.Close()
	pool := ps.Pool
	keyName, _ := pool.Name(idx)

	body, _ := io.ReadAll(resp.Body)
	px.metrics.UpstreamErrors.WithLabelValues("auth_rejected").Inc()
	if pool.RecordAuthFailure(idx) {
		pool.Disable(idx)
		ps.PersistKeys()
		slog.Warn("key permanently disabled", "provider", ps.Name, "key_index", idx, "key_name", keyName, "status", resp.StatusCode, "body_preview", MaskSensitiveData(string(body), 1024))
	} else {
		slog.Warn("key auth failure", "provider", ps.Name, "key_index", idx, "key_name", keyName, "status", resp.StatusCode, "fail_count", pool.CB(idx).AuthFailCount())
	}
	if pool.ActiveCount() == 0 {
		writeProxyError(w, http.StatusServiceUnavailable, ErrorAllKeysInvalid, fmt.Sprintf("%s 所有 Key 已失效或吊销", ps.Name))
		px.recordProxyMetrics(method, "5xx", "", start)
		return true
	}
	return false
}

// handleServerError processes a 502 Bad Gateway or 503 Service Unavailable (or other 5xx) response.
// It logs the error, records metrics, and records an upstream circuit breaker failure.
func (px *ProxyExecutor) handleServerError(ps *ProviderState, idx int, resp *http.Response, attempt int) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	pool := ps.Pool
	keyName, _ := pool.Name(idx)
	slog.Warn("upstream server error", "provider", ps.Name, "key_index", idx, "key_name", keyName, "status", resp.StatusCode, "body_preview", MaskSensitiveData(string(body), 1024))
	px.metrics.UpstreamErrors.WithLabelValues("server_error").Inc()
	ps.Proxy.upCB.RecordFailure()
}

// handleNonRetryable copies a non-retryable 4xx response through to the client
// without further retry attempts.
func (px *ProxyExecutor) handleNonRetryable(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, method, target string, bodyBytes []byte, attempt int, key string, ttfb time.Duration) {
	defer resp.Body.Close()
	keyName, _ := ps.Pool.Name(idx)
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	px.logs.Append(px.buildLogEntry(ps, key, idx, method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds()))
	slog.Warn("non-retryable client error", "provider", ps.Name, "method", method, "url", target, "status", resp.StatusCode, "key_name", keyName)
	slog.Debug("proxy response debug", "status", resp.StatusCode, "duration_ms", time.Since(start).Seconds()*1000, "retries", attempt+1)
	px.recordProxyMetrics(method, "4xx", fmt.Sprintf("%d", idx), start)
	if attempt > 0 {
		px.metrics.RetryCount.WithLabelValues(ps.Name).Add(float64(attempt))
	}
}

// handleSuccess processes a successful 2xx/3xx response, including streaming
// for SSE and chunked responses. For non-streaming responses, it extracts
// token usage from the response body and records it in the log entry.
func (px *ProxyExecutor) handleSuccess(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, method, target string, bodyBytes []byte, attempt int, key string, ttfb time.Duration) {
	pool := ps.Pool
	keyName, _ := pool.Name(idx)
	upCB := ps.Proxy.upCB

	pool.RecordSuccess(idx)
	upCB.RecordSuccess()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	var inputTokens, outputTokens int
	var model string

	// Extract model name from request body for calibration
	var reqBody struct {
		Model string `json:"model"`
	}
	if len(bodyBytes) > 0 {
		json.Unmarshal(bodyBytes, &reqBody)
		model = reqBody.Model
	}

	var respBodySize int64

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		inputTokens, outputTokens, respBodySize = streamSSEAndEstimateTokens(w, resp, bodyBytes, model)
	} else {
		// Non-streaming: read body to extract token usage, then write to client
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err == nil {
			inputTokens, outputTokens = tokenestimator.ExtractTokenUsage(body)
			// Also run tiktoken estimation for calibration comparison
			inputEstimate := tokenestimator.EstimateInput(bodyBytes, model)
			responseText := tokenestimator.ExtractResponseText(body)
			outputEstimate := tokenestimator.EstimateOutput(responseText, model)
			if model != "" {
				if inputEstimate > 0 && inputTokens > 0 {
					px.calibrator.Record(model, inputEstimate, inputTokens)
				}
				if outputEstimate > 0 && outputTokens > 0 {
					px.calibrator.Record(model, outputEstimate, outputTokens)
				}
			}
			// Fallback to tiktoken estimation when API response doesn't include output_tokens
			if outputTokens == 0 && outputEstimate > 0 {
				outputTokens = outputEstimate
			}
			w.Write(body)
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
	entry := px.buildLogEntry(ps, key, idx, method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds())
	entry.InputTokens = inputTokens
	entry.OutputTokens = outputTokens
	px.logs.Append(entry)
	if inputTokens > 0 {
		px.metrics.TokenUsage.WithLabelValues(ps.Name, "input").Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		px.metrics.TokenUsage.WithLabelValues(ps.Name, "output").Add(float64(outputTokens))
	}
	if attempt > 0 {
		px.metrics.RetryCount.WithLabelValues(ps.Name).Add(float64(attempt))
	}
	durationMs := time.Since(start).Milliseconds()
	slog.Info("proxy success",
		"provider", ps.Name,
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
	)
	slog.Debug("proxy response debug", "status", resp.StatusCode, "duration_ms", time.Since(start).Seconds()*1000, "retries", attempt+1)
	px.recordProxyMetrics(method, akswitchmetrics.StatusLabel(resp.StatusCode), fmt.Sprintf("%d", idx), start)
}

// ── Proxy Helpers ──────────────────────────────────────

// buildLogEntry creates a structured LogEntry for proxy request logging.
func (px *ProxyExecutor) buildLogEntry(ps *ProviderState, key string, idx int, method, target string, status int, bodySize int, start time.Time, attempt int, ttfbMs int64) logentry.LogEntry {
	keyName, _ := ps.Pool.Name(idx)
	return logentry.LogEntry{
		Timestamp:       time.Now().Format(time.RFC3339),
		Key:             key,
		KeyIndex:        idx + 1,
		KeyName:         keyName,
		Method:          method,
		URL:             target,
		Status:          status,
		RequestBodySize: bodySize,
		DurationMs:      time.Since(start).Milliseconds(),
		TtfbMs:          ttfbMs,
		Retries:         attempt,
		Provider:        ps.Name,
	}
}

// recordProxyMetrics records request total count and duration metrics.
func (px *ProxyExecutor) recordProxyMetrics(method, statusClass, keyIndex string, start time.Time) {
	px.metrics.RequestsTotal.WithLabelValues(method, statusClass, keyIndex).Inc()
	px.metrics.RequestDuration.WithLabelValues(method, statusClass).Observe(time.Since(start).Seconds())
}

// writeAllKeysExhausted writes the "all keys exhausted" error response and records metrics.
func (px *ProxyExecutor) writeAllKeysExhausted(w http.ResponseWriter, ps *ProviderState, method string, start time.Time) bool {
	writeProxyError(w, http.StatusServiceUnavailable, ErrorAllKeysInvalid, fmt.Sprintf("%s 所有 API Key 已熔断，请稍后重试", ps.Name))
	px.recordProxyMetrics(method, "5xx", "", start)
	return true
}

// ── Streaming Helpers ──────────────────────────────────

// streamResponse copies the response body to the client writer, flushing after
// each chunk for SSE compatibility. It always closes resp.Body.
func streamResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	if f, ok := w.(http.Flusher); ok {
		buf := make([]byte, 4096)
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					break
				}
				f.Flush()
			}
			if rerr != nil {
				break
			}
		}
	} else {
		io.Copy(w, resp.Body)
	}
}

// streamSSEAndEstimateTokens streams SSE events to the client while accumulating
// text for token estimation. Supports multiple SSE formats:
//   - Anthropic: content_block_delta (delta.text), content_block_start (content_block.text),
//     message_delta (usage.output_tokens)
//   - OpenAI: choices[].delta.content
// After the stream ends, it uses the API's output_tokens from message_delta
// when available, otherwise falls back to tiktoken estimation.
func streamSSEAndEstimateTokens(w http.ResponseWriter, resp *http.Response, bodyBytes []byte, model string) (int, int, int64) {
	defer resp.Body.Close()

	var outputBuf strings.Builder
	var respBodySize int64
	var apiOutputTokens int // from message_delta.usage.output_tokens

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

		// Write to client immediately
		if _, err := w.Write([]byte(line + "\n")); err != nil {
			break
		}

		// Parse data: lines for SSE events
		if strings.HasPrefix(line, "data: ") {
			raw := []byte(line[6:])

			// Try Anthropic format first
			var anthropicData struct {
				Type  string `json:"type"`
				Delta *struct {
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta,omitempty"`
				ContentBlock *struct {
					Text string `json:"text"`
				} `json:"content_block,omitempty"`
				Usage *struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage,omitempty"`
			}
			if err := json.Unmarshal(raw, &anthropicData); err == nil && anthropicData.Type != "" {
				switch anthropicData.Type {
				case "content_block_delta":
					if anthropicData.Delta != nil {
						// Accumulate text from both text_delta and input_json_delta
						outputBuf.WriteString(anthropicData.Delta.Text)
						outputBuf.WriteString(anthropicData.Delta.PartialJSON)
					}
				case "content_block_start":
					if anthropicData.ContentBlock != nil {
						outputBuf.WriteString(anthropicData.ContentBlock.Text)
					}
				case "message_delta":
					if anthropicData.Usage != nil && anthropicData.Usage.OutputTokens > 0 {
						apiOutputTokens = anthropicData.Usage.OutputTokens
					}
				}
			} else {
				// Try OpenAI streaming format: {"choices":[{"delta":{"content":"..."}}]}
				var openAIData struct {
					Choices []struct {
						Delta *struct {
							Content string `json:"content"`
						} `json:"delta,omitempty"`
					} `json:"choices"`
				}
				if err := json.Unmarshal(raw, &openAIData); err == nil {
					for _, choice := range openAIData.Choices {
						if choice.Delta != nil {
							outputBuf.WriteString(choice.Delta.Content)
						}
					}
				}
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