package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/zion-protocol/zion-node/internal/config"
	"github.com/zion-protocol/zion-node/internal/crypto"
	"github.com/zion-protocol/zion-node/internal/daemon"
	"github.com/zion-protocol/zion-node/internal/logger"
	"github.com/zion-protocol/zion-node/internal/tui"
)

// configFilePath holds the --config flag value, parsed before subcommand dispatch.
var configFilePath string

// noTUI disables the TUI and falls back to plain log output.
var noTUI bool

// walletDirFlag overrides the wallet directory (default: $HOME/.zion-node).
// When --config is provided, wallet_dir from the config file is used unless
// --wallet-dir is also set.
var walletDirFlag string

func main() {
	// Parse global flags (e.g. --config) before subcommand dispatch.
	// We use a custom FlagSet so that subcommands like "wallet" don't conflict.
	globalFlags := flag.NewFlagSet("zion-node", flag.ContinueOnError)
	globalFlags.SetOutput(io.Discard) // suppress default usage output
	globalFlags.StringVar(&configFilePath, "config", "", "path to config file (default: config.toml)")
	globalFlags.BoolVar(&noTUI, "no-tui", false, "disable TUI, use plain log output")
	globalFlags.StringVar(&walletDirFlag, "wallet-dir", "", "override wallet directory (default: $HOME/.zion-node)")
	// Silently ignore parse errors — unknown args will be handled by subcommands.
	_ = globalFlags.Parse(os.Args[1:])
	remainingArgs := globalFlags.Args()

	// Check for subcommands from remaining (non-flag) args
	if len(remainingArgs) >= 1 {
		switch remainingArgs[0] {
		case "wallet":
			handleWalletCommand(remainingArgs[1:])
			return
		case "update":
			handleUpdateCommand()
			return
		case "help":
			printHelp()
			return
		case "version":
			fmt.Printf("zion-node version %s\n", daemon.Version)
			return
		}
	}

	// Also check original args for --help / --version (flag-style)
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "--help", "-h":
			printHelp()
			return
		case "--version", "-v":
			fmt.Printf("zion-node version %s\n", daemon.Version)
			return
		}
	}

	// Default: run daemon
	if noTUI || !isatty.IsTerminal(os.Stdout.Fd()) {
		runDaemon()
	} else {
		runDaemonWithTUI()
	}
}

func handleWalletCommand(args []string) {
	if len(args) < 1 {
		printWalletHelp()
		os.Exit(1)
	}

	switch args[0] {
	case "new":
		walletNew()
	case "import":
		walletImport(args[1:])
	case "show":
		walletShow()
	case "login":
		walletLogin()
	default:
		printWalletHelp()
		os.Exit(1)
	}
}

func walletNew() {
	wallet, err := crypto.GenerateWallet()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate wallet: %v\n", err)
		os.Exit(1)
	}

	// Save to configured or default location
	walletPath, err := crypto.EnsureWalletDir(resolveWalletDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to prepare wallet dir: %v\n", err)
		os.Exit(1)
	}
	if err := wallet.SaveToFile(walletPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save wallet: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Wallet created successfully!\n\n")
	fmt.Printf("Address:     %s\n", wallet.Address)
	fmt.Printf("Private Key: %s\n", wallet.GetPrivateKeyHex())
	fmt.Printf("Saved to:    %s\n\n", walletPath)
	fmt.Printf("⚠️  IMPORTANT: Save your private key securely!\n")
}

func walletImport(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: zion-node wallet import <private-key>")
		os.Exit(1)
	}

	privateKeyHex := args[0]
	wallet, err := crypto.ImportWalletFromPrivateKey(privateKeyHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to import wallet: %v\n", err)
		os.Exit(1)
	}

	walletPath, err := crypto.EnsureWalletDir(resolveWalletDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to prepare wallet dir: %v\n", err)
		os.Exit(1)
	}
	if err := wallet.SaveToFile(walletPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save wallet: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Wallet imported successfully!\n\n")
	fmt.Printf("Address:  %s\n", wallet.Address)
	fmt.Printf("Saved to: %s\n", walletPath)
}

