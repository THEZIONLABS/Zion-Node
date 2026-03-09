package config

import (
	"os"
	"strconv"
)

// ApplyEnvOverrides applies environment variable overrides to configuration.
// Priority: Environment Variables > config.toml > defaults
func (c *Config) ApplyEnvOverrides() {
	// Hub connection
	if v := os.Getenv("HUB_ENDPOINT"); v != "" {
		c.HubURL = v
	}
	// Also support ZION_HUB_URL for backward compatibility
	if v := os.Getenv("ZION_HUB_URL"); v != "" {
		c.HubURL = v
	}
	if v := os.Getenv("HUB_AUTH_TOKEN"); v != "" {
		c.HubAuthToken = v
	}

	// Node identity
	if v := os.Getenv("NODE_ID"); v != "" {
		c.NodeID = v
	}
	if v := os.Getenv("OPERATOR_ADDRESS"); v != "" {
		c.OperatorAddress = v
	}

	// Logging
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := os.Getenv("LOG_DIR"); v != "" {
		c.LogDir = v
	}

	// Wallet
	if v := os.Getenv("WALLET_DIR"); v != "" {
		c.WalletDir = v
	}

	// Storage
	if v := os.Getenv("DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("SNAPSHOT_CACHE"); v != "" {
		c.SnapshotCache = v
	}

	// Capacity
	if v := os.Getenv("MAX_AGENTS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			c.MaxAgents = i
		}
	}
	if v := os.Getenv("CPU_PER_AGENT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			c.CPUPerAgent = i
		}
	}
	if v := os.Getenv("MEMORY_PER_AGENT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			c.MemoryPerAgent = i
		}
	}
	if v := os.Getenv("STORAGE_PER_AGENT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			c.StoragePerAgent = i
		}
	}

	// Container
	if v := os.Getenv("CONTAINER_ENGINE"); v != "" {
		c.ContainerEngine = v
	}
	if v := os.Getenv("RUNTIME_IMAGE"); v != "" {
		c.RuntimeImage = v
	}

	// Heartbeat
	if v := os.Getenv("HEARTBEAT_INTERVAL"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			c.HeartbeatInterval = i
		}
	}
	if v := os.Getenv("HEARTBEAT_RETRY_MAX"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			c.HeartbeatRetryMax = i
		}
	}
	if v := os.Getenv("HEARTBEAT_RETRY_INTERVAL"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			c.HeartbeatRetryInterval = i
		}
	}

	// HTTP
	if v := os.Getenv("HTTP_TIMEOUT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			c.HTTPTimeout = i
		}
	}
}
