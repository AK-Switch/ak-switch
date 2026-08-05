//go:build unit

package tokenestimator

import (
	"testing"
)

// ── ExtractTokenUsage ──────────────────────────────────

func TestExtractTokenUsage_NormalResponse(t *testing.T) {
	body := []byte(`{"id":"msg_xxx","type":"message","usage":{"input_tokens":9,"output_tokens":1}}`)
	input, output := ExtractTokenUsage(body)
	if input != 9 {
		t.Errorf("input_tokens = %d, want 9", input)
	}
	if output != 1 {
		t.Errorf("output_tokens = %d, want 1", output)
	}
}

func TestExtractTokenUsage_ZeroValues(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":0,"output_tokens":0}}`)
	input, output := ExtractTokenUsage(body)
	if input != 0 {
		t.Errorf("input_tokens = %d, want 0", input)
	}
	if output != 0 {
		t.Errorf("output_tokens = %d, want 0", output)
	}
}

func TestExtractTokenUsage_LargeNumbers(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":123456,"output_tokens":78901}}`)
	input, output := ExtractTokenUsage(body)
	if input != 123456 {
		t.Errorf("input_tokens = %d, want 123456", input)
	}
	if output != 78901 {
		t.Errorf("output_tokens = %d, want 78901", output)
	}
}

func TestExtractTokenUsage_NoUsage(t *testing.T) {
	body := []byte(`{"id":"msg_xxx","type":"message"}`)
	input, output := ExtractTokenUsage(body)
	if input != 0 || output != 0 {
		t.Errorf("expected 0,0 for missing usage, got %d,%d", input, output)
	}
}

func TestExtractTokenUsage_EmptyBody(t *testing.T) {
	input, output := ExtractTokenUsage([]byte{})
	if input != 0 || output != 0 {
		t.Errorf("expected 0,0 for empty body, got %d,%d", input, output)
	}
}

func TestExtractTokenUsage_InvalidJSON(t *testing.T) {
	input, output := ExtractTokenUsage([]byte(`not json`))
	if input != 0 || output != 0 {
		t.Errorf("expected 0,0 for invalid JSON, got %d,%d", input, output)
	}
}

func TestExtractTokenUsage_NilBody(t *testing.T) {
	input, output := ExtractTokenUsage(nil)
	if input != 0 || output != 0 {
		t.Errorf("expected 0,0 for nil body, got %d,%d", input, output)
	}
}

func TestExtractTokenUsage_OpenAIFormat(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`)
	input, output := ExtractTokenUsage(body)
	if input != 10 {
		t.Errorf("input_tokens = %d, want 10 (from prompt_tokens)", input)
	}
	if output != 3 {
		t.Errorf("output_tokens = %d, want 3 (from completion_tokens)", output)
	}
}

func TestExtractTokenUsage_PartialUsage(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":5}}`)
	input, output := ExtractTokenUsage(body)
	if input != 5 {
		t.Errorf("input_tokens = %d, want 5", input)
	}
	if output != 0 {
		t.Errorf("output_tokens = %d, want 0 (missing)", output)
	}
}

// ── ExtractResponseText ──────────────────────────

func TestExtractResponseText_AnthropicFormat(t *testing.T) {
	body := []byte(`{"content":[{"type":"text","text":"Hello world"},{"type":"text","text":"Second block"}]}`)
	text := ExtractResponseText(body)
	if text != "Hello world Second block" {
		t.Errorf("ExtractResponseText = %q, want %q", text, "Hello world Second block")
	}
}

func TestExtractResponseText_OpenAIFormat(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"Hello from OpenAI"}}]}`)
	text := ExtractResponseText(body)
	if text != "Hello from OpenAI" {
		t.Errorf("ExtractResponseText = %q, want %q", text, "Hello from OpenAI")
	}
}

func TestExtractResponseText_EmptyBody(t *testing.T) {
	text := ExtractResponseText(nil)
	if text != "" {
		t.Errorf("ExtractResponseText(nil) = %q, want empty", text)
	}

	text = ExtractResponseText([]byte{})
	if text != "" {
		t.Errorf("ExtractResponseText(empty) = %q, want empty", text)
	}
}

func TestExtractResponseText_NoTextContent(t *testing.T) {
	body := []byte(`{"id":"msg_xxx","type":"message"}`)
	text := ExtractResponseText(body)
	if text != "" {
		t.Errorf("ExtractResponseText = %q, want empty", text)
	}
}

func TestExtractResponseText_AnthropicNoTextBlock(t *testing.T) {
	body := []byte(`{"content":[{"type":"tool_use","name":"get_weather"}]}`)
	text := ExtractResponseText(body)
	if text != "" {
		t.Errorf("ExtractResponseText = %q, want empty for tool_use only", text)
	}
}

// ── ParseSSEEvent ──────────────────────────

func TestParseSSEEvent_ContentBlockDelta(t *testing.T) {
	raw := []byte(`{"type":"content_block_delta","delta":{"text":"hello","partial_json":"{\"key\""}}`)
	tokens, delta := ParseSSEEvent(raw)
	if tokens != 0 {
		t.Errorf("expected 0 tokens, got %d", tokens)
	}
	if delta != "hello{\"key\"" {
		t.Errorf("expected `hello{\"key\"`, got %q", delta)
	}
}

