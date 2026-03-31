package ui

import "github.com/charmbracelet/lipgloss"

// styles holds pre-built lipgloss styles derived from a Theme.
type styles struct {
	panel      lipgloss.Style
	panelFocus lipgloss.Style
	label      lipgloss.Style
	value      lipgloss.Style
	help       lipgloss.Style
	helpKey    lipgloss.Style
	title      lipgloss.Style
}

func newStyles(t Theme) styles {
	return styles{
		panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Border),

		panelFocus: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.BorderFocus),

		label: lipgloss.NewStyle().
			Foreground(t.Subtext).
			Width(22),

		value: lipgloss.NewStyle().
			Foreground(t.Text),

		help: lipgloss.NewStyle().
			Foreground(t.Muted).
			Background(t.HelpBg),

		helpKey: lipgloss.NewStyle().
			Foreground(t.Text).
			Background(t.HelpKeyBg).
			Padding(0, 1),

		title: lipgloss.NewStyle().
			Foreground(t.TitleText).
			Background(t.Primary).
			Bold(true).
			Padding(0, 1),
	}
}
