package core

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/cloud"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// Window minutes bounds.
const (
	MinWindowMinutes = 10
	MaxWindowMinutes = 480
)

// cacheSaveInterval is the minimum time between persisted-discovery-cache
// saves that a dirty (but not first) Reload() triggers. Long-lived surfaces
// (TUI/GUI/API) reload on every watcher event, and each save rewrites a few
// MB of JSON, so successful reloads after the first just mark the cache
// dirty; an actual save happens at most this often. See
// SessionManager.EnableCachePersistence.
const cacheSaveInterval = 5 * time.Minute

// SessionProvider abstracts how sessions are discovered.
type SessionProvider interface {
	// DiscoverSessions returns all available sessions.
	DiscoverSessions() ([]*model.Session, error)
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
	model.Session
	Activity ActivityKind
}

// SessionManager manages session discovery, file watching, and activity tracking.
type SessionManager struct {
	mu              sync.RWMutex
	sessions        []*model.Session
	tracker         *ActivityTracker
	watcher         *ProjectWatcher
	provider        SessionProvider
	names           *SessionNames
	cloud           *cloud.Cursor
	cloudConfigErr  error
	cloudGeneration uint64
	cloudSessions   map[string]*model.Session
	cloudShowIdle   bool
	cloudState      CloudStatus

	windowMinutes        int
	activityFilter       ActivityKind
	searchQuery          string
	lastPollDiscover     time.Time
	excludeCWDSubstrings []string

	// streamInFlight is the guard for the progressive-first-load /
	// steady-state interplay: true for the duration of a streaming reload,
	// false the rest of the time. Set synchronously by BeginReloadStreaming,
	// before ANY of the streaming reload's own work begins -- not merely
	// "before DiscoverMatchingStream starts": loadPersistedCacheOnce (a
	// multi-MB JSON parse when a persisted cache exists) runs first and can
	// easily take longer than a watcher event or a 1s ticker takes to fire,
	// so the flag must already be true before that step, or a Reload/
	// UpdateActivities call landing during it would pass the guard, run its
	// own full discovery, and race the stream's own per-batch appends into
	// duplicate SessionIDs. Cleared once DiscoverMatchingStream's done
	// channel closes, before any catch-up Reload (see pendingReload) or the
	// completion save. Reload and UpdateActivities' periodic re-discovery
	// both check it and back off rather than racing the stream's own
	// accumulating per-batch merges -- see their doc comments for exactly
	// how each responds.
	streamInFlight bool

	// pendingReload latches a Reload() call that Reload absorbed while
	// streamInFlight was true (see Reload's doc comment): set under the
	// same lock as the absorb check, cleared and acted on once the stream
	// completes (see runReloadStreaming), which runs exactly one catch-up
	// Reload if this was ever set. Without this, a watcher-triggered Reload
	// for an all-watcher-provider setup with no periodic RefreshInterval
	// (e.g. claude/pi/grok/kimi) that lands mid-stream would be silently
	// lost until some future, unrelated watcher event -- which might never
	// come for that exact change.
	pendingReload bool

	// Persisted discovery cache (opt-in, see EnableCachePersistence). All
	// guarded by mu like everything else above; the actual Load/Save disk
	// I/O always happens outside the lock (see loadPersistedCacheOnce /
	// savePersistedCacheIfDue / Close) so a slow write never blocks readers
	// like Sessions()/VisibleSessions().
	persistEnabled  bool
	persistDir      string
	persistLoaded   bool      // Load has been attempted (first Reload only)
	persistDirty    bool      // a successful Reload happened since the last save
	lastPersistSave time.Time // zero until the first save
	persistNow      func() time.Time
}

// NewSessionManager creates a new SessionManager with the given provider.
func NewSessionManager(windowMinutes int, provider SessionProvider) *SessionManager {
	return &SessionManager{
		tracker:       NewActivityTracker(),
		windowMinutes: windowMinutes,
		provider:      provider,
		names:         NewSessionNames(),
		persistNow:    time.Now,
	}
}

