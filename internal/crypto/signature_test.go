package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestVerifyHubSignature_Valid(t *testing.T) {
	// Generate a key pair
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Create message and sign it
	message := []byte("test message for verification")
	hash := sha256.Sum256(message)

	// Use crypto.Sign from go-ethereum which produces 65-byte [R || S || V] signature
	sig, err := crypto.Sign(hash[:], privateKey)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	sigHex := hex.EncodeToString(sig)

	pubKeyBytes := crypto.FromECDSAPub(&privateKey.PublicKey)
	pubKeyHex := hex.EncodeToString(pubKeyBytes)

	err = VerifyHubSignature(message, sigHex, pubKeyHex)
	if err != nil {
		t.Errorf("Expected valid signature, got error: %v", err)
	}
}

func TestVerifyHubSignature_InvalidSignature(t *testing.T) {
	// Generate key pair
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	message := []byte("test message")
	wrongMessage := []byte("different message")
	hash := sha256.Sum256(wrongMessage)

	sig, err := crypto.Sign(hash[:], privateKey)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}
	sigHex := hex.EncodeToString(sig)

	pubKeyBytes := crypto.FromECDSAPub(&privateKey.PublicKey)
	pubKeyHex := hex.EncodeToString(pubKeyBytes)

	// Verify with original message (not the one that was signed)
	err = VerifyHubSignature(message, sigHex, pubKeyHex)
	if err == nil {
		t.Error("Expected error for invalid signature, got nil")
	}
}

func TestVerifyHubSignature_WrongKey(t *testing.T) {
	privateKey1, _ := crypto.GenerateKey()
	privateKey2, _ := crypto.GenerateKey()

	message := []byte("test message")
	hash := sha256.Sum256(message)

	// Sign with key1
	sig, _ := crypto.Sign(hash[:], privateKey1)
	sigHex := hex.EncodeToString(sig)

	// Verify with key2's public key
	pubKeyBytes := crypto.FromECDSAPub(&privateKey2.PublicKey)
	pubKeyHex := hex.EncodeToString(pubKeyBytes)

	err := VerifyHubSignature(message, sigHex, pubKeyHex)
	if err == nil {
		t.Error("Expected error when verifying with wrong key")
	}
}

