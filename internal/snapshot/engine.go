package snapshot

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/config"
	"github.com/zion-protocol/zion-node/internal/errors"
	"github.com/zion-protocol/zion-node/internal/hub"
	"github.com/zion-protocol/zion-node/internal/utils"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// Engine manages snapshot operations
type Engine struct {
	cfg              *config.Config
	hubClient        *hub.Client
	containerManager ContainerManager
	logger           *logrus.Logger
}

// ContainerManager interface for pausing/resuming containers
type ContainerManager interface {
	Pause(ctx context.Context, containerID string) error
	Resume(ctx context.Context, containerID string) error
}

// NewEngine creates a new snapshot engine
func NewEngine(cfg *config.Config, hubClient *hub.Client, containerManager ContainerManager, logger *logrus.Logger) *Engine {
	return &Engine{
		cfg:              cfg,
		hubClient:        hubClient,
		containerManager: containerManager,
		logger:           logger,
	}
}

// Create creates a snapshot of agent state
func (e *Engine) Create(ctx context.Context, agentID string, containerID string) (*types.SnapshotRef, error) {
	// 1. Pause agent (SIGSTOP)
	if e.containerManager != nil && containerID != "" {
		if err := e.containerManager.Pause(ctx, containerID); err != nil {
			return nil, &errors.ErrSnapshotOperation{
				Operation: "pause",
				AgentID:   agentID,
				Err:       err,
			}
		}
		defer func() {
			// Resume on error or success
			if e.containerManager != nil && containerID != "" {
				if err := e.containerManager.Resume(ctx, containerID); err != nil {
					// Log resume failure - container may remain paused
					// This is a critical error but we can't return error from defer
					if e.logger != nil {
						e.logger.WithFields(logrus.Fields{
							"agent_id":     agentID,
							"container_id": containerID,
							"error":        err,
						}).Error("CRITICAL: Failed to resume container after snapshot, container may remain paused")
					}
				}
			}
		}()
	}

	// 2. Collect agent state
	agentDir := utils.AgentDataDir(e.cfg.DataDir, agentID)

	// 3. Create snapshot archive
	snapshotPath := utils.SnapshotPath(e.cfg.SnapshotCache, agentID)
	if err := e.createArchive(agentDir, snapshotPath); err != nil {
		return nil, &errors.ErrSnapshotOperation{
			Operation: "create_archive",
			AgentID:   agentID,
			Err:       err,
		}
	}

	// 4. Calculate SHA-256 hash (content-addressed reference)
	hash, err := e.calculateHash(snapshotPath)
	if err != nil {
		return nil, &errors.ErrSnapshotOperation{
			Operation: "calculate_hash",
			AgentID:   agentID,
			Err:       err,
		}
	}
	snapshotRef := fmt.Sprintf("sha256:%s", hash)

	// 5. Calculate CRC32 checksum (transfer verification)
	checksum, err := e.calculateCRC32(snapshotPath)
	if err != nil {
		return nil, &errors.ErrSnapshotOperation{
			Operation: "calculate_checksum",
			AgentID:   agentID,
			Err:       err,
		}
	}

	// 6. Get file size
	stat, err := os.Stat(snapshotPath)
	if err != nil {
		return nil, err
	}

	// 7. Upload to Hub (Hub will upload to S3)
	file, err := os.Open(snapshotPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Upload through Hub API
	uri, err := e.hubClient.UploadSnapshot(ctx, agentID, snapshotRef, file, stat.Size())
	if err != nil {
		// Clean up local file on upload failure
		os.Remove(snapshotPath)
		return nil, &errors.ErrSnapshotOperation{
			Operation: "upload",
			AgentID:   agentID,
			Err:       err,
		}
	}

	// 8. Resume agent (SIGCONT) - handled by defer

	// Clean up local file after successful upload (optional, can keep for cache)
	// os.Remove(snapshotPath)

	return &types.SnapshotRef{
		Ref:       snapshotRef,
		URI:       uri,
		Size:      stat.Size(),
		CreatedAt: time.Now(),
		Checksum:  fmt.Sprintf("crc32:%s", checksum),
	}, nil
}

// Restore restores agent from snapshot. If downloadURL is provided (presigned
// URL from Hub command), it is used directly instead of calling the Hub API.
func (e *Engine) Restore(ctx context.Context, agentID string, snapshotRef string, downloadURL string) error {
	// 1. Download snapshot (use direct URL if provided, otherwise via Hub API)
	reader, err := e.hubClient.DownloadSnapshot(ctx, snapshotRef, downloadURL)
	if err != nil {
		return &errors.ErrSnapshotOperation{
			Operation: "download",
			AgentID:   agentID,
			Err:       err,
		}
	}
	defer reader.Close()

	// 2. Verify hash matches snapshotRef
	// Download to temporary file first for hash verification
	// Ensure cache directory exists
	if err := os.MkdirAll(e.cfg.SnapshotCache, 0755); err != nil {
		return &errors.ErrSnapshotOperation{
			Operation: "create_cache_dir",
			AgentID:   agentID,
			Err:       err,
		}
	}
	tmpFile, err := os.CreateTemp(e.cfg.SnapshotCache, "restore-*.tmp")
	if err != nil {
		return &errors.ErrSnapshotOperation{
			Operation: "create_temp",
			AgentID:   agentID,
			Err:       err,
		}
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // Clean up temp file

	// Copy downloaded data to temp file
	if _, err := io.Copy(tmpFile, reader); err != nil {
		tmpFile.Close()
		return &errors.ErrSnapshotOperation{
			Operation: "copy_temp",
			AgentID:   agentID,
			Err:       err,
		}
	}
	tmpFile.Close()

	// Calculate hash of downloaded file
	actualHash, err := e.calculateHash(tmpPath)
	if err != nil {
		return &errors.ErrSnapshotOperation{
			Operation: "calculate_hash",
			AgentID:   agentID,
			Err:       err,
		}
	}

	// Extract expected hash from snapshotRef (format: sha256:abc123...)
	expectedHash := ""
	if strings.HasPrefix(snapshotRef, "sha256:") {
		expectedHash = strings.TrimPrefix(snapshotRef, "sha256:")
	} else {
		return &errors.ErrSnapshotOperation{
			Operation: "verify_hash",
			AgentID:   agentID,
			Err:       fmt.Errorf("invalid snapshot ref format"),
		}
	}

	// Verify hash matches
	if actualHash != expectedHash {
		return &errors.ErrSnapshotOperation{
			Operation: "verify_hash",
			AgentID:   agentID,
			Err:       fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash),
		}
	}

	// 3. Extract to agent directory (read from temp file)
	tmpFile, err = os.Open(tmpPath)
	if err != nil {
		return &errors.ErrSnapshotOperation{
			Operation: "open_temp",
			AgentID:   agentID,
			Err:       err,
		}
	}
	defer tmpFile.Close()
	agentDir := utils.AgentDataDir(e.cfg.DataDir, agentID)
	// Clear existing directory to avoid conflicts
	if err := os.RemoveAll(agentDir); err != nil {
		return &errors.ErrSnapshotOperation{
			Operation: "clear_dir",
			AgentID:   agentID,
			Err:       err,
		}
	}
	if err := e.extractArchive(tmpFile, agentDir); err != nil {
		return &errors.ErrSnapshotOperation{
			Operation: "extract",
			AgentID:   agentID,
			Err:       err,
		}
	}

	return nil
}

func (e *Engine) createArchive(sourceDir string, outputPath string) error {
	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	// Create temporary file first, then rename on success
	tmpPath := outputPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	// Track if we should clean up temp file
	shouldCleanup := true
	defer func() {
		file.Close()
		if shouldCleanup {
			os.Remove(tmpPath)
		}
	}()

	// Create tar writer
	tw := tar.NewWriter(file)
	defer tw.Close()

	// Walk source directory and add files to archive
	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories (we'll create them when extracting)
		if info.IsDir() {
			return nil
		}

		// Get relative path from source directory
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		// Write header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// Write file content
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Close file and tar writer before renaming
	if err := tw.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	// Rename temp file to final path (atomic operation)
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return err
	}

	// Success - don't clean up temp file
	shouldCleanup = false
	return nil
}

func (e *Engine) extractArchive(reader io.Reader, targetDir string) error {
	// Ensure target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	// Create tar reader
	tr := tar.NewReader(reader)

	// Extract files
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Get target path
		targetPath := filepath.Join(targetDir, header.Name)

		// Create parent directories if needed
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}

		// Handle different file types
		switch header.Typeflag {
		case tar.TypeReg:
			// Regular file
			file, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tr); err != nil {
				file.Close()
				return err
			}
			file.Close()

			// Set file permissions
			if err := os.Chmod(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeDir:
			// Directory
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *Engine) calculateHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (e *Engine) calculateCRC32(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := crc32.NewIEEE()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%08x", hasher.Sum32()), nil
}
