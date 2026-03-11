package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/zion-protocol/zion-node/internal/crypto"
)

// SetDefaults sets default values for configuration
func (c *Config) SetDefaults() {
	if c.NodeID == "" {
		c.NodeID = generateNodeID()
	}
	if c.MaxAgents == 0 {
		c.MaxAgents = 50
	}
	if c.CPUPerAgent == 0 {
		c.CPUPerAgent = 1
	}
	if c.MemoryPerAgent == 0 {
		c.MemoryPerAgent = 1024
	}
	if c.StoragePerAgent == 0 {
		c.StoragePerAgent = 10240
	}
	if c.DataDir == "" {
		c.DataDir = "/var/lib/zion-node"
	}
	if c.SnapshotCache == "" {
		c.SnapshotCache = "/var/lib/zion-node/cache"
	}
	if c.WalletDir == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			c.WalletDir = filepath.Join(home, ".zion-node")
		}
	}
	if c.ContainerEngine == "" {
		c.ContainerEngine = "docker"
	}
	if c.RuntimeImage == "" {
		c.RuntimeImage = "openclaw/runtime:v1"
	}
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 10
	}
	if c.HeartbeatRetryMax == 0 {
		c.HeartbeatRetryMax = 3
	}
	if c.HeartbeatRetryInterval == 0 {
		c.HeartbeatRetryInterval = 5
	}
	if c.SnapshotRetentionDays == 0 {
		c.SnapshotRetentionDays = 3
	}
	if c.LogDir == "" {
		c.LogDir = filepath.Join(c.WalletDir, "logs")
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.HTTPTimeout == 0 {
		c.HTTPTimeout = 10
	}
	// Load operator address from wallet if not set
	if c.OperatorAddress == "" {
		c.OperatorAddress = c.loadWalletAddress()
	}
}

// ReloadWallet reloads the operator address from the wallet file.
// Called after wallet creation in the TUI setup wizard.
func (c *Config) ReloadWallet() {
	c.OperatorAddress = c.loadWalletAddress()
}

// loadWalletAddress loads operator address from wallet file.
// It uses WalletDir if already set on the config, otherwise falls back to $HOME/.zion-node.
func (c *Config) loadWalletAddress() string {
	dir := c.WalletDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".zion-node")
	}

	walletPath := filepath.Join(dir, "wallet.json")
	wallet, err := crypto.LoadFromFile(walletPath)
	if err != nil {
		return ""
	}

	return wallet.Address
}

// generateNodeID creates a random node ID in the format "node-xxxxxxxxxxxx" (12 hex chars).
func generateNodeID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// Fallback: should never happen with crypto/rand
		return "node-000000000000"
	}
	return "node-" + hex.EncodeToString(b)
}
