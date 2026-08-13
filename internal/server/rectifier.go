package server

import (
	"encoding/json"
	"log/slog"
)

// ThinkingRectifier modifies request bodies to convert unsupported thinking.type
// values (e.g., "adaptive") to values accepted by the upstream API.
type ThinkingRectifier struct {
	enabled bool
	mapTo   string
	stats   RectifierStats
}

// RectifierStats holds counters for observability.
type RectifierStats struct {
	Total       int64
	Modified    int64
	Passthrough int64
}

// NewThinkingRectifier creates a rectifier with the given configuration.
// If enabled is false, Process() is a no-op passthrough.
// mapTo must be one of: "enabled", "auto", "disabled".
func NewThinkingRectifier(enabled bool, mapTo string) *ThinkingRectifier {
	if enabled && mapTo == "" {
		mapTo = "enabled"
	}
	return &ThinkingRectifier{
		enabled: enabled,
		mapTo:   mapTo,
	}
}

// ShouldRectify returns true if this rectifier is active and has a valid map target.
func (r *ThinkingRectifier) ShouldRectify() bool {
	return r.enabled && r.mapTo != ""
}

// Process examines the JSON body and modifies thinking.type from "adaptive"
// to the configured mapTo value. Returns the original body on any parse/marshal
// failure (safe degradation).
func (r *ThinkingRectifier) Process(body []byte) []byte {
	r.stats.Total++

	if !r.enabled || r.mapTo == "" {
		r.stats.Passthrough++
		return body
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		slog.Warn("thinking rectifier: JSON parse failed, passthrough", "error", err)
		r.stats.Passthrough++
		return body
	}

	thinking, ok := data["thinking"].(map[string]interface{})
	if !ok {
		r.stats.Passthrough++
		return body
	}

	thinkingType, ok := thinking["type"].(string)
	if !ok || thinkingType != "adaptive" {
		r.stats.Passthrough++
		return body
	}

	thinking["type"] = r.mapTo
	modified, err := json.Marshal(data)
	if err != nil {
		slog.Warn("thinking rectifier: JSON marshal failed, passthrough", "error", err)
		r.stats.Passthrough++
		return body
	}

	r.stats.Modified++
	return modified
}

// Stats returns a copy of the current rectifier statistics.
func (r *ThinkingRectifier) Stats() RectifierStats {
	return r.stats
}
