package daemon

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/zion-protocol/zion-node/internal/testutil"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// TestProcessHubCommand_UnsignedRejection verifies that when hub_public_key
// is configured, unsigned commands are rejected and reported to the hub.
// This is the root cause of the original bug: hub sends unsigned commands,
// node silently rejects them, agent stays stuck as "running" in hub DB.
func TestProcessHubCommand_UnsignedRejection(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)

	// Configure hub_public_key to require signatures
	cfg.HubPublicKey = "04aabbccdd" // Any non-empty value triggers signature requirement

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx := context.Background()

	// Send an unsigned command (like the hub was doing in the bug scenario)
	cmd := &types.HubCommand{
		Command: "run",
		AgentID: "ag_test_unsigned",
		Params: map[string]interface{}{
			"runtime_engine": "openclaw",
			"engine_version": "1.0.0",
			"image_hash":     "sha256:test",
		},
		// Signature intentionally left empty
	}

	d.processHubCommand(ctx, cmd)

	// Give the goroutine time to report the failure
	time.Sleep(500 * time.Millisecond)

	// Verify failure was reported to the hub
	failures := mockHub.GetFailures()
	found := false
	for _, f := range failures {
		if f.AgentID == "ag_test_unsigned" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected failure to be reported to hub for unsigned command, but no failure was found")
	}
}

// TestProcessHubCommand_InvalidSignatureRejection verifies that commands
// with invalid signatures are rejected and reported to the hub.
func TestProcessHubCommand_InvalidSignatureRejection(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	// Generate a real keypair for the "hub"
	hubKey, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	hubPubHex := hex.EncodeToString(ethcrypto.FromECDSAPub(&hubKey.PublicKey))

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)
	cfg.HubPublicKey = hubPubHex

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx := context.Background()

	// Send a command with a completely bogus signature
	cmd := &types.HubCommand{
		Command:   "run",
		AgentID:   "ag_test_badsig",
		Params:    map[string]interface{}{"runtime_engine": "openclaw"},
		Signature: "deadbeef" + hex.EncodeToString(make([]byte, 63)), // wrong length/data
		SignedAt:  time.Now().Unix(),
	}

	d.processHubCommand(ctx, cmd)

	time.Sleep(500 * time.Millisecond)

	failures := mockHub.GetFailures()
	found := false
	for _, f := range failures {
		if f.AgentID == "ag_test_badsig" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected failure to be reported to hub for invalid signature, but no failure was found")
	}
}

// TestProcessHubCommand_ValidSignatureAccepted verifies that correctly signed
// commands pass verification and proceed to execution.
func TestProcessHubCommand_ValidSignatureAccepted(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	// Generate a real keypair
	hubKey, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	hubPubHex := hex.EncodeToString(ethcrypto.FromECDSAPub(&hubKey.PublicKey))

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)
	cfg.HubPublicKey = hubPubHex

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx := context.Background()

	// Create a properly signed command
	agentID := "ag_test_validsig"
	signedAt := time.Now().Unix()
	message := fmt.Sprintf("zion:cmd:run:%s:%d", agentID, signedAt)
	hash := sha256.Sum256([]byte(message))

	sig, err := ethcrypto.Sign(hash[:], hubKey)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}
	sigHex := hex.EncodeToString(sig)

	cmd := &types.HubCommand{
		Command:   "run",
		AgentID:   agentID,
		Params:    map[string]interface{}{"runtime_engine": "openclaw"},
		Signature: sigHex,
		SignedAt:  signedAt,
	}

	d.processHubCommand(ctx, cmd)

	time.Sleep(300 * time.Millisecond)

	// No signature-related failure should be reported for a valid signature
	// (container failures like image pull are expected in CI and are acceptable)
	failures := mockHub.GetFailures()
	for _, f := range failures {
		if f.AgentID == agentID && strings.Contains(f.Reason, "signature") {
			t.Errorf("Unexpected signature failure reported for agent with valid signature: %s", f.Reason)
		}
	}
}

