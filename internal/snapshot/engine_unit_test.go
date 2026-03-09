package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/hub"
	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// TestEngineCreateArchive tests archive creation
func TestEngineCreateArchive(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	hubClient := hub.NewClient(cfg)
	mockContainer := testutil.NewMockContainerManager()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	engine := NewEngine(cfg, hubClient, mockContainer, logger)

	// Create test source directory
	sourceDir, err := os.MkdirTemp("", "test-source-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(sourceDir)

	// Create test file
	testFile := filepath.Join(sourceDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create output path
	outputPath := filepath.Join(cfg.SnapshotCache, "test-archive.tar")

	// Test archive creation
	if err := engine.createArchive(sourceDir, outputPath); err != nil {
		t.Fatalf("Failed to create archive: %v", err)
	}

	// Verify archive was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("Archive file was not created")
	}
}

// TestEngineExtractArchive tests archive extraction
func TestEngineExtractArchive(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	hubClient := hub.NewClient(cfg)
	mockContainer := testutil.NewMockContainerManager()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	engine := NewEngine(cfg, hubClient, mockContainer, logger)

	// Create test archive first
	sourceDir, err := os.MkdirTemp("", "test-source-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(sourceDir)

	testFile := filepath.Join(sourceDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	archivePath := filepath.Join(cfg.SnapshotCache, "test-archive.tar")
	if err := engine.createArchive(sourceDir, archivePath); err != nil {
		t.Fatalf("Failed to create archive: %v", err)
	}

	// Extract archive
	targetDir, err := os.MkdirTemp("", "test-target-*")
	if err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}
	defer os.RemoveAll(targetDir)

	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("Failed to open archive: %v", err)
	}
	defer file.Close()

	if err := engine.extractArchive(file, targetDir); err != nil {
		t.Fatalf("Failed to extract archive: %v", err)
	}

	// Verify file was extracted
	extractedFile := filepath.Join(targetDir, "test.txt")
	if _, err := os.Stat(extractedFile); os.IsNotExist(err) {
		t.Error("File was not extracted")
	}
}

// TestEngineCalculateHash tests hash calculation
func TestEngineCalculateHash(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	hubClient := hub.NewClient(cfg)
	mockContainer := testutil.NewMockContainerManager()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	engine := NewEngine(cfg, hubClient, mockContainer, logger)

	// Create test file
	testFile, err := os.CreateTemp("", "test-hash-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(testFile.Name())
	defer testFile.Close()

	testFile.WriteString("test content")
	testFile.Close()

	hash, err := engine.calculateHash(testFile.Name())
	if err != nil {
		t.Fatalf("Failed to calculate hash: %v", err)
	}

	if hash == "" {
		t.Error("Hash should not be empty")
	}

	// Hash should be consistent
	hash2, err := engine.calculateHash(testFile.Name())
	if err != nil {
		t.Fatalf("Failed to calculate hash again: %v", err)
	}

	if hash != hash2 {
		t.Error("Hash should be consistent for same content")
	}
}

// TestEngineCalculateCRC32 tests CRC32 calculation
func TestEngineCalculateCRC32(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	hubClient := hub.NewClient(cfg)
	mockContainer := testutil.NewMockContainerManager()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	engine := NewEngine(cfg, hubClient, mockContainer, logger)

	// Create test file
	testFile, err := os.CreateTemp("", "test-crc32-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(testFile.Name())
	defer testFile.Close()

	testFile.WriteString("test content")
	testFile.Close()

	checksum, err := engine.calculateCRC32(testFile.Name())
	if err != nil {
		t.Fatalf("Failed to calculate CRC32: %v", err)
	}

	if checksum == "" {
		t.Error("Checksum should not be empty")
	}

	// Checksum should be consistent
	checksum2, err := engine.calculateCRC32(testFile.Name())
	if err != nil {
		t.Fatalf("Failed to calculate CRC32 again: %v", err)
	}

	if checksum != checksum2 {
		t.Error("Checksum should be consistent for same content")
	}
}

// TestEngineCreateWithPauseResume tests snapshot creation with container pause/resume
func TestEngineCreateWithPauseResume(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	hubClient := hub.NewClient(cfg)
	mockContainer := testutil.NewMockContainerManager()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	engine := NewEngine(cfg, hubClient, mockContainer, logger)

	// Create agent data directory
	agentID := "test-pause-resume"
	agentDir := filepath.Join(cfg.DataDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create agent dir: %v", err)
	}

	testFile := filepath.Join(agentDir, "state.json")
	if err := os.WriteFile(testFile, []byte(`{"test": "data"}`), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create container
	ctx := context.Background()
	containerID, err := mockContainer.Create(ctx, agentID, types.RuntimeProfile{
		Engine:         "openclaw",
		EngineVersion:  "v1",
		ImageHash:      "test",
		SnapshotFormat: "tar.zst",
	}, "", nil)
	if err != nil {
		t.Fatalf("Failed to create container: %v", err)
	}

	mockContainer.Start(ctx, containerID)

	// Create snapshot with containerID (should pause/resume)
	snapshotRef, err := engine.Create(ctx, agentID, containerID)
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	if snapshotRef == nil {
		t.Fatal("Expected snapshot ref")
	}

	// Verify container was resumed
	container := mockContainer.GetContainer(containerID)
	if container == nil {
		t.Fatal("Container should still exist")
	}
	if container.Status == "paused" {
		t.Error("Container should be resumed after snapshot")
	}
}
