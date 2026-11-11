//go:build !notray

package tray

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/demo"
	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestRunProvider_DemoWins(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if _, ok := runProvider(true, "all").(demo.Provider); !ok {
		t.Errorf("demoMode=true must return demo.Provider (dead since 628cd84)")
	}
	if _, ok := runProvider(true, "claude").(demo.Provider); !ok {
		t.Errorf("demoMode wins over --agent")
	}
	if p := runProvider(false, "all"); p == nil {
		t.Errorf("live mode must return a provider")
	} else if _, ok := p.(demo.Provider); ok {
		t.Errorf("live mode must not return the demo provider")
	}
}

func TestCloudItemCapabilities(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	mgr := core.NewSessionManager(30, demo.Provider{})
	defer mgr.Close()
	svc := &SessionService{manager: mgr}
	item := svc.buildSessionItem(&model.Session{Agent: "cursor", SessionID: "cloud:cursor:one", Name: "Remote", Cloud: &model.CloudSession{ID: "one", URL: "https://cursor.com/agents/one", Repo: "https://github.com/org/project"}}, core.ActivityRunning, 30, 40)
	if item.Source != "cloud" || item.CloudURL != "https://cursor.com/agents/one" || item.RepoURL != "https://github.com/org/project" || item.CWD != "" || item.ResumeAvailable || item.YoloResumeAvailable {
		t.Fatalf("cloud capabilities: %+v", item)
	}
	if err := svc.OpenCloudSession("missing"); err == nil {
		t.Fatal("unknown cloud ID should fail")
	}
}

func TestCloudPollingNotifiesAfterOtherReaders(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	mgr := core.NewSessionManager(30, demo.Provider{})
	defer mgr.Close()
	svc := &SessionService{manager: mgr}
	previous := mgr.CloudStatus()
	mgr.SetCloudShowIdle(true)
	// A bound method can sample a changed snapshot before the watcher tick.
	mgr.VisibleSessions()
	mgr.SessionDetail("missing")
	if mgr.UpdateCloud() {
		t.Fatal("the manager should already have sampled the change")
	}
	if !svc.pollCloud(&previous) {
		t.Fatal("watcher must notify even when another reader applied the status first")
	}
	if svc.pollCloud(&previous) {
		t.Fatal("an unchanged snapshot must not emit repeatedly")
	}
}

func TestGUICursorCredentialsShareResolver(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "")
	mgr := core.NewSessionManager(30, demo.Provider{})
	defer mgr.Close()
	svc := &SessionService{manager: mgr}
	if svc.GetCursorCredentialStatus().Configured {
		t.Fatal("unexpected saved credential")
	}
	if err := svc.SetCursorAPIKey("fixture-key"); err != nil {
		t.Fatal(err)
	}
	status := svc.GetCursorCredentialStatus()
	key, source, err := core.ResolveProviderAPIKey("cursor")
	if err != nil || key != "fixture-key" || !status.Configured || status.Source != source {
		t.Fatalf("shared credential status: %+v, %v", status, err)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), "fixture-key") {
		t.Fatal("credential status exposed a key")
	}
	if mgr.CloudStatus().Configured {
		t.Fatal("demo must not start real cloud discovery")
	}
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "env-fixture")
	if err := svc.RemoveCursorAPIKey(); err != nil {
		t.Fatal(err)
	}
	if got := svc.GetCursorCredentialStatus(); !got.Configured || got.Source != "LAZYAGENT_CURSOR_API_KEY" {
		t.Fatalf("environment override after removal: %+v", got)
	}
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "")
	if svc.GetCursorCredentialStatus().Configured {
		t.Fatal("saved key was not removed")
	}
}

func TestBuildSessionItemResumeCapabilities(t *testing.T) {
	svc := &SessionService{manager: core.NewSessionManager(30, demo.Provider{})}
	item := svc.buildSessionItem(&model.Session{
		Agent: "cursor", SessionID: "abc", CWD: "/tmp/project", LastActivity: time.Now(),
	}, core.ActivityIdle, 30, 40)
	if !item.ResumeAvailable || !item.YoloResumeAvailable {
		t.Fatalf("cursor resume capabilities = normal:%v YOLO:%v", item.ResumeAvailable, item.YoloResumeAvailable)
	}

	piItem := svc.buildSessionItem(&model.Session{
		Agent: "pi", SessionID: "abc", CWD: "/tmp/project", LastActivity: time.Now(),
	}, core.ActivityIdle, 30, 40)
	if !piItem.ResumeAvailable || piItem.YoloResumeAvailable {
		t.Fatalf("pi resume capabilities = normal:%v YOLO:%v", piItem.ResumeAvailable, piItem.YoloResumeAvailable)
	}
}
