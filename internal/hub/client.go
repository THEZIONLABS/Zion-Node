package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zion-protocol/zion-node/internal/config"
	"github.com/zion-protocol/zion-node/internal/crypto"
	"github.com/zion-protocol/zion-node/internal/errors"
	httputil "github.com/zion-protocol/zion-node/internal/http"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// Client handles communication with Zion Hub
type Client struct {
	cfg          *config.Config
	httpClient   *httputil.Client
	hubURL       string
	nodeID       string
	version      string
	sequence     int64
	mu           sync.Mutex
	failureCount int
	successCount int // consecutive successes needed to transition back to online
	status       string
}

// NewClient creates a new Hub client
func NewClient(cfg *config.Config) *Client {
	timeout := time.Duration(cfg.HTTPTimeout) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	httpClient := httputil.NewClient(cfg.HubURL, timeout)
	httpClient.SetHeader("X-Node-ID", cfg.NodeID)

	// Set Authorization header if auth token is provided
	if cfg.HubAuthToken != "" {
		httpClient.SetHeader("Authorization", "Bearer "+cfg.HubAuthToken)
	}

	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
		hubURL:     cfg.HubURL,
		nodeID:     cfg.NodeID,
		sequence:   0,
		status:     "online",
	}
}

// SetVersion sets the node binary version to report during registration.
func (c *Client) SetVersion(v string) {
	c.version = v
}

// Register registers node with Hub (POST /v1/nodes)
// Returns true if registration successful, false if already registered
func (c *Client) Register(ctx context.Context, runtimeInfo types.RuntimeInfo) (bool, error) {
	// Get node's public key from wallet
	publicKey := c.getNodePublicKey()

	// Compute allocatable resources (ensure minimum requirements: 2 CPU, 2 GB memory, 10 GB disk)
	memoryGB := (c.cfg.MemoryPerAgent * c.cfg.MaxAgents) / 1024
	if memoryGB < 2 {
		memoryGB = 2
	}
	diskGB := (c.cfg.StoragePerAgent * c.cfg.MaxAgents) / 1024
	if diskGB < 10 {
		diskGB = 10
	}

	registration := types.NodeRegistration{
		NodeID:            c.cfg.NodeID,
		PublicKey:         publicKey,
		CPUCores:          c.cfg.CPUPerAgent * c.cfg.MaxAgents,
		MemoryGB:          memoryGB,
		DiskGB:            diskGB,
		SystemCPU:         c.cfg.SystemCPU,
		SystemMemoryMB:    c.cfg.SystemMemoryMB,
		TotalSlots:        c.cfg.MaxAgents,
		BinaryHash:        selfBinaryHash(),
		NodeVersion:       c.version,
		SupportedRuntimes: []types.RuntimeInfo{runtimeInfo},
	}

	resp, err := c.httpClient.PostJSON(ctx, "/v1/nodes", registration)
	if err != nil {
		return false, &errors.ErrHubCommunication{Operation: "register", Err: err}
	}
	defer resp.Body.Close()

	// 201 = created, 200 = re-registered (upsert)
	if resp.StatusCode == http.StatusCreated {
		return true, nil
	}
	if resp.StatusCode == http.StatusOK {
		// Already registered, data updated via upsert
		return false, nil
	}

	// 409 Conflict - could be "different owner" or legacy "already-exists"
	if resp.StatusCode == http.StatusConflict {
		// Read response body to determine the conflict reason
		body, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && strings.Contains(strings.ToLower(errResp.Error.Message), "different owner") {
			return false, &errors.ErrNodeIDOccupied{
				NodeID:  c.cfg.NodeID,
				Message: errResp.Error.Message,
			}
		}
		// Legacy 409 - treat as already registered
		return false, nil
	}

	// Other errors - read body for detail
	body, _ := io.ReadAll(resp.Body)
	detail := string(body)
	if detail == "" {
		detail = fmt.Sprintf("unexpected status code: %d", resp.StatusCode)
	} else {
		detail = fmt.Sprintf("status %d: %s", resp.StatusCode, detail)
	}
	return false, &errors.ErrHubCommunication{
		Operation: "register",
		Err:       fmt.Errorf("%s", detail),
	}
}

