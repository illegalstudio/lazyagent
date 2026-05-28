package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/apiauth"
)

// TUIConfig holds TUI-specific settings.
type TUIConfig struct {
	Theme string `json:"theme"` // "dark" (default) or "light"
}

// Config holds application settings shared by TUI and GUI.
type Config struct {
	WindowMinutes        int             `json:"window_minutes"`
	DefaultFilter        string          `json:"default_filter"`
	Editor               string          `json:"editor"`
	LaunchAtLogin        bool            `json:"launch_at_login"`
	Notifications        bool            `json:"notifications"`
	NotifyAfterSec       int             `json:"notify_after_sec"`
	Agents               map[string]bool `json:"agents"`
	ClaudeDirs           []string        `json:"claude_dirs,omitempty"`
	ExcludeCWDSubstrings []string        `json:"exclude_cwd_substrings"`
	TUI                  TUIConfig       `json:"tui"`
	// APIPassphrase is the secret used to derive the bearer token that
	// protects the HTTP API. Empty means the API has not been configured yet
	// — `lazyagent --api` will prompt for one on first run.
	APIPassphrase string `json:"api_passphrase,omitempty"`
	// APISalt is a public, per-install salt used with APIPassphrase when
	// deriving the bearer token. It is not secret, but must stay stable so
	// clients can derive the same token from the same passphrase.
	APISalt string `json:"api_salt,omitempty"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		WindowMinutes:  30,
		DefaultFilter:  "",
		Editor:         "",
		LaunchAtLogin:  false,
		Notifications:  false,
		NotifyAfterSec: 30,
		Agents: map[string]bool{
			"claude":   true,
			"pi":       true,
			"opencode": true,
			"cursor":   true,
			"codex":    true,
			"amp":      true,
			"grok":     true,
			"kilo":     true,
			"kimi":     true,
		},
		ExcludeCWDSubstrings: []string{},
		TUI:                  TUIConfig{Theme: "dark"},
	}
}

// AgentEnabled returns whether an agent is enabled in config.
// Defaults to true if the agent key is missing from the map.
func (c Config) AgentEnabled(name string) bool {
	if c.Agents == nil {
		return true
	}
	enabled, ok := c.Agents[name]
	if !ok {
		return true
	}
	return enabled
}

// ConfigDir returns the config directory path (~/.config/lazyagent).
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "lazyagent")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "lazyagent")
}

// ConfigPath returns the full path to the config file.
func ConfigPath() string {
	dir := ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.json")
}

// LoadConfig reads the config file, creating it with defaults if missing.
func LoadConfig() Config {
	cfg := DefaultConfig()
	path := ConfigPath()
	if path == "" {
		return cfg
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist — create it with defaults.
		ensureAPISalt(&cfg)
		_ = SaveConfig(cfg)
		return cfg
	}

	_ = json.Unmarshal(data, &cfg)

	// Backfill any missing generated/config keys so the file stays complete.
	changed := ensureAPISalt(&cfg)
	defaults := DefaultConfig()
	if cfg.Agents == nil {
		cfg.Agents = defaults.Agents
		changed = true
	} else {
		for k, v := range defaults.Agents {
			if _, ok := cfg.Agents[k]; !ok {
				cfg.Agents[k] = v
				changed = true
			}
		}
	}
	if cfg.ExcludeCWDSubstrings == nil {
		cfg.ExcludeCWDSubstrings = defaults.ExcludeCWDSubstrings
		changed = true
	}
	if changed {
		_ = SaveConfig(cfg)
	}

	return cfg
}

// EnsureAPISalt backfills the public per-install API salt when missing.
// It returns true when cfg was changed.
func EnsureAPISalt(cfg *Config) bool {
	salt := strings.TrimSpace(cfg.APISalt)
	if salt != "" {
		if salt == cfg.APISalt {
			return false
		}
		cfg.APISalt = salt
		return true
	}
	generated, err := apiauth.NewSalt()
	if err != nil {
		// The salt is public and only needs uniqueness, not secrecy. crypto/rand
		// failure is extremely unlikely; this fallback avoids silently reverting
		// to the global legacy salt in constrained environments.
		generated = fmt.Sprintf("%s-%d-%d", apiauth.SaltPrefix, os.Getpid(), time.Now().UnixNano())
	}
	cfg.APISalt = generated
	return true
}

func ensureAPISalt(cfg *Config) bool {
	return EnsureAPISalt(cfg)
}

// SaveConfig writes the config to disk. The file is created with mode 0o600
// because it carries the API passphrase: anyone who can read it can derive
// the bearer token. Existing files are chmod'ed back to 0o600 on every save
// so that historical files (originally 0o644) get tightened on first write
// after upgrading.
func SaveConfig(cfg Config) error {
	path := ConfigPath()
	if path == "" {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	// os.WriteFile only honors the mode at file creation; force-tighten any
	// pre-existing file that may have been created before this change.
	return os.Chmod(path, 0o600)
}
