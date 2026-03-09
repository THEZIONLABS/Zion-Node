package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateWallet(t *testing.T) {
	wallet, err := GenerateWallet()
	if err != nil {
		t.Fatalf("Failed to generate wallet: %v", err)
	}

	if wallet.Address == "" {
		t.Error("Wallet address is empty")
	}
	if wallet.PrivateKey == nil {
		t.Error("Wallet private key is nil")
	}
	if len(wallet.Address) != 42 || wallet.Address[:2] != "0x" {
		t.Errorf("Invalid address format: %s", wallet.Address)
	}
}

func TestImportWalletFromPrivateKey(t *testing.T) {
	// Test valid private key
	privateKey := "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	wallet, err := ImportWalletFromPrivateKey(privateKey)
	if err != nil {
		t.Fatalf("Failed to import wallet: %v", err)
	}

	expectedAddress := "0x1Be31A94361a391bBaFB2a4CCd704F57dc04d4bb"
	if wallet.Address != expectedAddress {
		t.Errorf("Expected address %s, got %s", expectedAddress, wallet.Address)
	}

	// Test without 0x prefix
	wallet2, err := ImportWalletFromPrivateKey(privateKey[2:])
	if err != nil {
		t.Fatalf("Failed to import wallet without 0x: %v", err)
	}
	if wallet2.Address != expectedAddress {
		t.Errorf("Expected address %s, got %s", expectedAddress, wallet2.Address)
	}

	// Test invalid private key
	_, err = ImportWalletFromPrivateKey("invalid")
	if err == nil {
		t.Error("Expected error for invalid private key")
	}
}

func TestWalletSaveAndLoad(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "wallet-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	walletPath := filepath.Join(tmpDir, "wallet.json")

	// Generate and save wallet
	wallet1, err := GenerateWallet()
	if err != nil {
		t.Fatalf("Failed to generate wallet: %v", err)
	}

	if err := wallet1.SaveToFile(walletPath); err != nil {
		t.Fatalf("Failed to save wallet: %v", err)
	}

	// Load wallet
	wallet2, err := LoadFromFile(walletPath)
	if err != nil {
		t.Fatalf("Failed to load wallet: %v", err)
	}

	// Verify addresses match
	if wallet1.Address != wallet2.Address {
		t.Errorf("Address mismatch: %s != %s", wallet1.Address, wallet2.Address)
	}

	// Verify private keys match
	if wallet1.GetPrivateKeyHex() != wallet2.GetPrivateKeyHex() {
		t.Error("Private key mismatch")
	}
}

func TestWalletGetters(t *testing.T) {
	privateKey := "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	wallet, err := ImportWalletFromPrivateKey(privateKey)
	if err != nil {
		t.Fatalf("Failed to import wallet: %v", err)
	}

	// Test GetPrivateKeyHex
	privKeyHex := wallet.GetPrivateKeyHex()
	if privKeyHex != privateKey {
		t.Errorf("Expected private key %s, got %s", privateKey, privKeyHex)
	}

	// Test GetPublicKeyHex
	pubKeyHex := wallet.GetPublicKeyHex()
	if len(pubKeyHex) < 130 || pubKeyHex[:2] != "0x" {
		t.Errorf("Invalid public key format: %s", pubKeyHex)
	}
}

func TestLoadFromFileErrors(t *testing.T) {
	// Test non-existent file
	_, err := LoadFromFile("/nonexistent/wallet.json")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	// Test invalid JSON
	tmpDir, _ := os.MkdirTemp("", "wallet-test-*")
	defer os.RemoveAll(tmpDir)
	
	invalidPath := filepath.Join(tmpDir, "invalid.json")
	os.WriteFile(invalidPath, []byte("invalid json"), 0600)
	
	_, err = LoadFromFile(invalidPath)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}
