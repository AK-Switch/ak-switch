//go:build unit

package server

import (
		"bufio"
		"io"
		"net/http"
		"net/http/httptest"
		"strings"
		"testing"
	)
// ── streamSSEAndEstimateTokens ─────────────────────────

func TestStreamSSE_EmptyStream(t *testing.T) {
	w := httptest.NewRecorder()
	respBody := io.NopCloser(strings.NewReader(""))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	input, output, _ := streamSSEAndEstimateTokens(w, resp, nil, "")

	if input != 0 {
		t.Errorf("input_tokens = %d, want 0", input)
	}
	if output != 0 {
		t.Errorf("output_tokens = %d, want 0", output)
	}
}

func TestStreamSSE_NoContentDeltaEvents(t *testing.T) {
	w := httptest.NewRecorder()
	sseData := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":0}}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil, "")

	if output != 0 {
		t.Errorf("output_tokens = %d, want 0 (no content delta)", output)
	}
}

func TestStreamSSE_SingleContentBlock(t *testing.T) {
	w := httptest.NewRecorder()
	sseData := "" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil, "")

	// "Hello world" should produce at least 1 token
	if output <= 0 {
		t.Errorf("output_tokens = %d, want > 0", output)
	}

	// Check that the response was written to the client
	body := w.Body.String()
	if !strings.Contains(body, "Hello") || !strings.Contains(body, "world") {
		t.Errorf("response body should contain SSE data, got: %q", body)
	}
}

func TestStreamSSE_InputTokenEstimation(t *testing.T) {
	w := httptest.NewRecorder()
	sseData := "" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Short reply\"}}\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	reqBody := []byte(`{"messages":[{"role":"user","content":"Say hello in one word"}]}`)

	input, output, _ := streamSSEAndEstimateTokens(w, resp, reqBody, "")

	if input <= 0 {
		t.Errorf("input_tokens = %d, want > 0", input)
	}
	if output <= 0 {
		t.Errorf("output_tokens = %d, want > 0", output)
	}
}

func TestStreamSSE_LongText(t *testing.T) {
	w := httptest.NewRecorder()
	// Simulate a longer response
	sseData := "" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"This is a longer piece of text that should produce multiple tokens when encoded with the tiktoken BPE tokenizer.\"}}\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil, "")

	if output <= 1 {
		t.Errorf("output_tokens = %d, want > 1 for longer text", output)
	}
}

func TestStreamSSE_MultipleContentBlocks(t *testing.T) {
	w := httptest.NewRecorder()
	// Simulate a response with multiple content blocks
	sseData := "" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Block 1\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Block 2\"}}\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil, "")

	if output <= 0 {
		t.Errorf("output_tokens = %d, want > 0", output)
	}

	// Both blocks should be accumulated
	responseBody := w.Body.String()
	if !strings.Contains(responseBody, "Block 1") || !strings.Contains(responseBody, "Block 2") {
		t.Errorf("response body should contain all SSE data, got: %q", responseBody)
	}
}

func TestStreamSSE_AllEventsPreserved(t *testing.T) {
	w := httptest.NewRecorder()
	sseData := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"role\":\"assistant\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Reply\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	streamSSEAndEstimateTokens(w, resp, nil, "")

	responseBody := w.Body.String()
	if !strings.Contains(responseBody, "message_start") {
		t.Error("response missing message_start event")
	}
	if !strings.Contains(responseBody, "message_stop") {
		t.Error("response missing message_stop event")
	}
	if !strings.Contains(responseBody, "message_delta") {
		t.Error("response missing message_delta event")
	}
}

func TestStreamSSE_MessageDeltaOutputTokens(t *testing.T) {
	w := httptest.NewRecorder()
	sseData := "" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":42}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil, "")

	// Should use API's output_tokens=42 from message_delta, not tiktoken estimation
	if output != 42 {
		t.Errorf("output_tokens = %d, want 42 (from message_delta.usage.output_tokens)", output)
	}
}

func TestStreamSSE_MessageDeltaNoUsage(t *testing.T) {
	w := httptest.NewRecorder()
	sseData := "" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello world\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil, "")

	// No usage in message_delta, should fall back to tiktoken estimation
	if output <= 0 {
		t.Errorf("output_tokens = %d, want > 0 (tiktoken estimation fallback)", output)
	}
}

func TestStreamSSE_ContentBlockStartText(t *testing.T) {
	w := httptest.NewRecorder()
	sseData := "" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"Initial text\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" and more\"}}\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil, "")

	// Should accumulate text from both content_block_start and content_block_delta
	if output <= 0 {
		t.Errorf("output_tokens = %d, want > 0 (text from start + delta)", output)
	}
}

func TestStreamSSE_OpenAIFormat(t *testing.T) {
	w := httptest.NewRecorder()
	sseData := "" +
		"data: {\"id\":\"chatcmpl-xxx\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-xxx\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-xxx\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil, "")

	// Should handle OpenAI format by accumulating choices[].delta.content
	if output <= 0 {
		t.Errorf("output_tokens = %d, want > 0 for OpenAI format", output)
	}

	// Verify the response was streamed to the client
	body := w.Body.String()
	if !strings.Contains(body, "Hello") || !strings.Contains(body, "world") {
		t.Errorf("response body should contain OpenAI streaming data, got: %q", body)
	}
}

func TestStreamSSE_MessageDeltaTakesPrecedence(t *testing.T) {
	w := httptest.NewRecorder()
	// message_delta has usage.output_tokens=50, but accumulated text would estimate differently
	sseData := "" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"A\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":50}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil, "")

	// API's output_tokens should take precedence
	if output != 50 {
		t.Errorf("output_tokens = %d, want 50 (API output_tokens should take precedence)", output)
	}
}

func TestStreamSSE_InputJsonDelta(t *testing.T) {
	w := httptest.NewRecorder()
	// Tool call: content_block_delta with input_json_delta (partial_json) instead of text_delta
	sseData := "" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"key\\\":\\\"value\\\"}\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n"

	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

_, output, _ := streamSSEAndEstimateTokens(w, resp, nil, "")

		// Should accumulate text from both input_json_delta (partial_json) and text_delta (text)
		if output <= 0 {
			t.Errorf("output_tokens = %d, want > 0 (should accumulate both input_json_delta and text_delta)", output)
		}
	}

func TestStreamSSE_RespBodySize(t *testing.T) {
	// SSE data lines (each line ends with LF in the raw stream)
	lines := []string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{},"usage":{"output_tokens":5}}`,
		`data: {"type":"message_stop"}`,
	}
	// Build SSE data with classic \n\n event separators
	var sseData string
	for i, line := range lines {
		if i > 0 {
			sseData += "\n" // blank line between events
		}
		sseData += line + "\n\n"
	}
	// Expected: each Scanner line is written back as (line + "\n").
	// Scanner splits on \n; blank lines between events are empty tokens.
	// The trailing \n\n means the last event has a trailing blank line.
	var expectedSize int
	scanner := bufio.NewScanner(strings.NewReader(sseData))
	for scanner.Scan() {
		expectedSize += len(scanner.Text()) + 1 // +1 for the "\n" added by w.Write
	}

	w := httptest.NewRecorder()
	respBody := io.NopCloser(strings.NewReader(sseData))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}
	resp.Header.Set("Content-Type", "text/event-stream")

	_, _, respBodySize := streamSSEAndEstimateTokens(w, resp, nil, "")

	if respBodySize != int64(expectedSize) {
		t.Errorf("respBodySize = %d, want %d", respBodySize, expectedSize)
	}
}