// SendHeartbeat sends heartbeat to Hub (POST /v1/nodes/{node_id}/heartbeat)
func (c *Client) SendHeartbeat(ctx context.Context, agents []types.AgentInfo, capacity types.CapacityInfo) ([]types.HubCommand, error) {
	c.mu.Lock()
	status := c.status
	c.mu.Unlock()

	heartbeat := types.Heartbeat{
		Timestamp: time.Now().Unix(),
		Status:    status,
		Capacity:  capacity,
		Agents:    agents,
	}

	path := fmt.Sprintf("/v1/nodes/%s/heartbeat", c.nodeID)
	resp, err := c.httpClient.PostJSON(ctx, path, heartbeat)
	if err != nil {
		c.recordFailure()
		return nil, &errors.ErrHubCommunication{Operation: "heartbeat", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.recordFailure()
		return nil, &errors.ErrHubCommunication{
			Operation: "heartbeat",
			Err:       fmt.Errorf("status code: %d", resp.StatusCode),
		}
	}

	c.recordSuccess()

	var hbResp types.HeartbeatResponse
	if err := httputil.DecodeJSON(resp, &hbResp); err != nil {
		return nil, nil // No commands
	}

	if len(hbResp.Commands) == 0 {
		return nil, nil
	}
	return hbResp.Commands, nil
}

// UploadSnapshot uploads snapshot data to Hub, Hub will upload to S3
func (c *Client) UploadSnapshot(ctx context.Context, agentID string, snapshotRef string, file io.Reader, size int64) (string, error) {
	fields := map[string]string{
		"agent_id":     agentID,
		"snapshot_ref": snapshotRef,
	}

	resp, err := c.httpClient.PostMultipart(ctx, "/v1/nodes/snapshots/upload", fields, "snapshot", file, snapshotRef)
	if err != nil {
		return "", &errors.ErrSnapshotOperation{
			Operation: "upload",
			AgentID:   agentID,
			Err:       err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", &errors.ErrSnapshotOperation{
			Operation: "upload",
			AgentID:   agentID,
			Err:       fmt.Errorf("status code: %d", resp.StatusCode),
		}
	}

	var result struct {
		URI string `json:"uri"`
	}
	if err := httputil.DecodeJSON(resp, &result); err != nil {
		return "", &errors.ErrSnapshotOperation{
			Operation: "upload",
			AgentID:   agentID,
			Err:       err,
		}
	}

	return result.URI, nil
}

// DownloadSnapshot downloads snapshot. If downloadURL is provided (presigned
// URL from Hub command params), it is used directly. Otherwise falls back to
// fetching a presigned URL via the Hub API.
func (c *Client) DownloadSnapshot(ctx context.Context, snapshotRef string, downloadURL string) (io.ReadCloser, error) {
	if snapshotRef == "" {
		return nil, &errors.ErrSnapshotOperation{Operation: "download", Err: fmt.Errorf("snapshot ref cannot be empty")}
	}

	url := downloadURL

	// If no direct URL provided, fetch one from Hub API
	if url == "" {
		resp, err := c.httpClient.Get(ctx, "/v1/checkpoints/"+snapshotRef+"/download")
		if err != nil {
			return nil, &errors.ErrSnapshotOperation{Operation: "download", Err: err}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, &errors.ErrSnapshotOperation{
				Operation: "download",
				Err:       fmt.Errorf("failed to get download URL: status %d", resp.StatusCode),
			}
		}

		var result struct {
			SnapshotRef string `json:"snapshot_ref"`
			DownloadURL string `json:"download_url"`
			ExpiresIn   int    `json:"expires_in"`
		}
		if err := httputil.DecodeJSON(resp, &result); err != nil {
			return nil, &errors.ErrSnapshotOperation{Operation: "download", Err: err}
		}
		url = result.DownloadURL
	}

	// Validate URL scheme
	if !strings.HasPrefix(url, "https://") {
		if !strings.HasPrefix(url, "http://127.0.0.1") &&
			!strings.HasPrefix(url, "http://localhost") {
			return nil, &errors.ErrSnapshotOperation{
				Operation: "download",
				Err:       fmt.Errorf("invalid download URL format: must be HTTPS (or HTTP for localhost)"),
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, &errors.ErrSnapshotOperation{Operation: "download", Err: err}
	}

	// Use HTTP client with timeout from config
	httpClient := &http.Client{
		Timeout: time.Duration(c.cfg.HTTPTimeout) * time.Second,
	}
	if httpClient.Timeout == 0 {
		httpClient.Timeout = 30 * time.Second // Default timeout for S3 download
	}
	s3Resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &errors.ErrSnapshotOperation{Operation: "download", Err: err}
	}

	if s3Resp.StatusCode != http.StatusOK {
		s3Resp.Body.Close()
		return nil, &errors.ErrSnapshotOperation{
			Operation: "download",
			Err:       fmt.Errorf("S3 download failed: status %d", s3Resp.StatusCode),
		}
	}

	return s3Resp.Body, nil
}

// WaitForSnapshotConfirmation waits for Hub to confirm snapshot upload
func (c *Client) WaitForSnapshotConfirmation(ctx context.Context, agentID string, snapshotRef string) (bool, error) {
	maxRetries := 30
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for retryCount := 0; retryCount < maxRetries; retryCount++ {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
			resp, err := c.httpClient.Get(ctx, "/v1/checkpoints/"+snapshotRef)
			if err != nil {
				continue
			}
			// Close body immediately, not with defer (avoid leak in loop)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true, nil
			}
		}
	}
	return false, fmt.Errorf("snapshot confirmation timeout after %d retries", maxRetries)
}

