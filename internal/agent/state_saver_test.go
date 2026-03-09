package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

func TestStateSaver_TriggerSave(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-saver-test-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: tmpDir + "/agents.json",
		agents:    make(map[string]*types.Agent),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	saver := &StateSaver{
		stateManager: sm,
		saveChan:     make(chan struct{}, 1),
		logger:       logger,
		maxRetries:   3,
		retryDelay:   time.Millisecond,
		ctx:          ctx,
		cancel:       cancel,
	}
	go saver.saveLoop()

	// Add an agent
	sm.mu.Lock()
	sm.agents["test-agent"] = &types.Agent{
		AgentID: "test-agent",
		Status:  types.AgentStatusRunning,
	}
	sm.mu.Unlock()

	// Trigger save
	saver.TriggerSave()

	// Wait for save to complete
	time.Sleep(200 * time.Millisecond)

	// Check file was created
	data, err := os.ReadFile(tmpDir + "/agents.json")
	if err != nil {
		t.Fatalf("State file not created: %v", err)
	}

	var agents map[string]*types.Agent
	if err := json.Unmarshal(data, &agents); err != nil {
		t.Fatalf("Failed to unmarshal state: %v", err)
	}

	if _, exists := agents["test-agent"]; !exists {
		t.Error("Expected test-agent in saved state")
	}
}

func TestStateSaver_Debounce(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-saver-debounce-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()

	var saveCount int32
	sm := &StateManager{
		stateFile: tmpDir + "/agents.json",
		agents:    make(map[string]*types.Agent),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Custom saver that counts saves
	saver := &StateSaver{
		stateManager: sm,
		saveChan:     make(chan struct{}, 1),
		logger:       logger,
		maxRetries:   0,
		retryDelay:   time.Millisecond,
		ctx:          ctx,
		cancel:       cancel,
	}

	// Override saveLoop with a counting version
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-saver.saveChan:
				saver.mu.Lock()
				saver.pending = false
				saver.mu.Unlock()
				atomic.AddInt32(&saveCount, 1)
				sm.Save()
			}
		}
	}()

	// Rapidly trigger multiple saves
	for i := 0; i < 100; i++ {
		saver.TriggerSave()
	}

	time.Sleep(200 * time.Millisecond)

	// Due to channel buffering (size 1), save count should be much less than 100
	count := atomic.LoadInt32(&saveCount)
	if count >= 50 {
		t.Errorf("Expected debounced saves (much less than 100), got %d", count)
	}
	if count == 0 {
		t.Error("Expected at least one save")
	}
}

func TestStateSaver_Shutdown(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-saver-shutdown-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: tmpDir + "/agents.json",
		agents:    make(map[string]*types.Agent),
	}

	saver := NewStateSaver(sm, logger)

	// Shutdown should return within timeout
	done := make(chan struct{})
	go func() {
		saver.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(10 * time.Second):
		t.Error("Shutdown did not complete within timeout")
	}
}

func TestStateSaver_ConcurrentTriggers(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-saver-concurrent-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: tmpDir + "/agents.json",
		agents:    make(map[string]*types.Agent),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	saver := &StateSaver{
		stateManager: sm,
		saveChan:     make(chan struct{}, 1),
		logger:       logger,
		maxRetries:   1,
		retryDelay:   time.Millisecond,
		ctx:          ctx,
		cancel:       cancel,
	}
	go saver.saveLoop()

	// Concurrent triggers should not panic
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sm.mu.Lock()
			sm.agents[fmt.Sprintf("agent-%d", i)] = &types.Agent{
				AgentID: fmt.Sprintf("agent-%d", i),
				Status:  types.AgentStatusRunning,
			}
			sm.mu.Unlock()
			saver.TriggerSave()
		}(i)
	}
	wg.Wait()

	// Wait for saves to complete
	time.Sleep(500 * time.Millisecond)
}

func TestStateSaver_PendingFlag(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-saver-pending-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: tmpDir + "/agents.json",
		agents:    make(map[string]*types.Agent),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	saver := &StateSaver{
		stateManager: sm,
		saveChan:     make(chan struct{}, 1),
		logger:       logger,
		maxRetries:   1,
		retryDelay:   time.Millisecond,
		ctx:          ctx,
		cancel:       cancel,
	}

	// Before trigger, pending should be false
	saver.mu.Lock()
	if saver.pending {
		t.Error("Expected pending to be false initially")
	}
	saver.mu.Unlock()

	// After trigger, pending should be true (before save loop processes it)
	saver.TriggerSave()
	// Note: Since there's no save loop running, pending should stay true
	saver.mu.Lock()
	if !saver.pending {
		t.Error("Expected pending to be true after TriggerSave")
	}
	saver.mu.Unlock()

	// Second trigger should not change anything since already pending
	saver.TriggerSave()
	saver.mu.Lock()
	if !saver.pending {
		t.Error("Expected pending to still be true")
	}
	saver.mu.Unlock()
}

