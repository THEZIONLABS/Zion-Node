package tui

import "github.com/charmbracelet/lipgloss"

// ──────────────────────────────────────────────
// Color palette — muted, professional tones
// ──────────────────────────────────────────────

var (
	// Primary accent (acid green — matches Zion brand #DFFF00)
	colorAccent = lipgloss.AdaptiveColor{Light: "#A3B800", Dark: "#DFFF00"}

	// Semantic
	colorGreen  = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34D399"}
	colorYellow = lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#FBBF24"}
	colorRed    = lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#FB7185"}

	// Text hierarchy
	colorText    = lipgloss.AdaptiveColor{Light: "#1E293B", Dark: "#E2E8F0"}
	colorSubtext = lipgloss.AdaptiveColor{Light: "#475569", Dark: "#94A3B8"}
	colorDim     = lipgloss.AdaptiveColor{Light: "#94A3B8", Dark: "#64748B"}
	colorFaint   = lipgloss.AdaptiveColor{Light: "#CBD5E1", Dark: "#475569"}
)

// ──────────────────────────────────────────────
// Reusable styles
// ──────────────────────────────────────────────

var (
	// Title banner — rounded box with accent border
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorText).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(0, 1)

	// Section divider label
	sectionTitleStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	// Key label (Node, Wallet, etc.)
	labelStyle = lipgloss.NewStyle().Foreground(colorSubtext)

	// Value
	valueStyle = lipgloss.NewStyle().Foreground(colorText)

	// Dim text (footer, hints)
	dimStyle = lipgloss.NewStyle().Foreground(colorDim)

	// Faint (structural lines, empty bar)
	faintStyle = lipgloss.NewStyle().Foreground(colorFaint)

	// Status styles
	statusOnline   = lipgloss.NewStyle().Foreground(colorGreen)
	statusDegraded = lipgloss.NewStyle().Foreground(colorYellow)
	statusOffline  = lipgloss.NewStyle().Foreground(colorRed)

	// Tab styles — active uses accent
	activeTabStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	inactiveTabStyle = lipgloss.NewStyle().Foreground(colorDim)

	// Progress bar
	barFilled = lipgloss.NewStyle().Foreground(colorAccent)
	barEmpty  = lipgloss.NewStyle().Foreground(colorFaint)

	// Table header
	tableHeaderStyle = lipgloss.NewStyle().
				Foreground(colorSubtext).
				Bold(true)

	// Log level styles
	logDebugStyle = lipgloss.NewStyle().Foreground(colorDim)
	logInfoStyle  = lipgloss.NewStyle().Foreground(colorText)
	logWarnStyle  = lipgloss.NewStyle().Foreground(colorYellow)
	logErrorStyle = lipgloss.NewStyle().Foreground(colorRed).Bold(true)

	// ASCII logo
	logoStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
)

// statusIcon returns a coloured status indicator.
func statusIcon(connected bool, failures int) string {
	switch {
	case connected && failures == 0:
		return statusOnline.Render("● Connected")
	case failures > 0 && failures < 3:
		return statusDegraded.Render("● Degraded")
	default:
		return statusOffline.Render("● Disconnected")
	}
}

// agentIcon returns a coloured agent state indicator.
func agentIcon(status string) string {
	switch status {
	case "running":
		return statusOnline.Render("●")
	case "paused":
		return statusDegraded.Render("●")
	case "stopped":
		return faintStyle.Render("○")
	case "dead":
		return statusOffline.Render("✕")
	default:
		return faintStyle.Render("○")
	}
}

// logLevelStyle picks the right style for a log level.
func logLevelStyle(level string) lipgloss.Style {
	switch level {
	case "debug", "trace":
		return logDebugStyle
	case "warning", "warn":
		return logWarnStyle
	case "error", "fatal", "panic":
		return logErrorStyle
	default:
		return logInfoStyle
	}
}

// progressBar renders a simple text progress bar.
// width = total character width (excluding label).
func progressBar(used, total float64, width int) string {
	if total <= 0 || width <= 0 {
		return ""
	}
	ratio := used / total
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}
	filled := int(ratio * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled

	// Pick colour based on ratio
	style := barFilled
	if ratio > 0.85 {
		style = lipgloss.NewStyle().Foreground(colorRed)
	} else if ratio > 0.6 {
		style = lipgloss.NewStyle().Foreground(colorYellow)
	}

	return style.Render(repeat("━", filled)) + barEmpty.Render(repeat("─", empty))
}

func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
