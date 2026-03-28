package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/claude"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// PiSessionsDir returns the path to ~/.pi/agent/sessions.
func PiSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "sessions")
}

// DiscoverSessions scans ~/.pi/agent/sessions for JSONL session files.
func DiscoverSessions(cache *model.SessionCache) ([]*model.Session, error) {
	return discoverSessionsFromDir(PiSessionsDir(), cache)
}

type wtInfo struct {
	isWorktree bool
	mainRepo   string
}

type parseJob struct {
	jsonlFile string
	dirName   string
	cached    *model.Session
	offset    int64
	mtime     time.Time
}

type parseResult struct {
	session   *model.Session
	jsonlFile string
	mtime     time.Time
	newOffset int64
}

// discoverSessionsFromDir scans a directory for pi session JSONL files.
func discoverSessionsFromDir(sessionsDir string, cache *model.SessionCache) ([]*model.Session, error) {
	if sessionsDir == "" {
		return nil, fmt.Errorf("could not find home directory")
	}

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // pi not installed, not an error
		}
		return nil, fmt.Errorf("could not read pi sessions dir: %w", err)
	}

	wtCache := make(map[string]wtInfo)
	seen := make(map[string]struct{})
	var sessions []*model.Session

	// Phase 1: collect all JSONL files and classify as cache-hit or needs-parse.
	var jobs []parseJob
	for _, projectEntry := range entries {
		if !projectEntry.IsDir() {
			continue
		}
		projectPath := filepath.Join(sessionsDir, projectEntry.Name())
		jsonlFiles, err := filepath.Glob(filepath.Join(projectPath, "*.jsonl"))
		if err != nil || len(jsonlFiles) == 0 {
			continue
		}

		for _, jsonlFile := range jsonlFiles {
			seen[jsonlFile] = struct{}{}
			cached, offset, mtime := cache.GetIncremental(jsonlFile)

			if cached != nil && offset == 0 {
				sessions = append(sessions, cached)
				continue
			}
			jobs = append(jobs, parseJob{
				jsonlFile: jsonlFile,
				dirName:   projectEntry.Name(),
				cached:    cached,
				offset:    offset,
				mtime:     mtime,
			})
		}
	}

	if len(jobs) > 0 {
		// Phase 2: parse files in parallel.
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
					var session *model.Session
					var newOffset int64

					if j.cached != nil && j.offset > 0 {
						s, off, err := ParsePiJSONLIncremental(j.jsonlFile, j.offset, j.cached)
						if err != nil {
							continue
						}
						session = s
						newOffset = off
					} else {
						s, size, err := ParsePiJSONL(j.jsonlFile)
						if err != nil {
							continue
						}
						session = s
						newOffset = size
					}

					if session.CWD == "" {
						session.CWD = decodePiDirName(j.dirName)
					}

					results[idx] = parseResult{
						session:   session,
						jsonlFile: j.jsonlFile,
						mtime:     j.mtime,
						newOffset: newOffset,
					}
				}
			}()
		}
		wg.Wait()

		// Phase 3: enrich worktree info and update cache (sequential).
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
			cache.Put(r.jsonlFile, r.mtime, r.newOffset, r.session)
			sessions = append(sessions, r.session)
		}
	}

	cache.Prune(seen)
	return sessions, nil
}

// decodePiDirName reverses the pi encoding: --path-segments-- → /path/segments
// Pi encodes paths as --<path with / replaced by ->--.
func decodePiDirName(name string) string {
	// Strip leading and trailing "--"
	if len(name) > 4 && name[:2] == "--" && name[len(name)-2:] == "--" {
		name = name[2 : len(name)-2]
	}
	// Replace - with /
	return "/" + strings.ReplaceAll(name, "-", "/")
}
