package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// TestManagerConcurrentOperations tests concurrent operations on manager
func TestManagerConcurrentOperations(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	stateManager := NewStateManager(cfg, testutil.NewTestLogger())
	hubClient := testutil.NewMockHubClient()
	var snapshotEngine SnapshotEngine = nil // Not needed for this test
	logger := testutil.NewTestLogger()

	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, snapshotEngine, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	agentID := "concurrent-test-agent"
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}

	// Run agent first
	ctx := context.Background()
	_, err = manager.Run(ctx, agentID, profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	// Concurrent read operations (should be safe)
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Multiple goroutines trying to get agent (read operations - should be safe)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.GetAgent(agentID)
			if err != nil {
				// Only report non-"not found" errors
				errStr := err.Error()
				if errStr != "agent not found: "+agentID && errStr != "agent not found" {
					errors <- err
				}
			}
		}()
	}

	// Wait for reads to complete before stopping
	wg.Wait()

	// Now stop the agent
	_, err = manager.Stop(ctx, agentID, false)
	if err != nil {
		if err.Error() != "agent not found: "+agentID && err.Error() != "agent not found" {
			t.Errorf("Unexpected stop error: %v", err)
		}
	}

	// Wait for all operations
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All operations completed
	case <-time.After(5 * time.Second):
		t.Error("Concurrent operations timeout")
	}

	// Check for errors
	close(errors)
	for err := range errors {
		if err != nil {
			t.Errorf("Concurrent operation error: %v", err)
		}
	}
}

// TestManagerConcurrentFailureHandling tests concurrent failure handling
func TestManagerConcurrentFailureHandling(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	stateManager := NewStateManager(cfg, testutil.NewTestLogger())
	hubClient := testutil.NewMockHubClient()
	var snapshotEngine SnapshotEngine = nil
	logger := testutil.NewTestLogger()

	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, snapshotEngine, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	agentID := "failure-test-agent"
	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}

	ctx := context.Background()
	_, err = manager.Run(ctx, agentID, profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	// Concurrent failure reports
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			manager.HandleContainerFailure(ctx, agentID, "test failure")
		}(i)
	}

	wg.Wait()

	// Verify agent state
	agent, err := manager.GetAgent(agentID)
	if err == nil {
		// Agent might still exist if not reached max failures
		if agent.RestartCount > MaxRestartAttempts {
			t.Errorf("Agent should be marked as dead after %d failures", MaxRestartAttempts)
		}
	}
}

// TestManagerConcurrentStopAndFailure tests race between Stop and HandleContainerFailure
func TestManagerConcurrentStopAndFailure(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	stateManager := NewStateManager(cfg, testutil.NewTestLogger())
	hubClient := testutil.NewMockHubClient()
	logger := testutil.NewTestLogger()

	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}

	ctx := context.Background()
	_, err = manager.Run(ctx, "race-agent", profile, "", "", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Race: Stop and HandleContainerFailure at the same time
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		manager.Stop(ctx, "race-agent", false)
	}()

	go func() {
		defer wg.Done()
		manager.HandleContainerFailure(ctx, "race-agent", "crash")
	}()

	// Should not panic or deadlock
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatal("Concurrent Stop+HandleContainerFailure deadlocked")
	}
}

// TestManagerConcurrentRunAndStop tests race between multiple Run and Stop
func TestManagerConcurrentRunAndStop(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	cfg.MaxAgents = 20
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	stateManager := NewStateManager(cfg, testutil.NewTestLogger())
	hubClient := testutil.NewMockHubClient()
	logger := testutil.NewTestLogger()

	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}
	ctx := context.Background()

	// Run 10 agents concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			agentID := "rapid-agent-" + string(rune('A'+id))
			_, _ = manager.Run(ctx, agentID, profile, "", "", nil)
		}(i)
	}
	wg.Wait()

	// Stop them all concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			agentID := "rapid-agent-" + string(rune('A'+id))
			manager.Stop(ctx, agentID, false)
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatal("Concurrent Run+Stop deadlocked")
	}

	// All agents should be gone
	list := manager.ListAgents()
	if len(list) != 0 {
		t.Errorf("Expected 0 agents after concurrent stop, got %d", len(list))
	}
}

// TestManagerConcurrentListWhileModifying tests ListAgents during concurrent runs
func TestManagerConcurrentListWhileModifying(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	cfg.MaxAgents = 50
	defer testutil.CleanupTestConfig(cfg)

	mockContainer := testutil.NewMockContainerManager()
	stateManager := NewStateManager(cfg, testutil.NewTestLogger())
	hubClient := testutil.NewMockHubClient()
	logger := testutil.NewTestLogger()

	manager, err := NewManager(cfg, mockContainer, stateManager, hubClient, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	profile := types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}
	ctx := context.Background()

	// Run, stop, and list concurrently
	var wg sync.WaitGroup

	// Producers: run agents
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			agentID := "list-agent-" + string(rune('A'+id))
			manager.Run(ctx, agentID, profile, "", "", nil)
		}(i)
	}

	// Concurrent readers: list agents while producers are running
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			list := manager.ListAgents()
			// Should never return negative or nil
			if list == nil {
				t.Error("ListAgents returned nil")
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success — no data race
	case <-time.After(10 * time.Second):
		t.Fatal("Concurrent list+modify deadlocked")
	}
}
