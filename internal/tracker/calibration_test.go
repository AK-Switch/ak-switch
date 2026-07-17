//go:build unit

package tracker

import (
	"testing"
)

// ── Calibrator ─────────────────────────────────────────

func TestCalibrator_NoSamplesReturnsDefault(t *testing.T) {
	c := NewCalibrator(15)
	ratio := c.Ratio("deepseek-v4-flash")
	if ratio != 1.0 {
		t.Errorf("Ratio() with no samples = %f, want 1.0", ratio)
	}
}

func TestCalibrator_SingleSampleReturnsDefault(t *testing.T) {
	c := NewCalibrator(15)
	c.Record("deepseek-v4-flash", 100, 90)
	ratio := c.Ratio("deepseek-v4-flash")
	if ratio != 1.0 {
		t.Errorf("Ratio() with 1 sample = %f, want 1.0 (need >= 3)", ratio)
	}
}

func TestCalibrator_TwoSamplesReturnsDefault(t *testing.T) {
	c := NewCalibrator(15)
	c.Record("deepseek-v4-flash", 100, 90)
	c.Record("deepseek-v4-flash", 200, 180)
	ratio := c.Ratio("deepseek-v4-flash")
	if ratio != 1.0 {
		t.Errorf("Ratio() with 2 samples = %f, want 1.0 (need >= 3)", ratio)
	}
}

func TestCalibrator_ThreeSamplesReturnsMedian(t *testing.T) {
	c := NewCalibrator(15)
	// estimate=100, actual=90 → ratio=0.9
	c.Record("deepseek-v4-flash", 100, 90)
	// estimate=100, actual=100 → ratio=1.0
	c.Record("deepseek-v4-flash", 100, 100)
	// estimate=100, actual=110 → ratio=1.1
	c.Record("deepseek-v4-flash", 100, 110)

	ratio := c.Ratio("deepseek-v4-flash")
	// Median of [0.9, 1.0, 1.1] = 1.0
	if ratio < 0.99 || ratio > 1.01 {
		t.Errorf("Ratio() = %f, want ~1.0 (median of 0.9, 1.0, 1.1)", ratio)
	}
}

func TestCalibrator_FiveSamples(t *testing.T) {
	c := NewCalibrator(15)
	// ratios: 0.5, 0.8, 1.0, 1.2, 1.5 → median = 1.0
	c.Record("deepseek-v4-flash", 100, 50)
	c.Record("deepseek-v4-flash", 100, 80)
	c.Record("deepseek-v4-flash", 100, 100)
	c.Record("deepseek-v4-flash", 100, 120)
	c.Record("deepseek-v4-flash", 100, 150)

	ratio := c.Ratio("deepseek-v4-flash")
	if ratio < 0.99 || ratio > 1.01 {
		t.Errorf("Ratio() = %f, want ~1.0 (median of 5)", ratio)
	}
}

func TestCalibrator_PerModelTracking(t *testing.T) {
	c := NewCalibrator(15)

	// Model A: ratios 0.8, 0.9, 1.0 → median ~0.9
	c.Record("model-a", 100, 80)
	c.Record("model-a", 100, 90)
	c.Record("model-a", 100, 100)

	// Model B: ratios 1.0, 1.1, 1.2 → median ~1.1
	c.Record("model-b", 100, 100)
	c.Record("model-b", 100, 110)
	c.Record("model-b", 100, 120)

	ratioA := c.Ratio("model-a")
	ratioB := c.Ratio("model-b")

	if ratioA < 0.88 || ratioA > 0.92 {
		t.Errorf("model-a ratio = %f, want ~0.9", ratioA)
	}
	if ratioB < 1.08 || ratioB > 1.12 {
		t.Errorf("model-b ratio = %f, want ~1.1", ratioB)
	}
}

func TestCalibrator_SlidingWindowDropsOldest(t *testing.T) {
	c := NewCalibrator(3) // window size = 3

	// Add 3 samples: ratios 0.5, 0.6, 0.7 → median = 0.6
	c.Record("m", 100, 50)
	c.Record("m", 100, 60)
	c.Record("m", 100, 70)

	ratio := c.Ratio("m")
	if ratio < 0.59 || ratio > 0.61 {
		t.Errorf("after 3 samples ratio = %f, want ~0.6", ratio)
	}

	// Add 4th sample: ratios 0.5, 0.6, 0.7, 1.0 → window = [0.6, 0.7, 1.0] (dropped 0.5)
	// median of [0.6, 0.7, 1.0] = 0.7
	c.Record("m", 100, 100)
	ratio = c.Ratio("m")
	if ratio < 0.69 || ratio > 0.71 {
		t.Errorf("after 4 samples ratio = %f, want ~0.7", ratio)
	}
}

