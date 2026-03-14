package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/config"
	"github.com/zion-protocol/zion-node/internal/utils"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// StateManager manages agent state persistence
type StateManager struct {
	stateFile string
	agents    map[string]*types.Agent
	mu        sync.RWMutex
	saver     *StateSaver
}

// NewStateManager creates a new state manager
func NewStateManager(cfg *config.Config, logger *logrus.Logger) *StateManager {
	sm := &StateManager{
		stateFile: utils.StateFilePath(cfg.DataDir),
		agents:    make(map[string]*types.Agent),
	}
	sm.saver = NewStateSaver(sm, logger)
	return sm
}

// Load loads state from file
func (s *StateManager) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.stateFile)
	if os.IsNotExist(err) {
		return nil // File doesn't exist, start from empty state
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil // Empty file, start from empty state
	}

	return json.Unmarshal(data, &s.agents)
}

// Save saves state to file
func (s *StateManager) Save() error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(s.stateFile), 0755); err != nil {
		return err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.agents, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.stateFile, data, 0644)
}

// SaveAgent saves a single agent (stores a copy to avoid shared-pointer races)
func (s *StateManager) SaveAgent(agent *types.Agent) {
	if agent == nil {
		return
	}
	agentCopy := *agent
	s.mu.Lock()
	s.agents[agent.AgentID] = &agentCopy
	s.mu.Unlock()
	s.saver.TriggerSave()
}

// RemoveAgent removes an agent
func (s *StateManager) RemoveAgent(agentID string) {
	s.mu.Lock()
	delete(s.agents, agentID)
	s.mu.Unlock()
	s.saver.TriggerSave()
}

// Shutdown gracefully stops the state saver goroutine and performs a final save.
func (s *StateManager) Shutdown() {
	if s.saver != nil {
		s.saver.Shutdown()
	}
	// Final synchronous save to ensure all state is persisted
	if err := s.Save(); err != nil {
		// Can't do much here, but at least the saver tried
		_ = err
	}
}

// GetAll returns all agents
func (s *StateManager) GetAll() map[string]*types.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*types.Agent)
	for k, v := range s.agents {
		result[k] = v
	}
	return result
}
