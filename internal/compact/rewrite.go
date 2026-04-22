package compact

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/illegalstudio/lazyagent/internal/chatops"
	"github.com/illegalstudio/lazyagent/internal/claude"
	"github.com/illegalstudio/lazyagent/internal/codex"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/pi"
)

// lineMutator rewrites a single parsed JSON line in place. It is called once
// per JSONL entry and returns the number of bytes saved (for reporting).
// Returning an error aborts the whole file — the caller will skip the
// rewrite and keep the original intact.
type lineMutator func(entry map[string]any, threshold int64) (saved int64)

// mutatorFor returns the per-agent field-path mutator. Unknown agents get a
// no-op mutator that leaves the file unchanged.
func mutatorFor(agent string) lineMutator {
	switch agent {
	case "claude":
		return compactClaudeLine
	case "codex":
		return compactCodexLine
	default:
		return func(map[string]any, int64) int64 { return 0 }
	}
}

// rewriteFile reads a JSONL file, applies the mutator to every line, and
// writes the result atomically. Returns the new file size. If the line
// count before and after differs, the rewrite is aborted and the original
// is left untouched.
//
// When backup is true, the pre-rewrite file is copied to <path>.bak.
func rewriteFile(path string, mutator lineMutator, threshold int64, backup bool) (int64, error) {
	if err := guardPath(path); err != nil {
		return 0, err
	}

	if backup {
		if err := copyFile(path, path+".bak"); err != nil {
			return 0, fmt.Errorf("backup: %w", err)
		}
	}

	srcInfo, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	in, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	tmpPath := path + ".compact.tmp"
	// Preserve the source mode so we don't quietly widen permissions
	// (e.g. turn a 0600 private JSONL into 0644 world-readable).
	out, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return 0, err
	}
	cleanup := true
	defer func() {
		out.Close()
		if cleanup {
			os.Remove(tmpPath)
		}
	}()

	scanner := bufio.NewScanner(in)
	// Claude / Codex entries can be multi-megabyte when images are embedded.
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	writer := bufio.NewWriter(out)

	inLines, outLines := 0, 0
	for scanner.Scan() {
		inLines++
		raw := scanner.Bytes()

		var entry map[string]any
		if err := json.Unmarshal(raw, &entry); err != nil {
			// Preserve malformed lines verbatim so we never turn a bad line
			// into a lost line. The line-count check still protects us.
			if _, err := writer.Write(raw); err != nil {
				return 0, err
			}
			if err := writer.WriteByte('\n'); err != nil {
				return 0, err
			}
			outLines++
			continue
		}

		mutator(entry, threshold)

		encoded, err := json.Marshal(entry)
		if err != nil {
			return 0, fmt.Errorf("re-marshal line %d: %w", inLines, err)
		}
		if _, err := writer.Write(encoded); err != nil {
			return 0, err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return 0, err
		}
		outLines++
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	if err := writer.Flush(); err != nil {
		return 0, err
	}
	if err := out.Close(); err != nil {
		return 0, err
	}

	if inLines != outLines {
		return 0, fmt.Errorf("line count mismatch (in=%d out=%d) — aborted", inLines, outLines)
	}

	info, err := os.Stat(tmpPath)
	if err != nil {
		return 0, err
	}
	// Bail if the rewrite didn't actually shrink the file — can happen
	// when nothing was truncated but map-key re-ordering slightly changed
	// the serialized form. Keep the original bytes on disk.
	origInfo, err := os.Stat(path)
	if err == nil && info.Size() >= origInfo.Size() {
		return origInfo.Size(), nil
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return 0, err
	}
	cleanup = false
	return info.Size(), nil
}

// estimateRewrite runs the mutator in memory without writing anything, and
// returns what the post-rewrite file size would be. Used by --dry-run.
func estimateRewrite(path, agent string, threshold int64) (int64, error) {
	in, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	mutator := mutatorFor(agent)

	var total int64
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)

	for scanner.Scan() {
		raw := scanner.Bytes()
		var entry map[string]any
		if err := json.Unmarshal(raw, &entry); err != nil {
			total += int64(len(raw)) + 1
			continue
		}
		mutator(entry, threshold)
		encoded, err := json.Marshal(entry)
		if err != nil {
			total += int64(len(raw)) + 1
			continue
		}
		total += int64(len(encoded)) + 1
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return total, nil
}

// estimateSizes fills SizeAfter for every candidate based on a dry-run
// simulation. Best-effort: on error the after-size equals the before-size.
func estimateSizes(candidates []Candidate, threshold int64) {
	for i := range candidates {
		c := &candidates[i]
		after, err := estimateRewrite(c.Session.JSONLPath, c.Session.Agent, threshold)
		if err != nil {
			c.SizeAfter = c.SizeBefore
			continue
		}
		c.SizeAfter = after
	}
}

// guardPath blocks compacting anything outside the known agent roots.
func guardPath(path string) error {
	cfg := core.LoadConfig()
	var roots []string
	roots = append(roots, claude.ClaudeProjectsDirs(cfg.ClaudeDirs)...)
	if d := pi.PiSessionsDir(); d != "" {
		roots = append(roots, d)
	}
	if d := codex.SessionsDir(); d != "" {
		roots = append(roots, d)
	}
	return chatops.EnsureWithin(path, roots)
}

// copyFile copies src → dst preserving the mode bits. Used for .bak sidecars.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	// If a stale backup exists remove it first — os.Create won't preserve
	// an existing file's permission and we don't want to risk losing
	// content we can't recover.
	_ = os.Remove(dst)

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// executeCompact runs the real rewrite over every candidate. Returns
// (processed, failed, bytesSaved).
func executeCompact(candidates []Candidate, opts options) (int, int, int64) {
	var processed, failed int
	var saved int64
	for i := range candidates {
		c := &candidates[i]
		mutator := mutatorFor(c.Session.Agent)
		newSize, err := rewriteFile(c.Session.JSONLPath, mutator, opts.threshold, opts.backup)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed: %s — %v\n", filepath.Base(c.Session.JSONLPath), err)
			failed++
			continue
		}
		c.SizeAfter = newSize
		if newSize < c.SizeBefore {
			saved += c.SizeBefore - newSize
		}
		processed++
	}
	return processed, failed, saved
}
