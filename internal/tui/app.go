package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zion-protocol/zion-node/internal/daemon"
)

// ── message types ────────────────────────────

// tickMsg fires periodically to refresh the dashboard.
type tickMsg time.Time

// logNotifyMsg signals that new log entries are available.
type logNotifyMsg struct{}

// ── tab enum ─────────────────────────────────

type tab int

const (
	tabDashboard tab = iota
	tabLogs
	tabWallet
	tabRewards
)

// Minimum terminal dimensions for usable display
const (
	minWidth  = 60
	minHeight = 16
)

// ── model ────────────────────────────────────

// Model is the top-level bubbletea model.
type Model struct {
	daemon *daemon.Daemon
	logBuf *LogBuffer
	status daemon.NodeStatus

	// sub-models
	setup  setupModel
	logs   logsModel
	wallet walletModel
	reward rewardModel

	// UI state
	activeTab   tab
	showSetup   bool
	confirmQuit bool
	width       int
	height      int
	quitting    bool
}

// NewModel creates the initial TUI model.
// If needsSetup is true the wallet wizard is shown first.
func NewModel(d *daemon.Daemon, logBuf *LogBuffer, needsSetup bool) Model {
	return Model{
		daemon:    d,
		logBuf:    logBuf,
		setup:     newSetupModel(d.Config().WalletDir),
		logs:      newLogsModel(logBuf),
		wallet:    newWalletModel(),
		reward:    newRewardModel(d),
		activeTab: tabDashboard,
		showSetup: needsSetup,
	}
}

// ── Init ─────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		waitForLog(m.logBuf),
	)
}

// tickCmd returns a command that sends tickMsg every second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// waitForLog listens on the LogBuffer channel.
func waitForLog(buf *LogBuffer) tea.Cmd {
	return func() tea.Msg {
		<-buf.Notify()
		return logNotifyMsg{}
	}
}

// ── Update ───────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.setup.width = msg.Width
		m.setup.height = msg.Height
		m.logs.setSize(msg.Width, msg.Height)
		m.reward.setSize(msg.Width, msg.Height)
		return m, nil

	case tickMsg:
		// Refresh node status snapshot (if daemon is running).
		if m.daemon != nil && !m.showSetup {
			m.status = m.daemon.Status()
		}
		// Auto-refresh reward tab every tick when active
		if m.activeTab == tabRewards && !m.reward.fetching {
			return m, tea.Batch(tickCmd(), m.reward.load())
		}
		return m, tickCmd()

	case logNotifyMsg:
		// Refresh log view
		m.logs.refresh()
		return m, waitForLog(m.logBuf)

	case rewardDataMsg:
		cmd := m.reward.update(msg)
		return m, cmd

	case setupDoneMsg:
		m.showSetup = false
		// Immediately pull status now that daemon should be starting.
		if m.daemon != nil {
			m.status = m.daemon.Status()
		}
		return m, nil
	}

	// Delegate to sub-model
	if m.showSetup {
		cmd := m.setup.update(msg)
		return m, cmd
	}

	// Global keys (only in main mode)
	if msg, ok := msg.(tea.KeyMsg); ok {
		// Quit confirmation flow
		if m.confirmQuit {
			switch {
			case key.Matches(msg, keys.Quit), msg.String() == "y", msg.String() == "enter":
				m.quitting = true
				return m, tea.Quit
			default:
				m.confirmQuit = false
				return m, nil
			}
		}

		switch {
		case key.Matches(msg, keys.Quit):
			if msg.String() == "ctrl+c" {
				m.quitting = true
				return m, tea.Quit
			}
			m.confirmQuit = true
			return m, nil
		case key.Matches(msg, keys.Tab1):
			m.activeTab = tabDashboard
			return m, nil
		case key.Matches(msg, keys.Tab2):
			m.activeTab = tabLogs
			m.logs.refresh()
			return m, nil
		case key.Matches(msg, keys.Tab3):
			m.activeTab = tabWallet
			m.wallet.load()
			return m, nil
		case key.Matches(msg, keys.Tab4):
			m.activeTab = tabRewards
			return m, m.reward.load()
		}
	}

	// Tab-specific update
	switch m.activeTab {
	case tabLogs:
		cmd := m.logs.update(msg)
		return m, cmd
	case tabWallet:
		cmd := m.wallet.update(msg)
		return m, cmd
	case tabRewards:
		cmd := m.reward.update(msg)
		return m, cmd
	}

	return m, nil
}

