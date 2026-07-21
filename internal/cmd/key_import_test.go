//go:build unit

package cmd

import (
	"testing"

	"akswitch/internal/keypool"
)

// ── parseJSONL ────────────────────────────────────────

func TestParseJSONL_Basic(t *testing.T) {
	data := []byte(`{"key": "sk-111", "name": "alpha"}
{"key": "sk-222", "name": "beta"}
`)
	entries, err := parseJSONL(data)
	if err != nil {
		t.Fatalf("parseJSONL failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Key != "sk-111" || entries[0].Name != "alpha" {
		t.Errorf("entry 0 = %+v, want {Key: sk-111, Name: alpha}", entries[0])
	}
	if entries[1].Key != "sk-222" || entries[1].Name != "beta" {
		t.Errorf("entry 1 = %+v, want {Key: sk-222, Name: beta}", entries[1])
	}
}

func TestParseJSONL_WithApiKeyField(t *testing.T) {
	data := []byte(`{"api_key": "sk-111", "api_key_name": "alpha"}
{"api_key": "sk-222", "api_key_name": "beta"}
`)
	entries, err := parseJSONL(data)
	if err != nil {
		t.Fatalf("parseJSONL failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Key != "sk-111" || entries[0].Name != "alpha" {
		t.Errorf("entry 0 = %+v, want {Key: sk-111, Name: alpha}", entries[0])
	}
	if entries[1].Key != "sk-222" || entries[1].Name != "beta" {
		t.Errorf("entry 1 = %+v, want {Key: sk-222, Name: beta}", entries[1])
	}
}

func TestParseJSONL_WithApiKeyPlain(t *testing.T) {
	data := []byte(`{"api_key_plain": "sk-111", "api_key_name": "alpha"}
{"api_key_plain": "sk-222"}
`)
	entries, err := parseJSONL(data)
	if err != nil {
		t.Fatalf("parseJSONL failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Key != "sk-111" || entries[0].Name != "alpha" {
		t.Errorf("entry 0 = %+v, want {Key: sk-111, Name: alpha}", entries[0])
	}
	if entries[1].Key != "sk-222" || entries[1].Name != "" {
		t.Errorf("entry 1 = %+v, want {Key: sk-222, Name: \"\"}", entries[1])
	}
}

func TestParseJSONL_SkipsEmptyLines(t *testing.T) {
	data := []byte(`{"key": "sk-111"}

{"key": "sk-222"}
`)
	entries, err := parseJSONL(data)
	if err != nil {
		t.Fatalf("parseJSONL failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestParseJSONL_InvalidJSON(t *testing.T) {
	data := []byte(`{"key": "sk-111"}
not json
`)
	_, err := parseJSONL(data)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParseJSONL_EmptyData(t *testing.T) {
	_, err := parseJSONL([]byte{})
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
}

func TestParseJSONL_OnlyWhitespace(t *testing.T) {
	_, err := parseJSONL([]byte("\n\n   \n"))
	if err == nil {
		t.Fatal("expected error for whitespace-only data, got nil")
	}
}

// ── dedupEntries ──────────────────────────────────────

func TestDedupEntries_AllNew(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "alpha"},
		{Key: "sk-222", Name: "beta"},
	}
	store := &keypool.KeyStore{Keys: []keypool.KeyEntry{}}
	newEntries, skipped := dedupEntries(entries, store)
	if skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", skipped)
	}
	if len(newEntries) != 2 {
		t.Fatalf("expected 2 new entries, got %d", len(newEntries))
	}
}

func TestDedupEntries_SomeDuplicates(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "alpha"},
		{Key: "sk-222", Name: "beta"},
		{Key: "sk-333", Name: "gamma"},
	}
	store := &keypool.KeyStore{Keys: []keypool.KeyEntry{
		{Key: "sk-111", Name: "existing-alpha"},
		{Key: "sk-333", Name: "existing-gamma"},
	}}
	newEntries, skipped := dedupEntries(entries, store)
	if skipped != 2 {
		t.Errorf("expected 2 skipped, got %d", skipped)
	}
	if len(newEntries) != 1 {
		t.Fatalf("expected 1 new entry, got %d", len(newEntries))
	}
	if newEntries[0].Key != "sk-222" {
		t.Errorf("new entry = %+v, want {Key: sk-222}", newEntries[0])
	}
}

func TestDedupEntries_AllDuplicates(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "alpha"},
		{Key: "sk-222", Name: "beta"},
	}
	store := &keypool.KeyStore{Keys: []keypool.KeyEntry{
		{Key: "sk-111"},
		{Key: "sk-222"},
	}}
	newEntries, skipped := dedupEntries(entries, store)
	if skipped != 2 {
		t.Errorf("expected 2 skipped, got %d", skipped)
	}
	if len(newEntries) != 0 {
		t.Errorf("expected 0 new entries, got %d", len(newEntries))
	}
}

func TestDedupEntries_EmptyInput(t *testing.T) {
	newEntries, skipped := dedupEntries(nil, &keypool.KeyStore{Keys: []keypool.KeyEntry{}})
	if skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", skipped)
	}
	if len(newEntries) != 0 {
		t.Errorf("expected 0 new entries, got %d", len(newEntries))
	}
}

// ── autoNumberNames ───────────────────────────────────

func TestAutoNumberNames_UniqueNames(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "alpha"},
		{Key: "sk-222", Name: "beta"},
		{Key: "sk-333", Name: "gamma"},
	}
	result := autoNumberNames(entries)
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	if result[0].Name != "alpha-1" {
		t.Errorf("entry 0 name = %q, want %q", result[0].Name, "alpha-1")
	}
	if result[1].Name != "beta-1" {
		t.Errorf("entry 1 name = %q, want %q", result[1].Name, "beta-1")
	}
	if result[2].Name != "gamma-1" {
		t.Errorf("entry 2 name = %q, want %q", result[2].Name, "gamma-1")
	}
}

func TestAutoNumberNames_DuplicateNames(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "auto-reg"},
		{Key: "sk-222", Name: "auto-reg"},
		{Key: "sk-333", Name: "auto-reg"},
	}
	result := autoNumberNames(entries)
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	if result[0].Name != "auto-reg-1" {
		t.Errorf("entry 0 name = %q, want %q", result[0].Name, "auto-reg-1")
	}
	if result[1].Name != "auto-reg-2" {
		t.Errorf("entry 1 name = %q, want %q", result[1].Name, "auto-reg-2")
	}
	if result[2].Name != "auto-reg-3" {
		t.Errorf("entry 2 name = %q, want %q", result[2].Name, "auto-reg-3")
	}
}

func TestAutoNumberNames_EmptyNames(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "alpha"},
		{Key: "sk-222", Name: ""},
		{Key: "sk-333", Name: "alpha"},
	}
	result := autoNumberNames(entries)
	if result[0].Name != "alpha-1" {
		t.Errorf("entry 0 name = %q, want %q", result[0].Name, "alpha-1")
	}
	if result[1].Name != "" {
		t.Errorf("entry 1 name should be empty, got %q", result[1].Name)
	}
	if result[2].Name != "alpha-2" {
		t.Errorf("entry 2 name = %q, want %q", result[2].Name, "alpha-2")
	}
}

func TestAutoNumberNames_SingleEntry(t *testing.T) {
	entries := []keypool.KeyEntry{
		{Key: "sk-111", Name: "only-one"},
	}
	result := autoNumberNames(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Name != "only-one-1" {
		t.Errorf("name = %q, want %q", result[0].Name, "only-one-1")
	}
}