package chatops

import (
	"fmt"
	"path/filepath"
	"strings"
)

// EnsureWithin returns nil only if target resolves to a path inside one of
// the given roots. It guards against deleting or mutating files outside the
// known agent directories (e.g. a rogue JSONLPath or a symlink escape).
func EnsureWithin(target string, roots []string) error {
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
	return fmt.Errorf("refusing to touch %s: outside expected agent directories", target)
}

// IsBelowAny returns true if path lives inside any of roots (excluding the
// roots themselves). Used when garbage-collecting empty project dirs.
func IsBelowAny(path string, roots []string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, r := range roots {
		absRoot, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		if absPath == absRoot {
			return false
		}
		rel, err := filepath.Rel(absRoot, absPath)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(rel, "..") && !strings.HasPrefix(rel, string(filepath.Separator)) {
			return true
		}
	}
	return false
}
