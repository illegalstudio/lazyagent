package prune

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/claude"
	"github.com/illegalstudio/lazyagent/internal/codex"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
	"github.com/illegalstudio/lazyagent/internal/pi"
)

// executeDelete removes every candidate and returns (deleted, failed) counts.
// After all per-session deletions it performs batched housekeeping: removing
// empty project folders and rewriting the Codex index file once.
func executeDelete(candidates []Candidate) (int, int) {
	cfg := core.LoadConfig()
	claudeRoots := claude.ClaudeProjectsDirs(cfg.ClaudeDirs)
	piRoot := pi.PiSessionsDir()
	codexRoot := codex.SessionsDir()
	desktopRoot := claude.DesktopSessionsDir()

	// Collect Codex session IDs to strip from the index in a single rewrite.
	codexIDsToStrip := make(map[string]struct{})
	// Collect parent dirs that might become empty after deletion.
	dirsToGC := make(map[string]struct{})

	var deleted, failed int
	for _, c := range candidates {
		s := c.Session
		var err error
		switch s.Agent {
		case "claude":
			err = deleteClaudeSession(s, claudeRoots, desktopRoot)
			if err == nil && s.JSONLPath != "" {
				dirsToGC[filepath.Dir(s.JSONLPath)] = struct{}{}
			}
		case "pi":
			err = deletePiSession(s, piRoot)
			if err == nil && s.JSONLPath != "" {
				dirsToGC[filepath.Dir(s.JSONLPath)] = struct{}{}
			}
		case "codex":
			err = deleteCodexSession(s, codexRoot)
			if err == nil && s.SessionID != "" {
				codexIDsToStrip[s.SessionID] = struct{}{}
				dirsToGC[filepath.Dir(s.JSONLPath)] = struct{}{}
			}
		default:
			err = fmt.Errorf("agent %q is not supported by prune", s.Agent)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "failed: %s — %v\n", s.JSONLPath, err)
			failed++
			continue
		}
		deleted++
	}

	// Rewrite Codex index once, removing all stripped IDs.
	if len(codexIDsToStrip) > 0 {
		if err := removeCodexIndexEntries(codex.SessionIndexPath(), codexIDsToStrip); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update %s: %v\n", codex.SessionIndexPath(), err)
		}
	}

	// Best-effort: remove any now-empty project directories.
	removeEmptyDirs(dirsToGC, claudeRoots, piRoot, codexRoot)

	return deleted, failed
}

func deleteClaudeSession(s *model.Session, roots []string, desktopRoot string) error {
	if err := ensureWithin(s.JSONLPath, roots); err != nil {
		return err
	}
	if err := os.Remove(s.JSONLPath); err != nil {
		return err
	}
	// Claude Desktop stores a sidecar JSON whose filename contains the CLI
	// session ID; delete it too so the desktop UI doesn't show a dangling entry.
	if desktopRoot != "" && s.SessionID != "" {
		removeDesktopSidecar(desktopRoot, s.SessionID)
	}
	return nil
}

func deletePiSession(s *model.Session, root string) error {
	if root == "" {
		return fmt.Errorf("pi sessions directory not found")
	}
	if err := ensureWithin(s.JSONLPath, []string{root}); err != nil {
		return err
	}
	return os.Remove(s.JSONLPath)
}

func deleteCodexSession(s *model.Session, root string) error {
	if root == "" {
		return fmt.Errorf("codex sessions directory not found")
	}
	if err := ensureWithin(s.JSONLPath, []string{root}); err != nil {
		return err
	}
	return os.Remove(s.JSONLPath)
}

// ensureWithin guards against deleting files outside the known agent roots.
// It resolves both the target and each root via filepath.Abs and checks that
// the target sits inside one of them.
func ensureWithin(target string, roots []string) error {
	if target == "" {
		return fmt.Errorf("empty file path")
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve target: %w", err)
	}
	for _, r := range roots {
		if r == "" {
			continue
		}
		absRoot, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absRoot, absTarget)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, "..") && !strings.HasPrefix(rel, string(filepath.Separator))) {
			return nil
		}
	}
	return fmt.Errorf("refusing to delete %s: outside expected agent directories", target)
}

// removeDesktopSidecar scans the desktop sessions directory for JSON files
// referencing the given CLI session ID and deletes them. Best-effort only.
func removeDesktopSidecar(desktopRoot, cliSessionID string) {
	entries, err := os.ReadDir(desktopRoot)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "local_") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(desktopRoot, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var meta struct {
			CLISessionID string `json:"cliSessionId"`
		}
		if json.Unmarshal(data, &meta) != nil {
			continue
		}
		if meta.CLISessionID == cliSessionID {
			_ = os.Remove(path)
		}
	}
}

// removeCodexIndexEntries rewrites the Codex session_index.jsonl file, dropping
// lines whose id matches one of the given sessions. The rewrite is atomic:
// write to a temp file, then rename over the original.
func removeCodexIndexEntries(indexPath string, ids map[string]struct{}) error {
	if indexPath == "" {
		return nil
	}
	in, err := os.Open(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer in.Close()

	tmpPath := indexPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	// Ensure we don't leave a stray temp file on any error path.
	cleanup := true
	defer func() {
		out.Close()
		if cleanup {
			os.Remove(tmpPath)
		}
	}()

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	writer := bufio.NewWriter(out)

	for scanner.Scan() {
		line := scanner.Bytes()
		var entry struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(line, &entry); err == nil {
			if _, drop := ids[entry.ID]; drop {
				continue
			}
		}
		if _, err := writer.Write(line); err != nil {
			return err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, indexPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// removeEmptyDirs deletes any project directory in dirs that is now empty.
// Only directories that sit directly inside one of the known agent roots are
// removed, never the roots themselves.
func removeEmptyDirs(dirs map[string]struct{}, claudeRoots []string, piRoot, codexRoot string) {
	roots := make([]string, 0, len(claudeRoots)+2)
	roots = append(roots, claudeRoots...)
	if piRoot != "" {
		roots = append(roots, piRoot)
	}
	if codexRoot != "" {
		roots = append(roots, codexRoot)
	}

	for dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if !isBelowAny(absDir, roots) {
			continue
		}
		entries, err := os.ReadDir(absDir)
		if err != nil || len(entries) > 0 {
			continue
		}
		_ = os.Remove(absDir)
	}
}

func isBelowAny(path string, roots []string) bool {
	for _, r := range roots {
		absRoot, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		if path == absRoot {
			// Never delete the root itself.
			return false
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(rel, "..") && !strings.HasPrefix(rel, string(filepath.Separator)) {
			return true
		}
	}
	return false
}
