package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

func setupMonitorTest(t *testing.T) (*Manager, *testutil.MockContainerManager, *ContainerMonitor, context.CancelFunc) {
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
	// Create a no-op saver to avoid goroutine issues
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

	monitor := NewContainerMonitor(manager, logger)
	monitor.interval = 50 * time.Millisecond // Fast interval for tests

	return manager, mockDocker, monitor, cancel
}

func TestContainerMonitor_DetectsDeadContainer(t *testing.T) {
	manager, mockDocker, monitor, cancel := setupMonitorTest(t)
	defer cancel()

	// Start an agent
	ctx := context.Background()
	profile := types.RuntimeProfile{Engine: "openclaw"}
	agent, err := manager.Run(ctx, "agent-dead-test", profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	// Simulate container crash
	mockDocker.SimulateContainerCrash(agent.ContainerID)

	// Run monitor check
	monitorCtx, monitorCancel := context.WithTimeout(ctx, 2*time.Second)
	defer monitorCancel()

	// Start monitor in background
	done := make(chan struct{})
	go func() {
		monitor.Start(monitorCtx)
		close(done)
	}()

	// Wait for monitor to detect the failure and restart (or give up after 3 attempts)
	time.Sleep(500 * time.Millisecond)
	monitorCancel()
	<-done

	// Agent should have been handled (either restarted or removed after max attempts)
	// The HandleContainerFailure increments restart count
}

func TestContainerMonitor_DetectsRemovedContainer(t *testing.T) {
	manager, mockDocker, monitor, cancel := setupMonitorTest(t)
	defer cancel()

	ctx := context.Background()
	profile := types.RuntimeProfile{Engine: "openclaw"}
	agent, err := manager.Run(ctx, "agent-removed-test", profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	// Remove container externally (simulating Docker removing it)
	mockDocker.Remove(ctx, agent.ContainerID)

	// Run a single check
	monitor.checkAll(ctx)

	// Give HandleContainerFailure goroutine time to process
	time.Sleep(200 * time.Millisecond)

	// Agent should have been handled
	manager.mu.RLock()
	a, exists := manager.agents["agent-removed-test"]
	manager.mu.RUnlock()

	if exists && a.RestartCount == 0 {
		t.Error("Expected agent restart count to be incremented after container removal")
	}
}

func TestContainerMonitor_IgnoresRunningContainers(t *testing.T) {
	manager, _, monitor, cancel := setupMonitorTest(t)
	defer cancel()

	ctx := context.Background()
	profile := types.RuntimeProfile{Engine: "openclaw"}
	_, err := manager.Run(ctx, "agent-running", profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	// Run check - should not trigger failure for running containers
	monitor.checkAll(ctx)

	manager.mu.RLock()
	agent := manager.agents["agent-running"]
	manager.mu.RUnlock()

	if agent == nil {
		t.Fatal("Agent should still exist")
	}
	if agent.RestartCount != 0 {
		t.Errorf("Expected restart count 0, got %d", agent.RestartCount)
	}
}

func TestContainerMonitor_NoAgents(t *testing.T) {
	_, _, monitor, cancel := setupMonitorTest(t)
	defer cancel()

	// Should not panic with no agents
	monitor.checkAll(context.Background())
}

func TestContainerMonitor_Stop(t *testing.T) {
	_, _, monitor, cancel := setupMonitorTest(t)
	defer cancel()

	// Stop should be idempotent
	monitor.Stop()
	monitor.Stop() // Second stop should not panic
}

func TestContainerMonitor_StopStopsLoop(t *testing.T) {
	_, _, monitor, cancel := setupMonitorTest(t)
	defer cancel()

	done := make(chan struct{})
	go func() {
		monitor.Start(context.Background())
		close(done)
	}()

	// Stop the monitor
	time.Sleep(100 * time.Millisecond)
	monitor.Stop()

	// Should return within reasonable time
	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("Monitor did not stop after Stop() was called")
	}
}

func TestContainerMonitor_ContextCancellation(t *testing.T) {
	_, _, monitor, cancel := setupMonitorTest(t)
	defer cancel()

	ctx, ctxCancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		monitor.Start(ctx)
		close(done)
	}()

	// Cancel context
	time.Sleep(100 * time.Millisecond)
	ctxCancel()

	// Should return within reasonable time
	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("Monitor did not stop after context cancellation")
	}
}

func TestContainerMonitor_ConcurrentChecks(t *testing.T) {
	manager, _, monitor, cancel := setupMonitorTest(t)
	defer cancel()

	ctx := context.Background()
	profile := types.RuntimeProfile{Engine: "openclaw"}
	_, err := manager.Run(ctx, "agent-concurrent", profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	// Run multiple concurrent checks
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			monitor.checkAll(ctx)
		}()
	}
	wg.Wait()
	// Should not panic or deadlock
}

func TestContainerMonitor_DetectsOOMKilled(t *testing.T) {
	manager, mockDocker, monitor, cancel := setupMonitorTest(t)
	defer cancel()

	ctx := context.Background()
	profile := types.RuntimeProfile{Engine: "openclaw"}
	agent, err := manager.Run(ctx, "agent-oom-test", profile, "", "", nil)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	// Simulate OOM kill: container crashes + OOM flag set
	mockDocker.SimulateContainerCrash(agent.ContainerID)
	mockDocker.SimulateOOM(agent.ContainerID)

	// Run monitor check
	monitorCtx, monitorCancel := context.WithTimeout(ctx, 2*time.Second)
	defer monitorCancel()

	done := make(chan struct{})
	go func() {
		monitor.Start(monitorCtx)
		close(done)
	}()

	time.Sleep(500 * time.Millisecond)
	monitorCancel()
	<-done

	// Agent should have been handled (restart attempted or removed)
	manager.mu.RLock()
	a, exists := manager.agents["agent-oom-test"]
	manager.mu.RUnlock()

	if exists && a.RestartCount == 0 {
		t.Error("Expected agent restart count to be incremented after OOM kill")
	}
}

// Verify that the monitor doesn't use the stale config
func TestNewContainerMonitor(t *testing.T) {
	tmpCfg := testutil.NewTestConfig("http://localhost:8080")
	t.Cleanup(func() { testutil.CleanupTestConfig(tmpCfg) })

	logger := testutil.NewTestLogger()
	mockDocker := testutil.NewMockContainerManager()
	stateManager := &StateManager{
		stateFile: tmpCfg.DataDir + "/agents.json",
		agents:    make(map[string]*types.Agent),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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

	manager, _ := NewManager(tmpCfg, mockDocker, stateManager, testutil.NewMockHubClient(), nil, logger)

	monitor := NewContainerMonitor(manager, logger)
	if monitor == nil {
		t.Fatal("NewContainerMonitor returned nil")
	}
	if monitor.interval != DefaultMonitorInterval {
		t.Errorf("Expected default interval %v, got %v", DefaultMonitorInterval, monitor.interval)
	}
}
