package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/config"
	"github.com/zion-protocol/zion-node/internal/errors"
	"github.com/zion-protocol/zion-node/pkg/types"
)

const MaxRestartAttempts = 3

// Manager manages agent lifecycle
type Manager struct {
	cfg            *config.Config
	agents         map[string]*types.Agent
	mu             sync.RWMutex
	container      ContainerManager
	stateManager   *StateManager
	hubClient      HubClient
	snapshotEngine SnapshotEngine // For creating checkpoints
	logger         *logrus.Logger
}

// SnapshotEngine interface for creating and restoring snapshots
type SnapshotEngine interface {
	Create(ctx context.Context, agentID string, containerID string) (*types.SnapshotRef, error)
	Restore(ctx context.Context, agentID string, snapshotRef string, downloadURL string) error
}

// HubClient interface for reporting to Hub
type HubClient interface {
	ReportAgentFailure(ctx context.Context, agentID string, reason string) error
}

// NewManager creates a new agent manager
func NewManager(cfg *config.Config, container ContainerManager, stateManager *StateManager, hubClient HubClient, snapshotEngine SnapshotEngine, logger *logrus.Logger) (*Manager, error) {
	return &Manager{
		cfg:            cfg,
		agents:         make(map[string]*types.Agent),
		container:      container,
		stateManager:   stateManager,
		hubClient:      hubClient,
		snapshotEngine: snapshotEngine,
		logger:         logger,
	}, nil
}

// Run starts an agent. downloadURL is an optional presigned URL for snapshot restore.
func (m *Manager) Run(ctx context.Context, agentID string, profile types.RuntimeProfile, snapshotRef string, downloadURL string, extraEnv map[string]string) (*types.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check capacity
	if len(m.agents) >= m.cfg.MaxAgents {
		return nil, &errors.ErrNodeAtCapacity{
			Used:  len(m.agents),
			Total: m.cfg.MaxAgents,
		}
	}

	// Check if already running
	if _, exists := m.agents[agentID]; exists {
		return nil, &errors.ErrAgentAlreadyRunning{AgentID: agentID}
	}

	// Restore from snapshot if provided (must be done before creating container)
	if snapshotRef != "" && m.snapshotEngine != nil {
		m.logger.WithFields(logrus.Fields{"agent_id": agentID, "snapshot_ref": snapshotRef}).Info("Restoring agent from snapshot")
		if err := m.snapshotEngine.Restore(ctx, agentID, snapshotRef, downloadURL); err != nil {
			return nil, fmt.Errorf("failed to restore from snapshot: %w", err)
		}
	}

	// Create container
	m.logger.WithFields(logrus.Fields{"agent_id": agentID, "engine": profile.Engine}).Info("Creating container for agent")
	containerID, err := m.container.Create(ctx, agentID, profile, snapshotRef, extraEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	// Start container
	if err := m.container.Start(ctx, containerID); err != nil {
		// Clean up container on start failure
		if removeErr := m.container.Remove(ctx, containerID); removeErr != nil {
			m.logger.WithError(removeErr).WithField("container_id", containerID).Error("Failed to remove container after start failure")
		}
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	agent := &types.Agent{
		AgentID:      agentID,
		ContainerID:  containerID,
		Status:       types.AgentStatusRunning,
		StartedAt:    time.Now(),
		Profile:      profile,
		RestartCount: 0,
	}

	m.agents[agentID] = agent
	m.stateManager.SaveAgent(agent)

	m.logger.WithFields(logrus.Fields{"agent_id": agentID, "container_id": containerID}).Info("Agent deployed successfully")

	return agent, nil
}

// Stop stops an agent
func (m *Manager) Stop(ctx context.Context, agentID string, createCheckpoint bool) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	agent, exists := m.agents[agentID]
	if !exists {
		return "", &errors.ErrAgentNotFound{AgentID: agentID}
	}

	var checkpointRef string
	if createCheckpoint {
		// Create checkpoint before stopping
		if m.snapshotEngine != nil {
			snapshotRef, err := m.snapshotEngine.Create(ctx, agentID, agent.ContainerID)
			if err != nil {
				// Log error - checkpoint creation failed
				// Continue with stop but return error to caller
				m.logger.WithError(err).WithField("agent_id", agentID).Error("Failed to create checkpoint before stop")
				return "", fmt.Errorf("failed to create checkpoint: %w", err)
			} else if snapshotRef != nil {
				checkpointRef = snapshotRef.Ref
			}
		}
	}

	// Stop container
	if err := m.container.Stop(ctx, agent.ContainerID); err != nil {
		return "", &errors.ErrContainerOperation{
			Operation:   "stop",
			ContainerID: agent.ContainerID,
			Err:         err,
		}
	}

	// Remove container
	if err := m.container.Remove(ctx, agent.ContainerID); err != nil {
		return "", &errors.ErrContainerOperation{
			Operation:   "remove",
			ContainerID: agent.ContainerID,
			Err:         err,
		}
	}

	delete(m.agents, agentID)
	m.stateManager.RemoveAgent(agentID)

	return checkpointRef, nil
}

// GetAgent returns agent info
func (m *Manager) GetAgent(agentID string) (*types.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agent, exists := m.agents[agentID]
	if !exists {
		return nil, &errors.ErrAgentNotFound{AgentID: agentID}
	}

	return agent, nil
}

// ListAgents returns all running agents for heartbeat
func (m *Manager) ListAgents() []types.AgentInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]types.AgentInfo, 0, len(m.agents))
	for _, agent := range m.agents {
		infos = append(infos, types.AgentInfo{
			AgentID:   agent.AgentID,
			Status:    string(agent.Status),
			UptimeSec: int64(time.Since(agent.StartedAt).Seconds()),
		})
	}
	return infos
}

