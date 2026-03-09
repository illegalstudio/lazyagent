package core

import (
	"testing"
	"time"

	"github.com/nahime0/lazyagent/internal/claude"
)

// fakeProvider is a test helper that returns pre-configured sessions.
type fakeProvider struct {
	sessions []*claude.Session
	err      error
	watcher  bool
	interval time.Duration
	dirs     []string
}

func (f fakeProvider) DiscoverSessions() ([]*claude.Session, error) {
	return f.sessions, f.err
}
func (f fakeProvider) UseWatcher() bool               { return f.watcher }
func (f fakeProvider) RefreshInterval() time.Duration { return f.interval }
func (f fakeProvider) WatchDirs() []string            { return f.dirs }

func TestMultiProvider_MergesSessions(t *testing.T) {
	p1 := fakeProvider{sessions: []*claude.Session{
		{SessionID: "s1", CWD: "/project1"},
	}}
	p2 := fakeProvider{sessions: []*claude.Session{
		{SessionID: "s2", CWD: "/project2"},
		{SessionID: "s3", CWD: "/project3"},
	}}

	mp := MultiProvider{Providers: []SessionProvider{p1, p2}}
	sessions, err := mp.DiscoverSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions, want 3", len(sessions))
	}
}

func TestMultiProvider_SkipsFailingProvider(t *testing.T) {
	failing := fakeProvider{err: errTest}
	working := fakeProvider{sessions: []*claude.Session{
		{SessionID: "s1"},
	}}

	mp := MultiProvider{Providers: []SessionProvider{failing, working}}
	sessions, err := mp.DiscoverSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
}

func TestMultiProvider_UseWatcher(t *testing.T) {
	noWatch := fakeProvider{watcher: false}
	watch := fakeProvider{watcher: true}

	mp1 := MultiProvider{Providers: []SessionProvider{noWatch}}
	if mp1.UseWatcher() {
		t.Error("expected false when no provider uses watcher")
	}

	mp2 := MultiProvider{Providers: []SessionProvider{noWatch, watch}}
	if !mp2.UseWatcher() {
		t.Error("expected true when at least one provider uses watcher")
	}
}

func TestMultiProvider_WatchDirs(t *testing.T) {
	p1 := fakeProvider{dirs: []string{"/dir1"}}
	p2 := fakeProvider{dirs: []string{"/dir2", "/dir3"}}

	mp := MultiProvider{Providers: []SessionProvider{p1, p2}}
	dirs := mp.WatchDirs()
	if len(dirs) != 3 {
		t.Fatalf("got %d dirs, want 3", len(dirs))
	}
}

func TestMultiProvider_RefreshInterval(t *testing.T) {
	p1 := fakeProvider{interval: 0}
	p2 := fakeProvider{interval: 30 * time.Second}
	p3 := fakeProvider{interval: 10 * time.Second}

	mp := MultiProvider{Providers: []SessionProvider{p1, p2, p3}}
	got := mp.RefreshInterval()
	if got != 10*time.Second {
		t.Errorf("RefreshInterval = %v, want 10s", got)
	}
}

var errTest = errorString("test error")

type errorString string

func (e errorString) Error() string { return string(e) }