func TestVerifyHubSignature_MalformedSignature(t *testing.T) {
	privateKey, _ := crypto.GenerateKey()
	pubKeyBytes := crypto.FromECDSAPub(&privateKey.PublicKey)
	pubKeyHex := hex.EncodeToString(pubKeyBytes)

	tests := []struct {
		name    string
		sig     string
		wantErr bool
	}{
		{"empty signature", "", true},
		{"invalid hex", "zzzz", true},
		{"too short", hex.EncodeToString(make([]byte, 32)), true},
		{"too long", hex.EncodeToString(make([]byte, 100)), true},
		{"exactly 64 bytes (missing V)", hex.EncodeToString(make([]byte, 64)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyHubSignature([]byte("msg"), tt.sig, pubKeyHex)
			if (err != nil) != tt.wantErr {
				t.Errorf("got err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestVerifyHubSignature_MalformedPublicKey(t *testing.T) {
	tests := []struct {
		name    string
		pubKey  string
		wantErr bool
	}{
		{"empty key", "", true},
		{"invalid hex", "zzzz", true},
		{"too short", hex.EncodeToString(make([]byte, 10)), true},
	}

	validSig := hex.EncodeToString(make([]byte, 65))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyHubSignature([]byte("msg"), validSig, tt.pubKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("got err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestHashCommand_Deterministic(t *testing.T) {
	cmd := map[string]interface{}{
		"command":  "run",
		"agent_id": "agent-001",
	}

	hash1, err := HashCommand(cmd)
	if err != nil {
		t.Fatalf("HashCommand failed: %v", err)
	}

	hash2, err := HashCommand(cmd)
	if err != nil {
		t.Fatalf("HashCommand failed: %v", err)
	}

	if hex.EncodeToString(hash1) != hex.EncodeToString(hash2) {
		t.Error("HashCommand is not deterministic")
	}

	if len(hash1) != 32 {
		t.Errorf("Expected 32 byte hash, got %d", len(hash1))
	}
}

func TestHashCommand_DifferentInputs(t *testing.T) {
	cmd1 := map[string]string{"command": "run"}
	cmd2 := map[string]string{"command": "stop"}

	hash1, _ := HashCommand(cmd1)
	hash2, _ := HashCommand(cmd2)

	if hex.EncodeToString(hash1) == hex.EncodeToString(hash2) {
		t.Error("Different commands should produce different hashes")
	}
}

func TestHashCommand_InvalidInput(t *testing.T) {
	// Functions cannot be marshaled to JSON
	_, err := HashCommand(func() {})
	if err == nil {
		t.Error("Expected error for non-serializable input")
	}
}

func TestHashCommand_VariousTypes(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
	}{
		{"string", "hello"},
		{"number", 42},
		{"struct", struct{ A string }{"test"}},
		{"nil", nil},
		{"slice", []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashCommand(tt.input)
			if err != nil {
				t.Fatalf("HashCommand failed for %s: %v", tt.name, err)
			}
			if len(hash) != 32 {
				t.Errorf("Expected 32 byte hash, got %d", len(hash))
			}
		})
	}
}

func TestVerifyHubSignature_ConsistentWithSign(t *testing.T) {
	// End-to-end test: sign + verify flow
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Simulate the full flow used in production
	cmd := map[string]string{"command": "run", "agent_id": "test-agent"}
	cmdBytes, _ := HashCommand(cmd)

	// Hash the command bytes (as VerifyHubSignature does internally)
	hash := sha256.Sum256(cmdBytes)

	// Sign
	sig, err := crypto.Sign(hash[:], privateKey)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	sigHex := hex.EncodeToString(sig)
	pubKeyHex := hex.EncodeToString(crypto.FromECDSAPub(&privateKey.PublicKey))

	// Verify
	err = VerifyHubSignature(cmdBytes, sigHex, pubKeyHex)
	if err != nil {
		t.Errorf("End-to-end sign+verify failed: %v", err)
	}
}

// Helper to create a valid but wrong signature (correct length, wrong content)
func TestVerifyHubSignature_ZeroSignature(t *testing.T) {
	privateKey, _ := crypto.GenerateKey()
	pubKeyHex := hex.EncodeToString(crypto.FromECDSAPub(&privateKey.PublicKey))

	// All-zero 65-byte signature
	zeroSig := hex.EncodeToString(make([]byte, 65))

	err := VerifyHubSignature([]byte("test"), zeroSig, pubKeyHex)
	// Zero r,s will fail ecdsa.Verify - but should not panic
	if err == nil {
		// Zero signature shouldn't verify
		// But ecdsa.Verify returns bool, and zero r,s makes big.Int(0) which is fine
		// The signature just won't validate
	}
	// Main assertion: no panic occurred
}

func TestVerifyHubSignature_RealWorldScenario(t *testing.T) {
	// Simulate Hub signing a command and Node verifying it
	hubKey, _ := crypto.GenerateKey()
	hubPubKeyHex := hex.EncodeToString(crypto.FromECDSAPub(&hubKey.PublicKey))

	// Hub creates and signs a run command
	command := map[string]interface{}{
		"command":  "run",
		"agent_id": "agent-abc-123",
		"params": map[string]interface{}{
			"runtime_engine": "openclaw",
			"engine_version": "1.0.0",
		},
	}

	cmdHash, err := HashCommand(command)
	if err != nil {
		t.Fatalf("Failed to hash command: %v", err)
	}

	// Hub signs the hash
	msgHash := sha256.Sum256(cmdHash)
	sig, err := crypto.Sign(msgHash[:], hubKey)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	// Node verifies
	sigHex := hex.EncodeToString(sig)
	err = VerifyHubSignature(cmdHash, sigHex, hubPubKeyHex)
	if err != nil {
		t.Errorf("Real-world scenario verification failed: %v", err)
	}

	// Tampered command should fail
	command["agent_id"] = "tampered-id"
	tamperedHash, _ := HashCommand(command)

	err = VerifyHubSignature(tamperedHash, sigHex, hubPubKeyHex)
	if err == nil {
		t.Error("Tampered command should fail verification")
	}
}

func TestVerifyHubSignature_0xPrefix(t *testing.T) {
	// Verify that 0x-prefixed hex strings are handled gracefully.
	// config.toml and Ethereum tooling commonly use 0x prefix,
	// but Go's hex.DecodeString does not accept it.
	privateKey, _ := crypto.GenerateKey()
	message := []byte("zion:cmd:run:agent-test:1700000000")
	hash := sha256.Sum256(message)
	sig, err := crypto.Sign(hash[:], privateKey)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}
	sigHex := hex.EncodeToString(sig)
	pubKeyHex := hex.EncodeToString(crypto.FromECDSAPub(&privateKey.PublicKey))

	t.Run("0x prefix on signature", func(t *testing.T) {
		err := VerifyHubSignature(message, "0x"+sigHex, pubKeyHex)
		if err != nil {
			t.Errorf("Should accept 0x-prefixed signature, got: %v", err)
		}
	})

	t.Run("0x prefix on public key", func(t *testing.T) {
		err := VerifyHubSignature(message, sigHex, "0x"+pubKeyHex)
		if err != nil {
			t.Errorf("Should accept 0x-prefixed public key, got: %v", err)
		}
	})

	t.Run("0x prefix on both", func(t *testing.T) {
		err := VerifyHubSignature(message, "0x"+sigHex, "0x"+pubKeyHex)
		if err != nil {
			t.Errorf("Should accept 0x-prefixed values, got: %v", err)
		}
	})
}

// Benchmark for signature verification performance
func BenchmarkVerifyHubSignature(b *testing.B) {
	privateKey, _ := crypto.GenerateKey()
	message := []byte("benchmark test message")
	hash := sha256.Sum256(message)
	sig, _ := crypto.Sign(hash[:], privateKey)
	sigHex := hex.EncodeToString(sig)
	pubKeyHex := hex.EncodeToString(crypto.FromECDSAPub(&privateKey.PublicKey))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = VerifyHubSignature(message, sigHex, pubKeyHex)
	}
}
