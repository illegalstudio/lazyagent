// Package kimi discovers Kimi Code CLI sessions from ~/.kimi-code/sessions.
//
// kimi-code stores sessions two levels deep:
//
//	~/.kimi-code/sessions/wd_<name>_<hash>/<session-id>/
//
// Each session directory carries its main agent's event stream at
// agents/main/wire.jsonl plus state.json metadata. Working directories are
// resolved through ~/.kimi-code/session_index.jsonl, which maps every session
// directory to its absolute workdir.
package kimi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/claude"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// ShareDir returns Kimi Code CLI's data root.
func ShareDir() string {
	if v := os.Getenv("KIMI_SHARE_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kimi-code")
}

// SessionsDir returns the path to Kimi Code CLI session directories.
func SessionsDir() string {
	root := ShareDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "sessions")
}

// SessionIndexPath returns the path to Kimi's session index, mapping every
// session directory to its absolute working directory.
func SessionIndexPath() string {
	root := ShareDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "session_index.jsonl")
}

// WireFile returns the main agent's event stream path for a session directory.
func WireFile(sessionDir string) string {
	return filepath.Join(sessionDir, "agents", "main", "wire.jsonl")
}

// wireFile returns the main agent's event stream path for a session directory.
func wireFile(sessionDir string) string {
	return WireFile(sessionDir)
}

// SessionDirForWire returns the session directory containing a wire stream path,
// inverting WireFile (<sessionDir>/agents/main/wire.jsonl).
func SessionDirForWire(wirePath string) string {
	return filepath.Dir(filepath.Dir(filepath.Dir(wirePath)))
}

// stateFile returns the metadata file path for a session directory.
func stateFile(sessionDir string) string {
	return filepath.Join(sessionDir, "state.json")
}

// WorkDirs returns a map from absolute session directory to its working
// directory, as recorded in session_index.jsonl.
func WorkDirs() map[string]string {
	return loadWorkDirIndex(SessionIndexPath())
}

// DiscoverSessions scans ~/.kimi-code/sessions for Kimi Code CLI sessions.
func DiscoverSessions(cache *model.SessionCache) ([]*model.Session, error) {
	return discoverSessionsFromDir(SessionsDir(), SessionIndexPath(), cache)
}

// SessionDirs returns every Kimi session directory on disk. Used by search and
// maintenance commands that need the raw directory list.
func SessionDirs() []string {
	return walkSessionDirs(SessionsDir())
}

// SessionDiskBytes returns the total size in bytes of every file inside a Kimi
// session directory. Best-effort: unreadable entries contribute zero.
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
	cacheKey   string
	workDir    string
	cached     *model.Session
	offset     int64
	mtime      time.Time
}

type parseResult struct {
	session   *model.Session
	cacheKey  string
	mtime     time.Time
	newOffset int64
}

func discoverSessionsFromDir(sessionsDir, indexPath string, cache *model.SessionCache) ([]*model.Session, error) {
	if sessionsDir == "" {
		return nil, nil
	}
	if _, err := os.Stat(sessionsDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read kimi sessions dir: %w", err)
	}

	workDirs := loadWorkDirIndex(indexPath)
	seen := make(map[string]struct{})
	var sessions []*model.Session
	var jobs []parseJob

	for _, sessionDir := range walkSessionDirs(sessionsDir) {
		cacheKey := wireFile(sessionDir)
		seen[cacheKey] = struct{}{}
		cached, offset, mtime := cache.GetIncremental(cacheKey)
		if cached != nil && offset == 0 {
			sessions = append(sessions, cached)
			continue
		}
		jobs = append(jobs, parseJob{
			sessionDir: sessionDir,
			cacheKey:   cacheKey,
			workDir:    workDirs[sessionDir],
			cached:     cached,
			offset:     offset,
			mtime:      mtime,
		})
	}

	if len(jobs) > 0 {
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
					session, newOffset, err := ParseSessionIncremental(j.sessionDir, j.workDir, j.offset, j.cached)
					if err != nil {
						continue
					}
					results[idx] = parseResult{
						session:   session,
						cacheKey:  j.cacheKey,
						mtime:     j.mtime,
						newOffset: newOffset,
					}
				}
			}()
		}
		wg.Wait()

		wtCache := make(map[string]wtInfo)
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
			cache.Put(r.cacheKey, r.mtime, r.newOffset, r.session)
			sessions = append(sessions, r.session)
		}
	}

	cache.Prune(seen)
	return sessions, nil
}

// walkSessionDirs returns every depth-2 session directory whose main agent has
// a wire.jsonl stream.
func walkSessionDirs(sessionsDir string) []string {
	if sessionsDir == "" {
		return nil
	}
	workEntries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, workEntry := range workEntries {
		if !workEntry.IsDir() {
			continue
		}
		workPath := filepath.Join(sessionsDir, workEntry.Name())
		sessionEntries, err := os.ReadDir(workPath)
		if err != nil {
			continue
		}
		for _, sessionEntry := range sessionEntries {
			if !sessionEntry.IsDir() {
				continue
			}
			sessionDir := filepath.Join(workPath, sessionEntry.Name())
			if _, err := os.Stat(wireFile(sessionDir)); err != nil {
				continue
			}
			dirs = append(dirs, sessionDir)
		}
	}
	return dirs
}

type sessionIndexEntry struct {
	SessionDir string `json:"sessionDir"`
	WorkDir    string `json:"workDir"`
}

// loadWorkDirIndex reads session_index.jsonl into a map keyed by absolute
// session directory.
func loadWorkDirIndex(path string) map[string]string {
	out := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var entry sessionIndexEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.SessionDir == "" || entry.WorkDir == "" {
			continue
		}
		out[filepath.Clean(entry.SessionDir)] = entry.WorkDir
	}
	return out
}
