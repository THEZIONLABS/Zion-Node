package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	if cfg.MaxAgents != 50 {
		t.Errorf("Expected default MaxAgents 50, got %d", cfg.MaxAgents)
	}
	if cfg.CPUPerAgent != 1 {
		t.Errorf("Expected default CPUPerAgent 1, got %d", cfg.CPUPerAgent)
	}
	if cfg.MemoryPerAgent != 1024 {
		t.Errorf("Expected default MemoryPerAgent 1024, got %d", cfg.MemoryPerAgent)
	}
	if cfg.StoragePerAgent != 10240 {
		t.Errorf("Expected default StoragePerAgent 10240, got %d", cfg.StoragePerAgent)
	}
	if cfg.DataDir != "/var/lib/zion-node" {
		t.Errorf("Expected default DataDir /var/lib/zion-node, got %s", cfg.DataDir)
	}
	if cfg.ContainerEngine != "docker" {
		t.Errorf("Expected default ContainerEngine docker, got %s", cfg.ContainerEngine)
	}
	if cfg.RuntimeImage != "openclaw/runtime:v1" {
		t.Errorf("Expected default RuntimeImage openclaw/runtime:v1, got %s", cfg.RuntimeImage)
	}
	if cfg.HeartbeatInterval != 10 {
		t.Errorf("Expected default HeartbeatInterval 10, got %d", cfg.HeartbeatInterval)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("Expected default LogLevel info, got %s", cfg.LogLevel)
	}
	if cfg.HTTPTimeout != 10 {
		t.Errorf("Expected default HTTPTimeout 10, got %d", cfg.HTTPTimeout)
	}
}