// ReportEvent reports an event to Hub (POST /v1/nodes/{node_id}/events)
func (c *Client) ReportEvent(ctx context.Context, event types.NodeEvent) error {
	path := fmt.Sprintf("/v1/nodes/%s/events", c.nodeID)
	resp, err := c.httpClient.PostJSON(ctx, path, event)
	if err != nil {
		return &errors.ErrHubCommunication{Operation: "report_event", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &errors.ErrHubCommunication{
			Operation: "report_event",
			Err:       fmt.Errorf("status code: %d", resp.StatusCode),
		}
	}
	return nil
}

// ReportAgentFailure reports agent crash to Hub
func (c *Client) ReportAgentFailure(ctx context.Context, agentID string, reason string) error {
	return c.ReportEvent(ctx, types.NodeEvent{
		EventType: "agent_crashed",
		AgentID:   agentID,
		Reason:    reason,
		Timestamp: time.Now().Unix(),
	})
}

// recordFailure records a failure and updates status.
// After 3 consecutive failures the node is marked offline.
// Also resets successCount so the hysteresis counter restarts.
func (c *Client) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failureCount++
	c.successCount = 0 // reset hysteresis
	if c.failureCount >= 3 {
		c.status = "offline"
	}
}

// SuccessThreshold is the number of consecutive successful heartbeats
// required before transitioning from "offline" back to "online".
// This prevents status flapping when the network is intermittently failing.
const SuccessThreshold = 3

// recordSuccess records a success. If the node was offline, it requires
// SuccessThreshold consecutive successes before going back to "online"
// to prevent rapid status flapping.
func (c *Client) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failureCount = 0
	c.successCount++
	if c.status == "online" || c.successCount >= SuccessThreshold {
		c.status = "online"
		c.successCount = 0
	}
}

// ReportMigrationFailure reports migration failure to Hub
func (c *Client) ReportMigrationFailure(ctx context.Context, agentID string, reason string) error {
	return c.ReportEvent(ctx, types.NodeEvent{
		EventType: "migration_failed",
		AgentID:   agentID,
		Reason:    reason,
		Timestamp: time.Now().Unix(),
	})
}

// ReportCheckpointComplete reports checkpoint completion to Hub
func (c *Client) ReportCheckpointComplete(ctx context.Context, agentID string, snapshotRef string) error {
	return c.ReportEvent(ctx, types.NodeEvent{
		EventType:   "checkpoint_complete",
		AgentID:     agentID,
		SnapshotRef: snapshotRef,
		Timestamp:   time.Now().Unix(),
	})
}

// getNodePublicKey returns node's public key from wallet
func (c *Client) getNodePublicKey() string {
	// Try to load from wallet first
	wallet, err := crypto.LoadWallet()
	if err == nil {
		return wallet.GetPublicKeyHex()
	}

	// Default placeholder
	return "0x00"
}

