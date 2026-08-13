package server

import "testing"

func TestThinkingRectifier_AdaptiveToEnabled(t *testing.T) {
	r := NewThinkingRectifier(true, "enabled")
	body := []byte(`{"model":"gpt-4","thinking":{"type":"adaptive"}}`)
	result := r.Process(body)
	if string(result) == string(body) {
		t.Fatal("expected body to be modified")
	}
	stats := r.Stats()
	if stats.Modified != 1 {
		t.Fatalf("expected 1 modified, got %d", stats.Modified)
	}
	if stats.Total != 1 {
		t.Fatalf("expected 1 total, got %d", stats.Total)
	}
}

func TestThinkingRectifier_AdaptiveToAuto(t *testing.T) {
	r := NewThinkingRectifier(true, "auto")
	body := []byte(`{"thinking":{"type":"adaptive"}}`)
	result := r.Process(body)
	if string(result) == string(body) {
		t.Fatal("expected body to be modified")
	}
	stats := r.Stats()
	if stats.Modified != 1 {
		t.Fatalf("expected 1 modified, got %d", stats.Modified)
	}
}

func TestThinkingRectifier_NonAdaptivePassthrough(t *testing.T) {
	r := NewThinkingRectifier(true, "enabled")
	body := []byte(`{"thinking":{"type":"enabled"}}`)
	result := r.Process(body)
	if string(result) != string(body) {
		t.Fatal("expected body unchanged for non-adaptive type")
	}
	stats := r.Stats()
	if stats.Passthrough != 1 {
		t.Fatalf("expected 1 passthrough, got %d", stats.Passthrough)
	}
}

func TestThinkingRectifier_NoThinkingField(t *testing.T) {
	r := NewThinkingRectifier(true, "enabled")
	body := []byte(`{"model":"gpt-4"}`)
	result := r.Process(body)
	if string(result) != string(body) {
		t.Fatal("expected body unchanged when no thinking field")
	}
	stats := r.Stats()
	if stats.Passthrough != 1 {
		t.Fatalf("expected 1 passthrough, got %d", stats.Passthrough)
	}
}

func TestThinkingRectifier_InvalidJSON(t *testing.T) {
	r := NewThinkingRectifier(true, "enabled")
	body := []byte(`not valid json {{{`)
	result := r.Process(body)
	if string(result) != string(body) {
		t.Fatal("expected original body returned on JSON parse failure")
	}
}

func TestThinkingRectifier_DisabledSkips(t *testing.T) {
	r := NewThinkingRectifier(false, "enabled")
	body := []byte(`{"thinking":{"type":"adaptive"}}`)
	result := r.Process(body)
	if string(result) != string(body) {
		t.Fatal("expected body unchanged when rectifier disabled")
	}
}

func TestThinkingRectifier_DefaultModeSkips(t *testing.T) {
	r := NewThinkingRectifier(true, "")
	body := []byte(`{"thinking":{"type":"adaptive"}}`)
	result := r.Process(body)
	if string(result) != string(body) {
		t.Fatal("expected body unchanged with empty mapTo")
	}
}
