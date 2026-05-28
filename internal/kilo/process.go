package kilo

import (
	"github.com/illegalstudio/lazyagent/internal/model"
	"github.com/illegalstudio/lazyagent/internal/opencodefamily"
)

var source = opencodefamily.Source{
	Agent:      "kilo",
	EnvVar:     "KILO_DATA_DIR",
	DataSubdir: "kilo",
	DBFile:     "kilo.db",
}

// Compatibility contract: Kilo 7.x stores CLI sessions in the same SQLite
// family as OpenCode (`session`, `message`, `part`, JSON `data` payloads).
// If Kilo diverges, update opencodefamily tests before changing this wrapper.

// SessionCache caches Kilo sessions keyed by session ID.
type SessionCache = opencodefamily.SessionCache

// NewSessionCache creates an empty Kilo session cache.
func NewSessionCache() *SessionCache {
	return opencodefamily.NewSessionCache()
}

// KiloDataDir returns the path to the Kilo data directory.
func KiloDataDir() string {
	return opencodefamily.DataDirFor(source)
}

// DBPath returns the path to the Kilo SQLite database.
func DBPath() string {
	return opencodefamily.DBPathFor(source)
}

// DiscoverSessions reads the Kilo SQLite database and returns sessions.
func DiscoverSessions(cache *SessionCache) ([]*model.Session, error) {
	return opencodefamily.DiscoverSessionsFor(source, cache)
}
