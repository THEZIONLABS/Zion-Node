package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zion-protocol/zion-node/internal/crypto"
)

// setupState tracks the wallet setup wizard steps.
type setupState int

const (
	setupChoose   setupState = iota // choose create or import
	setupCreated                    // just created a wallet, showing keys
	setupImport                     // asking for private key input
	setupImported                   // import succeeded
	setupError                      // display an error message
)

// setupModel drives the first-time wallet setup wizard.
type setupModel struct {
	state      setupState
	cursor     int    // 0 = create, 1 = import
	input      string // private key typed so far
	address    string
	privateKey string
	errMsg     string
	width      int
	height     int
	walletPath string
}

func newSetupModel(walletDir string) setupModel {
	return setupModel{
		walletPath: crypto.WalletPath(walletDir),
	}
}

// NeedsSetup returns true when no wallet file exists.
// If walletDir is empty, the default location ($HOME/.zion-node) is used.
func NeedsSetup(walletDir string) bool {
	walletPath := crypto.WalletPath(walletDir)
	_, err := os.Stat(walletPath)
	return os.IsNotExist(err)
}

// setupDoneMsg is sent when wallet setup finishes.
type setupDoneMsg struct{}

func (m *setupModel) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return nil
}

func (m *setupModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()

	switch m.state {
	case setupChoose:
		switch k {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < 1 {
				m.cursor++
			}
		case "1":
			m.cursor = 0
			return m.doCreate()
		case "2":
			m.cursor = 1
			m.state = setupImport
			m.input = ""
		case "enter":
			if m.cursor == 0 {
				return m.doCreate()
			}
			m.state = setupImport
			m.input = ""
		case "q", "ctrl+c":
			return tea.Quit
		}

	case setupImport:
		switch k {
		case "enter":
			return m.doImport()
		case "backspace":
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		case "esc":
			m.state = setupChoose
			m.input = ""
		case "ctrl+c":
			return tea.Quit
		default:
			// Only accept hex characters + 0x prefix chars
			if len(k) == 1 && isHexChar(k[0]) {
				m.input += k
			}
		}

	case setupCreated, setupImported:
		if k == "enter" {
			return func() tea.Msg { return setupDoneMsg{} }
		}
		if k == "q" || k == "ctrl+c" {
			return tea.Quit
		}

	case setupError:
		switch k {
		case "enter", "esc":
			m.state = setupChoose
			m.errMsg = ""
		case "q", "ctrl+c":
			return tea.Quit
		}
	}

	return nil
}

func (m *setupModel) doCreate() tea.Cmd {
	wallet, err := crypto.GenerateWallet()
	if err != nil {
		m.state = setupError
		m.errMsg = fmt.Sprintf("Failed to generate wallet: %v", err)
		return nil
	}

	// Ensure directory exists
	dir := filepath.Dir(m.walletPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		m.state = setupError
		m.errMsg = fmt.Sprintf("Failed to create directory: %v", err)
		return nil
	}

	if err := wallet.SaveToFile(m.walletPath); err != nil {
		m.state = setupError
		m.errMsg = fmt.Sprintf("Failed to save wallet: %v", err)
		return nil
	}

	m.address = wallet.Address
	m.privateKey = wallet.GetPrivateKeyHex()
	m.state = setupCreated
	return nil
}

func (m *setupModel) doImport() tea.Cmd {
	key := strings.TrimSpace(m.input)
	if key == "" {
		m.errMsg = "Private key cannot be empty"
		m.state = setupError
		return nil
	}

	wallet, err := crypto.ImportWalletFromPrivateKey(key)
	if err != nil {
		m.errMsg = fmt.Sprintf("Invalid private key: %v", err)
		m.state = setupError
		return nil
	}

	dir := filepath.Dir(m.walletPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		m.state = setupError
		m.errMsg = fmt.Sprintf("Failed to create directory: %v", err)
		return nil
	}

	if err := wallet.SaveToFile(m.walletPath); err != nil {
		m.errMsg = fmt.Sprintf("Failed to save wallet: %v", err)
		m.state = setupError
		return nil
	}

	m.address = wallet.Address
	m.state = setupImported
	return nil
}

func (m *setupModel) view() string {
	w := m.width
	if w < 40 {
		w = 60
	}

	banner := titleStyle.Width(w - 2).Render("ZION NODE — First Time Setup")

	var body string
	switch m.state {
	case setupChoose:
		body = m.viewChoose(w)
	case setupCreated:
		body = m.viewCreated(w)
	case setupImport:
		body = m.viewImport(w)
	case setupImported:
		body = m.viewImported(w)
	case setupError:
		body = m.viewError(w)
	}

	return banner + "\n\n" + body
}

func (m *setupModel) viewChoose(w int) string {
	warn := statusOffline.Render("  ⚠ No wallet found at " + m.walletPath)
	desc := dimStyle.Render(`
  A wallet is required to authenticate with the Hub and
  receive agent assignments.

  Choose an option:
`)
	var opt0, opt1 string
	if m.cursor == 0 {
		opt0 = statusOnline.Render("  > [1] Create new wallet")
		opt1 = dimStyle.Render("    [2] Import existing wallet")
	} else {
		opt0 = dimStyle.Render("    [1] Create new wallet")
		opt1 = statusOnline.Render("  > [2] Import existing wallet")
	}

	quit := "\n" + dimStyle.Render("  [q] Quit")

	return warn + "\n" + desc + "\n" + opt0 + "\n" + opt1 + quit
}

func (m *setupModel) viewCreated(w int) string {
	success := statusOnline.Render("  ✓ Wallet created successfully!")
	addr := fmt.Sprintf("  %s  %s",
		labelStyle.Render("Address:    "), valueStyle.Render(m.address))
	pk := fmt.Sprintf("  %s  %s",
		labelStyle.Render("Private Key:"), valueStyle.Render(m.privateKey))
	warning := "\n" + lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render(
		"  ⚠ IMPORTANT: Save your private key securely!\n     It will NOT be shown again.")
	cont := "\n\n" + dimStyle.Render("  Press [Enter] to continue...")

	return success + "\n\n" + addr + "\n" + pk + "\n" + warning + cont
}

func (m *setupModel) viewImport(w int) string {
	prompt := labelStyle.Render("  Enter your private key (hex, with or without 0x prefix):")
	display := m.input
	if len(display) > 66 {
		display = display[:66]
	}
	inputLine := fmt.Sprintf("  > %s█", display)

	hint := "\n" + dimStyle.Render("  [Enter] Submit   [Esc] Back")

	return prompt + "\n" + inputLine + "\n" + hint
}

func (m *setupModel) viewImported(w int) string {
	success := statusOnline.Render("  ✓ Wallet imported successfully!")
	addr := fmt.Sprintf("  %s  %s",
		labelStyle.Render("Address:"), valueStyle.Render(m.address))
	cont := "\n\n" + dimStyle.Render("  Press [Enter] to continue...")

	return success + "\n\n" + addr + cont
}

func (m *setupModel) viewError(w int) string {
	errText := statusOffline.Render("  ✗ " + m.errMsg)
	hint := "\n\n" + dimStyle.Render("  Press [Enter] to try again, or [q] to quit")
	return errText + hint
}

func isHexChar(c byte) bool {
	return (c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'f') ||
		(c >= 'A' && c <= 'F') ||
		c == 'x' || c == 'X'
}
