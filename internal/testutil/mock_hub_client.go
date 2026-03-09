package testutil

import (
	"context"
	"sync"
)

// MockHubClient implements HubClient interface for testing
type MockHubClient struct {
	mu       sync.RWMutex
	failures []AgentFailure
}

// NewMockHubClient creates a new mock Hub client
func NewMockHubClient() *MockHubClient {
	return &MockHubClient{
		failures: []AgentFailure{},
	}
}

// ReportAgentFailure implements HubClient
func (m *MockHubClient) ReportAgentFailure(ctx context.Context, agentID string, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures = append(m.failures, AgentFailure{
		AgentID: agentID,
		Reason:  reason,
	})
	return nil
}

// GetFailures returns all reported failures
func (m *MockHubClient) GetFailures() []AgentFailure {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]AgentFailure, len(m.failures))
	copy(result, m.failures)
	return result
}
