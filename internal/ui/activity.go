package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/core"
)

// activityColors maps each activity kind to a display color (TUI-specific).
var activityColors = map[core.ActivityKind]lipgloss.Color{
	core.ActivityIdle:       colorMuted,
	core.ActivityWaiting:    lipgloss.Color("#4ADE80"),
	core.ActivityThinking:   colorWarning,
	core.ActivityCompacting: lipgloss.Color("#2DD4BF"),
	core.ActivityReading:    lipgloss.Color("#38BDF8"),
	core.ActivityWriting:    lipgloss.Color("#FB923C"),
	core.ActivityRunning:    lipgloss.Color("#A78BFA"),
	core.ActivitySearching:  lipgloss.Color("#34D399"),
	core.ActivityBrowsing:   lipgloss.Color("#22D3EE"),
	core.ActivitySpawning:   lipgloss.Color("#F472B6"),
}
