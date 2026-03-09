package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// TestStateManager_LoadCorruptJSON verifies Load returns error for corrupt JSON
func TestStateManager_LoadCorruptJSON(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-corrupt-*")
	defer os.RemoveAll(tmpDir)

	stateFile := filepath.Join(tmpDir, "agents.json")
	// Write invalid JSON
	if err := os.WriteFile(stateFile, []byte("{invalid json!!!}"), 0644); err != nil {
		t.Fatalf("Failed to write corrupt file: %v", err)
	}

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: stateFile,
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	err := sm.Load()
	if err == nil {
		t.Error("Expected error loading corrupt JSON, got nil")
	}
}

// TestStateManager_LoadEmptyFile verifies Load returns error for empty file
func TestStateManager_LoadEmptyFile(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-empty-*")
	defer os.RemoveAll(tmpDir)

	stateFile := filepath.Join(tmpDir, "agents.json")
	// Write empty file
	if err := os.WriteFile(stateFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write empty file: %v", err)
	}

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: stateFile,
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	err := sm.Load()
	if err == nil {
		t.Error("Expected error loading empty file, got nil")
	}
}

// TestStateManager_LoadNonExistentFile verifies Load silently succeeds for missing file
func TestStateManager_LoadNonExistentFile(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-nofile-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: filepath.Join(tmpDir, "nonexistent.json"),
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	err := sm.Load()
	if err != nil {
		t.Errorf("Expected nil error for missing file, got: %v", err)
	}

	if len(sm.agents) != 0 {
		t.Errorf("Expected empty agents map, got %d agents", len(sm.agents))
	}
}

// TestStateManager_LoadValidFile verifies Load correctly parses a valid state file
func TestStateManager_LoadValidFile(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-valid-*")
	defer os.RemoveAll(tmpDir)

	stateFile := filepath.Join(tmpDir, "agents.json")
	agents := map[string]*types.Agent{
		"agent-1": {
			AgentID:     "agent-1",
			ContainerID: "container-abc",
			Status:      types.AgentStatusRunning,
			StartedAt:   time.Now(),
		},
		"agent-2": {
			AgentID:     "agent-2",
			ContainerID: "container-def",
			Status:      types.AgentStatusDead,
		},
	}
	data, _ := json.Marshal(agents)
	if err := os.WriteFile(stateFile, data, 0644); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: stateFile,
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	if err := sm.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(sm.agents) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(sm.agents))
	}
	if sm.agents["agent-1"].ContainerID != "container-abc" {
		t.Errorf("Wrong container ID: %s", sm.agents["agent-1"].ContainerID)
	}
}

// TestStateManager_SaveAgentNil verifies SaveAgent with nil doesn't panic
func TestStateManager_SaveAgentNil(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-nil-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: filepath.Join(tmpDir, "agents.json"),
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	// Should not panic
	sm.SaveAgent(nil)

	// No agents should be added
	sm.mu.RLock()
	count := len(sm.agents)
	sm.mu.RUnlock()
	if count != 0 {
		t.Errorf("Expected 0 agents after SaveAgent(nil), got %d", count)
	}
}

// TestStateManager_SaveAgentCreatesDirectory verifies Save creates missing dirs
func TestStateManager_SaveAgentCreatesDirectory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-mkdir-*")
	defer os.RemoveAll(tmpDir)

	stateFile := filepath.Join(tmpDir, "nested", "deep", "agents.json")

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: stateFile,
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	sm.agents["test"] = &types.Agent{AgentID: "test", Status: types.AgentStatusRunning}
	if err := sm.Save(); err != nil {
		t.Fatalf("Save should create directories: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Error("State file was not created")
	}
}

// TestStateManager_SaveReadOnlyDir verifies Save returns error for read-only directory
func TestStateManager_SaveReadOnlyDir(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-ro-*")
	defer func() {
		os.Chmod(tmpDir, 0755) // Restore permissions for cleanup
		os.RemoveAll(tmpDir)
	}()

	roDir := filepath.Join(tmpDir, "readonly")
	os.MkdirAll(roDir, 0755)
	os.Chmod(roDir, 0444) // Make read-only

	stateFile := filepath.Join(roDir, "subdir", "agents.json")

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: stateFile,
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	sm.agents["test"] = &types.Agent{AgentID: "test", Status: types.AgentStatusRunning}
	err := sm.Save()
	if err == nil {
		t.Error("Expected error saving to read-only directory")
	}
}

