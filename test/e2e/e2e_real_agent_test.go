package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zion-protocol/zion-node/internal/config"
	"github.com/zion-protocol/zion-node/internal/crypto"
	"github.com/zion-protocol/zion-node/internal/daemon"
)

// e2eAuthSetup generates a wallet, saves it for daemon autoLogin, and gets a JWT for test API calls.
// It sets HOME to a temp dir so crypto.LoadWallet() finds the wallet.
// Returns (jwt_token, wallet_address).
func e2eAuthSetup(t *testing.T, hubURL string) (token string, walletAddress string) {
	t.Helper()

	// Generate a new wallet
	wallet, err := crypto.GenerateWallet()
	if err != nil {
		t.Fatalf("Failed to generate wallet: %v", err)
	}

	// Create a home dir for the wallet
	homeDir, err := os.MkdirTemp("", "zion-e2e-home-*")
	if err != nil {
		t.Fatalf("Failed to create home dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(homeDir) })

	// Save wallet to homeDir/.zion-node/wallet.json so daemon's autoLogin finds it
	walletDir := filepath.Join(homeDir, ".zion-node")
	if err := os.MkdirAll(walletDir, 0755); err != nil {
		t.Fatalf("Failed to create wallet dir: %v", err)
	}
	if err := wallet.SaveToFile(filepath.Join(walletDir, "wallet.json")); err != nil {
		t.Fatalf("Failed to save wallet: %v", err)
	}

	// Set HOME so daemon's crypto.LoadWallet() finds the wallet
	t.Setenv("HOME", homeDir)

	// Get JWT for test API calls using the same wallet
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	authClient := crypto.NewHubAuthClient(hubURL)
	jwt, err := authClient.GetJWT(ctx, wallet)
	if err != nil {
		t.Fatalf("Failed to authenticate with hub: %v", err)
	}

	t.Logf("✓ Wallet %s authenticated with hub", wallet.Address)
	return jwt, wallet.Address
}

// cleanupHubAgent deletes an agent and all related data from the Hub database.
// Calls DELETE /v1/agents/:agent_id. Logs errors but does not fail the test.
func cleanupHubAgent(t *testing.T, hubURL, hubAuthToken, agentID string) {
	t.Helper()
	req, err := http.NewRequest("DELETE", hubURL+"/v1/agents/"+agentID, nil)
	if err != nil {
		t.Logf("cleanupHubAgent: failed to create request for %s: %v", agentID, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+hubAuthToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("cleanupHubAgent: failed to delete agent %s: %v", agentID, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		t.Logf("✓ Cleaned up hub agent %s", agentID)
	} else {
		t.Logf("cleanupHubAgent: DELETE %s returned %d", agentID, resp.StatusCode)
	}
}

// cleanupHubNode deletes a node record from the Hub database.
// Calls DELETE /v1/nodes/:node_id. Logs errors but does not fail the test.
func cleanupHubNode(t *testing.T, hubURL, hubAuthToken, nodeID string) {
	t.Helper()
	req, err := http.NewRequest("DELETE", hubURL+"/v1/nodes/"+nodeID, nil)
	if err != nil {
		t.Logf("cleanupHubNode: failed to create request for %s: %v", nodeID, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+hubAuthToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("cleanupHubNode: failed to delete node %s: %v", nodeID, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		t.Logf("✓ Cleaned up hub node %s", nodeID)
	} else {
		t.Logf("cleanupHubNode: DELETE %s returned %d", nodeID, resp.StatusCode)
	}
}

// TestRealE2EFullAgentWorkflow tests the complete agent workflow with real Hub
// This test:
// 1. Creates a real agent via Hub API
// 2. Sends run command to Hub
// 3. Verifies Node receives and processes the command
// 4. Verifies Docker container is created
// 5. Verifies agent status
// 6. Stops the agent
// 7. Verifies cleanup
func TestRealE2EFullAgentWorkflow(t *testing.T) {
	defer forceRemoveTestContainers(t)

	// Get Hub configuration from environment
	hubURL := os.Getenv("HUB_ENDPOINT")
	if hubURL == "" {
		hubURL = os.Getenv("ZION_HUB_URL")
	}

	if hubURL == "" {
		t.Skip("Skipping real E2E test: HUB_ENDPOINT or ZION_HUB_URL environment variable must be set")
	}

	// Self-authenticate: generate wallet, get JWT from hub via challenge/verify flow
	hubAuthToken, walletAddress := e2eAuthSetup(t, hubURL)

	// Create temporary directory for test
	tmpDir, err := os.MkdirTemp("", "zion-node-full-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test configuration — no HubAuthToken, daemon will autoLogin using wallet
	testNodeID := fmt.Sprintf("e2e-test-%d", time.Now().Unix())
	cfg := &config.Config{
		NodeID:                 testNodeID,
		OperatorAddress:        walletAddress,
		HubURL:                 hubURL,
		MaxAgents:              3,
		CPUPerAgent:            2,
		MemoryPerAgent:         2048,
		StoragePerAgent:        10240,
		DataDir:                filepath.Join(tmpDir, "data"),
		SnapshotCache:          filepath.Join(tmpDir, "cache"),
		ContainerEngine:        "docker",
		RuntimeImage:           "alpine/openclaw:main", // Use real OpenClaw image
		HeartbeatInterval:      5,                      // Fast heartbeat for testing
		HeartbeatRetryMax:      3,
		HeartbeatRetryInterval: 1,
		SnapshotRetentionDays:  1,
		LogDir:                 filepath.Join(tmpDir, "logs"),
		LogLevel:               "info",
		HTTPTimeout:            30,
	}
	cfg.SetDefaults()

	// Create required directories
	for _, dir := range []string{cfg.DataDir, cfg.SnapshotCache, cfg.LogDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	t.Logf("Starting Node: %s", testNodeID)

	// Create and start daemon
	d, err := daemon.NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	daemonErr := make(chan error, 1)
	go func() {
		daemonErr <- d.Run(ctx)
	}()

	// Wait for daemon to start and register
	time.Sleep(3 * time.Second)

	// Step 1: Create agent via Hub API
	var agentID string
	t.Run("CreateAgent", func(t *testing.T) {
		agentName := fmt.Sprintf("E2ETestAgent-%d", time.Now().Unix())
		createReq := map[string]interface{}{
			"name":    agentName,
			"llm_key": fmt.Sprintf("test-key-%d", time.Now().Unix()),
		}

		reqBody, _ := json.Marshal(createReq)
		req, err := http.NewRequest("POST", hubURL+"/v1/agents/register", bytes.NewBuffer(reqBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+hubAuthToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to create agent: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			var errResp map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&errResp)
			t.Fatalf("Expected status 201, got %d: %v", resp.StatusCode, errResp)
		}

		var createResp struct {
			AgentID    string `json:"agent_id"`
			Name       string `json:"name"`
			ZHPBalance int    `json:"shp_balance"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		agentID = createResp.AgentID
		t.Logf("✓ Created agent: %s (ZHP balance: %d)", agentID, createResp.ZHPBalance)
	})

	if agentID == "" {
		t.Fatal("Agent creation failed, cannot continue test")
	}

	// Cleanup agent at the end
	defer func() {
		// Best effort cleanup: stop agent
		stopReq, _ := http.NewRequest("POST", hubURL+"/v1/agents/"+agentID+"/stop", nil)
		stopReq.Header.Set("Authorization", "Bearer "+hubAuthToken)
		stopReq.Header.Set("Content-Type", "application/json")
		http.DefaultClient.Do(stopReq)
		t.Logf("Cleanup: Stopped agent %s", agentID)
	}()

	// Step 3: Send run command via Hub API
	t.Run("SendRunCommand", func(t *testing.T) {
		req, err := http.NewRequest("POST", hubURL+"/v1/agents/"+agentID+"/run", bytes.NewBuffer([]byte("{}")))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+hubAuthToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send run command: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errResp map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&errResp)
			t.Fatalf("Expected status 200, got %d: %v", resp.StatusCode, errResp)
		}

		var runResp struct {
			AgentID string `json:"agent_id"`
			NodeID  string `json:"node_id"`
			Status  string `json:"status"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&runResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if runResp.NodeID != testNodeID {
			t.Errorf("Expected node_id %s, got %s", testNodeID, runResp.NodeID)
		}

		t.Logf("✓ Run command sent, assigned to node: %s", runResp.NodeID)
	})

	// Step 4: Wait for Node to process command and create container
	// Heartbeat is 5s, give it up to 15s to process
	var containerCreated bool
	t.Run("VerifyContainerCreation", func(t *testing.T) {
		t.Log("Waiting for Node to process command (max 20s)...")

		for i := 0; i < 8; i++ {
			time.Sleep(2500 * time.Millisecond)

			// Check via Docker CLI
			cmd := exec.Command("docker", "ps", "-a", "--filter", "name=zion-agent-"+agentID, "--format", "{{.ID}}")
			output, err := cmd.Output()
			if err == nil && len(strings.TrimSpace(string(output))) > 0 {
				containerCreated = true
				t.Logf("✓ Container created: zion-agent-%s", agentID)
				break
			}

			if containerCreated {
				break
			}

			t.Logf("  [%d/8] Waiting for container creation...", i+1)
		}

		if !containerCreated {
			t.Error("Container was not created within timeout")
		}

		// Monitor container stability for 20 seconds
		if containerCreated {
			t.Log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			t.Log("Monitoring container stability for 20 seconds...")
			t.Log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			monitorInterval := 4 * time.Second
			monitorChecks := 5 // 5 checks over 20 seconds (4s * 5 = 20s)
			crashDetected := false

			for i := 1; i <= monitorChecks; i++ {
				time.Sleep(monitorInterval)

				t.Logf("[Check %d/%d at %ds] Checking container status...", i, monitorChecks, i*4)

				// Check if container is still running
				cmd := exec.Command("docker", "ps", "--filter", "name=zion-agent-"+agentID, "--format", "{{.Status}}")
				output, err := cmd.Output()
				if err == nil {
					status := strings.TrimSpace(string(output))
					if status == "" {
						// Container not found in running containers
						t.Logf("  ⚠️  Container not running, checking if it exited...")

						// Check all containers (including stopped)
						cmd = exec.Command("docker", "ps", "-a", "--filter", "name=zion-agent-"+agentID, "--format", "{{.Status}}")
						output, err = cmd.Output()
						if err == nil {
							allStatus := strings.TrimSpace(string(output))
							t.Logf("  ✗ Container status: %s", allStatus)

							// Get exit code
							cmd = exec.Command("docker", "inspect", "zion-agent-"+agentID, "--format", "{{.State.ExitCode}}")
							if exitOutput, err := cmd.Output(); err == nil {
								exitCode := strings.TrimSpace(string(exitOutput))
								t.Logf("  ✗ Exit code: %s", exitCode)
							}

							crashDetected = true
							break
						}
					} else {
						t.Logf("  ✓ Container still running: %s", status)
					}
				}

				// Check process count
				cmd = exec.Command("docker", "exec", "zion-agent-"+agentID, "ps", "aux")
				if output, err := cmd.Output(); err == nil {
					processLines := strings.Split(string(output), "\n")
					runningCount := 0
					for _, line := range processLines {
						if line != "" && !strings.Contains(line, "ps aux") && !strings.Contains(line, "PID") {
							runningCount++
						}
					}
					t.Logf("  ✓ Running processes: %d", runningCount)
				} else {
					t.Logf("  ⚠️  Could not check processes: %v", err)
				}
			}

			t.Log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			if crashDetected {
				t.Error("✗ Container crashed during monitoring period")
			} else {
				t.Log("✓ Container remained stable for 20 seconds")
			}
			t.Log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		}
	})

	// Step 5: Verify Docker container details
	t.Run("VerifyDockerContainer", func(t *testing.T) {
		if !containerCreated {
			t.Skip("Skipping: container was not created")
		}

		cmd := exec.Command("docker", "inspect", "zion-agent-"+agentID, "--format", "{{.State.Status}}")
		output, err := cmd.Output()
		if err != nil {
			t.Errorf("Failed to inspect container: %v", err)
			return
		}

		status := strings.TrimSpace(string(output))
		// Container might be running or exited (if it completed quickly)
		if status != "running" && status != "created" && status != "exited" {
			t.Errorf("Unexpected container status: %s", status)
		} else {
			t.Logf("✓ Docker container status: %s", status)
		}

		// Verify image
		cmd = exec.Command("docker", "inspect", "zion-agent-"+agentID, "--format", "{{.Config.Image}}")
		output, err = cmd.Output()
		if err == nil {
			image := strings.TrimSpace(string(output))
			t.Logf("✓ Using image: %s", image)
		}

		// Check container logs for errors
		t.Log("Checking container logs...")
		cmd = exec.Command("docker", "logs", "zion-agent-"+agentID, "--tail", "50")
		output, err = cmd.CombinedOutput()
		if err == nil {
			logs := string(output)
			t.Logf("Container logs (last 50 lines):\n%s", logs)

			// Check for common error patterns
			errorPatterns := []string{
				"panic:",
				"fatal error:",
				"Error:",
				"failed to",
				"cannot",
			}

			hasError := false
			for _, pattern := range errorPatterns {
				if strings.Contains(strings.ToLower(logs), strings.ToLower(pattern)) {
					hasError = true
					t.Logf("⚠️  Found potential error pattern in logs: %s", pattern)
				}
			}

			if !hasError {
				t.Log("✓ No obvious errors in container logs")
			}
		} else {
			t.Logf("Warning: Could not fetch container logs: %v", err)
		}

		// Check container processes
		t.Log("Checking container processes...")
		cmd = exec.Command("docker", "exec", "zion-agent-"+agentID, "ps", "aux")
		output, err = cmd.Output()
		if err == nil {
			processes := string(output)
			t.Logf("Container processes:\n%s", processes)

			// Count running processes (excluding ps itself)
			processLines := strings.Split(processes, "\n")
			runningCount := 0
			for _, line := range processLines {
				if line != "" && !strings.Contains(line, "ps aux") && !strings.Contains(line, "PID") {
					runningCount++
				}
			}

			if runningCount > 0 {
				t.Logf("✓ Container has %d running process(es)", runningCount)
			} else {
				t.Log("⚠️  No processes found in container (might have exited)")
			}
		} else {
			t.Logf("Warning: Could not check container processes: %v", err)
			t.Log("(This is expected if container has already exited)")
		}

		// Check container health (if health check is configured)
		t.Log("Checking container health...")
		cmd = exec.Command("docker", "inspect", "zion-agent-"+agentID, "--format", "{{.State.Health.Status}}")
		output, err = cmd.Output()
		if err == nil {
			healthStatus := strings.TrimSpace(string(output))
			if healthStatus != "" && healthStatus != "<no value>" {
				t.Logf("Container health status: %s", healthStatus)
			} else {
				t.Log("(No health check configured for this container)")
			}
		}

		// Get container uptime
		cmd = exec.Command("docker", "inspect", "zion-agent-"+agentID, "--format", "{{.State.StartedAt}}")
		output, err = cmd.Output()
		if err == nil {
			startTime := strings.TrimSpace(string(output))
			t.Logf("Container started at: %s", startTime)
		}

		// Check container exit code (if exited)
		cmd = exec.Command("docker", "inspect", "zion-agent-"+agentID, "--format", "{{.State.ExitCode}}")
		output, err = cmd.Output()
		if err == nil {
			exitCode := strings.TrimSpace(string(output))
			if exitCode != "0" {
				t.Logf("⚠️  Container exit code: %s (non-zero indicates error)", exitCode)
			}
		}
	})

	// Step 7: Stop agent via Hub API
	t.Run("StopAgent", func(t *testing.T) {
		if !containerCreated {
			t.Skip("Skipping: container was not created")
		}

		req, err := http.NewRequest("POST", hubURL+"/v1/agents/"+agentID+"/stop", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+hubAuthToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send stop command: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errResp map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&errResp)
			t.Logf("Warning: Stop command returned %d: %v", resp.StatusCode, errResp)
		} else {
			t.Log("✓ Stop command sent")
		}

		// Wait for Node to process stop command
		time.Sleep(8 * time.Second)

		// Verify container is stopped/removed
		cmd := exec.Command("docker", "ps", "--filter", "name=zion-agent-"+agentID, "--format", "{{.ID}}")
		output, err := cmd.Output()
		if err == nil {
			running := strings.TrimSpace(string(output))
			if running != "" {
				t.Logf("Warning: Container still running: %s", running)
			} else {
				t.Log("✓ Container stopped/removed")
			}
		}
	})

	// Cleanup
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	t.Log("Shutting down daemon...")
	if err := d.Shutdown(shutdownCtx); err != nil {
		t.Logf("Shutdown error: %v", err)
	}

	cancel()
	select {
	case err := <-daemonErr:
		if err != nil && err != context.Canceled {
			t.Logf("Daemon error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Log("Daemon shutdown timeout")
	}

	// Cleanup Hub data: delete agent and node records from Hub DB
	if agentID != "" {
		cleanupHubAgent(t, hubURL, hubAuthToken, agentID)
	}
	cleanupHubNode(t, hubURL, hubAuthToken, testNodeID)

	t.Log("✅ Full E2E agent workflow test completed")
}

// logDockerStats runs docker stats --no-stream for given container names and logs output.
func logDockerStats(t *testing.T, label string, containerNames []string) {
	t.Helper()
	if len(containerNames) == 0 {
		return
	}
	args := append([]string{"stats", "--no-stream", "--format", "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}"}, containerNames...)
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("[docker stats %s] error: %v", label, err)
		return
	}
	t.Logf("[docker stats %s]\n%s", label, string(out))
}

func multiAgentE2EConfig(t *testing.T, hubURL, walletAddress string, numAgents int) (*config.Config, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "zion-node-multi-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	ts := time.Now().Unix()
	cfg := &config.Config{
		NodeID:                 fmt.Sprintf("e2e-test-%d", ts),
		OperatorAddress:        walletAddress,
		HubURL:                 hubURL,
		MaxAgents:              numAgents,
		CPUPerAgent:            2,
		MemoryPerAgent:         2048,
		StoragePerAgent:        10240,
		DataDir:                filepath.Join(tmpDir, "data"),
		SnapshotCache:          filepath.Join(tmpDir, "cache"),
		ContainerEngine:        "docker",
		RuntimeImage:           "alpine/openclaw:main",
		HeartbeatInterval:      5,
		HeartbeatRetryMax:      3,
		HeartbeatRetryInterval: 1,
		SnapshotRetentionDays:  1,
		LogDir:                 filepath.Join(tmpDir, "logs"),
		LogLevel:               "info",
		HTTPTimeout:            30,
	}
	cfg.SetDefaults()
	for _, dir := range []string{cfg.DataDir, cfg.SnapshotCache, cfg.LogDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
	}
	return cfg, tmpDir
}

func multiAgentCreateAndRun(t *testing.T, hubURL, hubAuthToken string, numAgents int) []string {
	t.Helper()
	agentIDs := make([]string, 0, numAgents)
	for i := 0; i < numAgents; i++ {
		createReq := map[string]interface{}{
			"name":    fmt.Sprintf("E2EMulti-%d-%d", time.Now().Unix(), i),
			"llm_key": fmt.Sprintf("test-key-%d-%d", time.Now().Unix(), i),
		}
		reqBody, _ := json.Marshal(createReq)
		req, _ := http.NewRequest("POST", hubURL+"/v1/agents/register", bytes.NewBuffer(reqBody))
		req.Header.Set("Authorization", "Bearer "+hubAuthToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Create agent %d: %v", i, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			var errResp map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&errResp)
			t.Fatalf("Create agent %d: got %d %v", i, resp.StatusCode, errResp)
		}
		var createResp struct {
			AgentID string `json:"agent_id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
			t.Fatalf("Decode create response: %v", err)
		}
		agentIDs = append(agentIDs, createResp.AgentID)
	}
	for i, id := range agentIDs {
		req, _ := http.NewRequest("POST", hubURL+"/v1/agents/"+id+"/run", bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Authorization", "Bearer "+hubAuthToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Run agent %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Run agent %d: status %d", i, resp.StatusCode)
		}
	}
	t.Logf("✓ Run sent for %d agents", numAgents)
	return agentIDs
}

func multiAgentWaitContainers(t *testing.T, agentIDs []string, numAgents int) {
	t.Helper()
	for i := 0; i < 16; i++ {
		time.Sleep(2500 * time.Millisecond)
		ready := 0
		for _, id := range agentIDs {
			cmd := exec.Command("docker", "ps", "--filter", "name=zion-agent-"+id, "--format", "{{.ID}}")
			if out, err := cmd.Output(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
				ready++
			}
		}
		if ready == numAgents {
			t.Logf("✓ All %d containers up after %ds", numAgents, (i+1)*2)
			return
		}
		t.Logf("  [%d/16] Containers up: %d/%d", i+1, ready, numAgents)
	}
}

// TestRealE2EFullAgentWorkflowMulti runs multiple agents and logs Docker resource usage.
func TestRealE2EFullAgentWorkflowMulti(t *testing.T) {
	defer forceRemoveTestContainers(t)

	hubURL := os.Getenv("HUB_ENDPOINT")
	if hubURL == "" {
		hubURL = os.Getenv("ZION_HUB_URL")
	}
	if hubURL == "" {
		t.Skip("Skipping: HUB_ENDPOINT or ZION_HUB_URL required")
	}

	// Self-authenticate: generate wallet, get JWT from hub via challenge/verify flow
	hubAuthToken, walletAddress := e2eAuthSetup(t, hubURL)

	const numAgents = 3
	cfg, tmpDir := multiAgentE2EConfig(t, hubURL, walletAddress, numAgents)
	defer os.RemoveAll(tmpDir)

	d, err := daemon.NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	daemonErr := make(chan error, 1)
	go func() { daemonErr <- d.Run(ctx) }()
	time.Sleep(3 * time.Second)

	agentIDs := multiAgentCreateAndRun(t, hubURL, hubAuthToken, numAgents)
	defer func() {
		for _, id := range agentIDs {
			stopReq, _ := http.NewRequest("POST", hubURL+"/v1/agents/"+id+"/stop", nil)
			stopReq.Header.Set("Authorization", "Bearer "+hubAuthToken)
			http.DefaultClient.Do(stopReq)
		}
	}()

	multiAgentWaitContainers(t, agentIDs, numAgents)
	names := make([]string, len(agentIDs))
	for i, id := range agentIDs {
		names[i] = "zion-agent-" + id
	}
	logDockerStats(t, "T+5s", names)
	time.Sleep(5 * time.Second)
	logDockerStats(t, "T+10s", names)
	time.Sleep(10 * time.Second)
	logDockerStats(t, "T+20s", names)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	d.Shutdown(shutdownCtx)
	cancel()
	<-daemonErr

	// Cleanup Hub data: delete agent and node records from Hub DB
	for _, id := range agentIDs {
		cleanupHubAgent(t, hubURL, hubAuthToken, id)
	}
	cleanupHubNode(t, hubURL, hubAuthToken, cfg.NodeID)

	t.Log("✅ Multi-agent E2E completed")
}
