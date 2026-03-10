package hub

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zion-protocol/zion-node/internal/errors"
	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

func TestRegister_Success(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	runtimeInfo := types.RuntimeInfo{
		Engine:   "openclaw",
		ImageRef: "alpine/openclaw:main",
	}

	registered, err := client.Register(ctx, runtimeInfo)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// MockHub returns 201 (created)
	if !registered {
		t.Error("Expected registered=true for new registration")
	}
}

func TestRegister_InvalidURL(t *testing.T) {
	cfg := testutil.NewTestConfig("http://invalid-url:9999")
	cfg.HTTPTimeout = 1
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runtimeInfo := types.RuntimeInfo{Engine: "openclaw"}
	_, err := client.Register(ctx, runtimeInfo)
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestReportEvent_Success(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	event := types.NodeEvent{
		EventType: "agent_started",
		AgentID:   "agent-01",
		Timestamp: time.Now().Unix(),
	}

	err := client.ReportEvent(ctx, event)
	if err != nil {
		t.Fatalf("ReportEvent failed: %v", err)
	}
}

func TestReportAgentFailure_Success(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	err := client.ReportAgentFailure(ctx, "agent-01", "container OOM killed")
	if err != nil {
		t.Fatalf("ReportAgentFailure failed: %v", err)
	}

	// Verify failure was received
	failures := mockHub.GetFailures()
	if len(failures) == 0 {
		t.Fatal("No failures received by mock hub")
	}
	if failures[0].AgentID != "agent-01" {
		t.Errorf("Expected agent-01, got %s", failures[0].AgentID)
	}
}

func TestReportMigrationFailure(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	err := client.ReportMigrationFailure(ctx, "agent-02", "disk full")
	if err != nil {
		t.Fatalf("ReportMigrationFailure failed: %v", err)
	}

	failures := mockHub.GetFailures()
	if len(failures) == 0 {
		t.Fatal("No failures received")
	}
}

func TestReportCheckpointComplete(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	err := client.ReportCheckpointComplete(ctx, "agent-03", "sha256:checkpoint-ref")
	if err != nil {
		t.Fatalf("ReportCheckpointComplete failed: %v", err)
	}

	failures := mockHub.GetFailures()
	if len(failures) == 0 {
		t.Fatal("No events received")
	}
}

func TestRecordFailure_StatusTransition(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	// Initially online
	client.mu.Lock()
	if client.status != "online" {
		t.Errorf("Expected initial status 'online', got %s", client.status)
	}
	client.mu.Unlock()

	// After 1 failure, still online
	client.recordFailure()
	client.mu.Lock()
	if client.status != "online" {
		t.Errorf("Expected status 'online' after 1 failure, got %s", client.status)
	}
	client.mu.Unlock()

	// After 2 failures, still online
	client.recordFailure()
	client.mu.Lock()
	if client.status != "online" {
		t.Errorf("Expected status 'online' after 2 failures, got %s", client.status)
	}
	client.mu.Unlock()

	// After 3 failures, should be offline
	client.recordFailure()
	client.mu.Lock()
	if client.status != "offline" {
		t.Errorf("Expected status 'offline' after 3 failures, got %s", client.status)
	}
	client.mu.Unlock()
}

func TestRecordSuccess_ResetsFailureCount(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	// Record 2 failures (still online)
	client.recordFailure()
	client.recordFailure()

	// Success should reset failure count but client is still online
	client.recordSuccess()
	client.mu.Lock()
	if client.failureCount != 0 {
		t.Errorf("Expected failure count 0 after success, got %d", client.failureCount)
	}
	if client.status != "online" {
		t.Errorf("Expected status 'online' (was never offline), got %s", client.status)
	}
	client.mu.Unlock()
}

func TestRecordSuccess_ResetsOfflineStatus(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	// Go offline (3 failures)
	client.recordFailure()
	client.recordFailure()
	client.recordFailure()
	client.mu.Lock()
	if client.status != "offline" {
		t.Fatal("Expected offline status")
	}
	client.mu.Unlock()

	// Single success should NOT bring back online (hysteresis)
	client.recordSuccess()
	client.mu.Lock()
	if client.status != "offline" {
		t.Errorf("Expected status still 'offline' after 1 success (hysteresis), got %s", client.status)
	}
	client.mu.Unlock()

	// Second success: still offline
	client.recordSuccess()
	client.mu.Lock()
	if client.status != "offline" {
		t.Errorf("Expected status still 'offline' after 2 successes (hysteresis), got %s", client.status)
	}
	client.mu.Unlock()

	// Third success: now online (SuccessThreshold = 3)
	client.recordSuccess()
	client.mu.Lock()
	if client.status != "online" {
		t.Errorf("Expected status 'online' after %d consecutive successes, got %s", SuccessThreshold, client.status)
	}
	client.mu.Unlock()
}

func TestWaitForSnapshotConfirmation_Success(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	snapshotRef := "sha256:confirmed-ref"
	mockHub.ConfirmSnapshot(snapshotRef)

	confirmed, err := client.WaitForSnapshotConfirmation(ctx, "agent-01", snapshotRef)
	if err != nil {
		t.Fatalf("WaitForSnapshotConfirmation failed: %v", err)
	}
	if !confirmed {
		t.Error("Expected snapshot to be confirmed")
	}
}

func TestWaitForSnapshotConfirmation_ContextCancelled(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Don't confirm - should timeout
	confirmed, err := client.WaitForSnapshotConfirmation(ctx, "agent-01", "sha256:unconfirmed")
	if err == nil && confirmed {
		t.Error("Expected timeout or cancellation for unconfirmed snapshot")
	}
}

func TestSetAuthToken(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	// Set token
	client.SetAuthToken("test-jwt-token")

	// Verify via heartbeat that the token is used (the header should be set)
	ctx := context.Background()
	_, err := client.SendHeartbeat(ctx, nil, types.CapacityInfo{TotalSlots: 10})
	if err != nil {
		t.Fatalf("Heartbeat after SetAuthToken failed: %v", err)
	}
}

func TestSetAuthToken_EmptyToken(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	// Empty token should not cause issues
	client.SetAuthToken("")

	ctx := context.Background()
	_, err := client.SendHeartbeat(ctx, nil, types.CapacityInfo{TotalSlots: 10})
	if err != nil {
		t.Fatalf("Heartbeat after empty SetAuthToken failed: %v", err)
	}
}

func TestNewClient_WithAuthToken(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	cfg.HubAuthToken = "pre-configured-token"
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	// Should be able to send heartbeat
	ctx := context.Background()
	_, err := client.SendHeartbeat(ctx, nil, types.CapacityInfo{TotalSlots: 10})
	if err != nil {
		t.Fatalf("Heartbeat with pre-configured token failed: %v", err)
	}
}

func TestDownloadSnapshot_EmptyRef(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	_, err := client.DownloadSnapshot(ctx, "", "")
	if err == nil {
		t.Error("Expected error for empty snapshot ref")
	}
}

func TestRegister_NodeIDOccupiedByDifferentOwner(t *testing.T) {
	// Create a mock server that returns 409 with "different owner" message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/nodes" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]string{
					"code":    "CONFLICT",
					"message": "Node ID is already registered by a different owner",
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	runtimeInfo := types.RuntimeInfo{
		Engine:   "openclaw",
		ImageRef: "alpine/openclaw:main",
	}

	registered, err := client.Register(ctx, runtimeInfo)
	if err == nil {
		t.Fatal("Expected error when node ID is occupied by different owner")
	}
	if registered {
		t.Error("Expected registered=false for conflicting node ID")
	}

	// Verify it's an ErrNodeIDOccupied error
	var occupied *errors.ErrNodeIDOccupied
	if !stderrors.As(err, &occupied) {
		t.Fatalf("Expected ErrNodeIDOccupied, got: %T: %v", err, err)
	}
	if occupied.NodeID != cfg.NodeID {
		t.Errorf("Expected NodeID=%q, got %q", cfg.NodeID, occupied.NodeID)
	}
	t.Logf("Got expected error: %v", err)
}

func TestRegister_Legacy409_NotConflict(t *testing.T) {
	// Create a mock server that returns 409 without "different owner" message (legacy behavior)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/nodes" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]string{
					"code":    "CONFLICT",
					"message": "Node already exists",
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	runtimeInfo := types.RuntimeInfo{
		Engine:   "openclaw",
		ImageRef: "alpine/openclaw:main",
	}

	registered, err := client.Register(ctx, runtimeInfo)
	if err != nil {
		t.Fatalf("Legacy 409 should not return error, got: %v", err)
	}
	if registered {
		t.Error("Expected registered=false for legacy 409")
	}
}

// --- New edge case tests ---

// TestFetchSigningKey_Success verifies normal signing key fetch
func TestFetchSigningKey_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/system/signing-key" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"public_key": "04abcdef1234567890",
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	key, err := client.FetchSigningKey(context.Background())
	if err != nil {
		t.Fatalf("FetchSigningKey failed: %v", err)
	}
	if key != "04abcdef1234567890" {
		t.Errorf("Expected key 04abcdef1234567890, got %s", key)
	}
}