// EnableCachePersistence opts the manager into persisting the provider's
// discovery caches under dir between process runs, via
// core.CachePersister / LoadProviderCaches / SaveProviderCaches (the same
// mechanism the `sessions` subcommand already uses). Call it before the
// first Reload(). Once enabled:
//   - the FIRST Reload() loads any previously persisted caches before
//     discovering, and never loads again on later reloads;
//   - after the first successful discovery, the caches are saved once;
//   - later successful reloads mark the cache dirty; an actual save then
//     happens at most once per cacheSaveInterval;
//   - Close() performs a final save if the cache is still dirty at shutdown.
//
// Persistence is purely advisory: providers that don't implement
// CachePersister (e.g. demo.Provider) make every Load/Save call a silent
// no-op via the LoadProviderCaches/SaveProviderCaches helpers, and a load or
// save failure never affects discovery.
func (m *SessionManager) EnableCachePersistence(dir string) {
	m.mu.Lock()
	m.persistEnabled = true
	m.persistDir = dir
	m.mu.Unlock()
}

// loadPersistedCacheOnce loads the provider's persisted discovery caches on
// the first call after EnableCachePersistence, and is a no-op on every call
// after that (or when persistence was never enabled). Must run before the
// provider's first DiscoverSessions call so that discovery reuses the prior
// run's cache. The actual disk I/O happens without m.mu held.
func (m *SessionManager) loadPersistedCacheOnce() {
	m.mu.Lock()
	if !m.persistEnabled || m.persistLoaded {
		m.mu.Unlock()
		return
	}
	m.persistLoaded = true
	dir := m.persistDir
	m.mu.Unlock()

	LoadProviderCaches(m.provider, dir)
}

// savePersistedCacheIfDue runs after a successful Reload when persistence is
// enabled. The very first save (lastPersistSave still zero) always happens,
// so a persisted cache exists as early as possible; subsequent saves happen
// at most once per cacheSaveInterval, with reloads in between just marking
// the cache dirty for Close's final save. The actual disk I/O happens
// without m.mu held.
func (m *SessionManager) savePersistedCacheIfDue() {
	m.mu.Lock()
	if !m.persistEnabled {
		m.mu.Unlock()
		return
	}
	now := m.persistNow()
	due := m.lastPersistSave.IsZero() || now.Sub(m.lastPersistSave) >= cacheSaveInterval
	if !due {
		m.persistDirty = true
		m.mu.Unlock()
		return
	}
	dir := m.persistDir
	m.lastPersistSave = now
	m.persistDirty = false
	m.mu.Unlock()

	SaveProviderCaches(m.provider, dir)
}

// SetEventBus attaches an event bus so activity transitions are published
// to subscribers. Pass nil to detach.
func (m *SessionManager) SetEventBus(bus *EventBus) {
	m.mu.Lock()
	m.tracker.SetEventBus(bus)
	m.mu.Unlock()
}

