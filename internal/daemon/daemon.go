package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/agent"
	"github.com/zion-protocol/zion-node/internal/config"
	"github.com/zion-protocol/zion-node/internal/crypto"
	"github.com/zion-protocol/zion-node/internal/errors"
	"github.com/zion-protocol/zion-node/internal/hub"
	"github.com/zion-protocol/zion-node/internal/logger"
	"github.com/zion-protocol/zion-node/internal/snapshot"
	"github.com/zion-protocol/zion-node/pkg/types"
)

// Version is set at build time via ldflags.
var Version = "dev"

// Daemon is the main daemon process
type Daemon struct {
	cfg              *config.Config
	agentManager     *agent.Manager
	containerMonitor *agent.ContainerMonitor
	snapshotEngine   *snapshot.Engine
	hubClient        *hub.Client
	logger           *logrus.Logger
	shuttingDown     atomic.Bool        // atomic to avoid data race
	cancelFunc       context.CancelFunc // set by Run(), used by shutdown command
	startTime        time.Time          // set when Run() begins

	// Protected by mu
	mu              sync.Mutex
	lastHeartbeatAt time.Time
	miningReward    string
	tokenExpiresAt  time.Time // JWT expiry parsed from token

	// Guards to prevent duplicate goroutines
	refreshingMining atomic.Int32 // CAS guard for refreshMiningReward
}

// Config returns the daemon's configuration.
func (d *Daemon) Config() *config.Config {
	return d.cfg
}

// NewDaemon creates a new daemon with a default logger.
func NewDaemon(cfg *config.Config) (*Daemon, error) {
	return NewDaemonWithLogger(cfg, nil)
}

// NewDaemonWithLogger creates a new daemon using the supplied logger.
// If log is nil a default logrus logger is created.
func NewDaemonWithLogger(cfg *config.Config, log *logrus.Logger) (*Daemon, error) {
	if log == nil {
		log = logger.NewLogrusLogger(cfg.LogLevel)
	}

	// Initialize Hub client
	hubClient := hub.NewClient(cfg)

	// Initialize state manager
	stateManager := agent.NewStateManager(cfg, log)
	if err := stateManager.Load(); err != nil {
		return nil, err
	}

	// Initialize container manager
	containerManager, err := agent.NewDockerManager(cfg, log)
	if err != nil {
		return nil, err
	}

	// Initialize snapshot engine first (needed by agent manager)
	// Note: containerManager is created above, pass it to snapshot engine
	snapshotEngine := snapshot.NewEngine(cfg, hubClient, containerManager, log)

	// Initialize agent manager
	agentManager, err := agent.NewManager(cfg, containerManager, stateManager, hubClient, snapshotEngine, log)
	if err != nil {
		return nil, err
	}

	// Recover from Docker
	if err := agentManager.RecoverFromDocker(context.Background()); err != nil {
		log.WithError(err).Warn("Failed to recover from Docker")
	}

	// Initialize container health monitor
	containerMonitor := agent.NewContainerMonitor(agentManager, log)

	return &Daemon{
		cfg:              cfg,
		agentManager:     agentManager,
		containerMonitor: containerMonitor,
		snapshotEngine:   snapshotEngine,
		hubClient:        hubClient,
		logger:           log,
	}, nil
}

