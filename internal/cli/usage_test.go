//go:build unit

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"akswitch/internal/config"
)

func TestUsageCmd_Exists(t *testing.T) {
	if usageCmd == nil {
		t.Fatal("expected usageCmd to be defined")
	}
}

func TestUsageCmd_HasKeyFlag(t *testing.T) {
	if usageCmd.Flags().Lookup("key") == nil {
		t.Fatal("expected --key flag on usage command")
	}
}

func TestUsageCmd_HasProviderFlag(t *testing.T) {
	if usageCmd.Flags().Lookup("provider") == nil {
		t.Fatal("expected --provider flag on usage command")
	}
}

func TestUsageCmd_KeyRequired(t *testing.T) {
	origDefault := config.DefaultProviderName
	config.DefaultProviderName = "sensenova"
	defer func() { config.DefaultProviderName = origDefault }()

	rootCmd.SetArgs([]string{"usage"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --key is missing")
	}
	if !strings.Contains(err.Error(), "--key is required") {
		t.Fatalf("expected '--key is required' error, got: %v", err)
	}
}

func TestUsageCmd_ProviderRequired(t *testing.T) {
	config.DefaultProviderName = ""
	rootCmd.SetArgs([]string{"usage", "--key", "sk-xxx"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when provider is missing")
	}
	if !strings.Contains(err.Error(), "--provider is required") {
		t.Fatalf("expected '--provider is required' error, got: %v", err)
	}
}

func TestFindCredentialByKey_Found(t *testing.T) {
	jsonl := `{"api_key": "sk-111", "account": "a1", "password": "p1", "tenant_id": "t1"}
{"api_key_plain": "sk-222", "account": "a2", "password": "p2", "tenant_id": "t2"}`
	entry, err := findCredentialByKey([]byte(jsonl), "sk-222")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if entry.Account != "a2" || entry.TenantID != "t2" {
		t.Fatalf("unexpected entry: account=%s tenant=%s", entry.Account, entry.TenantID)
	}
}

func TestFindCredentialByKey_NotFound(t *testing.T) {
	_, err := findCredentialByKey([]byte(`{"api_key": "sk-111"}`), "sk-999")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestSensenovaLogin_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if payload["account"] != "test@example.com" {
			t.Fatalf("unexpected account: %s", payload["account"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"access_token": "tok-abc-123"}`))
	}))
	defer server.Close()

	token, err := sensenovaLoginWithURL("test@example.com", "pass123", server.URL+"/login")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if token != "tok-abc-123" {
		t.Fatalf("expected token 'tok-abc-123', got %s", token)
	}
}

func TestSensenovaLogin_AuthFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("invalid credentials"))
	}))
	defer server.Close()

	_, err := sensenovaLoginWithURL("bad", "bad", server.URL+"/login")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("expected HTTP 401 error, got: %v", err)
	}
}

func TestSensenovaQueryUsage_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"model_remaining_percent": {"deepseek-v4-flash": 27.8, "sensenova-6.7-flash-lite": 100}}`))
	}))
	defer server.Close()

	usages, err := sensenovaQueryUsageWithURL("test-token", "tenant-1", server.URL+"/usage")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(usages) != 2 {
		t.Fatalf("expected 2 models, got %d", len(usages))
	}
	if usages["deepseek-v4-flash"] != 27.8 {
		t.Fatalf("expected 27.8, got %f", usages["deepseek-v4-flash"])
	}
}

func TestSensenovaQueryUsage_EmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"model_remaining_percent": {}}`))
	}))
	defer server.Close()

	usages, err := sensenovaQueryUsageWithURL("tok", "t", server.URL+"/usage")
	if err != nil {
		t.Fatalf("expected no error for empty data, got: %v", err)
	}
	if len(usages) != 0 {
		t.Fatalf("expected 0 models, got %d", len(usages))
	}
}

func TestQueryUsage_UnsupportedProvider(t *testing.T) {
	err := queryUsage("unknown-provider", "sk-xxx", "/tmp")
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected 'not supported' error, got: %v", err)
	}
}
