package grok

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestGrokSessionsDir(t *testing.T) {
	dir := GrokSessionsDir()
	if dir == "" {
		t.Fatal("GrokSessionsDir() returned empty string")
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".grok", "sessions")
	if dir != want {
		t.Errorf("GrokSessionsDir() = %q, want %q", dir, want)
	}
}

func TestDiscoverSessions_MissingDir(t *testing.T) {
	sessions, err := discoverSessionsFromDir("/nonexistent/grok/sessions", model.NewSessionCache())
	if err != nil {
		t.Fatalf("missing dir must not error: %v", err)
	}
	if sessions != nil {
		t.Errorf("got %v, want nil", sessions)
	}
}

func TestDiscoverSessions_PrimaryAndSubagent(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "%2FUsers%2Falice%2Fproject", "019e0000-0000-7000-8000-000000000001", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat, "signals.json": primarySignals,
	})
	subSummary := `{"info":{"id":"sub","cwd":"/tmp/wt"},"chat_format_version":1,
		"updated_at":"2026-05-17T11:00:00Z","session_kind":"subagent"}`
	writeSession(t, root, "%2Ftmp%2Fwt", "sub", map[string]string{
		"summary.json": subSummary, "chat_history.jsonl": "",
	})

	sessions, err := discoverSessionsFromDir(root, model.NewSessionCache())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	var primary, sub *model.Session
	for _, s := range sessions {
		if s.IsSidechain {
			sub = s
		} else {
			primary = s
		}
	}
	if primary == nil || sub == nil {
		t.Fatal("expected one primary and one subagent session")
	}
	if primary.Agent != "grok" {
		t.Errorf("Agent = %q", primary.Agent)
	}
}

func TestDiscoverSessions_SkipsNonSessionEntries(t *testing.T) {
	root := t.TempDir()
	cwdDir := filepath.Join(root, "%2Ftmp%2Fp")
	if err := os.MkdirAll(cwdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A cwd-level file and a root-level file must both be ignored.
	if err := os.WriteFile(filepath.Join(cwdDir, "prompt_history.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "session_search.sqlite"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSession(t, root, "%2Ftmp%2Fp", "real", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat,
	})

	sessions, err := discoverSessionsFromDir(root, model.NewSessionCache())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 (sqlite + prompt_history must be skipped)", len(sessions))
	}
}

func TestDiscoverSessions_MalformedSummarySkipped(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "%2Ftmp%2Fp", "bad", map[string]string{
		"summary.json": "{not json", "chat_history.jsonl": "",
	})
	writeSession(t, root, "%2Ftmp%2Fp", "good", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat,
	})
	sessions, err := discoverSessionsFromDir(root, model.NewSessionCache())
	if err != nil {
		t.Fatalf("one bad session must not abort the scan: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
}

func TestDiscoverSessions_CacheHit(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "%2Ftmp%2Fp", "c", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat,
	})
	cache := model.NewSessionCache()
	first, err := discoverSessionsFromDir(root, cache)
	if err != nil || len(first) != 1 {
		t.Fatalf("first discover: %v, n=%d", err, len(first))
	}
	second, err := discoverSessionsFromDir(root, cache)
	if err != nil || len(second) != 1 {
		t.Fatalf("second discover: %v, n=%d", err, len(second))
	}
	if first[0] != second[0] {
		t.Error("unchanged session should be served from cache (same pointer)")
	}
}

func TestSessionDirsAndDiskBytes(t *testing.T) {
	root := t.TempDir()
	dir := writeSession(t, root, "%2Ftmp%2Fp", "s", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat,
		"terminal/cmd-1.log": "some output",
	})
	dirs := walkSessionDirs(root)
	if len(dirs) != 1 || dirs[0] != dir {
		t.Fatalf("walkSessionDirs = %v, want [%s]", dirs, dir)
	}
	if got := SessionDiskBytes(dir); got <= 0 {
		t.Errorf("SessionDiskBytes = %d, want > 0", got)
	}
}
