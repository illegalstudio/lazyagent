package opencode

import (
	"github.com/illegalstudio/lazyagent/internal/model"
	"github.com/illegalstudio/lazyagent/internal/opencodefamily"
)

var source = opencodefamily.Source{
	Agent:      "opencode",
	EnvVar:     "OPENCODE_DATA_DIR",
	DataSubdir: "opencode",
	DBFile:     "opencode.db",
}

// Compatibility contract: OpenCode and Kilo currently persist sessions using
// the same relational SQLite shape (`session`, `message`, `part`, JSON `data`
// payloads). Keep this package as a thin source declaration; shared parsing
// belongs in opencodefamily so schema drift is tested in one place.

// SessionCache caches OpenCode sessions keyed by session ID.
type SessionCache = opencodefamily.SessionCache

// NewSessionCache creates an empty OpenCode session cache.
func NewSessionCache() *SessionCache {
	return opencodefamily.NewSessionCache()
}

// OpenCodeDataDir returns the path to the OpenCode data directory.
func OpenCodeDataDir() string {
	return opencodefamily.DataDirFor(source)
}

// DBPath returns the path to the OpenCode SQLite database.
func DBPath() string {
	return opencodefamily.DBPathFor(source)
}

// DiscoverSessions reads the OpenCode SQLite database and returns sessions.
func DiscoverSessions(cache *SessionCache) ([]*model.Session, error) {
	return opencodefamily.DiscoverSessionsFor(source, cache)
}
