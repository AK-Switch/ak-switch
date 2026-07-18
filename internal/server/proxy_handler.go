package server

import (
	"akswitch/internal/circuitbreaker"
	"akswitch/internal/config"
	akswitchmetrics "akswitch/internal/metrics"
	"akswitch/internal/utils"
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

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// ── Proxy Handler ──────────────────────────────────────

func (pr *ProviderRouter) proxyHandler(w http.ResponseWriter, r *http.Request) {
	// Extract provider name from path: /{provider}/...
	providerName, restPath := pr.extractProvider(r.URL.Path)
	if providerName == "" {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "no provider specified in path"})
		return
	}

	pr.mu.RLock()
	ps, ok := pr.providers[providerName]
	pr.mu.RUnlock()
	if !ok {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found: " + providerName})
		return
	}

	// Rewrite the URL path (strip provider prefix)
	r.URL.Path = restPath

	// Delegate to the proxy logic with this provider's state
	pr.executeProxy(w, r, ps)
}

// executeProxy contains the core proxy request logic.
func (pr *ProviderRouter) executeProxy(w http.ResponseWriter, r *http.Request, ps *ProviderState) {
	cfg := ps.Config
	pool := ps.Pool
	client := ps.Proxy.client
	upCB := ps.Proxy.upCB

	start := time.Now()
	var lastKey string
	var lastIdx int

	bodyBytes, err := readRequestBody(w, r)
	if err != nil {
		pr.recordProxyMetrics(r.Method, "4xx", "", start)
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
		if !ok {
			wait := pool.TimeUntilAvailable()
			if wait < 0 {
				pr.writeAllKeysExhausted(w, ps, r.Method, start)
				return
			}
			jitter := time.Duration(rand.Intn(500)) * time.Millisecond
			slog.Warn("all keys cooling", "provider", ps.Name, "wait", (wait+jitter).Round(time.Second), "retry", attempt, "max", cfg.MaxRetries)
			time.Sleep(wait + jitter)
			continue
		}
		lastKey = key
		lastIdx = idx

		if !pool.CB(idx).Allow() {
			pool.Release(idx) // release since we're skipping this key
			remaining := pool.CB(idx).CooldownRemaining()
			if remaining < 0 {
				allPerma := true
				for i := range pool.Keys() {
					if pool.CB(i).State() != circuitbreaker.StatePermanent {
						allPerma = false
						break
					}
				}
				if allPerma {
					pr.writeAllKeysExhausted(w, ps, r.Method, start)
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
			pr.metrics.UpstreamErrors.WithLabelValues("network").Inc()
			writeProxyError(w, http.StatusInternalServerError, ErrorUpstreamError, "failed to build upstream request")
			pr.recordProxyMetrics(r.Method, "5xx", "", start)
			return
		}
		utils.CopyHeaders(req.Header, r.Header)
		req.Header.Set("Authorization", "Bearer "+key)

		resp, err := client.Do(req)
		ttfb := time.Since(start)
		pool.Release(idx) // done with the key, allow other goroutines to select it
		if err != nil {
			switch categorizeError(0, err) {
			case CatClientAbort:
				slog.Debug("client aborted request", "provider", ps.Name, "key_index", idx, "key_name", pool.Name(idx), "error", err)
				return
			default:
				slog.Warn("key network error", "provider", ps.Name, "key_index", idx, "key_name", pool.Name(idx), "error", err)
				pr.metrics.UpstreamErrors.WithLabelValues("network").Inc()
				upCB.RecordFailure()
				continue
			}
		}

		// ── Response status dispatch ──
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			pr.logs.Append(buildLogEntry(ps, key, idx, r.Method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds()))
			if pr.handleRateLimited(w, ps, idx, resp, cfg, start, r.Method, target, bodyBytes) {
				return
			}
			continue

		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			pr.logs.Append(buildLogEntry(ps, key, idx, r.Method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds()))
			if pr.handleAuthRejected(w, ps, idx, resp, start, r.Method, target, bodyBytes) {
				return
			}
			continue

		case resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable:
			pr.logs.Append(buildLogEntry(ps, key, idx, r.Method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds()))
			pr.handleServerError(ps, idx, resp, attempt)
			continue

		case resp.StatusCode >= 400 && resp.StatusCode < 500 || categorizeError(resp.StatusCode, nil) == CatNonRetryable:
			pr.handleNonRetryable(w, ps, idx, resp, start, r.Method, target, bodyBytes, attempt, key, ttfb)
			return

		case resp.StatusCode >= 500:
			pr.logs.Append(buildLogEntry(ps, key, idx, r.Method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds()))
			pr.handleServerError(ps, idx, resp, attempt)
			continue

		default:
			// 2xx/3xx — success
			pr.handleSuccess(w, ps, idx, resp, start, r.Method, target, bodyBytes, attempt, key, ttfb)
			return
		}
	}

	// Retry exhausted
	writeProxyError(w, http.StatusServiceUnavailable, ErrorExhaustedRetries, fmt.Sprintf("%s 重试已耗尽，所有 Key 无响应", ps.Name))
	pr.logs.Append(utils.LogEntry{
		Timestamp:       time.Now().Format(time.RFC3339),
		Key:             lastKey,
		KeyIndex:        lastIdx + 1,
		KeyName:         pool.Name(lastIdx),
		Method:          r.Method,
		URL:             target,
		Status:          http.StatusServiceUnavailable,
		RequestBodySize: len(bodyBytes),
		DurationMs:      time.Since(start).Milliseconds(),
		Retries:         cfg.MaxRetries,
		Provider:        ps.Name,
	})
	pr.metrics.RetryCount.WithLabelValues(ps.Name).Add(float64(cfg.MaxRetries))
	slog.Debug("proxy response debug", "status", 503, "duration_ms", time.Since(start).Seconds()*1000, "retries", cfg.MaxRetries)
	pr.recordProxyMetrics(r.Method, "5xx", "", start)
}

// ── Response Status Handlers ───────────────────────────

// handleRateLimited processes a 429 Too Many Requests response.
// It records the failure, applies cooldown (respecting Retry-After headers),
// and returns true if all keys are exhausted (caller should abort).
// When returning true, the error response has already been written to w.
func (pr *ProviderRouter) handleRateLimited(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, cfg *config.Config, start time.Time, method, target string, bodyBytes []byte) bool {
	defer resp.Body.Close()
	pool := ps.Pool

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
	slog.Warn("key rate limited", "provider", ps.Name, "key_index", idx, "key_name", pool.Name(idx), "status", resp.StatusCode, "cb_state", fmt.Sprintf("%d", pool.CB(idx).State()), "cb_retry", pool.CB(idx).Attempt(), "body_preview", MaskSensitiveData(string(body), 1024))
	pr.metrics.UpstreamErrors.WithLabelValues("rate_limited").Inc()

	if pool.CB(idx).State() == circuitbreaker.StatePermanent {
		slog.Warn("key quota exhausted, disabling permanently", "provider", ps.Name, "key_index", idx, "key_name", pool.Name(idx))
		pool.Disable(idx)
		if pool.ActiveCount() == 0 {
			return pr.writeAllKeysExhausted(w, ps, method, start)
		}
	}
	return false
}

// handleAuthRejected processes a 401 Unauthorized or 403 Forbidden response.
// It disables the key permanently and returns true if all keys are exhausted.
// When returning true, the error response has already been written to w.
func (pr *ProviderRouter) handleAuthRejected(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, method, target string, bodyBytes []byte) bool {
	defer resp.Body.Close()
	pool := ps.Pool

	body, _ := io.ReadAll(resp.Body)
	pr.metrics.UpstreamErrors.WithLabelValues("auth_rejected").Inc()
	if pool.RecordAuthFailure(idx) {
		pool.Disable(idx)
		slog.Warn("key permanently disabled", "provider", ps.Name, "key_index", idx, "key_name", pool.Name(idx), "status", resp.StatusCode, "body_preview", MaskSensitiveData(string(body), 1024))
	} else {
		slog.Warn("key auth failure", "provider", ps.Name, "key_index", idx, "key_name", pool.Name(idx), "status", resp.StatusCode, "fail_count", pool.CB(idx).AuthFailCount())
	}
	if pool.ActiveCount() == 0 {
		writeProxyError(w, http.StatusServiceUnavailable, ErrorAllKeysInvalid, fmt.Sprintf("%s 所有 Key 已失效或吊销", ps.Name))
		pr.recordProxyMetrics(method, "5xx", "", start)
		return true
	}
	return false
}

// handleServerError processes a 502 Bad Gateway or 503 Service Unavailable (or other 5xx) response.
// It logs the error, records metrics, and records an upstream circuit breaker failure.
func (pr *ProviderRouter) handleServerError(ps *ProviderState, idx int, resp *http.Response, attempt int) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	slog.Warn("upstream server error", "provider", ps.Name, "key_index", idx, "key_name", ps.Pool.Name(idx), "status", resp.StatusCode, "body_preview", MaskSensitiveData(string(body), 1024))
	pr.metrics.UpstreamErrors.WithLabelValues("server_error").Inc()
	ps.Proxy.upCB.RecordFailure()
}

