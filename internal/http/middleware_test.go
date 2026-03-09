package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
)

// TestRequestLogger tests request logging middleware
func TestRequestLogger(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Suppress logs in test

	handler := RequestLogger(logger)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestHubSignatureVerifier tests Hub signature verification
func TestHubSignatureVerifier(t *testing.T) {
	// This test would need actual crypto implementation
	// For now, test the middleware structure
	publicKey := "test-public-key"

	handler := HubSignatureVerifier(publicKey)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Test without signature
	req := httptest.NewRequest("POST", "/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	// Test with invalid signature
	req = httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("X-Hub-Signature", "invalid")
	w = httptest.NewRecorder()
	handler(w, req)

	// Should fail signature verification
	if w.Code == http.StatusOK {
		t.Error("Expected error for invalid signature")
	}
}

// TestChain tests middleware chaining
func TestChain(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	middleware1 := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Middleware1", "true")
			next(w, r)
		}
	}

	middleware2 := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Middleware2", "true")
			next(w, r)
		}
	}

	handler := Chain(middleware1, middleware2)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Header().Get("X-Middleware1") != "true" {
		t.Error("Middleware1 should be applied")
	}
	if w.Header().Get("X-Middleware2") != "true" {
		t.Error("Middleware2 should be applied")
	}
}
