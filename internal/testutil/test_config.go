package testutil

import (
	"os"
	"path/filepath"
	"time"

	"github.com/zion-protocol/zion-node/internal/config"
)

// NewTestConfig creates a test configuration
func NewTestConfig(hubURL string) *config.Config {
	tmpDir, _ := os.MkdirTemp("", "zion-node-test-*")

	return &config.Config{
		NodeID:                 "test-node-01",
		OperatorAddress:        "0x1234567890abcdef",
		HubURL:                 hubURL,
		MaxAgents:              10,
		CPUPerAgent:            1,
		MemoryPerAgent:         1024,
		StoragePerAgent:        2048,
		DataDir:                filepath.Join(tmpDir, "data"),
		SnapshotCache:          filepath.Join(tmpDir, "cache"),
		ContainerEngine:        "docker",
		RuntimeImage:           "alpine/openclaw:main",
		HeartbeatInterval:      5,
		HeartbeatRetryMax:      3,
		HeartbeatRetryInterval: 1,
		SnapshotRetentionDays:  1,
		LogDir:                 filepath.Join(tmpDir, "logs"),
		LogLevel:               "debug",
	}
}

// CleanupTestConfig cleans up test directories including the parent temp dir.
// It retries briefly to handle race conditions where background goroutines
// (e.g., state savers) may recreate subdirectories during shutdown.
func CleanupTestConfig(cfg *config.Config) {
	// DataDir is typically tmpDir/data, so its parent is the tmpDir itself.
	if cfg.DataDir != "" {
		parentDir := filepath.Dir(cfg.DataDir)
		if parentDir != "" && parentDir != "." && parentDir != "/" {
			os.RemoveAll(parentDir)
			// Brief retry: background goroutines may recreate subdirs during shutdown
			time.Sleep(50 * time.Millisecond)
			os.RemoveAll(parentDir)
			return
		}
	}
	// Fallback: remove individual dirs
	os.RemoveAll(cfg.DataDir)
	os.RemoveAll(cfg.SnapshotCache)
	os.RemoveAll(cfg.LogDir)
}
