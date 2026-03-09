package core

import (
	"time"

	"github.com/nahime0/lazyagent/internal/claude"
)

// LiveProvider discovers real Claude Code sessions from disk.
type LiveProvider struct{}

func (LiveProvider) DiscoverSessions() ([]*claude.Session, error) {
	return claude.DiscoverSessions()
}

func (LiveProvider) UseWatcher() bool               { return true }
func (LiveProvider) RefreshInterval() time.Duration { return 0 }