// TestProcessHubCommand_NoSignatureRequiredWithoutPublicKey verifies that
// commands are processed normally when hub_public_key is empty (no verification).
func TestProcessHubCommand_NoSignatureRequiredWithoutPublicKey(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)
	cfg.HubPublicKey = "" // No public key = no signature verification

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx := context.Background()

	// Send unsigned command — should be processed normally
	cmd := &types.HubCommand{
		Command: "run",
		AgentID: "ag_test_nosig",
		Params:  map[string]interface{}{"runtime_engine": "openclaw"},
	}

	d.processHubCommand(ctx, cmd)

	time.Sleep(300 * time.Millisecond)

	// No signature-related failure should be reported
	// (container failures like image pull are expected in CI and are acceptable)
	failures := mockHub.GetFailures()
	for _, f := range failures {
		if f.AgentID == "ag_test_nosig" && strings.Contains(f.Reason, "signature") {
			t.Errorf("Unexpected signature failure reported for unsigned command when no public key is configured: %s", f.Reason)
		}
	}
}

// TestProcessHubCommand_SignatureWithWrongKey verifies that a command signed
// with a different key than what the node expects is rejected.
func TestProcessHubCommand_SignatureWithWrongKey(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	// Generate TWO different keypairs
	hubKey, _ := ethcrypto.GenerateKey()
	wrongKey, _ := ethcrypto.GenerateKey()

	// Node is configured with the CORRECT public key
	hubPubHex := hex.EncodeToString(ethcrypto.FromECDSAPub(&hubKey.PublicKey))

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)
	cfg.HubPublicKey = hubPubHex

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx := context.Background()

	// Sign with the WRONG key
	agentID := "ag_test_wrongkey"
	signedAt := time.Now().Unix()
	message := fmt.Sprintf("zion:cmd:run:%s:%d", agentID, signedAt)
	hash := sha256.Sum256([]byte(message))
	sig, _ := ethcrypto.Sign(hash[:], wrongKey)

	cmd := &types.HubCommand{
		Command:   "run",
		AgentID:   agentID,
		Params:    map[string]interface{}{"runtime_engine": "openclaw"},
		Signature: hex.EncodeToString(sig),
		SignedAt:  signedAt,
	}

	d.processHubCommand(ctx, cmd)
	time.Sleep(500 * time.Millisecond)

	failures := mockHub.GetFailures()
	found := false
	for _, f := range failures {
		if f.AgentID == agentID {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected failure for command signed with wrong key")
	}
}

// TestProcessHubCommand_ConcurrentCommandRejection verifies that multiple
// unsigned commands can be rejected concurrently without race conditions.
func TestProcessHubCommand_ConcurrentCommandRejection(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)
	cfg.HubPublicKey = "04aabbccdd" // Require signatures → unsigned commands will be rejected

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx := context.Background()

	// Process 10 unsigned commands concurrently — all should be rejected
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := &types.HubCommand{
				Command: "run",
				AgentID: fmt.Sprintf("ag_concurrent_reject_%d", i),
				Params:  map[string]interface{}{"runtime_engine": "openclaw"},
			}
			d.processHubCommand(ctx, cmd)
		}(i)
	}
	wg.Wait()

	// Give time for failure reports to be sent
	time.Sleep(1 * time.Second)

	// All 10 should have been reported as failures
	failures := mockHub.GetFailures()
	rejectedCount := 0
	for _, f := range failures {
		for i := 0; i < 10; i++ {
			if f.AgentID == fmt.Sprintf("ag_concurrent_reject_%d", i) {
				rejectedCount++
				break
			}
		}
	}

	if rejectedCount != 10 {
		t.Errorf("Expected 10 rejection reports, got %d", rejectedCount)
	}
}

