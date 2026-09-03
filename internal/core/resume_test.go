package core

import (
	"slices"
	"testing"
)

func TestResumeCommand(t *testing.T) {
	tests := []struct {
		agent, sessionID, want string
	}{
		{"claude", "abc-123", "claude --resume abc-123"},
		{"codex", "abc-123", "codex resume abc-123"},
		{"amp", "abc-123", "amp threads continue abc-123"},
		{"pi", "abc-123", "pi --session abc-123"},
		{"opencode", "abc-123", "opencode -s abc-123"},
		{"kilo", "abc-123", "kilo --session=abc-123"},
		{"cursor", "abc-123", `cursor-agent --resume="abc-123"`},
		{"grok", "abc-123", "grok --resume 'abc-123'"},
		{"grok", "abc $(touch /tmp/pwn) 'quoted'", `grok --resume 'abc $(touch /tmp/pwn) '\''quoted'\'''`},
		{"kimi", "abc-123", "kimi --resume abc-123"},
		{"unknown", "abc-123", ""},
		{"claude", "", ""},
		{"", "abc-123", ""},
	}
	for _, tt := range tests {
		got := ResumeCommand(tt.agent, tt.sessionID)
		if got != tt.want {
			t.Errorf("ResumeCommand(%q, %q) = %q, want %q", tt.agent, tt.sessionID, got, tt.want)
		}
	}
}

func TestYoloResumeCommand(t *testing.T) {
	tests := []struct {
		agent, sessionID, want string
	}{
		{"claude", "abc-123", "claude --dangerously-skip-permissions --resume abc-123"},
		{"codex", "abc-123", "codex --yolo resume abc-123"},
		{"amp", "abc-123", "amp --dangerously-allow-all threads continue abc-123"},
		{"pi", "abc-123", ""},
		{"opencode", "abc-123", "opencode --auto -s abc-123"},
		{"kilo", "abc-123", "kilo --auto --session=abc-123"},
		{"cursor", "abc-123", `cursor-agent --force --resume="abc-123"`},
		{"grok", "abc-123", "grok --yolo --resume 'abc-123'"},
		{"grok", "abc $(touch /tmp/pwn) 'quoted'", `grok --yolo --resume 'abc $(touch /tmp/pwn) '\''quoted'\'''`},
		{"kimi", "abc-123", "kimi --yolo --resume abc-123"},
		{"unknown", "abc-123", ""},
		{"claude", "", ""},
	}
	for _, tt := range tests {
		got := YoloResumeCommand(tt.agent, tt.sessionID)
		if got != tt.want {
			t.Errorf("YoloResumeCommand(%q, %q) = %q, want %q", tt.agent, tt.sessionID, got, tt.want)
		}
	}
}

func TestResumeArgv(t *testing.T) {
	cases := []struct {
		agent string
		want  []string
	}{
		{"claude", []string{"claude", "--resume", "abc"}},
		{"codex", []string{"codex", "resume", "abc"}},
		{"amp", []string{"amp", "threads", "continue", "abc"}},
		{"pi", []string{"pi", "--session", "abc"}},
		{"opencode", []string{"opencode", "-s", "abc"}},
		{"kilo", []string{"kilo", "--session=abc"}},
		{"cursor", []string{"cursor-agent", "--resume=abc"}},
		{"grok", []string{"grok", "--resume", "abc"}},
		{"kimi", []string{"kimi", "--resume", "abc"}},
		{"unknown", nil},
	}
	for _, c := range cases {
		if got := ResumeArgv(c.agent, "abc"); !slices.Equal(got, c.want) {
			t.Errorf("ResumeArgv(%q) = %v, want %v", c.agent, got, c.want)
		}
	}
	if got := ResumeArgv("claude", ""); got != nil {
		t.Errorf("empty session ID: want nil, got %v", got)
	}
}

func TestYoloResumeArgv(t *testing.T) {
	cases := []struct {
		agent string
		want  []string
	}{
		{"claude", []string{"claude", "--dangerously-skip-permissions", "--resume", "abc"}},
		{"codex", []string{"codex", "--yolo", "resume", "abc"}},
		{"amp", []string{"amp", "--dangerously-allow-all", "threads", "continue", "abc"}},
		{"pi", nil},
		{"opencode", []string{"opencode", "--auto", "-s", "abc"}},
		{"kilo", []string{"kilo", "--auto", "--session=abc"}},
		{"cursor", []string{"cursor-agent", "--force", "--resume=abc"}},
		{"grok", []string{"grok", "--yolo", "--resume", "abc"}},
		{"kimi", []string{"kimi", "--yolo", "--resume", "abc"}},
		{"unknown", nil},
	}
	for _, c := range cases {
		if got := YoloResumeArgv(c.agent, "abc"); !slices.Equal(got, c.want) {
			t.Errorf("YoloResumeArgv(%q) = %v, want %v", c.agent, got, c.want)
		}
	}
}
