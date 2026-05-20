package search

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractGrok(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "%2Ftmp%2Fp", "sess-1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := `{"info":{"id":"sess-1","cwd":"/tmp/p"},"generated_title":"Parser work"}`
	chat := `{"type":"system","content":"ignore me"}
{"type":"user","content":[{"type":"text","text":"<system-reminder>\nskills available\n</system-reminder>"}]}
{"type":"user","content":[{"type":"text","text":"<user_query>find the parser bug</user_query>"}]}
{"type":"assistant","content":"looking into the parser now"}
{"type":"tool_result","content":"grep matched parser.go"}
`
	if err := os.WriteFile(filepath.Join(sessionDir, "summary.json"), []byte(summary), 0o644); err != nil {
		t.Fatal(err)
	}
	chatPath := filepath.Join(sessionDir, "chat_history.jsonl")
	if err := os.WriteFile(chatPath, []byte(chat), 0o644); err != nil {
		t.Fatal(err)
	}

	src, ok := fileSource("grok", "sess-1", chatPath)
	if !ok {
		t.Fatal("fileSource failed")
	}
	chunks, err := extractGrok(src)
	if err != nil {
		t.Fatalf("extractGrok: %v", err)
	}
	// user + assistant + tool_result = 3 chunks; system and <system-reminder> are skipped.
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	for _, c := range chunks {
		if c.SessionID != "sess-1" {
			t.Errorf("SessionID = %q", c.SessionID)
		}
		if c.CWD != "/tmp/p" {
			t.Errorf("CWD = %q", c.CWD)
		}
		if c.Name != "Parser work" {
			t.Errorf("Name = %q", c.Name)
		}
	}
	gotRoles := []string{chunks[0].Role, chunks[1].Role, chunks[2].Role}
	wantRoles := []string{"user", "assistant", "tool_result"}
	for i := range wantRoles {
		if gotRoles[i] != wantRoles[i] {
			t.Errorf("chunk %d Role = %q, want %q", i, gotRoles[i], wantRoles[i])
		}
	}
	// The user chunk must have the unwrapped text — no <user_query> tags, no <system-reminder>.
	if chunks[0].Text != "find the parser bug" {
		t.Errorf("user chunk Text = %q, want %q", chunks[0].Text, "find the parser bug")
	}
}

func TestExtractGrok_MissingSummary(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "%2Ftmp%2Fp", "fallback-id")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No summary.json on purpose.
	chat := `{"type":"user","content":[{"type":"text","text":"hello grok"}]}` + "\n"
	chatPath := filepath.Join(sessionDir, "chat_history.jsonl")
	if err := os.WriteFile(chatPath, []byte(chat), 0o644); err != nil {
		t.Fatal(err)
	}

	src, ok := fileSource("grok", "fallback-id", chatPath)
	if !ok {
		t.Fatal("fileSource failed")
	}
	chunks, err := extractGrok(src)
	if err != nil {
		t.Fatalf("extractGrok must not fail when summary.json is absent: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	// With no summary.json, the session ID falls back to src.ID.
	if chunks[0].SessionID != "fallback-id" {
		t.Errorf("SessionID = %q, want fallback-id", chunks[0].SessionID)
	}
}