// TestProcessHubCommand_ContextCancellation verifies that commands are not
// processed when the context is cancelled (e.g., during shutdown).
func TestProcessHubCommand_ContextCancellation(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)
	cfg.HubPublicKey = "04aabbccdd" // Require signatures

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	// Cancel context before processing
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := &types.HubCommand{
		Command: "run",
		AgentID: "ag_test_cancelled",
		Params:  map[string]interface{}{"runtime_engine": "openclaw"},
		// No signature — this should be caught by cancelled context first
	}

	d.processHubCommand(ctx, cmd)

	time.Sleep(300 * time.Millisecond)

	// With cancelled context, the command should be skipped entirely
	// (no failure report, no execution)
	failures := mockHub.GetFailures()
	for _, f := range failures {
		if f.AgentID == "ag_test_cancelled" {
			t.Errorf("Should not report failure when context is cancelled: %s", f.Reason)
		}
	}
}

// TestProcessHubCommand_StopCommandRejectedWithoutSignature verifies that
// stop commands are also subject to signature verification when configured.
func TestProcessHubCommand_StopCommandRejectedWithoutSignature(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)
	cfg.HubPublicKey = "04aabbccdd"

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx := context.Background()

	cmd := &types.HubCommand{
		Command: "stop",
		AgentID: "ag_test_stop_unsigned",
		Params:  map[string]interface{}{},
	}

	d.processHubCommand(ctx, cmd)
	time.Sleep(500 * time.Millisecond)

	failures := mockHub.GetFailures()
	found := false
	for _, f := range failures {
		if f.AgentID == "ag_test_stop_unsigned" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected failure to be reported for unsigned stop command")
	}
}

// TestProcessHubCommand_ValidSignedStopCommand verifies that properly signed
// stop commands are accepted.
func TestProcessHubCommand_ValidSignedStopCommand(t *testing.T) {
	mockHub := testutil.NewMockHub()
	defer mockHub.Close()

	hubKey, _ := ethcrypto.GenerateKey()
	hubPubHex := hex.EncodeToString(ethcrypto.FromECDSAPub(&hubKey.PublicKey))

	cfg := testutil.NewTestConfig(mockHub.URL())
	defer testutil.CleanupTestConfig(cfg)
	cfg.HubPublicKey = hubPubHex

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx := context.Background()

	agentID := "ag_test_stop_signed"
	signedAt := time.Now().Unix()
	message := fmt.Sprintf("zion:cmd:stop:%s:%d", agentID, signedAt)
	hash := sha256.Sum256([]byte(message))
	sig, _ := ethcrypto.Sign(hash[:], hubKey)

	cmd := &types.HubCommand{
		Command:   "stop",
		AgentID:   agentID,
		Params:    map[string]interface{}{},
		Signature: hex.EncodeToString(sig),
		SignedAt:  signedAt,
	}

	d.processHubCommand(ctx, cmd)
	time.Sleep(300 * time.Millisecond)

	// No failure should be reported
	failures := mockHub.GetFailures()
	for _, f := range failures {
		if f.AgentID == agentID {
			t.Errorf("Unexpected failure for valid signed stop command: %s", f.Reason)
		}
	}
}

// signCommand is a test helper that creates a properly signed HubCommand
func signCommand(t *testing.T, key *ecdsa.PrivateKey, command, agentID string, params map[string]interface{}) *types.HubCommand {
	t.Helper()
	signedAt := time.Now().Unix()
	message := fmt.Sprintf("zion:cmd:%s:%s:%d", command, agentID, signedAt)
	hash := sha256.Sum256([]byte(message))
	sig, err := ethcrypto.Sign(hash[:], key)
	if err != nil {
		t.Fatalf("Failed to sign command: %v", err)
	}
	return &types.HubCommand{
		Command:   command,
		AgentID:   agentID,
		Params:    params,
		Signature: hex.EncodeToString(sig),
		SignedAt:  signedAt,
	}
}
