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
		"http_timeout_sec", "log_level", "health_check_interval_sec",
		"admin_token", "disable_thinking", "genai_model", "keys_file",
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
	// Server-side runtimeConfigFields handles only these keys (from admin_api.go initRuntimeConfigDescriptors)
	serverHandled := map[string]bool{
		"http_timeout_sec":      true,
		"max_retries":           true,
		"cooldown_sec":          true,
		"backoff_cap_sec":       true,
		"backoff_multiplier":    true,
		"cb_reset_sec":          true,
		"upstream_cb_threshold": true,
		"log_level":             true,
	}

	for _, fd := range ConfigFieldDescriptors {
		if fd.RuntimeEditable && !serverHandled[fd.Key] {
			t.Errorf("field %q has RuntimeEditable=true but server does not handle it", fd.Key)
		}
	}
}
