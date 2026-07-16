package tracker

import (
	"math"
	"testing"
)

// ── computeRawCoefficient ───────────────────────────────

func TestComputeRawCoefficient_EmptyList(t *testing.T) {
	c := computeRawCoefficient(nil)
	if c != 1.0 {
		t.Errorf("empty list: got %f, want 1.0", c)
	}
	c = computeRawCoefficient([]float64{})
	if c != 1.0 {
		t.Errorf("empty slice: got %f, want 1.0", c)
	}
}

func TestComputeRawCoefficient_ExactMatch(t *testing.T) {
	// tiktoken_estimate == actual_tokens => ratio = 1.0 => coeff = 1.0
	c := computeRawCoefficient([]float64{1.0, 1.0, 1.0})
	if c != 1.0 {
		t.Errorf("exact match: got %f, want 1.0", c)
	}
}

func TestComputeRawCoefficient_TiktokenOverestimates(t *testing.T) {
	// tiktoken_estimate = 150, actual = 100 => ratio = 1.5 => coeff = 1/1.5 = 0.667
	c := computeRawCoefficient([]float64{1.5})
	got := math.Round(c*1000) / 1000
	want := 0.667
	if got != want {
		t.Errorf("overestimate: got %f, want %f", got, want)
	}
}

func TestComputeRawCoefficient_TiktokenUnderestimates(t *testing.T) {
	// tiktoken_estimate = 50, actual = 100 => ratio = 0.5 => coeff = 1/0.5 = 2.0
	c := computeRawCoefficient([]float64{0.5})
	if c != 2.0 {
		t.Errorf("underestimate: got %f, want 2.0", c)
	}
}

func TestComputeRawCoefficient_Median(t *testing.T) {
	// ratios: [0.8, 1.0, 1.5] => median = 1.0 => coeff = 1.0
	c := computeRawCoefficient([]float64{0.8, 1.0, 1.5})
	if c != 1.0 {
		t.Errorf("median 1.0: got %f, want 1.0", c)
	}

	// ratios: [0.5, 0.6, 2.0] => median = 0.6 => coeff = 1/0.6 = 1.667
	c = computeRawCoefficient([]float64{0.5, 0.6, 2.0})
	got := math.Round(c*1000) / 1000
	want := 1.667
	if got != want {
		t.Errorf("median 0.6: got %f, want %f", got, want)
	}
}

// ── clampCoefficient ────────────────────────────────────

func TestClampCoefficient_WithinRange(t *testing.T) {
	c := clampCoefficient(1.0)
	if c != 1.0 {
		t.Errorf("within range: got %f, want 1.0", c)
	}
	c = clampCoefficient(0.5)
	if c != 0.5 {
		t.Errorf("within range: got %f, want 0.5", c)
	}
}

func TestClampCoefficient_BelowMin(t *testing.T) {
	c := clampCoefficient(0.2)
	if c != 0.3 {
		t.Errorf("below min: got %f, want 0.3", c)
	}
	c = clampCoefficient(0.0)
	if c != 0.3 {
		t.Errorf("below min (zero): got %f, want 0.3", c)
	}
	c = clampCoefficient(-1.0)
	if c != 0.3 {
		t.Errorf("below min (negative): got %f, want 0.3", c)
	}
}

func TestClampCoefficient_AboveMax(t *testing.T) {
	c := clampCoefficient(5.0)
	if c != 3.0 {
		t.Errorf("above max: got %f, want 3.0", c)
	}
}

// ── Calibrator ──────────────────────────────────────────

func TestCalibrator_DefaultCoefficient(t *testing.T) {
	c := New()
	coeff := c.Coefficient("unknown-model")
	if coeff != 1.0 {
		t.Errorf("unknown model: got %f, want 1.0", coeff)
	}
}

func TestCalibrator_SampleCount(t *testing.T) {
	c := New()
	if cnt := c.SampleCount("gpt-4"); cnt != 0 {
		t.Errorf("initial count: got %d, want 0", cnt)
	}

	c.Record("gpt-4", 150, 100)
	if cnt := c.SampleCount("gpt-4"); cnt != 1 {
		t.Errorf("after one record: got %d, want 1", cnt)
	}
}

