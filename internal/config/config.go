package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/pelletier/go-toml/v2"
	"github.com/shirou/gopsutil/v3/mem"
)

// Config represents the node configuration
type Config struct {
	// Node identity
	NodeID          string `toml:"node_id"`
	OperatorAddress string `toml:"operator_address"`

	// Hub connection
	HubURL       string `toml:"hub_url"`
	HubPublicKey string `toml:"-"` // Fetched from hub at startup, not stored in config file
	HubAuthToken string `toml:"hub_auth_token"` // JWT or API key for Hub authentication

	// Capacity
	MaxAgents       int `toml:"max_agents"`
	CPUPerAgent     int `toml:"cpu_per_agent"`
	MemoryPerAgent  int `toml:"memory_per_agent"`  // MB
	StoragePerAgent int `toml:"storage_per_agent"` // MB

	// Wallet
	WalletDir string `toml:"wallet_dir"` // directory containing wallet.json (default: $HOME/.zion-node)

	// Storage
	DataDir       string `toml:"data_dir"`
	SnapshotCache string `toml:"snapshot_cache"`

	// Container
	ContainerEngine string `toml:"container_engine"` // docker
	RuntimeImage    string `toml:"runtime_image"`

	// Heartbeat
	HeartbeatInterval      int `toml:"heartbeat_interval"` // seconds
	HeartbeatRetryMax      int `toml:"heartbeat_retry_max"`
	HeartbeatRetryInterval int `toml:"heartbeat_retry_interval"` // seconds

	// Snapshot
	SnapshotRetentionDays int `toml:"snapshot_retention_days"`

	// Logging
	LogDir   string `toml:"log_dir"`
	LogLevel string `toml:"log_level"`

	// HTTP
	HTTPTimeout int `toml:"http_timeout"` // seconds

	// System resources (auto-detected, not configurable)
	SystemCPU      int `toml:"-" json:"-"`
	SystemMemoryMB int `toml:"-" json:"-"`
}

// Load loads configuration from TOML file.
// If configFile is provided (non-empty), it is used directly.
// Otherwise, the following paths are searched in order:
//   - ./config.toml
//   - /etc/zion-node/config.toml
//   - $HOME/.zion-node/config.toml
func Load(configFile ...string) (*Config, error) {
	var configPath string

	// If an explicit config file was provided, use it directly
	if len(configFile) > 0 && configFile[0] != "" {
		configPath = configFile[0]
		if _, err := os.Stat(configPath); err != nil {
			return nil, fmt.Errorf("config file not found: %s", configPath)
		}
	} else {
		// Find config file (by priority)
		configPaths := []string{
			"./config.toml",
			"/etc/zion-node/config.toml",
			filepath.Join(os.Getenv("HOME"), ".zion-node", "config.toml"),
		}

		for _, path := range configPaths {
			if _, err := os.Stat(path); err == nil {
				configPath = path
				break
			}
		}

		if configPath == "" {
			return nil, fmt.Errorf("config file not found in: %v", configPaths)
		}
	}

	// Read TOML file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Set defaults
	cfg.SetDefaults()

	// Apply environment variable overrides
	// Priority: ENV > .env > config.toml > defaults
	cfg.ApplyEnvOverrides()

	// Detect and store actual system resources
	cfg.detectSystemResources()

	// Auto-cap max_agents based on actual system resources (use 80% of system)
	cfg.capMaxAgentsBySystemResources()

	// Ensure data directories exist
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.SnapshotCache, 0755); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// detectSystemResources populates SystemCPU and SystemMemoryMB from actual hardware.
func (c *Config) detectSystemResources() {
	c.SystemCPU = runtime.NumCPU()
	if v, err := mem.VirtualMemory(); err == nil {
		c.SystemMemoryMB = int(v.Total / 1024 / 1024)
	}
}

// Reserved memory for the OS and zion-node process itself (in MB).
const systemReservedMB = 256

// capMaxAgentsBySystemResources caps max_agents based on memory.
// CPU is NOT used as a hard cap because containers share CPU (overcommit is fine).
// Memory is the hard constraint because Docker kills OOM containers.
func (c *Config) capMaxAgentsBySystemResources() {
	// Memory check — always reserve systemReservedMB for the OS
	totalMB := c.SystemMemoryMB
	if totalMB == 0 {
		return // can't detect, trust configured max_agents
	}

	pctBased := int(float64(totalMB) * 0.8)
	reserveBased := totalMB - systemReservedMB
	usableMB := pctBased
	if reserveBased < usableMB {
		usableMB = reserveBased
	}
	if usableMB < 0 {
		usableMB = 0
	}
	maxByMem := usableMB / c.MemoryPerAgent
	if maxByMem < 1 {
		maxByMem = 1
	}

	if maxByMem < c.MaxAgents {
		numCPU := c.SystemCPU
		if numCPU == 0 {
			numCPU = runtime.NumCPU()
		}
		fmt.Printf("[config] WARNING: max_agents reduced from %d to %d (system: %d CPUs, %d MB RAM; reserved %d MB for OS; agents: %d MB each)\n",
			c.MaxAgents, maxByMem, numCPU, totalMB, systemReservedMB, c.MemoryPerAgent)
		c.MaxAgents = maxByMem
	}
}
