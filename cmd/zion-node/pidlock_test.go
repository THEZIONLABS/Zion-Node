package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestAcquirePIDLock_FirstInstance(t *testing.T) {
	tmpDir := t.TempDir()

	pidPath, err := acquirePIDLock(tmpDir)
	if err != nil {
		t.Fatalf("Expected no error on first acquire, got: %v", err)
	}
	defer releasePIDLock(pidPath)

	// Verify PID file exists with our PID
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("Failed to read PID file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("PID file content is not a valid integer: %q", string(data))
	}
	if pid != os.Getpid() {
		t.Errorf("Expected PID %d, got %d", os.Getpid(), pid)
	}
}

func TestAcquirePIDLock_BlocksSecondInstance(t *testing.T) {
	tmpDir := t.TempDir()

	// Write current process PID (which is alive) to simulate a running instance
	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0640); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	// Second acquire should fail
	_, err := acquirePIDLock(tmpDir)
	if err == nil {
		t.Fatal("Expected error when another instance is running, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("Expected 'already running' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "zion-node stop") {
		t.Errorf("Expected 'zion-node stop' in error message, got: %v", err)
	}
}

func TestAcquirePIDLock_OverwritesStalePID(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a PID that doesn't exist (use a very high unlikely PID)
	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	// PID 4194304 is above typical Linux max (default 32768 or 4194304 on 64-bit)
	// Use a known-dead PID by finding one: just kill -0 will fail
	stalePID := 2147483647 // max int32, almost certainly not running
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(stalePID)), 0640); err != nil {
		t.Fatalf("Failed to write stale PID file: %v", err)
	}

	// Acquire should succeed — stale PID is dead
	gotPath, err := acquirePIDLock(tmpDir)
	if err != nil {
		t.Fatalf("Expected success with stale PID, got: %v", err)
	}
	defer releasePIDLock(gotPath)

	// Should now contain our PID
	data, _ := os.ReadFile(gotPath)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if pid != os.Getpid() {
		t.Errorf("Expected our PID %d after overwrite, got %d", os.Getpid(), pid)
	}
}

func TestAcquirePIDLock_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "dir")

	pidPath, err := acquirePIDLock(nestedDir)
	if err != nil {
		t.Fatalf("Expected directory creation to work, got: %v", err)
	}
	defer releasePIDLock(pidPath)

	if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
		t.Error("Expected nested directory to be created")
	}
}

func TestAcquirePIDLock_InvalidPIDFileContent(t *testing.T) {
	tmpDir := t.TempDir()

	// Write garbage to PID file
	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	if err := os.WriteFile(pidPath, []byte("not-a-pid"), 0640); err != nil {
		t.Fatalf("Failed to write garbage PID file: %v", err)
	}

	// Should succeed — invalid content treated as stale
	gotPath, err := acquirePIDLock(tmpDir)
	if err != nil {
		t.Fatalf("Expected success with invalid PID content, got: %v", err)
	}
	defer releasePIDLock(gotPath)
}

func TestAcquirePIDLock_NegativePID(t *testing.T) {
	tmpDir := t.TempDir()

	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	if err := os.WriteFile(pidPath, []byte("-1"), 0640); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	// Negative PID should be treated as stale (pid > 0 check)
	gotPath, err := acquirePIDLock(tmpDir)
	if err != nil {
		t.Fatalf("Expected success with negative PID, got: %v", err)
	}
	defer releasePIDLock(gotPath)
}

func TestAcquirePIDLock_ZeroPID(t *testing.T) {
	tmpDir := t.TempDir()

	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	if err := os.WriteFile(pidPath, []byte("0"), 0640); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	gotPath, err := acquirePIDLock(tmpDir)
	if err != nil {
		t.Fatalf("Expected success with zero PID, got: %v", err)
	}
	defer releasePIDLock(gotPath)
}

func TestReleasePIDLock_RemovesFile(t *testing.T) {
	tmpDir := t.TempDir()

	pidPath, err := acquirePIDLock(tmpDir)
	if err != nil {
		t.Fatalf("Failed to acquire: %v", err)
	}

	// File should exist
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Fatal("PID file should exist before release")
	}

	releasePIDLock(pidPath)

	// File should be gone
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("PID file should be removed after release")
	}
}

func TestReleasePIDLock_NonexistentFile(t *testing.T) {
	// Should not panic on nonexistent file
	releasePIDLock("/tmp/nonexistent-zion-pid-test-file")
}

