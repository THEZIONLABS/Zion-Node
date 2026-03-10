package types

import "time"

// AgentStatus represents agent runtime status
type AgentStatus string

const (
	AgentStatusRunning AgentStatus = "running"
	AgentStatusPaused  AgentStatus = "paused"
	AgentStatusStopped AgentStatus = "stopped"
	AgentStatusDead    AgentStatus = "dead"
)

// Agent represents a running agent
type Agent struct {
	AgentID       string         `json:"agent_id"`
	ContainerID   string         `json:"container_id"`
	Status        AgentStatus    `json:"status"`
	StartedAt     time.Time      `json:"started_at"`
	Profile       RuntimeProfile `json:"profile"`
	RestartCount  int            `json:"restart_count"`
	LastFailure   time.Time      `json:"last_failure,omitempty"`
	FailureReason string         `json:"failure_reason,omitempty"`
}

// RuntimeProfile defines OpenClaw runtime configuration
// See: docs/specs/core-objects.md
type RuntimeProfile struct {
	Engine             string `json:"engine"`               // "openclaw"
	EngineVersion      string `json:"engine_version"`       // e.g., "1.2.3"
	ImageHash          string `json:"image_hash"`           // SHA-256 of container image
	SkillsManifestHash string `json:"skills_manifest_hash"` // SHA-256 of skills manifest
	SnapshotFormat     string `json:"snapshot_format"`      // "openclaw-snapshot/v1"
}

// SnapshotRef is a content-addressed snapshot reference
// See: docs/specs/core-objects.md
type SnapshotRef struct {
	Ref       string    `json:"snapshot_ref"` // sha256:abc123...
	URI       string    `json:"uri"`          // s3://bucket/path
	Size      int64     `json:"size"`         // bytes
	CreatedAt time.Time `json:"created_at"`
	Checksum  string    `json:"checksum"` // crc32:12345678 for transfer verification
}

// Heartbeat request sent to Hub (POST /v1/nodes/{node_id}/heartbeat)
type Heartbeat struct {
	Timestamp int64        `json:"timestamp"`
	Status    string       `json:"status"` // online, offline, draining
	Capacity  CapacityInfo `json:"capacity"`
	Agents    []AgentInfo  `json:"agents"` // Always send, even if empty — omitempty would skip reconciliation on Hub
}

// HeartbeatResponse from Hub
type HeartbeatResponse struct {
	Ack        bool         `json:"ack"`
	ServerTime int64        `json:"server_time"`
	Commands   []HubCommand `json:"commands"`
}

// CapacityInfo represents node capacity
type CapacityInfo struct {
	TotalSlots   int     `json:"total_slots"`
	UsedSlots    int     `json:"used_slots"`
	CPUUsedCores float64 `json:"cpu_used_cores,omitempty"`
	MemoryUsedMB int     `json:"memory_used_mb,omitempty"`
}

// AgentInfo represents agent status in heartbeat
type AgentInfo struct {
	AgentID   string `json:"agent_id"`
	Status    string `json:"status"`
	UptimeSec int64  `json:"uptime_sec,omitempty"`
}

// HubCommand received from Hub heartbeat response
type HubCommand struct {
	Command   string                 `json:"command"` // run, stop, checkpoint
	AgentID   string                 `json:"agent_id"`
	Params    map[string]interface{} `json:"params,omitempty"`    // Additional params like profile, snapshot_ref
	Signature string                 `json:"signature,omitempty"` // ECDSA secp256k1 signature from hub
	SignedAt  int64                  `json:"signed_at,omitempty"` // Unix timestamp when command was signed
}

// NodeEvent sent to Hub (POST /v1/nodes/{node_id}/events)
type NodeEvent struct {
	EventType   string `json:"event_type"` // agent_started, agent_stopped, agent_crashed, checkpoint_complete, etc.
	AgentID     string `json:"agent_id,omitempty"`
	Reason      string `json:"reason,omitempty"`
	SnapshotRef string `json:"snapshot_ref,omitempty"`
	Timestamp   int64  `json:"timestamp"`
}

// ContainerStats represents container statistics
type ContainerStats struct {
	CPUPercent   float64 `json:"cpu_percent"`
	MemoryMB     int64   `json:"memory_mb"`
	OOMKilled    bool    `json:"oom_killed"`
	CPUThrottled float64 `json:"cpu_throttled"`
}

// ContainerStatus represents the state of a Docker container
type ContainerStatus struct {
	Running    bool   `json:"running"`
	ExitCode   int    `json:"exit_code"`
	OOMKilled  bool   `json:"oom_killed"`
	FinishedAt string `json:"finished_at"`
	Error      string `json:"error,omitempty"`
}

// RuntimeInfo represents detailed runtime information
type RuntimeInfo struct {
	Engine      string `json:"engine"`                 // e.g., "openclaw"
	ImageRef    string `json:"image_ref"`              // e.g., "alpine/openclaw:main"
	ImageDigest string `json:"image_digest,omitempty"` // e.g., "sha256:abc123..."
	PulledAt    string `json:"pulled_at,omitempty"`    // ISO 8601 timestamp
}

// NodeRegistration request sent to Hub (POST /v1/nodes)
type NodeRegistration struct {
	NodeID            string        `json:"node_id"`
	PublicKey         string        `json:"public_key"`
	CPUCores          int           `json:"cpu_cores"`
	MemoryGB          int           `json:"memory_gb"`
	DiskGB            int           `json:"disk_gb"`
	SystemCPU         int           `json:"system_cpu,omitempty"`
	SystemMemoryMB    int           `json:"system_memory_mb,omitempty"`
	TotalSlots        int           `json:"total_slots,omitempty"`
	BinaryHash        string        `json:"binary_hash,omitempty"` // SHA-256 of the node binary for attestation
	SupportedRuntimes []RuntimeInfo `json:"supported_runtimes,omitempty"`
}

// NodeRegistrationResponse from Hub
type NodeRegistrationResponse struct {
	NodeID            string   `json:"node_id"`
	PublicKey         string   `json:"public_key"`
	Region            string   `json:"region"`
	Status            string   `json:"status"`
	IP                string   `json:"ip"`
	CPUCores          int      `json:"cpu_cores"`
	MemoryGB          int      `json:"memory_gb"`
	DiskGB            int      `json:"disk_gb"`
	TotalSlots        int      `json:"total_slots"`
	UsedSlots         int      `json:"used_slots"`
	SupportedRuntimes []string `json:"supported_runtimes"`
	LastHeartbeatAt   string   `json:"last_heartbeat_at"`
	RegisteredAt      string   `json:"registered_at"`
	UpdatedAt         string   `json:"updated_at"`
}
