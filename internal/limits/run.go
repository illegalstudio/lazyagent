// Package limits implements the `lazyagent limits` subcommand: a one-shot
// snapshot of the user's Claude Code, Codex, and Grok rate-limit / billing
// windows, plus a "pace" indicator that compares actual consumption to
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
//
// IMPORTANT (Grok): the source for Grok is /v1/billing on
// cli-chat-proxy.grok.com — the same endpoint the Grok CLI's `/usage show`
// slash command calls. As of this writing xAI does not document it publicly.
// Same caveats as Claude: on-demand only, fail gracefully.
package limits

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// errAgentNotInstalled is returned by an agent's fetcher when there's no sign of
// the agent on this machine (no OAuth token / no session data). It's distinct from
// real errors (network failure, malformed response, expired token, …) so the
// dispatcher can quietly skip missing agents in `--agent all` mode while still
// surfacing them as helpful messages when the user explicitly asked for one.
var errAgentNotInstalled = errors.New("agent not installed")

type options struct {
	agent string
}

// Run is the entry point invoked by main.go for `lazyagent limits ...`.
func Run(args []string) int {
	fs := flag.NewFlagSet("limits", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var opts options
	fs.StringVar(&opts.agent, "agent", "all", "Which agent to query: claude, codex, grok, all")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `lazyagent limits — show rate-limit usage

Usage:
  lazyagent limits                  Show limits for Claude Code, Codex, and Grok
  lazyagent limits --agent claude   Show only Claude Code limits
  lazyagent limits --agent codex    Show only Codex limits
  lazyagent limits --agent grok     Show only Grok limits

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
          If none is found, run `+"`claude`"+` to log in.
  Codex   reads ~/.codex/sessions/<date>/rollout-*.jsonl (no network call).
  Grok    reads its OAuth token from, in order:
            1. GROK_OAUTH_TOKEN env var
            2. ~/.grok/auth.json
          If none is found, run `+"`grok login`"+`.

Disclaimer (Claude, Grok):
  Both providers expose their usage through undocumented endpoints used by
  their respective official CLIs. lazyagent calls them only on explicit user
  invocation. They may break or be revoked by their vendors without notice.

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
	printed := 0
	missing := 0
	explicit := len(agents) == 1
	for _, a := range agents {
		report, err := fetchReport(ctx, a)
		if err != nil {
			if errors.Is(err, errAgentNotInstalled) {
				missing++
				if explicit {
					fmt.Fprintln(os.Stderr, notInstalledMessage(a))
					exitCode = 1
				}
				// In `all` mode, silently skip — we'll print a single combined
				// message at the end if nothing was shown.
				continue
			}
			fmt.Fprintf(os.Stderr, "Error (%s): %v\n", a, err)
			exitCode = 1
			continue
		}
		if printed > 0 {
			out.WriteString("\n")
		}
		renderReport(&out, report, now)
		printed++
	}

	// All agents were missing AND no real errors fired: tell the user once,
	// rather than letting them stare at an empty stdout and wonder what happened.
	if printed == 0 && !explicit && missing == len(agents) {
		fmt.Fprintln(os.Stderr, "No supported agents are installed (none of Claude Code, Codex, or Grok was detected).")
		fmt.Fprintln(os.Stderr, "Run `claude` / `grok login` to authenticate, or run a Codex CLI session first.")
		exitCode = 1
	}

	fmt.Print(out.String())
	return exitCode
}

// notInstalledMessage returns the user-facing string when --agent X is explicit
// and X has no detectable installation footprint on this machine.
func notInstalledMessage(agent string) string {
	switch agent {
	case "claude":
		return "Claude Code is not installed or not logged in. Run `claude` to log in, or set CLAUDE_CODE_OAUTH_TOKEN."
	case "codex":
		return "Codex is not installed (no sessions under ~/.codex/sessions). Run a Codex CLI session first."
	case "grok":
		return "Grok CLI is not installed or not logged in (no ~/.grok/auth.json). Run `grok login`, or set GROK_OAUTH_TOKEN."
	default:
		return fmt.Sprintf("%s is not installed.", agent)
	}
}

func fetchReport(ctx context.Context, agent string) (Report, error) {
	switch agent {
	case "claude":
		return fetchClaudeReport(ctx)
	case "codex":
		return fetchCodexReport()
	case "grok":
		return fetchGrokReport(ctx)
	default:
		return Report{}, fmt.Errorf("unsupported agent %q", agent)
	}
}

func resolveAgents(arg string) ([]string, error) {
	arg = strings.TrimSpace(strings.ToLower(arg))
	switch arg {
	case "", "all":
		return []string{"claude", "codex", "grok"}, nil
	case "claude", "codex", "grok":
		return []string{arg}, nil
	default:
		return nil, fmt.Errorf("unsupported agent %q (use claude, codex, grok, or all)", arg)
	}
}
