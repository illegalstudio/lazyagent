package ui

import "github.com/charmbracelet/lipgloss"

// DarkTheme returns the default dark terminal theme.
func DarkTheme() Theme {
	return Theme{
		Primary:     lipgloss.Color("#7C3AED"),
		Accent:      lipgloss.Color("#10B981"),
		Warning:     lipgloss.Color("#F59E0B"),
		Muted:       lipgloss.Color("#6B7280"),
		Text:        lipgloss.Color("#F9FAFB"),
		Subtext:     lipgloss.Color("#9CA3AF"),
		Border:      lipgloss.Color("#374151"),
		BorderFocus: lipgloss.Color("#7C3AED"),
		SelectionBg:  lipgloss.Color("#3730A3"),
		TitleText:    lipgloss.Color("#F9FAFB"),
		TitleSubtext: lipgloss.Color("#9CA3AF"),
		TitleMuted:   lipgloss.Color("#6B7280"),
		TitleWarning: lipgloss.Color("#F59E0B"),
		HelpBg:       lipgloss.Color("#111827"),
		HelpKeyBg:   lipgloss.Color("#1F2937"),
		ModalBg:     lipgloss.Color("#1F2937"),
		OverlayBg:   lipgloss.Color("#111827"),

		ActivityWaiting:    lipgloss.Color("#4ADE80"),
		ActivityThinking:   lipgloss.Color("#F59E0B"),
		ActivityCompacting: lipgloss.Color("#2DD4BF"),
		ActivityReading:    lipgloss.Color("#38BDF8"),
		ActivityWriting:    lipgloss.Color("#FB923C"),
		ActivityRunning:    lipgloss.Color("#A78BFA"),
		ActivitySearching:  lipgloss.Color("#34D399"),
		ActivityBrowsing:   lipgloss.Color("#22D3EE"),
		ActivitySpawning:   lipgloss.Color("#F472B6"),
	}
}
