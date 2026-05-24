// Package kimi discovers Kimi Code CLI sessions from ~/.kimi/sessions.
//
// Kimi stores sessions two levels deep:
//
//	~/.kimi/sessions/<md5-workdir>/<session-uuid>/
//
// Each session directory carries a wire.jsonl event stream, context.jsonl
// transcript, and state.json metadata. The workdir hash is resolved through
// ~/.kimi/kimi.json when available.
package kimi

import (
	"crypto/md5"
	"encoding/hex"
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
	return filepath.Join(home, ".kimi")
}

// SessionsDir returns the path to Kimi Code CLI session directories.
func SessionsDir() string {
	root := ShareDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "sessions")
}

// MetadataPath returns the path to Kimi's workdir metadata file.
func MetadataPath() string {
	root := ShareDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "kimi.json")
}

// CredentialsPath returns the Kimi Code OAuth credential file path.
func CredentialsPath() string {
	root := ShareDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "credentials", "kimi-code.json")
}

// DiscoverSessions scans ~/.kimi/sessions for Kimi Code CLI session directories.
func DiscoverSessions(cache *model.SessionCache) ([]*model.Session, error) {
	return discoverSessionsFromDir(SessionsDir(), MetadataPath(), cache)
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

func discoverSessionsFromDir(sessionsDir, metadataPath string, cache *model.SessionCache) ([]*model.Session, error) {
	if sessionsDir == "" {
		return nil, nil
	}
	if _, err := os.Stat(sessionsDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read kimi sessions dir: %w", err)
	}

	workDirs := loadWorkDirs(metadataPath)
	seen := make(map[string]struct{})
	var sessions []*model.Session
	var jobs []parseJob

	for _, sessionDir := range walkSessionDirs(sessionsDir) {
		cacheKey := filepath.Join(sessionDir, "wire.jsonl")
		seen[cacheKey] = struct{}{}
		cached, offset, mtime := cache.GetIncremental(cacheKey)
		if cached != nil && offset == 0 {
			sessions = append(sessions, cached)
			continue
		}
		workHash := filepath.Base(filepath.Dir(sessionDir))
		jobs = append(jobs, parseJob{
			sessionDir: sessionDir,
			cacheKey:   cacheKey,
			workDir:    workDirs[workHash],
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

// walkSessionDirs returns every depth-2 session directory containing wire.jsonl.
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
			if _, err := os.Stat(filepath.Join(sessionDir, "wire.jsonl")); err != nil {
				continue
			}
			dirs = append(dirs, sessionDir)
		}
	}
	return dirs
}

type metadataFile struct {
	WorkDirs []workDirMeta `json:"work_dirs"`
}

type workDirMeta struct {
	Path string `json:"path"`
	Kaos string `json:"kaos"`
}

func loadWorkDirs(path string) map[string]string {
	out := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var meta metadataFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return out
	}
	for _, wd := range meta.WorkDirs {
		if wd.Path == "" || wd.Kaos != "local" {
			continue
		}
		out[md5Hex(wd.Path)] = wd.Path
	}
	return out
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
