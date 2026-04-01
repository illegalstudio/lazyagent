package core

import "fmt"

// ResumeCommand returns the CLI command to resume a session for the given agent.
// Returns empty string for unknown agents or empty session IDs.
func ResumeCommand(agent, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	switch agent {
	case "claude":
		return fmt.Sprintf("claude --resume %s", sessionID)
	case "codex":
		return fmt.Sprintf("codex resume %s", sessionID)
	case "amp":
		return fmt.Sprintf("amp threads continue %s", sessionID)
	case "pi":
		return fmt.Sprintf("pi --session %s", sessionID)
	case "opencode":
		return fmt.Sprintf("opencode -s %s", sessionID)
	case "cursor":
		return fmt.Sprintf("cursor-agent --resume=%q", sessionID)
	default:
		return ""
	}
}
