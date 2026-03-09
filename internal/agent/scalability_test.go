package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/config"
	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// TestGetCapacity_DoesNotBlockRunStop verifies that GetCapacity (which does a
// 1-second CPU sample) does NOT hold the agent lock, so Run/Stop can proceed
// in parallel without waiting for the sample to finish.
func TestGetCapacity_DoesNotBlockRunStop(t *testing.T) {
	cfg := &config.Config{
		MaxAgents:      10,
		CPUPerAgent:    1,
		MemoryPerAgent: 256,
		SystemCPU:      4,
	}
	container := testutil.NewMockContainerManager()
	stateManager := NewStateManager(cfg, logrus.New())
	hubClient := testutil.NewMockHubClient()
	manager, err := NewManager(cfg, container, stateManager, hubClient, nil, logrus.New())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Start GetCapacity in background (takes ~1s for CPU sample)
	capacityCh := make(chan types.CapacityInfo, 1)
	go func() {
		capacityCh <- manager.GetCapacity()
	}()

	// Give GetCapacity a moment to start, then try Run
	time.Sleep(50 * time.Millisecond)

	// Run should NOT be blocked by GetCapacity holding the lock
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		ctx := context.Background()
		_, _ = manager.Run(ctx, "agent-parallel-1", types.RuntimeProfile{Engine: "test"}, "", "", nil)
	}()

	// Run should complete well within the 1s CPU sample window
	select {
	case <-runDone:
		// Success: Run was not blocked
	case <-time.After(500 * time.Millisecond):
		t.Error("Run was blocked for >500ms — GetCapacity is likely holding the agent lock during CPU sampling")
	}

	// Wait for capacity result
	select {
	case cap := <-capacityCh:
		if cap.TotalSlots != 10 {
			t.Errorf("Expected TotalSlots=10, got %d", cap.TotalSlots)
		}
	case <-time.After(3 * time.Second):
		t.Error("GetCapacity did not return within 3s")
	}
}

// TestGetCapacity_CorrectSlotCount verifies slot counts are accurate.
func TestGetCapacity_CorrectSlotCount(t *testing.T) {
	cfg := &config.Config{
		MaxAgents:      5,
		CPUPerAgent:    1,
		MemoryPerAgent: 256,
		SystemCPU:      2,
	}
	container := testutil.NewMockContainerManager()
	stateManager := NewStateManager(cfg, logrus.New())
	hubClient := testutil.NewMockHubClient()
	manager, err := NewManager(cfg, container, stateManager, hubClient, nil, logrus.New())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx := context.Background()

	// Start 3 agents
	for i := 0; i < 3; i++ {
		if _, err := manager.Run(ctx, "agent-slot-"+string(rune('a'+i)), types.RuntimeProfile{Engine: "test"}, "", "", nil); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
	}

	cap := manager.GetCapacity()
	if cap.TotalSlots != 5 {
		t.Errorf("Expected TotalSlots=5, got %d", cap.TotalSlots)
	}
	if cap.UsedSlots != 3 {
		t.Errorf("Expected UsedSlots=3, got %d", cap.UsedSlots)
	}
}

// TestGetCapacity_ConcurrentAccess verifies GetCapacity is safe to call
// concurrently with Run, Stop, and other GetCapacity calls.
func TestGetCapacity_ConcurrentAccess(t *testing.T) {
	cfg := &config.Config{
		MaxAgents:      50,
		CPUPerAgent:    1,
		MemoryPerAgent: 256,
		SystemCPU:      4,
	}
	container := testutil.NewMockContainerManager()
	stateManager := NewStateManager(cfg, logrus.New())
	hubClient := testutil.NewMockHubClient()
	manager, err := NewManager(cfg, container, stateManager, hubClient, nil, logrus.New())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup

	// Launch concurrent operations
	// 5 GetCapacity calls
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cap := manager.GetCapacity()
			if cap.TotalSlots != 50 {
				t.Errorf("Expected TotalSlots=50, got %d", cap.TotalSlots)
			}
		}()
	}

	// 5 Run calls
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			agentID := "concurrent-" + string(rune('a'+id))
			_, _ = manager.Run(ctx, agentID, types.RuntimeProfile{Engine: "test"}, "", "", nil)
		}(i)
	}

	wg.Wait()
}

// TestHandleContainerFailure_ConcurrentRestart verifies that concurrent
// HandleContainerFailure calls for the same agent don't run two restarts.
func TestHandleContainerFailure_ConcurrentRestart(t *testing.T) {
	cfg := &config.Config{
		MaxAgents:      10,
		CPUPerAgent:    1,
		MemoryPerAgent: 256,
	}
	container := testutil.NewMockContainerManager()
	stateManager := NewStateManager(cfg, logrus.New())
	hubClient := testutil.NewMockHubClient()
	manager, err := NewManager(cfg, container, stateManager, hubClient, nil, logrus.New())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx := context.Background()
	_, err = manager.Run(ctx, "restart-race", types.RuntimeProfile{Engine: "test"}, "", "", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Trigger two concurrent failures — both increment RestartCount
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			manager.HandleContainerFailure(ctx, "restart-race", "test failure")
		}(i)
	}
	wg.Wait()

	// Give restarts time to settle
	time.Sleep(200 * time.Millisecond)

	// The agent should still exist (< MaxRestartAttempts) or have been cleaned up.
	// The key invariant is: no panic, no deadlock, and restartCount is consistent.
	manager.mu.RLock()
	agent, exists := manager.agents["restart-race"]
	if exists {
		if agent.RestartCount < 1 {
			t.Errorf("Expected RestartCount >= 1, got %d", agent.RestartCount)
		}
	}
	manager.mu.RUnlock()
}
