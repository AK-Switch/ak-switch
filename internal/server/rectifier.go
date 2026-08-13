package server

import (
	"encoding/json"
	"log/slog"
)

// ThinkingRectifier modifies request bodies to convert unsupported thinking.type
// values (e.g., "adaptive") to values accepted by the upstream API.
type ThinkingRectifier struct {
	enabled bool
	mapTo   string // "enabled" | "auto" | "disabled"
}

// NewThinkingRectifier creates a rectifier with the given configuration.
// If enabled is false or mapTo is empty, Process() is a no-op passthrough.
func NewThinkingRectifier(enabled bool, mapTo string) *ThinkingRectifier {
	return &ThinkingRectifier{
		enabled: enabled,
		mapTo:   mapTo,
	}
}

// Process examines the JSON body and modifies thinking.type from "adaptive"
// to the configured mapTo value. Returns (modifiedBody, true) when rectification
// occurred, or (originalBody, false) on any parse/marshal failure or passthrough.
func (r *ThinkingRectifier) Process(body []byte) ([]byte, bool) {
	if !r.enabled || r.mapTo == "" {
		return body, false
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		slog.Warn("thinking rectifier: JSON parse failed, passthrough", "error", err)
		return body, false
	}

	thinking, ok := data["thinking"].(map[string]interface{})
	if !ok {
		return body, false
	}

	thinkingType, ok := thinking["type"].(string)
	if !ok || thinkingType != "adaptive" {
		return body, false
	}

	thinking["type"] = r.mapTo
	modified, err := json.Marshal(data)
	if err != nil {
		slog.Warn("thinking rectifier: JSON marshal failed, passthrough", "error", err)
		return body, false
	}

	return modified, true
}
