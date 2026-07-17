//go:build unit

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── extractTokenUsage ──────────────────────────────────

func TestExtractTokenUsage_NormalResponse(t *testing.T) {
	body := []byte(`{"id":"msg_xxx","type":"message","usage":{"input_tokens":9,"output_tokens":1}}`)
	input, output := extractTokenUsage(body)
	if input != 9 {
		t.Errorf("input_tokens = %d, want 9", input)
	}
	if output != 1 {
		t.Errorf("output_tokens = %d, want 1", output)
	}
}

func TestExtractTokenUsage_ZeroValues(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":0,"output_tokens":0}}`)
	input, output := extractTokenUsage(body)
	if input != 0 {
		t.Errorf("input_tokens = %d, want 0", input)
	}
	if output != 0 {
		t.Errorf("output_tokens = %d, want 0", output)
	}
}

func TestExtractTokenUsage_LargeNumbers(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":123456,"output_tokens":78901}}`)
	input, output := extractTokenUsage(body)
	if input != 123456 {
		t.Errorf("input_tokens = %d, want 123456", input)
	}
	if output != 78901 {
		t.Errorf("output_tokens = %d, want 78901", output)
	}
}

func TestExtractTokenUsage_NoUsage(t *testing.T) {
	body := []byte(`{"id":"msg_xxx","type":"message"}`)
	input, output := extractTokenUsage(body)
	if input != 0 || output != 0 {
		t.Errorf("expected 0,0 for missing usage, got %d,%d", input, output)
	}
}

func TestExtractTokenUsage_EmptyBody(t *testing.T) {
	input, output := extractTokenUsage([]byte{})
	if input != 0 || output != 0 {
		t.Errorf("expected 0,0 for empty body, got %d,%d", input, output)
	}
}

func TestExtractTokenUsage_InvalidJSON(t *testing.T) {
	input, output := extractTokenUsage([]byte(`not json`))
	if input != 0 || output != 0 {
		t.Errorf("expected 0,0 for invalid JSON, got %d,%d", input, output)
	}
}

func TestExtractTokenUsage_NilBody(t *testing.T) {
	input, output := extractTokenUsage(nil)
	if input != 0 || output != 0 {
		t.Errorf("expected 0,0 for nil body, got %d,%d", input, output)
	}
}

func TestExtractTokenUsage_OpenAIFormat(t *testing.T) {
	// OpenAI-style format should not crash, but won't parse correctly
	body := []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`)
	_, _ = extractTokenUsage(body)
}

func TestExtractTokenUsage_PartialUsage(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":5}}`)
	input, output := extractTokenUsage(body)
	if input != 5 {
		t.Errorf("input_tokens = %d, want 5", input)
	}
	if output != 0 {
		t.Errorf("output_tokens = %d, want 0 (missing)", output)
	}
}

// ── streamSSEAndEstimateTokens ─────────────────────────

func TestStreamSSE_EmptyStream(t *testing.T) {
	w := httptest.NewRecorder()
	respBody := io.NopCloser(strings.NewReader(""))
	resp := &http.Response{Body: respBody, Header: make(http.Header)}

	input, output, _ := streamSSEAndEstimateTokens(w, resp, nil)

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

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil)

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

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil)

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

	input, output, _ := streamSSEAndEstimateTokens(w, resp, reqBody)

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

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil)

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

	_, output, _ := streamSSEAndEstimateTokens(w, resp, nil)

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

	streamSSEAndEstimateTokens(w, resp, nil)

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