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

func TestWebhookConfig_ValidateOK(t *testing.T) {
	tr := true
	w := WebhookConfig{
		Name:    "slack",
		URL:     "https://example.com/hook",
		Events:  []string{"waiting"},
		Agents:  []string{"claude"},
		Enabled: &tr,
	}
	if err := w.Validate(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !w.IsEnabled() {
		t.Fatal("IsEnabled should be true")
	}
}

func TestWebhookConfig_Validate_RejectsMissingFields(t *testing.T) {
	cases := []struct {
		name string
		w    WebhookConfig
	}{
		{"no name", WebhookConfig{URL: "https://x"}},
		{"no url", WebhookConfig{Name: "x"}},
		{"bad scheme", WebhookConfig{Name: "x", URL: "ftp://x"}},
		{"unparseable", WebhookConfig{Name: "x", URL: "::"}},
		{"unknown event", WebhookConfig{Name: "x", URL: "https://x", Events: []string{"nope"}}},
		{"unknown agent", WebhookConfig{Name: "x", URL: "https://x", Agents: []string{"nope"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.w.Validate(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestWebhookConfig_IsEnabled_DefaultTrue(t *testing.T) {
	w := WebhookConfig{Name: "x", URL: "https://x"}
	if !w.IsEnabled() {
		t.Fatal("absent Enabled should default to true")
	}
}

func TestConfig_ValidWebhooks_SkipsInvalid(t *testing.T) {
	cfg := Config{Webhooks: []WebhookConfig{
		{Name: "ok", URL: "https://x"},
		{Name: "bad", URL: "ftp://x"},
	}}
	got := cfg.ValidWebhooks()
	if len(got) != 1 || got[0].Name != "ok" {
		t.Fatalf("got %+v, want only 'ok'", got)
	}
}
