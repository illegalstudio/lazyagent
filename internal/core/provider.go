package core

import (
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/claude"
	"github.com/illegalstudio/lazyagent/internal/cursor"
	"github.com/illegalstudio/lazyagent/internal/model"
	"github.com/illegalstudio/lazyagent/internal/opencode"
	"github.com/illegalstudio/lazyagent/internal/pi"
)

// LiveProvider discovers real Claude Code sessions from disk.
type LiveProvider struct {
	cache        *model.SessionCache
	desktopCache *claude.DesktopCache
	claudeDirs   []string
}

// NewLiveProvider creates a LiveProvider with mtime-based caches.
// claudeDirs configures which Claude base directories to scan (e.g. ~/.claude).
// When empty, auto-detection via CLAUDE_CONFIG_DIR / ~/.claude is used.
func NewLiveProvider(claudeDirs []string) *LiveProvider {
	return &LiveProvider{
		cache:        model.NewSessionCache(),
		desktopCache: claude.NewDesktopCache(),
		claudeDirs:   claudeDirs,
	}
}

func (p *LiveProvider) DiscoverSessions() ([]*model.Session, error) {
	return claude.DiscoverSessions(p.cache, p.desktopCache, p.claudeDirs)
}

func (p *LiveProvider) UseWatcher() bool               { return true }
func (p *LiveProvider) RefreshInterval() time.Duration { return 0 }
func (p *LiveProvider) WatchDirs() []string {
	dirs := claude.ClaudeProjectsDirs(p.claudeDirs)
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

// OpenCodeProvider discovers OpenCode sessions from SQLite.
type OpenCodeProvider struct {
	cache *opencode.SessionCache
}

// NewOpenCodeProvider creates an OpenCodeProvider.
func NewOpenCodeProvider() *OpenCodeProvider {
	return &OpenCodeProvider{cache: opencode.NewSessionCache()}
}

func (p *OpenCodeProvider) DiscoverSessions() ([]*model.Session, error) {
	return opencode.DiscoverSessions(p.cache)
}

func (p *OpenCodeProvider) UseWatcher() bool               { return false }
func (p *OpenCodeProvider) RefreshInterval() time.Duration { return 3 * time.Second }
func (p *OpenCodeProvider) WatchDirs() []string {
	if d := opencode.OpenCodeDataDir(); d != "" {
		return []string{d}
	}
	return nil
}

// CursorProvider discovers Cursor sessions from store.db files.
type CursorProvider struct {
	cache *cursor.SessionCache
}

// NewCursorProvider creates a CursorProvider.
func NewCursorProvider() *CursorProvider {
	return &CursorProvider{cache: cursor.NewSessionCache()}
}

func (p *CursorProvider) DiscoverSessions() ([]*model.Session, error) {
	return cursor.DiscoverSessions(p.cache)
}

func (p *CursorProvider) UseWatcher() bool               { return false }
func (p *CursorProvider) RefreshInterval() time.Duration { return 3 * time.Second }
func (p *CursorProvider) WatchDirs() []string {
	if d := cursor.StateDBDir(); d != "" {
		return []string{d}
	}
	return nil
}

// BuildProvider creates a SessionProvider based on agent mode and config.
// When agentMode is "all", it reads the agents config to decide which providers
// to include. A specific agentMode (e.g. "claude") overrides the config.
func BuildProvider(agentMode string, cfg Config) SessionProvider {
	switch agentMode {
	case "claude":
		return NewLiveProvider(cfg.ClaudeDirs)
	case "pi":
		return NewPiProvider()
	case "opencode":
		return NewOpenCodeProvider()
	case "cursor":
		return NewCursorProvider()
	default: // "all"
		var providers []SessionProvider
		if cfg.AgentEnabled("claude") {
			providers = append(providers, NewLiveProvider(cfg.ClaudeDirs))
		}
		if cfg.AgentEnabled("pi") {
			providers = append(providers, NewPiProvider())
		}
		if cfg.AgentEnabled("opencode") {
			providers = append(providers, NewOpenCodeProvider())
		}
		if cfg.AgentEnabled("cursor") {
			providers = append(providers, NewCursorProvider())
		}
		if len(providers) == 0 {
			// All disabled — return a no-op provider.
			return MultiProvider{}
		}
		if len(providers) == 1 {
			return providers[0]
		}
		return MultiProvider{Providers: providers}
	}
}

// MultiProvider merges sessions from multiple providers.
type MultiProvider struct {
	Providers []SessionProvider
}

func (m MultiProvider) DiscoverSessions() ([]*model.Session, error) {
	if len(m.Providers) <= 1 {
		// Fast path: no need for goroutines.
		for _, p := range m.Providers {
			return p.DiscoverSessions()
		}
		return nil, nil
	}

	type result struct {
		sessions []*model.Session
	}
	results := make([]result, len(m.Providers))
	var wg sync.WaitGroup
	for i, p := range m.Providers {
		wg.Add(1)
		go func(idx int, prov SessionProvider) {
			defer wg.Done()
			sessions, err := prov.DiscoverSessions()
			if err == nil {
				results[idx] = result{sessions: sessions}
			}
		}(i, p)
	}
	wg.Wait()

	var all []*model.Session
	for _, r := range results {
		all = append(all, r.sessions...)
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
