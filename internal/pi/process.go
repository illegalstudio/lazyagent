package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nahime0/lazyagent/internal/claude"
	"github.com/nahime0/lazyagent/internal/model"
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

// discoverSessionsFromDir scans a directory for pi session JSONL files.
// Exported for testing with synthetic directories.
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
		projectPath := filepath.Join(sessionsDir, projectEntry.Name())
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
				s, newOffset, err := ParsePiJSONLIncremental(jsonlFile, offset, cached)
				if err != nil {
					continue
				}
				session = s
				cache.Put(jsonlFile, mtime, newOffset, session)
			default:
				// Full miss: parse entire file.
				s, size, err := ParsePiJSONL(jsonlFile)
				if err != nil {
					continue
				}
				session = s

				if session.CWD == "" {
					session.CWD = decodePiDirName(projectEntry.Name())
				}

				if _, ok := wtCache[session.CWD]; !ok {
					isWT, mainRepo := claude.IsWorktree(session.CWD)
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
