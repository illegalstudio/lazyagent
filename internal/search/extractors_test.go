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
{"type":"user","content":[{"type":"text","text":"find the parser bug"}]}
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
	// user + assistant + tool_result = 3 chunks; system is skipped.
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
}
