package agent

import (
	"context"
	"errors"
	stderrors "errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	apperrors "github.com/zion-protocol/zion-node/internal/errors"
	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// TestManagerRunWithContainerCreateError tests Run with container create error
func TestManagerRunWithContainerCreateError(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	mockContainer.SetCreateError(errors.New("docker create failed"))

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	stateManager := NewStateManager(cfg, logger)
	var snapshotEngine SnapshotEngine = nil
	var hubClient HubClient = nil

	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, snapshotEngine, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	agentID := "test-error-agent"
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}

	_, err = manager.Run(ctx, agentID, profile, "", "", nil)
	if err == nil {
		t.Error("Expected error when container creation fails")
	}
}

// TestManagerRunWithContainerStartError tests Run with container start error
func TestManagerRunWithContainerStartError(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	mockContainer.SetStartError(errors.New("docker start failed"))

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	stateManager := NewStateManager(cfg, logger)
	var snapshotEngine SnapshotEngine = nil
	var hubClient HubClient = nil

	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, snapshotEngine, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	agentID := "test-start-error-agent"
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}

	_, err = manager.Run(ctx, agentID, profile, "", "", nil)
	if err == nil {
		t.Error("Expected error when container start fails")
	}

	// Verify container was cleaned up
	containers := mockContainer.ListContainers()
	if len(containers) > 0 {
		t.Error("Container should be removed after start failure")
	}
}

// TestManagerStopWithCheckpointError tests Stop with checkpoint creation error
func TestManagerStopWithCheckpointError(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	mockContainer.SetImage(cfg.RuntimeImage, true)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	stateManager := NewStateManager(cfg, logger)
	var snapshotEngine SnapshotEngine = nil
	var hubClient HubClient = nil

	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, snapshotEngine, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	agentID := "test-checkpoint-error-agent"
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}

	// Run agent first
	_, err = manager.Run(ctx, agentID, profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	// Stop with checkpoint
	// Note: If snapshotEngine is nil, checkpoint creation is skipped, not an error
	// The test verifies that stop still works even if checkpoint is requested but engine is nil
	_, err = manager.Stop(ctx, agentID, true)
	// Stop should succeed even if checkpoint creation is skipped (when engine is nil)
	if err != nil {
		t.Errorf("Stop should succeed even when checkpoint engine is nil: %v", err)
	}
}

// TestManagerCapacityLimitError tests capacity limit error
func TestManagerCapacityLimitError(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	cfg.MaxAgents = 1 // Set capacity to 1
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	mockContainer.SetImage(cfg.RuntimeImage, true)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	stateManager := NewStateManager(cfg, logger)
	var snapshotEngine SnapshotEngine = nil
	var hubClient HubClient = nil

	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, snapshotEngine, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}

	// Run first agent
	_, err = manager.Run(ctx, "agent-01", profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run first agent: %v", err)
	}

	// Try to run second agent (should fail due to capacity)
	_, err = manager.Run(ctx, "agent-02", profile, "", "", nil)
	if err == nil {
		t.Error("Expected error when capacity limit is reached")
	}
}

// TestManagerRunWithImagePullError tests that EnsureImage errors propagate correctly
// Note: In the actual code flow, EnsureImage is called at node startup (not inside Run).
// This test verifies the mock infrastructure works correctly for EnsureImage failures.
func TestManagerRunWithImagePullError(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	mockContainer.SetEnsureImageError(errors.New("image pull failed: repository not found"))

	// Verify EnsureImage returns the error
	err := mockContainer.EnsureImage(context.Background())
	if err == nil {
		t.Error("Expected error from EnsureImage")
	}
	if err.Error() != "image pull failed: repository not found" {
		t.Errorf("Unexpected error message: %v", err)
	}

	// Clear the error
	mockContainer.SetEnsureImageError(nil)
	err = mockContainer.EnsureImage(context.Background())
	if err != nil {
		t.Errorf("Expected nil after clearing error: %v", err)
	}

	// Verify that Create still fails correctly with its own error
	mockContainer.SetCreateError(errors.New("docker create failed"))

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	stateManager := NewStateManager(cfg, logger)
	var snapshotEngine SnapshotEngine = nil
	var hubClient HubClient = nil

	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, snapshotEngine, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	agentID := "test-pull-fail-agent"
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}

	_, err = manager.Run(ctx, agentID, profile, "", "", nil)
	if err == nil {
		t.Error("Expected error when container creation fails")
	}
}

