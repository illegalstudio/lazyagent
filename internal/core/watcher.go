package core

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ProjectWatcher watches ~/.claude/projects for JSONL changes using FSEvents.
// It debounces rapid writes so that a burst of JSONL lines triggers one reload.
type ProjectWatcher struct {
	fw     *fsnotify.Watcher
	Events <-chan struct{}
	done   chan struct{}
}

// NewProjectWatcher starts an FSEvents watcher on the given directories.
// Returns nil (no error) if none of the directories exist.
func NewProjectWatcher(dirs ...string) (*ProjectWatcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	added := false
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := fw.Add(dir); err != nil {
			continue // directory might not exist
		}
		added = true
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() {
				_ = fw.Add(filepath.Join(dir, e.Name()))
			}
		}
	}

	if !added {
		fw.Close()
		return nil, nil
	}

	ch := make(chan struct{}, 1)
	done := make(chan struct{})
	w := &ProjectWatcher{fw: fw, Events: ch, done: done}
	go w.run(ch)
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

func (w *ProjectWatcher) run(out chan<- struct{}) {
	defer w.fw.Close()

	var timer *time.Timer
	notify := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(500*time.Millisecond, func() {
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
