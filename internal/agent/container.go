package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/config"
	"github.com/zion-protocol/zion-node/internal/errors"
	"github.com/zion-protocol/zion-node/internal/utils"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// ContainerManager manages Docker containers
type ContainerManager interface {
	Create(ctx context.Context, agentID string, profile types.RuntimeProfile, snapshotRef string, extraEnv map[string]string) (string, error)
	Start(ctx context.Context, containerID string) error
	Stop(ctx context.Context, containerID string) error
	Remove(ctx context.Context, containerID string) error
	List(ctx context.Context, prefix string) ([]dockertypes.Container, error)
	Inspect(ctx context.Context, containerID string) (*types.ContainerStatus, error)
	Stats(ctx context.Context, containerID string) (*types.ContainerStats, error)
	EnsureImage(ctx context.Context) error
	GetImageDigest(ctx context.Context) (string, error)
	Pause(ctx context.Context, containerID string) error
	Resume(ctx context.Context, containerID string) error
}

// DockerManager implements ContainerManager using Docker SDK
type DockerManager struct {
	client *client.Client
	cfg    *config.Config
	logger *logrus.Logger
}

// Close closes the Docker client
func (d *DockerManager) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

// NewDockerManager creates a new Docker manager
func NewDockerManager(cfg *config.Config, logger *logrus.Logger) (*DockerManager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	return &DockerManager{
		client: cli,
		cfg:    cfg,
		logger: logger,
	}, nil
}

// EnsureImage checks if image exists and pulls if needed
func (d *DockerManager) EnsureImage(ctx context.Context) error {
	images, err := d.client.ImageList(ctx, dockertypes.ImageListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", d.cfg.RuntimeImage)),
	})
	if err != nil {
		return err
	}

	if len(images) == 0 {
		// Image not found, pull it
		d.logger.WithField("image", d.cfg.RuntimeImage).Info("Image not found, pulling...")
		reader, err := d.client.ImagePull(ctx, d.cfg.RuntimeImage, dockertypes.ImagePullOptions{})
		if err != nil {
			return fmt.Errorf("failed to pull image %s: %w (check if image exists and is accessible)", d.cfg.RuntimeImage, err)
		}
		defer reader.Close()

		// Wait for pull to complete and check for errors
		// Docker ImagePull returns errors in the response stream
		var pullErr error
		decoder := json.NewDecoder(reader)
		for {
			var msg struct {
				Status   string `json:"status"`
				Error    string `json:"error"`
				Progress string `json:"progress"`
			}
			if err := decoder.Decode(&msg); err != nil {
				if err == io.EOF {
					break
				}
				pullErr = err
				break
			}
			// Check for error in stream
			if msg.Error != "" {
				pullErr = fmt.Errorf("docker pull error: %s", msg.Error)
				break
			}
		}

		if pullErr != nil {
			return fmt.Errorf("failed to pull image %s: %w (for private images, configure Docker credentials)", d.cfg.RuntimeImage, pullErr)
		}

		d.logger.WithField("image", d.cfg.RuntimeImage).Info("Image pulled successfully")
	}

	return nil
}

// GetImageDigest returns the digest of the runtime image
// Returns the RepoDigest if available (e.g., "sha256:abc123..."), otherwise the local image ID
func (d *DockerManager) GetImageDigest(ctx context.Context) (string, error) {
	inspect, _, err := d.client.ImageInspectWithRaw(ctx, d.cfg.RuntimeImage)
	if err != nil {
		return "", fmt.Errorf("failed to inspect image %s: %w", d.cfg.RuntimeImage, err)
	}

	// Prefer RepoDigests (content-addressed, immutable)
	if len(inspect.RepoDigests) > 0 {
		// RepoDigests format: "registry/repo@sha256:abc123..."
		// Extract just the "sha256:xxx" part
		parts := strings.Split(inspect.RepoDigests[0], "@")
		if len(parts) == 2 {
			return parts[1], nil
		}
	}

	// Fallback: use local image ID (not as precise, but better than nothing)
	// Remove "sha256:" prefix if present for consistency
	imageID := inspect.ID
	if strings.HasPrefix(imageID, "sha256:") {
		return imageID, nil
	}
	return "sha256:" + imageID, nil
}