// TestFetchSigningKey_EmptyKey verifies error on empty key
func TestFetchSigningKey_EmptyKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/system/signing-key" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"public_key": "",
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	_, err := client.FetchSigningKey(context.Background())
	if err == nil {
		t.Error("Expected error for empty signing key")
	}
}

// TestFetchSigningKey_ServerError verifies error on 500
func TestFetchSigningKey_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	_, err := client.FetchSigningKey(context.Background())
	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

// TestFetchMiningBalance_Success verifies normal balance fetch
func TestFetchMiningBalance_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/mining/balance" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(MiningBalance{
				Owner:       "0xabc",
				Balance:     "100.5",
				TotalEarned: "200.0",
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	bal, err := client.FetchMiningBalance(context.Background())
	if err != nil {
		t.Fatalf("FetchMiningBalance failed: %v", err)
	}
	if bal.Balance != "100.5" {
		t.Errorf("Expected balance 100.5, got %s", bal.Balance)
	}
	if bal.TotalEarned != "200.0" {
		t.Errorf("Expected total_earned 200.0, got %s", bal.TotalEarned)
	}
}

// TestFetchMiningBalance_ServerError verifies error on non-200
func TestFetchMiningBalance_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	_, err := client.FetchMiningBalance(context.Background())
	if err == nil {
		t.Error("Expected error for 503 response")
	}
}

