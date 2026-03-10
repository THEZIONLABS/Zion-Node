package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/zion-protocol/zion-node/pkg/types"
)

// MockHub simulates Zion Hub for testing
type MockHub struct {
	server        *httptest.Server
	mu            sync.RWMutex
	heartbeats    []types.Heartbeat
	commands      map[string]*types.HubCommand // agentID -> command
	uploads       map[string][]byte            // snapshotRef -> data
	downloads     map[string][]byte            // snapshotRef -> data
	confirmations map[string]bool              // snapshotRef -> confirmed
	failures      []AgentFailure
	signingKey    *TestSigningKeyPair // optional: signs commands before returning
	runtimeImages []RuntimeImageEntry // runtime image catalog
}

// AgentFailure represents a reported agent failure
type AgentFailure struct {
	AgentID string
	Reason  string
}

// RuntimeImageEntry represents a runtime image in the hub catalog
type RuntimeImageEntry struct {
	Image   string `json:"image"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
}

// NewMockHub creates a new mock Hub server
func NewMockHub() *MockHub {
	m := &MockHub{
		commands:      make(map[string]*types.HubCommand),
		uploads:       make(map[string][]byte),
		downloads:     make(map[string][]byte),
		confirmations: make(map[string]bool),
		failures:      []AgentFailure{},
		runtimeImages: []RuntimeImageEntry{
			{Image: "alpine/openclaw:main", Label: "OpenClaw Latest (stable)", Default: true},
			{Image: "alpine/openclaw:1.0.0-beta", Label: "OpenClaw v1.0.0 (beta)", Default: false},
		},
	}

	mux := http.NewServeMux()
	// Node registration: POST /v1/nodes (must be before /v1/nodes/ catch-all)
	mux.HandleFunc("/v1/nodes", m.handleRegister)
	// Heartbeat: POST /v1/nodes/{node_id}/heartbeat
	mux.HandleFunc("/v1/nodes/", m.handleNodes)
	// Snapshots: /v1/checkpoints/{snapshot_ref} and /v1/checkpoints/{snapshot_ref}/download
	mux.HandleFunc("/v1/checkpoints/", m.handleCheckpoint)
	mux.HandleFunc("/v1/nodes/snapshots/upload", m.handleUploadSnapshot)
	mux.HandleFunc("/v1/auth/challenge", m.handleAuthChallenge)
	mux.HandleFunc("/v1/system/signing-key", m.handleSigningKey)
	mux.HandleFunc("/v1/runtime/images", m.handleRuntimeImages)
	mux.HandleFunc("/health", m.handleHealth)

	m.server = httptest.NewServer(mux)
	return m
}

// URL returns the mock Hub URL
func (m *MockHub) URL() string {
	return m.server.URL
}

// Close closes the mock server
func (m *MockHub) Close() {
	m.server.Close()
}

// SetSigningKey configures the MockHub to sign all commands it returns
// using the given key pair. When set, commands returned via heartbeat
// will have valid Signature and SignedAt fields, enabling full E2E
// testing with signature verification enabled on the node.
func (m *MockHub) SetSigningKey(kp *TestSigningKeyPair) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.signingKey = kp
}

// SetCommand sets a command to be returned for an agent
func (m *MockHub) SetCommand(agentID string, cmd *types.HubCommand) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands[agentID] = cmd
}

// GetHeartbeats returns all received heartbeats
func (m *MockHub) GetHeartbeats() []types.Heartbeat {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.heartbeats
}

// GetFailures returns all reported failures
func (m *MockHub) GetFailures() []AgentFailure {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.failures
}

// ResetState clears all accumulated state (heartbeats, failures, commands, etc.)
// Use between tests or subtests to avoid cross-contamination.
func (m *MockHub) ResetState() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeats = nil
	m.commands = make(map[string]*types.HubCommand)
	m.uploads = make(map[string][]byte)
	m.downloads = make(map[string][]byte)
	m.confirmations = make(map[string]bool)
	m.failures = nil
	m.runtimeImages = []RuntimeImageEntry{
		{Image: "alpine/openclaw:main", Label: "OpenClaw Latest (stable)", Default: true},
		{Image: "alpine/openclaw:1.0.0-beta", Label: "OpenClaw v1.0.0 (beta)", Default: false},
	}
}

// GetUploads returns all uploaded snapshots
func (m *MockHub) GetUploads() map[string][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string][]byte, len(m.uploads))
	for k, v := range m.uploads {
		result[k] = v
	}
	return result
}

// HeartbeatCount returns number of heartbeats received
func (m *MockHub) HeartbeatCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.heartbeats)
}

// ConfirmSnapshot confirms a snapshot
func (m *MockHub) ConfirmSnapshot(snapshotRef string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.confirmations[snapshotRef] = true
}

// handleNodes routes /v1/nodes/{node_id}/heartbeat and /v1/nodes/{node_id}/events
func (m *MockHub) handleNodes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if len(path) > len("/v1/nodes/") {
		suffix := path[len("/v1/nodes/"):]
		// Check for heartbeat endpoint
		if idx := len(suffix) - len("/heartbeat"); idx > 0 && suffix[idx:] == "/heartbeat" {
			m.handleHeartbeat(w, r)
			return
		}
		// Check for events endpoint
		if idx := len(suffix) - len("/events"); idx > 0 && suffix[idx:] == "/events" {
			m.handleEvents(w, r)
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (m *MockHub) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var hb types.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.heartbeats = append(m.heartbeats, hb)
	// Collect commands for agents
	var cmds []types.HubCommand
	for _, agent := range hb.Agents {
		if c, ok := m.commands[agent.AgentID]; ok {
			cmds = append(cmds, *c)
			delete(m.commands, agent.AgentID)
		}
	}
	// Also check for commands with new agent IDs (not yet in heartbeat)
	for agentID, c := range m.commands {
		cmds = append(cmds, *c)
		delete(m.commands, agentID)
	}

	// Sign commands if signing key is configured
	if m.signingKey != nil {
		for i := range cmds {
			m.signingKey.SignCommand(&cmds[i])
		}
	}
	m.mu.Unlock()

	// Return HeartbeatResponse format
	resp := types.HeartbeatResponse{
		Ack:        true,
		ServerTime: hb.Timestamp,
		Commands:   cmds,
	}
	json.NewEncoder(w).Encode(resp)
}

func (m *MockHub) handleEvents(w http.ResponseWriter, r *http.Request) {
	var event types.NodeEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.failures = append(m.failures, AgentFailure{
		AgentID: event.AgentID,
		Reason:  event.EventType + ": " + event.Reason,
	})
	m.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

func (m *MockHub) handleUploadSnapshot(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	snapshotRef := r.FormValue("snapshot_ref")
	file, _, err := r.FormFile("snapshot")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	data := make([]byte, r.ContentLength)
	if _, err := file.Read(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	m.mu.Lock()
	m.uploads[snapshotRef] = data
	m.mu.Unlock()

	// Return S3 URI
	json.NewEncoder(w).Encode(map[string]string{
		"uri": fmt.Sprintf("s3://test-bucket/%s", snapshotRef),
	})
}

// handleCheckpoint handles /v1/checkpoints/{snapshot_ref} and /v1/checkpoints/{snapshot_ref}/download
func (m *MockHub) handleCheckpoint(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	snapshotRef := path[len("/v1/checkpoints/"):]

	// Route by path suffix
	if len(snapshotRef) >= len("/download") && snapshotRef[len(snapshotRef)-len("/download"):] == "/download" {
		// GET /v1/checkpoints/{snapshot_ref}/download - return presigned URL
		snapshotRef = snapshotRef[:len(snapshotRef)-len("/download")]
		m.mu.RLock()
		_, ok := m.downloads[snapshotRef]
		m.mu.RUnlock()

		if !ok {
			http.Error(w, "snapshot not found", http.StatusNotFound)
			return
		}
		// Return presigned URL pointing to test endpoint
		json.NewEncoder(w).Encode(map[string]interface{}{
			"download_url": m.server.URL + "/v1/checkpoints/" + snapshotRef + "/data",
			"expires_in":   3600,
		})
	} else if len(snapshotRef) >= len("/data") && snapshotRef[len(snapshotRef)-len("/data"):] == "/data" {
		// GET /v1/checkpoints/{snapshot_ref}/data - actual download (simulates S3)
		snapshotRef = snapshotRef[:len(snapshotRef)-len("/data")]
		m.mu.RLock()
		data, ok := m.downloads[snapshotRef]
		m.mu.RUnlock()

		if !ok {
			http.Error(w, "snapshot not found", http.StatusNotFound)
			return
		}
		w.Write(data)
	} else {
		// GET /v1/checkpoints/{snapshot_ref} - check if snapshot exists
		m.mu.RLock()
		confirmed := m.confirmations[snapshotRef]
		m.mu.RUnlock()

		if confirmed {
			json.NewEncoder(w).Encode(map[string]string{"status": "available"})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (m *MockHub) handleSigningKey(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	kp := m.signingKey
	m.mu.RUnlock()

	if kp == nil {
		// No signing key configured — return 503 like the real hub
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "Hub signing key not configured"})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"public_key": kp.PublicKeyHex})
}

func (m *MockHub) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleRegister handles POST /v1/nodes (node registration)
func (m *MockHub) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var reg types.NodeRegistration
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp := types.NodeRegistrationResponse{
		NodeID: reg.NodeID,
		Region: "mock-region",
		Status: "online",
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// handleAuthChallenge handles GET /v1/auth/challenge (wallet login)
func (m *MockHub) handleAuthChallenge(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{
		"challenge": "mock-challenge-" + fmt.Sprintf("%d", time.Now().Unix()),
		"expires":   "2099-01-01T00:00:00Z",
	})
}

// SetSnapshotData sets snapshot data for download
func (m *MockHub) SetSnapshotData(snapshotRef string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.downloads[snapshotRef] = data
}

// SetRuntimeImages overrides the runtime image catalog returned by GET /v1/runtime/images
func (m *MockHub) SetRuntimeImages(images []RuntimeImageEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtimeImages = images
}

// handleRuntimeImages handles GET /v1/runtime/images
func (m *MockHub) handleRuntimeImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m.mu.RLock()
	images := m.runtimeImages
	m.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"images": images,
	})
}
