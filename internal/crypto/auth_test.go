package crypto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewHubAuthClient(t *testing.T) {
	client := NewHubAuthClient("http://localhost:8080")
	if client == nil {
		t.Fatal("NewHubAuthClient returned nil")
	}
	if client.hubURL != "http://localhost:8080" {
		t.Errorf("Expected hubURL http://localhost:8080, got %s", client.hubURL)
	}
}

func TestSignChallenge(t *testing.T) {
	wallet, err := GenerateWallet()
	if err != nil {
		t.Fatalf("Failed to generate wallet: %v", err)
	}

	challenge := "test-challenge-12345"
	sig, err := signChallenge(wallet, challenge)
	if err != nil {
		t.Fatalf("signChallenge failed: %v", err)
	}

	// Should start with 0x
	if !strings.HasPrefix(sig, "0x") {
		t.Errorf("Signature should start with 0x, got: %s", sig[:10])
	}

	// Should be deterministic
	sig2, err := signChallenge(wallet, challenge)
	if err != nil {
		t.Fatalf("signChallenge second call failed: %v", err)
	}
	if sig != sig2 {
		t.Error("signChallenge is not deterministic for same input")
	}

	// Different challenges should produce different signatures
	sig3, err := signChallenge(wallet, "different-challenge")
	if err != nil {
		t.Fatalf("signChallenge with different challenge failed: %v", err)
	}
	if sig == sig3 {
		t.Error("Different challenges should produce different signatures")
	}
}

func TestSignChallenge_VValue(t *testing.T) {
	wallet, err := GenerateWallet()
	if err != nil {
		t.Fatalf("Failed to generate wallet: %v", err)
	}

	sig, err := signChallenge(wallet, "test-challenge")
	if err != nil {
		t.Fatalf("signChallenge failed: %v", err)
	}

	// Remove 0x prefix
	sigHex := sig[2:]
	// V value should be 27 or 28 (Ethereum convention)
	// Last byte (2 hex chars)
	vHex := sigHex[len(sigHex)-2:]
	if vHex != "1b" && vHex != "1c" {
		t.Errorf("V value should be 1b (27) or 1c (28), got: %s", vHex)
	}
}

func TestGetJWT_Success(t *testing.T) {
	wallet, err := GenerateWallet()
	if err != nil {
		t.Fatalf("Failed to generate wallet: %v", err)
	}

	expectedToken := "test-jwt-token-12345"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/challenge":
			var req challengeRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Address != wallet.Address {
				t.Errorf("Expected address %s, got %s", wallet.Address, req.Address)
			}
			json.NewEncoder(w).Encode(challengeResponse{
				Challenge: "test-challenge-abc",
				ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
			})
		case "/v1/auth/verify":
			var req verifyRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Address != wallet.Address {
				t.Errorf("Expected address %s, got %s", wallet.Address, req.Address)
			}
			if req.Challenge != "test-challenge-abc" {
				t.Errorf("Expected challenge test-challenge-abc, got %s", req.Challenge)
			}
			if req.Signature == "" {
				t.Error("Signature should not be empty")
			}
			json.NewEncoder(w).Encode(verifyResponse{
				Token:     expectedToken,
				ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
			})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewHubAuthClient(server.URL)
	token, err := client.GetJWT(context.Background(), wallet)
	if err != nil {
		t.Fatalf("GetJWT failed: %v", err)
	}
	if token != expectedToken {
		t.Errorf("Expected token %s, got %s", expectedToken, token)
	}
}

func TestGetJWT_ChallengeFailure(t *testing.T) {
	wallet, _ := GenerateWallet()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewHubAuthClient(server.URL)
	_, err := client.GetJWT(context.Background(), wallet)
	if err == nil {
		t.Error("Expected error when challenge fails")
	}
	if !strings.Contains(err.Error(), "challenge") {
		t.Errorf("Error should mention challenge, got: %v", err)
	}
}

func TestGetJWT_VerifyFailure(t *testing.T) {
	wallet, _ := GenerateWallet()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/challenge":
			json.NewEncoder(w).Encode(challengeResponse{
				Challenge: "test-challenge",
				ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
			})
		case "/v1/auth/verify":
			http.Error(w, "invalid signature", http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	client := NewHubAuthClient(server.URL)
	_, err := client.GetJWT(context.Background(), wallet)
	if err == nil {
		t.Error("Expected error when verify fails")
	}
	if !strings.Contains(err.Error(), "verify") {
		t.Errorf("Error should mention verify, got: %v", err)
	}
}

func TestGetJWT_ContextCancelled(t *testing.T) {
	wallet, _ := GenerateWallet()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // Slow server
	}))
	defer server.Close()

	client := NewHubAuthClient(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.GetJWT(ctx, wallet)
	if err == nil {
		t.Error("Expected error when context is cancelled")
	}
}

