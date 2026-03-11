package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// DefaultMonitorInterval is how often the monitor checks container health
	DefaultMonitorInterval = 15 * time.Second
)

// ContainerMonitor periodically checks the health of all managed containers
// and triggers HandleContainerFailure for any that have exited unexpectedly.
type ContainerMonitor struct {
	manager  *Manager
	interval time.Duration
	logger   *logrus.Logger
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewContainerMonitor creates a new container health monitor
func NewContainerMonitor(manager *Manager, logger *logrus.Logger) *ContainerMonitor {
	return &ContainerMonitor{
		manager:  manager,
		interval: DefaultMonitorInterval,
		logger:   logger,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the monitoring loop. It blocks until ctx is cancelled or Stop is called.
func (cm *ContainerMonitor) Start(ctx context.Context) {
	cm.logger.WithField("interval", cm.interval).Info("Container health monitor started")

	// Run an initial check immediately
	cm.checkAll(ctx)

	ticker := time.NewTicker(cm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cm.logger.Info("Container health monitor stopped (context cancelled)")
			return
		case <-cm.stopCh:
			cm.logger.Info("Container health monitor stopped")
			return
		case <-ticker.C:
			cm.checkAll(ctx)
		}
	}
}

// Stop stops the monitor
func (cm *ContainerMonitor) Stop() {
	cm.stopOnce.Do(func() {
		close(cm.stopCh)
	})
}

// checkAll inspects every agent's container and handles failures
func (cm *ContainerMonitor) checkAll(ctx context.Context) {
	cm.manager.mu.RLock()
	// Take a snapshot of agent IDs and container IDs to avoid holding the lock during Docker calls
	type agentSnapshot struct {
		agentID     string
		containerID string
	}
	snapshots := make([]agentSnapshot, 0, len(cm.manager.agents))
	for _, agent := range cm.manager.agents {
		snapshots = append(snapshots, agentSnapshot{
			agentID:     agent.AgentID,
			containerID: agent.ContainerID,
		})
	}
	cm.manager.mu.RUnlock()

	if len(snapshots) == 0 {
		return
	}

	for _, snap := range snapshots {
		if ctx.Err() != nil {
			return
		}

		status, err := cm.manager.container.Inspect(ctx, snap.containerID)
		if err != nil {
			// Container not found — it was removed externally
			cm.logger.WithFields(logrus.Fields{
				"agent_id":     snap.agentID,
				"container_id": snap.containerID,
			}).Warn("Container not found, treating as failed")
			cm.manager.HandleContainerFailure(ctx, snap.agentID, "container not found (removed externally)")
			continue
		}

		if !status.Running {
			reason := fmt.Sprintf("container exited with code %d", status.ExitCode)
			if status.OOMKilled {
				reason = "container OOM killed"
			}
			if status.Error != "" {
				reason = fmt.Sprintf("%s: %s", reason, status.Error)
			}

			// Retrieve container logs before removal for diagnostics
			containerLogs := ""
			if logs, err := cm.manager.container.Logs(ctx, snap.containerID, 50); err == nil && logs != "" {
				containerLogs = logs
			}

			fields := logrus.Fields{
				"agent_id":     snap.agentID,
				"container_id": snap.containerID,
				"exit_code":    status.ExitCode,
				"oom_killed":   status.OOMKilled,
				"finished_at":  status.FinishedAt,
			}
			cm.logger.WithFields(fields).Warn("Detected dead container, triggering failure handler")

			if containerLogs != "" {
				cm.logger.WithFields(logrus.Fields{
					"agent_id": snap.agentID,
				}).Errorf("Container logs:\n%s", containerLogs)
				reason = fmt.Sprintf("%s | container_logs: %s", reason, containerLogs)
			}

			cm.manager.HandleContainerFailure(ctx, snap.agentID, reason)
		}
	}
}
