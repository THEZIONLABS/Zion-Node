package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateRequiredFields tests required fields validation
func TestValidateRequiredFields(t *testing.T) {
	cfg := &Config{}

	if err := validateRequiredFields(cfg); err == nil {
		t.Error("Expected error for missing node_id")
	}

	cfg.NodeID = "test-node"
	if err := validateRequiredFields(cfg); err == nil {
		t.Error("Expected error for missing hub_url")
	}

	cfg.HubURL = "http://test.com"
	if err := validateRequiredFields(cfg); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestValidateResourceLimits tests resource limits validation
func TestValidateResourceLimits(t *testing.T) {
	cfg := &Config{
		MemoryPerAgent: 1023,
	}
	if err := validateResourceLimits(cfg); err == nil {
		t.Error("Expected error for memory < 1024")
	}

	cfg.MemoryPerAgent = 1024
	cfg.CPUPerAgent = 0
	if err := validateResourceLimits(cfg); err == nil {
		t.Error("Expected error for cpu < 1")
	}

	cfg.CPUPerAgent = 1
	cfg.MaxAgents = 0
	if err := validateResourceLimits(cfg); err == nil {
		t.Error("Expected error for max_agents < 1")
	}

	cfg.MaxAgents = 10
	cfg.StoragePerAgent = 99
	if err := validateResourceLimits(cfg); err == nil {
		t.Error("Expected error for storage < 100")
	}

	cfg.StoragePerAgent = 1024
	if err := validateResourceLimits(cfg); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestValidateDataDirectory tests data directory validation
func TestValidateDataDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		DataDir: filepath.Join(tmpDir, "data"),
	}

	if err := validateDataDirectory(cfg); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Test with non-writable directory (if possible)
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(readOnlyDir, 0400); err == nil {
		cfg.DataDir = readOnlyDir
		if err := validateDataDirectory(cfg); err == nil {
			t.Error("Expected error for non-writable directory")
		}
	}
}

// TestValidateResourceLimits_ExtremeValues tests edge cases for resource limits
func TestValidateResourceLimits_ExtremeValues(t *testing.T) {
	tests := []struct {
		name      string
		cfg       Config
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "zero memory",
			cfg:       Config{MemoryPerAgent: 0, CPUPerAgent: 1, MaxAgents: 1, StoragePerAgent: 100},
			wantErr:   true,
			errSubstr: "memory_per_agent",
		},
		{
			name:      "negative-like memory (underflow)",
			cfg:       Config{MemoryPerAgent: -1, CPUPerAgent: 1, MaxAgents: 1, StoragePerAgent: 100},
			wantErr:   true,
			errSubstr: "memory_per_agent",
		},
		{
			name:      "zero cpu",
			cfg:       Config{MemoryPerAgent: 1024, CPUPerAgent: 0, MaxAgents: 1, StoragePerAgent: 100},
			wantErr:   true,
			errSubstr: "cpu_per_agent",
		},
		{
			name:      "negative cpu",
			cfg:       Config{MemoryPerAgent: 1024, CPUPerAgent: -1, MaxAgents: 1, StoragePerAgent: 100},
			wantErr:   true,
			errSubstr: "cpu_per_agent",
		},
		{
			name:      "zero max_agents",
			cfg:       Config{MemoryPerAgent: 1024, CPUPerAgent: 1, MaxAgents: 0, StoragePerAgent: 100},
			wantErr:   true,
			errSubstr: "max_agents",
		},
		{
			name:      "zero storage",
			cfg:       Config{MemoryPerAgent: 1024, CPUPerAgent: 1, MaxAgents: 1, StoragePerAgent: 0},
			wantErr:   true,
			errSubstr: "storage_per_agent",
		},
		{
			name:    "very large max_agents",
			cfg:     Config{MemoryPerAgent: 1024, CPUPerAgent: 1, MaxAgents: 100000, StoragePerAgent: 1024},
			wantErr: false, // Validation passes, actual capping happens in CapMaxAgentsBySystemResources
		},
		{
			name:    "boundary - exactly minimum memory",
			cfg:     Config{MemoryPerAgent: 1024, CPUPerAgent: 1, MaxAgents: 1, StoragePerAgent: 100},
			wantErr: false,
		},
		{
			name:      "boundary - one below minimum memory",
			cfg:       Config{MemoryPerAgent: 1023, CPUPerAgent: 1, MaxAgents: 1, StoragePerAgent: 100},
			wantErr:   true,
			errSubstr: "1024",
		},
		{
			name:      "boundary - one below minimum storage",
			cfg:       Config{MemoryPerAgent: 1024, CPUPerAgent: 1, MaxAgents: 1, StoragePerAgent: 99},
			wantErr:   true,
			errSubstr: "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResourceLimits(&tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("Expected error containing %q, got: %v", tt.errSubstr, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// TestValidateDiskSpace tests disk space validation
func TestValidateDiskSpace(t *testing.T) {
	// This test runs on the actual filesystem, so we just verify it doesn't panic
	// and returns a result (pass or fail depending on actual disk space)
	err := validateDiskSpace()
	// We don't assert pass/fail since it depends on environment,
	// but we verify it runs without panicking and returns a sensible error message
	if err != nil {
		if !strings.Contains(err.Error(), "disk space") && !strings.Contains(err.Error(), "not accessible") {
			t.Errorf("Unexpected error format: %v", err)
		}
		t.Logf("validateDiskSpace returned (expected in low-disk environments): %v", err)
	} else {
		t.Log("validateDiskSpace passed (sufficient disk space)")
	}
}

// TestValidateRequiredFields_Empty tests empty string edge cases
func TestValidateRequiredFields_Empty(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"both empty", Config{}, true},
		{"node_id only", Config{NodeID: "n1"}, true},
		{"hub_url only", Config{HubURL: "http://x"}, true},
		{"whitespace node_id", Config{NodeID: "  ", HubURL: "http://x"}, false}, // whitespace is technically non-empty
		{"both set", Config{NodeID: "n1", HubURL: "http://x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRequiredFields(&tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got error: %v", tt.wantErr, err)
			}
		})
	}
}
