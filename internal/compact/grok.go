package compact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// grokBulkJSONL lists the JSONL files inside a Grok session directory whose
// oversized payloads compact truncates. Phase 0 resume-verification confirmed
// these are safe to shrink (see 2026-05-20-grok-compact-phase0-findings.md):
//
//   - updates.jsonl       — ACP render/telemetry stream, ~70% of session size,
//     not replayed on resume → deep string truncation.
//   - chat_history.jsonl  — model-facing transcript; only tool_result.content
//     is truncated (same trade-off as Claude compact).
//   - rewind_points.jsonl — checkpoint snapshots; truncating disables Grok's
//     rewind feature for the session.
var grokBulkJSONL = []string{"updates.jsonl", "chat_history.jsonl", "rewind_points.jsonl"}

// sessionSizeBytes returns the on-disk size of a session: the live directory
// total (excluding .bak sidecars) for directory-backed agents, a single file
// size for JSONL-per-session agents.
func sessionSizeBytes(s *model.Session) int64 {
	if s.Agent == "grok" {
		return grokLiveDirBytes(s.JSONLPath)
	}
	if s.Agent == "kimi" {
		return kimiLiveDirBytes(s.JSONLPath)
	}
	info, err := os.Stat(s.JSONLPath)
	if err != nil {
		return 0
	}
	return info.Size()
}

// grokFileMutator returns the per-line mutator for a Grok session file.
func grokFileMutator(name string) lineMutator {
	if name == "chat_history.jsonl" {
		return compactGrokChatLine
	}
	// updates.jsonl, rewind_points.jsonl: not replayed on resume, so every
	// oversized string leaf can be truncated.
	return func(entry map[string]any, threshold int64) int64 {
		return truncateGrokDeep(entry, threshold)
	}
}

// compactGrokChatLine truncates the content of a tool_result entry. The
// tool_call_id ↔ tool_calls[].id linkage is untouched so the transcript stays
// internally consistent and resumable.
func compactGrokChatLine(entry map[string]any, threshold int64) int64 {
	if entry["type"] != "tool_result" {
		return 0
	}
	s, ok := entry["content"].(string)
	if !ok {
		return 0
	}
	if newVal, delta := truncateString(s, threshold); delta > 0 {
		entry["content"] = newVal
		return delta
	}
	return 0
}

// truncateGrokDeep recursively truncates every oversized string value in a
// decoded JSON structure. Used for files Grok does not replay on resume.
func truncateGrokDeep(v any, threshold int64) int64 {
	var saved int64
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if s, ok := child.(string); ok {
				if newVal, delta := truncateString(s, threshold); delta > 0 {
					t[k] = newVal
					saved += delta
				}
				continue
			}
			saved += truncateGrokDeep(child, threshold)
		}
	case []any:
		for i, child := range t {
			if s, ok := child.(string); ok {
				if newVal, delta := truncateString(s, threshold); delta > 0 {
					t[i] = newVal
					saved += delta
				}
				continue
			}
			saved += truncateGrokDeep(child, threshold)
		}
	}
	return saved
}

// grokTerminalLogs returns the terminal/*.log paths inside a session dir.
func grokTerminalLogs(dir string) []string {
	logs, _ := filepath.Glob(filepath.Join(dir, "terminal", "*.log"))
	return logs
}

// grokLiveDirBytes returns the total on-disk size of a Grok session directory,
// excluding .bak sidecar files created by compact. This gives the "live"
// session size that callers compare against the pre-compaction baseline.
func grokLiveDirBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if filepath.Ext(name) == ".bak" || strings.HasSuffix(name, ".compact.tmp") {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// compactGrokSession truncates oversized payloads across the bulky files of a
// Grok session directory and returns the directory's new total size (excluding
// .bak sidecar files so the caller can compare against the pre-compact size).
func compactGrokSession(dir string, threshold int64, backup bool) (int64, error) {
	if err := guardPath(dir); err != nil {
		return 0, err
	}
	for _, name := range grokBulkJSONL {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			continue // file absent for this session
		}
		if _, err := rewriteFile(path, grokFileMutator(name), threshold, backup); err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
	}
	for _, log := range grokTerminalLogs(dir) {
		if err := compactGrokTerminalLog(log, threshold, backup); err != nil {
			return 0, fmt.Errorf("%s: %w", filepath.Base(log), err)
		}
	}
	return grokLiveDirBytes(dir), nil
}

// compactGrokTerminalLog truncates a raw (non-JSON) terminal capture log.
func compactGrokTerminalLog(path string, threshold int64, backup bool) error {
	if err := guardPath(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= threshold {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	newVal, delta := truncateString(string(data), threshold)
	if delta <= 0 {
		return nil
	}
	if backup {
		if err := copyFile(path, path+".bak"); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	return os.WriteFile(path, []byte(newVal), info.Mode())
}

// estimateGrokSession simulates the rewrite of every bulky file and returns
// the directory's projected post-compaction total size. It writes nothing.
// The returned size excludes .bak sidecars to match compactGrokSession output.
func estimateGrokSession(dir string, threshold int64) (int64, error) {
	total := grokLiveDirBytes(dir)
	for _, name := range grokBulkJSONL {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		after, err := estimateJSONL(path, grokFileMutator(name), threshold)
		if err != nil {
			continue
		}
		total -= info.Size() - after
	}
	for _, log := range grokTerminalLogs(dir) {
		info, err := os.Stat(log)
		if err != nil || info.Size() <= threshold {
			continue
		}
		data, err := os.ReadFile(log)
		if err != nil {
			continue
		}
		if newVal, delta := truncateString(string(data), threshold); delta > 0 {
			total -= info.Size() - int64(len(newVal))
		}
	}
	return total, nil
}
