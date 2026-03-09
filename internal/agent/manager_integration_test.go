package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// TestAgentLifecycle tests agent run and stop
func TestAgentLifecycle(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	mockContainer.SetImage(cfg.RuntimeImage, true)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Suppress test logs
	stateManager := NewStateManager(cfg, logger)
	manager, err := NewManager(cfg, mockContainer, stateManager, nil, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	agentID := "test-agent-01"
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "abc123",
		SnapshotFormat: "tar.zst",
	}

	// Test: Run agent
	agent, err := manager.Run(ctx, agentID, profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	if agent.Status != types.AgentStatusRunning {
		t.Errorf("Expected status running, got %s", agent.Status)
	}

	// Verify container was created
	container := mockContainer.GetContainer(agent.ContainerID)
	if container == nil {
		t.Fatal("Container was not created")
	}
	if !container.Running {
		t.Error("Container should be running")
	}

	// Test: Get agent
	retrieved, err := manager.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if retrieved.AgentID != agentID {
		t.Errorf("Expected agent ID %s, got %s", agentID, retrieved.AgentID)
	}

	// Test: List agents
	agents := manager.ListAgents()
	if len(agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(agents))
	}

	// Test: Stop agent
	_, err = manager.Stop(ctx, agentID, false)
	if err != nil {
		t.Fatalf("Failed to stop agent: %v", err)
	}

	// Verify container was removed
	container = mockContainer.GetContainer(agent.ContainerID)
	if container != nil {
		t.Error("Container should be removed")
	}

	// Verify agent is removed from manager
	_, err = manager.GetAgent(agentID)
	if err == nil {
		t.Error("Agent should be removed")
	}
}

// TestAgentCapacityLimit tests capacity limit enforcement
func TestAgentCapacityLimit(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	cfg.MaxAgents = 2
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	mockContainer.SetImage(cfg.RuntimeImage, true)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Suppress test logs
	stateManager := NewStateManager(cfg, logger)
	manager, err := NewManager(cfg, mockContainer, stateManager, nil, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "abc123",
		SnapshotFormat: "tar.zst",
	}

	// Run 2 agents (at capacity)
	for i := 0; i < 2; i++ {
		agentID := fmt.Sprintf("test-agent-%d", i)
		_, err := manager.Run(ctx, agentID, profile, "", "", nil)
		if err != nil {
			t.Fatalf("Failed to run agent %d: %v", i, err)
		}
	}

	// Try to run 3rd agent (should fail)
	_, err = manager.Run(ctx, "test-agent-3", profile, "", "", nil)
	if err == nil {
		t.Error("Expected error when exceeding capacity")
	}

	capacity := manager.GetCapacity()
	if capacity.UsedSlots != 2 {
		t.Errorf("Expected 2 used slots, got %d", capacity.UsedSlots)
	}
	if capacity.TotalSlots != 2 {
		t.Errorf("Expected 2 total slots, got %d", capacity.TotalSlots)
	}
}

// TestAgentFailureRecovery tests 3-retry failure recovery
func TestAgentFailureRecovery(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	mockContainer.SetImage(cfg.RuntimeImage, true)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Suppress test logs
	stateManager := NewStateManager(cfg, logger)
	hubClient := testutil.NewMockHubClient()
	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	agentID := "test-agent-01"
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "abc123",
		SnapshotFormat: "tar.zst",
	}

	// Run agent
	_, err = manager.Run(ctx, agentID, profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	// Simulate 3 failures
	// Note: restartAgent is async, so we need to wait for restart to complete
	// before simulating the next failure. The containerID changes after each restart.
	for i := 0; i < 3; i++ {
		// Get current agent to get current containerID
		currentAgent, err := manager.GetAgent(agentID)
		if err != nil {
			// Agent was already deleted (shouldn't happen before 3 failures)
			break
		}
		originalContainerID := currentAgent.ContainerID

		// Simulate container crash
		mockContainer.SimulateContainerCrash(originalContainerID)
		manager.HandleContainerFailure(ctx, agentID, "container crashed")

		// Wait for restart to complete (if not the last failure)
		if i < 2 {
			// Wait for restartAgent goroutine to complete
			// Poll until containerID changes or timeout
			maxWait := 3 * time.Second
			startTime := time.Now()
			var restartedAgent *types.Agent
			for time.Since(startTime) < maxWait {
				time.Sleep(100 * time.Millisecond)
				var err error
				restartedAgent, err = manager.GetAgent(agentID)
				if err != nil {
					t.Fatalf("Agent should still exist after %d failures", i+1)
				}
				if restartedAgent.ContainerID != originalContainerID {
					// Success - containerID changed
					break
				}
			}

			// Verify agent was restarted (new containerID)
			if restartedAgent == nil {
				var err error
				restartedAgent, err = manager.GetAgent(agentID)
				if err != nil {
					t.Fatalf("Agent should still exist after %d failures", i+1)
				}
			}
			if restartedAgent.ContainerID == originalContainerID {
				t.Errorf("Expected new containerID after restart, got same ID %s (iteration %d, waited %v)", originalContainerID, i+1, time.Since(startTime))
			}
		}
	}

	// Verify agent is removed (after 3 failures, agent is deleted)
	_, err = manager.GetAgent(agentID)
	if err == nil {
		t.Error("Expected agent to be removed after 3 failures")
	}

	// Verify container was removed
	// After 3 failures, the container should be stopped and removed
	// We verify by checking that GetAgent returns an error (agent deleted)
	// and the container should also be removed from mockContainer
	containers := mockContainer.ListContainers()
	found := false
	for _, c := range containers {
		if c.Name == "zion-agent-"+agentID {
			found = true
			break
		}
	}
	if found {
		t.Error("Container should be removed after 3 failures")
	}

	// Verify failure was reported to Hub
	// Note: ReportAgentFailure is called in goroutine, so wait a bit
	time.Sleep(100 * time.Millisecond)
	failures := hubClient.GetFailures()
	if len(failures) < 3 {
		t.Errorf("Expected at least 3 failure reports, got %d", len(failures))
	}
}

