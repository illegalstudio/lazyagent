package core

import "testing"

func TestToolActivity_ClaudeToolNames(t *testing.T) {
	tests := []struct {
		tool string
		want ActivityKind
	}{
		{"Read", ActivityReading},
		{"Write", ActivityWriting},
		{"Edit", ActivityWriting},
		{"NotebookEdit", ActivityWriting},
		{"Bash", ActivityRunning},
		{"Glob", ActivitySearching},
		{"Grep", ActivitySearching},
		{"WebFetch", ActivityBrowsing},
		{"WebSearch", ActivityBrowsing},
		{"Agent", ActivitySpawning},
		{"UnknownTool", ActivityRunning},
		{"", ActivityIdle},
	}
	for _, tt := range tests {
		got := ToolActivity(tt.tool)
		if got != tt.want {
			t.Errorf("ToolActivity(%q) = %q, want %q", tt.tool, got, tt.want)
		}
	}
}

func TestToolActivity_PiToolNames(t *testing.T) {
	tests := []struct {
		tool string
		want ActivityKind
	}{
		{"read", ActivityReading},
		{"write", ActivityWriting},
		{"edit", ActivityWriting},
		{"bash", ActivityRunning},
		{"process", ActivityRunning},
		{"find", ActivitySearching},
		{"lsp", ActivitySearching},
		{"web_search", ActivityBrowsing},
		{"subagent", ActivitySpawning},
	}
	for _, tt := range tests {
		got := ToolActivity(tt.tool)
		if got != tt.want {
			t.Errorf("ToolActivity(%q) = %q, want %q", tt.tool, got, tt.want)
		}
	}
}
