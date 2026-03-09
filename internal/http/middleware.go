package http

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/crypto"
)

// Middleware type
type Middleware func(http.HandlerFunc) http.HandlerFunc

// Chain chains multiple middlewares
func Chain(middlewares ...Middleware) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

// HubSignatureVerifier verifies Hub signature
func HubSignatureVerifier(publicKey string) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			signature := r.Header.Get("X-Hub-Signature")
			if signature == "" {
				JSONError(w, http.StatusUnauthorized, "missing hub signature")
				return
			}

			// Read request body
			body, err := io.ReadAll(r.Body)
			if err != nil {
				JSONError(w, http.StatusBadRequest, "failed to read request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewBuffer(body))

			// Verify signature
			hash, err := crypto.HashCommand(body)
			if err != nil {
				JSONError(w, http.StatusBadRequest, "failed to hash command")
				return
			}

			if err := crypto.VerifyHubSignature(hash, signature, publicKey); err != nil {
				JSONError(w, http.StatusUnauthorized, "invalid hub signature")
				return
			}

			next(w, r)
		}
	}
}

// RequestLogger logs HTTP requests using logrus
func RequestLogger(logger *logrus.Logger) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next(w, r)
			duration := time.Since(start)
			logger.WithFields(logrus.Fields{
				"method":   r.Method,
				"path":     r.URL.Path,
				"duration": duration,
			}).Info("HTTP request")
		}
	}
}
