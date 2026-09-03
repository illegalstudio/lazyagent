package sessions

import (
	"flag"
	"fmt"
	"os"

	"github.com/illegalstudio/lazyagent/internal/core"
)

// RunLatest implements `lazyagent latest`. It resumes the most recently
// active session recorded for the current (or --dir) directory, without any
// table or prompt: same discovery and directory filter as Run, then straight
// into openSession on the newest match.
func RunLatest(args []string) int {
	fs := flag.NewFlagSet("latest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	agent := fs.String("agent", "all", "Agent to consider: claude, pi, opencode, kilo, cursor, codex, amp, grok, kimi, all")
	dirFlag := fs.String("dir", "", "Resume the latest session for this directory instead of the current one")
	yolo := fs.Bool("yolo", false, "Use the agent-specific YOLO mode")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `lazyagent latest — resume the most recent session for a directory

Finds the most recently active session whose working directory is the
current directory (or --dir) or a subdirectory of it, across all agents,
and resumes it with the originating agent's CLI. Use "lazyagent sessions"
to see the sessions it picks from.

Usage:
  lazyagent latest
  lazyagent latest --agent claude
  lazyagent latest --dir ~/projects/foo
  lazyagent latest --yolo

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if !validAgents[*agent] {
		fmt.Fprintf(os.Stderr, "Error: unknown --agent value %q (use claude, pi, opencode, kilo, cursor, codex, amp, grok, kimi, or all)\n", *agent)
		return 2
	}

	dir, err := resolveTargetDir(*dirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}

	filtered, code := discoverDirSessions(*agent, dir)
	if code != 0 {
		return code
	}

	// Unlike the listing commands, an empty result here is a failure: the
	// whole point was to open a session, and there is none to open.
	if len(filtered) == 0 {
		fmt.Fprintf(os.Stderr, "No sessions found in %s.\n", stripControl(abbreviateHome(dir)))
		return 1
	}

	mode := core.ResumeNormal
	if *yolo {
		mode = core.ResumeYolo
	}
	return openSession(filtered[0], mode)
}