// Run runs the daemon
func (d *Daemon) Run(ctx context.Context) error {
	// Wrap context with a cancel function so hub "shutdown" commands can stop the daemon
	ctx, cancel := context.WithCancel(ctx)
	d.cancelFunc = cancel
	d.startTime = time.Now()

	// Log node identity and configuration on startup
	d.logger.WithFields(logrus.Fields{
		"node_id":       d.cfg.NodeID,
		"hub_url":       d.cfg.HubURL,
		"max_agents":    d.cfg.MaxAgents,
		"cpu_per_agent": d.cfg.CPUPerAgent,
		"mem_per_agent": d.cfg.MemoryPerAgent,
		"runtime_image": d.cfg.RuntimeImage,
	}).Info("Starting zion-node")

	// Auto-authenticate with Hub if no token set
	if d.cfg.HubAuthToken == "" {
		d.logger.Info("No auth token found, attempting wallet login...")
		if err := d.autoLogin(ctx); err != nil {
			d.logger.WithError(err).Warn("Auto-login failed, will retry on registration failure")
		}
	}

	// Fetch hub's command-signing public key (used to verify hub commands)
	if d.cfg.HubPublicKey == "" {
		d.logger.Info("Fetching hub signing key...")
		if pubKey, err := d.hubClient.FetchSigningKey(ctx); err != nil {
			d.logger.WithError(err).Warn("Failed to fetch hub signing key — commands will not be verified")
		} else {
			d.cfg.HubPublicKey = pubKey
			d.logger.WithField("hub_public_key", pubKey[:16]+"...").Info("Hub signing key loaded")
		}
	}

	// Ensure runtime image is available and get digest
	containerMgr, ok := d.agentManager.GetContainerManager()
	if !ok {
		return fmt.Errorf("failed to get container manager")
	}

	// Fetch the runtime image from hub (hub is the authority for which image to use)
	d.logger.Info("Fetching runtime image from hub...")
	if hubImage, err := d.hubClient.FetchRuntimeImage(ctx); err != nil {
		d.logger.WithError(err).Warn("Failed to fetch runtime image from hub, using local config")
	} else {
		if hubImage != d.cfg.RuntimeImage {
			d.logger.WithFields(logrus.Fields{
				"old_image": d.cfg.RuntimeImage,
				"new_image": hubImage,
			}).Info("Runtime image updated from hub")
		}
		d.cfg.RuntimeImage = hubImage
	}

	// Ensure image exists first
	if err := containerMgr.EnsureImage(ctx); err != nil {
		d.logger.WithError(err).Warn("Failed to ensure runtime image, continuing anyway")
	}

	// Get runtime info for registration
	runtimeInfo := types.RuntimeInfo{
		Engine:   "openclaw",
		ImageRef: d.cfg.RuntimeImage,
	}

	// Try to get image digest (non-fatal if fails)
	if digest, err := containerMgr.GetImageDigest(ctx); err == nil {
		runtimeInfo.ImageDigest = digest
		runtimeInfo.PulledAt = time.Now().Format(time.RFC3339)
		d.logger.WithFields(logrus.Fields{
			"image":  d.cfg.RuntimeImage,
			"digest": digest,
		}).Info("Runtime image info retrieved")
	} else {
		d.logger.WithError(err).Warn("Failed to get image digest, registering without it")
	}

	// Register node with Hub (attempt registration, ignore if already registered)
	registered, err := d.hubClient.Register(ctx, runtimeInfo)
	if err != nil {
		var occupied *errors.ErrNodeIDOccupied
		if stderrors.As(err, &occupied) {
			d.logger.WithFields(logrus.Fields{
				"node_id": d.cfg.NodeID,
				"error":   occupied.Message,
			}).Error("NODE ID CONFLICT: This node ID is already registered by a different owner. " +
				"Please use a different node_id in your config file or ensure you are using the correct wallet.")
			return fmt.Errorf("node ID %q is occupied by a different owner, cannot start", d.cfg.NodeID)
		}
		// Log warning but continue (node may not require authentication in test mode)
		d.logger.WithError(err).Warn("Failed to register with Hub, continuing anyway")
	} else if registered {
		d.logger.WithField("node_id", d.cfg.NodeID).Info("Successfully registered node with Hub")
	} else {
		d.logger.WithField("node_id", d.cfg.NodeID).Info("Node already registered with Hub")
	}

	// Send initial heartbeat immediately to make node visible in dashboard
	_ = d.sendHeartbeat(ctx)

	// Start container health monitor
	go d.containerMonitor.Start(ctx)

	// Start heartbeat loop
	go d.heartbeatLoop(ctx)

	// Wait for context cancellation
	<-ctx.Done()
	return nil
}

