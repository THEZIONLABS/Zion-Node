package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/agent"
	"github.com/zion-protocol/zion-node/internal/config"
)

func main() {
	// Create config with non-existent image to test error handling
	cfg := &config.Config{
		RuntimeImage:    "ghcr.io/nonexistent/private-image:latest",
		DataDir:         "/tmp/test-node-data",
		CPUPerAgent:     2,
		MemoryPerAgent:  2048,
		StoragePerAgent: 10240,
		MaxAgents:       1,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	dockerMgr, err := agent.NewDockerManager(cfg, logger)
	if err != nil {
		fmt.Printf("Failed to create Docker manager: %v\n", err)
		os.Exit(1)
	}
	defer dockerMgr.Close()

	ctx := context.Background()
	err = dockerMgr.EnsureImage(ctx)
	if err != nil {
		fmt.Printf("\n✅ Error handling test passed!\n")
		fmt.Printf("Error message: %v\n", err)
	} else {
		fmt.Printf("\n❌ Expected error but got none\n")
	}
}
