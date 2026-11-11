package core

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/cloud"
	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestCloudMonitorNonblockingDiscoveryAndFilters(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	started, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Write([]byte(`{"items":[{"id":"same","name":"Active cloud","status":"ACTIVE","updatedAt":"2020-01-01T00:00:00Z","url":"https://evil.example/agents/same","repos":[{"url":"https://github.com/org/remote"}]},{"id":"old","name":"Old run","status":"FINISHED","updatedAt":"2020-01-01T00:00:00Z","repos":[]}]}`))
	}))
	defer upstream.Close()
	mgr := NewSessionManager(10, fakeProvider{sessions: []*model.Session{{SessionID: "same", Name: "Local", LastActivity: time.Now()}}})
	defer mgr.Close()
	if err := mgr.Reload(); err != nil {
		t.Fatal(err)
	}
	mgr.setCloud(cloud.NewCursor(upstream.URL, "test-key"), nil)
	<-started
	local := mgr.VisibleSessions()
	if len(local) != 1 || local[0].SessionID != "same" || !mgr.CloudStatus().Loading {
		t.Fatalf("local sessions must remain available during refresh: %v, %+v", local, mgr.CloudStatus())
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for mgr.CloudStatus().Count != 2 && time.Now().Before(deadline) {
		mgr.UpdateCloud()
		time.Sleep(time.Millisecond)
	}
	if len(mgr.VisibleSessions()) != 2 {
		t.Fatalf("want local and old active cloud, got %+v", mgr.VisibleSessions())
	}
	id := "cloud:cursor:same"
	if mgr.ActivityFor(id) != ActivityRunning {
		t.Fatal("old updated_at must not turn ACTIVE cloud idle")
	}
	detail := mgr.SessionDetail(id)
	if detail == nil || detail.Cloud == nil || detail.CWD != "" || detail.Cloud.Repo != "https://github.com/org/remote" || detail.Cloud.URL != "https://cursor.com/agents/same" {
		t.Fatalf("cloud detail must preserve metadata with safe URL and no local cwd: %+v", detail)
	}
	detail.Cloud.Repo = "mutated"
	if mgr.SessionDetail(id).Cloud.Repo == "mutated" {
		t.Fatal("detail must clone cloud metadata")
	}
	if len(mgr.Sessions()) != 1 || len(mgr.QuerySessions("", "")) != 1 {
		t.Fatal("local maintenance/API query must not include cloud agents")
	}
	mgr.SetActivityFilter(ActivityIdle)
	for _, s := range mgr.VisibleSessions() {
		if s.Cloud != nil {
			t.Fatal("idle cloud must stay hidden until explicitly enabled")
		}
	}
	mgr.SetCloudShowIdle(true)
	visible := mgr.VisibleSessions()
	found := false
	for _, s := range visible {
		if s.SessionID == "cloud:cursor:old" {
			found = true
		}
	}
	if !found {
		t.Fatal("idle cloud toggle must include history outside the local time window")
	}
	mgr.SetActivityFilter("")
	mgr.SetSearchQuery("org/remote")
	if got := mgr.VisibleSessions(); len(got) != 1 || got[0].SessionID != id {
		t.Fatalf("repository search: %+v", got)
	}
	mgr.setCloud(nil, errors.New("invalid credentials file"))
	if mgr.CloudStatus().Error == "" || mgr.SessionDetail(id) != nil {
		t.Fatal("reconfiguration must remove old cloud sessions and report errors")
	}
}

func TestCloudMonitorProviderSelection(t *testing.T) {
	for _, tc := range []struct {
		provider SessionProvider
		want     bool
	}{
		{&CursorProvider{}, true},
		{MultiProvider{Providers: []SessionProvider{fakeProvider{}, &CursorProvider{}}}, true},
		{&MultiProvider{Providers: []SessionProvider{&CursorProvider{}}}, true},
		{fakeProvider{}, false},
		{MultiProvider{Providers: []SessionProvider{fakeProvider{}}}, false},
	} {
		if got := providerHasCursor(tc.provider); got != tc.want {
			t.Errorf("%T: %v, want %v", tc.provider, got, tc.want)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "test-key")
	mgr := NewSessionManager(30, fakeProvider{})
	defer mgr.Close()
	mgr.ConfigureCloud()
	if mgr.CloudStatus().Configured {
		t.Fatal("non-Cursor providers must not enable cloud discovery")
	}
}

func TestCloudDisplayAndAuthoritativeActivity(t *testing.T) {
	if got := CloudDisplayText("\x1b[31mRemote\x1b[0m\nrun\x00"); got != "Remote run" {
		t.Fatalf("unsafe display text: %q", got)
	}
	for _, tc := range []struct {
		status string
		want   ActivityKind
	}{{cloud.Working, ActivityRunning}, {cloud.NeedsYou, ActivityWaiting}, {cloud.Idle, ActivityIdle}} {
		s := &model.Session{Cloud: &model.CloudSession{Status: tc.status}, LastActivity: time.Now().Add(-24 * time.Hour), CurrentTool: "Bash"}
		if got := ResolveActivity(s, time.Now()); got != tc.want {
			t.Errorf("%s: %s", tc.status, got)
		}
	}
	if strings.Contains(CloudDisplayText("\x1b]0;title\aName"), "title") {
		t.Fatal("OSC escape leaked into terminal")
	}
}
