package core

import (
	"testing"
	"time"

	"github.com/nahime0/lazyagent/internal/claude"
)

func TestReload_AutoPopulatesNamesFromSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	provider := fakeProvider{
		sessions: []*claude.Session{
			{SessionID: "s1", CWD: "/project1", Name: "My cool session", LastActivity: time.Now()},
			{SessionID: "s2", CWD: "/project2", LastActivity: time.Now()},
		},
	}

	mgr := NewSessionManager(30, provider)
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// s1 should have its name auto-populated
	if got := mgr.SessionName("s1"); got != "My cool session" {
		t.Errorf("SessionName(s1) = %q, want 'My cool session'", got)
	}

	// s2 should have no name
	if got := mgr.SessionName("s2"); got != "" {
		t.Errorf("SessionName(s2) = %q, want empty", got)
	}
}

func TestReload_DoesNotOverwriteUserSetNames(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	provider := fakeProvider{
		sessions: []*claude.Session{
			{SessionID: "s1", CWD: "/project1", Name: "From pi", LastActivity: time.Now()},
		},
	}

	mgr := NewSessionManager(30, provider)

	// User sets a custom name first
	_ = mgr.SetSessionName("s1", "My custom name")

	// Reload — should NOT overwrite user-set name
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	if got := mgr.SessionName("s1"); got != "My custom name" {
		t.Errorf("SessionName(s1) = %q, want 'My custom name' (should not be overwritten)", got)
	}
}
