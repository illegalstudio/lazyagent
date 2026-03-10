package core

import (
	"time"

	"github.com/nahime0/lazyagent/internal/claude"
	"github.com/nahime0/lazyagent/internal/model"
	"github.com/nahime0/lazyagent/internal/pi"
)

// LiveProvider discovers real Claude Code sessions from disk.
type LiveProvider struct {
	cache        *model.SessionCache
	desktopCache *claude.DesktopCache
}

// NewLiveProvider creates a LiveProvider with mtime-based caches.
func NewLiveProvider() *LiveProvider {
	return &LiveProvider{
		cache:        model.NewSessionCache(),
		desktopCache: claude.NewDesktopCache(),
	}
}

func (p *LiveProvider) DiscoverSessions() ([]*model.Session, error) {
	return claude.DiscoverSessions(p.cache, p.desktopCache)
}

func (p *LiveProvider) UseWatcher() bool               { return true }
func (p *LiveProvider) RefreshInterval() time.Duration { return 0 }
func (p *LiveProvider) WatchDirs() []string {
	dirs := []string{claude.ClaudeProjectsDir()}
	if d := claude.DesktopSessionsDir(); d != "" {
		dirs = append(dirs, d)
	}
	return dirs
}

// PiProvider discovers pi coding agent sessions from disk.
type PiProvider struct {
	cache *model.SessionCache
}

// NewPiProvider creates a PiProvider with an mtime-based cache.
func NewPiProvider() *PiProvider {
	return &PiProvider{cache: model.NewSessionCache()}
}

func (p *PiProvider) DiscoverSessions() ([]*model.Session, error) {
	return pi.DiscoverSessions(p.cache)
}

func (p *PiProvider) UseWatcher() bool               { return true }
func (p *PiProvider) RefreshInterval() time.Duration { return 0 }
func (p *PiProvider) WatchDirs() []string            { return []string{pi.PiSessionsDir()} }

// MultiProvider merges sessions from multiple providers.
type MultiProvider struct {
	Providers []SessionProvider
}

func (m MultiProvider) DiscoverSessions() ([]*model.Session, error) {
	var all []*model.Session
	for _, p := range m.Providers {
		sessions, err := p.DiscoverSessions()
		if err != nil {
			continue // One provider failing shouldn't block others
		}
		all = append(all, sessions...)
	}
	return all, nil
}

func (m MultiProvider) UseWatcher() bool {
	for _, p := range m.Providers {
		if p.UseWatcher() {
			return true
		}
	}
	return false
}

func (m MultiProvider) RefreshInterval() time.Duration {
	var min time.Duration
	for _, p := range m.Providers {
		d := p.RefreshInterval()
		if d > 0 && (min == 0 || d < min) {
			min = d
		}
	}
	return min
}

func (m MultiProvider) WatchDirs() []string {
	var dirs []string
	for _, p := range m.Providers {
		dirs = append(dirs, p.WatchDirs()...)
	}
	return dirs
}