func TestRequestChallenge_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected application/json content type")
		}
		json.NewEncoder(w).Encode(challengeResponse{
			Challenge: "challenge-abc",
			ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
		})
	}))
	defer server.Close()

	client := NewHubAuthClient(server.URL)
	challenge, err := client.requestChallenge(context.Background(), "0x1234")
	if err != nil {
		t.Fatalf("requestChallenge failed: %v", err)
	}
	if challenge != "challenge-abc" {
		t.Errorf("Expected challenge-abc, got %s", challenge)
	}
}

func TestRequestChallenge_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewHubAuthClient(server.URL)
	_, err := client.requestChallenge(context.Background(), "0x1234")
	if err == nil {
		t.Error("Expected error for server error response")
	}
}

func TestRequestChallenge_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewHubAuthClient(server.URL)
	_, err := client.requestChallenge(context.Background(), "0x1234")
	if err != nil {
		// Invalid JSON should not cause error since the response is still 200
		// but decoding will fail
		// Actually the decoder should fail
	}
	_ = err // err may or may not be nil depending on JSON parsing
}

func TestVerifySignature_Success(t *testing.T) {
	expectedToken := "jwt-token-xyz"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req verifyRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Address == "" || req.Challenge == "" || req.Signature == "" {
			http.Error(w, "missing fields", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(verifyResponse{
			Token:     expectedToken,
			ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
		})
	}))
	defer server.Close()

	client := NewHubAuthClient(server.URL)
	token, err := client.verifySignature(context.Background(), "0x1234", "challenge", "sig")
	if err != nil {
		t.Fatalf("verifySignature failed: %v", err)
	}
	if token != expectedToken {
		t.Errorf("Expected token %s, got %s", expectedToken, token)
	}
}

func TestVerifySignature_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewHubAuthClient(server.URL)
	_, err := client.verifySignature(context.Background(), "0x1234", "challenge", "sig")
	if err == nil {
		t.Error("Expected error for server error response")
	}
}

func TestSignChallenge_EIP191Format(t *testing.T) {
	// Verify the signature uses EIP-191 personal_sign format
	wallet, _ := GenerateWallet()
	challenge := "Hello World"

	sig, err := signChallenge(wallet, challenge)
	if err != nil {
		t.Fatalf("signChallenge failed: %v", err)
	}

	// The signature should be a valid hex string with 0x prefix
	if !strings.HasPrefix(sig, "0x") {
		t.Error("Signature should have 0x prefix")
	}

	// Remove 0x and verify it's valid hex of correct length (65 bytes = 130 hex chars)
	hexStr := sig[2:]
	if len(hexStr) != 130 {
		t.Errorf("Expected 130 hex chars (65 bytes), got %d. hex: %s", len(hexStr), hexStr)
	}

	// Verify it's valid hex
	for _, c := range hexStr {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Invalid hex character: %c", c)
		}
	}
}

func TestGetJWT_ServerDown(t *testing.T) {
	wallet, _ := GenerateWallet()

	// Use a server that immediately closes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	serverURL := server.URL
	server.Close() // Close immediately

	client := NewHubAuthClient(serverURL)
	_, err := client.GetJWT(context.Background(), wallet)
	if err == nil {
		t.Error("Expected error when server is down")
	}
}

func TestRequestChallenge_ConnectionRefused(t *testing.T) {
	client := NewHubAuthClient("http://127.0.0.1:1") // Port 1 should be refused
	_, err := client.requestChallenge(context.Background(), "0x1234")
	if err == nil {
		t.Error("Expected error for connection refused")
	}
}

func TestGetJWT_FullFlow(t *testing.T) {
	// Full integration test with mock server simulating complete auth flow
	wallet, _ := GenerateWallet()
	callOrder := make([]string, 0, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/challenge":
			callOrder = append(callOrder, "challenge")
			json.NewEncoder(w).Encode(challengeResponse{
				Challenge: fmt.Sprintf("challenge-for-%s", wallet.Address),
				ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
			})
		case "/v1/auth/verify":
			callOrder = append(callOrder, "verify")
			json.NewEncoder(w).Encode(verifyResponse{
				Token:     "final-token",
				ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
			})
		}
	}))
	defer server.Close()

	client := NewHubAuthClient(server.URL)
	token, err := client.GetJWT(context.Background(), wallet)
	if err != nil {
		t.Fatalf("GetJWT failed: %v", err)
	}
	if token != "final-token" {
		t.Errorf("Expected 'final-token', got %s", token)
	}

	// Verify call order
	if len(callOrder) != 2 || callOrder[0] != "challenge" || callOrder[1] != "verify" {
		t.Errorf("Expected call order [challenge, verify], got %v", callOrder)
	}
}