// TestManagerRunWithMultipleErrors tests multiple sequential failure modes
func TestManagerRunWithMultipleErrors(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	stateManager := NewStateManager(cfg, logger)
	var snapshotEngine SnapshotEngine = nil
	var hubClient HubClient = nil

	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, snapshotEngine, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}

	// First: create error
	mockContainer.SetCreateError(errors.New("container create failed"))
	_, err = manager.Run(ctx, "agent-1", profile, "", "", nil)
	if err == nil {
		t.Error("Expected error for container create failure")
	}

	// Clear create error, set start error
	mockContainer.SetCreateError(nil)
	mockContainer.SetStartError(errors.New("container start failed"))
	_, err = manager.Run(ctx, "agent-2", profile, "", "", nil)
	if err == nil {
		t.Error("Expected error for container start failure")
	}

	// Verify cleanup after start failure (container should be removed)
	containers := mockContainer.ListContainers()
	for _, c := range containers {
		if c.Name == "zion-agent-agent-2" {
			t.Error("Container for agent-2 should be removed after start failure")
		}
	}

	// Clear all errors - should succeed
	mockContainer.SetStartError(nil)
	_, err = manager.Run(ctx, "agent-3", profile, "", "", nil)
	if err != nil {
		t.Errorf("Expected success after clearing errors: %v", err)
	}
}

// --- New edge case tests ---

