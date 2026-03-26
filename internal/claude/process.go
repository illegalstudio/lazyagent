package claude

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
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

// ClaudeProjectsDirs returns the Claude projects directories to scan.
// If configDirs is non-empty, those are used directly (with /projects appended).
// Otherwise it auto-detects from CLAUDE_CONFIG_DIR (falling back to ~/.claude).
func ClaudeProjectsDirs(configDirs []string) []string {
	if len(configDirs) > 0 {
		dirs := make([]string, 0, len(configDirs))
		for _, d := range configDirs {
			dirs = append(dirs, filepath.Join(d, "projects"))
		}
		return dirs
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	defaultDir := filepath.Join(home, ".claude")
	if configDir := os.Getenv("CLAUDE_CONFIG_DIR"); configDir != "" {
		if configDir == defaultDir {
			return []string{filepath.Join(defaultDir, "projects")}
		}
		return []string{
			filepath.Join(configDir, "projects"),
			filepath.Join(defaultDir, "projects"),
		}
	}
	return []string{filepath.Join(defaultDir, "projects")}
}

// wtInfo caches worktree lookups per CWD to avoid redundant git calls.
type wtInfo struct {
	isWorktree bool
	mainRepo   string
}

// ProjectDirForCWD encodes a CWD path to the ~/.claude/projects directory name.
// Claude replaces every / and . with -, keeping the leading slash as a leading -.
func ProjectDirForCWD(cwd string) string {
	r := strings.NewReplacer("/", "-", ".", "-")
	return r.Replace(cwd)
}

// DiscoverSessions scans Claude projects directories for JSONL session files.
// Every JSONL file is a separate session. Uses caches to skip unchanged files.
func DiscoverSessions(cache *model.SessionCache, desktopCache *DesktopCache, configDirs []string) ([]*model.Session, error) {
	projectsDirs := ClaudeProjectsDirs(configDirs)
	if len(projectsDirs) == 0 {
		return nil, fmt.Errorf("could not find any Claude projects directories")
	}

	wtCache := make(map[string]wtInfo)

	seen := make(map[string]struct{})
	var sessions []*model.Session
	for _, projectsDir := range projectsDirs {
		sessions = discoverInDir(projectsDir, cache, wtCache, seen, sessions)
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

// discoverInDir scans a single projects directory for JSONL session files
// and appends discovered sessions to the provided slice.
func discoverInDir(projectsDir string, cache *model.SessionCache, wtCache map[string]wtInfo, seen map[string]struct{}, sessions []*model.Session) []*model.Session {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return sessions // Directory may not exist; skip it
	}

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
				session = cached
			case cached != nil && offset > 0:
				s, newOffset, err := ParseJSONLIncremental(jsonlFile, offset, cached)
				if err != nil {
					continue
				}
				session = s
				if session.CWD == "" {
					session.CWD = decodeDirName(projectEntry.Name())
				}
				enrichWorktree(session, wtCache)
				cache.Put(jsonlFile, mtime, newOffset, session)
			default:
				s, size, err := ParseJSONL(jsonlFile)
				if err != nil {
					continue
				}
				session = s
				if session.CWD == "" {
					session.CWD = decodeDirName(projectEntry.Name())
				}
				enrichWorktree(session, wtCache)
				cache.Put(jsonlFile, mtime, size, session)
			}

			sessions = append(sessions, session)
		}
	}
	return sessions
}

// enrichWorktree sets worktree info on a session, using a cache to avoid redundant git calls.
func enrichWorktree(session *model.Session, wtCache map[string]wtInfo) {
	if _, ok := wtCache[session.CWD]; !ok {
		isWT, mainRepo := IsWorktree(session.CWD)
		wtCache[session.CWD] = wtInfo{isWT, mainRepo}
	}
	wt := wtCache[session.CWD]
	session.IsWorktree = wt.isWorktree
	session.MainRepo = wt.mainRepo
}

func decodeDirName(name string) string {
	// Reverse of ProjectDirForCWD: dashes → slashes, prepend /
	// This is a best-effort heuristic
	return "/" + strings.ReplaceAll(name, "-", "/")
}
