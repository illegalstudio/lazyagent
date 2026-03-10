package claude

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nahime0/lazyagent/internal/model"
)

// SessionCache caches parsed JSONL sessions keyed by file path.
// On subsequent calls, only files whose mtime changed are re-parsed.
type SessionCache struct {
	mu      sync.Mutex
	entries map[string]sessionCacheEntry
}

type sessionCacheEntry struct {
	mtime   time.Time
	session *model.Session
}

// NewSessionCache creates an empty session cache.
func NewSessionCache() *SessionCache {
	return &SessionCache{entries: make(map[string]sessionCacheEntry)}
}

// DesktopCache caches parsed desktop metadata JSON files keyed by file path.
type DesktopCache struct {
	mu      sync.Mutex
	entries map[string]desktopCacheEntry
}

type desktopCacheEntry struct {
	mtime time.Time
	meta  *model.DesktopMeta
	cliID string
}

// NewDesktopCache creates an empty desktop cache.
func NewDesktopCache() *DesktopCache {
	return &DesktopCache{entries: make(map[string]desktopCacheEntry)}
}

// IsWorktree detects if a path is a git worktree and returns the main repo.
func IsWorktree(path string) (bool, string) {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--git-dir").Output()
	if err != nil {
		return false, ""
	}
	gitDir := strings.TrimSpace(string(out))

	// In a regular repo: .git
	// In a worktree: absolute path like /repo/.git/worktrees/name
	if filepath.Base(gitDir) == ".git" || !filepath.IsAbs(gitDir) {
		return false, ""
	}

	// It's a worktree — find the main repo
	// gitDir looks like /path/to/main/.git/worktrees/branch-name
	parts := strings.Split(gitDir, string(os.PathSeparator))
	for i, p := range parts {
		if p == ".git" && i+1 < len(parts) && parts[i+1] == "worktrees" {
			mainRepo := filepath.Join(parts[:i]...)
			return true, "/" + mainRepo
		}
	}
	return true, ""
}

// ClaudeProjectsDir returns the path to ~/.claude/projects.
func ClaudeProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// ProjectDirForCWD encodes a CWD path to the ~/.claude/projects directory name.
// Claude replaces every / and . with -, keeping the leading slash as a leading -.
func ProjectDirForCWD(cwd string) string {
	r := strings.NewReplacer("/", "-", ".", "-")
	return r.Replace(cwd)
}

// DiscoverSessions scans ~/.claude/projects for JSONL session files.
// Every JSONL file is a separate session. Uses caches to skip unchanged files.
func DiscoverSessions(cache *SessionCache, desktopCache *DesktopCache) ([]*model.Session, error) {
	projectsDir := ClaudeProjectsDir()
	if projectsDir == "" {
		return nil, fmt.Errorf("could not find home directory")
	}

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("could not read projects dir: %w", err)
	}

	// Cache worktree lookups per CWD to avoid redundant git calls.
	type wtInfo struct {
		isWorktree bool
		mainRepo   string
	}
	wtCache := make(map[string]wtInfo)

	seen := make(map[string]struct{})
	var sessions []*model.Session
	for _, projectEntry := range entries {
		if !projectEntry.IsDir() {
			continue
		}
		projectPath := filepath.Join(projectsDir, projectEntry.Name())
		jsonlFiles, err := filepath.Glob(filepath.Join(projectPath, "*.jsonl"))
		if err != nil || len(jsonlFiles) == 0 {
			continue
		}

		for _, jsonlFile := range jsonlFiles {
			seen[jsonlFile] = struct{}{}

			cached := true
			session := cache.get(jsonlFile)
			if session == nil {
				cached = false
				s, err := ParseJSONL(jsonlFile)
				if err != nil {
					continue
				}
				session = s
			}

			// If CWD is empty (brand new session not yet written), derive
			// from the encoded directory name as a best-effort fallback
			if session.CWD == "" {
				session.CWD = decodeDirName(projectEntry.Name())
			}

			// Only run git worktree check for newly parsed sessions.
			// Cached sessions already have IsWorktree/MainRepo set.
			if !cached {
				if _, ok := wtCache[session.CWD]; !ok {
					isWT, mainRepo := IsWorktree(session.CWD)
					wtCache[session.CWD] = wtInfo{isWorktree: isWT, mainRepo: mainRepo}
				}
				wt := wtCache[session.CWD]
				session.IsWorktree = wt.isWorktree
				session.MainRepo = wt.mainRepo
				cache.put(jsonlFile, session)
			}

			sessions = append(sessions, session)
		}
	}
	cache.prune(seen)

	// Enrich with Claude Desktop metadata.
	desktopMeta := loadDesktopMetadata(desktopCache)
	for _, session := range sessions {
		if meta, ok := desktopMeta[session.SessionID]; ok {
			session.Desktop = meta
			if session.Name == "" && meta.Title != "" {
				session.Name = meta.Title
			}
		}
	}

	return sessions, nil
}

// get returns a cached session if the file mtime hasn't changed.
func (c *SessionCache) get(path string) *model.Session {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[path]; ok && e.mtime.Equal(info.ModTime()) {
		return e.session
	}
	return nil
}

// put stores a session in the cache with the file's current mtime.
func (c *SessionCache) put(path string, s *model.Session) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.entries[path] = sessionCacheEntry{mtime: info.ModTime(), session: s}
	c.mu.Unlock()
}

// prune removes cache entries for files no longer on disk.
func (c *SessionCache) prune(seen map[string]struct{}) {
	c.mu.Lock()
	for k := range c.entries {
		if _, ok := seen[k]; !ok {
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()
}

func decodeDirName(name string) string {
	// Reverse of ProjectDirForCWD: dashes → slashes, prepend /
	// This is a best-effort heuristic
	return "/" + strings.ReplaceAll(name, "-", "/")
}