// handleNonRetryable copies a non-retryable 4xx response through to the client
// without further retry attempts.
func (pr *ProviderRouter) handleNonRetryable(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, method, target string, bodyBytes []byte, attempt int, key string, ttfb time.Duration) {
	defer resp.Body.Close()
	utils.CopyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	pr.logs.Append(buildLogEntry(ps, key, idx, method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds()))
	slog.Warn("non-retryable client error", "provider", ps.Name, "method", method, "url", target, "status", resp.StatusCode)
	slog.Debug("proxy response debug", "status", resp.StatusCode, "duration_ms", time.Since(start).Seconds()*1000, "retries", attempt+1)
	pr.recordProxyMetrics(method, "4xx", fmt.Sprintf("%d", idx), start)
	if attempt > 0 {
		pr.metrics.RetryCount.WithLabelValues(ps.Name).Add(float64(attempt))
	}
}

// handleSuccess processes a successful 2xx/3xx response, including streaming
// for SSE and chunked responses. For non-streaming responses, it extracts
// token usage from the response body and records it in the log entry.
func (pr *ProviderRouter) handleSuccess(w http.ResponseWriter, ps *ProviderState, idx int, resp *http.Response, start time.Time, method, target string, bodyBytes []byte, attempt int, key string, ttfb time.Duration) {
	pool := ps.Pool
	upCB := ps.Proxy.upCB

	pool.RecordSuccess(idx)
	upCB.RecordSuccess()

	utils.CopyHeaders(w.Header(), resp.Header)
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
			inputTokens, outputTokens = extractTokenUsage(body)
			// Also run tiktoken estimation for calibration comparison
			inputEstimate := estimateInputTokens(bodyBytes, model)
			responseText := extractResponseText(body)
			outputEstimate := estimateOutputTokens(responseText, model)
			if model != "" {
				if inputEstimate > 0 && inputTokens > 0 {
					pr.calibrator.Record(model, inputEstimate, inputTokens)
				}
				if outputEstimate > 0 && outputTokens > 0 {
					pr.calibrator.Record(model, outputEstimate, outputTokens)
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
			outputTokens = pr.calibrator.Apply(model, outputTokens)
		}
	}

	pool.IncrementRequestCount(idx)
	entry := buildLogEntry(ps, key, idx, method, target, resp.StatusCode, len(bodyBytes), start, attempt, ttfb.Milliseconds())
	entry.InputTokens = inputTokens
	entry.OutputTokens = outputTokens
	pr.logs.Append(entry)
	if inputTokens > 0 {
		pr.metrics.TokenUsage.WithLabelValues(ps.Name, "input").Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		pr.metrics.TokenUsage.WithLabelValues(ps.Name, "output").Add(float64(outputTokens))
	}
	if attempt > 0 {
		pr.metrics.RetryCount.WithLabelValues(ps.Name).Add(float64(attempt))
	}
	durationMs := time.Since(start).Milliseconds()
	slog.Info("proxy success",
		"provider", ps.Name,
		"method", method,
		"url", target,
		"status", resp.StatusCode,
		"key_index", idx,
		"key_name", pool.Name(idx),
		"retry", attempt,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"duration_ms", durationMs,
		"ttfb_ms", ttfb.Milliseconds(),
		"request_body_size", len(bodyBytes),
		"response_body_size", respBodySize,
	)
	slog.Debug("proxy response debug", "status", resp.StatusCode, "duration_ms", time.Since(start).Seconds()*1000, "retries", attempt+1)
	pr.recordProxyMetrics(method, akswitchmetrics.StatusLabel(resp.StatusCode), fmt.Sprintf("%d", idx), start)
}