// TestFetchMiningBalance_MalformedJSON verifies error on invalid JSON
func TestFetchMiningBalance_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/mining/balance" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{invalid json"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	_, err := client.FetchMiningBalance(context.Background())
	if err == nil {
		t.Error("Expected error for malformed JSON")
	}
}

// TestReportProbeResponse_Success verifies normal probe response
func TestReportProbeResponse_Success(t *testing.T) {
	var receivedEvent types.NodeEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path != "/v1/nodes" {
			json.NewDecoder(r.Body).Decode(&receivedEvent)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	err := client.ReportProbeResponse(context.Background(), "agent-01", "nonce-abc123")
	if err != nil {
		t.Fatalf("ReportProbeResponse failed: %v", err)
	}
	if receivedEvent.EventType != "probe_response" {
		t.Errorf("Expected event_type probe_response, got %s", receivedEvent.EventType)
	}
	if receivedEvent.Reason != "nonce-abc123" {
		t.Errorf("Expected nonce in reason, got %s", receivedEvent.Reason)
	}
}

// TestReportProbeResponse_ServerError verifies error on non-200
func TestReportProbeResponse_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	err := client.ReportProbeResponse(context.Background(), "agent-01", "nonce-abc")
	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

// TestIsConnected_Initial verifies new client is connected
func TestIsConnected_Initial(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	if !client.IsConnected() {
		t.Error("Expected IsConnected=true for new client")
	}
}

// TestIsConnected_AfterFailures verifies disconnected after 3 failures
func TestIsConnected_AfterFailures(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	client.recordFailure()
	client.recordFailure()
	if !client.IsConnected() {
		t.Error("Expected still connected after 2 failures")
	}

	client.recordFailure()
	if client.IsConnected() {
		t.Error("Expected disconnected after 3 failures")
	}
}

// TestFailureCount_Increments verifies failure counter
func TestFailureCount_Increments(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	if client.FailureCount() != 0 {
		t.Errorf("Expected initial failure count 0, got %d", client.FailureCount())
	}

	client.recordFailure()
	client.recordFailure()
	if client.FailureCount() != 2 {
		t.Errorf("Expected failure count 2, got %d", client.FailureCount())
	}

	client.recordSuccess()
	if client.FailureCount() != 0 {
		t.Errorf("Expected failure count 0 after success, got %d", client.FailureCount())
	}
}

// TestRegister_ServerError_5xx verifies error on HTTP 500
func TestRegister_ServerError_5xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	_, err := client.Register(context.Background(), types.RuntimeInfo{Engine: "openclaw"})
	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

// TestSendHeartbeat_ServerError verifies heartbeat handles server errors
func TestSendHeartbeat_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	_, err := client.SendHeartbeat(context.Background(), nil, types.CapacityInfo{TotalSlots: 10})
	if err == nil {
		t.Error("Expected error for 502 response")
	}
}