func TestSetDefaults_NoOverride(t *testing.T) {
	cfg := &Config{
		MaxAgents: 5,
		LogLevel:  "debug",
		DataDir:   "/custom/dir",
	}
	cfg.SetDefaults()

	if cfg.MaxAgents != 5 {
		t.Errorf("SetDefaults should not override MaxAgents, got %d", cfg.MaxAgents)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("SetDefaults should not override LogLevel, got %s", cfg.LogLevel)
	}
	if cfg.DataDir != "/custom/dir" {
		t.Errorf("SetDefaults should not override DataDir, got %s", cfg.DataDir)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	cfg := &Config{
		HubURL: "http://original.com",
		NodeID: "original-node",
	}

	// Set env vars
	os.Setenv("HUB_ENDPOINT", "http://override.com")
	os.Setenv("NODE_ID", "override-node")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("MAX_AGENTS", "20")
	os.Setenv("HUB_AUTH_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("HUB_ENDPOINT")
		os.Unsetenv("NODE_ID")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("MAX_AGENTS")
		os.Unsetenv("HUB_AUTH_TOKEN")
	}()

	cfg.ApplyEnvOverrides()

	if cfg.HubURL != "http://override.com" {
		t.Errorf("Expected HubURL http://override.com, got %s", cfg.HubURL)
	}
	if cfg.NodeID != "override-node" {
		t.Errorf("Expected NodeID override-node, got %s", cfg.NodeID)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("Expected LogLevel debug, got %s", cfg.LogLevel)
	}
	if cfg.MaxAgents != 20 {
		t.Errorf("Expected MaxAgents 20, got %d", cfg.MaxAgents)
	}
	if cfg.HubAuthToken != "test-token" {
		t.Errorf("Expected HubAuthToken test-token, got %s", cfg.HubAuthToken)
	}
}

func TestApplyEnvOverrides_ZION_HUB_URL(t *testing.T) {
	cfg := &Config{HubURL: "http://original.com"}

	os.Setenv("ZION_HUB_URL", "http://zion-override.com")
	defer os.Unsetenv("ZION_HUB_URL")

	cfg.ApplyEnvOverrides()

	if cfg.HubURL != "http://zion-override.com" {
		t.Errorf("Expected ZION_HUB_URL override, got %s", cfg.HubURL)
	}
}

func TestApplyEnvOverrides_AllIntFields(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	os.Setenv("CPU_PER_AGENT", "4")
	os.Setenv("MEMORY_PER_AGENT", "2048")
	os.Setenv("STORAGE_PER_AGENT", "20480")
	os.Setenv("HEARTBEAT_INTERVAL", "30")
	os.Setenv("HEARTBEAT_RETRY_MAX", "5")
	defer func() {
		os.Unsetenv("CPU_PER_AGENT")
		os.Unsetenv("MEMORY_PER_AGENT")
		os.Unsetenv("STORAGE_PER_AGENT")
		os.Unsetenv("HEARTBEAT_INTERVAL")
		os.Unsetenv("HEARTBEAT_RETRY_MAX")
	}()

	cfg.ApplyEnvOverrides()

	if cfg.CPUPerAgent != 4 {
		t.Errorf("Expected CPUPerAgent 4, got %d", cfg.CPUPerAgent)
	}
	if cfg.MemoryPerAgent != 2048 {
		t.Errorf("Expected MemoryPerAgent 2048, got %d", cfg.MemoryPerAgent)
	}
	if cfg.StoragePerAgent != 20480 {
		t.Errorf("Expected StoragePerAgent 20480, got %d", cfg.StoragePerAgent)
	}
	if cfg.HeartbeatInterval != 30 {
		t.Errorf("Expected HeartbeatInterval 30, got %d", cfg.HeartbeatInterval)
	}
	if cfg.HeartbeatRetryMax != 5 {
		t.Errorf("Expected HeartbeatRetryMax 5, got %d", cfg.HeartbeatRetryMax)
	}
}

func TestApplyEnvOverrides_StringFields(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	os.Setenv("DATA_DIR", "/custom/data")
	os.Setenv("SNAPSHOT_CACHE", "/custom/cache")
	os.Setenv("CONTAINER_ENGINE", "podman")
	os.Setenv("RUNTIME_IMAGE", "custom/image:v2")
	os.Setenv("LOG_DIR", "/custom/logs")
	os.Setenv("OPERATOR_ADDRESS", "0xabcdef")
	defer func() {
		os.Unsetenv("DATA_DIR")
		os.Unsetenv("SNAPSHOT_CACHE")
		os.Unsetenv("CONTAINER_ENGINE")
		os.Unsetenv("RUNTIME_IMAGE")
		os.Unsetenv("LOG_DIR")
		os.Unsetenv("OPERATOR_ADDRESS")
	}()

	cfg.ApplyEnvOverrides()

	if cfg.DataDir != "/custom/data" {
		t.Errorf("Expected DataDir /custom/data, got %s", cfg.DataDir)
	}
	if cfg.SnapshotCache != "/custom/cache" {
		t.Errorf("Expected SnapshotCache /custom/cache, got %s", cfg.SnapshotCache)
	}
	if cfg.ContainerEngine != "podman" {
		t.Errorf("Expected ContainerEngine podman, got %s", cfg.ContainerEngine)
	}
	if cfg.RuntimeImage != "custom/image:v2" {
		t.Errorf("Expected RuntimeImage custom/image:v2, got %s", cfg.RuntimeImage)
	}
	if cfg.OperatorAddress != "0xabcdef" {
		t.Errorf("Expected OperatorAddress 0xabcdef, got %s", cfg.OperatorAddress)
	}
}

func TestLoad_FromFile(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "config-test-*")
	defer os.RemoveAll(tmpDir)

	configContent := `
node_id = "test-node"
hub_url = "http://localhost:8080"
max_agents = 5
cpu_per_agent = 2
memory_per_agent = 1024
storage_per_agent = 5120
log_level = "debug"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	// Create data dirs to avoid errors
	dataDir := filepath.Join(tmpDir, "data")
	cacheDir := filepath.Join(tmpDir, "cache")
	os.MkdirAll(dataDir, 0755)
	os.MkdirAll(cacheDir, 0755)

	// Set env vars to override data dir (since Load creates dirs)
	os.Setenv("DATA_DIR", dataDir)
	os.Setenv("SNAPSHOT_CACHE", cacheDir)
	defer func() {
		os.Unsetenv("DATA_DIR")
		os.Unsetenv("SNAPSHOT_CACHE")
	}()

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.NodeID != "test-node" {
		t.Errorf("Expected NodeID test-node, got %s", cfg.NodeID)
	}
	if cfg.HubURL != "http://localhost:8080" {
		t.Errorf("Expected HubURL http://localhost:8080, got %s", cfg.HubURL)
	}
	if cfg.MaxAgents < 1 {
		t.Error("MaxAgents should be at least 1")
	}
	if cfg.CPUPerAgent != 2 {
		t.Errorf("Expected CPUPerAgent 2, got %d", cfg.CPUPerAgent)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.toml")
	if err == nil {
		t.Error("Expected error for nonexistent config file")
	}
}

func TestLoad_InvalidTOML(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "config-invalid-*")
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte("invalid = [toml content"), 0644)

	_, err := Load(configPath)
	if err == nil {
		t.Error("Expected error for invalid TOML")
	}
}

func TestCapMaxAgentsBySystemResources(t *testing.T) {
	cfg := &Config{
		MaxAgents:      100,
		MemoryPerAgent: 512,
		SystemMemoryMB: 2048, // 2GB
	}

	cfg.capMaxAgentsBySystemResources()

	// With 2048 MB total, 80% = 1638 MB; 2048-256 = 1792 MB; min = 1638
	// 1638 / 512 = 3 agents max
	if cfg.MaxAgents > 100 {
		t.Error("MaxAgents should not increase")
	}
	if cfg.MaxAgents > 4 {
		t.Errorf("Expected MaxAgents capped to ~3, got %d", cfg.MaxAgents)
	}
}

func TestCapMaxAgentsBySystemResources_ZeroMemory(t *testing.T) {
	cfg := &Config{
		MaxAgents:      10,
		MemoryPerAgent: 512,
		SystemMemoryMB: 0, // Can't detect
	}

	cfg.capMaxAgentsBySystemResources()

	// Should not change if SystemMemoryMB is 0
	if cfg.MaxAgents != 10 {
		t.Errorf("MaxAgents should not change when SystemMemoryMB=0, got %d", cfg.MaxAgents)
	}
}

func TestCapMaxAgentsBySystemResources_LargeMemory(t *testing.T) {
	cfg := &Config{
		MaxAgents:      10,
		MemoryPerAgent: 512,
		SystemMemoryMB: 65536, // 64GB
	}

	cfg.capMaxAgentsBySystemResources()

	// Should not cap since there's plenty of memory
	if cfg.MaxAgents != 10 {
		t.Errorf("MaxAgents should not change with large memory, got %d", cfg.MaxAgents)
	}
}

func TestCapMaxAgentsBySystemResources_MinimumOne(t *testing.T) {
	cfg := &Config{
		MaxAgents:      10,
		MemoryPerAgent: 10240, // 10GB per agent
		SystemMemoryMB: 512,   // Only 512MB
	}

	cfg.capMaxAgentsBySystemResources()

	if cfg.MaxAgents < 1 {
		t.Error("MaxAgents should be at least 1")
	}
}

func TestDetectSystemResources(t *testing.T) {
	cfg := &Config{}
	cfg.detectSystemResources()

	if cfg.SystemCPU == 0 {
		t.Error("SystemCPU should be detected (> 0)")
	}
	// SystemMemoryMB might be 0 in some CI environments, but generally > 0
	if cfg.SystemMemoryMB <= 0 {
		t.Log("WARNING: SystemMemoryMB not detected, might be in limited environment")
	}
}
