package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewProjectWatcher_MultipleDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	w, err := NewProjectWatcher(dir1, dir2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w == nil {
		t.Fatal("watcher should not be nil")
	}
	defer w.Close()

	// Write a .jsonl file to dir1 and expect an event
	time.Sleep(50 * time.Millisecond) // let watcher start
	os.WriteFile(filepath.Join(dir1, "test.jsonl"), []byte("{}"), 0644)

	select {
	case <-w.Events:
		// good
	case <-time.After(2 * time.Second):
		t.Error("expected event from dir1 write")
	}
}

func TestNewProjectWatcher_NoDirs(t *testing.T) {
	w, err := NewProjectWatcher()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != nil {
		t.Error("expected nil watcher when no dirs provided")
	}
}

func TestNewProjectWatcher_NonexistentDirsSkipped(t *testing.T) {
	existing := t.TempDir()
	w, err := NewProjectWatcher("/nonexistent/path", existing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w == nil {
		t.Fatal("watcher should not be nil when at least one dir exists")
	}
	defer w.Close()
}

func TestNewProjectWatcher_AllNonexistent(t *testing.T) {
	w, err := NewProjectWatcher("/nonexistent/a", "/nonexistent/b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != nil {
		t.Error("expected nil watcher when all dirs nonexistent")
	}
}
