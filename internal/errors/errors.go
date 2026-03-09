package errors

import "fmt"

// Error types for better error handling
type (
	// ErrAgentNotFound indicates agent not found
	ErrAgentNotFound struct {
		AgentID string
	}

	// ErrAgentAlreadyRunning indicates agent is already running
	ErrAgentAlreadyRunning struct {
		AgentID string
	}

	// ErrNodeAtCapacity indicates node is at capacity
	ErrNodeAtCapacity struct {
		Used  int
		Total int
	}

	// ErrContainerOperation indicates container operation failed
	ErrContainerOperation struct {
		Operation   string
		ContainerID string
		Err         error
	}

	// ErrSnapshotOperation indicates snapshot operation failed
	ErrSnapshotOperation struct {
		Operation string
		AgentID   string
		Err       error
	}

	// ErrHubCommunication indicates Hub communication failed
	ErrHubCommunication struct {
		Operation string
		Err       error
	}

	// ErrNodeIDOccupied indicates the node ID is already registered by a different owner
	ErrNodeIDOccupied struct {
		NodeID  string
		Message string
	}
)

func (e *ErrAgentNotFound) Error() string {
	return fmt.Sprintf("agent not found: %s", e.AgentID)
}

func (e *ErrAgentAlreadyRunning) Error() string {
	return fmt.Sprintf("agent already running: %s", e.AgentID)
}

func (e *ErrNodeAtCapacity) Error() string {
	return fmt.Sprintf("node at capacity: %d/%d", e.Used, e.Total)
}

func (e *ErrContainerOperation) Error() string {
	return fmt.Sprintf("failed to %s container %s: %v", e.Operation, e.ContainerID, e.Err)
}

func (e *ErrContainerOperation) Unwrap() error {
	return e.Err
}

func (e *ErrSnapshotOperation) Error() string {
	return fmt.Sprintf("failed to %s snapshot for agent %s: %v", e.Operation, e.AgentID, e.Err)
}

func (e *ErrSnapshotOperation) Unwrap() error {
	return e.Err
}

func (e *ErrHubCommunication) Error() string {
	return fmt.Sprintf("hub communication failed (%s): %v", e.Operation, e.Err)
}

func (e *ErrHubCommunication) Unwrap() error {
	return e.Err
}

func (e *ErrNodeIDOccupied) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("node ID %q is already occupied: %s", e.NodeID, e.Message)
	}
	return fmt.Sprintf("node ID %q is already registered by a different owner", e.NodeID)
}
