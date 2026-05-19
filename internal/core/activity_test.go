package core

import (
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestToolActivity_ClaudeToolNames(t *testing.T) {
	tests := []struct {
		tool string
		want ActivityKind
	}{
		{"Read", ActivityReading},
		{"Write", ActivityWriting},
		{"Edit", ActivityWriting},
		{"NotebookEdit", ActivityWriting},
		{"Bash", ActivityRunning},
		{"Glob", ActivitySearching},
		{"Grep", ActivitySearching},
		{"WebFetch", ActivityBrowsing},
		{"WebSearch", ActivityBrowsing},
		{"Agent", ActivitySpawning},
		{"UnknownTool", ActivityRunning},
		{"", ActivityIdle},
	}
	for _, tt := range tests {
		got := ToolActivity(tt.tool)
		if got != tt.want {
			t.Errorf("ToolActivity(%q) = %q, want %q", tt.tool, got, tt.want)
		}
	}
}

func TestToolActivity_PiToolNames(t *testing.T) {
	tests := []struct {
		tool string
		want ActivityKind
	}{
		{"read", ActivityReading},
		{"write", ActivityWriting},
		{"edit", ActivityWriting},
		{"bash", ActivityRunning},
		{"process", ActivityRunning},
		{"find", ActivitySearching},
		{"lsp", ActivitySearching},
		{"web_search", ActivityBrowsing},
		{"subagent", ActivitySpawning},
	}
	for _, tt := range tests {
		got := ToolActivity(tt.tool)
		if got != tt.want {
			t.Errorf("ToolActivity(%q) = %q, want %q", tt.tool, got, tt.want)
		}
	}
}

func TestActivityTracker_EmitsTransitionOnChange(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(8)
	defer bus.Unsubscribe(ch)

	tr := NewActivityTracker()
	tr.SetEventBus(bus)

	now := time.Now()
	s := &model.Session{SessionID: "s1", Agent: "claude", CWD: "/p", LastActivity: now, Status: model.StatusThinking}
	tr.Update([]*model.Session{s}, now)

	// First Update: new session emits Unknown→Thinking.
	select {
	case ev := <-ch:
		if ev.SessionID != "s1" || ev.Agent != "claude" || ev.From != "" || ev.To != ActivityThinking || ev.ProjectPath != "/p" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event emitted")
	}

	// Same activity: no event.
	tr.Update([]*model.Session{s}, now)
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event on unchanged state: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}

	// Status flips to waiting (after grace).
	s.Status = model.StatusWaitingForUser
	tr.Update([]*model.Session{s}, now.Add(WaitingGrace+time.Second))
	select {
	case ev := <-ch:
		if ev.From != ActivityThinking || ev.To != ActivityWaiting {
			t.Fatalf("unexpected transition: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event on change")
	}
}

func TestActivityTracker_NilBusSafe(t *testing.T) {
	tr := NewActivityTracker()
	// No SetEventBus call. Must not panic.
	s := &model.Session{SessionID: "s1", Agent: "claude", LastActivity: time.Now(), Status: model.StatusThinking}
	tr.Update([]*model.Session{s}, time.Now())
}
