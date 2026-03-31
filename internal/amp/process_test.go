package amp

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestThreadsDir(t *testing.T) {
	dir := ThreadsDir()
	if dir == "" {
		t.Fatal("ThreadsDir() returned empty string")
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "amp", "threads")
	if dir != want {
		t.Fatalf("ThreadsDir() = %q, want %q", dir, want)
	}
}

func TestDiscoverSessions_FromSyntheticDir(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(sessionPath, []byte(`{"lastThreadId":"T-1"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `{
  "v": 1,
  "id": "T-1",
  "created": 1774696835566,
  "title": "Amp test thread",
  "messages": [
    {
      "role": "user",
      "messageId": 0,
      "content": [{"type":"text","text":"hello amp"}],
      "meta": {"sentAt": 1774696836000}
    },
    {
      "role": "assistant",
      "messageId": 1,
      "content": [
        {"type":"text","text":"reading files"},
        {"type":"tool_use","name":"Read","input":{"path":"/tmp/project/README.md"},"startTime":1774696837000,"finalTime":1774696838000,"complete":true}
      ],
      "state": {"type":"complete","stopReason":"tool_use"},
      "usage": {
        "model":"claude-opus-4-6",
        "inputTokens":10,
        "outputTokens":20,
        "cacheCreationInputTokens":30,
        "cacheReadInputTokens":40,
        "timestamp":"2026-03-28T11:20:38.000Z"
      }
    }
  ],
  "env": {
    "initial": {
      "trees": [{"displayName":"project","uri":"file:///tmp/project","repository":{"type":"git","url":"https://github.com/example/repo","ref":"refs/heads/main","sha":"abc"}}],
      "platform": {"client":"CLI","clientVersion":"1.2.3","clientType":"cli"},
      "tags": ["model:claude-opus-4-6"]
    }
  },
  "~debug": {
    "lastInferenceUsage": {
      "model":"claude-opus-4-6",
      "inputTokens":10,
      "outputTokens":20,
      "cacheCreationInputTokens":30,
      "cacheReadInputTokens":40,
      "timestamp":"2026-03-28T11:20:38.000Z"
    }
  }
}`
	path := filepath.Join(dir, "T-1.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	sessions, err := discoverSessionsFromDir(dir, sessionPath, model.NewSessionCache())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}

	got := sessions[0]
	if got.Agent != "amp" {
		t.Fatalf("Agent = %q, want amp", got.Agent)
	}
	if got.Name != "Amp test thread" {
		t.Fatalf("Name = %q, want Amp test thread", got.Name)
	}
	if got.CWD != "/tmp/project" {
		t.Fatalf("CWD = %q, want /tmp/project", got.CWD)
	}
	if got.GitBranch != "main" {
		t.Fatalf("GitBranch = %q, want main", got.GitBranch)
	}
	if got.Version != "1.2.3" {
		t.Fatalf("Version = %q, want 1.2.3", got.Version)
	}
	if got.Model != "claude-opus-4-6" {
		t.Fatalf("Model = %q, want claude-opus-4-6", got.Model)
	}
	if got.UserMessages != 1 || got.AssistantMessages != 1 || got.TotalMessages != 2 {
		t.Fatalf("message counts = (%d,%d,%d), want (1,1,2)", got.UserMessages, got.AssistantMessages, got.TotalMessages)
	}
	if got.Status != model.StatusExecutingTool {
		t.Fatalf("Status = %v, want executing tool", got.Status)
	}
	if got.CurrentTool != "Read" {
		t.Fatalf("CurrentTool = %q, want Read", got.CurrentTool)
	}
	if len(got.RecentTools) != 1 || got.RecentTools[0].Name != "Read" {
		t.Fatalf("RecentTools = %#v, want one Read tool", got.RecentTools)
	}
	if got.InputTokens != 10 || got.OutputTokens != 20 || got.CacheCreationTokens != 30 || got.CacheReadTokens != 40 {
		t.Fatalf("tokens = (%d,%d,%d,%d), want (10,20,30,40)", got.InputTokens, got.OutputTokens, got.CacheCreationTokens, got.CacheReadTokens)
	}
}

func TestDiscoverSessions_ParallelMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(sessionPath, []byte(`{"lastThreadId":"T-0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		content := fmt.Sprintf(`{
  "id": "T-%d",
  "created": 1774696835566,
  "title": "Thread %d",
  "messages": [
    {"role":"user","content":[{"type":"text","text":"hello %d"}],"meta":{"sentAt":1774696836000}},
    {"role":"assistant","content":[{"type":"text","text":"hi %d"}],"usage":{"model":"claude-sonnet-4-5","inputTokens":10,"outputTokens":20,"timestamp":"2026-03-28T11:20:38.000Z"}}
  ],
  "env": {"initial": {"trees":[{"uri":"file:///tmp/project%d"}], "platform":{"clientVersion":"1.0.0"}}}
}`, i, i, i, i, i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("T-%d.json", i)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sessions, err := discoverSessionsFromDir(dir, sessionPath, model.NewSessionCache())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 5 {
		t.Fatalf("got %d sessions, want 5", len(sessions))
	}

	ids := make(map[string]bool)
	for _, s := range sessions {
		ids[s.SessionID] = true
		if s.Agent != "amp" {
			t.Errorf("session %s: Agent = %q, want amp", s.SessionID, s.Agent)
		}
		if s.UserMessages != 1 || s.AssistantMessages != 1 {
			t.Errorf("session %s: messages = (%d,%d), want (1,1)", s.SessionID, s.UserMessages, s.AssistantMessages)
		}
	}
	if len(ids) != 5 {
		t.Errorf("got %d unique session IDs, want 5", len(ids))
	}
}

func TestParseThread_LastToolResultMeansProcessing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "T-2.json")
	content := `{
  "id": "T-2",
  "created": 1774696835566,
  "messages": [
    {"role":"assistant","content":[{"type":"tool_use","name":"edit_file","input":{"path":"/tmp/project/out.txt"},"startTime":1774696837000,"finalTime":1774696838000,"complete":true}],"state":{"type":"complete","stopReason":"tool_use"},"usage":{"model":"claude-sonnet-4-5","timestamp":"2026-03-28T11:20:38.000Z"}},
    {"role":"user","content":[{"type":"tool_result"}],"meta":{"sentAt":1774696839000}}
  ],
  "env": {"initial": {"trees":[{"uri":"file:///tmp/project"}], "platform":{"clientVersion":"1.0.0"}}}
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, _, err := ParseThread(path, "")
	if err != nil {
		t.Fatalf("ParseThread error: %v", err)
	}
	if got.Status != model.StatusProcessingResult {
		t.Fatalf("Status = %v, want processing", got.Status)
	}
	if got.LastFileWrite != "/tmp/project/out.txt" {
		t.Fatalf("LastFileWrite = %q, want /tmp/project/out.txt", got.LastFileWrite)
	}
}
