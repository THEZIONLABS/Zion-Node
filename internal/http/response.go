package http

import (
	"encoding/json"
	"net/http"
)

// JSONResponse writes JSON response
func JSONResponse(w http.ResponseWriter, statusCode int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(data)
}

// JSONError writes JSON error response
func JSONError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": message,
	})
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}