// Create creates a new container
func (d *DockerManager) Create(ctx context.Context, agentID string, profile types.RuntimeProfile, snapshotRef string, extraEnv map[string]string) (string, error) {
	// Ensure image exists
	if err := d.EnsureImage(ctx); err != nil {
		return "", fmt.Errorf("failed to ensure image: %w", err)
	}

	containerName := fmt.Sprintf("zion-agent-%s", agentID)

	// Check if container with this name already exists
	// This handles edge cases where previous cleanup failed
	// Use exact match regex: ^/container-name$ (Docker stores names with leading /)
	existingContainers, err := d.client.ContainerList(ctx, container.ListOptions{
		All: true, // Include stopped containers
		Filters: filters.NewArgs(
			filters.Arg("name", "^/"+containerName+"$"),
		),
	})
	if err != nil {
		d.logger.WithError(err).Warn("Failed to check for existing container")
	} else if len(existingContainers) > 0 {
		// Found existing container with same name, clean it up
		for _, existing := range existingContainers {
			d.logger.WithFields(logrus.Fields{
				"agent_id":     agentID,
				"container_id": existing.ID,
				"status":       existing.State,
				"name":         existing.Names,
			}).Warn("Found existing container with same name, removing it")

			// Try to stop first (ignore errors if already stopped)
			_ = d.client.ContainerStop(ctx, existing.ID, container.StopOptions{})

			// Remove the container
			if err := d.client.ContainerRemove(ctx, existing.ID, container.RemoveOptions{
				Force: true,
			}); err != nil {
				d.logger.WithError(err).WithField("container_id", existing.ID).Error("Failed to remove existing container")
				return "", fmt.Errorf("failed to remove existing container %s: %w", existing.ID, err)
			}
		}
	}

	// Agent data directory
	dataDir := utils.AgentDataDir(d.cfg.DataDir, agentID)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", &errors.ErrContainerOperation{
			Operation:   "create_data_dir",
			ContainerID: "",
			Err:         err,
		}
	}

	// Ensure openclaw config has required settings for gateway and channel plugins.
	// - gateway.mode must be "local" for the gateway to start
	// - channel plugins (telegram, discord, etc.) are bundled but disabled by
	//   default in openclaw; we enable only the ones the agent actually uses
	//   (detected via extraEnv keys set by buildAgentEnv).
	openclawDir := fmt.Sprintf("%s/.openclaw", dataDir)
	openclawCfg := fmt.Sprintf("%s/openclaw.json", openclawDir)
	if err := os.MkdirAll(openclawDir, 0755); err != nil {
		d.logger.WithError(err).Warn("Failed to create .openclaw config dir")
	} else {
		existing := map[string]interface{}{}
		if raw, err := os.ReadFile(openclawCfg); err == nil {
			_ = json.Unmarshal(raw, &existing)
		}
		// Ensure gateway.mode = "local"
		if gw, ok := existing["gateway"].(map[string]interface{}); ok {
			if _, hasMode := gw["mode"]; !hasMode {
				gw["mode"] = "local"
			}
		} else {
			existing["gateway"] = map[string]interface{}{"mode": "local"}
		}
		// Set default model from OPENCLAW_DEFAULT_MODEL env var.
		// openclaw reads model from config (agents.defaults.model), not env vars.
		if defaultModel, ok := extraEnv["OPENCLAW_DEFAULT_MODEL"]; ok && defaultModel != "" {
			agents, _ := existing["agents"].(map[string]interface{})
			if agents == nil {
				agents = map[string]interface{}{}
			}
			defaults, _ := agents["defaults"].(map[string]interface{})
			if defaults == nil {
				defaults = map[string]interface{}{}
			}
			defaults["model"] = map[string]interface{}{"primary": defaultModel}
			agents["defaults"] = defaults
			existing["agents"] = agents
		}
		// Enable channel plugins based on which env vars are present,
		// and set dmPolicy to "open" so users can chat without pairing approval.
		envToPlugin := map[string]string{
			"TELEGRAM_BOT_TOKEN": "telegram",
			"DISCORD_BOT_TOKEN":  "discord",
			"SLACK_BOT_TOKEN":    "slack",
			"FEISHU_APP_ID":      "feishu",
		}
		var channelPlugins []string
		for envKey, pluginID := range envToPlugin {
			if v, ok := extraEnv[envKey]; ok && v != "" {
				channelPlugins = append(channelPlugins, pluginID)
			}
		}
		if len(channelPlugins) > 0 {
			plugins, _ := existing["plugins"].(map[string]interface{})
			if plugins == nil {
				plugins = map[string]interface{}{}
			}
			entries, _ := plugins["entries"].(map[string]interface{})
			if entries == nil {
				entries = map[string]interface{}{}
			}
			for _, p := range channelPlugins {
				entries[p] = map[string]interface{}{"enabled": true}
			}
			plugins["entries"] = entries
			existing["plugins"] = plugins

			// Set channel-specific defaults (open DM policy, open group policy)
			channels, _ := existing["channels"].(map[string]interface{})
			if channels == nil {
				channels = map[string]interface{}{}
			}
			for _, p := range channelPlugins {
				ch, _ := channels[p].(map[string]interface{})
				if ch == nil {
					ch = map[string]interface{}{}
				}
				ch["enabled"] = true
				ch["dmPolicy"] = "open"
				ch["allowFrom"] = []interface{}{"*"}

				// Feishu plugin needs accounts config with appId/appSecret
				// in openclaw.json — env vars alone are not enough.
				if p == "feishu" {
					appId, _ := extraEnv["FEISHU_APP_ID"]
					appSecret, _ := extraEnv["FEISHU_APP_SECRET"]
					if appId != "" && appSecret != "" {
						ch["accounts"] = map[string]interface{}{
							"default": map[string]interface{}{
								"appId":     appId,
								"appSecret": appSecret,
							},
						}
						ch["defaultAccount"] = "default"
					}
				}

				channels[p] = ch
			}
			existing["channels"] = channels
		}

		// Skills allowlist → openclaw.json skills.allowBundled
		// Hub already resolves frontend keys to openclaw dir names, so we write directly.
		if skillsJSON, ok := extraEnv["ZION_SKILLS_CONFIG"]; ok && skillsJSON != "" {
			var openclawSkills []interface{}
			if err := json.Unmarshal([]byte(skillsJSON), &openclawSkills); err == nil && len(openclawSkills) > 0 {
				skills, _ := existing["skills"].(map[string]interface{})
				if skills == nil {
					skills = map[string]interface{}{}
				}
				skills["allowBundled"] = openclawSkills
				existing["skills"] = skills
			}
			delete(extraEnv, "ZION_SKILLS_CONFIG")
		}

		if data, err := json.MarshalIndent(existing, "", "  "); err == nil {
			if err := os.WriteFile(openclawCfg, data, 0644); err != nil {
				d.logger.WithError(err).Warn("Failed to write openclaw seed config")
			}
			_ = os.Chown(openclawDir, 1000, 1000)
			_ = os.Chown(openclawCfg, 1000, 1000)
		}
	}

	// Automations → cron/jobs.json for OpenClaw scheduler
	if automationsJSON, ok := extraEnv["ZION_AUTOMATIONS_CONFIG"]; ok && automationsJSON != "" {
		var automations []map[string]interface{}
		if err := json.Unmarshal([]byte(automationsJSON), &automations); err == nil && len(automations) > 0 {
			cronDir := fmt.Sprintf("%s/.openclaw/cron", dataDir)
			if err := os.MkdirAll(cronDir, 0755); err != nil {
				d.logger.WithError(err).Warn("Failed to create cron dir")
			} else {
				var cronJobs []map[string]interface{}
				for _, a := range automations {
					id, _ := a["id"].(string)
					cron, _ := a["cron"].(string)
					message, _ := a["message"].(string)
					if id == "" || cron == "" || message == "" {
						continue
					}
					cronJobs = append(cronJobs, map[string]interface{}{
						"id":            fmt.Sprintf("zion-%s", id),
						"name":          "Zion Automation",
						"schedule":      map[string]interface{}{"kind": "cron", "expr": cron},
						"sessionTarget": "isolated",
						"payload":       map[string]interface{}{"kind": "agentTurn", "message": message},
						"enabled":       true,
					})
				}
				if len(cronJobs) > 0 {
					if data, err := json.MarshalIndent(cronJobs, "", "  "); err == nil {
						jobsFile := fmt.Sprintf("%s/jobs.json", cronDir)
						if err := os.WriteFile(jobsFile, data, 0644); err != nil {
							d.logger.WithError(err).Warn("Failed to write cron/jobs.json")
						}
						_ = os.Chown(cronDir, 1000, 1000)
						_ = os.Chown(jobsFile, 1000, 1000)
					}
				}
			}
		}
		delete(extraEnv, "ZION_AUTOMATIONS_CONFIG")
	}

	// Container configuration
	containerConfig := &container.Config{
		Image: d.cfg.RuntimeImage,
		Labels: map[string]string{
			"zion.node":  "true",
			"agent.id":   agentID,
			"managed.by": "zion-node",
		},
		User: "1000:1000", // Non-root
		Env: func() []string {
			env := []string{
				"OPENCLAW_GATEWAY_TOKEN=" + agentID,
				"OPENCLAW_GATEWAY_AUTH=none",
				"OPENCLAW_HOME=/data",
				fmt.Sprintf("NODE_OPTIONS=--max-old-space-size=%d", d.cfg.MemoryPerAgent*3/4),
			}
			for k, v := range extraEnv {
				env = append(env, k+"="+v)
			}
			return env
		}(),
	}

	pidsLimit := int64(256)
	hostConfig := &container.HostConfig{
		Tmpfs: map[string]string{
			"/tmp":       "rw,noexec,nosuid,size=1g",
			"/state":     "rw,noexec,nosuid",
			"/workspace": "rw,noexec,nosuid",
		},
		Resources: container.Resources{
			NanoCPUs:   int64(d.cfg.CPUPerAgent) * 1e9, // CPU limit (allows overcommit)
			Memory:     int64(d.cfg.MemoryPerAgent) * 1024 * 1024,
			MemorySwap: int64(d.cfg.MemoryPerAgent) * 1024 * 1024 * 2,
			PidsLimit:  &pidsLimit,
		},
		NetworkMode: "bridge",
		RestartPolicy: container.RestartPolicy{
			Name: "no", // No auto-restart, controlled by Node
		},
		SecurityOpt: []string{"no-new-privileges:true"},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: dataDir,
				Target: "/data",
			},
		},
	}

	// Create container
	resp, err := d.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

