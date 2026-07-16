// Package tracker provides token-estimation calibration for streaming responses.
//
// Non-streaming API responses return accurate token counts in their usage block.
// By comparing tiktoken estimates against these ground-truth values, we can
// derive per-model calibration coefficients that correct streaming estimates.
package tracker

import (
	"log/slog"
	"math"
	"sort"
	"sync"
)

const (
	defaultWindowSize = 15
	minCoefficient    = 0.3
	maxCoefficient    = 3.0
)

// Calibrator maintains per-model calibration coefficients using a sliding
// window of tiktoken-estimate-to-actual-token ratios collected from
// non-streaming API responses.
type Calibrator struct {
	mu         sync.RWMutex
	models     map[string]*calibrationData
	windowSize int
}

// calibrationData holds the sliding window of ratios and derived coefficient
// for a single model.
type calibrationData struct {
	ratios []float64 // sliding window of tiktoken_estimate / actual_tokens
	coeff  float64   // current calibration coefficient (1 / median of ratios)
	count  int       // total samples received (including evicted entries)
}

// New creates a Calibrator with default settings (window size 15).
func New() *Calibrator {
	return &Calibrator{
		models:     make(map[string]*calibrationData),
		windowSize: defaultWindowSize,
	}
}

// Record feeds a new observation into the calibrator. It records the ratio of
// tiktoken_estimate to actual_tokens for the given model, recomputes the
// calibration coefficient from the sliding window, and returns the updated
// (clamped) coefficient.
//
// If tiktokenEstimate or actualTokens is <= 0, the call is silently ignored.
func (c *Calibrator) Record(model string, tiktokenEstimate, actualTokens int) float64 {
	if tiktokenEstimate <= 0 || actualTokens <= 0 {
		return c.coefficient(model)
	}

	ratio := float64(tiktokenEstimate) / float64(actualTokens)

	c.mu.Lock()

	data, ok := c.models[model]
	if !ok {
		data = &calibrationData{}
		c.models[model] = data
	}

	data.ratios = append(data.ratios, ratio)
	if len(data.ratios) > c.windowSize {
		data.ratios = data.ratios[1:]
	}
	data.count++

	rawCoeff := computeRawCoefficient(data.ratios)
	data.coeff = clampCoefficient(rawCoeff)
	sampleCount := data.count
	windowLen := len(data.ratios)
	clamped := data.coeff

	c.mu.Unlock()

	if rawCoeff < minCoefficient || rawCoeff > maxCoefficient {
		slog.Warn("calibration coefficient out of range",
			"model", model,
			"raw_coefficient", rawCoeff,
			"clamped_coefficient", clamped,
			"samples", sampleCount,
			"window_size", windowLen,
			"min", minCoefficient,
			"max", maxCoefficient,
		)
	}

	return clamped
}

// Coefficient returns the current calibration coefficient for the given model.
// Returns 1.0 (no correction) if no samples have been recorded for the model.
func (c *Calibrator) Coefficient(model string) float64 {
	return c.coefficient(model)
}

func (c *Calibrator) coefficient(model string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, ok := c.models[model]
	if !ok || data.count == 0 {
		return 1.0
	}
	return data.coeff
}

// SampleCount returns the total number of samples recorded for the given model.
func (c *Calibrator) SampleCount(model string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, ok := c.models[model]
	if !ok {
		return 0
	}
	return data.count
}

// computeRawCoefficient derives the raw (unclamped) calibration coefficient
// from a list of ratios (tiktoken_estimate / actual_tokens). It uses the
// median of ratios: coefficient = 1 / median(ratio).
//
// Returns 1.0 if the ratios slice is empty to avoid division by zero.
func computeRawCoefficient(ratios []float64) float64 {
	if len(ratios) == 0 {
		return 1.0
	}

	sorted := make([]float64, len(ratios))
	copy(sorted, ratios)
	sort.Float64s(sorted)

	median := sorted[len(sorted)/2]

	// Guard against division by zero or near-zero median
	if math.Abs(median) < 1e-10 {
		return 1.0
	}

	return 1.0 / median
}

// clampCoefficient restricts a raw coefficient to [minCoefficient, maxCoefficient].
func clampCoefficient(coeff float64) float64 {
	switch {
	case coeff < minCoefficient:
		return minCoefficient
	case coeff > maxCoefficient:
		return maxCoefficient
	default:
		return coeff
	}
}