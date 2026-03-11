package config

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
	httputil "github.com/zion-protocol/zion-node/internal/http"
	"golang.org/x/sys/unix"
)

// validateConfig validates the configuration with comprehensive checks
func validateConfig(c *Config, logger *logrus.Logger) error {
	// Required fields validation
	if err := validateRequiredFields(c); err != nil {
		return err
	}

	// Resource limits validation
	if err := validateResourceLimits(c); err != nil {
		return err
	}

	// Docker validation
	if err := validateDocker(c); err != nil {
		return err
	}

	// Disk space validation
	if err := validateDiskSpace(); err != nil {
		return err
	}

	// Data directory validation
	if err := validateDataDirectory(c); err != nil {
		return err
	}

	// Hub reachability check (optional, just warn)
	checkHubReachability(c, logger)

	return nil
}

// validateRequiredFields checks required configuration fields
func validateRequiredFields(c *Config) error {
	if c.NodeID == "" {
		return fmt.Errorf("node_id is required")
	}
	if c.HubURL == "" {
		return fmt.Errorf("hub_url is required")
	}
	return nil
}

// validateResourceLimits validates resource limits are reasonable
func validateResourceLimits(c *Config) error {
	if c.MemoryPerAgent < 1024 {
		return fmt.Errorf("memory_per_agent must be at least 1024 MB, got %d", c.MemoryPerAgent)
	}
	if c.CPUPerAgent < 1 {
		return fmt.Errorf("cpu_per_agent must be at least 1, got %d", c.CPUPerAgent)
	}
	if c.MaxAgents < 1 {
		return fmt.Errorf("max_agents must be at least 1, got %d", c.MaxAgents)
	}
	if c.StoragePerAgent < 100 {
		return fmt.Errorf("storage_per_agent must be at least 100 MB, got %d", c.StoragePerAgent)
	}
	return nil
}

// validateDocker checks Docker daemon accessibility
func validateDocker(c *Config) error {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer cli.Close()

	// Check Docker daemon
	_, err = cli.Ping(context.Background())
	if err != nil {
		return fmt.Errorf("Docker daemon not accessible: %w", err)
	}

	return nil
}

// validateDiskSpace checks available disk space for Docker operations
func validateDiskSpace() error {
	// Check /var/lib/docker or fallback to /
	paths := []string{"/var/lib/docker", "/"}

	for _, path := range paths {
		var stat unix.Statfs_t
		if err := unix.Statfs(path, &stat); err != nil {
			continue // Try next path
		}

		// Calculate available space in GB
		availableBytes := stat.Bavail * uint64(stat.Bsize)
		availableGB := availableBytes / (1024 * 1024 * 1024)

		// Need at least 5GB for Docker images + agent containers
		// alpine/openclaw:main image is ~3GB + decompression overhead + container layers
		const minRequiredGB = 5

		if availableGB < minRequiredGB {
			return fmt.Errorf("insufficient disk space at %s: %dGB available, need %dGB minimum", path, availableGB, minRequiredGB)
		}

		return nil // Success
	}

	return fmt.Errorf("could not check disk space: paths not accessible")
}

// validateDataDirectory checks data directory permissions
func validateDataDirectory(c *Config) error {
	if err := os.MkdirAll(c.DataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data_dir: %w", err)
	}

	// Check write permission
	testFile := c.DataDir + "/.test_write"
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("data_dir is not writable: %w", err)
	}
	os.Remove(testFile)

	return nil
}

// checkHubReachability checks if Hub is reachable (optional, just warns)
func checkHubReachability(c *Config, logger *logrus.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use unified HTTP client
	httpClient := httputil.NewClient(c.HubURL, 5*time.Second)
	resp, err := httpClient.Get(ctx, "/health")
	if err == nil {
		resp.Body.Close()
	}

	if err != nil {
		logger.WithFields(logrus.Fields{
			"hub_url": c.HubURL,
			"error":   err,
		}).Warn("Hub not reachable during validation")
	}
}