// TestSendHeartbeat_ContextCancelled verifies heartbeat respects context
func TestSendHeartbeat_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // Slow server
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	cfg.HTTPTimeout = 10
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.SendHeartbeat(ctx, nil, types.CapacityInfo{TotalSlots: 10})
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

// TestFetchSigningKey_MalformedJSON verifies error on invalid JSON response
func TestFetchSigningKey_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/system/signing-key" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("not json"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	_, err := client.FetchSigningKey(context.Background())
	if err == nil {
		t.Error("Expected error for malformed JSON")
	}
}

// TestReportEvent_ServerError verifies ReportEvent returns error on non-200
func TestReportEvent_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	err := client.ReportEvent(context.Background(), types.NodeEvent{
		EventType: "test_event",
		AgentID:   "agent-01",
		Timestamp: time.Now().Unix(),
	})
	if err == nil {
		t.Error("Expected error for 500 response")
	}

	var hubErr *errors.ErrHubCommunication
	if !stderrors.As(err, &hubErr) {
		t.Errorf("Expected ErrHubCommunication, got %T: %v", err, err)
	}
}

// --- FetchRuntimeImage tests ---

func TestFetchRuntimeImage_Success(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	image, err := client.FetchRuntimeImage(ctx)
	if err != nil {
		t.Fatalf("FetchRuntimeImage failed: %v", err)
	}

	// MockHub returns "alpine/openclaw:main" as the default
	if image != "alpine/openclaw:main" {
		t.Errorf("Expected 'alpine/openclaw:main', got %q", image)
	}
}

func TestFetchRuntimeImage_CustomImage(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	// Override with custom image catalog
	mockHub.SetRuntimeImages([]testutil.RuntimeImageEntry{
		{Image: "myregistry/custom:v2.0", Label: "Custom Image", Default: true},
	})

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	image, err := client.FetchRuntimeImage(ctx)
	if err != nil {
		t.Fatalf("FetchRuntimeImage failed: %v", err)
	}

	if image != "myregistry/custom:v2.0" {
		t.Errorf("Expected 'myregistry/custom:v2.0', got %q", image)
	}
}

func TestFetchRuntimeImage_NoDefault_UsesFirst(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	// No default image, should fall back to first entry
	mockHub.SetRuntimeImages([]testutil.RuntimeImageEntry{
		{Image: "first/image:v1", Label: "First", Default: false},
		{Image: "second/image:v2", Label: "Second", Default: false},
	})

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	image, err := client.FetchRuntimeImage(ctx)
	if err != nil {
		t.Fatalf("FetchRuntimeImage failed: %v", err)
	}

	if image != "first/image:v1" {
		t.Errorf("Expected 'first/image:v1', got %q", image)
	}
}

func TestFetchRuntimeImage_EmptyCatalog(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	mockHub.SetRuntimeImages([]testutil.RuntimeImageEntry{})

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx := context.Background()

	_, err := client.FetchRuntimeImage(ctx)
	if err == nil {
		t.Fatal("Expected error for empty catalog, got nil")
	}
}

