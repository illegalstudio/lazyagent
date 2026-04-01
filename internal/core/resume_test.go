package core

import "testing"

func TestResumeCommand(t *testing.T) {
	tests := []struct {
		agent, sessionID, want string
	}{
		{"claude", "abc-123", "claude --resume abc-123"},
		{"codex", "abc-123", "codex resume abc-123"},
		{"amp", "abc-123", "amp threads continue abc-123"},
		{"pi", "abc-123", "pi --session abc-123"},
		{"opencode", "abc-123", "opencode -s abc-123"},
		{"cursor", "abc-123", `cursor-agent --resume="abc-123"`},
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
