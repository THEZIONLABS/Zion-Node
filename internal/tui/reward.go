package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zion-protocol/zion-node/internal/daemon"
	"github.com/zion-protocol/zion-node/internal/hub"
)

// rewardModel manages the Rewards tab state.
type rewardModel struct {
	daemon *daemon.Daemon

	balance      string // current balance
	totalEarned  string // cumulative total
	transactions []hub.MiningTransaction
	totalTxCount int

	page     int
	pageSize int
	fetching bool // guard to prevent concurrent fetches
	loaded   bool // true after first successful fetch
	errMsg   string

	width  int
	height int
}

// rewardDataMsg carries fetched reward data back to the model.
type rewardDataMsg struct {
	balance      string
	totalEarned  string
	transactions []hub.MiningTransaction
	totalCount   int
	err          error
}

func newRewardModel(d *daemon.Daemon) rewardModel {
	return rewardModel{
		daemon:   d,
		page:     1,
		pageSize: 20,
	}
}

// load fetches both balance and transaction history from the hub (silent).
func (m *rewardModel) load() tea.Cmd {
	if m.fetching {
		return nil
	}
	m.fetching = true
	d := m.daemon
	page := m.page
	limit := m.pageSize

	return func() tea.Msg {
		var msg rewardDataMsg

		bal, err := d.FetchMiningBalance()
		if err != nil {
			msg.err = err
			return msg
		}
		msg.balance = bal.Balance
		msg.totalEarned = bal.TotalEarned

		txResp, err := d.FetchRewardHistory(page, limit)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.transactions = txResp.Data
		msg.totalCount = txResp.Pagination.Total

		return msg
	}
}

func (m *rewardModel) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case rewardDataMsg:
		m.fetching = false
		if msg.err != nil {
			// Only show error if we have no data yet
			if !m.loaded {
				m.errMsg = msg.err.Error()
			}
			return nil
		}
		m.loaded = true
		m.errMsg = ""
		m.balance = msg.balance
		m.totalEarned = msg.totalEarned
		m.transactions = msg.transactions
		m.totalTxCount = msg.totalCount
		return nil

	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			return m.load()
		case "right", "l", "n":
			totalPages := (m.totalTxCount + m.pageSize - 1) / m.pageSize
			if m.page < totalPages {
				m.page++
				return m.load()
			}
		case "left", "h", "p":
			if m.page > 1 {
				m.page--
				return m.load()
			}
		}
	}
	return nil
}

func (m *rewardModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *rewardModel) view() string {
	w := m.width
	if w < 40 {
		w = 80
	}

	title := "⬡ REWARDS"
	header := titleStyle.Width(w - 2).Render(title)

	var lines []string

	// Balance summary
	lines = append(lines, sectionDivider("Balance", w-2))
	lines = append(lines, "")

	bal := formatReward(m.balance)
	earned := formatReward(m.totalEarned)
	lines = append(lines, fmt.Sprintf("  %s  %s %s",
		labelStyle.Render("Balance     "),
		valueStyle.Render(bal),
		logoStyle.Render("$ZION")))
	lines = append(lines, fmt.Sprintf("  %s  %s %s",
		labelStyle.Render("Total Earned"),
		valueStyle.Render(earned),
		logoStyle.Render("$ZION")))

	// Error only when we have no data at all
	if !m.loaded && m.errMsg != "" {
		lines = append(lines, "")
		lines = append(lines, statusOffline.Render("  ✗ "+m.errMsg))
		lines = append(lines, dimStyle.Render("  Press [r] to retry"))
		return header + "\n\n" + lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	// Transaction history
	lines = append(lines, "")
	lines = append(lines, sectionDivider("Reward History", w-2))
	lines = append(lines, "")

	if !m.loaded {
		lines = append(lines, dimStyle.Render("  Waiting for data..."))
	} else if len(m.transactions) == 0 {
		lines = append(lines, dimStyle.Render("  No rewards yet — keep your node running!"))
	} else {
		// Table header
		lines = append(lines, "  "+tableHeaderStyle.Render(
			fmt.Sprintf("%-19s  %-12s  %s", "TIME", "AMOUNT", "MEMO")))
		lines = append(lines, "  "+faintStyle.Render(strings.Repeat("─", clampInt(w-6, 0, 80))))

		// Calculate max rows based on height
		maxRows := m.height - 18
		if maxRows < 5 {
			maxRows = 5
		}
		if maxRows > len(m.transactions) {
			maxRows = len(m.transactions)
		}

		memoW := w - 40
		if memoW < 15 {
			memoW = 15
		}

		for i := 0; i < maxRows; i++ {
			tx := m.transactions[i]
			ts := formatTxTime(tx.CreatedAt)
			amount := formatReward(tx.Amount)
			memo := tx.Memo
			if len(memo) > memoW {
				memo = memo[:memoW-1] + "…"
			}

			lines = append(lines, fmt.Sprintf("  %s  %s %s  %s",
				faintStyle.Render(ts),
				valueStyle.Render(fmt.Sprintf("%12s", amount)),
				logoStyle.Render("$ZION"),
				dimStyle.Render(memo)))
		}

		if len(m.transactions) > maxRows {
			lines = append(lines, dimStyle.Render(
				fmt.Sprintf("  … and %d more on this page", len(m.transactions)-maxRows)))
		}
	}

	// Pagination
	totalPages := (m.totalTxCount + m.pageSize - 1) / m.pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s  %s",
		dimStyle.Render(fmt.Sprintf("Page %d / %d", m.page, totalPages)),
		faintStyle.Render("  [←/→] page  [r] refresh")))

	return header + "\n\n" + lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// formatTxTime parses ISO 8601 and formats as "Jan 02 15:04".
func formatTxTime(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05.000Z", iso)
		if err != nil {
			if len(iso) >= 19 {
				return iso[:19]
			}
			return iso
		}
	}
	return t.Local().Format("Jan 02 15:04")
}
