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