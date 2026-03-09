package tui

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/zion-protocol/zion-node/internal/daemon"
)

var zionTaglines = []string{
	`"If you don't run a node, you don't have a vote."`,
	`"Chancellor on brink of second bailout for banks."`,
	`"Running a node is not a waste of energy. Not running one is."`,
	`"Not your keys, not your coins. Not your node, not your rules."`,
	`"One CPU, one vote."`,
	`"The Times 03/March/2026 — the genesis of a new era."`,
	`"Nodes are to agents what miners are to bitcoin."`,
	`"Be your own bank. Run your own node."`,
	`"In math we trust."`,
	`"Vires in numeris."`,
}

// zionTagline is picked once at startup.
var zionTagline = zionTaglines[rand.Intn(len(zionTaglines))]

// sectionDivider renders a ── Label ────── style divider.
func sectionDivider(label string, width int) string {
	prefix := "── "
	suffix := " "
	visW := 2 + lipgloss.Width(prefix) + lipgloss.Width(label) + lipgloss.Width(suffix)
	lineLen := width - visW
	if lineLen < 4 {
		lineLen = 4
	}
	return "  " + dimStyle.Render(prefix) +
		sectionTitleStyle.Render(label) +
		dimStyle.Render(suffix+repeat("─", lineLen))
}

