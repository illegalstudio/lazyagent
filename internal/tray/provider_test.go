//go:build !notray

package tray

import (
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
