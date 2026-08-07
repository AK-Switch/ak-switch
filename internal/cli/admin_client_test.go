//go:build unit

package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewAdminClient_BuildsCorrectBaseURL(t *testing.T) {
	client, err := NewAdminClient(5*time.Second, "")
	if err != nil {
		t.Fatalf("NewAdminClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewAdminClient() returned nil")
	}
	if client.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
	if client.baseURL == "" {
		t.Fatal("baseURL is empty")
	}
}

func TestAdminClient_Do_InjectsAuthHeader(t *testing.T) {
	receivedToken := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("X-Admin-Token")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &AdminClient{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    srv.URL,
		token:      "test-token-123",
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/test", nil)
	_, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if receivedToken != "test-token-123" {
		t.Errorf("token = %q, want %q", receivedToken, "test-token-123")
	}
}

func TestAdminClient_Do_NoTokenNoHeader(t *testing.T) {
	receivedToken := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("X-Admin-Token")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &AdminClient{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    srv.URL,
		token:      "",
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/test", nil)
	_, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if receivedToken != "" {
		t.Errorf("token header = %q, want empty", receivedToken)
	}
}

func TestAdminClient_Do_ReturnsErrorForNilRequest(t *testing.T) {
	client := &AdminClient{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    "http://example.com",
		token:      "",
	}
	_, err := client.Do(nil)
	if err == nil {
		t.Error("Do(nil) expected error, got nil")
	}
}

func TestAdminClient_Get_AppendsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &AdminClient{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    srv.URL,
		token:      "",
	}

	_, err := client.Get("/api/health")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if gotPath != "/api/health" {
		t.Errorf("path = %q, want %q", gotPath, "/api/health")
	}
}

func TestAdminClient_Post_SetsContentType(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &AdminClient{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    srv.URL,
		token:      "",
	}

	_, err := client.Post("/api/test", "application/json", nil)
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
}
