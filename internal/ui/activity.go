package ui

import (
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nahime0/lazyclaude/internal/claude"
)

// ActivityTimeout is the duration a tool-based activity stays visible after
// the tool finishes, unless replaced by a newer activity.
const ActivityTimeout = 30 * time.Second

// ActivityKind is the human-readable label shown in the session list.
type ActivityKind string

const (
	ActivityIdle      ActivityKind = "idle"
	ActivityWaiting   ActivityKind = "waiting"   // Claude waiting for user input
	ActivityThinking  ActivityKind = "thinking"  // Claude generating a response
	ActivityReading   ActivityKind = "reading"   // Read / Glob / Grep
	ActivityWriting   ActivityKind = "writing"   // Write / Edit
	ActivityRunning   ActivityKind = "running"   // Bash
	ActivitySearching ActivityKind = "searching" // Grep / Glob
	ActivityBrowsing  ActivityKind = "browsing"  // WebFetch / WebSearch
	ActivitySpawning  ActivityKind = "spawning"  // Agent (subagent)
)

// isTransient returns true for tool-based activities that should be sticky.
func (a ActivityKind) isTransient() bool {
	switch a {
	case ActivityReading, ActivityWriting, ActivityRunning,
		ActivitySearching, ActivityBrowsing, ActivitySpawning:
		return true
	}
	return false
}

// activityColors maps each activity kind to a display color.
var activityColors = map[ActivityKind]lipgloss.Color{
	ActivityIdle:      colorMuted,
	ActivityWaiting:   colorAccent,  // green — needs user attention
	ActivityThinking:  colorWarning, // amber
	ActivityReading:   lipgloss.Color("#38BDF8"), // sky blue
	ActivityWriting:   lipgloss.Color("#FB923C"), // orange
	ActivityRunning:   lipgloss.Color("#A78BFA"), // violet
	ActivitySearching: lipgloss.Color("#34D399"), // emerald
	ActivityBrowsing:  lipgloss.Color("#22D3EE"), // cyan
	ActivitySpawning:  lipgloss.Color("#F472B6"), // pink
}

// activityEntry holds a session's current sticky activity state.
type activityEntry struct {
	kind     ActivityKind
	lastSeen time.Time // last time this kind was confirmed from JSONL
}

// resolveRawActivity determines the instantaneous activity from a session's
// current status and last tool — no stickiness applied here.
func resolveRawActivity(s *claude.Session) ActivityKind {
	switch s.Status {
	case claude.StatusWaitingForUser:
		return ActivityWaiting
	case claude.StatusThinking, claude.StatusProcessingResult:
		return ActivityThinking
	case claude.StatusExecutingTool:
		return toolActivity(s.CurrentTool)
	}
	return ActivityIdle
}

// toolActivity maps a Claude tool name to an activity kind.
func toolActivity(tool string) ActivityKind {
	switch tool {
	case "Read":
		return ActivityReading
	case "Write", "Edit", "NotebookEdit":
		return ActivityWriting
	case "Bash":
		return ActivityRunning
	case "Glob", "Grep":
		return ActivitySearching
	case "WebFetch", "WebSearch":
		return ActivityBrowsing
	case "Agent":
		return ActivitySpawning
	default:
		if tool != "" {
			return ActivityRunning // unknown tools treated as running
		}
		return ActivityIdle
	}
}

// updateActivities applies the sticky-timeout logic for all sessions.
// Called on every sessionsMsg refresh.
//
// Rules:
//  1. Tool active (transient) → enter/stay in that activity, reset timer.
//  2. Same transient activity again → reset timer only (no visual change).
//  3. Non-tool state (waiting/thinking/idle) but previous transient is within
//     timeout → keep showing the transient activity.
//  4. Timeout expired or never had a transient → show real state.
func (m *Model) updateActivities(now time.Time) {
	if m.activities == nil {
		m.activities = make(map[string]*activityEntry)
	}
	for _, s := range m.sessions {
		id := s.SessionID
		if id == "" {
			continue
		}
		raw := resolveRawActivity(s)
		entry, exists := m.activities[id]

		if raw.isTransient() {
			if !exists || entry.kind != raw {
				// New transient activity: switch immediately
				m.activities[id] = &activityEntry{kind: raw, lastSeen: now}
			} else {
				// Same transient: just reset the timer
				entry.lastSeen = now
			}
		} else {
			// Non-transient (waiting / thinking / idle)
			if exists && entry.kind.isTransient() && now.Sub(entry.lastSeen) < ActivityTimeout {
				// Still within timeout: keep showing the previous tool activity
			} else {
				// Timeout expired or was already non-transient
				m.activities[id] = &activityEntry{kind: raw, lastSeen: now}
			}
		}
	}
}

// activityFor returns the current sticky activity for a session.
func (m Model) activityFor(sessionID string) ActivityKind {
	if e, ok := m.activities[sessionID]; ok {
		return e.kind
	}
	return ActivityIdle
}

// renderActivity returns a styled activity label for use in the list row.
func renderActivity(kind ActivityKind) string {
	color, ok := activityColors[kind]
	if !ok {
		color = colorMuted
	}
	return lipgloss.NewStyle().Foreground(color).Bold(true).Render(string(kind))
}
