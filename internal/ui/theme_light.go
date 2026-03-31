package ui

import "github.com/charmbracelet/lipgloss"

// LightTheme returns a theme optimized for light terminal backgrounds.
func LightTheme() Theme {
	return Theme{
		Primary:     lipgloss.Color("#6D28D9"),
		Accent:      lipgloss.Color("#059669"),
		Warning:     lipgloss.Color("#D97706"),
		Muted:       lipgloss.Color("#6B7280"),
		Text:        lipgloss.Color("#111827"),
		Subtext:     lipgloss.Color("#4B5563"),
		Border:      lipgloss.Color("#D1D5DB"),
		BorderFocus: lipgloss.Color("#6D28D9"),
		SelectionBg:  lipgloss.Color("#DDD6FE"),
		TitleText:    lipgloss.Color("#F9FAFB"),
		TitleSubtext: lipgloss.Color("#E0E7FF"),
		TitleMuted:   lipgloss.Color("#C4B5FD"),
		TitleWarning: lipgloss.Color("#FDE68A"),
		HelpBg:       lipgloss.Color("#F3F4F6"),
		HelpKeyBg:   lipgloss.Color("#E5E7EB"),
		ModalBg:     lipgloss.Color("#E5E7EB"),
		OverlayBg:   lipgloss.Color("#F3F4F6"),

		ActivityWaiting:    lipgloss.Color("#16A34A"),
		ActivityThinking:   lipgloss.Color("#D97706"),
		ActivityCompacting: lipgloss.Color("#0D9488"),
		ActivityReading:    lipgloss.Color("#0284C7"),
		ActivityWriting:    lipgloss.Color("#EA580C"),
		ActivityRunning:    lipgloss.Color("#7C3AED"),
		ActivitySearching:  lipgloss.Color("#059669"),
		ActivityBrowsing:   lipgloss.Color("#0891B2"),
		ActivitySpawning:   lipgloss.Color("#DB2777"),
	}
}
