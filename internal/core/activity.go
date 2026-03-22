package core

import (
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// ActivityTimeout is the duration a tool-based activity stays visible after
// the tool finishes, unless replaced by a newer activity.
const ActivityTimeout = 30 * time.Second

// WaitingTimeout is how long "waiting" stays visible before falling back to idle.
const WaitingTimeout = 2 * time.Minute

// SpawningTimeout is how long a spawning activity (Agent, subagent, task) stays
// visible without new JSONL entries before falling back to idle. Spawned agents
// can run for minutes, so this is much longer than ActivityTimeout.
const SpawningTimeout = 20 * time.Minute

// WaitingGrace is the minimum time StatusWaitingForUser must be stable before
// displaying ActivityWaiting. Claude sometimes writes a text-only assistant message
// before immediately continuing with a tool_use message (~2s later), which would
// otherwise cause a brief false "waiting" flash.
const WaitingGrace = 10 * time.Second

// ActivityKind is the human-readable label shown in the session list.
type ActivityKind string

const (
	ActivityIdle       ActivityKind = "idle"
	ActivityWaiting    ActivityKind = "waiting"
	ActivityThinking   ActivityKind = "thinking"
	ActivityCompacting ActivityKind = "compacting"
	ActivityReading    ActivityKind = "reading"
	ActivityWriting    ActivityKind = "writing"
	ActivityRunning    ActivityKind = "running"
	ActivitySearching  ActivityKind = "searching"
	ActivityBrowsing   ActivityKind = "browsing"
	ActivitySpawning   ActivityKind = "spawning"
)

// AllActivities returns all ActivityKind values in display order (empty = all).
var AllActivities = []ActivityKind{
	"",
	ActivityIdle,
	ActivityWaiting,
	ActivityThinking,
	ActivityCompacting,
	ActivityReading,
	ActivityWriting,
	ActivityRunning,
	ActivitySearching,
	ActivityBrowsing,
	ActivitySpawning,
}

// IsActiveActivity returns true for any activity that represents ongoing work
// (i.e. everything except idle and waiting).
func IsActiveActivity(a ActivityKind) bool {
	return a != ActivityIdle && a != ActivityWaiting && a != ""
}

// ActivityEntry holds a session's current sticky activity state.
type ActivityEntry struct {
	Kind     ActivityKind
	LastSeen time.Time
}

// ResolveActivity determines the display activity for a session.
//
// Priority:
//  1. Compacting (summary entry written recently).
//  2. Most recent tool in RecentTools within ActivityTimeout.
//  3. StatusWaitingForUser within WaitingTimeout (with grace period).
//  4. LastActivity older than ActivityTimeout → idle.
//  5. Current JSONL status → thinking if Claude is processing, idle otherwise.
func ResolveActivity(s *model.Session, now time.Time) ActivityKind {
	sinceActivity := now.Sub(s.LastActivity)

	if !s.LastSummaryAt.IsZero() && now.Sub(s.LastSummaryAt) < ActivityTimeout {
		return ActivityCompacting
	}

	if len(s.RecentTools) > 0 {
		last := s.RecentTools[len(s.RecentTools)-1]
		if !last.Timestamp.IsZero() && now.Sub(last.Timestamp) < ActivityTimeout {
			return ToolActivity(last.Name)
		}
	}

	if s.Status == model.StatusWaitingForUser {
		if !s.LastActivity.IsZero() && sinceActivity < WaitingTimeout {
			return ActivityWaiting
		}
		return ActivityIdle
	}

	// Spawning tools (Agent, subagent, task) can run for minutes — use a
	// longer timeout while the session status still shows tool execution.
	if s.Status == model.StatusExecutingTool && ToolActivity(s.CurrentTool) == ActivitySpawning && sinceActivity < SpawningTimeout {
		return ActivitySpawning
	}

	if s.LastActivity.IsZero() || sinceActivity > ActivityTimeout {
		return ActivityIdle
	}

	switch s.Status {
	case model.StatusThinking, model.StatusProcessingResult:
		return ActivityThinking
	case model.StatusExecutingTool:
		return ToolActivity(s.CurrentTool)
	}
	return ActivityIdle
}

// ToolActivity maps a tool name to an activity kind.
// Supports both Claude Code (PascalCase) and pi (snake_case) tool names.
func ToolActivity(tool string) ActivityKind {
	switch tool {
	// Claude Code (PascalCase)
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
	// Cursor (raw names) — safety net for unnormalized names
	case "Shell":
		return ActivityRunning
	case "read_file", "Read_file_v2":
		return ActivityReading
	case "edit_file", "write_to_file", "Edit_file_v2", "Write_to_file_v2":
		return ActivityWriting
	case "codebase_search", "Codebase_search", "Glob_file_search", "Grep_search":
		return ActivitySearching
	// opencode / pi (snake_case) — safety net for unnormalized names
	case "read":
		return ActivityReading
	case "write", "edit":
		return ActivityWriting
	case "bash", "process":
		return ActivityRunning
	case "find", "lsp":
		return ActivitySearching
	case "web_search":
		return ActivityBrowsing
	case "subagent", "task":
		return ActivitySpawning
	default:
		if tool != "" {
			return ActivityRunning
		}
		return ActivityIdle
	}
}

// NextActivityFilter cycles to the next activity filter in AllActivities.
func NextActivityFilter(current ActivityKind) ActivityKind {
	for i, k := range AllActivities {
		if k == current {
			return AllActivities[(i+1)%len(AllActivities)]
		}
	}
	return ""
}

// ActivityTracker manages sticky activity states with grace period logic.
type ActivityTracker struct {
	activities   map[string]*ActivityEntry
	waitingSince map[string]time.Time
}

// NewActivityTracker creates a new ActivityTracker.
func NewActivityTracker() *ActivityTracker {
	return &ActivityTracker{
		activities:   make(map[string]*ActivityEntry),
		waitingSince: make(map[string]time.Time),
	}
}

// Update resolves and stores the current activity for each session.
// Applies a grace period before showing ActivityWaiting to avoid false positives.
func (t *ActivityTracker) Update(sessions []*model.Session, now time.Time) {
	activeIDs := make(map[string]struct{}, len(sessions))
	for _, s := range sessions {
		id := s.SessionID
		if id == "" {
			continue
		}
		activeIDs[id] = struct{}{}
		activity := ResolveActivity(s, now)

		if activity == ActivityWaiting {
			if _, seen := t.waitingSince[id]; !seen {
				t.waitingSince[id] = now
			}
			if now.Sub(t.waitingSince[id]) < WaitingGrace {
				continue
			}
		} else {
			delete(t.waitingSince, id)
		}

		t.activities[id] = &ActivityEntry{Kind: activity, LastSeen: now}
	}
	for id := range t.activities {
		if _, ok := activeIDs[id]; !ok {
			delete(t.activities, id)
			delete(t.waitingSince, id)
		}
	}
}

// Get returns the current sticky activity for a session.
func (t *ActivityTracker) Get(sessionID string) ActivityKind {
	if e, ok := t.activities[sessionID]; ok {
		return e.Kind
	}
	return ActivityIdle
}
