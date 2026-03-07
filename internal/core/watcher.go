package core

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/nahime0/lazyagent/internal/claude"
)

// ProjectWatcher watches ~/.claude/projects for JSONL changes using FSEvents.
// It debounces rapid writes so that a burst of JSONL lines triggers one reload.
type ProjectWatcher struct {
	fw     *fsnotify.Watcher
	Events <-chan struct{}
	done   chan struct{}
}

// NewProjectWatcher starts an FSEvents watcher on ~/.claude/projects.
// Returns nil (no error) if the directory doesn't exist yet.
func NewProjectWatcher() (*ProjectWatcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	projectsDir := claude.ClaudeProjectsDir()
	if projectsDir == "" {
		fw.Close()
		return nil, nil
	}

	if err := fw.Add(projectsDir); err != nil {
		fw.Close()
		return nil, err
	}

	entries, _ := os.ReadDir(projectsDir)
	for _, e := range entries {
		if e.IsDir() {
			_ = fw.Add(filepath.Join(projectsDir, e.Name()))
		}
	}

	ch := make(chan struct{}, 1)
	done := make(chan struct{})
	w := &ProjectWatcher{fw: fw, Events: ch, done: done}
	go w.run(projectsDir, ch)
	return w, nil
}

// Close signals the watcher goroutine to stop and releases resources.
func (w *ProjectWatcher) Close() {
	select {
	case <-w.done:
	default:
		close(w.done)
	}
}

func (w *ProjectWatcher) run(projectsDir string, out chan<- struct{}) {
	defer w.fw.Close()

	var timer *time.Timer
	notify := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(200*time.Millisecond, func() {
			select {
			case out <- struct{}{}:
			default:
			}
		})
	}

	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.fw.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = w.fw.Add(event.Name)
				}
			}
			if strings.HasSuffix(event.Name, ".jsonl") {
				notify()
			}
		case _, ok := <-w.fw.Errors:
			if !ok {
				return
			}
		}
	}
}
