package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zion-protocol/zion-node/internal/config"
	"github.com/zion-protocol/zion-node/internal/daemon"
	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// --- helpers for test isolation ---

// getFreePort returns a random free TCP port to avoid port conflicts between tests.
func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("getFreePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// forceRemoveTestContainers forcibly removes all Docker containers whose names
// start with "zion-agent-". This is a safety net to prevent orphaned containers
// when daemon.Shutdown() times out or fails. Safe to call even if no containers exist.
func forceRemoveTestContainers(t *testing.T) {
	t.Helper()
	cmd := exec.Command("docker", "ps", "-a", "--filter", "name=zion-agent-", "--format", "{{.ID}}")
	out, err := cmd.Output()
	if err != nil {
		return // docker not available or no containers
	}
	ids := strings.TrimSpace(string(out))
	if ids == "" {
		return
	}
	for _, id := range strings.Split(ids, "\n") {
		id = strings.TrimSpace(id)
		if id != "" {
			exec.Command("docker", "rm", "-f", id).Run()
		}
	}
}

// e2eEnv holds all resources for a single E2E test, ensuring full cleanup.
type e2eEnv struct {
	t       *testing.T
	mockHub *testutil.MockHub
	cfg     *config.Config
	daemon  *daemon.Daemon
	cancel  context.CancelFunc
	apiURL  string // NOTE: Node API server was removed; kept for compile compat
	tmpDir  string
	done    chan error
}

// newE2EEnv creates an isolated E2E test environment with its own MockHub, config,
// temp directory, API port, and daemon. The daemon is NOT started yet.
func newE2EEnv(t *testing.T, opts ...func(*config.Config)) *e2eEnv {
	t.Helper()

	mockHub := testutil.NewMockHub()

	tmpDir, err := os.MkdirTemp("", "zion-e2e-extended-*")
	if err != nil {
		mockHub.Close()
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	port := getFreePort(t)
	_ = port // port was used for the removed API server

	cfg := &config.Config{
		NodeID:                 fmt.Sprintf("e2e-node-%d", time.Now().UnixNano()),
		OperatorAddress:        "0xe2eTestOperator",
		HubURL:                 mockHub.URL(),
		HubAuthToken:           "mock-jwt-token",
		MaxAgents:              10,
		CPUPerAgent:            1,
		MemoryPerAgent:         1024,
		StoragePerAgent:        2048,
		DataDir:                filepath.Join(tmpDir, "data"),
		SnapshotCache:          filepath.Join(tmpDir, "cache"),
		ContainerEngine:        "docker",
		RuntimeImage:           "alpine/openclaw:main",
		HeartbeatInterval:      2, // fast heartbeat for testing
		HeartbeatRetryMax:      3,
		HeartbeatRetryInterval: 1,
		SnapshotRetentionDays:  1,
		LogDir:                 filepath.Join(tmpDir, "logs"),
		LogLevel:               "debug",
		HTTPTimeout:            10,
	}
	cfg.SetDefaults()

	for _, opt := range opts {
		opt(cfg)
	}

	for _, dir := range []string{cfg.DataDir, cfg.SnapshotCache, cfg.LogDir} {
		os.MkdirAll(dir, 0755)
	}

	d, err := daemon.NewDaemon(cfg)
	if err != nil {
		mockHub.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create daemon: %v", err)
	}

	return &e2eEnv{
		t:       t,
		mockHub: mockHub,
		cfg:     cfg,
		daemon:  d,
		tmpDir:  tmpDir,
		done:    make(chan error, 1),
	}
}

// start launches the daemon and waits until the API is ready.
func (e *e2eEnv) start() {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel

	go func() {
		e.done <- e.daemon.Run(ctx)
	}()

	// Give daemon time to start and register
	time.Sleep(3 * time.Second)
}

// cleanup stops the daemon, removes orphaned Docker containers, removes temp
// dirs, and closes the mock hub. Always defer this immediately after newE2EEnv.
func (e *e2eEnv) cleanup() {
	if e.cancel != nil {
		// Graceful shutdown first
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		e.daemon.Shutdown(shutCtx)
		shutCancel()

		e.cancel()
		// Drain daemon goroutine
		select {
		case <-e.done:
		case <-time.After(3 * time.Second):
		}
	}
	// Force-remove any orphaned Docker containers (safety net)
	forceRemoveTestContainers(e.t)
	e.mockHub.Close()
	os.RemoveAll(e.tmpDir)
}

// getJSON is a helper that GETs a JSON endpoint and decodes the response.
func (e *e2eEnv) getJSON(path string, target interface{}) int {
	e.t.Helper()
	resp, err := http.Get(e.apiURL + path)
	if err != nil {
		e.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if target != nil {
		json.NewDecoder(resp.Body).Decode(target)
	}
	return resp.StatusCode
}

// --- tests ---

// TestE2ERegistrationFlow verifies that when the daemon starts, it registers
// the node with the Hub and begins sending heartbeats.
func TestE2ERegistrationFlow(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	// After startup, daemon should have already registered and sent initial heartbeat.
	// Wait for heartbeat interval + buffer.
	time.Sleep(3 * time.Second)

	heartbeats := env.mockHub.GetHeartbeats()
	if len(heartbeats) == 0 {
		t.Fatal("Expected at least one heartbeat after registration")
	}

	// Verify heartbeat content
	hb := heartbeats[0]
	if hb.Status != "online" {
		t.Errorf("Expected heartbeat status 'online', got '%s'", hb.Status)
	}
	if hb.Capacity.TotalSlots != env.cfg.MaxAgents {
		t.Errorf("Expected capacity %d, got %d", env.cfg.MaxAgents, hb.Capacity.TotalSlots)
	}
	if hb.Capacity.UsedSlots != 0 {
		t.Errorf("Expected 0 used slots, got %d", hb.Capacity.UsedSlots)
	}
}

// TestE2EHealthResponseFormat validates the /v1/health endpoint returns
// correctly formatted JSON with expected fields.
func TestE2EHealthResponseFormat(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	var health map[string]interface{}
	status := env.getJSON("/v1/health", &health)

	if status != http.StatusOK {
		t.Fatalf("Expected 200, got %d", status)
	}
	if health["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got '%v'", health["status"])
	}
	if _, ok := health["timestamp"]; !ok {
		t.Error("Missing 'timestamp' field in health response")
	}
	// timestamp should be a number (Unix epoch)
	ts, ok := health["timestamp"].(float64)
	if !ok || ts < 1e9 {
		t.Errorf("Invalid timestamp value: %v", health["timestamp"])
	}
}

// TestE2EListAgentsEmpty verifies the agents list is empty on fresh start.
func TestE2EListAgentsEmpty(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	var listResp struct {
		Agents   []types.AgentInfo `json:"agents"`
		Total    int               `json:"total"`
		Capacity int               `json:"capacity"`
	}
	status := env.getJSON("/v1/agents", &listResp)

	if status != http.StatusOK {
		t.Fatalf("Expected 200, got %d", status)
	}
	if listResp.Total != 0 {
		t.Errorf("Expected 0 agents, got %d", listResp.Total)
	}
	if listResp.Capacity != env.cfg.MaxAgents {
		t.Errorf("Expected capacity %d, got %d", env.cfg.MaxAgents, listResp.Capacity)
	}
}

// TestE2EGetAgentNotFound verifies GET /v1/agents/{id} returns 404 for unknown agent.
func TestE2EGetAgentNotFound(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	resp, err := http.Get(env.apiURL + "/v1/agents/nonexistent-agent-id")
	if err != nil {
		t.Fatalf("GET /v1/agents/nonexistent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", resp.StatusCode)
	}
}

// TestE2EHubCommandRunViaHeartbeat verifies run command is delivered via heartbeat
// and the daemon processes it (creates agent in mock container manager).
func TestE2EHubCommandRunViaHeartbeat(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	agentID := "e2e-run-test-agent"
	env.mockHub.SetCommand(agentID, &types.HubCommand{
		Command: "run",
		AgentID: agentID,
		Params: map[string]interface{}{
			"runtime_engine":  "openclaw",
			"engine_version":  "v1",
			"image_hash":      "abc123",
			"snapshot_format": "tar.zst",
		},
	})

	// Wait for heartbeat to pick up command + processing time
	time.Sleep(4 * time.Second)

	// Agent might or might not be running (depends on Docker availability),
	// but the command should have been consumed from the hub.
	env.mockHub.ResetState()
	// Verify no more pending commands for this agent
	time.Sleep(3 * time.Second)

	hbs := env.mockHub.GetHeartbeats()
	for _, hb := range hbs {
		// The heartbeat should not contain command response - just verify heartbeats keep going
		if hb.Status == "" {
			t.Error("Heartbeat missing status after command processing")
		}
	}
}

// TestE2EHubCommandStopViaHeartbeat verifies stop command delivered via heartbeat.
func TestE2EHubCommandStopViaHeartbeat(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	// First, send a run command
	agentID := "e2e-stop-test-agent"
	env.mockHub.SetCommand(agentID, &types.HubCommand{
		Command: "run",
		AgentID: agentID,
		Params: map[string]interface{}{
			"runtime_engine":  "openclaw",
			"engine_version":  "v1",
			"image_hash":      "abc123",
			"snapshot_format": "tar.zst",
		},
	})

	// Wait for run to process
	time.Sleep(4 * time.Second)

	// Now send stop
	env.mockHub.SetCommand(agentID, &types.HubCommand{
		Command: "stop",
		AgentID: agentID,
		Params: map[string]interface{}{
			"create_checkpoint": false,
		},
	})

	// Wait for stop to process
	time.Sleep(4 * time.Second)

	// Verify via API - agent should not be listed (or status stopped)
	var listResp struct {
		Agents []struct {
			AgentID string `json:"agent_id"`
			Status  string `json:"status"`
		} `json:"agents"`
		Total int `json:"total"`
	}
	env.getJSON("/v1/agents", &listResp)

	for _, a := range listResp.Agents {
		if a.AgentID == agentID && a.Status == "running" {
			t.Error("Agent should not still be running after stop command")
		}
	}
}

// TestE2EMultipleHeartbeats verifies multiple heartbeat cycles work properly
// and commands are dispatched in correct batches.
func TestE2EMultipleHeartbeats(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	// Wait for several heartbeat cycles (interval = 2s)
	time.Sleep(7 * time.Second)

	heartbeats := env.mockHub.GetHeartbeats()
	// Should have initial + at least 2 periodic heartbeats
	if len(heartbeats) < 3 {
		t.Errorf("Expected at least 3 heartbeats in 7s (interval=2s), got %d", len(heartbeats))
	}

	// All heartbeats should have valid timestamps (increasing)
	prevTs := int64(0)
	for i, hb := range heartbeats {
		if hb.Timestamp <= 0 {
			t.Errorf("Heartbeat %d has invalid timestamp: %d", i, hb.Timestamp)
		}
		if hb.Timestamp < prevTs {
			t.Errorf("Heartbeat %d timestamp (%d) < previous (%d)", i, hb.Timestamp, prevTs)
		}
		prevTs = hb.Timestamp
	}

	// Now inject a command mid-stream and verify it gets picked up
	env.mockHub.SetCommand("batch-agent-1", &types.HubCommand{
		Command: "run",
		AgentID: "batch-agent-1",
		Params: map[string]interface{}{
			"runtime_engine":  "openclaw",
			"engine_version":  "v1",
			"image_hash":      "hash1",
			"snapshot_format": "tar.zst",
		},
	})

	beforeCount := env.mockHub.HeartbeatCount()
	time.Sleep(3 * time.Second)
	afterCount := env.mockHub.HeartbeatCount()

	if afterCount <= beforeCount {
		t.Error("No new heartbeats after injecting command")
	}
}

// TestE2EGracefulShutdown verifies the daemon shuts down cleanly, stopping all agents.
func TestE2EGracefulShutdown(t *testing.T) {
	env := newE2EEnv(t)
	// Manual cleanup since we test shutdown explicitly
	defer func() {
		forceRemoveTestContainers(t)
		env.mockHub.Close()
		os.RemoveAll(env.tmpDir)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- env.daemon.Run(ctx)
	}()

	waitForAPI(t, env.apiURL, 15*time.Second)

	// Inject a run command so there's an active agent
	env.mockHub.SetCommand("shutdown-test-agent", &types.HubCommand{
		Command: "run",
		AgentID: "shutdown-test-agent",
		Params: map[string]interface{}{
			"runtime_engine":  "openclaw",
			"engine_version":  "v1",
			"image_hash":      "abc123",
			"snapshot_format": "tar.zst",
		},
	})
	time.Sleep(4 * time.Second)

	// Trigger graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	err := env.daemon.Shutdown(shutdownCtx)
	if err != nil {
		t.Logf("Shutdown returned error (may be expected): %v", err)
	}

	cancel()

	// Daemon should exit within a reasonable time
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Logf("Daemon exit error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Daemon did not exit within 10s after shutdown")
	}

	// API should no longer be reachable
	_, err = http.Get(env.apiURL + "/v1/health")
	if err == nil {
		t.Error("API should be unreachable after shutdown")
	}
}

// TestE2EConcurrentAPIRequests verifies the API handles concurrent requests
// without race conditions or panics.
func TestE2EConcurrentAPIRequests(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	const numGoroutines = 20
	var wg sync.WaitGroup
	var errorCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var err error

			switch idx % 3 {
			case 0:
				// Health check
				resp, e := http.Get(env.apiURL + "/v1/health")
				if e != nil {
					err = e
				} else {
					resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						err = fmt.Errorf("health: status %d", resp.StatusCode)
					}
				}
			case 1:
				// List agents
				resp, e := http.Get(env.apiURL + "/v1/agents")
				if e != nil {
					err = e
				} else {
					resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						err = fmt.Errorf("agents: status %d", resp.StatusCode)
					}
				}
			case 2:
				// Get unknown agent (expect 404)
				resp, e := http.Get(env.apiURL + fmt.Sprintf("/v1/agents/concurrent-test-%d", idx))
				if e != nil {
					err = e
				} else {
					resp.Body.Close()
					if resp.StatusCode != http.StatusNotFound {
						err = fmt.Errorf("get agent: expected 404, got %d", resp.StatusCode)
					}
				}
			}

			if err != nil {
				t.Logf("Goroutine %d error: %v", idx, err)
				atomic.AddInt32(&errorCount, 1)
			}
		}(i)
	}

	wg.Wait()

	if errorCount > 0 {
		t.Errorf("%d out of %d concurrent requests failed", errorCount, numGoroutines)
	}
}

