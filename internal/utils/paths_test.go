package utils

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentDataDir(t *testing.T) {
	tests := []struct {
		name    string
		baseDir string
		agentID string
		wantEnd string // Expected suffix of the path
	}{
		{"normal", "/var/lib/zion-node", "agent-001", filepath.Join("var", "lib", "zion-node", "agents", "agent-001")},
		{"uuid agent", "/data", "550e8400-e29b-41d4-a716-446655440000", filepath.Join("data", "agents", "550e8400-e29b-41d4-a716-446655440000")},
		{"relative base", "data", "agent-001", filepath.Join("data", "agents", "agent-001")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AgentDataDir(tt.baseDir, tt.agentID)
			if !strings.HasSuffix(result, tt.wantEnd) {
				t.Errorf("AgentDataDir(%s, %s) = %s, want suffix %s", tt.baseDir, tt.agentID, result, tt.wantEnd)
			}
			// Should return absolute path
			if !filepath.IsAbs(result) {
				t.Errorf("Expected absolute path, got %s", result)
			}
		})
	}
}

func TestSnapshotPath(t *testing.T) {
	tests := []struct {
		cacheDir string
		agentID  string
		expected string
	}{
		{"/cache", "agent-001", "/cache/agent-001.tar.zst"},
		{"/var/cache", "test", "/var/cache/test.tar.zst"},
	}

	for _, tt := range tests {
		result := SnapshotPath(tt.cacheDir, tt.agentID)
		if result != tt.expected {
			t.Errorf("SnapshotPath(%s, %s) = %s, want %s", tt.cacheDir, tt.agentID, result, tt.expected)
		}
	}
}

func TestStateFilePath(t *testing.T) {
	tests := []struct {
		dataDir  string
		expected string
	}{
		{"/var/lib/zion-node", "/var/lib/zion-node/agents.json"},
		{"/data", "/data/agents.json"},
	}

	for _, tt := range tests {
		result := StateFilePath(tt.dataDir)
		if result != tt.expected {
			t.Errorf("StateFilePath(%s) = %s, want %s", tt.dataDir, result, tt.expected)
		}
	}
}

func TestAgentDataDir_EmptyInputs(t *testing.T) {
	// Empty agent ID
	result := AgentDataDir("/data", "")
	if !strings.Contains(result, "agents") {
		t.Errorf("Should still contain 'agents' dir, got: %s", result)
	}
}

func TestSnapshotPath_EmptyAgentID(t *testing.T) {
	result := SnapshotPath("/cache", "")
	expected := "/cache/.tar.zst"
	if result != expected {
		t.Errorf("SnapshotPath with empty agentID = %s, want %s", result, expected)
	}
}
