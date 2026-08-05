//go:build unit

package tokenestimator

import "testing"

func TestExtractModel_ValidJSON(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[]}`)
	if got := ExtractModel(body); got != "gpt-4o" {
		t.Errorf("ExtractModel = %q, want %q", got, "gpt-4o")
	}
}

func TestExtractModel_MissingField(t *testing.T) {
	body := []byte(`{"messages":[]}`)
	if got := ExtractModel(body); got != "" {
		t.Errorf("ExtractModel = %q, want empty", got)
	}
}

func TestExtractModel_InvalidJSON(t *testing.T) {
	if got := ExtractModel([]byte(`not json`)); got != "" {
		t.Errorf("ExtractModel = %q, want empty", got)
	}
}

func TestExtractModel_NilBody(t *testing.T) {
	if got := ExtractModel(nil); got != "" {
		t.Errorf("ExtractModel(nil) = %q, want empty", got)
	}
}

func TestExtractModel_EmptyBody(t *testing.T) {
	if got := ExtractModel([]byte{}); got != "" {
		t.Errorf("ExtractModel(empty) = %q, want empty", got)
	}
}
