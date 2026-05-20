// Package grok discovers xAI Grok CLI sessions from ~/.grok/sessions.
//
// Grok stores one directory per session, exactly two levels deep:
//
//	~/.grok/sessions/<url-encoded-cwd>/<session-uuid>/
//
// Each session directory carries a summary.json (metadata) and a
// chat_history.jsonl (transcript). This package walks that tree, parses each
// session into a model.Session, and integrates with model.SessionCache so
// unchanged sessions are not re-parsed.
package grok

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/claude"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// GrokSessionsDir returns the path to ~/.grok/sessions.
func GrokSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grok", "sessions")
}

// DiscoverSessions scans ~/.grok/sessions for Grok session directories.
func DiscoverSessions(cache *model.SessionCache) ([]*model.Session, error) {
	return discoverSessionsFromDir(GrokSessionsDir(), cache)
}

// SessionDirs returns every Grok session directory on disk. Used by the
// search indexer and maintenance commands that need the raw directory list.
func SessionDirs() []string {
	return walkSessionDirs(GrokSessionsDir())
}

// SessionDiskBytes returns the total size in bytes of every file inside a
// Grok session directory. Best-effort: unreadable entries contribute zero.
func SessionDiskBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

type wtInfo struct {
	isWorktree bool
	mainRepo   string
}

type parseJob struct {
	sessionDir string
	cacheKey   string // chat_history.jsonl path — the cache invalidation key
	mtime      time.Time
}

type parseResult struct {
	session  *model.Session
	cacheKey string
	mtime    time.Time
}

// walkSessionDirs returns every session directory under sessionsDir: every
// depth-2 directory that contains a summary.json. Files at the root
// (session_search.sqlite) and at cwd level (prompt_history.jsonl) are skipped
// because only directories are descended into.
func walkSessionDirs(sessionsDir string) []string {
	if sessionsDir == "" {
		return nil
	}
	cwdEntries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, cwdEntry := range cwdEntries {
		if !cwdEntry.IsDir() {
			continue
		}
		cwdPath := filepath.Join(sessionsDir, cwdEntry.Name())
		sessEntries, err := os.ReadDir(cwdPath)
		if err != nil {
			continue
		}
		for _, sessEntry := range sessEntries {
			if !sessEntry.IsDir() {
				continue
			}
			sessionDir := filepath.Join(cwdPath, sessEntry.Name())
			if _, err := os.Stat(filepath.Join(sessionDir, "summary.json")); err != nil {
				continue // not a session directory
			}
			dirs = append(dirs, sessionDir)
		}
	}
	return dirs
}

// discoverSessionsFromDir scans a Grok sessions root and returns parsed
// sessions. A missing root is not an error (Grok not installed → nil, nil).
func discoverSessionsFromDir(sessionsDir string, cache *model.SessionCache) ([]*model.Session, error) {
	if sessionsDir == "" {
		return nil, nil
	}
	if _, err := os.Stat(sessionsDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Grok not installed — not an error.
		}
		return nil, fmt.Errorf("could not read grok sessions dir: %w", err)
	}

	wtCache := make(map[string]wtInfo)
	seen := make(map[string]struct{})
	var sessions []*model.Session
	var jobs []parseJob

	// Phase 1: classify each session as cache-hit or needs-parse.
	for _, sessionDir := range walkSessionDirs(sessionsDir) {
		cacheKey := filepath.Join(sessionDir, "chat_history.jsonl")
		seen[cacheKey] = struct{}{}
		cached, offset, mtime := cache.GetIncremental(cacheKey)
		// A full cache hit (file unchanged) reuses the parsed session. Any
		// change — including the offset>0 "file grew" case — triggers a full
		// re-parse, because Grok parsing reads summary.json + the whole
		// chat_history.jsonl rather than appending incrementally.
		// Trade-off: the cache key is chat_history.jsonl only, so a change
		// to summary.json or updates.jsonl alone (e.g. an asynchronously-
		// generated title) is not detected until the next chat_history.jsonl
		// write or a full --reindex. chat_history.jsonl is chosen because it
		// is always present and grows on every turn.
		if cached != nil && offset == 0 {
			sessions = append(sessions, cached)
			continue
		}
		jobs = append(jobs, parseJob{sessionDir: sessionDir, cacheKey: cacheKey, mtime: mtime})
	}

	if len(jobs) > 0 {
		// Phase 2: parse sessions in parallel.
		workers := runtime.GOMAXPROCS(0)
		if workers > len(jobs) {
			workers = len(jobs)
		}
		results := make([]parseResult, len(jobs))
		var wg sync.WaitGroup
		jobCh := make(chan int, len(jobs))
		for i := range jobs {
			jobCh <- i
		}
		close(jobCh)
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range jobCh {
					j := &jobs[idx]
					session, err := ParseGrokSession(j.sessionDir)
					if err != nil {
						continue // skip malformed / unsupported session
					}
					results[idx] = parseResult{session: session, cacheKey: j.cacheKey, mtime: j.mtime}
				}
			}()
		}
		wg.Wait()

		// Phase 3: enrich worktree info and update the cache (sequential).
		for _, r := range results {
			if r.session == nil {
				continue
			}
			if r.session.CWD != "" {
				if _, ok := wtCache[r.session.CWD]; !ok {
					isWT, mainRepo := claude.IsWorktree(r.session.CWD)
					wtCache[r.session.CWD] = wtInfo{isWorktree: isWT, mainRepo: mainRepo}
				}
				wt := wtCache[r.session.CWD]
				r.session.IsWorktree = wt.isWorktree
				r.session.MainRepo = wt.mainRepo
			}
			// size 0 forces a full re-parse on any future mtime change.
			cache.Put(r.cacheKey, r.mtime, 0, r.session)
			sessions = append(sessions, r.session)
		}
	}

	cache.Prune(seen)
	return sessions, nil
}