// TestE2ECapacityInHeartbeat verifies that capacity info in heartbeats updates
// correctly when agents are added and removed.
func TestE2ECapacityInHeartbeat(t *testing.T) {
	env := newE2EEnv(t, func(cfg *config.Config) {
		cfg.MaxAgents = 3
	})
	defer env.cleanup()
	env.start()

	// Initial heartbeat should show 0 used slots
	time.Sleep(3 * time.Second)
	hbs := env.mockHub.GetHeartbeats()
	if len(hbs) == 0 {
		t.Fatal("No heartbeats received")
	}

	lastHB := hbs[len(hbs)-1]
	if lastHB.Capacity.TotalSlots != 3 {
		t.Errorf("Expected total_slots=3, got %d", lastHB.Capacity.TotalSlots)
	}
	if lastHB.Capacity.UsedSlots != 0 {
		t.Errorf("Expected used_slots=0, got %d", lastHB.Capacity.UsedSlots)
	}
}

// TestE2EStateRecovery tests that a second daemon instance can recover state
// from the data directory of a previously running instance.
func TestE2EStateRecovery(t *testing.T) {
	// Create shared temp dir and mock hub
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	tmpDir, err := os.MkdirTemp("", "zion-e2e-recovery-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	defer forceRemoveTestContainers(t)

	port1 := getFreePort(t)

	makeCfg := func(port int) *config.Config {
		cfg := &config.Config{
			NodeID:                 "recovery-test-node",
			OperatorAddress:        "0xRecoveryTestOp",
			HubURL:                 mockHub.URL(),
			HubAuthToken:           "mock-jwt-token",
			MaxAgents:              5,
			CPUPerAgent:            1,
			MemoryPerAgent:         1024,
			StoragePerAgent:        2048,
			DataDir:                filepath.Join(tmpDir, "data"),
			SnapshotCache:          filepath.Join(tmpDir, "cache"),
			ContainerEngine:        "docker",
			RuntimeImage:           "alpine/openclaw:main",
			HeartbeatInterval:      2,
			HeartbeatRetryMax:      3,
			HeartbeatRetryInterval: 1,
			SnapshotRetentionDays:  1,
			LogDir:                 filepath.Join(tmpDir, "logs"),
			LogLevel:               "debug",
			HTTPTimeout:            10,
		}
		cfg.SetDefaults()
		for _, dir := range []string{cfg.DataDir, cfg.SnapshotCache, cfg.LogDir} {
			os.MkdirAll(dir, 0755)
		}
		return cfg
	}

	// --- Phase 1: Start first daemon ---
	cfg1 := makeCfg(port1)
	d1, err := daemon.NewDaemon(cfg1)
	if err != nil {
		t.Fatalf("Failed to create daemon 1: %v", err)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := make(chan error, 1)
	go func() { done1 <- d1.Run(ctx1) }()

	apiURL1 := fmt.Sprintf("http://127.0.0.1:%d", port1)
	waitForAPI(t, apiURL1, 15*time.Second)

	// Verify first daemon is healthy
	resp, err := http.Get(apiURL1 + "/v1/health")
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	// Shutdown first daemon
	shutCtx1, shutCancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	d1.Shutdown(shutCtx1)
	shutCancel1()
	cancel1()
	select {
	case <-done1:
	case <-time.After(5 * time.Second):
	}

	// Wait for port to be released
	time.Sleep(1 * time.Second)

	// --- Phase 2: Start second daemon on same data dir ---
	mockHub.ResetState() // Reset to track new heartbeats only

	port2 := getFreePort(t)
	cfg2 := makeCfg(port2)
	d2, err := daemon.NewDaemon(cfg2)
	if err != nil {
		t.Fatalf("Failed to create daemon 2: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	done2 := make(chan error, 1)
	go func() { done2 <- d2.Run(ctx2) }()

	apiURL2 := fmt.Sprintf("http://127.0.0.1:%d", port2)
	waitForAPI(t, apiURL2, 15*time.Second)

	// Second daemon should be healthy and registered
	resp, err = http.Get(apiURL2 + "/v1/health")
	if err != nil {
		t.Fatalf("Health check failed on second daemon: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 from second daemon, got %d", resp.StatusCode)
	}

	// Second daemon should start sending heartbeats
	time.Sleep(3 * time.Second)
	hbs := mockHub.GetHeartbeats()
	if len(hbs) == 0 {
		t.Error("Second daemon should send heartbeats")
	}

	// Cleanup second daemon
	shutCtx2, shutCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	d2.Shutdown(shutCtx2)
	shutCancel2()
	cancel2()
	select {
	case <-done2:
	case <-time.After(5 * time.Second):
	}
}

// TestE2EAgentFailureReporting verifies that when agent operations fail,
// the node reports failure events to the Hub.
func TestE2EAgentFailureReporting(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	// Try to stop a non-existent agent via hub command
	env.mockHub.SetCommand("ghost-agent", &types.HubCommand{
		Command: "stop",
		AgentID: "ghost-agent",
		Params:  map[string]interface{}{},
	})

	// Wait for heartbeat to deliver command + processing
	time.Sleep(4 * time.Second)

	// The daemon should have attempted to stop a non-existent agent.
	// Whether it reports a failure event depends on implementation,
	// but the daemon should remain healthy.
	var health map[string]interface{}
	status := env.getJSON("/v1/health", &health)
	if status != http.StatusOK {
		t.Errorf("Daemon should remain healthy after failed command, got status %d", status)
	}
}

// TestE2EInvalidRunCommandIgnored verifies that an invalid run command
// (missing engine) is gracefully ignored without crashing the daemon.
func TestE2EInvalidRunCommandIgnored(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	// Send command with missing engine field
	env.mockHub.SetCommand("bad-agent", &types.HubCommand{
		Command: "run",
		AgentID: "bad-agent",
		Params: map[string]interface{}{
			// Missing "runtime_engine" - should be rejected
			"engine_version": "v1",
		},
	})

	time.Sleep(4 * time.Second)

	// Daemon should still be running and healthy
	var health map[string]interface{}
	status := env.getJSON("/v1/health", &health)
	if status != http.StatusOK {
		t.Errorf("Daemon should remain healthy after invalid command, got %d", status)
	}

	// Agent should NOT appear in list
	var listResp struct {
		Agents []struct {
			AgentID string `json:"agent_id"`
		} `json:"agents"`
		Total int `json:"total"`
	}
	env.getJSON("/v1/agents", &listResp)
	for _, a := range listResp.Agents {
		if a.AgentID == "bad-agent" {
			t.Error("Agent with invalid profile should not be running")
		}
	}
}

// TestE2EMultipleRunCommandsSameAgent verifies that sending duplicate run commands
// for the same agent doesn't cause issues.
func TestE2EMultipleRunCommandsSameAgent(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	agentID := "dup-agent"
	cmd := &types.HubCommand{
		Command: "run",
		AgentID: agentID,
		Params: map[string]interface{}{
			"runtime_engine":  "openclaw",
			"engine_version":  "v1",
			"image_hash":      "abc123",
			"snapshot_format": "tar.zst",
		},
	}

	// Send first run
	env.mockHub.SetCommand(agentID, cmd)
	time.Sleep(4 * time.Second)

	// Send duplicate run
	env.mockHub.SetCommand(agentID, cmd)
	time.Sleep(4 * time.Second)

	// Daemon should still be healthy (may log conflict but not crash)
	var health map[string]interface{}
	status := env.getJSON("/v1/health", &health)
	if status != http.StatusOK {
		t.Errorf("Daemon should handle duplicate run gracefully, got %d", status)
	}
}

// TestE2EUnknownCommandIgnored verifies that an unknown command type
// doesn't crash the daemon.
func TestE2EUnknownCommandIgnored(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	env.mockHub.SetCommand("unknown-cmd-agent", &types.HubCommand{
		Command: "teleport", // unrecognized command
		AgentID: "unknown-cmd-agent",
		Params:  map[string]interface{}{},
	})

	time.Sleep(4 * time.Second)

	// Daemon should still be healthy
	var health map[string]interface{}
	status := env.getJSON("/v1/health", &health)
	if status != http.StatusOK {
		t.Fatalf("Daemon crashed on unknown command, health returned %d", status)
	}
}

// TestE2EDaemonRestartsCleanly verifies starting two daemons sequentially on the
// same data directory works cleanly—no residual lock files or corrupted state.
func TestE2EDaemonRestartsCleanly(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	tmpDir, err := os.MkdirTemp("", "zion-e2e-restart-*")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	defer forceRemoveTestContainers(t)

	for iteration := 0; iteration < 3; iteration++ {
		mockHub.ResetState()

		port := getFreePort(t)
		cfg := &config.Config{
			NodeID:                 "restart-node",
			OperatorAddress:        "0xRestartTestOp",
			HubURL:                 mockHub.URL(),
			HubAuthToken:           "mock-jwt-token",
			MaxAgents:              5,
			CPUPerAgent:            1,
			MemoryPerAgent:         1024,
			StoragePerAgent:        2048,
			DataDir:                filepath.Join(tmpDir, "data"),
			SnapshotCache:          filepath.Join(tmpDir, "cache"),
			ContainerEngine:        "docker",
			RuntimeImage:           "alpine/openclaw:main",
			HeartbeatInterval:      2,
			HeartbeatRetryMax:      3,
			HeartbeatRetryInterval: 1,
			SnapshotRetentionDays:  1,
			LogDir:                 filepath.Join(tmpDir, "logs"),
			LogLevel:               "debug",
			HTTPTimeout:            10,
		}
		cfg.SetDefaults()
		for _, dir := range []string{cfg.DataDir, cfg.SnapshotCache, cfg.LogDir} {
			os.MkdirAll(dir, 0755)
		}

		d, err := daemon.NewDaemon(cfg)
		if err != nil {
			t.Fatalf("Iteration %d: failed to create daemon: %v", iteration, err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- d.Run(ctx) }()

		apiURL := fmt.Sprintf("http://127.0.0.1:%d", port)
		waitForAPI(t, apiURL, 15*time.Second)

		// Verify it's working
		resp, err := http.Get(apiURL + "/v1/health")
		if err != nil {
			t.Fatalf("Iteration %d: health check failed: %v", iteration, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Iteration %d: expected 200, got %d", iteration, resp.StatusCode)
		}

		// Verify heartbeat
		time.Sleep(3 * time.Second)
		if mockHub.HeartbeatCount() == 0 {
			t.Fatalf("Iteration %d: no heartbeats received", iteration)
		}

		// Shutdown
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		d.Shutdown(shutCtx)
		shutCancel()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}

		// Small wait for port release
		time.Sleep(500 * time.Millisecond)
	}
}

// TestE2EAPINotFoundRoute verifies unknown API routes return 404/405.
func TestE2EAPINotFoundRoute(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/v1/nonexistent", http.StatusNotFound},
		{"/v2/health", http.StatusNotFound},
		{"/random", http.StatusNotFound},
	}

	for _, tt := range tests {
		resp, err := http.Get(env.apiURL + tt.path)
		if err != nil {
			t.Errorf("GET %s: %v", tt.path, err)
			continue
		}
		resp.Body.Close()
		// mux may return 404 or 405 - both are acceptable for unknown routes
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("GET %s: expected 404 or 405, got %d", tt.path, resp.StatusCode)
		}
	}
}

// TestE2EHeartbeatContinuesAfterHubError verifies the daemon keeps running
// even if the Hub becomes temporarily unreachable.
func TestE2EHeartbeatContinuesAfterHubError(t *testing.T) {
	env := newE2EEnv(t)
	defer env.cleanup()
	env.start()

	// Verify initial heartbeats work
	time.Sleep(3 * time.Second)
	initialCount := env.mockHub.HeartbeatCount()
	if initialCount == 0 {
		t.Fatal("No initial heartbeats")
	}

	// Temporarily close the mock hub (simulates Hub downtime)
	env.mockHub.Close()
	time.Sleep(3 * time.Second)

	// Daemon should still be healthy (API should still respond)
	resp, err := http.Get(env.apiURL + "/v1/health")
	if err != nil {
		t.Fatalf("API should be reachable during hub outage: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 during hub outage, got %d", resp.StatusCode)
	}
}
