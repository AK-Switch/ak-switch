//go:build unit

package cmd

import (
	"strings"
	"testing"
)

func makeEntry(overrides map[string]interface{}) map[string]interface{} {
	entry := map[string]interface{}{
		"timestamp":   "2026-07-15T10:30:00.123Z",
		"method":      "GET",
		"url":         "/v1/chat/completions",
		"status":      "200",
		"provider":    "openai",
		"duration_ms": "1234",
		"retry":       "0",
		"key_name":    "my-key",
	}
	for k, v := range overrides {
		entry[k] = v
	}
	return entry
}

func TestFormatLogLine_Default_HidesMethodUrl(t *testing.T) {
	entry := makeEntry(nil)
	line := formatLogLine(entry, false)

	if strings.Contains(line, "GET") {
		t.Errorf("default view should not contain method, got: %s", line)
	}
	if strings.Contains(line, "/v1/chat/completions") {
		t.Errorf("default view should not contain URL, got: %s", line)
	}
}

func TestFormatLogLine_Default_ShowsStatusKeyDuration(t *testing.T) {
	entry := makeEntry(nil)
	line := formatLogLine(entry, false)

	if !strings.Contains(line, "200") {
		t.Errorf("default view should contain status, got: %s", line)
	}
	if !strings.Contains(line, "key: my-key") {
		t.Errorf("default view should contain key name, got: %s", line)
	}
	if !strings.Contains(line, "1234ms") {
		t.Errorf("default view should contain duration, got: %s", line)
	}
}

func TestFormatLogLine_Default_RetryZeroHidden(t *testing.T) {
	entry := makeEntry(map[string]interface{}{"retry": "0"})
	line := formatLogLine(entry, false)

	if strings.Contains(line, "retry") {
		t.Errorf("default view should not show retry=0, got: %s", line)
	}
}

func TestFormatLogLine_Default_RetryPositiveShown(t *testing.T) {
	entry := makeEntry(map[string]interface{}{"retry": "3"})
	line := formatLogLine(entry, false)

	if !strings.Contains(line, "retry 3") {
		t.Errorf("default view should show retry when >0, got: %s", line)
	}
}

func TestFormatLogLine_Default_EmptyDuration(t *testing.T) {
	entry := makeEntry(map[string]interface{}{"duration_ms": ""})
	line := formatLogLine(entry, false)

	if strings.Contains(line, "ms") {
		t.Errorf("default view should not show ms when duration is empty, got: %s", line)
	}
}

func TestFormatLogLine_Default_NoProvider(t *testing.T) {
	entry := makeEntry(map[string]interface{}{"provider": ""})
	line := formatLogLine(entry, false)

	if !strings.Contains(line, "key: my-key") {
		t.Errorf("default view should show key even without provider, got: %s", line)
	}
	if !strings.Contains(line, "1234ms") {
		t.Errorf("default view should show duration even without provider, got: %s", line)
	}
}

func TestFormatLogLine_Verbose_ShowsMethodUrl(t *testing.T) {
	entry := makeEntry(nil)
	line := formatLogLine(entry, true)

	if !strings.Contains(line, "GET") {
		t.Errorf("verbose view should contain method, got: %s", line)
	}
	if !strings.Contains(line, "/v1/chat/completions") {
		t.Errorf("verbose view should contain URL, got: %s", line)
	}
	if !strings.Contains(line, "200") {
		t.Errorf("verbose view should contain status, got: %s", line)
	}
	if !strings.Contains(line, "->") {
		t.Errorf("verbose view should contain '->' separator, got: %s", line)
	}
}

func TestFormatLogLine_Verbose_NegativeDuration(t *testing.T) {
	entry := makeEntry(map[string]interface{}{"duration_ms": "-1"})
	line := formatLogLine(entry, true)

	if !strings.Contains(line, "-1ms") {
		t.Errorf("verbose view should show negative duration, got: %s", line)
	}
}