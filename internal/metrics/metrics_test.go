//go:build unit

package metrics

import (
	"testing"
)

func TestTokenUsageCounterRegistration(t *testing.T) {
	reg, m := NewRegistry()
	if m.TokenUsage == nil {
		t.Fatal("TokenUsage counter should not be nil")
	}

	// Increment and verify
	m.TokenUsage.WithLabelValues("test-provider", "input").Add(100)
	m.TokenUsage.WithLabelValues("test-provider", "output").Add(50)

	// Read back via registry (prometheus client_golang)
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	foundInput := false
	foundOutput := false
	for _, mf := range metrics {
		if mf.GetName() == "akswitch_token_usage_total" {
			for _, m := range mf.GetMetric() {
				labels := m.GetLabel()
				for _, l := range labels {
					if l.GetName() == "direction" && l.GetValue() == "input" {
						foundInput = true
						if m.GetCounter().GetValue() != 100 {
							t.Errorf("input token count = %v, want 100", m.GetCounter().GetValue())
						}
					}
					if l.GetName() == "direction" && l.GetValue() == "output" {
						foundOutput = true
						if m.GetCounter().GetValue() != 50 {
							t.Errorf("output token count = %v, want 50", m.GetCounter().GetValue())
						}
					}
				}
			}
		}
	}
	if !foundInput {
		t.Error("input token metric not found in gathered metrics")
	}
	if !foundOutput {
		t.Error("output token metric not found in gathered metrics")
	}
}

func TestLogStoreMetricsRegistration(t *testing.T) {
	reg, m := NewRegistry()
	if m.LogStoreEntries == nil {
		t.Fatal("LogStoreEntries counter should not be nil")
	}
	if m.LogStoreDropped == nil {
		t.Fatal("LogStoreDropped counter should not be nil")
	}
	if m.LogStoreFillRatio == nil {
		t.Fatal("LogStoreFillRatio gauge should not be nil")
	}

	// Increment counters and set gauge
	m.LogStoreEntries.Inc()
	m.LogStoreEntries.Inc()
	m.LogStoreDropped.Inc()
	m.LogStoreFillRatio.Set(0.75)

	// Read back via registry
	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	checks := map[string]float64{
		"akswitch_logstore_entries_total": 2,
		"akswitch_logstore_dropped_total": 1,
	}
	for _, mf := range metrics {
		name := mf.GetName()
		if expected, ok := checks[name]; ok {
			for _, m := range mf.GetMetric() {
				if m.GetCounter().GetValue() != expected {
					t.Errorf("%s = %v, want %v", name, m.GetCounter().GetValue(), expected)
				}
			}
		}
		if name == "akswitch_logstore_fill_ratio" {
			for _, m := range mf.GetMetric() {
				if m.GetGauge().GetValue() != 0.75 {
					t.Errorf("fill_ratio = %v, want 0.75", m.GetGauge().GetValue())
				}
			}
		}
	}
}

func TestRetryCounterRegistration(t *testing.T) {
	reg, m := NewRegistry()
	if m.RetryCount == nil {
		t.Fatal("RetryCount counter should not be nil")
	}

	// Increment and verify
	m.RetryCount.WithLabelValues("test-provider").Add(5)
	m.RetryCount.WithLabelValues("test-provider").Add(3)

	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	found := false
	for _, mf := range metrics {
		if mf.GetName() == "akswitch_retries_total" {
			for _, m := range mf.GetMetric() {
				if m.GetCounter().GetValue() != 8 {
					t.Errorf("retry count = %v, want 8", m.GetCounter().GetValue())
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("retry metric not found in gathered metrics")
	}

	// Verify provider label
	if m.RetryCount.WithLabelValues("other-provider").Add(0); true {
		// Ensure no panic on label assignment
	}
}

func TestUptimeGaugeRegistration(t *testing.T) {
	reg, m := NewRegistry()
	if m.UptimeSeconds == nil {
		t.Fatal("UptimeSeconds gauge should not be nil")
	}

	// Set and verify
	m.UptimeSeconds.Set(123.456)

	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	found := false
	for _, mf := range metrics {
		if mf.GetName() == "akswitch_uptime_seconds" {
			for _, m := range mf.GetMetric() {
				val := m.GetGauge().GetValue()
				if val < 123 || val > 124 {
					t.Errorf("uptime = %v, want near 123.456", val)
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("uptime metric not found in gathered metrics")
	}
}