func TestCalibrator_ZeroEstimateSkipped(t *testing.T) {
	c := NewCalibrator(15)
	c.Record("m", 0, 100) // estimate=0, should be skipped
	c.Record("m", 100, 90)
	c.Record("m", 100, 100)
	c.Record("m", 100, 110)

	ratio := c.Ratio("m")
	if ratio < 0.99 || ratio > 1.01 {
		t.Errorf("after zero estimate skip, ratio = %f, want ~1.0", ratio)
	}
}

func TestCalibrator_ZeroActualReturnsDefault(t *testing.T) {
	c := NewCalibrator(15)
	c.Record("m", 100, 0) // actual=0, should be skipped (division by zero)
	c.Record("m", 100, 90)
	c.Record("m", 100, 100)
	c.Record("m", 100, 110)

	ratio := c.Ratio("m")
	if ratio < 0.99 || ratio > 1.01 {
		t.Errorf("after zero actual skip, ratio = %f, want ~1.0", ratio)
	}
}

func TestCalibrator_ApplyAdjustsValue(t *testing.T) {
	c := NewCalibrator(15)
	c.Record("m", 100, 80)
	c.Record("m", 100, 90)
	c.Record("m", 100, 100)

	adjusted := c.Apply("m", 100)
	// ratio ~0.9, so 100 * 0.9 = 90
	if adjusted < 88 || adjusted > 92 {
		t.Errorf("Apply(100) = %d, want ~90", adjusted)
	}
}

func TestCalibrator_ApplyNoSamplesReturnsOriginal(t *testing.T) {
	c := NewCalibrator(15)
	adjusted := c.Apply("m", 100)
	if adjusted != 100 {
		t.Errorf("Apply() with no samples = %d, want 100", adjusted)
	}
}

func TestCalibrator_LargeWindow(t *testing.T) {
	c := NewCalibrator(100)

	// Fill with 50 samples at ratio 0.8
	for i := 0; i < 50; i++ {
		c.Record("m", 100, 80)
	}
	ratio := c.Ratio("m")
	if ratio < 0.79 || ratio > 0.81 {
		t.Errorf("with 50 samples ratio = %f, want ~0.8", ratio)
	}
}

func TestCalibrator_MultipleModels(t *testing.T) {
	c := NewCalibrator(15)

	// Model A: 3 samples
	c.Record("model-a", 100, 80)
	c.Record("model-a", 100, 90)
	c.Record("model-a", 100, 100)

	// Model B: 3 samples
	c.Record("model-b", 200, 180)
	c.Record("model-b", 200, 200)
	c.Record("model-b", 200, 220)

	// Model C: no samples
	ratioC := c.Ratio("model-c")
	if ratioC != 1.0 {
		t.Errorf("model-c (no samples) ratio = %f, want 1.0", ratioC)
	}
}

func TestCalibrator_ResetModel(t *testing.T) {
	c := NewCalibrator(15)
	c.Record("m", 100, 80)
	c.Record("m", 100, 90)
	c.Record("m", 100, 100)

	c.ResetModel("m")
	ratio := c.Ratio("m")
	if ratio != 1.0 {
		t.Errorf("after reset ratio = %f, want 1.0", ratio)
	}
}

func TestCalibrator_AllModels(t *testing.T) {
	c := NewCalibrator(15)
	c.Record("m1", 100, 80)
	c.Record("m1", 100, 90)
	c.Record("m1", 100, 100)

	c.Record("m2", 200, 180)
	c.Record("m2", 200, 200)
	c.Record("m2", 200, 220)

	models := c.Models()
	if len(models) != 2 {
		t.Errorf("Models() returned %d, want 2", len(models))
	}
}

func TestCalibrator_ConcurrentSafe(t *testing.T) {
	c := NewCalibrator(15)

	// Basic concurrent access test
	done := make(chan bool)
	go func() {
		for i := 0; i < 10; i++ {
			c.Record("m", 100, 90)
		}
		done <- true
	}()
	go func() {
		for i := 0; i < 10; i++ {
			_ = c.Ratio("m")
		}
		done <- true
	}()
	go func() {
		for i := 0; i < 10; i++ {
			_ = c.Apply("m", 100)
		}
		done <- true
	}()

	<-done
	<-done
	<-done
	// If we get here without a data race, the test passes
	// (run with -race to verify)
}