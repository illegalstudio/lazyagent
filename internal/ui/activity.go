package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/core"
)

// activityColorMap builds the activity-to-color mapping from a Theme.
func activityColorMap(t Theme) map[core.ActivityKind]lipgloss.Color {
	return map[core.ActivityKind]lipgloss.Color{
		core.ActivityIdle:       t.Muted,
		core.ActivityWaiting:    t.ActivityWaiting,
		core.ActivityThinking:   t.ActivityThinking,
		core.ActivityCompacting: t.ActivityCompacting,
		core.ActivityReading:    t.ActivityReading,
		core.ActivityWriting:    t.ActivityWriting,
		core.ActivityRunning:    t.ActivityRunning,
		core.ActivitySearching:  t.ActivitySearching,
		core.ActivityBrowsing:   t.ActivityBrowsing,
		core.ActivitySpawning:   t.ActivitySpawning,
	}
}