// ── Proxy Helpers ──────────────────────────────────────

// buildLogEntry creates a structured LogEntry for proxy request logging.
func buildLogEntry(ps *ProviderState, key string, idx int, method, target string, status int, bodySize int, start time.Time, attempt int, ttfbMs int64) utils.LogEntry {
	return utils.LogEntry{
		Timestamp:       time.Now().Format(time.RFC3339),
		Key:             key,
		KeyIndex:        idx + 1,
		KeyName:         ps.Pool.Name(idx),
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

// extractTokenUsage attempts to parse input_tokens/output_tokens from a JSON response body.
// Supports both Anthropic format (input_tokens/output_tokens) and OpenAI format
// (prompt_tokens/completion_tokens). Returns 0, 0 on failure.
func extractTokenUsage(body []byte) (inputTokens, outputTokens int) {
	var result struct {
		Usage struct {
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, 0
	}
	// Prefer Anthropic format (input_tokens/output_tokens)
	if result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 {
		return result.Usage.InputTokens, result.Usage.OutputTokens
	}
	// Fallback to OpenAI format (prompt_tokens/completion_tokens)
	return result.Usage.PromptTokens, result.Usage.CompletionTokens
}

// extractResponseText extracts the text content from a response JSON body.
// Supports both Anthropic format (content array of {type, text}) and
// OpenAI format (choices[0].message.content). Returns empty string on failure.
func extractResponseText(body []byte) string {
	// Anthropic format: {"content": [{"type": "text", "text": "..."}]}
	var anthropicResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &anthropicResp); err == nil {
		var textBuilder strings.Builder
		for _, block := range anthropicResp.Content {
			if block.Type == "text" && block.Text != "" {
				if textBuilder.Len() > 0 {
					textBuilder.WriteByte(' ')
				}
				textBuilder.WriteString(block.Text)
			}
		}
		if textBuilder.Len() > 0 {
			return textBuilder.String()
		}
	}

	// OpenAI format: {"choices": [{"message": {"content": "..."}}]}
	var openAIResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &openAIResp); err == nil {
		var textBuilder strings.Builder
		for _, choice := range openAIResp.Choices {
			if choice.Message.Content != "" {
				if textBuilder.Len() > 0 {
					textBuilder.WriteByte(' ')
				}
				textBuilder.WriteString(choice.Message.Content)
			}
		}
		if textBuilder.Len() > 0 {
			return textBuilder.String()
		}
	}

	return ""
}

// recordProxyMetrics records request total count and duration metrics.
func (pr *ProviderRouter) recordProxyMetrics(method, statusClass, keyIndex string, start time.Time) {
	pr.metrics.RequestsTotal.WithLabelValues(method, statusClass, keyIndex).Inc()
	pr.metrics.RequestDuration.WithLabelValues(method, statusClass).Observe(time.Since(start).Seconds())
}

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
					Text string `json:"text"`
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
						outputBuf.WriteString(anthropicData.Delta.Text)
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
		inputTokens := estimateInputTokens(bodyBytes, model)
		return inputTokens, apiOutputTokens, respBodySize
	}

	// Fall back to tiktoken estimation
	outputTokens := estimateOutputTokens(outputBuf.String(), model)
	inputTokens := estimateInputTokens(bodyBytes, model)
	return inputTokens, outputTokens, respBodySize
}

