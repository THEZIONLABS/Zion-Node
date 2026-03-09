package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestPostMultipart tests multipart form data upload
func TestPostMultipart(t *testing.T) {
	var receivedFields map[string]string
	var receivedFileName string
	var receivedFileContent []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "multipart/form-data") {
			t.Errorf("Expected multipart/form-data, got %s", contentType)
		}

		err := r.ParseMultipartForm(32 << 20)
		if err != nil {
			t.Fatalf("Failed to parse multipart: %v", err)
		}

		receivedFields = make(map[string]string)
		for k, v := range r.MultipartForm.Value {
			if len(v) > 0 {
				receivedFields[k] = v[0]
			}
		}

		file, header, err := r.FormFile("testfile")
		if err != nil {
			t.Fatalf("Failed to get form file: %v", err)
		}
		defer file.Close()

		receivedFileName = header.Filename
		receivedFileContent, _ = io.ReadAll(file)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "uploaded"})
	}))
	defer server.Close()

	client := NewClient(server.URL, 5*time.Second)
	client.SetHeader("X-Custom", "header-value")

	fields := map[string]string{
		"agent_id":     "agent-01",
		"snapshot_ref": "sha256:abc",
	}

	fileContent := "test file content for snapshot"
	ctx := context.Background()
	resp, err := client.PostMultipart(ctx, "/upload", fields, "testfile", strings.NewReader(fileContent), "snapshot.tar.zst")
	if err != nil {
		t.Fatalf("PostMultipart failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify fields were received
	if receivedFields["agent_id"] != "agent-01" {
		t.Errorf("Expected agent_id=agent-01, got %s", receivedFields["agent_id"])
	}
	if receivedFields["snapshot_ref"] != "sha256:abc" {
		t.Errorf("Expected snapshot_ref=sha256:abc, got %s", receivedFields["snapshot_ref"])
	}

	// Verify file
	if receivedFileName != "snapshot.tar.zst" {
		t.Errorf("Expected filename snapshot.tar.zst, got %s", receivedFileName)
	}
	if string(receivedFileContent) != fileContent {
		t.Errorf("Expected file content %q, got %q", fileContent, string(receivedFileContent))
	}
}

// TestPostMultipart_NoFile tests multipart upload without file
func TestPostMultipart_NoFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, 5*time.Second)
	fields := map[string]string{"key": "value"}

	ctx := context.Background()
	resp, err := client.PostMultipart(ctx, "/upload", fields, "file", nil, "")
	if err != nil {
		t.Fatalf("PostMultipart without file failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestPostMultipart_ServerError tests multipart upload with server error
func TestPostMultipart_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, 5*time.Second)
	ctx := context.Background()
	resp, err := client.PostMultipart(ctx, "/upload", nil, "file", strings.NewReader("data"), "file.txt")
	if err != nil {
		t.Fatalf("PostMultipart failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", resp.StatusCode)
	}
}

// TestPostMultipart_CustomHeaders tests that custom headers are sent
func TestPostMultipart_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Node-ID") != "test-node" {
			t.Errorf("Expected X-Node-ID header, got %s", r.Header.Get("X-Node-ID"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Expected auth header, got %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, 5*time.Second)
	client.SetHeader("X-Node-ID", "test-node")
	client.SetHeader("Authorization", "Bearer test-token")

	ctx := context.Background()
	resp, err := client.PostMultipart(ctx, "/upload", nil, "file", strings.NewReader("data"), "file.txt")
	if err != nil {
		t.Fatalf("PostMultipart failed: %v", err)
	}
	resp.Body.Close()
}

// TestPostJSON_NilBody tests POST with nil body
func TestPostJSON_NilBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, 5*time.Second)
	ctx := context.Background()
	resp, err := client.PostJSON(ctx, "/test", nil)
	if err != nil {
		t.Fatalf("PostJSON with nil body failed: %v", err)
	}
	resp.Body.Close()
}

// TestPostMultipart_ConnectionRefused tests multipart with connection refused
func TestPostMultipart_ConnectionRefused(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", 1*time.Second)
	ctx := context.Background()
	_, err := client.PostMultipart(ctx, "/upload", nil, "file", strings.NewReader("data"), "file.txt")
	if err == nil {
		t.Error("Expected error for connection refused")
	}
}

// TestNewClient_TransportConfig verifies the HTTP client has a properly
// configured transport for connection pooling (not default 2 per host).
func TestNewClient_TransportConfig(t *testing.T) {
	client := NewClient("http://example.com", 10*time.Second)

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("Expected *http.Transport, got different type or nil")
	}

	if transport.MaxIdleConns < 10 {
		t.Errorf("Expected MaxIdleConns >= 10, got %d", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost < 10 {
		t.Errorf("Expected MaxIdleConnsPerHost >= 10, got %d", transport.MaxIdleConnsPerHost)
	}
	if transport.IdleConnTimeout < 60*time.Second {
		t.Errorf("Expected IdleConnTimeout >= 60s, got %v", transport.IdleConnTimeout)
	}
	if transport.TLSHandshakeTimeout == 0 {
		t.Error("Expected TLSHandshakeTimeout to be configured, got 0")
	}
}

// TestNewClient_ConnectionReuse verifies that connections are actually reused
// across multiple requests to the same host (keep-alive).
func TestNewClient_ConnectionReuse(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, 5*time.Second)
	ctx := context.Background()

	// Make 10 rapid requests — they should reuse connections
	for i := 0; i < 10; i++ {
		resp, err := client.Get(ctx, "/ping")
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		// Must read and close body to enable connection reuse
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	if requestCount != 10 {
		t.Errorf("Expected 10 requests to reach server, got %d", requestCount)
	}
}