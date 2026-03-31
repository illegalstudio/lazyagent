package ui

import "github.com/charmbracelet/lipgloss"

// Theme holds all TUI colors. Each field is a lipgloss.Color so themes
// can be swapped at startup without touching rendering code.
type Theme struct {
	Primary     lipgloss.Color
	Accent      lipgloss.Color
	Warning     lipgloss.Color
	Muted       lipgloss.Color
	Text        lipgloss.Color
	Subtext     lipgloss.Color
	Border      lipgloss.Color
	BorderFocus lipgloss.Color
	SelectionBg lipgloss.Color

	// Title bar foreground colors (rendered on Primary background)
	TitleText    lipgloss.Color
	TitleSubtext lipgloss.Color
	TitleMuted   lipgloss.Color
	TitleWarning lipgloss.Color

	HelpBg      lipgloss.Color
	HelpKeyBg   lipgloss.Color
	ModalBg     lipgloss.Color
	OverlayBg   lipgloss.Color

	// Activity colors
	ActivityWaiting    lipgloss.Color
	ActivityThinking   lipgloss.Color
	ActivityCompacting lipgloss.Color
	ActivityReading    lipgloss.Color
	ActivityWriting    lipgloss.Color
	ActivityRunning    lipgloss.Color
	ActivitySearching  lipgloss.Color
	ActivityBrowsing   lipgloss.Color
	ActivitySpawning   lipgloss.Color
}

// LoadTheme returns the theme for the given name. Falls back to dark.
func LoadTheme(name string) Theme {
	switch name {
	case "light":
		return LightTheme()
	default:
		return DarkTheme()
	}
}
