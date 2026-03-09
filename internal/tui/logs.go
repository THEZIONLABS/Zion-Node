package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// filterLevels is the cycle order when pressing 'f'.
var filterLevels = []string{"ALL", "DEBUG", "INFO", "WARN", "ERROR"}

// logsModel manages the Logs tab state.
type logsModel struct {
	vp         viewport.Model
	logBuf     *LogBuffer
	filterIdx  int    // index into filterLevels
	filter     string // current filter level
	autoFollow bool   // auto-scroll to bottom on new logs
	lastCount  int    // entry count at last render
	width      int
	height     int
}

func newLogsModel(buf *LogBuffer) logsModel {
	vp := viewport.New(80, 20)
	vp.YPosition = 0
	return logsModel{
		vp:         vp,
		logBuf:     buf,
		filter:     "ALL",
		autoFollow: true,
	}
}

// setSize updates the viewport dimensions.
func (m *logsModel) setSize(w, h int) {
	m.width = w
	m.height = h
	// Reserve 3 lines for header + 1 for footer
	vpH := h - 4
	if vpH < 1 {
		vpH = 1
	}
	m.vp.Width = w - 2
	m.vp.Height = vpH
}

// refresh rebuilds the viewport content from the log buffer.
func (m *logsModel) refresh() {
	entries := m.logBuf.Entries(m.filter)

	// Max message width: total width minus time(8) minus level(5) minus module(10) minus spacing(8) minus border(2)
	maxMsgW := m.width - 35
	if maxMsgW < 20 {
		maxMsgW = 20
	}

	var sb strings.Builder
	for _, e := range entries {
		style := logLevelStyle(e.Level)
		line := fmt.Sprintf("  %s  %s  ",
			dimStyle.Render(e.Time.Format("15:04:05")),
			style.Render(fmt.Sprintf("%-5s", strings.ToUpper(e.Level))))
		if e.Module != "" {
			line += dimStyle.Render(fmt.Sprintf("%-10s ", e.Module))
		}
		msg := e.Message
		// Calculate remaining space after prefix
		prefixW := lipgloss.Width(line)
		msgMaxW := m.width - prefixW - 2
		if msgMaxW < 10 {
			msgMaxW = 10
		}
		if len(msg) > msgMaxW {
			msg = msg[:msgMaxW-1] + "…"
		}
		line += msg
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	m.vp.SetContent(sb.String())
	m.lastCount = len(entries)

	if m.autoFollow {
		m.vp.GotoBottom()
	}
}

// update handles key messages for the logs viewport.
func (m *logsModel) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.String() == "f":
			m.filterIdx = (m.filterIdx + 1) % len(filterLevels)
			m.filter = filterLevels[m.filterIdx]
			m.autoFollow = true // reset scroll on filter change
			m.refresh()
			return nil
		case msg.String() == "end":
			m.autoFollow = true
			m.vp.GotoBottom()
			return nil
		case msg.String() == "home":
			m.autoFollow = false
			m.vp.GotoTop()
			return nil
		case msg.String() == "up" || msg.String() == "k":
			m.autoFollow = false
		case msg.String() == "down" || msg.String() == "j":
			// If user is at the bottom, re-enable auto-follow
			// We'll check after viewport update
		}
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)

	// Re-enable auto-follow if user scrolled to the very bottom
	if m.vp.AtBottom() {
		m.autoFollow = true
	}

	return cmd
}

// view renders the Logs tab.
func (m *logsModel) view() string {
	var filterLabel string
	if m.filter == "ALL" {
		filterLabel = "Level: ALL"
	} else {
		filterLabel = "Level: ≥ " + m.filter
	}
	title := "⬡ LOGS"
	padW := m.width - lipgloss.Width(title) - len(filterLabel) - 6
	if padW < 1 {
		padW = 1
	}
	header := titleStyle.Width(m.width - 2).Render(
		title + repeat(" ", padW) + filterLabel)

	total := m.logBuf.Len()
	showing := m.lastCount
	footer := dimStyle.Render(
		fmt.Sprintf("  %d / %d entries", showing, total))

	return header + "\n" + m.vp.View() + "\n" + footer
}
