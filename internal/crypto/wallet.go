package crypto

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Wallet represents an EVM wallet
type Wallet struct {
	PrivateKey *ecdsa.PrivateKey
	Address    string
}

// KeystoreFile represents encrypted wallet file format
type KeystoreFile struct {
	Address    string `json:"address"`
	PrivateKey string `json:"private_key"` // Hex encoded, not encrypted for simplicity
}

// GenerateWallet creates a new random EVM wallet
func GenerateWallet() (*Wallet, error) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

	return &Wallet{
		PrivateKey: privateKey,
		Address:    address,
	}, nil
}

// ImportWalletFromPrivateKey imports wallet from hex private key
func ImportWalletFromPrivateKey(privateKeyHex string) (*Wallet, error) {
	// Remove 0x prefix if present
	if len(privateKeyHex) >= 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	privateKeyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key format: %w", err)
	}

	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

	return &Wallet{
		PrivateKey: privateKey,
		Address:    address,
	}, nil
}

// SaveToFile saves wallet to encrypted keystore file
func (w *Wallet) SaveToFile(filepath string) error {
	privateKeyBytes := crypto.FromECDSA(w.PrivateKey)
	privateKeyHex := hex.EncodeToString(privateKeyBytes)

	keystore := KeystoreFile{
		Address:    w.Address,
		PrivateKey: privateKeyHex,
	}

	data, err := json.MarshalIndent(keystore, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal keystore: %w", err)
	}

	// Write with 0600 permissions (owner read/write only)
	if err := os.WriteFile(filepath, data, 0600); err != nil {
		return fmt.Errorf("failed to write keystore: %w", err)
	}

	return nil
}

// LoadFromFile loads wallet from keystore file
func LoadFromFile(filepath string) (*Wallet, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read keystore: %w", err)
	}

	var keystore KeystoreFile
	if err := json.Unmarshal(data, &keystore); err != nil {
		return nil, fmt.Errorf("failed to parse keystore: %w", err)
	}

	if keystore.PrivateKey == "" {
		return nil, errors.New("keystore missing private key")
	}

	wallet, err := ImportWalletFromPrivateKey(keystore.PrivateKey)
	if err != nil {
		return nil, err
	}

	// Verify address matches
	if common.HexToAddress(wallet.Address) != common.HexToAddress(keystore.Address) {
		return nil, errors.New("address mismatch in keystore")
	}

	return wallet, nil
}

// GetPrivateKeyHex returns private key as hex string
func (w *Wallet) GetPrivateKeyHex() string {
	privateKeyBytes := crypto.FromECDSA(w.PrivateKey)
	return "0x" + hex.EncodeToString(privateKeyBytes)
}

// GetPublicKeyHex returns public key as hex string
func (w *Wallet) GetPublicKeyHex() string {
	publicKeyBytes := crypto.FromECDSAPub(&w.PrivateKey.PublicKey)
	return "0x" + hex.EncodeToString(publicKeyBytes)
}

// LoadWallet loads wallet from default location ($HOME/.zion-node/wallet.json)
func LoadWallet() (*Wallet, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home dir: %w", err)
	}

	walletPath := filepath.Join(home, ".zion-node", "wallet.json")
	return LoadFromFile(walletPath)
}

// LoadWalletFrom loads wallet from the specified directory.
// The directory should contain a wallet.json file.
func LoadWalletFrom(dir string) (*Wallet, error) {
	if dir == "" {
		return LoadWallet()
	}
	walletPath := filepath.Join(dir, "wallet.json")
	return LoadFromFile(walletPath)
}

// WalletPath returns the wallet.json path for the given directory.
// If dir is empty, returns the default path.
func WalletPath(dir string) string {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".zion-node")
	}
	return filepath.Join(dir, "wallet.json")
}

// EnsureWalletDir creates the wallet directory if it does not exist.
func EnsureWalletDir(dir string) (string, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home dir: %w", err)
		}
		dir = filepath.Join(home, ".zion-node")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create wallet dir: %w", err)
	}
	return filepath.Join(dir, "wallet.json"), nil
}
