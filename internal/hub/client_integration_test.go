package hub

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// TestHeartbeat tests heartbeat sending and command reception
func TestHeartbeat(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	ctx := context.Background()
	agents := []types.AgentInfo{
		{
			AgentID:   "agent-01",
			Status:    "running",
			UptimeSec: 100,
		},
	}
	capacity := types.CapacityInfo{
		TotalSlots: 10,
		UsedSlots:  1,
	}

	// Send heartbeat
	cmd, err := client.SendHeartbeat(ctx, agents, capacity)
	if err != nil {
		t.Fatalf("Failed to send heartbeat: %v", err)
	}

	// Verify heartbeat was received
	heartbeats := mockHub.GetHeartbeats()
	if len(heartbeats) == 0 {
		t.Fatal("No heartbeats received")
	}

	hb := heartbeats[0]
	if len(hb.Agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(hb.Agents))
	}
	if hb.Capacity.UsedSlots != 1 {
		t.Errorf("Expected 1 used slot, got %d", hb.Capacity.UsedSlots)
	}

	// Test command reception
	hubCmd := &types.HubCommand{
		Command: "stop",
		AgentID: "agent-01",
	}
	mockHub.SetCommand("agent-01", hubCmd)

	cmd, err = client.SendHeartbeat(ctx, agents, capacity)
	if err != nil {
		t.Fatalf("Failed to send heartbeat: %v", err)
	}

	if len(cmd) == 0 {
		t.Fatal("Expected command from Hub")
	}
	if cmd[0].Command != "stop" {
		t.Errorf("Expected command 'stop', got %s", cmd[0].Command)
	}
}

// TestHeartbeatFailureHandling tests heartbeat failure handling
func TestHeartbeatFailureHandling(t *testing.T) {
	// Use invalid URL to simulate failure
	cfg := testutil.NewTestConfig("http://invalid-url:9999")
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	agents := []types.AgentInfo{}
	capacity := types.CapacityInfo{TotalSlots: 10, UsedSlots: 0}

	// Send heartbeat (should fail)
	_, err := client.SendHeartbeat(ctx, agents, capacity)
	if err == nil {
		t.Error("Expected error for invalid Hub URL")
	}

	// Verify client continues to work (doesn't crash)
	if client == nil {
		t.Fatal("Client should still exist after failure")
	}
}

// TestSnapshotUpload tests snapshot upload
func TestSnapshotUpload(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	ctx := context.Background()
	agentID := "agent-01"
	snapshotRef := "sha256:abc123"
	snapshotData := []byte("test snapshot data")

	uri, err := client.UploadSnapshot(ctx, agentID, snapshotRef,
		bytes.NewReader(snapshotData), int64(len(snapshotData)))
	if err != nil {
		t.Fatalf("Failed to upload snapshot: %v", err)
	}

	if uri == "" {
		t.Error("Expected non-empty URI")
	}

	// Verify upload was received (would need to check mockHub internals)
	// For now, just verify no error
}

// TestSnapshotDownload tests snapshot download
func TestSnapshotDownload(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	client := NewClient(cfg)

	snapshotRef := "sha256:abc123"
	snapshotData := []byte("test snapshot data")
	mockHub.SetSnapshotData(snapshotRef, snapshotData)

	ctx := context.Background()
	reader, err := client.DownloadSnapshot(ctx, snapshotRef, "")
	if err != nil {
		t.Fatalf("Failed to download snapshot: %v", err)
	}
	defer reader.Close()

	// Read data
	downloaded := make([]byte, len(snapshotData))
	n, err := reader.Read(downloaded)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read snapshot: %v", err)
	}

	if n != len(snapshotData) {
		t.Errorf("Expected %d bytes, got %d", len(snapshotData), n)
	}
}
