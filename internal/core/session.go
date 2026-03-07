package core

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nahime0/lazyagent/internal/claude"
)

// Window minutes bounds.
const (
	MinWindowMinutes = 10
	MaxWindowMinutes = 480
)

// SessionDetailView is the full struct for a detail panel.
type SessionDetailView struct {
	claude.Session
	Activity ActivityKind
}

// SessionManager manages session discovery, file watching, and activity tracking.
type SessionManager struct {
	mu       sync.RWMutex
	sessions []*claude.Session
	tracker  *ActivityTracker
	watcher  *ProjectWatcher

	windowMinutes  int
	activityFilter ActivityKind
	searchQuery    string
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(windowMinutes int) *SessionManager {
	return &SessionManager{
		tracker:       NewActivityTracker(),
		windowMinutes: windowMinutes,
	}
}

// StartWatcher starts the file system watcher. Returns nil if the directory doesn't exist.
func (m *SessionManager) StartWatcher() error {
	w, err := NewProjectWatcher()
	if err != nil {
		return err
	}
	m.watcher = w
	return nil
}

// StopWatcher stops the file system watcher.
func (m *SessionManager) StopWatcher() {
	if m.watcher != nil {
		m.watcher.Close()
	}
}

// WatcherEvents returns the channel for file change notifications, or nil.
func (m *SessionManager) WatcherEvents() <-chan struct{} {
	if m.watcher != nil {
		return m.watcher.Events
	}
	return nil
}

// Reload discovers sessions from disk and updates activity states.
func (m *SessionManager) Reload() error {
	sessions, err := claude.DiscoverSessions()
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.sessions = sessions
	m.tracker.Update(sessions, time.Now())
	SortSessions(m.sessions)
	m.mu.Unlock()
	return nil
}

// UpdateActivities refreshes activity states without reloading from disk.
// Returns true if any activity state changed.
func (m *SessionManager) UpdateActivities() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	old := make(map[string]ActivityKind, len(m.sessions))
	for _, s := range m.sessions {
		old[s.SessionID] = m.tracker.Get(s.SessionID)
	}
	m.tracker.Update(m.sessions, time.Now())
	for _, s := range m.sessions {
		if m.tracker.Get(s.SessionID) != old[s.SessionID] {
			return true
		}
	}
	return false
}

// SetWindowMinutes sets the time window filter, clamped to [MinWindowMinutes, MaxWindowMinutes].
func (m *SessionManager) SetWindowMinutes(minutes int) {
	m.mu.Lock()
	m.windowMinutes = Clamp(MinWindowMinutes, MaxWindowMinutes, minutes)
	m.mu.Unlock()
}

// WindowMinutes returns the current time window.
func (m *SessionManager) WindowMinutes() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.windowMinutes
}

// ActivityFilter returns the current activity filter.
func (m *SessionManager) ActivityFilter() ActivityKind {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activityFilter
}

// SetActivityFilter sets the activity filter.
func (m *SessionManager) SetActivityFilter(filter ActivityKind) {
	m.mu.Lock()
	m.activityFilter = filter
	m.mu.Unlock()
}

// SetSearchQuery sets the search query.
func (m *SessionManager) SetSearchQuery(query string) {
	m.mu.Lock()
	m.searchQuery = query
	m.mu.Unlock()
}

// ActivityFor returns the current activity for a session.
func (m *SessionManager) ActivityFor(sessionID string) ActivityKind {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tracker.Get(sessionID)
}

// Sessions returns all raw sessions (unfiltered).
func (m *SessionManager) Sessions() []*claude.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions
}

// VisibleSessions returns sessions filtered by time window, activity, and search query.
func (m *SessionManager) VisibleSessions() []*claude.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cutoff := time.Now().Add(-time.Duration(m.windowMinutes) * time.Minute)
	lowerQuery := strings.ToLower(m.searchQuery)
	var visible []*claude.Session
	for _, s := range m.sessions {
		if s.IsSidechain || !s.LastActivity.After(cutoff) {
			continue
		}
		if m.activityFilter != "" && m.tracker.Get(s.SessionID) != m.activityFilter {
			continue
		}
		if lowerQuery != "" && !strings.Contains(strings.ToLower(s.CWD), lowerQuery) {
			continue
		}
		visible = append(visible, s)
	}
	return visible
}

// SessionDetail returns the full detail view for a session.
func (m *SessionManager) SessionDetail(id string) *SessionDetailView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if s.SessionID == id {
			return &SessionDetailView{
				Session:  *s,
				Activity: m.tracker.Get(id),
			}
		}
	}
	return nil
}

// SortSessions sorts sessions by last activity (most recent first).
func SortSessions(sessions []*claude.Session) {
	slices.SortFunc(sessions, func(a, b *claude.Session) int {
		return cmp.Compare(b.LastActivity.UnixNano(), a.LastActivity.UnixNano())
	})
}