// renderDashboard draws the full Dashboard tab.
func renderDashboard(status daemon.NodeStatus, recentLogs []LogEntry, width, height int) string {
	if width < 40 {
		width = 80 // sensible default before WindowSizeMsg arrives
	}
	if height < 10 {
		height = 24
	}
	innerW := width - 4 // account for section border padding

	var sections []string

	// ── Title box ────────────────────────────
	boxW := width - 2

	titleText := logoStyle.Render("⬡ ZION NODE")
	tagline := dimStyle.Render(zionTagline)
	var versionStr string
	if status.Version != "" && status.Version != "dev" {
		versionStr = "  " + dimStyle.Render("v"+status.Version)
	}

	boxContent := titleText + versionStr + "\n" + tagline
	sections = append(sections, titleStyle.Width(boxW).Render(boxContent))

	// ── Identity block ───────────────────────
	nodeID := truncate(status.NodeID, 24)
	wallet := truncate(status.WalletAddress, 14)
	hubHost := extractHost(status.HubURL)
	heartbeatAgo := formatAgo(status.LastHeartbeat)
	uptime := formatDuration(status.Uptime)
	stIcon := statusIcon(status.HubConnected, status.HubFailureCount)

	reward := formatReward(status.Reward)

	identityLines := []string{
		fmt.Sprintf("  %s  %-24s  %s  %s",
			labelStyle.Render("Node"), valueStyle.Render(nodeID),
			labelStyle.Render("Status"), stIcon),
		fmt.Sprintf("  %s  %-24s  %s  %s %s",
			labelStyle.Render("Wallet"), valueStyle.Render(wallet),
			labelStyle.Render("Reward"), valueStyle.Render(reward), logoStyle.Render("$ZION")),
		fmt.Sprintf("  %s  %-24s  %s  %s  %s  %s",
			labelStyle.Render("Hub"), valueStyle.Render(hubHost),
			labelStyle.Render("Heartbeat"), valueStyle.Render(heartbeatAgo),
			labelStyle.Render("Uptime"), valueStyle.Render(uptime)),
	}
	sections = append(sections, strings.Join(identityLines, "\n"))

	// ── Resources ────────────────────────────
	barW := innerW - 40
	if barW < 10 {
		barW = 10
	}

	cpuTotal := float64(status.SystemCPU)
	if cpuTotal == 0 {
		cpuTotal = 1
	}
	cpuUsed := status.Capacity.CPUUsedCores

	memTotal := float64(status.SystemMemoryMB) / 1024.0 // display as GB
	memUsed := float64(status.Capacity.MemoryUsedMB) / 1024.0

	slotsTotal := float64(status.Capacity.TotalSlots)
	slotsUsed := float64(status.Capacity.UsedSlots)

	resourceLines := []string{
		fmt.Sprintf("  CPU    %s  %.1f / %d cores   (%d%%)",
			progressBar(cpuUsed, cpuTotal, barW),
			cpuUsed, status.SystemCPU, pct(cpuUsed, cpuTotal)),
		fmt.Sprintf("  Memory %s  %.1f / %.1f GB    (%d%%)",
			progressBar(memUsed, memTotal, barW),
			memUsed, memTotal, pct(memUsed, memTotal)),
		fmt.Sprintf("  Slots  %s  %d / %d slots",
			progressBar(slotsUsed, slotsTotal, barW),
			status.Capacity.UsedSlots, status.Capacity.TotalSlots),
	}

	sections = append(sections, sectionDivider("Resources", width-2)+"\n"+strings.Join(resourceLines, "\n"))

	// ── Agents table ─────────────────────────
	agentHeader := "  " + tableHeaderStyle.Render(fmt.Sprintf("%-20s %-12s %-12s", "ID", "STATUS", "UPTIME"))
	agentSep := "  " + faintStyle.Render(strings.Repeat("─", clampInt(innerW-2, 0, 44)))

	agentRows := []string{agentHeader, agentSep}
	if len(status.Agents) == 0 {
		agentRows = append(agentRows, dimStyle.Render("  No agents assigned"))
	} else {
		maxShow := 8
		if height > 0 {
			// Reserve space for other sections; allow more rows on tall terminals
			maxShow = (height - 24)
			if maxShow < 3 {
				maxShow = 3
			}
			if maxShow > len(status.Agents) {
				maxShow = len(status.Agents)
			}
		}
		for i, a := range status.Agents {
			if i >= maxShow {
				agentRows = append(agentRows,
					dimStyle.Render(fmt.Sprintf("  … and %d more", len(status.Agents)-maxShow)))
				break
			}
			id := truncate(a.AgentID, 20)
			icon := agentIcon(a.Status)
			up := formatSeconds(a.UptimeSec)
			agentRows = append(agentRows,
				fmt.Sprintf("  %-20s %s %-8s %s",
					id, icon, a.Status, up))
		}
	}

	sections = append(sections, sectionDivider("Agents", width-2)+"\n"+strings.Join(agentRows, "\n"))

	// ── Recent Activity ──────────────────────
	maxRecent := 6
	if len(recentLogs) > maxRecent {
		recentLogs = recentLogs[len(recentLogs)-maxRecent:]
	}
	var actLines []string
	if len(recentLogs) == 0 {
		actLines = append(actLines, dimStyle.Render("  Waiting for activity…"))
	} else {
		// Max message width: total width minus time(8) minus level(5) minus spacing(6) minus border padding(4)
		maxMsgW := innerW - 25
		if maxMsgW < 20 {
			maxMsgW = 20
		}
		for _, e := range recentLogs {
			style := logLevelStyle(e.Level)
			msg := e.Message
			if len(msg) > maxMsgW {
				msg = msg[:maxMsgW-1] + "…"
			}
			actLines = append(actLines,
				fmt.Sprintf("  %s  %s  %s",
					faintStyle.Render(e.Time.Format("15:04:05")),
					style.Render(fmt.Sprintf("%-5s", strings.ToUpper(e.Level))),
					msg))
		}
	}

	sections = append(sections, sectionDivider("Recent Activity", width-2)+"\n"+strings.Join(actLines, "\n"))

	return strings.Join(sections, "\n\n")
}

// ── helpers ──────────────────────────────────

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 4 {
		return s[:maxLen]
	}
	half := (maxLen - 2) / 2
	return s[:half] + ".." + s[len(s)-half:]
}

func extractHost(url string) string {
	s := url
	for _, prefix := range []string{"https://", "http://"} {
		s = strings.TrimPrefix(s, prefix)
	}
	if idx := strings.Index(s, "/"); idx > 0 {
		s = s[:idx]
	}
	return truncate(s, 30)
}

func formatAgo(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm %ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

func formatSeconds(sec int64) string {
	if sec <= 0 {
		return "-"
	}
	return formatDuration(time.Duration(sec) * time.Second)
}

func pct(used, total float64) int {
	if total <= 0 {
		return 0
	}
	p := int(used / total * 100)
	if p > 100 {
		p = 100
	}
	return p
}

// formatReward trims a reward string like "11.616666666666666800" to 4 decimal places.
func formatReward(s string) string {
	if s == "" {
		return "-"
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%.4f", f)
}

// clampInt returns v clamped between lo and hi.
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
