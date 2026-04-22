package prune

import "github.com/illegalstudio/lazyagent/internal/chatops"

// pruneAgents is the catalog of agents that the prune selector exposes.
// Only agents with plain-text file storage are listed — deleting SQLite
// rows or re-syncable Amp threads is not in scope here.
var pruneAgents = []chatops.Agent{
	{Key: "claude", Label: "Claude Code", Color: "#E7A15E"},
	{Key: "pi", Label: "pi coding agent", Color: "#F38BA8"},
	{Key: "codex", Label: "Codex CLI", Color: "#A6E3A1"},
}

func pickAgents() ([]string, error) {
	return chatops.PickAgents(
		pruneAgents,
		"Select agents to prune",
		"Only agents with plain-text file storage are shown.",
	)
}