// encodingForModel returns the appropriate tiktoken encoding name for a given model.
// Returns "cl100k_base" as the default fallback for unknown models.
func encodingForModel(model string) string {
	// gpt-4o / o1 / o3 series use o200k_base
	if strings.HasPrefix(model, "gpt-4o") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") {
		return "o200k_base"
	}
	return "cl100k_base"
}

// estimateOutputTokens uses tiktoken to estimate the number of tokens in a text string.
// The model parameter determines which tiktoken encoding to use.
// Returns 0 if tiktoken initialization fails or text is empty.
func estimateOutputTokens(text string, model string) int {
	if text == "" {
		return 0
	}
	tke, err := tiktoken.GetEncoding(encodingForModel(model))
	if err != nil {
		return 0
	}
	return len(tke.Encode(text, nil, nil))
}

// estimateInputTokens extracts message content from a request body and estimates
// the input token count using tiktoken. Returns 0 if parsing fails or body is empty.
// Supports both OpenAI format (content is a string) and Anthropic format
// (content is an array of {type, text} objects).
func estimateInputTokens(bodyBytes []byte, model string) int {
	if len(bodyBytes) == 0 {
		return 0
	}
	var reqBody struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil || len(reqBody.Messages) == 0 {
		return 0
	}
	var inputBuf strings.Builder
	for _, msg := range reqBody.Messages {
		// content can be either a string or an array of {type, text} objects
		if len(msg.Content) == 0 {
			continue
		}
		if msg.Content[0] == '"' {
			// String format: "content": "text"
			var s string
			if json.Unmarshal(msg.Content, &s) == nil {
				inputBuf.WriteString(s)
			}
		} else if msg.Content[0] == '[' {
			// Anthropic array format: "content": [{"type": "text", "text": "..."}]
			var parts []struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(msg.Content, &parts) == nil {
				for _, p := range parts {
					inputBuf.WriteString(p.Text)
				}
			}
		}
	}
	tke, err := tiktoken.GetEncoding(encodingForModel(model))
	if err != nil {
		return 0
	}
	return len(tke.Encode(inputBuf.String(), nil, nil))
}

