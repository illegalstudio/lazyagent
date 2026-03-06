package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	colorPrimary     = lipgloss.Color("#7C3AED")
	colorAccent      = lipgloss.Color("#10B981")
	colorWarning     = lipgloss.Color("#F59E0B")
	colorDanger      = lipgloss.Color("#EF4444")
	colorMuted       = lipgloss.Color("#6B7280")
	colorText        = lipgloss.Color("#F9FAFB")
	colorSubtext     = lipgloss.Color("#9CA3AF")
	colorBorder      = lipgloss.Color("#374151")
	colorBorderFocus = lipgloss.Color("#7C3AED")
	colorSelBg       = lipgloss.Color("#3730A3") // visible indigo selection

	// Status colors map
	statusColors = map[string]lipgloss.Color{
		"waiting":    colorAccent,
		"thinking":   colorWarning,
		"tool":       colorPrimary,
		"processing": colorWarning,
		"idle":       colorMuted,
		"unknown":    colorMuted,
	}

	// Panels: border only, no padding (avoids width overflow)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	panelFocusStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderFocus)

	// Detail panel labels
	labelStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			Width(22)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorText)

	// Help bar
	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Background(lipgloss.Color("#111827"))

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1)

	// Title bar
	titleStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorPrimary).
			Bold(true).
			Padding(0, 1)
)

// statusDot returns a single ASCII dot colored by status (no emoji = no width bugs).
func statusDot(status string) string {
	color, ok := statusColors[status]
	if !ok {
		color = colorMuted
	}
	return lipgloss.NewStyle().Foreground(color).Render("●")
}

// statusLabel returns the status string styled with its color.
func statusLabel(status string) string {
	color, ok := statusColors[status]
	if !ok {
		color = colorMuted
	}
	return lipgloss.NewStyle().Foreground(color).Bold(true).Render(status)
}
