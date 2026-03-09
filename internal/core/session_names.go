package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionNames manages user-defined aliases for sessions.
// Stored in ~/.config/lazyagent/session-names.json.
type SessionNames struct {
	mu      sync.RWMutex
	names   map[string]string // sessionID → custom name
	modTime time.Time         // last known modification time of the file
}

// NewSessionNames creates a new SessionNames and loads from disk.
func NewSessionNames() *SessionNames {
	sn := &SessionNames{names: make(map[string]string)}
	sn.loadLocked()
	return sn
}

func sessionNamesPath() string {
	dir := ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "session-names.json")
}

// loadLocked reads the file from disk. Must be called with mu held or during init.
func (sn *SessionNames) loadLocked() {
	path := sessionNamesPath()
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	sn.modTime = info.ModTime()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &sn.names)
}

func (sn *SessionNames) save() error {
	path := sessionNamesPath()
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sn.names, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(path, data, 0o644)
	if err == nil {
		// Update modTime so we don't re-read our own write.
		if info, e := os.Stat(path); e == nil {
			sn.modTime = info.ModTime()
		}
	}
	return err
}

// Get returns the custom name for a session, or empty string.
func (sn *SessionNames) Get(sessionID string) string {
	sn.mu.RLock()
	defer sn.mu.RUnlock()
	return sn.names[sessionID]
}

// Set stores a custom name for a session and persists to disk.
// Empty name removes the alias.
func (sn *SessionNames) Set(sessionID, name string) error {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	if name == "" {
		delete(sn.names, sessionID)
	} else {
		sn.names[sessionID] = name
	}
	return sn.save()
}

// Refresh re-reads the file from disk if it was modified externally.
// Returns true if the file was actually reloaded.
func (sn *SessionNames) Refresh() bool {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	path := sessionNamesPath()
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !info.ModTime().After(sn.modTime) {
		return false
	}
	sn.modTime = info.ModTime()
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	sn.names = make(map[string]string)
	_ = json.Unmarshal(data, &sn.names)
	return true
}
