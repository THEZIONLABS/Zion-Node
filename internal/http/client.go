package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Client wraps http.Client with common utilities
type Client struct {
	httpClient *http.Client
	baseURL    string
	headers    map[string]string
}

// NewClient creates a new HTTP client with a properly configured connection pool.
// All connections go to a single hub host, so MaxIdleConnsPerHost is set high
// to enable keep-alive reuse and avoid repeated TCP/TLS handshakes.
func NewClient(baseURL string, timeout time.Duration) *Client {
	transport := &http.Transport{
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   10,  // single hub host — match total
		IdleConnTimeout:       120 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	return &Client{
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		baseURL: baseURL,
		headers: make(map[string]string),
	}
}

// SetHeader sets a header for all requests
func (c *Client) SetHeader(key, value string) {
	c.headers[key] = value
}

// PostJSON sends a POST request with JSON body
func (c *Client) PostJSON(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	return c.httpClient.Do(req)
}

// Get sends a GET request
func (c *Client) Get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	return c.httpClient.Do(req)
}

// PostMultipart sends a POST request with multipart form data
func (c *Client) PostMultipart(ctx context.Context, path string, fields map[string]string, fileField string, file io.Reader, fileName string) (*http.Response, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add fields
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, err
		}
	}

	// Add file
	if file != nil {
		part, err := writer.CreateFormFile(fileField, fileName)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(part, file); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	return c.httpClient.Do(req)
}

// DecodeJSON decodes JSON response
func DecodeJSON(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