// Shutdown gracefully shuts down the daemon
func (d *Daemon) Shutdown(ctx context.Context) error {
	d.logger.Info("Shutting down...")

	// Create timeout context
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Stop accepting new tasks
	d.shuttingDown.Store(true)

	// Wait for pending snapshot operations
	// d.snapshotEngine.WaitForPending(shutdownCtx)

	// Stop all agents
	for _, agentInfo := range d.agentManager.ListAgents() {
		if _, err := d.agentManager.Stop(shutdownCtx, agentInfo.AgentID, false); err != nil {
			d.logger.WithFields(logrus.Fields{
				"agent_id": agentInfo.AgentID,
				"error":    err,
			}).Warn("Failed to stop agent during shutdown")
		}
	}

	// Save state
	// d.agentManager.SaveState()

	d.logger.Info("Shutdown complete")
	return nil
}

// maxHeartbeatBackoff is the maximum interval between heartbeat retries
// when the hub is unreachable. Kept well below the hub's offline threshold
// (default 30s) so the node can recover quickly after transient failures.
const maxHeartbeatBackoff = 20 * time.Second

// tokenRenewalThreshold is how long before JWT expiry we start proactively
// renewing the token (4 hours).
const tokenRenewalThreshold = 4 * time.Hour

// maxConcurrentCommands limits how many hub commands the node processes
// in parallel to avoid overwhelming the Docker daemon.
const maxConcurrentCommands = 5

func (d *Daemon) heartbeatLoop(ctx context.Context) {
	baseInterval := time.Duration(d.cfg.HeartbeatInterval) * time.Second
	currentInterval := baseInterval

	// Add startup jitter to prevent thundering herd when many nodes
	// restart simultaneously after a hub outage.
	jitter := time.Duration(rand.Int63n(int64(baseInterval)))
	select {
	case <-ctx.Done():
		return
	case <-time.After(jitter):
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(currentInterval):
			if err := d.sendHeartbeat(ctx); err != nil {
				// Exponential backoff with jitter, capped at maxHeartbeatBackoff
				currentInterval = currentInterval * 2
				if currentInterval > maxHeartbeatBackoff {
					currentInterval = maxHeartbeatBackoff
				}
				// Subtract up to 25% jitter (keeps interval ≤ cap)
				currentInterval -= time.Duration(rand.Int63n(int64(currentInterval / 4)))
			} else {
				currentInterval = baseInterval // Reset on success
				d.maybeRenewToken(ctx)
			}
		}
	}
}

func (d *Daemon) sendHeartbeat(ctx context.Context) error {
	agents := d.agentManager.ListAgents()
	capacity := d.agentManager.GetCapacity()

	commands, err := d.hubClient.SendHeartbeat(ctx, agents, capacity)
	if err != nil {
		d.logger.WithError(err).Warn("Heartbeat failed")
		return err
	}

	d.mu.Lock()
	d.lastHeartbeatAt = time.Now()
	d.mu.Unlock()

	// Refresh mining reward balance (non-blocking, best-effort)
	// Use CAS guard to prevent goroutine accumulation when hub is slow.
	go func() {
		if !d.refreshingMining.CompareAndSwap(0, 1) {
			return // another refresh is already in-flight
		}
		defer d.refreshingMining.Store(0)
		d.refreshMiningReward(ctx)
	}()

	// Process Hub commands with bounded concurrency to avoid overwhelming
	// the Docker daemon with simultaneous container operations.
	if len(commands) > 0 {
		sem := make(chan struct{}, maxConcurrentCommands)
		for i := range commands {
			cmd := commands[i] // capture for goroutine
			sem <- struct{}{}
			go func() {
				defer func() { <-sem }()
				d.processHubCommand(ctx, &cmd)
			}()
		}
	}

	return nil
}

// refreshMiningReward fetches the latest ZION mining balance from the hub.
func (d *Daemon) refreshMiningReward(ctx context.Context) {
	// Use a short timeout to avoid blocking if hub is slow
	refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	bal, err := d.hubClient.FetchMiningBalance(refreshCtx)
	if err != nil {
		return // silently ignore — reward display is best-effort
	}
	d.mu.Lock()
	d.miningReward = bal.TotalEarned
	d.mu.Unlock()
}

// FetchMiningBalance returns the current ZION mining balance from the hub.
func (d *Daemon) FetchMiningBalance() (*hub.MiningBalance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return d.hubClient.FetchMiningBalance(ctx)
}

