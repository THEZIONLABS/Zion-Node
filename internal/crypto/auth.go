package crypto

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// HubAuthClient handles Hub authentication
type HubAuthClient struct {
	hubURL     string
	httpClient *http.Client
}

// NewHubAuthClient creates a new Hub auth client
func NewHubAuthClient(hubURL string) *HubAuthClient {
	return &HubAuthClient{
		hubURL: hubURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetJWT obtains JWT token using wallet signature
func (c *HubAuthClient) GetJWT(ctx context.Context, wallet *Wallet) (string, error) {
	// Step 1: Request challenge
	challenge, err := c.requestChallenge(ctx, wallet.Address)
	if err != nil {
		return "", fmt.Errorf("failed to request challenge: %w", err)
	}

	// Step 2: Sign challenge with wallet
	signature, err := signChallenge(wallet, challenge)
	if err != nil {
		return "", fmt.Errorf("failed to sign challenge: %w", err)
	}

	// Step 3: Verify signature and get JWT
	token, err := c.verifySignature(ctx, wallet.Address, challenge, signature)
	if err != nil {
		return "", fmt.Errorf("failed to verify signature: %w", err)
	}

	return token, nil
}

type challengeRequest struct {
	Address string `json:"address"`
}

type challengeResponse struct {
	Challenge string `json:"challenge"`
	ExpiresAt int64  `json:"expires_at"`
}

func (c *HubAuthClient) requestChallenge(ctx context.Context, address string) (string, error) {
	reqBody := challengeRequest{Address: address}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.hubURL+"/v1/auth/challenge", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("challenge request failed: %d %s", resp.StatusCode, string(body))
	}

	var result challengeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Challenge, nil
}

type verifyRequest struct {
	Address   string `json:"address"`
	Challenge string `json:"challenge"`
	Signature string `json:"signature"`
}

type verifyResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

func (c *HubAuthClient) verifySignature(ctx context.Context, address, challenge, signature string) (string, error) {
	reqBody := verifyRequest{
		Address:   address,
		Challenge: challenge,
		Signature: signature,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.hubURL+"/v1/auth/verify", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("verify request failed: %d %s", resp.StatusCode, string(body))
	}

	var result verifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Token, nil
}

// signChallenge signs a challenge string using EIP-191 personal_sign
func signChallenge(wallet *Wallet, challenge string) (string, error) {
	// EIP-191 personal_sign format: "\x19Ethereum Signed Message:\n" + len(message) + message
	message := []byte(challenge)
	hash := crypto.Keccak256Hash(
		[]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)),
	)

	signature, err := crypto.Sign(hash.Bytes(), wallet.PrivateKey)
	if err != nil {
		return "", err
	}

	// Adjust V value for Ethereum (add 27)
	if signature[64] < 27 {
		signature[64] += 27
	}

	return "0x" + fmt.Sprintf("%x", signature), nil
}
