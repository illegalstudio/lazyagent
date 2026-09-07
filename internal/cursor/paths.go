package cursor

import (
	"os"
	"path/filepath"
	"runtime"
)

// stateDBPath returns the path to Cursor's global state.vscdb for the current
// platform, or "" when the home directory cannot be resolved. Every reader of
// Cursor state (session discovery, watch directories, the limits token) goes
// through here, so a wrong platform path hides Cursor entirely rather than
// producing an error.
func stateDBPath() string {
	// The home directory is only needed on macOS and Linux; a failed lookup
	// must not hide an APPDATA-based Windows path.
	home, _ := os.UserHomeDir()
	return stateDBPathFor(runtime.GOOS, home, os.Getenv("XDG_CONFIG_HOME"), os.Getenv("APPDATA"))
}

// stateDBPathFor is the platform-specific rule behind stateDBPath, with every
// input explicit so tests can cover each OS without changing the environment.
// Cursor keeps its user data where VS Code does on each platform:
//   - macOS:   ~/Library/Application Support/Cursor
//   - Linux:   $XDG_CONFIG_HOME/Cursor, defaulting to ~/.config/Cursor
//   - Windows: %APPDATA%\Cursor
//
// It returns "" when the input that platform depends on is missing.
func stateDBPathFor(goos, home, xdgConfigHome, appData string) string {
	var base string
	switch goos {
	case "darwin":
		if home == "" {
			return ""
		}
		base = filepath.Join(home, "Library", "Application Support", "Cursor")
	case "windows":
		if appData == "" {
			return ""
		}
		base = filepath.Join(appData, "Cursor")
	default:
		if xdgConfigHome == "" {
			if home == "" {
				return ""
			}
			xdgConfigHome = filepath.Join(home, ".config")
		}
		base = filepath.Join(xdgConfigHome, "Cursor")
	}
	return filepath.Join(base, "User", "globalStorage", "state.vscdb")
}