// FetchRewardHistory returns paginated ZION mining transactions from the hub.
func (d *Daemon) FetchRewardHistory(page, limit int) (*hub.MiningTransactionsResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return d.hubClient.FetchMiningTransactions(ctx, page, limit)
}

func (d *Daemon) processHubCommand(ctx context.Context, cmd *types.HubCommand) {
	// Check if context is cancelled (e.g., during shutdown)
	if ctx.Err() != nil {
		return
	}

	log := d.logger.WithField("command", cmd.Command)
	if cmd.AgentID != "" {
		log = log.WithField("agent_id", cmd.AgentID)
	}

	// Verify hub command signature if HubPublicKey is configured
	if d.cfg.HubPublicKey != "" {
		if cmd.Signature == "" {
			log.Error("Rejecting unsigned hub command (hub_public_key is configured)")
			// Report failure to hub so the agent record is cleaned up immediately.
			// Without this, the agent stays stuck as ALIVE/running in the hub DB
			// until the heartbeat reconciliation grace period expires.
			go func() {
				reportCtx, reportCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer reportCancel()
				reason := "command rejected: unsigned command (hub_public_key is configured on node)"
				if reportErr := d.hubClient.ReportAgentFailure(reportCtx, cmd.AgentID, reason); reportErr != nil {
					d.logger.WithError(reportErr).Warn("Failed to report command rejection to hub")
				}
			}()
			return
		}
		// Reconstruct the signed message: "zion:cmd:<command>:<agent_id>:<signed_at>"
		message := fmt.Sprintf("zion:cmd:%s:%s:%d", cmd.Command, cmd.AgentID, cmd.SignedAt)
		if err := crypto.VerifyHubSignature([]byte(message), cmd.Signature, d.cfg.HubPublicKey); err != nil {
			log.WithError(err).Error("Invalid hub command signature — rejecting command")
			// Report failure to hub for the same reason as above
			go func() {
				reportCtx, reportCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer reportCancel()
				reason := fmt.Sprintf("command rejected: invalid signature: %s", err.Error())
				if reportErr := d.hubClient.ReportAgentFailure(reportCtx, cmd.AgentID, reason); reportErr != nil {
					d.logger.WithError(reportErr).Warn("Failed to report command rejection to hub")
				}
			}()
			return
		}
		log.Debug("Hub command signature verified")
	}

	// Add timeout for command processing (30s for run, 15s for stop, 120s for checkpoint/migrate)
	var cmdCtx context.Context
	var cancel context.CancelFunc
	switch cmd.Command {
	case "run", "restore":
		cmdCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
	case "stop":
		cmdCtx, cancel = context.WithTimeout(ctx, 15*time.Second)
	case "checkpoint", "migrate_out":
		cmdCtx, cancel = context.WithTimeout(ctx, 120*time.Second)
	case "shutdown":
		// Shutdown uses its own timeout internally; use background context
		cmdCtx, cancel = context.WithCancel(context.Background())
	default:
		cmdCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
	}
	defer cancel()

	switch cmd.Command {
	case "run":
		// Check context before operation
		if cmdCtx.Err() != nil {
			return
		}
		// Extract profile from params
		profile := d.extractProfile(cmd.Params)
		if profile == nil {
			log.Warn("Invalid run command: missing profile")
			return
		}
		snapshotRef, _ := cmd.Params["snapshot_ref"].(string)

		// Build extra environment variables from command params
		extraEnv := d.buildAgentEnv(cmd.Params)

		log.WithFields(logrus.Fields{"agent_id": cmd.AgentID, "engine": profile.Engine}).Info("Deploying agent")
		if _, err := d.agentManager.Run(cmdCtx, cmd.AgentID, *profile, snapshotRef, "", extraEnv); err != nil {
			if cmdCtx.Err() == context.DeadlineExceeded {
				log.WithError(err).Error("Command processing timeout")
			} else {
				// Provide more helpful error messages based on error type
				errMsg := err.Error()
				if strings.Contains(errMsg, "failed to ensure image") {
					log.WithError(err).Error("Failed to run agent: container image not available")
					log.Info("To fix: ensure Docker is running and can pull the configured runtime_image")
					log.Info("For private images: configure Docker credentials with 'docker login'")
				} else if strings.Contains(errMsg, "capacity") {
					log.WithError(err).Info("Failed to run agent: node at capacity")
				} else if strings.Contains(errMsg, "already running") {
					log.WithError(err).Warn("Failed to run agent: agent already running")
				} else {
					log.WithError(err).Error("Failed to run agent")
				}
			}
			// Report failure to Hub immediately so the agent record is cleaned up
			// without waiting for the 60s heartbeat reconciliation grace period.
			go func() {
				reportCtx, reportCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer reportCancel()
				reason := fmt.Sprintf("run failed: %s", err.Error())
				if reportErr := d.hubClient.ReportAgentFailure(reportCtx, cmd.AgentID, reason); reportErr != nil {
					d.logger.WithError(reportErr).Warn("Failed to report agent failure to hub")
				}
			}()
		}

	case "restore":
		if cmdCtx.Err() != nil {
			return
		}
		profile := d.extractProfile(cmd.Params)
		if profile == nil {
			profile = &types.RuntimeProfile{Engine: "openclaw"}
		}
		snapshotRef, _ := cmd.Params["snapshot_ref"].(string)
		if snapshotRef == "" {
			log.Warn("Invalid restore command: missing snapshot_ref")
			return
		}
		downloadURL, _ := cmd.Params["download_url"].(string)
		extraEnv := d.buildAgentEnv(cmd.Params)
		log.WithFields(logrus.Fields{
			"agent_id":     cmd.AgentID,
			"snapshot_ref": snapshotRef,
		}).Info("Restoring agent from snapshot")
		if _, err := d.agentManager.Run(cmdCtx, cmd.AgentID, *profile, snapshotRef, downloadURL, extraEnv); err != nil {
			log.WithError(err).Error("Failed to restore agent")
			go func() {
				reportCtx, reportCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer reportCancel()
				reason := fmt.Sprintf("restore failed: %s", err.Error())
				if reportErr := d.hubClient.ReportAgentFailure(reportCtx, cmd.AgentID, reason); reportErr != nil {
					d.logger.WithError(reportErr).Warn("Failed to report agent failure to hub")
				}
			}()
		}

	case "stop":
		if cmdCtx.Err() != nil {
			return
		}
		createCheckpoint, _ := cmd.Params["create_checkpoint"].(bool)
		if _, err := d.agentManager.Stop(cmdCtx, cmd.AgentID, createCheckpoint); err != nil {
			if cmdCtx.Err() == context.DeadlineExceeded {
				log.WithError(err).Error("Command processing timeout")
			} else {
				log.WithError(err).Error("Failed to stop agent")
			}
		}

	case "checkpoint":
		if cmdCtx.Err() != nil {
			return
		}
		agent, err := d.agentManager.GetAgent(cmd.AgentID)
		if err != nil {
			log.WithError(err).Error("Failed to get agent for checkpoint")
			return
		}
		snapshotRef, err := d.snapshotEngine.Create(cmdCtx, cmd.AgentID, agent.ContainerID)
		if err != nil {
			if cmdCtx.Err() == context.DeadlineExceeded {
				log.WithError(err).Error("Command processing timeout")
			} else {
				log.WithError(err).Error("Failed to checkpoint agent")
			}
			return
		}
		// Report checkpoint completion
		_ = d.hubClient.ReportCheckpointComplete(cmdCtx, cmd.AgentID, snapshotRef.Ref)

	case "migrate_out":
		if cmdCtx.Err() != nil {
			return
		}
		agent, err := d.agentManager.GetAgent(cmd.AgentID)
		if err != nil {
			log.WithError(err).Error("Failed to get agent for migration")
			return
		}
		snapshotRef, err := d.snapshotEngine.Create(cmdCtx, cmd.AgentID, agent.ContainerID)
		if err != nil {
			log.WithError(err).Error("Failed to checkpoint agent for migration")
			_ = d.hubClient.ReportMigrationFailure(cmdCtx, cmd.AgentID, err.Error())
			return
		}

		// Wait for Hub confirmation
		confirmed, err := d.hubClient.WaitForSnapshotConfirmation(cmdCtx, cmd.AgentID, snapshotRef.Ref)
		if err != nil || !confirmed {
			_ = d.hubClient.ReportMigrationFailure(cmdCtx, cmd.AgentID, "hub confirmation failed")
			return
		}

		// Stop agent after successful migration
		if _, err = d.agentManager.Stop(cmdCtx, cmd.AgentID, false); err != nil {
			log.WithError(err).Error("Failed to stop agent after migration")
		}

	case "shutdown":
		log.Warn("Received shutdown command from Hub — initiating graceful shutdown")
		// Trigger graceful shutdown: stop all agents, then cancel context to exit
		if err := d.Shutdown(context.Background()); err != nil {
			log.WithError(err).Error("Error during hub-initiated shutdown")
		}
		if d.cancelFunc != nil {
			d.cancelFunc()
		}
		return

	case "probe":
		// Anti-cheat: hub is verifying that the agent is actually running.
		// Check that the agent container exists and is alive, then respond
		// with the nonce to prove liveness.
		nonce, _ := cmd.Params["nonce"].(string)
		if nonce == "" {
			log.Warn("Probe command missing nonce")
			return
		}
		if _, err := d.agentManager.GetAgent(cmd.AgentID); err != nil {
			log.WithError(err).Warn("Probe failed: agent not found locally")
			return
		}
		// Agent exists and is running — report probe_response
		if err := d.hubClient.ReportProbeResponse(ctx, cmd.AgentID, nonce); err != nil {
			log.WithError(err).Warn("Failed to send probe response")
		} else {
			log.Debug("Probe response sent successfully")
		}
		return
	}
}

