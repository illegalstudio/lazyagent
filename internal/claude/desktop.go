package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nahime0/lazyagent/internal/model"
)

// DesktopSessionsDir returns the path to Claude Desktop's session metadata directory.
// Returns empty string on non-macOS platforms.
func DesktopSessionsDir() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions")
}

// desktopSessionJSON maps the JSON metadata files written by Claude Desktop.
type desktopSessionJSON struct {
	SessionID      string `json:"sessionId"`
	CLISessionID   string `json:"cliSessionId"`
	Title          string `json:"title"`
	CWD            string `json:"cwd"`
	Model          string `json:"model"`
	PermissionMode string `json:"permissionMode"`
	IsArchived     bool   `json:"isArchived"`
	CreatedAt      int64  `json:"createdAt"`      // epoch millis
	LastActivityAt int64  `json:"lastActivityAt"` // epoch millis
}

// loadDesktopMetadata scans the Desktop sessions directory and returns
// metadata indexed by cliSessionId (which matches JSONL filenames).
// Uses a cache to skip unchanged files. Returns an empty map on any error.
func loadDesktopMetadata(cache *DesktopCache) map[string]*model.DesktopMeta {
	dir := DesktopSessionsDir()
	if dir == "" {
		return nil
	}

	result := make(map[string]*model.DesktopMeta)
	seen := make(map[string]struct{})

	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasPrefix(d.Name(), "local_") || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		seen[path] = struct{}{}

		// Check cache by mtime
		info, infoErr := d.Info()
		if infoErr == nil {
			cache.mu.Lock()
			if e, ok := cache.entries[path]; ok && e.mtime.Equal(info.ModTime()) {
				result[e.cliID] = e.meta
				cache.mu.Unlock()
				return nil
			}
			cache.mu.Unlock()
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var dj desktopSessionJSON
		if err := json.Unmarshal(data, &dj); err != nil {
			return nil
		}
		if dj.CLISessionID == "" {
			return nil
		}

		meta := &model.DesktopMeta{
			Title:          dj.Title,
			DesktopID:      dj.SessionID,
			PermissionMode: dj.PermissionMode,
			IsArchived:     dj.IsArchived,
		}
		if dj.CreatedAt > 0 {
			meta.CreatedAt = time.UnixMilli(dj.CreatedAt)
		}

		result[dj.CLISessionID] = meta

		// Store in cache
		if infoErr == nil {
			cache.mu.Lock()
			cache.entries[path] = desktopCacheEntry{mtime: info.ModTime(), meta: meta, cliID: dj.CLISessionID}
			cache.mu.Unlock()
		}
		return nil
	})

	// Prune stale entries
	cache.mu.Lock()
	for k := range cache.entries {
		if _, ok := seen[k]; !ok {
			delete(cache.entries, k)
		}
	}
	cache.mu.Unlock()

	return result
}
