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

// SessionProvider abstracts how sessions are discovered.
type SessionProvider interface {
	// DiscoverSessions returns all available sessions.
	DiscoverSessions() ([]*claude.Session, error)
	// UseWatcher returns whether a file system watcher should be started.
	UseWatcher() bool
	// RefreshInterval returns how often UpdateActivities should re-discover sessions,
	// or 0 to never re-discover (only on explicit Reload or watcher events).
	RefreshInterval() time.Duration
	// WatchDirs returns directories to watch for file system changes.
	WatchDirs() []string
}

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
	provider SessionProvider
	names    *SessionNames

	windowMinutes  int
	activityFilter ActivityKind
	searchQuery    string
	lastDiscover   time.Time
}

// NewSessionManager creates a new SessionManager with the given provider.
func NewSessionManager(windowMinutes int, provider SessionProvider) *SessionManager {
	return &SessionManager{
		tracker:       NewActivityTracker(),
		windowMinutes: windowMinutes,
		provider:      provider,
		names:         NewSessionNames(),
	}
}

// SessionName returns the custom name for a session, or empty string.
func (m *SessionManager) SessionName(sessionID string) string {
	return m.names.Get(sessionID)
}

// SetSessionName stores a custom name for a session.
func (m *SessionManager) SetSessionName(sessionID, name string) error {
	return m.names.Set(sessionID, name)
}

// StartWatcher starts the file system watcher if the provider supports it.
func (m *SessionManager) StartWatcher() error {
	if !m.provider.UseWatcher() {
		return nil
	}
	w, err := NewProjectWatcher(m.provider.WatchDirs()...)
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

// Reload discovers sessions via the provider and updates activity states.
func (m *SessionManager) Reload() error {
	sessions, err := m.provider.DiscoverSessions()
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.sessions = sessions
	m.tracker.Update(sessions, time.Now())
	SortSessions(m.sessions)
	m.lastDiscover = time.Now()
	m.mu.Unlock()
	return nil
}

// UpdateActivities refreshes activity states without reloading from disk.
// Returns true if any activity state changed.
// If the provider specifies a RefreshInterval, sessions are re-discovered periodically.
// Also refreshes session names from disk if modified externally.
func (m *SessionManager) UpdateActivities() bool {
	namesChanged := m.names.Refresh()
	if interval := m.provider.RefreshInterval(); interval > 0 {
		now := time.Now()
		m.mu.RLock()
		needsRefresh := len(m.sessions) == 0 || now.Sub(m.lastDiscover) > interval
		m.mu.RUnlock()
		if needsRefresh {
			sessions, err := m.provider.DiscoverSessions()
			if err == nil {
				m.mu.Lock()
				m.sessions = sessions
				m.tracker.Update(sessions, time.Now())
				SortSessions(m.sessions)
				m.lastDiscover = time.Now()
				m.mu.Unlock()
				return true
			}
		}
	}
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
	return namesChanged
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

// VisibleSessions returns sessions filtered by the manager's internal state
// (time window, activity filter, search query). Used by TUI and tray.
func (m *SessionManager) VisibleSessions() []*claude.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.filterSessionsLocked(m.searchQuery, m.activityFilter)
}

// QuerySessions returns sessions filtered by explicit parameters without using
// the manager's internal filter/search state. Safe for concurrent API use.
// Empty search/filter means no filtering.
func (m *SessionManager) QuerySessions(search string, filter ActivityKind) []*claude.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.filterSessionsLocked(search, filter)
}

// filterSessionsLocked applies time window, activity, and search filters.
// Must be called with m.mu held (at least RLock).
func (m *SessionManager) filterSessionsLocked(search string, filter ActivityKind) []*claude.Session {
	cutoff := time.Now().Add(-time.Duration(m.windowMinutes) * time.Minute)
	lowerQuery := strings.ToLower(search)
	var visible []*claude.Session
	for _, s := range m.sessions {
		if s.IsSidechain || !s.LastActivity.After(cutoff) {
			continue
		}
		if filter != "" && m.tracker.Get(s.SessionID) != filter {
			continue
		}
		if lowerQuery != "" {
			matchCWD := strings.Contains(strings.ToLower(s.CWD), lowerQuery)
			matchName := strings.Contains(strings.ToLower(m.names.Get(s.SessionID)), lowerQuery)
			if !matchCWD && !matchName {
				continue
			}
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