func TestCalibrator_RecordAndCoefficient(t *testing.T) {
	c := New()

	// tiktoken estimates 150, actual 100 => ratio=1.5 => raw coeff=0.667
	coeff := c.Record("gpt-4", 150, 100)
	got := math.Round(coeff*1000) / 1000
	want := 0.667
	if got != want {
		t.Errorf("first record: got %f, want %f", got, want)
	}

	// Same ratio again, should keep same coefficient
	coeff = c.Record("gpt-4", 150, 100)
	got = math.Round(coeff*1000) / 1000
	if got != want {
		t.Errorf("second record: got %f, want %f", got, want)
	}
}

func TestCalibrator_PerModelIsolation(t *testing.T) {
	c := New()

	// Model A: tiktoken overestimates
	c.Record("model-a", 200, 100) // ratio=2.0 => coeff=0.5
	// Model B: tiktoken underestimates
	c.Record("model-b", 50, 100) // ratio=0.5 => coeff=2.0

	coeffA := c.Coefficient("model-a")
	coeffB := c.Coefficient("model-b")

	if coeffA != 0.5 {
		t.Errorf("model-a: got %f, want 0.5", coeffA)
	}
	if coeffB != 2.0 {
		t.Errorf("model-b: got %f, want 2.0", coeffB)
	}
}

func TestCalibrator_SlidingWindow(t *testing.T) {
	c := New()

	// Fill window with ratio=1.0 (exact match)
	for i := 0; i < 20; i++ {
		c.Record("gpt-4", 100, 100)
	}

	// After 20 inserts, window should have only 15 entries (last 15)
	coeff := c.Coefficient("gpt-4")
	if coeff != 1.0 {
		t.Errorf("after 20 exact records: got %f, want 1.0", coeff)
	}

	// Now add a bad ratio=2.0 - it should push out one 1.0
	// Window: 14x 1.0 + 1x 2.0 => median = 1.0 => coeff = 1.0
	c.Record("gpt-4", 200, 100)
	coeff = c.Coefficient("gpt-4")
	if coeff != 1.0 {
		t.Errorf("after outlier: got %f, want 1.0", coeff)
	}

	// Fill the window with 2.0 ratios
	for i := 0; i < 15; i++ {
		c.Record("gpt-4", 200, 100)
	}

	// Now window should be all 2.0 ratios => median=2.0 => raw coeff=0.5
	coeff = c.Coefficient("gpt-4")
	if coeff != 0.5 {
		t.Errorf("after full 2.0 window: got %f, want 0.5", coeff)
	}
}

func TestCalibrator_ZeroValuesIgnored(t *testing.T) {
	c := New()

	// Zero or negative values should not affect calibration
	c.Record("gpt-4", 0, 100)
	c.Record("gpt-4", 100, 0)
	c.Record("gpt-4", -1, 100)
	c.Record("gpt-4", 100, -1)

	if cnt := c.SampleCount("gpt-4"); cnt != 0 {
		t.Errorf("should have ignored zero/negative values: got %d, want 0", cnt)
	}

	coeff := c.Coefficient("gpt-4")
	if coeff != 1.0 {
		t.Errorf("default coefficient: got %f, want 1.0", coeff)
	}
}

func TestCalibrator_ClampOutOfRange(t *testing.T) {
	c := New()

	// ratio = 5.0 (tiktoken overestimates by 5x) => raw coeff=0.2, clamped to 0.3
	coeff := c.Record("extreme", 500, 100)
	if coeff != 0.3 {
		t.Errorf("clamp low: got %f, want 0.3", coeff)
	}

	// ratio = 0.2 (tiktoken underestimates by 5x) => raw coeff=5.0, clamped to 3.0
	coeff = c.Record("extreme2", 20, 100)
	if coeff != 3.0 {
		t.Errorf("clamp high: got %f, want 3.0", coeff)
	}
}

