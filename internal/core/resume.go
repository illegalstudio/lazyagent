package core

import (
	"fmt"
	"strings"
)

// ResumeMode selects the permission mode used when reopening a session.
type ResumeMode uint8

const (
	ResumeNormal ResumeMode = iota
	ResumeYolo
)

// ResumeCommand returns the CLI command to resume a session for the given agent.
// Returns empty string for unknown agents or empty session IDs.
func ResumeCommand(agent, sessionID string) string {
	return ResumeCommandWithMode(agent, sessionID, ResumeNormal)
}

// YoloResumeCommand returns the command that resumes a session with the
// agent-specific permissive mode enabled. It returns an empty string when
// the agent has no distinct YOLO mode.
func YoloResumeCommand(agent, sessionID string) string {
	return ResumeCommandWithMode(agent, sessionID, ResumeYolo)
}

// ResumeCommandWithMode returns the display command for a resume mode.
func ResumeCommandWithMode(agent, sessionID string, mode ResumeMode) string {
	if sessionID == "" {
		return ""
	}
	switch agent {
	case "claude":
		if mode == ResumeYolo {
			return fmt.Sprintf("claude --dangerously-skip-permissions --resume %s", sessionID)
		}
		return fmt.Sprintf("claude --resume %s", sessionID)
	case "codex":
		if mode == ResumeYolo {
			return fmt.Sprintf("codex --yolo resume %s", sessionID)
		}
		return fmt.Sprintf("codex resume %s", sessionID)
	case "amp":
		if mode == ResumeYolo {
			return fmt.Sprintf("amp --dangerously-allow-all threads continue %s", sessionID)
		}
		return fmt.Sprintf("amp threads continue %s", sessionID)
	case "pi":
		if mode == ResumeYolo {
			return ""
		}
		return fmt.Sprintf("pi --session %s", sessionID)
	case "opencode":
		if mode == ResumeYolo {
			return fmt.Sprintf("opencode --auto -s %s", sessionID)
		}
		return fmt.Sprintf("opencode -s %s", sessionID)
	case "kilo":
		if mode == ResumeYolo {
			return fmt.Sprintf("kilo --auto --session=%s", sessionID)
		}
		return fmt.Sprintf("kilo --session=%s", sessionID)
	case "cursor":
		if mode == ResumeYolo {
			return fmt.Sprintf("cursor-agent --force --resume=%q", sessionID)
		}
		return fmt.Sprintf("cursor-agent --resume=%q", sessionID)
	case "grok":
		if mode == ResumeYolo {
			return fmt.Sprintf("grok --yolo --resume %s", shellQuoteArg(sessionID))
		}
		return fmt.Sprintf("grok --resume %s", shellQuoteArg(sessionID))
	case "kimi":
		if mode == ResumeYolo {
			return fmt.Sprintf("kimi --yolo --resume %s", sessionID)
		}
		return fmt.Sprintf("kimi --resume %s", sessionID)
	default:
		return ""
	}
}

// shellQuoteArg returns one POSIX shell word using single-quote escaping.
func shellQuoteArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// ResumeArgv returns the executable argv to resume a session, or nil when the
// agent is unknown.
func ResumeArgv(agent, sessionID string) []string {
	return ResumeArgvWithMode(agent, sessionID, ResumeNormal)
}

// YoloResumeArgv returns the executable argv for the agent-specific YOLO
// resume mode. It returns nil when the agent has no distinct YOLO mode.
func YoloResumeArgv(agent, sessionID string) []string {
	return ResumeArgvWithMode(agent, sessionID, ResumeYolo)
}

// ResumeArgvWithMode returns the executable argv for a resume mode.
func ResumeArgvWithMode(agent, sessionID string, mode ResumeMode) []string {
	if sessionID == "" {
		return nil
	}
	switch agent {
	case "claude":
		if mode == ResumeYolo {
			return []string{"claude", "--dangerously-skip-permissions", "--resume", sessionID}
		}
		return []string{"claude", "--resume", sessionID}
	case "codex":
		if mode == ResumeYolo {
			return []string{"codex", "--yolo", "resume", sessionID}
		}
		return []string{"codex", "resume", sessionID}
	case "amp":
		if mode == ResumeYolo {
			return []string{"amp", "--dangerously-allow-all", "threads", "continue", sessionID}
		}
		return []string{"amp", "threads", "continue", sessionID}
	case "pi":
		if mode == ResumeYolo {
			return nil
		}
		return []string{"pi", "--session", sessionID}
	case "opencode":
		if mode == ResumeYolo {
			return []string{"opencode", "--auto", "-s", sessionID}
		}
		return []string{"opencode", "-s", sessionID}
	case "kilo":
		if mode == ResumeYolo {
			return []string{"kilo", "--auto", "--session=" + sessionID}
		}
		return []string{"kilo", "--session=" + sessionID}
	case "cursor":
		if mode == ResumeYolo {
			return []string{"cursor-agent", "--force", "--resume=" + sessionID}
		}
		return []string{"cursor-agent", "--resume=" + sessionID}
	case "grok":
		if mode == ResumeYolo {
			return []string{"grok", "--yolo", "--resume", sessionID}
		}
		return []string{"grok", "--resume", sessionID}
	case "kimi":
		if mode == ResumeYolo {
			return []string{"kimi", "--yolo", "--resume", sessionID}
		}
		return []string{"kimi", "--resume", sessionID}
	default:
		return nil
	}
}
