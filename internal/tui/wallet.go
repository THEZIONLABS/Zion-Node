package tui

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zion-protocol/zion-node/internal/crypto"
)

// walletModel manages the Wallet tab state.
type walletModel struct {
	address    string
	publicKey  string
	privateKey string
	walletPath string

	showPrivateKey bool // toggle with 's'
	loaded         bool
	errMsg         string

	width int
}

func newWalletModel() walletModel {
	home, _ := os.UserHomeDir()
	return walletModel{
		walletPath: filepath.Join(home, ".zion-node", "wallet.json"),
	}
}

// load reads wallet from disk. Called once on first view.
func (m *walletModel) load() {
	if m.loaded {
		return
	}
	m.loaded = true

	wallet, err := crypto.LoadFromFile(m.walletPath)
	if err != nil {
		m.errMsg = fmt.Sprintf("Failed to load wallet: %v", err)
		return
	}

	m.address = wallet.Address
	m.publicKey = wallet.GetPublicKeyHex()
	m.privateKey = wallet.GetPrivateKeyHex()
}

func (m *walletModel) update(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "s":
			m.showPrivateKey = !m.showPrivateKey
		}
	}
	return nil
}

func (m *walletModel) view() string {
	w := m.width
	if w < 40 {
		w = 80
	}

	// Title
	title := "⬡ WALLET"
	header := titleStyle.Width(w - 2).Render(title)

	if m.errMsg != "" {
		errLine := statusOffline.Render("  ✗ " + m.errMsg)
		hint := dimStyle.Render("  Wallet path: " + m.walletPath)
		return header + "\n\n" + errLine + "\n" + hint
	}

	if m.address == "" {
		return header + "\n\n" + dimStyle.Render("  No wallet loaded")
	}

	// Wallet info section
	var lines []string

	lines = append(lines, sectionDivider("Identity", w-2))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s  %s",
		labelStyle.Render("Address    "),
		valueStyle.Render(m.address)))
	lines = append(lines, fmt.Sprintf("  %s  %s",
		labelStyle.Render("Public Key "),
		dimStyle.Render(truncate(m.publicKey, 52))))
	lines = append(lines, fmt.Sprintf("  %s  %s",
		labelStyle.Render("Wallet File"),
		dimStyle.Render(m.walletPath)))

	// Private key section
	lines = append(lines, "")
	lines = append(lines, sectionDivider("Private Key", w-2))
	lines = append(lines, "")

	if m.showPrivateKey {
		lines = append(lines,
			fmt.Sprintf("  %s  %s",
				labelStyle.Render("Private Key"),
				lipgloss.NewStyle().Foreground(colorYellow).Render(m.privateKey)))
		lines = append(lines, "")
		lines = append(lines,
			lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(
				"  ⚠ NEVER share your private key with anyone!"))
		lines = append(lines, dimStyle.Render("  Press [s] to hide"))
	} else {
		masked := "0x" + repeat("•", 64)
		lines = append(lines,
			fmt.Sprintf("  %s  %s",
				labelStyle.Render("Private Key"),
				faintStyle.Render(masked)))
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("  Press [s] to reveal private key"))
	}

	// Tips
	lines = append(lines, "")
	lines = append(lines, sectionDivider("Usage", w-2))
	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("  • Your wallet authenticates this node with the Hub"))
	lines = append(lines, dimStyle.Render("  • It signs heartbeat messages to prove node identity"))
	lines = append(lines, dimStyle.Render("  • Back up your private key to migrate to another machine"))

	return header + "\n\n" + lipgloss.JoinVertical(lipgloss.Left, lines...)
}