// extractProfile extracts RuntimeProfile from command params
func (d *Daemon) extractProfile(params map[string]interface{}) *types.RuntimeProfile {
	if params == nil {
		return nil
	}
	profile := &types.RuntimeProfile{}
	if v, ok := params["runtime_engine"].(string); ok {
		profile.Engine = v
	}
	if v, ok := params["engine_version"].(string); ok {
		profile.EngineVersion = v
	}
	if v, ok := params["image_hash"].(string); ok {
		profile.ImageHash = v
	}
	if v, ok := params["skills_manifest_hash"].(string); ok {
		profile.SkillsManifestHash = v
	}
	if v, ok := params["snapshot_format"].(string); ok {
		profile.SnapshotFormat = v
	}
	if profile.Engine == "" {
		return nil
	}
	return profile
}

// buildAgentEnv builds environment variables for the agent container from hub command params.
// Translates hub-level config (llm_provider, llm_key, channels_config) into
// environment variables that openclaw-gateway understands (ANTHROPIC_API_KEY, TELEGRAM_BOT_TOKEN, etc).
func (d *Daemon) buildAgentEnv(params map[string]interface{}) map[string]string {
	env := make(map[string]string)
	if params == nil {
		return env
	}

	// Agent session token
	if v, ok := params["agent_token"].(string); ok && v != "" {
		env["ZION_AGENT_TOKEN"] = v
	}

	// LLM API key → provider-specific env var
	llmProvider, _ := params["llm_provider"].(string)
	llmModel, _ := params["llm_model"].(string)
	llmKey, _ := params["llm_key"].(string)

	if llmKey != "" {
		switch strings.ToLower(llmProvider) {
		case "anthropic":
			env["ANTHROPIC_API_KEY"] = llmKey
		case "openai":
			env["OPENAI_API_KEY"] = llmKey
		case "gemini", "google":
			env["GEMINI_API_KEY"] = llmKey
		case "openrouter":
			env["OPENROUTER_API_KEY"] = llmKey
		default:
			// Fallback: set both common providers
			env["ANTHROPIC_API_KEY"] = llmKey
		}
	}

	// Model config (provider/model format for openclaw)
	// For openrouter, the model already contains the full path (e.g. google/gemma-3-27b-it:free)
	if llmModel != "" {
		if strings.ToLower(llmProvider) == "openrouter" {
			env["OPENCLAW_DEFAULT_MODEL"] = llmModel
		} else if llmProvider != "" {
			env["OPENCLAW_DEFAULT_MODEL"] = llmProvider + "/" + llmModel
		}
	}

	// Channels config → platform-specific env vars
	if channels, ok := params["channels_config"]; ok && channels != nil {
		if channelList, ok := channels.([]interface{}); ok {
			for _, ch := range channelList {
				chMap, ok := ch.(map[string]interface{})
				if !ok {
					continue
				}
				platform, _ := chMap["platform"].(string)
				creds, _ := chMap["credentials"].(map[string]interface{})
				if creds == nil {
					continue
				}
				switch strings.ToLower(platform) {
				case "telegram":
					if v, ok := creds["bot_token"].(string); ok && v != "" {
						env["TELEGRAM_BOT_TOKEN"] = v
					}
				case "discord":
					if v, ok := creds["bot_token"].(string); ok && v != "" {
						env["DISCORD_BOT_TOKEN"] = v
					}
				case "slack":
					if v, ok := creds["bot_token"].(string); ok && v != "" {
						env["SLACK_BOT_TOKEN"] = v
					}
					if v, ok := creds["app_token"].(string); ok && v != "" {
						env["SLACK_APP_TOKEN"] = v
					}
				case "feishu":
					if v, ok := creds["app_id"].(string); ok && v != "" {
						env["FEISHU_APP_ID"] = v
					}
					if v, ok := creds["app_secret"].(string); ok && v != "" {
						env["FEISHU_APP_SECRET"] = v
					}
				}
			}
		}
	}

	// Also keep raw JSON for any custom usage
	if channels, ok := params["channels_config"]; ok && channels != nil {
		if data, err := json.Marshal(channels); err == nil {
			env["ZION_CHANNELS_CONFIG"] = string(data)
		}
	}

	// Skills config → JSON env var for container.go to write into openclaw.json
	if skills, ok := params["skills"]; ok && skills != nil {
		if data, err := json.Marshal(skills); err == nil {
			env["ZION_SKILLS_CONFIG"] = string(data)
		}
	}

	return env
}