func TestCalibrator_ConcurrentSafe(t *testing.T) {
	c := New()

	// Run concurrent records to detect data races
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				c.Record("gpt-4", 100, 100)
				_ = c.Coefficient("gpt-4")
				_ = c.SampleCount("gpt-4")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have some samples (at least 10*10 = 100, but window capped at 15)
	if cnt := c.SampleCount("gpt-4"); cnt < 100 {
		t.Errorf("too few samples after concurrent access: got %d, want >= 100", cnt)
	}
}

// ── Acceptance criteria: streaming vs non-streaming deviation < 20% ──────────

func TestCalibrator_AcceptanceDeviation(t *testing.T) {
	// Simulate the scenario: same prompt, once streaming (using tiktoken), once
	// non-streaming (using actual tokens). Calibrated streaming estimate should
	// be within 20% of the non-streaming actual value.

	c := New()

	// Seed calibration with 10 non-streaming samples
	// true ratio: tiktoken estimates 100 tokens per 100 actual tokens
	for i := 0; i < 10; i++ {
		c.Record("gpt-4", 100, 100)
	}

	// Streaming request: tiktoken estimates 85 tokens for the output
	// Before calibration (coeff = 1.0): raw = 85
	// After calibration: corrected = 85 * 1.0 = 85
	// Actual (from non-streaming): 100
	// Deviation: |85 - 100| / 100 = 15% < 20% ✓
	coeff := c.Coefficient("gpt-4")
	raw := 85
	corrected := int(math.Round(float64(raw) * coeff))
	actual := 100
	deviation := math.Abs(float64(corrected-actual)) / float64(actual)
	if deviation > 0.20 {
		t.Errorf("deviation %.0f%% > 20%%: raw=%d coeff=%.3f corrected=%d actual=%d",
			deviation*100, raw, coeff, corrected, actual)
	}
}

func TestCalibrator_AcceptanceRealistic(t *testing.T) {
	// More realistic: tiktoken overestimates by ~20% for this model
	c := New()

	// Non-streaming samples where tiktoken estimates 120, actual is 100
	// ratio = 1.2 => coeff = 1/1.2 = 0.833
	for i := 0; i < 15; i++ {
		c.Record("gpt-4", 120, 100)
	}

	coeff := c.Coefficient("gpt-4")
	got := math.Round(coeff*1000) / 1000
	want := 0.833
	if got != want {
		t.Errorf("coefficient: got %f, want %f", got, want)
	}

	// Streaming: tiktoken estimates 60 tokens
	// Corrected: 60 * 0.833 = 50
	// Actual: 50
	// Deviation: 0%
	raw := 60
	corrected := int(math.Round(float64(raw) * coeff))
	actual := 50
	deviation := math.Abs(float64(corrected-actual)) / float64(actual)
	if deviation > 0.20 {
		t.Errorf("deviation %.0f%% > 20%%: raw=%d coeff=%.3f corrected=%d actual=%d",
			deviation*100, raw, coeff, corrected, actual)
	}
}

func TestCalibrator_AcceptanceUnderestimate(t *testing.T) {
	// Tiktoken underestimates for this model
	c := New()

	// Non-streaming samples where tiktoken estimates 80, actual is 100
	// ratio = 0.8 => coeff = 1/0.8 = 1.25
	for i := 0; i < 15; i++ {
		c.Record("gpt-4", 80, 100)
	}

	coeff := c.Coefficient("gpt-4")
	if coeff != 1.25 {
		t.Errorf("coefficient: got %f, want 1.25", coeff)
	}

	// Streaming: tiktoken estimates 40 tokens
	// Corrected: 40 * 1.25 = 50
	// Actual: 50
	raw := 40
	corrected := int(math.Round(float64(raw) * coeff))
	actual := 50
	deviation := math.Abs(float64(corrected-actual)) / float64(actual)
	if deviation > 0.20 {
		t.Errorf("deviation %.0f%% > 20%%: raw=%d coeff=%.3f corrected=%d actual=%d",
			deviation*100, raw, coeff, corrected, actual)
	}
}