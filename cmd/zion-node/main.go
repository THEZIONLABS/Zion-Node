package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
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

func runDaemon() {
	// Load configuration first (use default logger for errors)
	log := logger.NewLogrusLogger("info")
	cfg, err := config.Load(configFilePath)
	if err != nil {
		log.WithError(err).Fatal("Failed to load config")
	}

	// Re-initialize logger with config level
	log = logger.NewLogrusLogger(cfg.LogLevel)

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

	// Start daemon in background goroutine
	go func() {
		if runErr := d.Run(ctx); runErr != nil {
			log.WithError(runErr).Error("Daemon failed")
			cancel()
		}
	}()

	// Build TUI model
	model := tui.NewModel(d, logBuf, needsSetup)

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
