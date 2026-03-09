package errors

import (
	"fmt"
	"strings"
	"testing"
)

func TestErrAgentNotFound_Error(t *testing.T) {
	err := &ErrAgentNotFound{AgentID: "agent-123"}
	msg := err.Error()
	if !strings.Contains(msg, "agent-123") {
		t.Errorf("Error message should contain agent ID, got: %s", msg)
	}
	if !strings.Contains(msg, "not found") {
		t.Errorf("Error message should mention 'not found', got: %s", msg)
	}
}

func TestErrAgentAlreadyRunning_Error(t *testing.T) {
	err := &ErrAgentAlreadyRunning{AgentID: "agent-456"}
	msg := err.Error()
	if !strings.Contains(msg, "agent-456") {
		t.Errorf("Error message should contain agent ID, got: %s", msg)
	}
	if !strings.Contains(msg, "already running") {
		t.Errorf("Error message should mention 'already running', got: %s", msg)
	}
}

func TestErrNodeAtCapacity_Error(t *testing.T) {
	err := &ErrNodeAtCapacity{Used: 10, Total: 10}
	msg := err.Error()
	if !strings.Contains(msg, "10/10") {
		t.Errorf("Error message should contain capacity info, got: %s", msg)
	}
	if !strings.Contains(msg, "capacity") {
		t.Errorf("Error message should mention 'capacity', got: %s", msg)
	}
}

func TestErrContainerOperation_Error(t *testing.T) {
	inner := fmt.Errorf("connection refused")
	err := &ErrContainerOperation{
		Operation:   "start",
		ContainerID: "container-abc",
		Err:         inner,
	}
	msg := err.Error()
	if !strings.Contains(msg, "start") {
		t.Errorf("Error message should contain operation, got: %s", msg)
	}
	if !strings.Contains(msg, "container-abc") {
		t.Errorf("Error message should contain container ID, got: %s", msg)
	}
	if !strings.Contains(msg, "connection refused") {
		t.Errorf("Error message should contain inner error, got: %s", msg)
	}
}

func TestErrContainerOperation_Unwrap(t *testing.T) {
	inner := fmt.Errorf("connection refused")
	err := &ErrContainerOperation{
		Operation:   "start",
		ContainerID: "container-abc",
		Err:         inner,
	}
	if err.Unwrap() != inner {
		t.Error("Unwrap should return inner error")
	}
}

func TestErrSnapshotOperation_Error(t *testing.T) {
	inner := fmt.Errorf("disk full")
	err := &ErrSnapshotOperation{
		Operation: "upload",
		AgentID:   "agent-789",
		Err:       inner,
	}
	msg := err.Error()
	if !strings.Contains(msg, "upload") {
		t.Errorf("Error message should contain operation, got: %s", msg)
	}
	if !strings.Contains(msg, "agent-789") {
		t.Errorf("Error message should contain agent ID, got: %s", msg)
	}
	if !strings.Contains(msg, "disk full") {
		t.Errorf("Error message should contain inner error, got: %s", msg)
	}
}

func TestErrSnapshotOperation_Unwrap(t *testing.T) {
	inner := fmt.Errorf("disk full")
	err := &ErrSnapshotOperation{
		Operation: "upload",
		AgentID:   "agent-789",
		Err:       inner,
	}
	if err.Unwrap() != inner {
		t.Error("Unwrap should return inner error")
	}
}

func TestErrHubCommunication_Error(t *testing.T) {
	inner := fmt.Errorf("timeout")
	err := &ErrHubCommunication{
		Operation: "heartbeat",
		Err:       inner,
	}
	msg := err.Error()
	if !strings.Contains(msg, "heartbeat") {
		t.Errorf("Error message should contain operation, got: %s", msg)
	}
	if !strings.Contains(msg, "timeout") {
		t.Errorf("Error message should contain inner error, got: %s", msg)
	}
}

func TestErrHubCommunication_Unwrap(t *testing.T) {
	inner := fmt.Errorf("timeout")
	err := &ErrHubCommunication{
		Operation: "heartbeat",
		Err:       inner,
	}
	if err.Unwrap() != inner {
		t.Error("Unwrap should return inner error")
	}
}

// Test that errors implement the error interface
func TestErrorInterface(t *testing.T) {
	var _ error = &ErrAgentNotFound{}
	var _ error = &ErrAgentAlreadyRunning{}
	var _ error = &ErrNodeAtCapacity{}
	var _ error = &ErrContainerOperation{}
	var _ error = &ErrSnapshotOperation{}
	var _ error = &ErrHubCommunication{}
}