// ── View ─────────────────────────────────────

func (m Model) View() string {
	if m.quitting {
		return "\n  Shutting down...\n"
	}

	// Terminal too small — show resize prompt
	if m.width > 0 && m.height > 0 && (m.width < minWidth || m.height < minHeight) {
		return m.viewTooSmall()
	}

	if m.showSetup {
		return m.setup.view()
	}

	var body string
	switch m.activeTab {
	case tabDashboard:
		recentLogs := m.logBuf.Entries("ALL")
		body = renderDashboard(m.status, recentLogs, m.width, m.height)
	case tabLogs:
		body = m.logs.view()
	case tabWallet:
		body = m.wallet.view()
	case tabRewards:
		body = m.reward.view()
	}

	return body + "\n" + m.footer()
}

func (m Model) footer() string {
	// Quit confirmation banner
	if m.confirmQuit {
		sep := faintStyle.Render(repeat("─", clampInt(m.width, 0, 200)))
		prompt := statusOffline.Render(" Quit Zion Node? ") +
			dimStyle.Render("  [y/Enter] confirm  [Esc/any] cancel")
		return sep + "\n" + prompt
	}

	tabs := []struct {
		label string
		t     tab
	}{
		{"Dashboard", tabDashboard},
		{"Logs", tabLogs},
		{"Wallet", tabWallet},
		{"Rewards", tabRewards},
	}
	var tabParts []string
	for i, tb := range tabs {
		label := fmt.Sprintf("[%d] %s", i+1, tb.label)
		if m.activeTab == tb.t {
			tabParts = append(tabParts, activeTabStyle.Render("● "+label))
		} else {
			tabParts = append(tabParts, inactiveTabStyle.Render("  "+label))
		}
	}

	extra := ""
	switch m.activeTab {
	case tabLogs:
		extra = faintStyle.Render("   [f] level  [↑↓] scroll")
	case tabWallet:
		extra = faintStyle.Render("   [s] show/hide key")
	case tabRewards:
		extra = faintStyle.Render("   [←/→] page  [r] refresh")
	}

	quit := dimStyle.Render("[q] quit")

	left := " " + fmt.Sprintf("%s%s", strings.Join(tabParts, "   "), extra)
	leftW := lipgloss.Width(left)
	quitW := lipgloss.Width(quit)
	pad := m.width - leftW - quitW - 2
	if pad < 2 {
		pad = 2
	}
	sep := faintStyle.Render(repeat("─", clampInt(m.width, 0, 200)))
	return sep + "\n" + left + repeat(" ", pad) + quit
}

func (m Model) viewTooSmall() string {
	cur := fmt.Sprintf("%d×%d", m.width, m.height)
	need := fmt.Sprintf("%d×%d", minWidth, minHeight)

	lines := []string{
		"",
		sectionTitleStyle.Render("  ⬡ ZION NODE"),
		"",
		dimStyle.Render("  Terminal size too small for UI rendering."),
		"",
		fmt.Sprintf("  Current:  %s", valueStyle.Render(cur)),
		fmt.Sprintf("  Minimum:  %s", valueStyle.Render(need)),
		"",
		dimStyle.Render("  Please resize your terminal window."),
		"",
		faintStyle.Render("  [q] quit"),
	}

	// Center vertically
	topPad := (m.height - len(lines)) / 2
	if topPad < 0 {
		topPad = 0
	}
	return repeat("\n", topPad) + fmt.Sprintf("%s", lipgloss.JoinVertical(lipgloss.Left, lines...))
}
