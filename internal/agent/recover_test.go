package agent

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

func setupRecoverTest(t *testing.T) (*Manager, *testutil.MockContainerManager, context.CancelFunc) {
	t.Helper()

	cfg := testutil.NewTestConfig("http://localhost:8080")
	t.Cleanup(func() { testutil.CleanupTestConfig(cfg) })
	logger := testutil.NewTestLogger()
	mockDocker := testutil.NewMockContainerManager()
	mockHub := testutil.NewMockHubClient()
	stateManager := &StateManager{
		stateFile: cfg.DataDir + "/agents.json",
		agents:    make(map[string]*types.Agent),
	}
	ctx, cancel := context.WithCancel(context.Background())
	stateManager.saver = &StateSaver{
		stateManager: stateManager,
		saveChan:     make(chan struct{}, 1),
		logger:       logger,
		maxRetries:   1,
		retryDelay:   time.Millisecond,
		ctx:          ctx,
		cancel:       cancel,
	}
	go stateManager.saver.saveLoop()

	manager, _ := NewManager(cfg, mockDocker, stateManager, mockHub, nil, logger)

	return manager, mockDocker, cancel
}

func TestExtractAgentID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"with leading slash", "/zion-agent-abc123", "abc123"},
		{"without leading slash", "zion-agent-abc123", "abc123"},
		{"non-matching name", "other-container", ""},
		{"empty string", "", ""},
		{"just prefix", "zion-agent-", ""},
		{"with slash just prefix", "/zion-agent-", ""},
		{"complex agent ID", "/zion-agent-agent-with-dashes", "agent-with-dashes"},
		{"uuid agent ID", "/zion-agent-550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractAgentID(tt.input)
			if result != tt.expected {
				t.Errorf("extractAgentID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRecoverFromDocker_OrphanCleanup(t *testing.T) {
	manager, mockDocker, cancel := setupRecoverTest(t)
	defer cancel()

	ctx := context.Background()

	// Create a container directly in Docker (orphan - not in state file)
	containerID, err := mockDocker.Create(ctx, "orphan-agent", types.RuntimeProfile{Engine: "openclaw"}, "", nil)
	if err != nil {
		t.Fatalf("Failed to create container: %v", err)
	}
	mockDocker.Start(ctx, containerID)

	// Recover - should detect orphan and remove it
	err = manager.RecoverFromDocker(ctx)
	if err != nil {
		t.Fatalf("RecoverFromDocker failed: %v", err)
	}

	// Orphan container should be removed
	containers := mockDocker.ListContainers()
	for _, c := range containers {
		if c.Name == "zion-agent-orphan-agent" {
			t.Error("Orphan container should have been removed")
		}
	}

	// Manager should not have the orphan agent
	manager.mu.RLock()
	_, exists := manager.agents["orphan-agent"]
	manager.mu.RUnlock()
	if exists {
		t.Error("Orphan agent should not be in manager")
	}
}

func TestRecoverFromDocker_StateReconciliation(t *testing.T) {
	manager, _, cancel := setupRecoverTest(t)
	defer cancel()

	ctx := context.Background()

	// Add agent to state file but NOT to Docker
	manager.stateManager.mu.Lock()
	manager.stateManager.agents["missing-agent"] = &types.Agent{
		AgentID:     "missing-agent",
		ContainerID: "nonexistent-container",
		Status:      types.AgentStatusRunning,
		StartedAt:   time.Now(),
		Profile:     types.RuntimeProfile{Engine: "openclaw"},
	}
	manager.stateManager.mu.Unlock()

	// Recover - should notice container doesn't exist and remove from state
	err := manager.RecoverFromDocker(ctx)
	if err != nil {
		t.Fatalf("RecoverFromDocker failed: %v", err)
	}

	// Agent should NOT be in manager (container doesn't exist)
	manager.mu.RLock()
	_, exists := manager.agents["missing-agent"]
	manager.mu.RUnlock()
	if exists {
		t.Error("Agent without container should not be recovered")
	}
}

func TestRecoverFromDocker_ValidRecovery(t *testing.T) {
	manager, mockDocker, cancel := setupRecoverTest(t)
	defer cancel()

	ctx := context.Background()

	// Create a container in Docker
	containerID, _ := mockDocker.Create(ctx, "valid-agent", types.RuntimeProfile{Engine: "openclaw"}, "", nil)
	mockDocker.Start(ctx, containerID)

	// Add matching entry to state file
	manager.stateManager.mu.Lock()
	manager.stateManager.agents["valid-agent"] = &types.Agent{
		AgentID:     "valid-agent",
		ContainerID: "old-container-id", // Different from actual container ID
		Status:      types.AgentStatusRunning,
		StartedAt:   time.Now(),
		Profile:     types.RuntimeProfile{Engine: "openclaw"},
	}
	manager.stateManager.mu.Unlock()

	// Recover - should update container ID
	err := manager.RecoverFromDocker(ctx)
	if err != nil {
		t.Fatalf("RecoverFromDocker failed: %v", err)
	}

	// Agent should be in manager with updated container ID
	manager.mu.RLock()
	agent, exists := manager.agents["valid-agent"]
	manager.mu.RUnlock()

	if !exists {
		t.Fatal("Agent should be recovered")
	}
	if agent.ContainerID != containerID {
		t.Errorf("Expected container ID %s, got %s", containerID, agent.ContainerID)
	}
}

func TestRecoverFromDocker_EmptyState(t *testing.T) {
	manager, _, cancel := setupRecoverTest(t)
	defer cancel()

	// No containers, no state - should succeed without error
	err := manager.RecoverFromDocker(context.Background())
	if err != nil {
		t.Fatalf("RecoverFromDocker with empty state failed: %v", err)
	}
}

func TestRecoverFromDocker_MultipleOrphans(t *testing.T) {
	manager, mockDocker, cancel := setupRecoverTest(t)
	defer cancel()

	ctx := context.Background()

	// Create multiple orphan containers
	for i := 0; i < 5; i++ {
		agentID := "orphan-" + string(rune('a'+i))
		containerID, _ := mockDocker.Create(ctx, agentID, types.RuntimeProfile{Engine: "openclaw"}, "", nil)
		mockDocker.Start(ctx, containerID)
	}

	err := manager.RecoverFromDocker(ctx)
	if err != nil {
		t.Fatalf("RecoverFromDocker failed: %v", err)
	}

	// All orphans should be cleaned up
	containers := mockDocker.ListContainers()
	if len(containers) > 0 {
		t.Errorf("Expected all orphan containers to be removed, got %d remaining", len(containers))
	}
}

// TestRecoverFromDocker_ContainerListFailure verifies error propagation when List fails
func TestRecoverFromDocker_ContainerListFailure(t *testing.T) {
	manager, mockDocker, cancel := setupRecoverTest(t)
	defer cancel()

	mockDocker.SetListError(fmt.Errorf("docker daemon not responding"))

	err := manager.RecoverFromDocker(context.Background())
	if err == nil {
		t.Fatal("Expected error when container List fails")
	}
	if err.Error() != "docker daemon not responding" {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestRecoverFromDocker_StateLoadFailure verifies error when state file is corrupt
func TestRecoverFromDocker_StateLoadFailure(t *testing.T) {
	manager, _, cancel := setupRecoverTest(t)
	defer cancel()

	// Write corrupt data to state file
	stateFile := manager.stateManager.stateFile
	dir := stateFile[:len(stateFile)-len("/agents.json")]
	os.MkdirAll(dir, 0755)
	os.WriteFile(stateFile, []byte("{corrupt!!!}"), 0644)

	err := manager.RecoverFromDocker(context.Background())
	if err == nil {
		t.Fatal("Expected error when state Load fails with corrupt file")
	}
}

// TestRecoverFromDocker_MixedContainerStates verifies handling of running and stopped containers
func TestRecoverFromDocker_MixedContainerStates(t *testing.T) {
	manager, mockDocker, cancel := setupRecoverTest(t)
	defer cancel()
	ctx := context.Background()

	// Create running orphan
	runningID, _ := mockDocker.Create(ctx, "running-orphan", types.RuntimeProfile{Engine: "openclaw"}, "", nil)
	mockDocker.Start(ctx, runningID)

	// Create stopped orphan (created but not started)
	_, _ = mockDocker.Create(ctx, "stopped-orphan", types.RuntimeProfile{Engine: "openclaw"}, "", nil)

	// Create matched agent (in both Docker and state)
	matchedID, _ := mockDocker.Create(ctx, "matched-agent", types.RuntimeProfile{Engine: "openclaw"}, "", nil)
	mockDocker.Start(ctx, matchedID)
	manager.stateManager.mu.Lock()
	manager.stateManager.agents["matched-agent"] = &types.Agent{
		AgentID:     "matched-agent",
		ContainerID: "old-id",
		Status:      types.AgentStatusRunning,
		StartedAt:   time.Now(),
		Profile:     types.RuntimeProfile{Engine: "openclaw"},
	}
	manager.stateManager.mu.Unlock()

	err := manager.RecoverFromDocker(ctx)
	if err != nil {
		t.Fatalf("RecoverFromDocker failed: %v", err)
	}

	// Matched agent should be recovered
	manager.mu.RLock()
	matched, exists := manager.agents["matched-agent"]
	manager.mu.RUnlock()
	if !exists {
		t.Error("matched-agent should be recovered")
	} else if matched.ContainerID != matchedID {
		t.Errorf("Expected container ID %s, got %s", matchedID, matched.ContainerID)
	}

	// Both orphans should be removed
	manager.mu.RLock()
	_, running := manager.agents["running-orphan"]
	_, stopped := manager.agents["stopped-orphan"]
	manager.mu.RUnlock()
	if running {
		t.Error("running-orphan should not be in manager")
	}
	if stopped {
		t.Error("stopped-orphan should not be in manager")
	}
}
