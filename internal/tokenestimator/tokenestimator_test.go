//go:build unit

package tokenestimator

import (
	"testing"

	"akswitch/internal/tracker"
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
	tokens, delta, _ := ParseSSEEvent(raw)
	if tokens != 0 {
		t.Errorf("expected 0 tokens, got %d", tokens)
	}
	if delta != "hello{\"key\"" {
		t.Errorf("expected `hello{\"key\"`, got %q", delta)
	}
}

func TestParseSSEEvent_ContentBlockStart(t *testing.T) {
	raw := []byte(`{"type":"content_block_start","content_block":{"text":"greeting"}}`)
	_, delta, _ := ParseSSEEvent(raw)
	if delta != "greeting" {
		t.Errorf("expected `greeting`, got %q", delta)
	}
}

func TestParseSSEEvent_MessageDelta(t *testing.T) {
	raw := []byte(`{"type":"message_delta","usage":{"output_tokens":42}}`)
	tokens, _, _ := ParseSSEEvent(raw)
	if tokens != 42 {
		t.Errorf("expected 42 tokens, got %d", tokens)
	}
}

func TestParseSSEEvent_OpenAIFormat(t *testing.T) {
	raw := []byte(`{"choices":[{"delta":{"content":"hi"}}]}`)
	_, delta, _ := ParseSSEEvent(raw)
	if delta != "hi" {
		t.Errorf("expected `hi`, got %q", delta)
	}
}

func TestParseSSEEvent_NonDataLine(t *testing.T) {
	raw := []byte(`event: ping`)
	tokens, delta, _ := ParseSSEEvent(raw)
	if tokens != 0 || delta != "" {
		t.Errorf("expected (0, \"\"), got (%d, %q)", tokens, delta)
	}
}

func TestParseSSEEvent_InvalidJSON(t *testing.T) {
	raw := []byte(`not json`)
	tokens, delta, _ := ParseSSEEvent(raw)
	if tokens != 0 || delta != "" {
		t.Errorf("expected (0, \"\"), got (%d, %q)", tokens, delta)
	}
}

func TestParseSSEEvent_PartialJSON(t *testing.T) {
	raw := []byte(`{"type":"content_block_delta","delta":{"partial_json":"{\"name\":"}}`)
	_, delta, _ := ParseSSEEvent(raw)
	if delta != "{\"name\":" {
		t.Errorf("expected `{\"name\":`, got %q", delta)
	}
}

func TestParseSSEEvent_ThinkingDelta(t *testing.T) {
	raw := []byte(`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"Let me think step by step"}}`)
	_, _, delta := ParseSSEEvent(raw)
	if delta != "Let me think step by step" {
		t.Errorf("expected `Let me think step by step`, got %q", delta)
	}
}

func TestParseSSEEvent_ContentBlockStartThinking(t *testing.T) {
	raw := []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"deep reasoning"}}`)
	_, _, delta := ParseSSEEvent(raw)
	if delta != "deep reasoning" {
		t.Errorf("expected `deep reasoning`, got %q", delta)
	}
}

func TestParseSSEEvent_ThinkingAndTextDelta(t *testing.T) {
	raw := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"reasoning","text":"answer"}}`)
	_, textDelta, thinkingDelta := ParseSSEEvent(raw)
	if textDelta != "answer" {
		t.Errorf("expected `answer`, got %q", textDelta)
	}
	if thinkingDelta != "reasoning" {
		t.Errorf("expected `reasoning`, got %q", thinkingDelta)
	}
}

func TestParseSSEEvent_ThinkingAndTextCombined(t *testing.T) {
	events := []struct {
		name string
		raw  []byte
	}{
		{"thinking_start", []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"Let me think"}}`)},
		{"thinking_delta", []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" step by step"}}`)},
		{"text_start", []byte(`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":"Here is"}}`)},
		{"text_delta", []byte(`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":" the answer"}}`)},
		{"message_delta", []byte(`{"type":"message_delta","usage":{"output_tokens":50}}`)},
	}

	var accumulated string
	var apiTokens int
	for _, ev := range events {
		tokens, textDelta, thinkingDelta := ParseSSEEvent(ev.raw)
		accumulated += textDelta + thinkingDelta
		if tokens > 0 {
			apiTokens = tokens
		}
	}

	expected := "Let me think step by stepHere is the answer"
	if accumulated != expected {
		t.Errorf("accumulated text = %q, want %q", accumulated, expected)
	}
	if apiTokens != 50 {
		t.Errorf("apiTokens = %d, want 50", apiTokens)
	}
}

// ── ProcessResponse ──────────────────────────

func TestProcessResponse_AnthropicFormat(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"text","text":"hello"}]}`)
	in, out, text := ProcessResponse(body)
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
	in, out, text := ProcessResponse(body)
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
	in, out, _ := ProcessResponse(body)
	if in != 0 || out != 0 {
		t.Errorf("expected 0,0 for missing usage, got %d,%d", in, out)
	}
}

func TestProcessResponse_EmptyBody(t *testing.T) {
	in, out, text := ProcessResponse([]byte{})
	if in != 0 || out != 0 {
		t.Errorf("expected 0,0 for empty body, got %d,%d", in, out)
	}
	if text != "" {
		t.Errorf("responseText = %q, want empty", text)
	}
}

func TestProcessResponse_InvalidJSON(t *testing.T) {
	in, out, _ := ProcessResponse([]byte(`not json`))
	if in != 0 || out != 0 {
		t.Errorf("expected 0,0 for invalid JSON, got %d,%d", in, out)
	}
}

// ── RecordCalibration ──────────────────────────

func TestRecordCalibration_BothPositive(t *testing.T) {
	cal := tracker.NewCalibrator(100)
	RecordCalibration(cal, "gpt-4o", 10, 10, 5, 5)
	// No panic = pass. Calibrator internals are tested separately.
}

func TestRecordCalibration_ZeroInput(t *testing.T) {
	cal := tracker.NewCalibrator(100)
	RecordCalibration(cal, "gpt-4o", 0, 0, 5, 5)   // estInput=0, should not record
	RecordCalibration(cal, "gpt-4o", 10, 10, 0, 0) // estOutput=0, should not record
	// No panic = pass
}

func TestRecordCalibration_AllZero(t *testing.T) {
	cal := tracker.NewCalibrator(100)
	RecordCalibration(cal, "", 0, 0, 0, 0) // nothing to record
	// No panic = pass
}

func TestRecordCalibration_EmptyModel(t *testing.T) {
	cal := tracker.NewCalibrator(100)
	RecordCalibration(cal, "", 10, 10, 5, 5) // empty model, should not record
	// No panic = pass
}
