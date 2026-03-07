package ui

import (
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nahime0/lazyagent/internal/claude"
)

// ActivityTimeout is the duration a tool-based activity stays visible after
// the tool finishes, unless replaced by a newer activity.
const ActivityTimeout = 30 * time.Second

// WaitingTimeout is how long "waiting" stays visible before falling back to idle.
const WaitingTimeout = 2 * time.Minute

// WaitingGrace is the minimum time StatusWaitingForUser must be stable before
// displaying ActivityWaiting. Claude sometimes writes a text-only assistant message
// before immediately continuing with a tool_use message (~2s later), which would
// otherwise cause a brief false "waiting" flash.
const WaitingGrace = 10 * time.Second

// ActivityKind is the human-readable label shown in the session list.
type ActivityKind string

const (
	ActivityIdle       ActivityKind = "idle"
	ActivityWaiting    ActivityKind = "waiting"    // Claude finished, awaiting user input
	ActivityThinking   ActivityKind = "thinking"   // Claude generating a response
	ActivityCompacting ActivityKind = "compacting" // Context compaction in progress
	ActivityReading    ActivityKind = "reading"    // Read
	ActivityWriting    ActivityKind = "writing"    // Write / Edit
	ActivityRunning    ActivityKind = "running"    // Bash
	ActivitySearching  ActivityKind = "searching"  // Glob / Grep
	ActivityBrowsing   ActivityKind = "browsing"   // WebFetch / WebSearch
	ActivitySpawning   ActivityKind = "spawning"   // Agent (subagent)
)

// activityColors maps each activity kind to a display color.
var activityColors = map[ActivityKind]lipgloss.Color{
	ActivityIdle:       colorMuted,
	ActivityWaiting:    lipgloss.Color("#4ADE80"), // green
	ActivityThinking:   colorWarning,              // amber
	ActivityCompacting: lipgloss.Color("#2DD4BF"), // teal
	ActivityReading:    lipgloss.Color("#38BDF8"), // sky blue
	ActivityWriting:    lipgloss.Color("#FB923C"), // orange
	ActivityRunning:    lipgloss.Color("#A78BFA"), // violet
	ActivitySearching:  lipgloss.Color("#34D399"), // emerald
	ActivityBrowsing:   lipgloss.Color("#22D3EE"), // cyan
	ActivitySpawning:   lipgloss.Color("#F472B6"), // pink
}

// isActiveActivity returns true for any activity that represents ongoing work
// (i.e. everything except idle and waiting).
func isActiveActivity(a ActivityKind) bool {
	return a != ActivityIdle && a != ActivityWaiting && a != ""
}

// activityEntry holds a session's current sticky activity state.
type activityEntry struct {
	kind     ActivityKind
	lastSeen time.Time // last time this kind was confirmed from JSONL
}

// resolveActivity determines the display activity for a session.
//
// Priority:
//  1. Compacting (summary entry written recently).
//  2. Most recent tool in RecentTools within ActivityTimeout → show that tool activity.
//     Must come before WaitingForUser: Claude often uses a tool and then sends a final
//     text response, making status flip to WaitingForUser while the tool is still recent.
//  3. StatusWaitingForUser within WaitingTimeout (2m) → "waiting" (with grace period).
//  4. LastActivity older than ActivityTimeout (30s) → idle.
//  5. Current JSONL status → thinking if Claude is processing, idle otherwise.
func resolveActivity(s *claude.Session, now time.Time) ActivityKind {
	sinceActivity := now.Sub(s.LastActivity)

	// Context compaction: a summary entry was written recently.
	if !s.LastSummaryAt.IsZero() && now.Sub(s.LastSummaryAt) < ActivityTimeout {
		return ActivityCompacting
	}

	// Most recent tool within timeout takes priority over all states, including
	// WaitingForUser, since the tool ran just before Claude sent its final response.
	if len(s.RecentTools) > 0 {
		last := s.RecentTools[len(s.RecentTools)-1]
		if !last.Timestamp.IsZero() && now.Sub(last.Timestamp) < ActivityTimeout {
			return toolActivity(last.Name)
		}
	}

	// Claude finished responding and is waiting for user input.
	if s.Status == claude.StatusWaitingForUser {
		if !s.LastActivity.IsZero() && sinceActivity < WaitingTimeout {
			return ActivityWaiting
		}
		return ActivityIdle
	}

	// Gate on LastActivity for all other states.
	if s.LastActivity.IsZero() || sinceActivity > ActivityTimeout {
		return ActivityIdle
	}

	// Active but no recent tool — use JSONL status.
	switch s.Status {
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

// updateActivities resolves and stores the current activity for each session.
// Applies a grace period before showing ActivityWaiting to avoid false positives
// when Claude writes an intermediate text message before continuing with a tool.
func (m *Model) updateActivities(now time.Time) {
	if m.activities == nil {
		m.activities = make(map[string]*activityEntry)
	}
	if m.waitingSince == nil {
		m.waitingSince = make(map[string]time.Time)
	}
	activeIDs := make(map[string]struct{}, len(m.sessions))
	for _, s := range m.sessions {
		id := s.SessionID
		if id == "" {
			continue
		}
		activeIDs[id] = struct{}{}
		activity := resolveActivity(s, now)

		if activity == ActivityWaiting {
			// Start the grace period timer on first observation.
			if _, seen := m.waitingSince[id]; !seen {
				m.waitingSince[id] = now
			}
			// Only show "waiting" once the state has been stable for WaitingGrace.
			if now.Sub(m.waitingSince[id]) < WaitingGrace {
				continue // keep previous activity during grace period
			}
		} else {
			// Left the waiting state — reset grace period timer.
			delete(m.waitingSince, id)
		}

		m.activities[id] = &activityEntry{kind: activity, lastSeen: now}
	}
	// Remove stale entries for sessions that no longer exist.
	for id := range m.activities {
		if _, ok := activeIDs[id]; !ok {
			delete(m.activities, id)
			delete(m.waitingSince, id)
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