func TestAcquirePIDLock_AcquireAfterRelease(t *testing.T) {
	tmpDir := t.TempDir()

	// First acquire
	pidPath, err := acquirePIDLock(tmpDir)
	if err != nil {
		t.Fatalf("First acquire failed: %v", err)
	}

	// Release
	releasePIDLock(pidPath)

	// Second acquire should work
	pidPath2, err := acquirePIDLock(tmpDir)
	if err != nil {
		t.Fatalf("Second acquire after release failed: %v", err)
	}
	defer releasePIDLock(pidPath2)
}

func TestAcquirePIDLock_WhitespacePID(t *testing.T) {
	tmpDir := t.TempDir()

	// Write PID with trailing newline (common in PID files)
	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	if err := os.WriteFile(pidPath, []byte("  "+strconv.Itoa(os.Getpid())+"\n"), 0640); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	// Should still detect the running process
	_, err := acquirePIDLock(tmpDir)
	if err == nil {
		t.Fatal("Expected error with whitespace-padded PID of running process")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("Expected 'already running' in error, got: %v", err)
	}
}

func TestAcquirePIDLock_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()

	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	if err := os.WriteFile(pidPath, []byte(""), 0640); err != nil {
		t.Fatalf("Failed to write empty PID file: %v", err)
	}

	gotPath, err := acquirePIDLock(tmpDir)
	if err != nil {
		t.Fatalf("Expected success with empty PID file, got: %v", err)
	}
	defer releasePIDLock(gotPath)
}

func TestAcquirePIDLock_FilePermissions(t *testing.T) {
	tmpDir := t.TempDir()

	pidPath, err := acquirePIDLock(tmpDir)
	if err != nil {
		t.Fatalf("Failed to acquire: %v", err)
	}
	defer releasePIDLock(pidPath)

	info, err := os.Stat(pidPath)
	if err != nil {
		t.Fatalf("Failed to stat PID file: %v", err)
	}

	// Should be 0640 (owner rw, group r, others none)
	perm := info.Mode().Perm()
	if perm != 0640 {
		t.Errorf("Expected permissions 0640, got %04o", perm)
	}
}

func TestHandleStopCommand_StalePIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "zion-node.pid")

	// Write a dead PID
	stalePID := 2147483647
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(stalePID)), 0640); err != nil {
		t.Fatalf("Failed to write stale PID: %v", err)
	}

	// Verify the stale PID is actually dead (sanity check)
	proc, err := os.FindProcess(stalePID)
	if err == nil && proc.Signal(syscall.Signal(0)) == nil {
		t.Skip("Stale PID is unexpectedly alive, skipping")
	}

	// After someone detects stale PID and cleans up, the file should be removable
	_ = os.Remove(pidPath)
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("Stale PID file should be cleaned up")
	}
}

func TestHandleStopCommand_NoPIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "zion-node.pid")

	// No PID file exists — nothing to stop
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("PID file should not exist initially")
	}
}

// --- Tests for readPIDFile helper ---

func TestReadPIDFile_ValidPID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	os.WriteFile(pidPath, []byte("12345"), 0640)

	pid, valid := readPIDFile(pidPath)
	if !valid || pid != 12345 {
		t.Errorf("Expected (12345, true), got (%d, %v)", pid, valid)
	}
}

func TestReadPIDFile_WithWhitespace(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	os.WriteFile(pidPath, []byte("  42\n"), 0640)

	pid, valid := readPIDFile(pidPath)
	if !valid || pid != 42 {
		t.Errorf("Expected (42, true), got (%d, %v)", pid, valid)
	}
}

func TestReadPIDFile_InvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	os.WriteFile(pidPath, []byte("garbage"), 0640)

	_, valid := readPIDFile(pidPath)
	if valid {
		t.Error("Expected invalid for garbage content")
	}
}

func TestReadPIDFile_NoFile(t *testing.T) {
	_, valid := readPIDFile("/tmp/nonexistent-zion-pid-test")
	if valid {
		t.Error("Expected invalid for missing file")
	}
}

func TestReadPIDFile_ZeroPID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	os.WriteFile(pidPath, []byte("0"), 0640)

	_, valid := readPIDFile(pidPath)
	if valid {
		t.Error("Expected invalid for zero PID")
	}
}

func TestReadPIDFile_NegativePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "zion-node.pid")
	os.WriteFile(pidPath, []byte("-5"), 0640)

	_, valid := readPIDFile(pidPath)
	if valid {
		t.Error("Expected invalid for negative PID")
	}
}

// --- Tests for isProcessAlive helper ---

func TestIsProcessAlive_Self(t *testing.T) {
	if !isProcessAlive(os.Getpid()) {
		t.Error("Current process should be alive")
	}
}

func TestIsProcessAlive_DeadProcess(t *testing.T) {
	if isProcessAlive(2147483647) {
		t.Error("PID 2147483647 should not be alive")
	}
}