func walletShow() {
	walletPath := crypto.WalletPath(resolveWalletDir())
	wallet, err := crypto.LoadFromFile(walletPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load wallet: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Wallet Information\n\n")
	fmt.Printf("Address:     %s\n", wallet.Address)
	fmt.Printf("Private Key: %s\n", wallet.GetPrivateKeyHex())
	fmt.Printf("Public Key:  %s\n", wallet.GetPublicKeyHex())
	fmt.Printf("File:        %s\n", walletPath)
}

// resolveWalletDir returns the wallet directory for CLI commands.
// Priority: --wallet-dir flag > wallet_dir from config file > WALLET_DIR env > default ($HOME/.zion-node).
func resolveWalletDir() string {
	// 1. Explicit --wallet-dir flag (highest priority)
	if walletDirFlag != "" {
		return walletDirFlag
	}

	// 2. WALLET_DIR env var
	if v := os.Getenv("WALLET_DIR"); v != "" {
		return v
	}

	// 3. wallet_dir from config file (if --config was provided)
	if configFilePath != "" {
		cfg, err := config.Load(configFilePath)
		if err == nil && cfg.WalletDir != "" {
			return cfg.WalletDir
		}
	}

	// 4. Default: $HOME/.zion-node (empty string signals "use default")
	return ""
}

func printHelp() {
	fmt.Println("Zion Node - Compute node for Zion Protocol")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  zion-node [flags]                Start the node daemon")
	fmt.Println("  zion-node wallet <cmd>           Manage EVM wallet")
	fmt.Println("  zion-node help                   Show this help")
	fmt.Println("  zion-node version                Show version")
	fmt.Println("  zion-node update                 Update to the latest version")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --config <path>      Path to config file (default: config.toml)")
	fmt.Println("  --wallet-dir <path>  Override wallet directory (default: $HOME/.zion-node)")
	fmt.Println("  --no-tui             Disable TUI, use plain log output")
	fmt.Println()
	fmt.Println("Wallet commands:")
	fmt.Println("  wallet new                Generate a new wallet")
	fmt.Println("  wallet import <key>       Import wallet from private key")
	fmt.Println("  wallet show               Show wallet info")
	fmt.Println("  wallet login              Get JWT token from Hub")
}

func walletLogin() {
	// Get hub URL from environment or default
	hubURL := os.Getenv("HUB_ENDPOINT")
	if hubURL == "" {
		hubURL = os.Getenv("SOUR_HUB_URL")
	}
	if hubURL == "" {
		fmt.Fprintln(os.Stderr, "Error: HUB_ENDPOINT or SOUR_HUB_URL environment variable must be set")
		os.Exit(1)
	}

	// Load wallet from configured or default location
	walletPath := crypto.WalletPath(resolveWalletDir())
	wallet, err := crypto.LoadFromFile(walletPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load wallet: %v\n", err)
		fmt.Fprintln(os.Stderr, "Run 'zion-node wallet new' to create a wallet first")
		os.Exit(1)
	}

	fmt.Printf("Authenticating with Hub...\n")
	fmt.Printf("Wallet: %s\n", wallet.Address)
	fmt.Printf("Hub:    %s\n\n", hubURL)

	// Get JWT token
	authClient := crypto.NewHubAuthClient(hubURL)
	ctx := context.Background()

	token, err := authClient.GetJWT(ctx, wallet)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Authentication successful!\n\n")
	fmt.Printf("JWT Token:\n%s\n", token)
}

func printWalletHelp() {
	fmt.Println("Wallet Management")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  zion-node wallet new              Generate a new wallet")
	fmt.Println("  zion-node wallet import <key>     Import from private key")
	fmt.Println("  zion-node wallet show             Show wallet info")
	fmt.Println("  zion-node wallet login            Get JWT token from Hub")
}

const updateRepo = "THEZIONLABS/Zion-Node"

// githubRelease represents a subset of the GitHub release API response.
type githubRelease struct {
	TagName string `json:"tag_name"`
}

func handleUpdateCommand() {
	// Check if another zion-node process is running (daemon).
	selfPID := os.Getpid()
	if isNodeRunning(selfPID) {
		fmt.Fprintln(os.Stderr, "Error: another zion-node instance is currently running. Stop it before updating.")
		os.Exit(1)
	}

	currentVersion := daemon.Version
	fmt.Printf("Current version: %s\n", currentVersion)

	// Fetch latest release from GitHub
	fmt.Println("Checking for updates...")
	latestTag, err := fetchLatestTag()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching latest version: %v\n", err)
		os.Exit(1)
	}

	latestNum := strings.TrimPrefix(latestTag, "v")
	currentNum := strings.TrimPrefix(currentVersion, "v")
	if latestNum == currentNum {
		fmt.Printf("Already up to date (%s)\n", latestTag)
		return
	}

	fmt.Printf("New version available: %s\n", latestTag)

	// Detect platform
	platform := runtime.GOOS + "-" + runtime.GOARCH
	archive := fmt.Sprintf("zion-node-%s-%s.tar.gz", latestNum, platform)
	downloadURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", updateRepo, latestTag, archive)

	// Download to temp dir
	tmpDir, err := os.MkdirTemp("", "zion-node-update-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, archive)
	fmt.Printf("Downloading %s...\n", archive)
	if err := downloadFile(archivePath, downloadURL); err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		os.Exit(1)
	}

	// Extract
	fmt.Println("Extracting...")
	cmd := exec.Command("tar", "-xzf", archivePath, "-C", tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Extraction failed: %s\n%v\n", out, err)
		os.Exit(1)
	}

	// Find current binary path to replace in-place
	selfPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot determine current binary path: %v\n", err)
		os.Exit(1)
	}
	selfPath, err = filepath.EvalSymlinks(selfPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot resolve binary path: %v\n", err)
		os.Exit(1)
	}

	newBinary := filepath.Join(tmpDir, "zion-node")
	if _, err := os.Stat(newBinary); err != nil {
		fmt.Fprintf(os.Stderr, "Error: extracted binary not found at %s\n", newBinary)
		os.Exit(1)
	}

	// Replace the current binary
	fmt.Printf("Installing to %s...\n", selfPath)
	installDir := filepath.Dir(selfPath)

	// Check if we can write directly
	if err := replaceFile(newBinary, selfPath, installDir); err != nil {
		fmt.Fprintf(os.Stderr, "Install failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("✓ Updated successfully to %s\n", latestTag)
}

func fetchLatestTag() (string, error) {
	urls := []string{
		fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateRepo),
		fmt.Sprintf("https://api.github.com/repos/%s/releases", updateRepo),
	}

	client := &http.Client{}
	for _, u := range urls {
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		var tag string
		if strings.HasSuffix(u, "/releases") {
			var releases []githubRelease
			if err := json.NewDecoder(resp.Body).Decode(&releases); err == nil && len(releases) > 0 {
				tag = releases[0].TagName
			}
		} else {
			var release githubRelease
			if err := json.NewDecoder(resp.Body).Decode(&release); err == nil {
				tag = release.TagName
			}
		}
		resp.Body.Close()

		if tag != "" {
			return tag, nil
		}
	}

	return "", fmt.Errorf("could not determine latest version from GitHub")
}

