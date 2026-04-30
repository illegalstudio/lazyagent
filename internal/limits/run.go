// Package limits implements the `lazyagent limits` subcommand: a one-shot
// snapshot of the user's Claude Code and Codex rate-limit windows (5-hour
// and weekly), plus a "pace" indicator that compares actual consumption to
// a perfectly linear consumption rate.
//
// IMPORTANT (Claude): the source for Claude is /api/oauth/usage on
// api.anthropic.com — the same endpoint Claude Code's own `/status` calls.
// As of this writing Anthropic does not document it publicly. lazyagent
// queries it on explicit user invocation only (no polling). Behavior may
// change without notice; failures degrade gracefully.
//
// Codex limits are read from the latest session rollout under
// ~/.codex/sessions, where Codex itself persists the server's rate_limits
// response after each turn. No network call is made for Codex.
package limits

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

type options struct {
	agent string
}

// Run is the entry point invoked by main.go for `lazyagent limits ...`.
func Run(args []string) int {
	fs := flag.NewFlagSet("limits", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var opts options
	fs.StringVar(&opts.agent, "agent", "all", "Which agent to query: claude, codex, all")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `lazyagent limits — show 5-hour and weekly rate-limit usage

Usage:
  lazyagent limits                  Show limits for both Claude Code and Codex
  lazyagent limits --agent claude   Show only Claude Code limits
  lazyagent limits --agent codex    Show only Codex limits

Output explains:
  - Used %:    how much of the window has been consumed
  - Elapsed %: how far we are into the window's time
  - Pace:      consumption vs. a perfectly linear pace
                 underutilizing  (used < 0.85 × elapsed)
                 on track        (0.85 ≤ used/elapsed ≤ 1.15)
                 overutilizing   (used > 1.15 × elapsed)

Authentication:
  Claude  reads its OAuth token from, in order:
            1. CLAUDE_CODE_OAUTH_TOKEN env var
            2. macOS Keychain (service "Claude Code-credentials")
            3. ~/.claude/.credentials.json
          If none is found, run ` + "`claude`" + ` to log in.
  Codex   reads ~/.codex/sessions/<date>/rollout-*.jsonl (no network call).

Disclaimer (Claude):
  /api/oauth/usage is an undocumented Anthropic endpoint used by Claude Code
  itself. lazyagent calls it only on explicit user invocation. It may break or
  be revoked by Anthropic without notice.

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

	agents, err := resolveAgents(opts.agent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintln(os.Stderr, "Run `lazyagent limits --help` for usage.")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	now := time.Now()
	var out strings.Builder
	exitCode := 0
	for i, a := range agents {
		if i > 0 {
			out.WriteString("\n")
		}
		report, err := fetchReport(ctx, a)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error (%s): %v\n", a, err)
			exitCode = 1
			continue
		}
		renderReport(&out, report, now)
	}

	fmt.Print(out.String())
	return exitCode
}

func fetchReport(ctx context.Context, agent string) (Report, error) {
	switch agent {
	case "claude":
		return fetchClaudeReport(ctx)
	case "codex":
		return fetchCodexReport()
	default:
		return Report{}, fmt.Errorf("unsupported agent %q", agent)
	}
}

func resolveAgents(arg string) ([]string, error) {
	arg = strings.TrimSpace(strings.ToLower(arg))
	switch arg {
	case "", "all":
		return []string{"claude", "codex"}, nil
	case "claude", "codex":
		return []string{arg}, nil
	default:
		return nil, fmt.Errorf("unsupported agent %q (use claude, codex, or all)", arg)
	}
}