// TestStateManager_SaveAgentCopy verifies SaveAgent stores a copy (not shared pointer)
func TestStateManager_SaveAgentCopy(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-copy-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: filepath.Join(tmpDir, "agents.json"),
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	original := &types.Agent{
		AgentID:     "test-copy",
		ContainerID: "original-container",
		Status:      types.AgentStatusRunning,
	}

	sm.SaveAgent(original)

	// Modify the original
	original.ContainerID = "modified-container"
	original.Status = types.AgentStatusDead

	// The saved version should still have the original values
	sm.mu.RLock()
	saved := sm.agents["test-copy"]
	sm.mu.RUnlock()

	if saved.ContainerID != "original-container" {
		t.Errorf("SaveAgent did not copy: container ID was modified to %s", saved.ContainerID)
	}
	if saved.Status != types.AgentStatusRunning {
		t.Errorf("SaveAgent did not copy: status was modified to %s", saved.Status)
	}
}

// TestStateManager_RemoveAgent verifies RemoveAgent deletes the agent
func TestStateManager_RemoveAgent(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-remove-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: filepath.Join(tmpDir, "agents.json"),
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	sm.agents["agent-1"] = &types.Agent{AgentID: "agent-1"}
	sm.agents["agent-2"] = &types.Agent{AgentID: "agent-2"}

	sm.RemoveAgent("agent-1")

	sm.mu.RLock()
	_, exists := sm.agents["agent-1"]
	remaining := len(sm.agents)
	sm.mu.RUnlock()

	if exists {
		t.Error("agent-1 should be removed")
	}
	if remaining != 1 {
		t.Errorf("Expected 1 remaining agent, got %d", remaining)
	}
}

// TestStateManager_RemoveNonExistentAgent verifies removing a non-existent agent is harmless
func TestStateManager_RemoveNonExistentAgent(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-remove-noexist-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: filepath.Join(tmpDir, "agents.json"),
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	sm.agents["agent-1"] = &types.Agent{AgentID: "agent-1"}

	// Should not panic
	sm.RemoveAgent("nonexistent")

	sm.mu.RLock()
	count := len(sm.agents)
	sm.mu.RUnlock()

	if count != 1 {
		t.Errorf("Expected 1 agent, got %d", count)
	}
}

// TestStateManager_GetAll verifies GetAll returns a copy
func TestStateManager_GetAll(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-getall-*")
	defer os.RemoveAll(tmpDir)

	logger := testutil.NewTestLogger()
	sm := &StateManager{
		stateFile: filepath.Join(tmpDir, "agents.json"),
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	sm.agents["agent-1"] = &types.Agent{AgentID: "agent-1"}
	sm.agents["agent-2"] = &types.Agent{AgentID: "agent-2"}

	all := sm.GetAll()
	if len(all) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(all))
	}

	// Modifying the returned map should not affect the original
	delete(all, "agent-1")
	sm.mu.RLock()
	original := len(sm.agents)
	sm.mu.RUnlock()
	if original != 2 {
		t.Error("GetAll should return a copy, not the original map")
	}
}

// TestStateManager_LoadThenSaveRoundtrip verifies Load→Save→Load roundtrip
func TestStateManager_LoadThenSaveRoundtrip(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "state-roundtrip-*")
	defer os.RemoveAll(tmpDir)

	stateFile := filepath.Join(tmpDir, "agents.json")
	logger := testutil.NewTestLogger()

	// Create and save
	sm1 := &StateManager{
		stateFile: stateFile,
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	sm1.agents["agent-rt"] = &types.Agent{
		AgentID:      "agent-rt",
		ContainerID:  "container-rt",
		Status:       types.AgentStatusRunning,
		RestartCount: 2,
	}

	if err := sm1.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load into new state manager
	sm2 := &StateManager{
		stateFile: stateFile,
		agents:    make(map[string]*types.Agent),
		saver:     &StateSaver{saveChan: make(chan struct{}, 1), logger: logger},
	}

	if err := sm2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(sm2.agents) != 1 {
		t.Fatalf("Expected 1 agent, got %d", len(sm2.agents))
	}

	agent := sm2.agents["agent-rt"]
	if agent.RestartCount != 2 {
		t.Errorf("Expected RestartCount=2, got %d", agent.RestartCount)
	}
	if agent.ContainerID != "container-rt" {
		t.Errorf("Expected ContainerID=container-rt, got %s", agent.ContainerID)
	}
}
