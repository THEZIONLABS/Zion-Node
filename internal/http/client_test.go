package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestClientPostJSON tests POST JSON request
func TestClientPostJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Expected Content-Type: application/json")
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, 5*time.Second)
	client.SetHeader("X-Test", "test-value")

	ctx := context.Background()
	resp, err := client.PostJSON(ctx, "/test", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("X-Test") != "" {
		t.Error("Request should include custom header")
	}
}

// TestClientGet tests GET request
func TestClientGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, 5*time.Second)
	client.SetHeader("X-Test", "test-value")

	ctx := context.Background()
	resp, err := client.Get(ctx, "/test")
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestClientTimeout tests request timeout
func TestClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Simulate slow response
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, 100*time.Millisecond) // Very short timeout

	ctx := context.Background()
	_, err := client.Get(ctx, "/test")
	if err == nil {
		t.Error("Expected timeout error")
	}
}

// TestDecodeJSON tests JSON decoding
func TestDecodeJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"key": "value"})
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}

	var result map[string]string
	if err := DecodeJSON(resp, &result); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("Expected 'value', got %s", result["key"])
	}
}

// TestDecodeJSONError tests JSON decoding with error status
func TestDecodeJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}

	var result map[string]string
	if err := DecodeJSON(resp, &result); err == nil {
		t.Error("Expected error for non-200 status")
	}
}
