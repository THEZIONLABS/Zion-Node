package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// TestDaemonStartStop tests daemon start and stop
func TestDaemonStartStop(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start daemon
	daemonErr := make(chan error, 1)
	go func() {
		daemonErr <- d.Run(ctx)
	}()

	// Wait a bit for daemon to start
	time.Sleep(500 * time.Millisecond)

	// Stop daemon
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := d.Shutdown(shutdownCtx); err != nil {
		t.Logf("Shutdown error: %v", err)
	}

	cancel()

	// Wait for daemon to stop
	select {
	case err := <-daemonErr:
		if err != nil && err != context.Canceled {
			t.Errorf("Unexpected daemon error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Daemon did not stop in time")
	}
}

// TestDaemonProcessHubCommand tests Hub command processing
func TestDaemonProcessHubCommand(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test run command
	cmd := &types.HubCommand{
		Command: "run",
		AgentID: "test-agent-01",
		Params: map[string]interface{}{
			"runtime_engine":  "openclaw",
			"engine_version":  "v1",
			"image_hash":      "test",
			"snapshot_format": "tar.zst",
		},
	}

	d.processHubCommand(ctx, cmd)

	// Test stop command
	cmd.Command = "stop"
	cmd.Params = map[string]interface{}{}
	d.processHubCommand(ctx, cmd)

	// Test invalid command
	cmd.Command = "invalid"
	d.processHubCommand(ctx, cmd)
}

// TestDaemonProcessHubCommand_Shutdown tests that the shutdown command triggers graceful shutdown
func TestDaemonProcessHubCommand_Shutdown(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	kp := testutil.NewTestSigningKeyPair()

	cfg := testutil.NewTestConfig(mockHub.URL())
	cfg.HubPublicKey = kp.PublicKeyHex // Use real key for signature verification
	defer testutil.CleanupTestConfig(cfg)

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	// Set up a cancellable context and wire d.cancelFunc (same as Run() does)
	ctx, cancel := context.WithCancel(context.Background())
	d.cancelFunc = cancel

	cmd := &types.HubCommand{
		Command: "shutdown",
		AgentID: "",
		Params:  map[string]interface{}{},
	}
	kp.SignCommand(cmd) // Sign with the real key

	d.processHubCommand(ctx, cmd)

	// After processing shutdown, the context should be cancelled
	select {
	case <-ctx.Done():
		// expected — shutdown triggered cancel
	case <-time.After(5 * time.Second):
		t.Error("Context was not cancelled after shutdown command")
	}

	// Daemon should be in shuttingDown state
	if !d.shuttingDown.Load() {
		t.Error("Expected daemon to be in shuttingDown state")
	}
}

// TestDaemonShutdownViaHeartbeat tests the full flow: hub queues shutdown → heartbeat delivers it → daemon exits
func TestDaemonShutdownViaHeartbeat(t *testing.T) {
	kp := testutil.NewTestSigningKeyPair()

	mockHub := testutil.NewMockHub()
	mockHub.SetSigningKey(kp) // MockHub signs commands like the real hub
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	cfg.HubPublicKey = kp.PublicKeyHex // Node verifies signatures
	cfg.HeartbeatInterval = 1          // 1 second for fast test
	defer testutil.CleanupTestConfig(cfg)

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start daemon
	daemonErr := make(chan error, 1)
	go func() {
		daemonErr <- d.Run(ctx)
	}()

	// Wait for daemon to start (image pull may take time in CI)
	time.Sleep(3 * time.Second)

	// Queue a shutdown command — MockHub will sign it before returning on next heartbeat
	mockHub.SetCommand("__shutdown__", &types.HubCommand{
		Command: "shutdown",
		AgentID: "",
		Params:  map[string]interface{}{},
	})

	// Wait for the daemon to pick up shutdown via heartbeat and exit
	select {
	case err := <-daemonErr:
		if err != nil && err != context.Canceled {
			t.Errorf("Unexpected daemon error: %v", err)
		}
		// Success — daemon exited
	case <-time.After(60 * time.Second):
		cancel() // clean up
		t.Error("Daemon did not shut down after receiving shutdown command via heartbeat")
	}
}

// TestDaemonProcessHubCommandTimeout tests command timeout
func TestDaemonProcessHubCommandTimeout(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Wait for timeout
	time.Sleep(10 * time.Millisecond)

	cmd := &types.HubCommand{
		Command: "run",
		AgentID: "test-agent-01",
		Params: map[string]interface{}{
			"runtime_engine": "openclaw",
		},
	}

	// Should return early due to context cancellation
	d.processHubCommand(ctx, cmd)
}
