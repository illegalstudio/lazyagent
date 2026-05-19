package core

import (
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestReload_DoesNotAutoPopulateNames(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	provider := fakeProvider{
		sessions: []*model.Session{
			{SessionID: "s1", CWD: "/project1", Name: "My cool session", LastActivity: time.Now()},
			{SessionID: "s2", CWD: "/project2", LastActivity: time.Now()},
		},
	}

	mgr := NewSessionManager(30, provider)
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// Agent-provided names should NOT be persisted as custom names.
	// The UI reads s.Name directly as a display fallback.
	if got := mgr.SessionName("s1"); got != "" {
		t.Errorf("SessionName(s1) = %q, want empty (agent name should not be persisted)", got)
	}
	if got := mgr.SessionName("s2"); got != "" {
		t.Errorf("SessionName(s2) = %q, want empty", got)
	}
}

func TestReload_PreservesUserSetNames(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	provider := fakeProvider{
		sessions: []*model.Session{
			{SessionID: "s1", CWD: "/project1", Name: "From agent", LastActivity: time.Now()},
		},
	}

	mgr := NewSessionManager(30, provider)

	// User sets a custom name
	_ = mgr.SetSessionName("s1", "My custom name")

	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	if got := mgr.SessionName("s1"); got != "My custom name" {
		t.Errorf("SessionName(s1) = %q, want 'My custom name'", got)
	}
}

func TestFilterSessionsLocked_ExcludesCWDSubstrings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	now := time.Now()
	provider := fakeProvider{
		sessions: []*model.Session{
			{SessionID: "normal", CWD: "/home/user/project", LastActivity: now},
			{SessionID: "excluded", CWD: "/home/user/.claude-mem/observer-sessions/abc", LastActivity: now},
			{SessionID: "also-normal", CWD: "/home/user/other-project", LastActivity: now},
		},
	}

	mgr := NewSessionManager(60, provider)
	mgr.SetExcludeCWDSubstrings([]string{".claude-mem/observer-sessions"})
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// Sessions() returns all raw sessions (unfiltered by CWD exclusion).
	raw := mgr.Sessions()
	if len(raw) != 3 {
		t.Errorf("Sessions() returned %d sessions, want 3", len(raw))
	}

	// VisibleSessions should exclude the matching session.
	visible := mgr.VisibleSessions()
	for _, s := range visible {
		if s.SessionID == "excluded" {
			t.Error("VisibleSessions() should not contain the excluded session")
		}
	}
	if len(visible) != 2 {
		t.Errorf("VisibleSessions() returned %d sessions, want 2", len(visible))
	}

	// QuerySessions should also exclude the matching session.
	queried := mgr.QuerySessions("", "")
	for _, s := range queried {
		if s.SessionID == "excluded" {
			t.Error("QuerySessions() should not contain the excluded session")
		}
	}
	if len(queried) != 2 {
		t.Errorf("QuerySessions() returned %d sessions, want 2", len(queried))
	}
}

func TestFilterSessionsLocked_NoExcludePatterns(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	now := time.Now()
	provider := fakeProvider{
		sessions: []*model.Session{
			{SessionID: "s1", CWD: "/home/user/project", LastActivity: now},
			{SessionID: "s2", CWD: "/home/user/.claude-mem/observer-sessions/abc", LastActivity: now},
		},
	}

	mgr := NewSessionManager(60, provider)
	// No SetExcludeCWDSubstrings call — empty/nil patterns.
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	visible := mgr.VisibleSessions()
	if len(visible) != 2 {
		t.Errorf("VisibleSessions() returned %d sessions, want 2 (no exclusion)", len(visible))
	}
}

func TestFilterSessionsLocked_MultipleExcludePatterns(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	now := time.Now()
	provider := fakeProvider{
		sessions: []*model.Session{
			{SessionID: "normal", CWD: "/home/user/project", LastActivity: now},
			{SessionID: "obs", CWD: "/home/user/.claude-mem/observer-sessions/x", LastActivity: now},
			{SessionID: "tmp", CWD: "/tmp/scratch/build", LastActivity: now},
		},
	}

	mgr := NewSessionManager(60, provider)
	mgr.SetExcludeCWDSubstrings([]string{".claude-mem/observer-sessions", "/tmp/scratch"})
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	visible := mgr.VisibleSessions()
	if len(visible) != 1 {
		t.Errorf("VisibleSessions() returned %d sessions, want 1", len(visible))
	}
	if len(visible) > 0 && visible[0].SessionID != "normal" {
		t.Errorf("expected 'normal' session, got %q", visible[0].SessionID)
	}
}

func TestSessionManager_SetEventBus_PropagatesToTracker(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	bus := NewEventBus()
	ch := bus.Subscribe(4)
	defer bus.Unsubscribe(ch)

	now := time.Now()
	p := &mutableStubProvider{sessions: []*model.Session{{SessionID: "s1", Agent: "claude", LastActivity: now, Status: model.StatusThinking}}}
	m := NewSessionManager(60, p)
	m.SetEventBus(bus)

	// First Reload is an observation: no event expected.
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload 1: %v", err)
	}
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event on first observation: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}

	// Mutate state and reload: now we expect a transition event.
	p.sessions[0].Status = model.StatusExecutingTool
	p.sessions[0].CurrentTool = "Bash"
	p.sessions[0].RecentTools = []model.ToolCall{{Name: "Bash", Timestamp: time.Now()}}
	p.sessions[0].LastActivity = time.Now()
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload 2: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.From != ActivityThinking || ev.To != ActivityRunning {
			t.Fatalf("unexpected transition: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event after real transition")
	}
}

type mutableStubProvider struct{ sessions []*model.Session }

func (p *mutableStubProvider) DiscoverSessions() ([]*model.Session, error) { return p.sessions, nil }
func (p *mutableStubProvider) UseWatcher() bool                            { return false }
func (p *mutableStubProvider) RefreshInterval() time.Duration              { return 0 }
func (p *mutableStubProvider) WatchDirs() []string                         { return nil }