// TestStateSaver_SaveLoopContinuesAfterSuccess is a regression test for the bug
// where saveLoop used `return` instead of `break` after a successful save,
// causing the goroutine to exit permanently after the first save.
func TestStateSaver_SaveLoopContinuesAfterSuccess(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-saver-continue-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: tmpDir + "/agents.json",
		agents:    make(map[string]*types.Agent),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	saver := &StateSaver{
		stateManager: sm,
		saveChan:     make(chan struct{}, 1),
		logger:       logger,
		maxRetries:   1,
		retryDelay:   time.Millisecond,
		ctx:          ctx,
		cancel:       cancel,
	}
	go saver.saveLoop()

	// First save: add agent-1
	sm.mu.Lock()
	sm.agents["agent-1"] = &types.Agent{
		AgentID: "agent-1",
		Status:  types.AgentStatusRunning,
	}
	sm.mu.Unlock()
	saver.TriggerSave()
	time.Sleep(200 * time.Millisecond)

	// Verify first save succeeded
	data, err := os.ReadFile(tmpDir + "/agents.json")
	if err != nil {
		t.Fatalf("First save failed: %v", err)
	}
	var agents1 map[string]*types.Agent
	json.Unmarshal(data, &agents1)
	if _, ok := agents1["agent-1"]; !ok {
		t.Fatal("agent-1 not found in first save")
	}

	// Second save: add agent-2 (this would fail with the old `return` bug)
	sm.mu.Lock()
	sm.agents["agent-2"] = &types.Agent{
		AgentID: "agent-2",
		Status:  types.AgentStatusRunning,
	}
	sm.mu.Unlock()
	saver.TriggerSave()
	time.Sleep(200 * time.Millisecond)

	// Verify second save also succeeded
	data, err = os.ReadFile(tmpDir + "/agents.json")
	if err != nil {
		t.Fatalf("Second save failed: %v", err)
	}
	var agents2 map[string]*types.Agent
	json.Unmarshal(data, &agents2)
	if _, ok := agents2["agent-2"]; !ok {
		t.Error("agent-2 not found in second save — saveLoop goroutine died after first successful save (regression bug)")
	}
	if len(agents2) != 2 {
		t.Errorf("Expected 2 agents in second save, got %d", len(agents2))
	}
}

// TestStateSaver_RetryOnSaveFailure verifies the retry loop runs maxRetries
// times and logs errors on repeated failures. We test this by using a
// read-only directory that causes all retries to fail, then verifying
// that a subsequent save (after fixing permissions) still works —
// confirming the saveLoop goroutine survived the retry exhaustion.
func TestStateSaver_RetryOnSaveFailure(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-saver-retry-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()

	// State file is inside a nested dir inside a read-only parent.
	// MkdirAll will fail for the nested path while parent is 0444.
	roDir := tmpDir + "/readonly"
	os.MkdirAll(roDir, 0755)

	stateFile := roDir + "/nested/agents.json"
	sm := &StateManager{
		stateFile: stateFile,
		agents:    make(map[string]*types.Agent),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	saver := &StateSaver{
		stateManager: sm,
		saveChan:     make(chan struct{}, 1),
		logger:       logger,
		maxRetries:   2,
		retryDelay:   10 * time.Millisecond,
		ctx:          ctx,
		cancel:       cancel,
	}
	go saver.saveLoop()

	sm.mu.Lock()
	sm.agents["retry-agent"] = &types.Agent{AgentID: "retry-agent"}
	sm.mu.Unlock()

	// Make read-only so Save will fail on MkdirAll
	os.Chmod(roDir, 0444)

	saver.TriggerSave()
	time.Sleep(200 * time.Millisecond) // Wait for retries to exhaust

	// Restore write access
	os.Chmod(roDir, 0755)

	// Trigger another save — saveLoop should still be alive
	saver.TriggerSave()
	time.Sleep(200 * time.Millisecond)

	// Verify save succeeded
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("Save did not succeed after fixing permissions: %v", err)
	}
	var agents map[string]*types.Agent
	json.Unmarshal(data, &agents)
	if _, ok := agents["retry-agent"]; !ok {
		t.Error("retry-agent not found — saveLoop may have died after retries exhausted")
	}
}