func TestParseSSEEvent_ContentBlockStart(t *testing.T) {
	raw := []byte(`{"type":"content_block_start","content_block":{"text":"greeting"}}`)
	_, delta := ParseSSEEvent(raw)
	if delta != "greeting" {
		t.Errorf("expected `greeting`, got %q", delta)
	}
}

func TestParseSSEEvent_MessageDelta(t *testing.T) {
	raw := []byte(`{"type":"message_delta","usage":{"output_tokens":42}}`)
	tokens, _ := ParseSSEEvent(raw)
	if tokens != 42 {
		t.Errorf("expected 42 tokens, got %d", tokens)
	}
}

func TestParseSSEEvent_OpenAIFormat(t *testing.T) {
	raw := []byte(`{"choices":[{"delta":{"content":"hi"}}]}`)
	_, delta := ParseSSEEvent(raw)
	if delta != "hi" {
		t.Errorf("expected `hi`, got %q", delta)
	}
}

func TestParseSSEEvent_NonDataLine(t *testing.T) {
	raw := []byte(`event: ping`)
	tokens, delta := ParseSSEEvent(raw)
	if tokens != 0 || delta != "" {
		t.Errorf("expected (0, \"\"), got (%d, %q)", tokens, delta)
	}
}

func TestParseSSEEvent_InvalidJSON(t *testing.T) {
	raw := []byte(`not json`)
	tokens, delta := ParseSSEEvent(raw)
	if tokens != 0 || delta != "" {
		t.Errorf("expected (0, \"\"), got (%d, %q)", tokens, delta)
	}
}

func TestParseSSEEvent_PartialJSON(t *testing.T) {
	raw := []byte(`{"type":"content_block_delta","delta":{"partial_json":"{\"name\":"}}`)
	_, delta := ParseSSEEvent(raw)
	if delta != "{\"name\":" {
		t.Errorf("expected `{\"name\":`, got %q", delta)
	}
}

// ── ProcessResponse ──────────────────────────

func TestProcessResponse_AnthropicFormat(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"text","text":"hello"}]}`)
	in, out, text := ProcessResponse(body, "gpt-4o")
	if in != 10 {
		t.Errorf("inputTokens = %d, want 10", in)
	}
	if out != 5 {
		t.Errorf("outputTokens = %d, want 5", out)
	}
	if text != "hello" {
		t.Errorf("responseText = %q, want %q", text, "hello")
	}
}

func TestProcessResponse_OpenAIFormat(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":8,"completion_tokens":3},"choices":[{"message":{"content":"hi"}}]}`)
	in, out, text := ProcessResponse(body, "gpt-4o")
	if in != 8 {
		t.Errorf("inputTokens = %d, want 8", in)
	}
	if out != 3 {
		t.Errorf("outputTokens = %d, want 3", out)
	}
	if text != "hi" {
		t.Errorf("responseText = %q, want %q", text, "hi")
	}
}

func TestProcessResponse_NoUsage(t *testing.T) {
	body := []byte(`{"id":"msg_xxx"}`)
	in, out, _ := ProcessResponse(body, "")
	if in != 0 || out != 0 {
		t.Errorf("expected 0,0 for missing usage, got %d,%d", in, out)
	}
}

func TestProcessResponse_EmptyBody(t *testing.T) {
	in, out, text := ProcessResponse([]byte{}, "")
	if in != 0 || out != 0 {
		t.Errorf("expected 0,0 for empty body, got %d,%d", in, out)
	}
	if text != "" {
		t.Errorf("responseText = %q, want empty", text)
	}
}

func TestProcessResponse_InvalidJSON(t *testing.T) {
	in, out, _ := ProcessResponse([]byte(`not json`), "")
	if in != 0 || out != 0 {
		t.Errorf("expected 0,0 for invalid JSON, got %d,%d", in, out)
	}
}