// SetExcludeCWDSubstrings sets the CWD substring patterns used to exclude
// sessions from filtered views (VisibleSessions / QuerySessions).
func (m *SessionManager) SetExcludeCWDSubstrings(patterns []string) {
	m.mu.Lock()
	m.excludeCWDSubstrings = slices.Clone(patterns)
	m.mu.Unlock()
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

// Close is the shutdown hook the long-lived surfaces (TUI, GUI/tray, API)
// should call in place of StopWatcher: it stops the file watcher and, if
// cache persistence is enabled and a Reload happened since the last save
// (the cache is dirty — see savePersistedCacheIfDue), performs one final
// save so shutdown never loses more than the most recent in-flight reload.
// A no-op final save (persistence disabled, or already clean) costs nothing
// beyond the StopWatcher call it always makes.
func (m *SessionManager) Close() {
	m.StopWatcher()

	m.mu.Lock()
	cloudClient := m.cloud
	m.cloud = nil
	m.cloudGeneration++
	m.cloudSessions = nil
	due := m.persistEnabled && m.persistDirty
	dir := m.persistDir
	m.persistDirty = false
	m.mu.Unlock()
	if cloudClient != nil {
		cloudClient.Close()
	}

	if due {
		SaveProviderCaches(m.provider, dir)
	}
}

// WatcherEvents returns the channel for file change notifications, or nil.
func (m *SessionManager) WatcherEvents() <-chan struct{} {
	if m.watcher != nil {
		return m.watcher.Events
	}
	return nil
}

// discoverHonoringExclusions discovers sessions via m.provider, applying the
// exclude_cwd_substrings pushdown when patterns are configured, or the
// provider's plain DiscoverSessions() when they aren't. This is the single
// implementation of "discover, honoring exclusions" shared by every
// re-discovery path — Reload (first load, watcher events) and
// UpdateActivities (periodic re-discovery for providers with
// RefreshInterval > 0, e.g. opencode/kilo/codex) — so the pushdown applies
// uniformly no matter which path triggers a given discovery, not just the
// first one.
//
// When exclude_cwd_substrings patterns are configured, discovery itself is
// scoped via DiscoverMatching(m.provider, excludeCWDPredicate(patterns)):
// dir-scoped providers (DirScopedProvider) skip parsing excluded sessions
// entirely instead of discovering-then-hiding them at the view layer. With
// no patterns set, this calls the provider's plain DiscoverSessions(),
// exactly as before this pushdown existed — zero behavior change for the
// common case. Either way, the view-level filter (filterSessionsLocked)
// still re-applies the same exclusion: DiscoverMatching's contract leaves
// non-dir-scoped providers' results unfiltered, so the view filter remains
// the only thing guaranteeing exclusion for those.
//
// Runtime pattern changes: SetExcludeCWDSubstrings can be called at any
// time, and this re-reads m.excludeCWDSubstrings fresh on every call, so
// the very next discovery — via either Reload() or UpdateActivities() —
// automatically applies the new patterns, no extra plumbing needed. (As of
// this change, no caller actually changes patterns after startup; see the
// report for that investigation.)
func (m *SessionManager) discoverHonoringExclusions() ([]*model.Session, error) {
	matcher := m.exclusionMatcher()
	if matcher == nil {
		return m.provider.DiscoverSessions()
	}
	return DiscoverMatching(m.provider, matcher)
}

// exclusionMatcher returns the cwdMatch predicate for the
// exclude_cwd_substrings pushdown: nil when no patterns are configured, or
// excludeCWDPredicate(patterns) otherwise. This is the single place that
// decides the predicate -- discoverHonoringExclusions (used by Reload and
// UpdateActivities' periodic re-discovery) and ReloadStreaming (the
// progressive first load) both call it, so the pushdown can never diverge
// between the synchronous and streaming discovery paths.
//
// nil is a deliberate, meaningful value here, not merely "no filtering
// needed": discoverHonoringExclusions treats it as "skip DiscoverMatching's
// dir-scoped fast-path machinery entirely and call the provider's plain,
// unscoped DiscoverSessions()" -- the exact zero-diff behavior Reload had
// before the exclusion pushdown existed. ReloadStreaming, whose per-member
// fan-out has no such top-level bypass (DiscoverMatchingStream always
// dispatches through discoverMatchingOne per member), instead passes the
// nil straight through to DiscoverMatchingStream: every DirScopedProvider
// implementation in this codebase (claude, codex, opencode, kilo) documents
// "cwdMatch may be nil, in which case every session matches", so a nil
// matcher is output-equivalent to the plain-DiscoverSessions() bypass
// either way -- just reached via a different call path, since streaming's
// architecture requires going through the per-member dispatch regardless.
func (m *SessionManager) exclusionMatcher() func(cwd string) bool {
	m.mu.RLock()
	patterns := m.excludeCWDSubstrings
	m.mu.RUnlock()
	if len(patterns) == 0 {
		return nil
	}
	return excludeCWDPredicate(patterns)
}

// Reload discovers sessions via the provider and updates activity states.
// When cache persistence is enabled (EnableCachePersistence), this is also
// where the persisted cache gets loaded (once, before the first discovery)
// and saved (see loadPersistedCacheOnce / savePersistedCacheIfDue). See
// discoverHonoringExclusions for how the exclude_cwd_substrings pushdown is
// applied.
//
// While the manager's progressive first load is still in flight
// (streamInFlight), Reload is a no-op that returns nil immediately instead
// of running a second, independent discovery concurrently with the
// stream's own accumulating per-batch merges, and latches pendingReload so
// the call isn't lost (see runReloadStreaming's catch-up). This absorbs
// watcher/ticker-driven Reload calls that land mid-stream: the TUI's
// fileWatchMsg/tickMsg handlers and the tray/API's watchLoop all call
// Reload unconditionally on every watcher event or fallback tick, with no
// way to know a stream is running. The stream is already the freshest full
// discovery in progress, and letting a concurrent Reload replace m.sessions
// mid-stream would either lose sessions a later batch alone would have
// merged in, or -- since the stream's batches are appended, not replaced --
// let a later batch duplicate sessions Reload's own complete discovery
// already included. Absorbing the call (rather than blocking until the
// stream finishes, or queueing it to run concurrently anyway) is the
// simplest rule that's provably safe under concurrency; pendingReload is
// what keeps "absorbed" from also meaning "silently dropped forever" for a
// provider with no periodic RefreshInterval to naturally retry on.
func (m *SessionManager) Reload() error {
	m.mu.Lock()
	if m.streamInFlight {
		m.pendingReload = true
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	m.loadPersistedCacheOnce()

	sessions, err := m.discoverHonoringExclusions()
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.sessions = sessions
	m.tracker.Update(sessions, time.Now())
	SortSessions(m.sessions)
	m.lastPollDiscover = time.Now()
	m.mu.Unlock()

	m.savePersistedCacheIfDue()
	return nil
}

// BeginReloadStreaming synchronously marks the manager's progressive first
// load as in-flight (streamInFlight -- see the field doc) and returns a
// func that performs the actual streaming discovery; the caller must run
// that func (typically in its own goroutine, for progressive rendering) to
// perform the load, passing the onUpdate callback ReloadStreaming's doc
// describes.
//
// This split exists because a caller that does
// `go mgr.ReloadStreaming(onUpdate)` and then immediately starts a watcher
// or poll loop cannot guarantee streamInFlight is already true by the time
// that loop's first event/tick fires: Go makes no promise a spawned
// goroutine's first statement runs before the spawning goroutine's next
// one, and even if it did, ReloadStreaming's own first real step
// (loadPersistedCacheOnce) can easily take longer than a 1s ticker or an
// already-queued watcher event needs to fire anyway. Both real production
// callers (internal/ui's Init, internal/tray's ServiceStartup) call
// BeginReloadStreaming synchronously -- before starting anything else that
// could race a Reload/UpdateActivities call -- and only run the returned
// func in a goroutine; see their doc comments for how each wires this up.
func (m *SessionManager) BeginReloadStreaming() (run func(onUpdate func())) {
	m.mu.Lock()
	m.streamInFlight = true
	m.mu.Unlock()
	return m.runReloadStreaming
}

// ReloadStreaming performs the manager's progressive FIRST load: instead of
// waiting for every provider to finish (the barrier
// MultiProvider.DiscoverSessions imposes) before showing anything, each
// provider member's sessions are merged into the snapshot -- and onUpdate
// (if non-nil) is called -- as soon as that member completes, via
// core.DiscoverMatchingStream. It blocks until every member has finished
// (and, if a Reload was absorbed while it ran, until the resulting
// catch-up Reload finishes too -- see runReloadStreaming). onUpdate is
// always called with m.mu NOT held, so it's safe for onUpdate to call back
// into the manager (Sessions(), VisibleSessions(), etc.) without
// deadlocking; it may also be called concurrently by more than one
// member's goroutine, so it must itself be safe for concurrent use.
//
// This is exactly BeginReloadStreaming immediately followed by running the
// returned func in the current goroutine -- a convenience for callers
// (including most tests) that don't need streamInFlight to be guaranteed
// true before they do anything else concurrently. See BeginReloadStreaming
// when that ordering matters, which it does for both real production
// callers.
//
// Uses exclusionMatcher -- the SAME predicate-selection logic
// discoverHonoringExclusions uses -- so the exclude_cwd_substrings pushdown
// applies identically on the streaming path.
//
// Intended to be called exactly once, as the very first load, before any
// Reload().
//
// Persistence (when EnableCachePersistence was called): the persisted cache
// is loaded once before discovery starts, exactly like Reload's first call
// (loadPersistedCacheOnce is idempotent, so whichever of Reload/
// ReloadStreaming runs first "wins" the load). On completion, the cache is
// normally saved exactly once -- never per batch -- via
// savePersistedCacheIfDue, regardless of whether any member actually found
// sessions: DiscoverMatchingStream has no error channel (best-effort per
// member, by design, same as MultiProvider.DiscoverSessions), so "every
// member failed" is indistinguishable from "every member genuinely found
// nothing" at this level. Unlike Reload (which skips the save entirely on a
// hard discovery error), ReloadStreaming therefore always saves on
// completion, since "the stream finished" -- not "discovery succeeded" --
// is its completion signal. (The one exception: if a Reload was absorbed
// mid-stream, the catch-up Reload's own save covers completion instead --
// see runReloadStreaming.) If every member fails or emits nothing, the
// manager simply ends with an empty snapshot: the same observable outcome
// Reload's own error path already produces via empty views today, and the
// resulting save is advisory and harmless (see CachePersister) -- no new
// error surface is introduced.
func (m *SessionManager) ReloadStreaming(onUpdate func()) {
	m.BeginReloadStreaming()(onUpdate)
}

// runReloadStreaming is BeginReloadStreaming's returned func -- see both
// its doc and ReloadStreaming's for the full contract. streamInFlight is
// already true by the time this runs (BeginReloadStreaming set it
// synchronously before returning it), so everything here can safely take
// as long as it needs to.
func (m *SessionManager) runReloadStreaming(onUpdate func()) {
	m.loadPersistedCacheOnce()

	matcher := m.exclusionMatcher()

	done := DiscoverMatchingStream(m.provider, matcher, func(batch []*model.Session) {
		m.mu.Lock()
		m.sessions = append(m.sessions, batch...)
		m.tracker.Update(m.sessions, time.Now())
		SortSessions(m.sessions)
		m.lastPollDiscover = time.Now()
		m.mu.Unlock()

		if onUpdate != nil {
			onUpdate()
		}
	})
	<-done

	m.mu.Lock()
	m.streamInFlight = false
	pending := m.pendingReload
	m.pendingReload = false
	m.mu.Unlock()

	if pending {
		// A watcher/ticker-driven Reload was absorbed while the stream was
		// in flight (see Reload's doc comment) -- catch up now with one
		// full, ordinary Reload, rather than risk silently losing whatever
		// it would have picked up until some future watcher event (which,
		// for an all-watcher provider setup with no periodic
		// RefreshInterval, e.g. claude/pi/grok/kimi, might never come again
		// for that exact change). Reload replaces m.sessions with a fresh,
		// complete, non-duplicated discovery -- superseding the stream's
		// own accumulated batches entirely -- and already calls
		// savePersistedCacheIfDue as part of its own normal flow, so that
		// IS this call's completion save; skip the explicit one below to
		// avoid a redundant (harmless, but pointless) second save attempt
		// back to back. onUpdate is called again so a progressive-rendering
		// caller notices the catch-up's effect too, not just whatever the
		// stream's own batches produced.
		_ = m.Reload()
		if onUpdate != nil {
			onUpdate()
		}
		return
	}

	m.savePersistedCacheIfDue()
}

// UpdateActivities refreshes activity states without reloading from disk.
// Returns true if any activity state changed.
// If the provider specifies a RefreshInterval, sessions are re-discovered
// periodically — via discoverHonoringExclusions, same as Reload, so the
// exclude_cwd_substrings pushdown applies to this periodic re-discovery
// too, not just the first one.
// Also refreshes session names from disk if modified externally.
//
// While a progressive first load is in flight (streamInFlight -- see
// ReloadStreaming), the periodic re-discovery branch below is skipped: a
// 3s poll firing mid-stream must not replace the accumulating snapshot with
// a full (or worse, partial-provider, since the poll could itself land
// between two of the stream's own batches) re-discovery. The activity-only
// recomputation further down (tracker.Update against whatever's currently
// in m.sessions) still runs regardless -- it only reads the existing lock-
// guarded snapshot, so it's safe to run concurrently with the stream's own
// merges and keeps activity states from going stale for sessions that have
// already streamed in.
func (m *SessionManager) UpdateActivities() bool {
	namesChanged := m.names.Refresh()
	if interval := m.provider.RefreshInterval(); interval > 0 {
		now := time.Now()
		m.mu.RLock()
		streaming := m.streamInFlight
		needsRefresh := len(m.sessions) == 0 || now.Sub(m.lastPollDiscover) > interval
		m.mu.RUnlock()
		if needsRefresh && !streaming {
			sessions, err := m.discoverHonoringExclusions()
			if err == nil {
				m.mu.Lock()
				m.sessions = sessions
				m.tracker.Update(sessions, time.Now())
				SortSessions(m.sessions)
				m.lastPollDiscover = time.Now()
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
	if session := m.cloudSessions[sessionID]; session != nil {
		return cloudActivity(session)
	}
	return m.tracker.Get(sessionID)
}

// Sessions returns all raw sessions (unfiltered).
func (m *SessionManager) Sessions() []*model.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions
}

// VisibleSessions returns sessions filtered by the manager's internal state
// (time window, activity filter, search query). Used by TUI and tray.
func (m *SessionManager) VisibleSessions() []*model.Session {
	m.UpdateCloud()
	m.mu.RLock()
	defer m.mu.RUnlock()
	visible := m.filterSessionsLocked(m.searchQuery, m.activityFilter)
	query := strings.ToLower(m.searchQuery)
	for _, session := range m.cloudSessions {
		activity := cloudActivity(session)
		if activity == ActivityIdle && !m.cloudShowIdle {
			continue
		}
		if m.activityFilter != "" && activity != m.activityFilter {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(session.Name+" "+session.Cloud.Repo+" cloud cursor "+m.names.Get(session.SessionID)), query) {
			continue
		}
		visible = append(visible, session)
	}
	SortSessions(visible)
	return visible
}

// QuerySessions returns sessions filtered by explicit parameters without using
// the manager's internal filter/search state. Safe for concurrent API use.
// Empty search/filter means no filtering.
func (m *SessionManager) QuerySessions(search string, filter ActivityKind) []*model.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.filterSessionsLocked(search, filter)
}

// excludedByCWD reports whether cwd matches any of patterns, using
// case-sensitive substring containment (strings.Contains). An empty-string
// pattern never matches anything (otherwise it would match every CWD).
//
// This is the single implementation of exclude_cwd_substrings matching
// semantics: both the view-level filter (filterSessionsLocked) and the
// discovery-time pushdown predicate (excludeCWDPredicate, used by Reload)
// call it, so the two layers can never disagree about what's excluded.
func excludedByCWD(cwd string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(cwd, pattern) {
			return true
		}
	}
	return false
}

// excludeCWDPredicate returns a DiscoverMatching cwdMatch predicate — it
// returns true to KEEP a cwd — that keeps exactly the CWDs excludedByCWD
// would not exclude. Because it's built directly from excludedByCWD, it can
// only ever match what the view-level filter would keep: a dir-scoped
// provider can never be made to skip, via this predicate, a session the
// view filter would have shown.
func excludeCWDPredicate(patterns []string) func(cwd string) bool {
	return func(cwd string) bool {
		return !excludedByCWD(cwd, patterns)
	}
}

// filterSessionsLocked applies time window, activity, and search filters.
// Must be called with m.mu held (at least RLock).
func (m *SessionManager) filterSessionsLocked(search string, filter ActivityKind) []*model.Session {
	cutoff := time.Now().Add(-time.Duration(m.windowMinutes) * time.Minute)
	lowerQuery := strings.ToLower(search)
	var visible []*model.Session
	for _, s := range m.sessions {
		// CWD-based exclusion: skip sessions whose CWD contains any excluded
		// substring. Defense in depth: Reload already pushes this same
		// exclusion into discovery for dir-scoped providers (see
		// excludeCWDPredicate), but this check must stay here too since
		// DiscoverMatching leaves non-dir-scoped providers' results
		// unfiltered.
		if excludedByCWD(s.CWD, m.excludeCWDSubstrings) {
			continue
		}
		if s.IsSidechain || !s.LastActivity.After(cutoff) {
			continue
		}
		// Grok and Kimi both create a session directory the moment the CLI is
		// launched, before any message is sent. Hide such never-used sessions
		// from the list. They are still returned by the provider, so `prune`
		// and `compact` can reach very old empty sessions.
		if (s.Agent == "grok" || s.Agent == "kimi") && s.UserMessages == 0 {
			continue
		}
		if filter != "" && m.tracker.Get(s.SessionID) != filter {
			continue
		}
		if lowerQuery != "" {
			matchCWD := strings.Contains(strings.ToLower(s.CWD), lowerQuery)
			matchCustom := strings.Contains(strings.ToLower(m.names.Get(s.SessionID)), lowerQuery)
			matchAgent := strings.Contains(strings.ToLower(s.Name), lowerQuery)
			if !matchCWD && !matchCustom && !matchAgent {
				continue
			}
		}
		visible = append(visible, s)
	}
	return visible
}

// SessionDetail returns the full detail view for a session.
func (m *SessionManager) SessionDetail(id string) *SessionDetailView {
	m.UpdateCloud()
	m.mu.RLock()
	defer m.mu.RUnlock()
	if session := m.cloudSessions[id]; session != nil {
		return &SessionDetailView{Session: *session.Clone(), Activity: cloudActivity(session)}
	}
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

// SortSessions sorts sessions by last activity (most recent first),
// breaking ties by SessionID ascending for a fully deterministic order.
// The tiebreak matters for ReloadStreaming: batches from different
// provider members can merge in a nondeterministic arrival order, so two
// sessions that happen to share an exact LastActivity must still land in
// the same relative position no matter which batch each came from or when
// it arrived -- a plain LastActivity-only comparator would leave that pair
// in whatever order slices.SortFunc's internal algorithm happened to
// produce. Uses SortStableFunc (not SortFunc) as defense in depth on top of
// that: even a literal duplicate SessionID (which should never legitimately
// occur, but is exactly the shape a guard bug like the one this stable sort
// was added alongside would produce) sorts into a predictable, inspectable
// order rather than getting shuffled.
//
// Mirrors internal/sessions' bySessionRecency (same rule: LastActivity
// descending, SessionID ascending) -- checked and confirmed NOT importable
// here instead of duplicated: internal/sessions already imports
// internal/core, so importing back would be a cycle. Keep the two in sync
// by hand if the ordering rule ever changes.
func SortSessions(sessions []*model.Session) {
	slices.SortStableFunc(sessions, func(a, b *model.Session) int {
		if c := cmp.Compare(b.LastActivity.UnixNano(), a.LastActivity.UnixNano()); c != 0 {
			return c
		}
		return strings.Compare(a.SessionID, b.SessionID)
	})
}
