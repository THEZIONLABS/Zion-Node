package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestJSONResponse tests JSON response writing
func TestJSONResponse(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	if err := JSONResponse(w, http.StatusOK, data); err != nil {
		t.Fatalf("Failed to write JSON response: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("Expected Content-Type: application/json")
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("Expected 'value', got %s", result["key"])
	}
}

// TestJSONError tests JSON error response
func TestJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	message := "test error"

	JSONError(w, http.StatusBadRequest, message)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("Expected Content-Type: application/json")
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["error"] != message {
		t.Errorf("Expected error message '%s', got %v", message, result["error"])
	}
}