// newTestManager is a helper to create a Manager with mock dependencies.
func newTestManager(t *testing.T) (*Manager, *testutil.MockContainerManager, *testutil.MockHubClient) {
	t.Helper()
	mockHub := testutil.NewMockHub()
	t.Cleanup(mockHub.Close)

	cfg := testutil.NewTestConfig(mockHub.URL())
	t.Cleanup(func() { testutil.CleanupTestConfig(cfg) })

	mockContainer := testutil.NewMockContainerManager()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	stateManager := NewStateManager(cfg, logger)
	mockHubClient := testutil.NewMockHubClient()

	manager, err := NewManager(cfg, mockContainer, stateManager, mockHubClient, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	return manager, mockContainer, mockHubClient
}

var defaultProfile = types.RuntimeProfile{
	Engine:         "openclaw",
	EngineVersion:  "v1",
	ImageHash:      "test",
	SnapshotFormat: "tar.zst",
}

// TestManagerRun_DuplicateAgentID verifies ErrAgentAlreadyRunning on duplicate Run
func TestManagerRun_DuplicateAgentID(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := context.Background()

	// First run succeeds
	_, err := manager.Run(ctx, "dup-agent", defaultProfile, "", "", nil)
	if err != nil {
		t.Fatalf("First Run failed: %v", err)
	}

	// Second run with same ID should fail
	_, err = manager.Run(ctx, "dup-agent", defaultProfile, "", "", nil)
	if err == nil {
		t.Fatal("Expected error for duplicate agentID, got nil")
	}

	var alreadyRunning *apperrors.ErrAgentAlreadyRunning
	if !stderrors.As(err, &alreadyRunning) {
		t.Errorf("Expected ErrAgentAlreadyRunning, got %T: %v", err, err)
	}
}

// TestManagerRun_EmptyAgentID verifies Run with empty string doesn't crash
func TestManagerRun_EmptyAgentID(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := context.Background()

	// Should not panic — creates agent with empty ID
	agent, err := manager.Run(ctx, "", defaultProfile, "", "", nil)
	if err != nil {
		t.Fatalf("Run with empty agentID returned error: %v", err)
	}
	if agent.AgentID != "" {
		t.Errorf("Expected empty agentID, got %q", agent.AgentID)
	}
}

// TestManagerStop_NonExistentAgent verifies ErrAgentNotFound on Stop unknown agent
func TestManagerStop_NonExistentAgent(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := context.Background()

	_, err := manager.Stop(ctx, "nonexistent-agent", false)
	if err == nil {
		t.Fatal("Expected error for nonexistent agent, got nil")
	}

	var notFound *apperrors.ErrAgentNotFound
	if !stderrors.As(err, &notFound) {
		t.Errorf("Expected ErrAgentNotFound, got %T: %v", err, err)
	}
}

// TestManagerStop_ContainerStopFailure verifies ErrContainerOperation on Stop failure
func TestManagerStop_ContainerStopFailure(t *testing.T) {
	manager, mockContainer, _ := newTestManager(t)
	ctx := context.Background()

	// Run an agent
	_, err := manager.Run(ctx, "stop-fail-agent", defaultProfile, "", "", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Set stop error
	mockContainer.SetStopError(errors.New("docker stop timeout"))

	_, err = manager.Stop(ctx, "stop-fail-agent", false)
	if err == nil {
		t.Fatal("Expected error for container stop failure, got nil")
	}

	var containerErr *apperrors.ErrContainerOperation
	if !stderrors.As(err, &containerErr) {
		t.Errorf("Expected ErrContainerOperation, got %T: %v", err, err)
	}
}

// TestManagerStop_ContainerRemoveFailure verifies ErrContainerOperation on Remove failure
func TestManagerStop_ContainerRemoveFailure(t *testing.T) {
	manager, mockContainer, _ := newTestManager(t)
	ctx := context.Background()

	// Run an agent
	_, err := manager.Run(ctx, "remove-fail-agent", defaultProfile, "", "", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Set remove error
	mockContainer.SetRemoveError(errors.New("device busy"))

	_, err = manager.Stop(ctx, "remove-fail-agent", false)
	if err == nil {
		t.Fatal("Expected error for container remove failure, got nil")
	}

	var containerErr *apperrors.ErrContainerOperation
	if !stderrors.As(err, &containerErr) {
		t.Errorf("Expected ErrContainerOperation, got %T: %v", err, err)
	}

	// Clean up: clear prevent further errors
	mockContainer.SetRemoveError(nil)
}

// TestManagerGetAgent_NonExistent verifies GetAgent returns ErrAgentNotFound
func TestManagerGetAgent_NonExistent(t *testing.T) {
	manager, _, _ := newTestManager(t)

	_, err := manager.GetAgent("nonexistent")
	if err == nil {
		t.Fatal("Expected error for nonexistent agent")
	}

	var notFound *apperrors.ErrAgentNotFound
	if !stderrors.As(err, &notFound) {
		t.Errorf("Expected ErrAgentNotFound, got %T: %v", err, err)
	}
}

// TestManagerGetAgent_Exists verifies GetAgent returns the correct agent
func TestManagerGetAgent_Exists(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := context.Background()

	_, err := manager.Run(ctx, "existing-agent", defaultProfile, "", "", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	agent, err := manager.GetAgent("existing-agent")
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if agent.AgentID != "existing-agent" {
		t.Errorf("Expected agentID existing-agent, got %s", agent.AgentID)
	}
	if agent.Status != types.AgentStatusRunning {
		t.Errorf("Expected status running, got %s", agent.Status)
	}
}

// TestManagerListAgents_Empty verifies ListAgents returns empty slice
func TestManagerListAgents_Empty(t *testing.T) {
	manager, _, _ := newTestManager(t)

	list := manager.ListAgents()
	if len(list) != 0 {
		t.Errorf("Expected empty list, got %d agents", len(list))
	}
}

// TestManagerListAgents_Multiple verifies ListAgents returns all agents
func TestManagerListAgents_Multiple(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		agentID := "list-agent-" + string(rune('a'+i))
		if _, err := manager.Run(ctx, agentID, defaultProfile, "", "", nil); err != nil {
			t.Fatalf("Run %s failed: %v", agentID, err)
		}
	}

	list := manager.ListAgents()
	if len(list) != 3 {
		t.Errorf("Expected 3 agents, got %d", len(list))
	}

	// Verify all have status "running" and positive uptime
	for _, info := range list {
		if info.Status != string(types.AgentStatusRunning) {
			t.Errorf("Expected running, got %s for %s", info.Status, info.AgentID)
		}
	}
}

// TestManagerHandleContainerFailure_NonExistentAgent verifies silent return
func TestManagerHandleContainerFailure_NonExistentAgent(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := context.Background()

	// Should not panic — just returns silently
	manager.HandleContainerFailure(ctx, "ghost-agent", "container crashed")

	// Should still be empty
	list := manager.ListAgents()
	if len(list) != 0 {
		t.Errorf("Expected 0 agents, got %d", len(list))
	}
}

// TestManagerHandleContainerFailure_MaxRestarts_MarkedDead verifies agent is marked dead
func TestManagerHandleContainerFailure_MaxRestarts_MarkedDead(t *testing.T) {
	manager, _, mockHubClient := newTestManager(t)
	ctx := context.Background()

	// Run an agent
	agent, err := manager.Run(ctx, "dead-agent", defaultProfile, "", "", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Simulate max restart failures
	manager.mu.Lock()
	agent.RestartCount = MaxRestartAttempts - 1 // one below max
	manager.mu.Unlock()

	// This should push it over the limit
	manager.HandleContainerFailure(ctx, "dead-agent", "OOM killed")

	// Wait for async cleanup
	time.Sleep(200 * time.Millisecond)

	// Agent should be removed (dead agents are cleaned up)
	_, err = manager.GetAgent("dead-agent")
	if err == nil {
		t.Error("Expected dead agent to be removed after max restarts")
	}

	// Hub should have been notified of failure
	failures := mockHubClient.GetFailures()
	found := false
	for _, f := range failures {
		if f.AgentID == "dead-agent" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected hub to be notified of agent failure")
	}
}

// TestManagerHandleContainerFailure_RestartsSuccessful verifies restart increments count
func TestManagerHandleContainerFailure_RestartsSuccessful(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := context.Background()

	_, err := manager.Run(ctx, "restart-agent", defaultProfile, "", "", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Trigger one failure (below max restarts)
	manager.HandleContainerFailure(ctx, "restart-agent", "segfault")

	// Wait for async restart
	time.Sleep(500 * time.Millisecond)

	// Agent should still exist (restarted)
	agent, err := manager.GetAgent("restart-agent")
	if err != nil {
		t.Fatalf("Expected agent to exist after restart: %v", err)
	}

	if agent.RestartCount != 1 {
		t.Errorf("Expected restart count 1, got %d", agent.RestartCount)
	}
}

// TestManagerGetContainerManager verifies GetContainerManager returns correctly
func TestManagerGetContainerManager(t *testing.T) {
	manager, _, _ := newTestManager(t)

	cm, ok := manager.GetContainerManager()
	if !ok {
		t.Error("Expected GetContainerManager to return true")
	}
	if cm == nil {
		t.Error("Expected non-nil ContainerManager")
	}
}

// TestManagerGetContainerManager_Nil verifies GetContainerManager with nil container
func TestManagerGetContainerManager_Nil(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	stateManager := NewStateManager(cfg, logger)

	manager, _ := NewManager(cfg, nil, stateManager, nil, nil, logger)

	cm, ok := manager.GetContainerManager()
	if ok {
		t.Error("Expected GetContainerManager to return false when container is nil")
	}
	if cm != nil {
		t.Error("Expected nil ContainerManager")
	}
}

// TestManagerStop_ThenGetAgent verifies state is cleaned up after Stop
func TestManagerStop_ThenGetAgent(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := context.Background()

	_, err := manager.Run(ctx, "cleanup-agent", defaultProfile, "", "", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_, err = manager.Stop(ctx, "cleanup-agent", false)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// GetAgent should return not found
	_, err = manager.GetAgent("cleanup-agent")
	if err == nil {
		t.Error("Expected GetAgent to return error after Stop")
	}

	// ListAgents should be empty
	list := manager.ListAgents()
	if len(list) != 0 {
		t.Errorf("Expected 0 agents after stop, got %d", len(list))
	}
}

// TestManagerRun_SnapshotRestoreFailure verifies Run returns error on snapshot restore failure
func TestManagerRun_SnapshotRestoreFailure(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	stateManager := NewStateManager(cfg, logger)

	// Use a mock snapshot engine that fails
	mockSnapshot := &mockSnapshotEngine{
		restoreErr: errors.New("corrupt snapshot"),
	}

	manager, err := NewManager(cfg, mockContainer, stateManager, nil, mockSnapshot, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	_, err = manager.Run(ctx, "snapshot-fail-agent", defaultProfile, "sha256:bad", "", nil)
	if err == nil {
		t.Fatal("Expected error for snapshot restore failure")
	}
	if !errors.Is(err, mockSnapshot.restoreErr) {
		t.Errorf("Expected restore error to be wrapped, got: %v", err)
	}
}

// mockSnapshotEngine is a test mock for SnapshotEngine
type mockSnapshotEngine struct {
	createErr  error
	restoreErr error
	createRef  *types.SnapshotRef
}

func (m *mockSnapshotEngine) Create(ctx context.Context, agentID string, containerID string) (*types.SnapshotRef, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.createRef, nil
}

func (m *mockSnapshotEngine) Restore(ctx context.Context, agentID string, snapshotRef string, downloadURL string) error {
	return m.restoreErr
}

// TestManagerStop_CheckpointCreateFailure verifies Stop continues when checkpoint fails
func TestManagerStop_CheckpointCreateFailure(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	stateManager := NewStateManager(cfg, logger)

	mockSnapshot := &mockSnapshotEngine{
		createErr: errors.New("disk full"),
	}

	manager, _ := NewManager(cfg, mockContainer, stateManager, nil, mockSnapshot, logger)
	ctx := context.Background()

	// Run agent first
	_, err := manager.Run(ctx, "checkpoint-agent", defaultProfile, "", "", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Stop with checkpoint=true — should succeed even though checkpoint fails
	ref, err := manager.Stop(ctx, "checkpoint-agent", true)
	if err != nil {
		t.Fatalf("Stop should succeed even with checkpoint failure, got: %v", err)
	}
	if ref != "" {
		t.Errorf("Expected empty ref when checkpoint fails, got %s", ref)
	}

	// Agent should be removed from manager
	if _, err := manager.GetAgent("checkpoint-agent"); err == nil {
		t.Error("Agent should be removed after stop, but it still exists")
	}
}

// TestManagerStop_CheckpointSuccess verifies checkpointRef is returned
func TestManagerStop_CheckpointSuccess(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	stateManager := NewStateManager(cfg, logger)

	mockSnapshot := &mockSnapshotEngine{
		createRef: &types.SnapshotRef{Ref: "sha256:checkpoint123"},
	}

	manager, _ := NewManager(cfg, mockContainer, stateManager, nil, mockSnapshot, logger)
	ctx := context.Background()

	_, err := manager.Run(ctx, "checkpoint-ok-agent", defaultProfile, "", "", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	ref, err := manager.Stop(ctx, "checkpoint-ok-agent", true)
	if err != nil {
		t.Fatalf("Stop with checkpoint failed: %v", err)
	}
	if ref != "sha256:checkpoint123" {
		t.Errorf("Expected checkpoint ref sha256:checkpoint123, got %s", ref)
	}
}