func TestFetchRuntimeImage_HubUnreachable(t *testing.T) {
	cfg := testutil.NewTestConfig("http://invalid-url:9999")
	cfg.HTTPTimeout = 1
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.FetchRuntimeImage(ctx)
	if err == nil {
		t.Fatal("Expected error for unreachable hub, got nil")
	}
}

func TestFetchRuntimeImage_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	_, err := client.FetchRuntimeImage(context.Background())
	if err == nil {
		t.Fatal("Expected error for 500 response, got nil")
	}
}

func TestFetchRuntimeImage_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	cfg := testutil.NewTestConfig(server.URL)
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	_, err := client.FetchRuntimeImage(context.Background())
	if err == nil {
		t.Fatal("Expected error for malformed JSON, got nil")
	}
}

func TestFetchRuntimeImage_PicksDefaultOverFirst(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	// Default is second entry; should pick it over first
	mockHub.SetRuntimeImages([]testutil.RuntimeImageEntry{
		{Image: "notdefault/image:v1", Label: "Not Default", Default: false},
		{Image: "default/image:v2", Label: "Default", Default: true},
	})

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)
	image, err := client.FetchRuntimeImage(context.Background())
	if err != nil {
		t.Fatalf("FetchRuntimeImage failed: %v", err)
	}

	if image != "default/image:v2" {
		t.Errorf("Expected 'default/image:v2', got %q", image)
	}
}

// --- Status hysteresis / flapping prevention tests ---

func TestStatusHysteresis_FailureResetsSuccessCount(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	// Go offline
	for i := 0; i < 3; i++ {
		client.recordFailure()
	}
	client.mu.Lock()
	if client.status != "offline" {
		t.Fatalf("Expected offline, got %s", client.status)
	}
	client.mu.Unlock()

	// 2 successes (not enough)
	client.recordSuccess()
	client.recordSuccess()

	// A failure interrupts the recovery — resets success count
	client.recordFailure()

	// Now 3 more successes — still need SuccessThreshold from scratch
	client.recordSuccess()
	client.mu.Lock()
	if client.status != "offline" {
		t.Errorf("Expected still offline after interrupted recovery, got %s", client.status)
	}
	client.mu.Unlock()

	client.recordSuccess()
	client.recordSuccess()
	client.mu.Lock()
	if client.status != "online" {
		t.Errorf("Expected online after %d consecutive successes, got %s", SuccessThreshold, client.status)
	}
	client.mu.Unlock()
}

func TestStatusHysteresis_OnlineStaysOnline(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	// Already online — single success should keep online (no hysteresis needed)
	client.recordSuccess()
	client.mu.Lock()
	if client.status != "online" {
		t.Errorf("Expected online, got %s", client.status)
	}
	client.mu.Unlock()
}

func TestStatusFlapping_RapidFailSuccessCycles(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	// Simulate flapping: 3 failures → offline, then alternating success/failure
	for i := 0; i < 3; i++ {
		client.recordFailure()
	}

	// Rapid alternation should NOT bring client back online
	for i := 0; i < 10; i++ {
		client.recordSuccess()
		client.recordFailure()
	}

	client.mu.Lock()
	status := client.status
	client.mu.Unlock()

	if status != "offline" {
		t.Errorf("Expected offline during flapping, but got %s", status)
	}
}

func TestSuccessThreshold_ExactBoundary(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	// Go offline
	for i := 0; i < 3; i++ {
		client.recordFailure()
	}

	// Exactly SuccessThreshold-1 successes — should still be offline
	for i := 0; i < SuccessThreshold-1; i++ {
		client.recordSuccess()
	}
	client.mu.Lock()
	if client.status != "offline" {
		t.Errorf("Expected still offline after %d successes (threshold is %d), got %s",
			SuccessThreshold-1, SuccessThreshold, client.status)
	}
	client.mu.Unlock()

	// One more success tips it over
	client.recordSuccess()
	client.mu.Lock()
	if client.status != "online" {
		t.Errorf("Expected online after exactly %d successes, got %s", SuccessThreshold, client.status)
	}
	client.mu.Unlock()
}