// FetchSigningKey fetches the hub's command-signing public key from GET /v1/system/signing-key.
// Returns the hex-encoded uncompressed secp256k1 public key, or an error.
func (c *Client) FetchSigningKey(ctx context.Context) (string, error) {
	resp, err := c.httpClient.Get(ctx, "/v1/system/signing-key")
	if err != nil {
		return "", fmt.Errorf("failed to fetch hub signing key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("hub returned status %d for signing-key endpoint", resp.StatusCode)
	}

	var result struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode signing key response: %w", err)
	}
	if result.PublicKey == "" {
		return "", fmt.Errorf("hub returned empty signing key")
	}
	return result.PublicKey, nil
}

// SetAuthToken updates the authorization token
func (c *Client) SetAuthToken(token string) {
	if token != "" {
		c.httpClient.SetHeader("Authorization", "Bearer "+token)
	}
}

// IsConnected returns true when the hub connection is healthy (no recent failures).
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status == "online"
}

// FailureCount returns the consecutive heartbeat failure count.
func (c *Client) FailureCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failureCount
}

// ReportProbeResponse sends a probe_response event to the hub with the given nonce.
func (c *Client) ReportProbeResponse(ctx context.Context, agentID, nonce string) error {
	event := types.NodeEvent{
		EventType: "probe_response",
		AgentID:   agentID,
		Reason:    nonce, // nonce carried in the reason field
		Timestamp: time.Now().Unix(),
	}
	path := fmt.Sprintf("/v1/nodes/%s/events", c.nodeID)
	resp, err := c.httpClient.PostJSON(ctx, path, event)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probe_response: status %d", resp.StatusCode)
	}
	return nil
}

// RuntimeImage represents a single entry from the hub's runtime image catalog.
type RuntimeImage struct {
	Image   string `json:"image"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
}

// FetchRuntimeImage fetches the default runtime image from the hub's image catalog.
// Returns the Docker image reference (e.g. "alpine/openclaw:main") that this node should use.
func (c *Client) FetchRuntimeImage(ctx context.Context) (string, error) {
	resp, err := c.httpClient.Get(ctx, "/v1/runtime/images")
	if err != nil {
		return "", fmt.Errorf("failed to fetch runtime images: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("hub returned status %d for runtime images endpoint", resp.StatusCode)
	}

	var result struct {
		Images []RuntimeImage `json:"images"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode runtime images response: %w", err)
	}

	if len(result.Images) == 0 {
		return "", fmt.Errorf("hub returned empty runtime image catalog")
	}

	// Find the default image
	for _, img := range result.Images {
		if img.Default && img.Image != "" {
			return img.Image, nil
		}
	}

	// No default found, use the first image
	if result.Images[0].Image != "" {
		return result.Images[0].Image, nil
	}

	return "", fmt.Errorf("hub returned runtime images with no valid image reference")
}

// MiningBalance holds the ZION mining reward balance for a wallet.
type MiningBalance struct {
	Owner       string `json:"owner"`
	Balance     string `json:"balance"`
	TotalEarned string `json:"total_earned"`
}

// FetchMiningBalance fetches the ZION mining balance for the authenticated wallet.
func (c *Client) FetchMiningBalance(ctx context.Context) (*MiningBalance, error) {
	resp, err := c.httpClient.Get(ctx, "/v1/mining/balance")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mining balance: status %d", resp.StatusCode)
	}

	var result MiningBalance
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// MiningTransaction represents a single ZION reward event.
type MiningTransaction struct {
	TxID        int    `json:"tx_id"`
	Owner       string `json:"owner"`
	Type        string `json:"type"`
	Amount      string `json:"amount"`
	ReferenceID string `json:"reference_id"`
	Memo        string `json:"memo"`
	CreatedAt   string `json:"created_at"`
}

// MiningTransactionsResponse wraps the paginated response from the Hub.
type MiningTransactionsResponse struct {
	Data       []MiningTransaction `json:"data"`
	Pagination struct {
		Page  int `json:"page"`
		Limit int `json:"limit"`
		Total int `json:"total"`
	} `json:"pagination"`
}

// FetchMiningTransactions fetches paginated ZION reward history.
func (c *Client) FetchMiningTransactions(ctx context.Context, page, limit int) (*MiningTransactionsResponse, error) {
	url := fmt.Sprintf("/v1/mining/transactions?page=%d&limit=%d", page, limit)
	resp, err := c.httpClient.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mining transactions: status %d", resp.StatusCode)
	}

	var result MiningTransactionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// selfBinaryHash returns the SHA-256 hash of the running binary for attestation.
// Returns empty string if the hash cannot be computed (non-fatal).
func selfBinaryHash() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
