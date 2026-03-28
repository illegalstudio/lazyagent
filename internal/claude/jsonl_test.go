package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseJSONL_BridgeStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	data := `{"type":"system","subtype":"bridge_status","url":"https://claude.ai/code/session_ABC123","sessionId":"abc","version":"2.1.86","timestamp":"2026-01-15T10:00:00Z"}
{"type":"user","timestamp":"2026-01-15T10:00:01Z","message":{"role":"user","content":"hello"},"cwd":"/proj","version":"1.0","gitBranch":"main"}
{"type":"assistant","timestamp":"2026-01-15T10:00:05Z","message":{"role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"Hi!"}],"usage":{"input_tokens":10,"output_tokens":5}}}
`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	sess, _, err := ParseJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	if sess.RemoteURL != "https://claude.ai/code/session_ABC123" {
		t.Errorf("RemoteURL = %q, want %q", sess.RemoteURL, "https://claude.ai/code/session_ABC123")
	}
}

func TestParseJSONL_NoBridgeStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	data := `{"type":"user","timestamp":"2026-01-15T10:00:01Z","message":{"role":"user","content":"hello"},"cwd":"/proj","version":"1.0","gitBranch":"main"}
{"type":"assistant","timestamp":"2026-01-15T10:00:05Z","message":{"role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"Hi!"}],"usage":{"input_tokens":10,"output_tokens":5}}}
`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	sess, _, err := ParseJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	if sess.RemoteURL != "" {
		t.Errorf("RemoteURL = %q, want empty", sess.RemoteURL)
	}
}