// TestAgentOOMRecovery tests OOM handling
func TestAgentOOMRecovery(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	mockContainer.SetImage(cfg.RuntimeImage, true)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Suppress test logs
	stateManager := NewStateManager(cfg, logger)
	hubClient := testutil.NewMockHubClient()
	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	agentID := "test-agent-01"
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "abc123",
		SnapshotFormat: "tar.zst",
	}

	// Run agent
	agent, err := manager.Run(ctx, agentID, profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	// Simulate OOM
	// Note: In real implementation, OOM would be detected by monitoring container stats
	// and then HandleContainerFailure would be called. For testing, we simulate this
	// by setting OOM stats and then calling HandleContainerFailure directly.
	mockContainer.SimulateOOM(agent.ContainerID)
	mockContainer.SetContainerStats(agent.ContainerID, &types.ContainerStats{
		OOMKilled: true,
	})

	// Simulate OOM detection and failure handling
	// In production, this would be triggered by a monitoring loop checking container stats
	originalContainerID := agent.ContainerID
	manager.HandleContainerFailure(ctx, agentID, "OOM killed")

	// Wait for restart to complete - poll until containerID changes
	maxWait := 3 * time.Second
	startTime := time.Now()
	var restartedAgent *types.Agent
	for time.Since(startTime) < maxWait {
		time.Sleep(100 * time.Millisecond)
		var err error
		restartedAgent, err = manager.GetAgent(agentID)
		if err != nil {
			t.Error("Agent should still exist after OOM (should restart)")
			return
		}
		if restartedAgent.ContainerID != originalContainerID {
			// Success - containerID changed
			break
		}
	}

	// Verify failure was reported to Hub
	time.Sleep(100 * time.Millisecond)
	failures := hubClient.GetFailures()
	if len(failures) == 0 {
		t.Error("Expected OOM failure to be reported")
	}

	// Verify agent was restarted (should still exist with new container)
	if restartedAgent == nil {
		var err error
		restartedAgent, err = manager.GetAgent(agentID)
		if err != nil {
			t.Error("Agent should still exist after OOM (should restart)")
			return
		}
	}
	// Verify container was restarted (new containerID)
	if restartedAgent.ContainerID == originalContainerID {
		t.Errorf("Expected new containerID after OOM restart, got same ID %s (waited %v)", originalContainerID, time.Since(startTime))
	}
	// Verify restart count increased
	if restartedAgent.RestartCount == 0 {
		t.Error("Expected restart count to increase after OOM")
	}
}

// TestStatePersistence tests state persistence
func TestStatePersistence(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	mockContainer.SetImage(cfg.RuntimeImage, true)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Suppress test logs
	stateManager := NewStateManager(cfg, logger)
	manager, err := NewManager(cfg, mockContainer, stateManager, nil, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	agentID := "test-agent-01"
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "abc123",
		SnapshotFormat: "tar.zst",
	}

	// Run agent
	_, err = manager.Run(ctx, agentID, profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	// Save state (async, need to wait for save to complete)
	stateManager.Save()
	// Wait for async save to complete
	time.Sleep(200 * time.Millisecond)

	// Create new state manager and load
	newStateManager := NewStateManager(cfg, logger)
	if err := newStateManager.Load(); err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	// Verify agent was loaded
	agents := newStateManager.GetAll()
	if len(agents) != 1 {
		t.Errorf("Expected 1 agent in state, got %d", len(agents))
	}
	if agents[agentID] == nil {
		t.Error("Agent not found in loaded state")
	}
}
