package claude

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nahime0/lazyagent/internal/model"
)

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
// Every JSONL file is a separate session.
func DiscoverSessions() ([]*model.Session, error) {
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
			session, err := ParseJSONL(jsonlFile)
			if err != nil {
				continue
			}
			// If CWD is empty (brand new session not yet written), derive
			// from the encoded directory name as a best-effort fallback
			if session.CWD == "" {
				session.CWD = decodeDirName(projectEntry.Name())
			}

			if _, seen := wtCache[session.CWD]; !seen {
				isWT, mainRepo := IsWorktree(session.CWD)
				wtCache[session.CWD] = wtInfo{isWorktree: isWT, mainRepo: mainRepo}
			}
			wt := wtCache[session.CWD]
			session.IsWorktree = wt.isWorktree
			session.MainRepo = wt.mainRepo

			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func decodeDirName(name string) string {
	// Reverse of ProjectDirForCWD: dashes → slashes, prepend /
	// This is a best-effort heuristic
	return "/" + strings.ReplaceAll(name, "-", "/")
}
