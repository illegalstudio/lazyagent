package core

import (
	"testing"
	"time"

	"github.com/nahime0/lazyagent/internal/model"
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
