package crypto

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// VerifyHubSignature verifies Hub command signature
func VerifyHubSignature(message []byte, signature string, publicKeyHex string) error {
	// Hash message
	hash := sha256.Sum256(message)

	// Defensively strip 0x prefix — Go's hex.DecodeString does not accept it,
	// but config files and Ethereum tooling commonly include it.
	signature = strings.TrimPrefix(signature, "0x")
	publicKeyHex = strings.TrimPrefix(publicKeyHex, "0x")

	// Decode signature
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return err
	}

	if len(sigBytes) != 65 {
		return errors.New("invalid signature length")
	}

	// Decode public key
	pubKeyBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return err
	}

	pubKey, err := crypto.UnmarshalPubkey(pubKeyBytes)
	if err != nil {
		return err
	}

	// Verify signature
	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:64])

	if !ecdsa.Verify(pubKey, hash[:], r, s) {
		return errors.New("invalid signature")
	}

	return nil
}

// HashCommand creates hash of command for verification
func HashCommand(cmd interface{}) ([]byte, error) {
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	return hash[:], nil
}
