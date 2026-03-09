package testutil

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/zion-protocol/zion-node/pkg/types"
)

var ErrContainerNotFound = errors.New("container not found")

// MockContainerManager implements ContainerManager for testing
type MockContainerManager struct {
	mu               sync.RWMutex
	containers       map[string]*MockContainer
	images           map[string]bool
	stats            map[string]*types.ContainerStats
	createError      error
	startError       error
	stopError        error
	ensureImageError error
	listError        error
	removeError      error
	counter          int64 // Counter for unique container IDs
}

// MockContainer represents a mock container
type MockContainer struct {
	ID      string
	Name    string
	Status  string
	Running bool
}

// NewMockContainerManager creates a new mock container manager
func NewMockContainerManager() *MockContainerManager {
	return &MockContainerManager{
		containers: make(map[string]*MockContainer),
		images:     make(map[string]bool),
		stats:      make(map[string]*types.ContainerStats),
	}
}

// SetImage sets whether an image exists
func (m *MockContainerManager) SetImage(image string, exists bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.images[image] = exists
}

// SetContainerStats sets stats for a container
func (m *MockContainerManager) SetContainerStats(containerID string, stats *types.ContainerStats) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats[containerID] = stats
}

// SetCreateError sets error for Create
func (m *MockContainerManager) SetCreateError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createError = err
}

// SetStartError sets error for Start
func (m *MockContainerManager) SetStartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startError = err
}

// SetStopError sets error for Stop
func (m *MockContainerManager) SetStopError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopError = err
}

// SetEnsureImageError sets error for EnsureImage (image pull failure)
func (m *MockContainerManager) SetEnsureImageError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureImageError = err
}

// SetListError sets error for List
func (m *MockContainerManager) SetListError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listError = err
}

// SetRemoveError sets error for Remove
func (m *MockContainerManager) SetRemoveError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeError = err
}

// GetContainer returns a container
func (m *MockContainerManager) GetContainer(containerID string) *MockContainer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.containers[containerID]
}

// ListContainers returns all containers
func (m *MockContainerManager) ListContainers() map[string]*MockContainer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*MockContainer)
	for k, v := range m.containers {
		result[k] = v
	}
	return result
}

// EnsureImage implements ContainerManager
func (m *MockContainerManager) EnsureImage(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.ensureImageError != nil {
		return m.ensureImageError
	}
	return nil
}

// GetImageDigest implements ContainerManager
func (m *MockContainerManager) GetImageDigest(ctx context.Context) (string, error) {
	return "sha256:mock-digest-for-testing", nil
}

// Create implements ContainerManager
func (m *MockContainerManager) Create(ctx context.Context, agentID string, profile types.RuntimeProfile, snapshotRef string, extraEnv map[string]string) (string, error) {
	if m.createError != nil {
		return "", m.createError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate unique container ID each time (for restart scenarios)
	// Use counter + timestamp to ensure uniqueness
	m.counter++
	containerID := fmt.Sprintf("mock-container-%s-%d-%d", agentID, time.Now().UnixNano(), m.counter)
	m.containers[containerID] = &MockContainer{
		ID:      containerID,
		Name:    "zion-agent-" + agentID,
		Status:  "created",
		Running: false,
	}

	return containerID, nil
}

// Start implements ContainerManager
func (m *MockContainerManager) Start(ctx context.Context, containerID string) error {
	if m.startError != nil {
		return m.startError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	container, ok := m.containers[containerID]
	if !ok {
		return ErrContainerNotFound
	}

	container.Running = true
	container.Status = "running"
	return nil
}

// Stop implements ContainerManager
func (m *MockContainerManager) Stop(ctx context.Context, containerID string) error {
	if m.stopError != nil {
		return m.stopError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	container, ok := m.containers[containerID]
	if !ok {
		return ErrContainerNotFound
	}

	container.Running = false
	container.Status = "stopped"
	return nil
}

// Remove implements ContainerManager
func (m *MockContainerManager) Remove(ctx context.Context, containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.removeError != nil {
		return m.removeError
	}
	delete(m.containers, containerID)
	return nil
}

// List implements ContainerManager
func (m *MockContainerManager) List(ctx context.Context, prefix string) ([]dockertypes.Container, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.listError != nil {
		return nil, m.listError
	}

	var result []dockertypes.Container
	for _, c := range m.containers {
		if prefix == "" || len(c.Name) >= len(prefix) && c.Name[:len(prefix)] == prefix {
			result = append(result, dockertypes.Container{
				ID:     c.ID,
				Names:  []string{c.Name},
				Status: c.Status,
			})
		}
	}
	return result, nil
}

// Inspect implements ContainerManager
func (m *MockContainerManager) Inspect(ctx context.Context, containerID string) (*types.ContainerStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	c, exists := m.containers[containerID]
	if !exists {
		return nil, ErrContainerNotFound
	}

	return &types.ContainerStatus{
		Running:  c.Running,
		ExitCode: 0,
	}, nil
}

// Stats implements ContainerManager
func (m *MockContainerManager) Stats(ctx context.Context, containerID string) (*types.ContainerStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats, ok := m.stats[containerID]
	if !ok {
		return &types.ContainerStats{
			CPUPercent:   0.0,
			MemoryMB:     0,
			OOMKilled:    false,
			CPUThrottled: 0.0,
		}, nil
	}

	return stats, nil
}

// SimulateContainerCrash simulates a container crash
// This sets the container status to "exited" and stops it
// Note: In real implementation, restartAgent will Stop and Remove the old container,
// then create a new one. This mock just marks it as crashed.
func (m *MockContainerManager) SimulateContainerCrash(containerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	container, ok := m.containers[containerID]
	if ok {
		container.Running = false
		container.Status = "exited"
	}
}

// SimulateOOM simulates OOM kill
func (m *MockContainerManager) SimulateOOM(containerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats := m.stats[containerID]
	if stats == nil {
		stats = &types.ContainerStats{}
		m.stats[containerID] = stats
	}
	stats.OOMKilled = true
}

// Pause implements ContainerManager
func (m *MockContainerManager) Pause(ctx context.Context, containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	container, ok := m.containers[containerID]
	if !ok {
		return ErrContainerNotFound
	}

	container.Status = "paused"
	return nil
}

// Resume implements ContainerManager
func (m *MockContainerManager) Resume(ctx context.Context, containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	container, ok := m.containers[containerID]
	if !ok {
		return ErrContainerNotFound
	}

	if container.Status == "paused" {
		container.Status = "running"
		container.Running = true
	}
	return nil
}
