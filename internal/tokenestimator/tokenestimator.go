// Package tokenestimator provides token estimation and extraction utilities
// for API responses, supporting both Anthropic and OpenAI formats.
package tokenestimator

import (
	"encoding/json"
	"strings"

	"akswitch/internal/tracker"
	tiktoken "github.com/pkoukk/tiktoken-go"
)

// ExtractTokenUsage attempts to parse input_tokens/output_tokens from a JSON response body.
// Supports both Anthropic format (input_tokens/output_tokens) and OpenAI format
// (prompt_tokens/completion_tokens). Returns 0, 0 on failure.
func ExtractTokenUsage(body []byte) (inputTokens, outputTokens int) {
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

// ExtractResponseText extracts the text content from a response JSON body.
// Supports both Anthropic format (content array of {type, text}) and
// OpenAI format (choices[0].message.content). Returns empty string on failure.
func ExtractResponseText(body []byte) string {
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

// encodingForModel returns the appropriate tiktoken encoding name for a given model.
// Returns "cl100k_base" as the default fallback for unknown models.
func encodingForModel(model string) string {
	// gpt-4o / o1 / o3 series use o200k_base
	if strings.HasPrefix(model, "gpt-4o") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") {
		return "o200k_base"
	}
	return "cl100k_base"
}

// EstimateOutput uses tiktoken to estimate the number of tokens in a text string.
// The model parameter determines which tiktoken encoding to use.
// Returns 0 if tiktoken initialization fails or text is empty.
func EstimateOutput(text string, model string) int {
	if text == "" {
		return 0
	}
	tke, err := tiktoken.GetEncoding(encodingForModel(model))
	if err != nil {
		return 0
	}
	return len(tke.Encode(text, nil, nil))
}

// EstimateInput extracts message content from a request body and estimates
// the input token count using tiktoken. Returns 0 if parsing fails or body is empty.
// Supports both OpenAI format (content is a string) and Anthropic format
// (content is an array of {type, text} objects).
func EstimateInput(bodyBytes []byte, model string) int {
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
		switch msg.Content[0] {
		case '"':
			// String format: "content": "text"
			var s string
			if json.Unmarshal(msg.Content, &s) == nil {
				inputBuf.WriteString(s)
			}
		case '[':
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

// ParseSSEEvent parses a single SSE "data: " event line and returns
// the extracted text delta, thinking delta, and output token count.
// Returns (0, "", "") for non-data lines, unrecognized JSON, or events
// that don't carry text/token information.
func ParseSSEEvent(raw []byte) (outputTokens int, textDelta string, thinkingDelta string) {
	if len(raw) == 0 {
		return 0, "", ""
	}

	var result struct {
		Type  string `json:"type"`
		Delta *struct {
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
			Thinking    string `json:"thinking"`
		} `json:"delta,omitempty"`
		ContentBlock *struct {
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content_block,omitempty"`
		Usage *struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage,omitempty"`
		Choices []struct {
			Delta *struct {
				Content string `json:"content"`
			} `json:"delta,omitempty"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Type == "" {
		// Try OpenAI format (no "type" field)
		if err := json.Unmarshal(raw, &result); err != nil {
			return 0, "", ""
		}
		for _, choice := range result.Choices {
			if choice.Delta != nil {
				textDelta += choice.Delta.Content
			}
		}
		return 0, textDelta, ""
	}

	switch result.Type {
	case "content_block_delta":
		if result.Delta != nil {
			textDelta += result.Delta.Text
			textDelta += result.Delta.PartialJSON
			thinkingDelta += result.Delta.Thinking
		}
	case "content_block_start":
		if result.ContentBlock != nil {
			textDelta += result.ContentBlock.Text
			thinkingDelta += result.ContentBlock.Thinking
		}
	case "message_delta":
		if result.Usage != nil && result.Usage.OutputTokens > 0 {
			outputTokens = result.Usage.OutputTokens
		}
	}
	return outputTokens, textDelta, thinkingDelta
}

// ProcessResponse extracts the actual token counts and response text
// from a non-streaming API response body. Returns (inputTokens, outputTokens, responseText).
// This consolidates the repeated pattern of ExtractTokenUsage + ExtractResponseText calls.
func ProcessResponse(bodyBytes []byte) (inputTokens, outputTokens int, responseText string) {
	inputTokens, outputTokens = ExtractTokenUsage(bodyBytes)
	responseText = ExtractResponseText(bodyBytes)
	return
}

// RecordCalibration conditionally records calibration samples.
// Only records when estimate > 0 and actual > 0 for each direction.
// This consolidates the repeated if-estimate>0-actual>0 pattern.
func RecordCalibration(cal *tracker.Calibrator, model string, estInput, actualInput, estOutput, actualOutput int) {
	if cal == nil || model == "" {
		return
	}
	if estInput > 0 && actualInput > 0 {
		cal.Record(model, estInput, actualInput)
	}
	if estOutput > 0 && actualOutput > 0 {
		cal.Record(model, estOutput, actualOutput)
	}
}
