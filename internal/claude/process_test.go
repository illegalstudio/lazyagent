package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeProjectsDirs_WithConfigDirs(t *testing.T) {
	dirs := ClaudeProjectsDirs([]string{"/custom/claude", "/other/path"})
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d", len(dirs))
	}
	if dirs[0] != "/custom/claude/projects" {
		t.Errorf("dirs[0] = %q, want /custom/claude/projects", dirs[0])
	}
	if dirs[1] != "/other/path/projects" {
		t.Errorf("dirs[1] = %q, want /other/path/projects", dirs[1])
	}
}

func TestClaudeProjectsDirs_DefaultFallback(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	dirs := ClaudeProjectsDirs(nil)
	if len(dirs) != 1 {
		t.Fatalf("expected 1 dir, got %d", len(dirs))
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".claude", "projects")
	if dirs[0] != want {
		t.Errorf("dirs[0] = %q, want %q", dirs[0], want)
	}
}

func TestClaudeProjectsDirs_EnvVarCustom(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/custom-claude")
	dirs := ClaudeProjectsDirs(nil)
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d", len(dirs))
	}
	if dirs[0] != "/tmp/custom-claude/projects" {
		t.Errorf("dirs[0] = %q, want /tmp/custom-claude/projects", dirs[0])
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".claude", "projects")
	if dirs[1] != want {
		t.Errorf("dirs[1] = %q, want %q", dirs[1], want)
	}
}

func TestClaudeProjectsDirs_EnvVarSameAsDefault(t *testing.T) {
	home, _ := os.UserHomeDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	dirs := ClaudeProjectsDirs(nil)
	if len(dirs) != 1 {
		t.Fatalf("expected 1 dir (deduplicated), got %d", len(dirs))
	}
	want := filepath.Join(home, ".claude", "projects")
	if dirs[0] != want {
		t.Errorf("dirs[0] = %q, want %q", dirs[0], want)
	}
}

func TestClaudeProjectsDirs_ConfigDirsOverridesEnv(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/should/be/ignored")
	dirs := ClaudeProjectsDirs([]string{"/explicit/dir"})
	if len(dirs) != 1 {
		t.Fatalf("expected 1 dir, got %d", len(dirs))
	}
	if dirs[0] != "/explicit/dir/projects" {
		t.Errorf("dirs[0] = %q, want /explicit/dir/projects", dirs[0])
	}
}