// autoLogin attempts to authenticate with Hub using wallet
func (d *Daemon) autoLogin(ctx context.Context) error {
	wallet, err := crypto.LoadWalletFrom(d.cfg.WalletDir)
	if err != nil {
		return fmt.Errorf("failed to load wallet: %w", err)
	}

	authClient := crypto.NewHubAuthClient(d.cfg.HubURL)
	token, err := authClient.GetJWT(ctx, wallet)
	if err != nil {
		return fmt.Errorf("failed to get JWT: %w", err)
	}

	// Update config and hub client with new token
	d.cfg.HubAuthToken = token
	d.hubClient.SetAuthToken(token)

	// Parse and cache token expiry for proactive renewal
	d.mu.Lock()
	d.tokenExpiresAt = jwtExpiry(token)
	d.mu.Unlock()

	d.logger.WithField("wallet", wallet.Address).Info("Successfully authenticated with Hub")
	return nil
}

// maybeRenewToken proactively renews the JWT if it expires within tokenRenewalThreshold.
// Called after each successful heartbeat.
func (d *Daemon) maybeRenewToken(ctx context.Context) {
	d.mu.Lock()
	expiresAt := d.tokenExpiresAt
	d.mu.Unlock()

	if expiresAt.IsZero() {
		return
	}

	remaining := time.Until(expiresAt)
	if remaining > tokenRenewalThreshold {
		return
	}

	d.logger.WithFields(logrus.Fields{
		"expires_in": remaining.Round(time.Second).String(),
		"expires_at": expiresAt.Format(time.RFC3339),
	}).Info("JWT token expiring soon, renewing...")

	if err := d.autoLogin(ctx); err != nil {
		d.logger.WithError(err).Warn("Proactive JWT renewal failed, will retry next heartbeat")
	} else {
		d.logger.Info("JWT token renewed successfully")
	}
}

// jwtExpiry extracts the "exp" claim from a JWT token without signature verification.
// Returns zero time if the token cannot be parsed.
func jwtExpiry(token string) time.Time {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return time.Time{}
	}

	// JWT payload is base64url-encoded (no padding)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return time.Time{}
	}

	return time.Unix(claims.Exp, 0)
}
