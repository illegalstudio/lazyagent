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
func DiscoverSessions(cache *model.SessionCache, desktopCache *DesktopCache) ([]*model.Session, error) {
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

			cached, offset, mtime := cache.GetIncremental(jsonlFile)
			var session *model.Session
			switch {
			case cached != nil && offset == 0:
				// Full cache hit — file unchanged.
				session = cached
			case cached != nil && offset > 0:
				// Incremental: parse only new tail lines.
				s, newOffset, err := ParseJSONLIncremental(jsonlFile, offset, cached)
				if err != nil {
					continue
				}
				session = s
				cache.Put(jsonlFile, mtime, newOffset, session)
			default:
				// Full miss: parse entire file.
				s, size, err := ParseJSONL(jsonlFile)
				if err != nil {
					continue
				}
				session = s

				if session.CWD == "" {
					session.CWD = decodeDirName(projectEntry.Name())
				}

				if _, ok := wtCache[session.CWD]; !ok {
					isWT, mainRepo := IsWorktree(session.CWD)
					wtCache[session.CWD] = wtInfo{isWorktree: isWT, mainRepo: mainRepo}
				}
				wt := wtCache[session.CWD]
				session.IsWorktree = wt.isWorktree
				session.MainRepo = wt.mainRepo
				cache.Put(jsonlFile, mtime, size, session)
			}

			sessions = append(sessions, session)
		}
	}
	cache.Prune(seen)

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

func decodeDirName(name string) string {
	// Reverse of ProjectDirForCWD: dashes → slashes, prepend /
	// This is a best-effort heuristic
	return "/" + strings.ReplaceAll(name, "-", "/")
}
