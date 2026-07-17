// Package tracker provides usage tracking utilities including
// token estimation calibration and sliding-window statistics.
package tracker

import (
	"math"
	"sort"
	"sync"
)

// Calibrator maintains per-model calibration ratios using a sliding window.
// It compares tiktoken estimates against actual upstream token counts (from
// non-streaming responses) to compute a correction factor.
//
// The calibration ratio is the median of (actual / estimate) for the last N samples.
// Ratios are applied to streaming estimates to improve accuracy.
type Calibrator struct {
	mu         sync.RWMutex
	windowSize int
	models     map[string][]sample
}

type sample struct {
	estimate int
	actual   int
	ratio    float64
}

// NewCalibrator creates a Calibrator with the given sliding window size.
// windowSize should be at least 3 for meaningful calibration.
func NewCalibrator(windowSize int) *Calibrator {
	if windowSize < 3 {
		windowSize = 3
	}
	return &Calibrator{
		windowSize: windowSize,
		models:     make(map[string][]sample),
	}
}

// Record adds a calibration sample for a model.
// estimate is the tiktoken-estimated value, actual is the upstream-reported value.
// Samples with estimate <= 0 or actual <= 0 are silently skipped.
func (c *Calibrator) Record(model string, estimate, actual int) {
	if estimate <= 0 || actual <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	s := sample{
		estimate: estimate,
		actual:   actual,
		ratio:    float64(actual) / float64(estimate),
	}

	samples := c.models[model]
	samples = append(samples, s)

	// Trim to window size
	if len(samples) > c.windowSize {
		samples = samples[len(samples)-c.windowSize:]
	}

	c.models[model] = samples
}

// Ratio returns the calibration ratio for a model.
// Returns 1.0 if fewer than 3 samples are available (no meaningful calibration).
func (c *Calibrator) Ratio(model string) float64 {
	c.mu.RLock()
	samples := c.models[model]
	c.mu.RUnlock()

	if len(samples) < 3 {
		return 1.0
	}

	// Compute median of ratios
	ratios := make([]float64, len(samples))
	for i, s := range samples {
		ratios[i] = s.ratio
	}
	sort.Float64s(ratios)

	return ratios[len(ratios)/2]
}

// Apply adjusts a token estimate using the model's calibration ratio.
// Returns the original estimate if no calibration data is available.
func (c *Calibrator) Apply(model string, estimate int) int {
	ratio := c.Ratio(model)
	if ratio == 1.0 {
		return estimate
	}
	adjusted := int(math.Round(float64(estimate) * ratio))
	if adjusted < 0 {
		return 0
	}
	return adjusted
}

// ResetModel clears all calibration data for a specific model.
func (c *Calibrator) ResetModel(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.models, model)
}

// Models returns the list of model names that have calibration data.
func (c *Calibrator) Models() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.models))
	for name := range c.models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SampleCount returns the number of calibration samples for a model.
func (c *Calibrator) SampleCount(model string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.models[model])
}