// GetCapacity returns current capacity info including system resource usage.
// Only the slot count requires the agent lock; CPU and memory sampling are
// done outside the lock to avoid blocking Run/Stop/HandleContainerFailure
// during the CPU sample window.
func (m *Manager) GetCapacity() types.CapacityInfo {
	// Grab slot counts under lock (fast, no I/O)
	m.mu.RLock()
	capacity := types.CapacityInfo{
		TotalSlots: m.cfg.MaxAgents,
		UsedSlots:  len(m.agents),
	}
	m.mu.RUnlock()

	// Sample CPU usage outside the lock — cpu.Percent blocks for up to 1 s,
	// and we must not hold any lock during that time.
	percents, err := cpu.Percent(time.Second, false)
	if err == nil && len(percents) > 0 {
		cores := m.cfg.SystemCPU
		if cores == 0 {
			cores, _ = cpu.Counts(true)
		}
		capacity.CPUUsedCores = float64(cores) * (percents[0] / 100.0)
	}

	// Memory sampling is fast but also done outside the lock for consistency.
	if vmem, err := mem.VirtualMemory(); err == nil {
		capacity.MemoryUsedMB = int(vmem.Used / 1024 / 1024)
	}

	return capacity
}

// HandleContainerFailure handles container failure
func (m *Manager) HandleContainerFailure(ctx context.Context, agentID string, reason string) {
	m.mu.Lock()
	agent, exists := m.agents[agentID]
	if !exists {
		m.mu.Unlock()
		return
	}

	agent.RestartCount++
	agent.LastFailure = time.Now()
	agent.FailureReason = reason
	restartCount := agent.RestartCount
	containerID := agent.ContainerID
	m.mu.Unlock()

	// Report to Hub (non-blocking)
	if m.hubClient != nil {
		go func() {
			_ = m.hubClient.ReportAgentFailure(ctx, agentID, reason)
		}()
	}

	if restartCount >= MaxRestartAttempts {
		// 3 failures, give up
		m.mu.Lock()
		agent, exists := m.agents[agentID]
		if exists && agent != nil {
			agent.Status = types.AgentStatusDead
			containerID = agent.ContainerID // Use current container ID
		}
		m.mu.Unlock()

		// Stop and remove container (best effort, only if containerID is valid)
		if containerID != "" {
			if err := m.container.Stop(ctx, containerID); err != nil {
				m.logger.WithError(err).WithField("container_id", containerID).Warn("Failed to stop dead agent container")
			}
			if err := m.container.Remove(ctx, containerID); err != nil {
				m.logger.WithError(err).WithField("container_id", containerID).Warn("Failed to remove dead agent container")
			}
		}

		m.mu.Lock()
		delete(m.agents, agentID)
		m.mu.Unlock()
		m.stateManager.RemoveAgent(agentID)
		return
	}

	// Auto restart
	go m.restartAgent(ctx, agentID)
}

func (m *Manager) restartAgent(ctx context.Context, agentID string) {
	m.mu.Lock()
	agent, exists := m.agents[agentID]
	if !exists || agent == nil {
		m.mu.Unlock()
		return
	}
	// Read ContainerID while holding lock to avoid race with concurrent restarts
	oldContainerID := agent.ContainerID
	profile := agent.Profile
	m.mu.Unlock()

	// Stop old container
	if err := m.container.Stop(ctx, oldContainerID); err != nil {
		m.logger.WithError(err).WithField("container_id", oldContainerID).Warn("Failed to stop old container during restart")
	}
	if err := m.container.Remove(ctx, oldContainerID); err != nil {
		m.logger.WithError(err).WithField("container_id", oldContainerID).Warn("Failed to remove old container during restart")
	}

	// Create new container (this will generate a new unique ID)
	containerID, err := m.container.Create(ctx, agentID, profile, "", nil)
	if err != nil {
		m.HandleContainerFailure(ctx, agentID, fmt.Sprintf("restart failed: %v", err))
		return
	}

	// Start
	if err := m.container.Start(ctx, containerID); err != nil {
		// Clean up created container on start failure
		if removeErr := m.container.Remove(ctx, containerID); removeErr != nil {
			m.logger.WithError(removeErr).WithField("container_id", containerID).Error("Failed to remove container after start failure")
		}
		m.HandleContainerFailure(ctx, agentID, fmt.Sprintf("start failed: %v", err))
		return
	}

	// Update state - check agent still exists after container creation
	m.mu.Lock()
	ag, exists := m.agents[agentID]
	if !exists {
		m.mu.Unlock()
		// Agent was deleted during restart, clean up the container we just created
		if removeErr := m.container.Remove(ctx, containerID); removeErr != nil {
			m.logger.WithError(removeErr).WithField("container_id", containerID).Error("Failed to remove container after agent deletion")
		}
		return
	}
	ag.ContainerID = containerID
	ag.Status = types.AgentStatusRunning
	ag.StartedAt = time.Now()
	agCopy := *ag // copy while holding lock to avoid race with concurrent writers
	m.mu.Unlock()
	m.stateManager.SaveAgent(&agCopy)
}

// GetContainerManager returns the container manager (for accessing runtime image info)
func (m *Manager) GetContainerManager() (ContainerManager, bool) {
	if m.container != nil {
		return m.container, true
	}
	return nil, false
}

// ShutdownState shuts down the state saver, flushing pending saves.
func (m *Manager) ShutdownState() {
	m.stateManager.Shutdown()
}

// CloseContainerManager closes the container manager, releasing resources.
func (m *Manager) CloseContainerManager() {
	if closer, ok := m.container.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}
