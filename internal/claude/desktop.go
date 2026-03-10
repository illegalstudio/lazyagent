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
// Returns an empty map on any error — never fatal.
func loadDesktopMetadata() map[string]*model.DesktopMeta {
	dir := DesktopSessionsDir()
	if dir == "" {
		return nil
	}

	result := make(map[string]*model.DesktopMeta)

	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasPrefix(d.Name(), "local_") || !strings.HasSuffix(d.Name(), ".json") {
			return nil
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
		return nil
	})

	return result
}
