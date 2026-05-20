package prune

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestDeleteGrokSession(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "%2Ftmp%2Fp", "sess-1")
	if err := os.MkdirAll(filepath.Join(sessionDir, "terminal"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"summary.json", "chat_history.jsonl", "updates.jsonl", "terminal/cmd-1.log"} {
		if err := os.WriteFile(filepath.Join(sessionDir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := &model.Session{Agent: "grok", JSONLPath: sessionDir}
	if err := deleteGrokSession(s, root); err != nil {
		t.Fatalf("deleteGrokSession: %v", err)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Error("session directory should be gone")
	}
}

func TestDeleteGrokSession_RejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a different root
	s := &model.Session{Agent: "grok", JSONLPath: outside}
	if err := deleteGrokSession(s, root); err == nil {
		t.Error("expected error deleting a directory outside the grok root")
	}
}
