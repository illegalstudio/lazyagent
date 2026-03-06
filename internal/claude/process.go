package claude

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcessInfo holds OS-level info about a running Claude process.
type ProcessInfo struct {
	PID         int
	CWD         string
	Args        string
	IsDangerous bool
	ResumeUUID  string // session UUID from --resume flag, if present
}

// FindClaudeProcesses returns all running Claude Code processes on macOS.
// Uses pgrep -lf to avoid issues with ps aliases and spaces in paths.
func FindClaudeProcesses() ([]ProcessInfo, error) {
	out, err := exec.Command("pgrep", "-lf", "claude").Output()
	if err != nil {
		// exit code 1 means no matches — not an error
		return nil, nil
	}

	selfPID := os.Getpid()
	var procs []ProcessInfo

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		if pid == selfPID {
			continue
		}
		// fields[1] is the executable path (or bare name).
		// Only match actual claude binaries, not wrappers (disclaimer, ShipIt, etc.)
		if filepath.Base(fields[1]) != "claude" {
			continue
		}
		args := strings.Join(fields[1:], " ")
		cwd := getCWD(pid)
		procs = append(procs, ProcessInfo{
			PID:         pid,
			CWD:         cwd,
			Args:        args,
			IsDangerous: strings.Contains(args, "dangerously-skip-permissions"),
			ResumeUUID:  extractResumeUUID(args),
		})
	}
	return procs, nil
}

// getCWD returns the current working directory of a process via lsof.
// The -a flag is required to AND the filters (-p AND -d cwd), otherwise lsof
// returns all files for all processes matching any criterion.
func getCWD(pid int) string {
	out, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			return strings.TrimPrefix(line, "n")
		}
	}
	return ""
}

// EnrichWithProcessInfo matches sessions to running processes.
// Matching strategy (in order):
//  1. Session UUID → process --resume flag (exact match, most reliable)
//  2. CWD → process working directory (for fresh sessions without --resume)
func EnrichWithProcessInfo(sessions []*Session, procs []ProcessInfo) {
	cwdToProc := make(map[string]ProcessInfo)
	uuidToProc := make(map[string]ProcessInfo)
	for _, p := range procs {
		if p.CWD != "" {
			cwdToProc[p.CWD] = p
		}
		if p.ResumeUUID != "" {
			uuidToProc[p.ResumeUUID] = p
		}
	}
	for _, s := range sessions {
		// Prefer UUID match (handles resumed sessions and avoids CWD collisions)
		if p, ok := uuidToProc[s.SessionID]; ok {
			s.PID = p.PID
			s.IsDangerous = p.IsDangerous
			continue
		}
		// Fall back to CWD match (handles brand new sessions)
		if p, ok := cwdToProc[s.CWD]; ok {
			s.PID = p.PID
			s.IsDangerous = p.IsDangerous
		}
	}
}

// extractResumeUUID parses the --resume <uuid> flag from Claude's args.
func extractResumeUUID(args string) string {
	fields := strings.Fields(args)
	for i, f := range fields {
		if f == "--resume" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
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
// Claude encodes paths by replacing / with -.
func ProjectDirForCWD(cwd string) string {
	// Replace leading / and all / with -
	encoded := strings.TrimPrefix(cwd, "/")
	encoded = strings.ReplaceAll(encoded, "/", "-")
	return encoded
}

// DiscoverActiveSessions builds the session list from running processes (process-first).
// Each process yields exactly one session, so N processes → N entries, with no
// historical duplicates from old JSONL files in the same project directory.
func DiscoverActiveSessions(procs []ProcessInfo) ([]*Session, error) {
	projectsDir := ClaudeProjectsDir()
	if projectsDir == "" {
		return nil, fmt.Errorf("could not find home directory")
	}

	wtCache := make(map[string][2]string)
	var sessions []*Session

	for _, proc := range procs {
		var jsonlPath string

		// Strategy 1: --resume <uuid> → direct file by UUID
		if proc.ResumeUUID != "" && proc.CWD != "" {
			encoded := ProjectDirForCWD(proc.CWD)
			candidate := filepath.Join(projectsDir, encoded, proc.ResumeUUID+".jsonl")
			if _, err := os.Stat(candidate); err == nil {
				jsonlPath = candidate
			}
		}

		// Strategy 2: most recent JSONL in the project directory
		if jsonlPath == "" && proc.CWD != "" {
			encoded := ProjectDirForCWD(proc.CWD)
			projectDir := filepath.Join(projectsDir, encoded)
			jsonlFiles, _ := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
			jsonlPath = mostRecentFile(jsonlFiles)
		}

		var session *Session
		if jsonlPath != "" {
			var err error
			session, err = ParseJSONL(jsonlPath)
			if err != nil {
				session = nil
			}
		}
		if session == nil {
			// Process running but no JSONL yet (just launched)
			session = &Session{Status: StatusUnknown}
		}

		if session.CWD == "" {
			session.CWD = proc.CWD
		}
		session.PID = proc.PID
		session.IsDangerous = proc.IsDangerous

		if _, seen := wtCache[session.CWD]; !seen {
			isWT, mainRepo := IsWorktree(session.CWD)
			v := [2]string{"", ""}
			if isWT {
				v[0] = "1"
			}
			v[1] = mainRepo
			wtCache[session.CWD] = v
		}
		wt := wtCache[session.CWD]
		session.IsWorktree = wt[0] == "1"
		session.MainRepo = wt[1]

		sessions = append(sessions, session)
	}
	return sessions, nil
}

// DiscoverSessions scans ~/.claude/projects for JSONL session files.
// Every JSONL file is a separate session — used for "show all" mode.
func DiscoverSessions() ([]*Session, error) {
	projectsDir := ClaudeProjectsDir()
	if projectsDir == "" {
		return nil, fmt.Errorf("could not find home directory")
	}

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("could not read projects dir: %w", err)
	}

	// Cache worktree lookups per CWD to avoid redundant git calls
	wtCache := make(map[string][2]string) // cwd → [isWT, mainRepo]

	var sessions []*Session
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
				v := [2]string{"", ""}
				if isWT {
					v[0] = "1"
				}
				v[1] = mainRepo
				wtCache[session.CWD] = v
			}
			wt := wtCache[session.CWD]
			session.IsWorktree = wt[0] == "1"
			session.MainRepo = wt[1]

			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func mostRecentFile(files []string) string {
	var latest string
	var latestMod int64
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if info.ModTime().Unix() > latestMod {
			latestMod = info.ModTime().Unix()
			latest = f
		}
	}
	return latest
}

func decodeDirName(name string) string {
	// Reverse of ProjectDirForCWD: dashes → slashes, prepend /
	// This is a best-effort heuristic
	return "/" + strings.ReplaceAll(name, "-", "/")
}
