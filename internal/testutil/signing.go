package testutil

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// TestSigningKeyPair holds an ECDSA secp256k1 key pair for testing.
type TestSigningKeyPair struct {
	PrivateKey   *ecdsa.PrivateKey
	PublicKeyHex string // 65-byte uncompressed, hex-encoded (no 0x prefix)
}

// NewTestSigningKeyPair generates a random secp256k1 key pair suitable for
// signing hub commands in tests. The returned PublicKeyHex should be set as
// cfg.HubPublicKey so the daemon will verify command signatures.
func NewTestSigningKeyPair() *TestSigningKeyPair {
	key, err := ethcrypto.GenerateKey()
	if err != nil {
		panic(fmt.Sprintf("testutil: failed to generate signing key: %v", err))
	}
	pubBytes := ethcrypto.FromECDSAPub(&key.PublicKey)
	return &TestSigningKeyPair{
		PrivateKey:   key,
		PublicKeyHex: hex.EncodeToString(pubBytes),
	}
}

// SignCommand signs a HubCommand using the same format the real hub uses:
//
//	message = "zion:cmd:<command>:<agent_id>:<unix_timestamp>"
//	hash    = SHA-256(message)
//	sig     = secp256k1.Sign(hash, privateKey) → 65 bytes (r||s||v)
//
// It mutates cmd in-place, setting Signature and SignedAt fields.
func (kp *TestSigningKeyPair) SignCommand(cmd *types.HubCommand) {
	now := time.Now().Unix()
	cmd.SignedAt = now

	message := fmt.Sprintf("zion:cmd:%s:%s:%d", cmd.Command, cmd.AgentID, cmd.SignedAt)
	hash := sha256.Sum256([]byte(message))

	sig, err := ethcrypto.Sign(hash[:], kp.PrivateKey)
	if err != nil {
		panic(fmt.Sprintf("testutil: failed to sign command: %v", err))
	}
	cmd.Signature = hex.EncodeToString(sig)
}
