package snapshot

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/hub"
	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// TestSnapshotCreate tests snapshot creation
func TestSnapshotCreate(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	// Create agent data directory
	agentID := "test-agent-01"
	agentDir := filepath.Join(cfg.DataDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create agent dir: %v", err)
	}

	// Create test data file
	testFile := filepath.Join(agentDir, "state.json")
	if err := os.WriteFile(testFile, []byte(`{"test": "data"}`), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	hubClient := hub.NewClient(cfg)
	mockContainer := testutil.NewMockContainerManager()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	engine := NewEngine(cfg, hubClient, mockContainer, logger)

	ctx := context.Background()
	// Test snapshot creation without containerID (empty string is valid - will skip pause/resume)
	// This tests the case where snapshot is created without a running container
	snapshotRef, err := engine.Create(ctx, agentID, "")
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Also test with containerID (if container exists)
	// Create agent data directory for second test
	agentID2 := agentID + "-with-container"
	agentDir2 := filepath.Join(cfg.DataDir, "agents", agentID2)
	if err := os.MkdirAll(agentDir2, 0755); err != nil {
		t.Fatalf("Failed to create agent dir: %v", err)
	}
	// Create test data file
	testFile2 := filepath.Join(agentDir2, "state.json")
	if err := os.WriteFile(testFile2, []byte(`{"test": "data2"}`), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create a mock container for testing pause/resume
	containerID, err := mockContainer.Create(ctx, agentID2, types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}, "", nil)
	if err != nil {
		t.Fatalf("Failed to create mock container: %v", err)
	}
	mockContainer.Start(ctx, containerID)

	// Test snapshot creation with containerID (should pause/resume)
	snapshotRef2, err := engine.Create(ctx, agentID2, containerID)
	if err != nil {
		t.Fatalf("Failed to create snapshot with container: %v", err)
	}
	if snapshotRef2 == nil {
		t.Fatal("Expected snapshot ref for container snapshot")
	}

	if snapshotRef == nil {
		t.Fatal("Expected snapshot ref")
	}

	if snapshotRef.Ref == "" {
		t.Error("Expected non-empty snapshot ref")
	}

	if snapshotRef.URI == "" {
		t.Error("Expected non-empty URI")
	}
}

// TestSnapshotRestore tests snapshot restore
func TestSnapshotRestore(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	// Create a simple tar archive for testing
	var tarData bytes.Buffer
	tw := tar.NewWriter(&tarData)
	header := &tar.Header{
		Name: "test.txt",
		Size: int64(len("test content")),
		Mode: 0644,
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatalf("Failed to write tar header: %v", err)
	}
	if _, err := tw.Write([]byte("test content")); err != nil {
		t.Fatalf("Failed to write tar data: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}

	// Calculate real hash for the tar data
	hasher := sha256.New()
	hasher.Write(tarData.Bytes())
	hash := hex.EncodeToString(hasher.Sum(nil))
	snapshotRef := fmt.Sprintf("sha256:%s", hash)

	mockHub.SetSnapshotData(snapshotRef, tarData.Bytes())

	hubClient := hub.NewClient(cfg)
	mockContainer := testutil.NewMockContainerManager()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	engine := NewEngine(cfg, hubClient, mockContainer, logger)

	ctx := context.Background()
	agentID := "test-agent-01"
	if err := engine.Restore(ctx, agentID, snapshotRef, ""); err != nil {
		t.Fatalf("Failed to restore snapshot: %v", err)
	}

	// Verify agent directory was created
	agentDir := filepath.Join(cfg.DataDir, "agents", agentID)
	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		t.Error("Agent directory should be created after restore")
	}
}
