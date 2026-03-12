package pi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestPiSessionsDir(t *testing.T) {
	dir := PiSessionsDir()
	if dir == "" {
		t.Fatal("PiSessionsDir() returned empty string")
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".pi", "agent", "sessions")
	if dir != want {
		t.Errorf("PiSessionsDir() = %q, want %q", dir, want)
	}
}

func TestDecodePiDirName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"--home-vincent--", "/home/vincent"},
		{"--home-vincent-src-home--", "/home/vincent/src/home"},
		{"--tmp--", "/tmp"},
	}
	for _, tt := range tests {
		got := decodePiDirName(tt.input)
		if got != tt.want {
			t.Errorf("decodePiDirName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDiscoverSessions_FromSyntheticDir(t *testing.T) {
	// Create a synthetic pi sessions directory structure
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "--home-user-project--")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a session file
	content := `{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/home/user/project"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":"Hello","timestamp":1741514401000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-09T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Hi"}],"provider":"anthropic","model":"claude-sonnet-4-5","usage":{"input":100,"output":50,"cacheRead":0,"cacheWrite":0,"totalTokens":150,"cost":{"input":0.0003,"output":0.00075,"cacheRead":0,"cacheWrite":0,"total":0.00105}},"stopReason":"stop","timestamp":1741514402000}}
`
	sessionFile := filepath.Join(projectDir, "2026-03-09T10-00-00-000Z_abc-123.jsonl")
	if err := os.WriteFile(sessionFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sessions, err := discoverSessionsFromDir(dir, model.NewSessionCache())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].CWD != "/home/user/project" {
		t.Errorf("CWD = %q, want /home/user/project", sessions[0].CWD)
	}
	if sessions[0].UserMessages != 1 {
		t.Errorf("UserMessages = %d, want 1", sessions[0].UserMessages)
	}
}

func TestDiscoverSessions_FallbackCWD(t *testing.T) {
	// Session file without CWD in header — should fall back to dir name decoding
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "--home-user-myproject--")
	os.MkdirAll(projectDir, 0755)

	content := `{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":"Hi","timestamp":1741514401000}}
`
	os.WriteFile(filepath.Join(projectDir, "session.jsonl"), []byte(content), 0644)

	sessions, err := discoverSessionsFromDir(dir, model.NewSessionCache())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].CWD != "/home/user/myproject" {
		t.Errorf("CWD = %q, want /home/user/myproject", sessions[0].CWD)
	}
}

func TestDiscoverSessions_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	sessions, err := discoverSessionsFromDir(dir, model.NewSessionCache())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("got %d sessions, want 0", len(sessions))
	}
}

func TestDiscoverSessions_NonexistentDir(t *testing.T) {
	sessions, err := discoverSessionsFromDir("/nonexistent/path/that/does/not/exist", model.NewSessionCache())
	if err != nil {
		t.Fatalf("should not error for nonexistent dir, got: %v", err)
	}
	if sessions != nil {
		t.Errorf("got %v, want nil", sessions)
	}
}