func downloadFile(dest, url string) error {
	resp, err := http.Get(url) //nolint:gosec // URL is constructed from hardcoded repo constant
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// replaceFile atomically replaces destPath with srcPath.
// On Linux a running binary cannot be written to, but it can be unlinked and
// replaced via rename. If direct operations fail (permission denied), it
// retries with sudo.
func replaceFile(srcPath, destPath, installDir string) error {
	// Atomic replace: copy new binary to a temp name in the same directory,
	// then rename over the old one. rename(2) on the same filesystem is
	// atomic and works even when the destination is a running executable.
	tmpDest := destPath + ".new"

	if err := copyFile(srcPath, tmpDest); err == nil {
		if err := os.Chmod(tmpDest, 0755); err != nil {
			os.Remove(tmpDest)
			return err
		}
		if err := os.Rename(tmpDest, destPath); err != nil {
			os.Remove(tmpDest)
			return err
		}
		return nil
	}

	// Permission denied — try sudo
	fmt.Println("Elevated permissions required...")
	cmd := exec.Command("sudo", "cp", srcPath, tmpDest)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo install failed: %w", err)
	}
	_ = exec.Command("sudo", "chmod", "+x", tmpDest).Run()
	mvCmd := exec.Command("sudo", "mv", tmpDest, destPath)
	mvCmd.Stdin = os.Stdin
	mvCmd.Stdout = os.Stdout
	mvCmd.Stderr = os.Stderr
	return mvCmd.Run()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// isNodeRunning checks if another zion-node process (not ourselves) is running.
func isNodeRunning(selfPID int) bool {
	// Use pgrep to find zion-node processes
	out, err := exec.Command("pgrep", "-x", "zion-node").Output()
	if err != nil {
		return false // pgrep returns error when no match
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line != fmt.Sprintf("%d", selfPID) {
			return true
		}
	}
	return false
}

func runDaemon() {
	// Load configuration first (use default logger for errors)
	log := logger.NewLogrusLogger("info")
	cfg, err := config.Load(configFilePath)
	if err != nil {
		log.WithError(err).Fatal("Failed to load config")
	}

	// Re-initialize logger with config level
	log = logger.NewLogrusLogger(cfg.LogLevel)

	// Setup file logging
	logCloser, err := logger.SetupFileLogging(log, cfg.LogDir)
	if err != nil {
		log.WithError(err).Warn("Failed to setup file logging")
	} else {
		defer logCloser.Close()
		log.WithField("log_dir", cfg.LogDir).Info("File logging enabled")
	}

	// Validate configuration
	if err := cfg.ValidateWithLogger(log); err != nil {
		log.WithError(err).Fatal("Config validation failed")
	}

	// Create daemon
	d, err := daemon.NewDaemon(cfg)
	if err != nil {
		log.WithError(err).Fatal("Failed to create daemon")
	}

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info("Shutting down...")
		if err := d.Shutdown(ctx); err != nil {
			log.WithError(err).Error("Shutdown error")
		}
		cancel()
	}()

	// Run daemon
	if err := d.Run(ctx); err != nil {
		log.WithError(err).Fatal("Daemon failed")
	}
}

