package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds application settings shared by TUI and GUI.
type Config struct {
	WindowMinutes  int             `json:"window_minutes"`
	DefaultFilter  string          `json:"default_filter"`
	Editor         string          `json:"editor"`
	LaunchAtLogin  bool            `json:"launch_at_login"`
	Notifications  bool            `json:"notifications"`
	NotifyAfterSec int             `json:"notify_after_sec"`
	Agents         map[string]bool `json:"agents"`
	ClaudeDirs     []string        `json:"claude_dirs,omitempty"`
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
			"amp":      true,
		},
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
		_ = SaveConfig(cfg)
		return cfg
	}

	_ = json.Unmarshal(data, &cfg)

	// Backfill any missing agent keys so the file stays complete.
	defaults := DefaultConfig()
	if cfg.Agents == nil {
		cfg.Agents = defaults.Agents
	} else {
		changed := false
		for k, v := range defaults.Agents {
			if _, ok := cfg.Agents[k]; !ok {
				cfg.Agents[k] = v
				changed = true
			}
		}
		if changed {
			_ = SaveConfig(cfg)
		}
	}

	return cfg
}

// SaveConfig writes the config to disk.
func SaveConfig(cfg Config) error {
	path := ConfigPath()
	if path == "" {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}