// ── Extracted Utilities ───────────────────────────────

// readRequestBody reads and limits the request body to 10MB.
// Returns the body bytes, or nil and an error if the body is too large.
func readRequestBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB limit
	bodyBytes, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		writeProxyError(w, http.StatusBadRequest, ErrorBadRequest, "request body too large or unreadable")
		return nil, err
	}
	return bodyBytes, nil
}

// buildTargetURL constructs the upstream URL based on path routing rules.
// Routes /genai/ paths to GenaiBase, everything else to TargetBase.
func buildTargetURL(cfg *config.Config, path, rawQuery string) string {
	if strings.Contains(path, "/genai/") {
		target := cfg.GenaiBase + path
		if rawQuery != "" {
			target += "?" + rawQuery
		}
		return target
	}
	if strings.HasSuffix(cfg.TargetBase, "/v1") && strings.HasPrefix(path, "/v1") {
		path = path[3:]
	}
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	return cfg.TargetBase + path
}

// writeAllKeysExhausted writes the "all keys exhausted" error response and records metrics.
func (pr *ProviderRouter) writeAllKeysExhausted(w http.ResponseWriter, ps *ProviderState, method string, start time.Time) bool {
	writeProxyError(w, http.StatusServiceUnavailable, ErrorAllKeysInvalid, fmt.Sprintf("%s 所有 API Key 已熔断，请稍后重试", ps.Name))
	pr.recordProxyMetrics(method, "5xx", "", start)
	return true
}