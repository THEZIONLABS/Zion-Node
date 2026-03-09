package daemon

import (
	"time"

	"github.com/zion-protocol/zion-node/pkg/types"
)

// NodeStatus is a read-only snapshot of the current node state.
// TUI polls this every 1–2 seconds via Daemon.Status().
type NodeStatus struct {
	// Identity
	NodeID        string
	WalletAddress string
	HubURL        string
	Version       string

	// Runtime state
	Uptime          time.Duration
	HubConnected    bool
	HubFailureCount int
	LastHeartbeat   time.Time

	// Resources
	Capacity       types.CapacityInfo
	SystemCPU      int
	SystemMemoryMB int

	// Mining reward
	Reward string

	// Agents
	Agents []types.AgentInfo
}

// Status returns a point-in-time snapshot of the node.
// Safe to call from any goroutine.
func (d *Daemon) Status() NodeStatus {
	agents := d.agentManager.ListAgents()
	capacity := d.agentManager.GetCapacity()

	return NodeStatus{
		NodeID:          d.cfg.NodeID,
		WalletAddress:   d.cfg.OperatorAddress,
		HubURL:          d.cfg.HubURL,
		Version:         Version,
		Uptime:          time.Since(d.startTime),
		HubConnected:    d.hubClient.IsConnected(),
		HubFailureCount: d.hubClient.FailureCount(),
		LastHeartbeat:   d.lastHeartbeatAt,
		Capacity:        capacity,
		SystemCPU:       d.cfg.SystemCPU,
		SystemMemoryMB:  d.cfg.SystemMemoryMB,
		Agents:          agents,
		Reward:          d.miningReward,
	}
}