// runDaemonWithTUI starts the daemon with the bubbletea TUI.
// The TUI takes over stdout; logs are captured via a logrus Hook and
// displayed in the Logs tab. On non-TTY or --no-tui, runDaemon() is used instead.
func runDaemonWithTUI() {
	log := logger.NewLogrusLogger("info")
	cfg, err := config.Load(configFilePath)
	if err != nil {
		log.WithError(err).Fatal("Failed to load config")
	}

	// Re-initialize logger with config level
	log = logger.NewLogrusLogger(cfg.LogLevel)

	// Setup file logging
	logCloser, err := logger.SetupFileLogging(log, cfg.LogDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to setup file logging: %v\n", err)
	} else {
		defer logCloser.Close()
	}

	// Attach log buffer hook — TUI reads from this
	logBuf := tui.NewLogBuffer(500)
	log.AddHook(logBuf)

	// In TUI mode we don't want logrus writing to stdout (bubbletea owns it).
	// All output goes through the TUI log buffer hook.
	log.SetOutput(io.Discard)

	// In TUI mode, logrus output is discarded (bubbletea owns stdout).
	// Fatal errors before TUI starts would be invisible, so print to stderr.
	if err := cfg.ValidateWithLogger(log); err != nil {
		fmt.Fprintf(os.Stderr, "Config validation failed: %v\n", err)
		os.Exit(1)
	}

	// Determine if wallet setup wizard is needed
	needsSetup := tui.NeedsSetup(cfg.WalletDir)

	// Create daemon — pass the configured logger so all daemon logs
	// go through the hook (captured by TUI) and NOT to stdout.
	d, err := daemon.NewDaemonWithLogger(cfg, log)
	if err != nil {
		// If daemon creation fails we can't show TUI; fall back to stderr.
		fmt.Fprintf(os.Stderr, "Failed to create daemon: %v\n", err)
		os.Exit(1)
	}

	// Context + signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start daemon in background goroutine.
	// If wallet setup is needed, wait until it completes before starting.
	setupDone := make(chan struct{})
	if !needsSetup {
		close(setupDone) // no setup needed, start immediately
	}
	go func() {
		<-setupDone
		// Reload wallet into config now that it exists
		cfg.ReloadWallet()
		if runErr := d.Run(ctx); runErr != nil {
			log.WithError(runErr).Error("Daemon failed")
			cancel()
		}
	}()

	// Build TUI model
	model := tui.NewModel(d, logBuf, needsSetup, setupDone)

	// Run bubbletea program
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Forward OS signals to both TUI quit and daemon shutdown
	go func() {
		select {
		case <-sigChan:
			p.Quit()
			_ = d.Shutdown(ctx)
			cancel()
		case <-ctx.Done():
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	// On TUI exit, shut down daemon gracefully
	_ = d.Shutdown(ctx)
	cancel()
}
