package utils

import (
	"path/filepath"
)

// AgentDataDir returns the absolute data directory path for an agent
func AgentDataDir(baseDir, agentID string) string {
	path := filepath.Join(baseDir, "agents", agentID)
	// Convert to absolute path for Docker mount compatibility
	if absPath, err := filepath.Abs(path); err == nil {
		return absPath
	}
	return path
}

// SnapshotPath returns the snapshot file path
func SnapshotPath(cacheDir, agentID string) string {
	return filepath.Join(cacheDir, agentID+".tar.zst")
}

// StateFilePath returns the state file path
func StateFilePath(dataDir string) string {
	return filepath.Join(dataDir, "agents.json")
}
