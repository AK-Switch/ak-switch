//go:build unit

package config

import "testing"

func TestFindField_Existing(t *testing.T) {
	f := FindField("target")
	if f == nil {
		t.Fatal("expected to find 'target' field")
	}
	if f.Key != "target" {
		t.Errorf("expected key 'target', got %q", f.Key)
	}
	if f.Scope != FieldScopeProvider {
		t.Errorf("expected provider scope, got %s", f.Scope)
	}
}

func TestFindField_Missing(t *testing.T) {
	if FindField("nonexistent") != nil {
		t.Fatal("expected nil for unknown key")
	}
}

func TestFindField_AllRegistered(t *testing.T) {
	expected := []string{
		"target", "cooldown_sec", "max_retries", "backoff_cap_sec",
		"backoff_multiplier", "cb_reset_sec", "upstream_cb_threshold",
		"http_timeout_sec", "log_level", "thinking_mode",
		"rectify_thinking_map_to", "health_check_interval_sec",
		"admin_token", "disable_thinking", "genai_model", "keys_file", "key_selection",
		"port", "log_file",
	}
	for _, key := range expected {
		if FindField(key) == nil {
			t.Errorf("field %q not registered", key)
		}
	}
}

func TestFieldDescriptor_ParseInt(t *testing.T) {
	f := FindField("cooldown_sec")
	if f == nil {
		t.Fatal("field not found")
	}
	v, err := f.Parse("30")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if v.(int) != 30 {
		t.Errorf("expected 30, got %v", v)
	}
}

func TestFieldDescriptor_ParseInvalidInt(t *testing.T) {
	f := FindField("cooldown_sec")
	if f == nil {
		t.Fatal("field not found")
	}
	if _, err := f.Parse("abc"); err == nil {
		t.Error("expected error for non-numeric input")
	}
}

func TestFieldDescriptor_ParseBool(t *testing.T) {
	f := FindField("disable_thinking")
	if f == nil {
		t.Fatal("field not found")
	}
	v, err := f.Parse("true")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !v.(bool) {
		t.Error("expected true")
	}
}

func TestFieldDescriptor_FormatInt(t *testing.T) {
	f := FindField("cooldown_sec")
	if f == nil {
		t.Fatal("field not found")
	}
	s := f.Format(30)
	if s != "30" {
		t.Errorf("expected '30', got %q", s)
	}
}

func TestFieldDescriptor_FormatFloat(t *testing.T) {
	f := FindField("backoff_multiplier")
	if f == nil {
		t.Fatal("field not found")
	}
	s := f.Format(2.0)
	if s != "2" {
		t.Errorf("expected '2', got %q", s)
	}
}

func TestFieldDescriptor_LogLevelParse_ValidValues(t *testing.T) {
	f := FindField("log_level")
	if f == nil {
		t.Fatal("field not found")
	}

	for _, level := range []string{"debug", "info", "warn", "error"} {
		v, err := f.Parse(level)
		if err != nil {
			t.Errorf("Parse(%q) returned error: %v", level, err)
		}
		if v.(string) != level {
			t.Errorf("Parse(%q) = %q, want %q", level, v.(string), level)
		}
	}
}

func TestFieldDescriptor_LogLevelParse_InvalidValue(t *testing.T) {
	f := FindField("log_level")
	if f == nil {
		t.Fatal("field not found")
	}

	_, err := f.Parse("invalid_level")
	if err == nil {
		t.Error("expected error for invalid log level, got nil")
	}
}

func TestProviderRuntimeInterface_NoCallers(t *testing.T) {
	// providerRuntime was an unused interface in field_descriptor.go.
	// This test documents that ConfigFieldDescriptors work without it.
	// If the interface is reintroduced, this test should be updated.
	for _, fd := range ConfigFieldDescriptors {
		if fd.Key == "" {
			t.Fatal("empty key in descriptor table")
		}
	}
}

func TestFieldDescriptor_RuntimeEditableMatchesServer(t *testing.T) {
	// Server-side handles only these keys (via config.FindField + RuntimeEditable)
	serverHandled := map[string]bool{
		"http_timeout_sec":        true,
		"max_retries":             true,
		"cooldown_sec":            true,
		"backoff_cap_sec":         true,
		"backoff_multiplier":      true,
		"cb_reset_sec":            true,
		"upstream_cb_threshold":   true,
		"log_level":               true,
		"thinking_mode":           true,
		"rectify_thinking_map_to": true,
	}

	for _, fd := range ConfigFieldDescriptors {
		if fd.RuntimeEditable && !serverHandled[fd.Key] {
			t.Errorf("field %q has RuntimeEditable=true but server does not handle it", fd.Key)
		}
	}
}

func TestKeySelectionDescriptor(t *testing.T) {
	d := FindField("key_selection")
	if d == nil {
		t.Fatal("FindField('key_selection') returned nil")
	}
	if d.Default != "polling" {
		t.Errorf("default = %q, want 'polling'", d.Default)
	}
	if d.RuntimeEditable {
		t.Error("key_selection should not be runtime editable")
	}

	tests := []struct {
		input string
		want  string
		err   bool
	}{
		{"polling", "polling", false},
		{"random", "random", false},
		{"smart", "", true},
		{"unknown", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		got, err := d.Parse(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("Parse(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsValidLogLevel(t *testing.T) {
	valid := []string{"debug", "info", "warn", "error"}
	for _, l := range valid {
		if !IsValidLogLevel(l) {
			t.Errorf("IsValidLogLevel(%q) = false, want true", l)
		}
	}
	// Case-insensitive normalization is done by callers, not by IsValidLogLevel
	invalid := []string{"", "trace", "fatal", "DEBUG", "INFO"}
	for _, l := range invalid {
		if IsValidLogLevel(l) {
			t.Errorf("IsValidLogLevel(%q) = true, want false", l)
		}
	}
}
