package agent

import (
	"context"
	"strings"

	"github.com/sirupsen/logrus"
)

// RecoverFromDocker recovers agent state from Docker containers
func (m *Manager) RecoverFromDocker(ctx context.Context) error {
	// Query all zion-agent-* containers
	containers, err := m.container.List(ctx, "zion-agent-")
	if err != nil {
		return err
	}

	// Load local state
	if err := m.stateManager.Load(); err != nil {
		return err
	}

	// Compare
	localAgents := m.stateManager.GetAll()
	dockerContainers := make(map[string]string) // agentID -> containerID

	for _, c := range containers {
		if len(c.Names) > 0 {
			agentID := extractAgentID(c.Names[0]) // Extract from container name
			if agentID != "" {
				dockerContainers[agentID] = c.ID
			}
		}
	}

	// Handle inconsistencies
	for agentID, containerID := range dockerContainers {
		if _, exists := localAgents[agentID]; !exists {
			// Docker has but local doesn't: orphaned, stop and clean up
			if m.logger != nil {
				m.logger.WithFields(logrus.Fields{
					"container_id": containerID,
					"agent_id":     agentID,
				}).Warn("Found orphaned container, cleaning up")
			}
			
			// Try to stop (ignore errors - container might already be stopped)
			if err := m.container.Stop(ctx, containerID); err != nil {
				if m.logger != nil {
					m.logger.WithError(err).WithField("container_id", containerID).Debug("Stop failed (container may already be stopped)")
				}
			}
			
			// Force remove the container (must succeed)
			if err := m.container.Remove(ctx, containerID); err != nil {
				if m.logger != nil {
					m.logger.WithError(err).WithFields(logrus.Fields{
						"container_id": containerID,
						"agent_id":     agentID,
					}).Error("Failed to remove orphaned container - this will cause conflicts")
				}
				// Don't fail the whole recovery, but log the error prominently
			} else {
				if m.logger != nil {
					m.logger.WithField("container_id", containerID).Info("Successfully removed orphaned container")
				}
			}
		}
	}

	// Restore local state to memory
	m.mu.Lock()
	for agentID, agent := range localAgents {
		// Verify container is still running
		if containerID, exists := dockerContainers[agentID]; exists {
			agent.ContainerID = containerID
			m.agents[agentID] = agent
		} else {
			// Container doesn't exist, remove from state file
			m.stateManager.RemoveAgent(agentID)
		}
	}
	m.mu.Unlock()

	return nil
}

// extractAgentID extracts agent ID from container name
// Format: zion-agent-{agentID} or /zion-agent-{agentID}
func extractAgentID(containerName string) string {
	// Docker API returns names with leading "/"
	containerName = strings.TrimPrefix(containerName, "/")
	
	prefix := "zion-agent-"
	if strings.HasPrefix(containerName, prefix) {
		return strings.TrimPrefix(containerName, prefix)
	}
	return ""
}
