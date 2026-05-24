package prune

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestTotalBytesUsesKimiDirectoryContents(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "sess-1")
	if err := os.MkdirAll(filepath.Join(sessionDir, "subagents", "sub-1"), 0o755); err != nil {
		t.Fatal(err)
	}

	files := map[string]int{
		"wire.jsonl":                 128,
		"context.jsonl":              256,
		"subagents/sub-1/output":     512,
		"subagents/sub-1/prompt.txt": 64,
	}
	var want int64
	for name, size := range files {
		want += int64(size)
		path := filepath.Join(sessionDir, name)
		if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := totalBytes([]Candidate{{
		Session: &model.Session{
			Agent:     "kimi",
			JSONLPath: sessionDir,
		},
	}})
	if got != want {
		t.Fatalf("totalBytes() = %d, want %d", got, want)
	}
}
