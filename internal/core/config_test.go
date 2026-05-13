package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig_ExcludeCWDSubstrings(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ExcludeCWDSubstrings == nil {
		t.Fatal("DefaultConfig().ExcludeCWDSubstrings should not be nil")
	}
	if len(cfg.ExcludeCWDSubstrings) != 0 {
		t.Errorf("DefaultConfig().ExcludeCWDSubstrings = %v, want empty slice", cfg.ExcludeCWDSubstrings)
	}
}

func TestLoadConfig_BackfillsExcludeCWDSubstrings(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Write a config file without ExcludeCWDSubstrings.
	cfgDir := filepath.Join(tmpDir, "lazyagent")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	minimalCfg := map[string]interface{}{
		"window_minutes": 30,
		"agents":         map[string]bool{"claude": true},
	}
	data, _ := json.Marshal(minimalCfg)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig()
	if cfg.ExcludeCWDSubstrings == nil {
		t.Fatal("LoadConfig() did not backfill ExcludeCWDSubstrings")
	}
	if len(cfg.ExcludeCWDSubstrings) != 0 {
		t.Errorf("LoadConfig().ExcludeCWDSubstrings = %v, want []", cfg.ExcludeCWDSubstrings)
	}
}

func TestLoadConfig_PreservesExistingExcludeCWDSubstrings(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Write a config file with custom ExcludeCWDSubstrings.
	cfgDir := filepath.Join(tmpDir, "lazyagent")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	customCfg := map[string]interface{}{
		"window_minutes":         30,
		"agents":                 map[string]bool{"claude": true},
		"exclude_cwd_substrings": []string{"/custom/path", "/another"},
	}
	data, _ := json.Marshal(customCfg)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig()
	if len(cfg.ExcludeCWDSubstrings) != 2 {
		t.Fatalf("LoadConfig() ExcludeCWDSubstrings length = %d, want 2", len(cfg.ExcludeCWDSubstrings))
	}
	if cfg.ExcludeCWDSubstrings[0] != "/custom/path" || cfg.ExcludeCWDSubstrings[1] != "/another" {
		t.Errorf("LoadConfig().ExcludeCWDSubstrings = %v, want [/custom/path /another]", cfg.ExcludeCWDSubstrings)
	}
}