// Start starts a container
func (d *DockerManager) Start(ctx context.Context, containerID string) error {
	return d.client.ContainerStart(ctx, containerID, container.StartOptions{})
}

// Stop stops a container
func (d *DockerManager) Stop(ctx context.Context, containerID string) error {
	timeout := 10
	return d.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

// Remove removes a container
func (d *DockerManager) Remove(ctx context.Context, containerID string) error {
	return d.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

// List lists containers with prefix
func (d *DockerManager) List(ctx context.Context, prefix string) ([]dockertypes.Container, error) {
	return d.client.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", prefix)),
		All:     true,
	})
}

// Inspect returns the current status of a container
func (d *DockerManager) Inspect(ctx context.Context, containerID string) (*types.ContainerStatus, error) {
	info, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}
	status := &types.ContainerStatus{
		Running: info.State.Running,
	}
	if info.State != nil {
		status.ExitCode = info.State.ExitCode
		status.OOMKilled = info.State.OOMKilled
		status.FinishedAt = info.State.FinishedAt
		status.Error = info.State.Error
	}
	return status, nil
}

// Stats gets container statistics
func (d *DockerManager) Stats(ctx context.Context, containerID string) (*types.ContainerStats, error) {
	stats, err := d.client.ContainerStats(ctx, containerID, false)
	if err != nil {
		return nil, err
	}
	defer stats.Body.Close()

	var v dockertypes.StatsJSON
	if err := json.NewDecoder(stats.Body).Decode(&v); err != nil {
		return nil, err
	}

	// Calculate CPU usage
	cpuPercent := calculateCPUPercent(&v)

	// Memory usage
	memoryMB := v.MemoryStats.Usage / 1024 / 1024

	// OOM check
	// Docker API reports OOM in MemoryStats.Stats map[string]uint64
	oomKilled := false
	if v.MemoryStats.Stats != nil {
		if oomKill, ok := v.MemoryStats.Stats["oom_kill"]; ok && oomKill > 0 {
			oomKilled = true
		}
	}

	// CPU throttling - check if throttling data exists
	var cpuThrottled float64
	// Note: CPU throttling data structure may vary by Docker version
	// If not available, default to 0

	return &types.ContainerStats{
		CPUPercent:   cpuPercent,
		MemoryMB:     int64(memoryMB),
		OOMKilled:    oomKilled,
		CPUThrottled: cpuThrottled,
	}, nil
}

// Pause pauses a container
func (d *DockerManager) Pause(ctx context.Context, containerID string) error {
	return d.client.ContainerPause(ctx, containerID)
}

// Resume resumes a paused container
func (d *DockerManager) Resume(ctx context.Context, containerID string) error {
	return d.client.ContainerUnpause(ctx, containerID)
}

func calculateCPUPercent(v *dockertypes.StatsJSON) float64 {
	// Calculate CPU usage percentage
	// This is a simplified version - full implementation would calculate delta
	if v.CPUStats.CPUUsage.TotalUsage > 0 && v.CPUStats.SystemUsage > 0 {
		cpuDelta := float64(v.CPUStats.CPUUsage.TotalUsage)
		systemDelta := float64(v.CPUStats.SystemUsage)
		if systemDelta > 0 {
			return (cpuDelta / systemDelta) * 100.0
		}
	}
	return 0.0
}
