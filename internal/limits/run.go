// Package limits implements the `lazyagent limits` subcommand: a one-shot
// summary of the user's Claude Code, Codex, Grok, Kimi, and Cursor rate-limit /
// billing windows. The --detailed view also includes a "pace" indicator that
// compares actual consumption to a perfectly linear consumption rate.
//
// IMPORTANT (Claude): the source for Claude is /api/oauth/usage on
// api.anthropic.com — the same endpoint Claude Code's own `/status` calls.
// As of this writing Anthropic does not document it publicly. lazyagent
// queries it on explicit user invocation only (no polling). Behavior may
// change without notice; failures degrade gracefully.
//
// IMPORTANT (Codex): the source for Codex is /backend-api/wham/usage on
// chatgpt.com — the same endpoint the Codex CLI's TUI polls (~every 60s) to
// render its rate-limit display. It is read live with the ChatGPT OAuth token
// from ~/.codex/auth.json. Same caveats as Claude: on-demand only, undocumented,
// fail gracefully. This replaces the older approach of reading session rollouts,
// which lagged behind the live figures the CLI shows.
//
// IMPORTANT (Grok): the source for Grok is /v1/billing on
// cli-chat-proxy.grok.com — the same endpoint the Grok CLI's `/usage show`
// slash command calls. As of this writing xAI does not document it publicly.
// Same caveats as Claude: on-demand only, fail gracefully.
//
// IMPORTANT (Kimi): the source for Kimi is /coding/v1/usages on api.kimi.com —
// the same endpoint Kimi Code CLI's `/status` slash command calls. lazyagent
// uses the current access token as-is and does not refresh OAuth credentials.
//
// IMPORTANT (Cursor): the source for Cursor is /api/usage-summary on cursor.com —
// the same endpoint the Cursor dashboard uses to render its usage headline. It is
// read with the session token from Cursor's local state.vscdb. Cursor meters two
// disjoint pools against separate allowances, so it is reported as two rows:
// "Cursor Models" (the Auto/Composer pool, autoPercentUsed) and "Cursor API" (the
// usage-based pool, apiPercentUsed). Same caveats as the others: on-demand only,
// undocumented, fail gracefully.
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

// errAgentUnavailable means the agent IS installed and authenticated, but we
// can't produce a usable report right now for a reason worth telling the user
// about (e.g. Cursor reports the account as unlimited, or has no enabled usage
// plan). Unlike a hard error it's skipped silently in `--agent all` mode so
// one agent's quirk never breaks the aggregate command; in explicit single-agent
// mode the wrapped message is shown so the user knows how to fix it.
var errAgentUnavailable = errors.New("agent unavailable")

type options struct {
	agent    string
	detailed bool
	json     bool
}

// agentReport pairs a report with the agent id it was fetched for. The Report
// itself only carries the display name ("Cursor Models"), and JSON consumers
// need the stable id ("cursor") to key on.
type agentReport struct {
	agent  string
	report Report
}

// Error kinds recorded by collect. They double as the "kind" value in the JSON
// output, so they are stable strings rather than an enum.
const (
	errKindNotInstalled = "not_installed"
	errKindUnavailable  = "unavailable"
	errKindError        = "error"
)

// agentError records why one agent produced no report.
type agentError struct {
	agent string
	kind  string
	err   error
}

// message returns the user-facing text for the error, matching what the
// human output prints on stderr for the same case.
func (e agentError) message() string {
	if e.kind == errKindNotInstalled {
		return notInstalledMessage(e.agent)
	}
	return e.err.Error()
}

// Run is the entry point invoked by main.go for `lazyagent limits ...`.
func Run(args []string) int {
	fs := flag.NewFlagSet("limits", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var opts options
	fs.StringVar(&opts.agent, "agent", "all", "Which agent to query: claude, codex, grok, kimi, cursor, all")
	fs.BoolVar(&opts.detailed, "detailed", false, "Show the detailed per-window report with bars, reset times, sources, and notes")
	fs.BoolVar(&opts.json, "json", false, "Print every window, pace, and per-agent error as JSON (for scripts and widgets)")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `lazyagent limits — show rate-limit usage

Usage:
  lazyagent limits                  Show a summary table for Claude Code, Codex, Grok, Kimi, and Cursor
  lazyagent limits --detailed       Show detailed per-window reports with pace and reset times
  lazyagent limits --json           Print the full report as JSON (windows, pace, reset times, errors)
  lazyagent limits --agent claude   Show only Claude Code limits
  lazyagent limits --agent codex    Show only Codex limits
  lazyagent limits --agent grok     Show only Grok limits
  lazyagent limits --agent kimi     Show only Kimi Code limits
  lazyagent limits --agent cursor   Show only Cursor limits (Models + API pools)

Summary output:
  The default table shows used % and expected % for the 5-hour window and the
  weekly/global window. Expected % is the linear pace for elapsed window time.
  Missing windows are shown as --.

JSON output:
  --json prints one object with "reports" (every window with used %, expected %,
  pace, severity, and reset time) and "errors" (agents that produced no report,
  each tagged not_installed, unavailable, or error). Nothing else is written to
  stdout, and --detailed is redundant because the JSON is always complete.

Detailed output explains:
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
  Codex   reads its ChatGPT OAuth token from, in order:
            1. CODEX_OAUTH_TOKEN env var
            2. ~/.codex/auth.json
          If none is found, run `+"`codex`"+` to log in.
  Grok    reads its OAuth token from, in order:
            1. GROK_OAUTH_TOKEN env var
            2. ~/.grok/auth.json
          If none is found, run `+"`grok login`"+`.
  Kimi    reads its OAuth token from, in order:
            1. KIMI_CODE_OAUTH_TOKEN env var
            2. ~/.kimi-code/credentials/kimi-code.json
          If none is found, run `+"`kimi login`"+`.
  Cursor  reads its session token from Cursor's local state.vscdb.
          If none is found, open Cursor and sign in. Cursor reports its
          Auto/Composer and usage-based API pools as separate percentages,
          shown as two rows.

Disclaimer (Claude, Codex, Grok, Kimi, Cursor):
  These providers expose their usage through undocumented endpoints used by
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
	explicit := len(agents) == 1
	results, errs := collect(ctx, agents)
	exitCode := exitCodeFor(results, errs, len(agents), explicit)

	if opts.json {
		if err := writeJSON(os.Stdout, results, errs, now); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return exitCode
	}

	// Missing and unavailable agents are only worth a message when the user
	// asked for that agent by name. In `all` mode they are skipped silently,
	// and a single combined message covers the case where nothing was found.
	for _, ae := range errs {
		switch ae.kind {
		case errKindNotInstalled, errKindUnavailable:
			if explicit {
				fmt.Fprintln(os.Stderr, ae.message())
			}
		default:
			fmt.Fprintf(os.Stderr, "Error (%s): %v\n", ae.agent, ae.err)
		}
	}
	if allMissing(results, errs, len(agents), explicit) {
		fmt.Fprintln(os.Stderr, "No supported agents are installed (none of Claude Code, Codex, Grok, Kimi, or Cursor was detected).")
		fmt.Fprintln(os.Stderr, "Run `claude` / `codex` / `grok login` / `kimi login`, or sign in to Cursor.")
	}

	reports := make([]Report, 0, len(results))
	for _, ar := range results {
		reports = append(reports, ar.report)
	}

	var out strings.Builder
	if len(reports) > 0 {
		if opts.detailed {
			for i, report := range reports {
				if i > 0 {
					out.WriteString("\n")
				}
				renderReport(&out, report, now)
			}
		} else {
			renderSummaryTable(&out, reports, now)
		}
	}

	fmt.Print(out.String())
	return exitCode
}

// collect fetches every requested agent in order and classifies each failure.
// It never prints; the caller decides what each error kind means for its
// output format.
func collect(ctx context.Context, agents []string) ([]agentReport, []agentError) {
	var results []agentReport
	var errs []agentError
	for _, a := range agents {
		rs, err := fetchReports(ctx, a)
		if err != nil {
			kind := errKindError
			switch {
			case errors.Is(err, errAgentNotInstalled):
				kind = errKindNotInstalled
			case errors.Is(err, errAgentUnavailable):
				kind = errKindUnavailable
			}
			errs = append(errs, agentError{agent: a, kind: kind, err: err})
			continue
		}
		for _, r := range rs {
			results = append(results, agentReport{agent: a, report: r})
		}
	}
	return results, errs
}

// allMissing reports the `--agent all` case where every agent was skipped as
// not installed or unavailable and nothing else went wrong.
func allMissing(results []agentReport, errs []agentError, requested int, explicit bool) bool {
	if explicit || len(results) > 0 {
		return false
	}
	missing := 0
	for _, ae := range errs {
		if ae.kind == errKindNotInstalled || ae.kind == errKindUnavailable {
			missing++
		}
	}
	return missing == requested
}

// exitCodeFor applies the documented exit-code rules, shared by the human and
// JSON outputs: 1 when any agent failed outright, when an explicitly requested
// agent is missing or unavailable, or when `--agent all` found nothing at all.
func exitCodeFor(results []agentReport, errs []agentError, requested int, explicit bool) int {
	for _, ae := range errs {
		if ae.kind == errKindError || explicit {
			return 1
		}
	}
	if allMissing(results, errs, requested, explicit) {
		return 1
	}
	return 0
}

// notInstalledMessage returns the user-facing string when --agent X is explicit
// and X has no detectable installation footprint on this machine.
func notInstalledMessage(agent string) string {
	switch agent {
	case "claude":
		return "Claude Code is not installed or not logged in. Run `claude` to log in, or set CLAUDE_CODE_OAUTH_TOKEN."
	case "codex":
		return "Codex is not installed or not logged in (no ~/.codex/auth.json). Run `codex` to log in, or set CODEX_OAUTH_TOKEN."
	case "grok":
		return "Grok CLI is not installed or not logged in (no ~/.grok/auth.json). Run `grok login`, or set GROK_OAUTH_TOKEN."
	case "kimi":
		return "Kimi Code CLI is not installed or not logged in (no ~/.kimi-code/credentials/kimi-code.json). Run `kimi login`, or set KIMI_CODE_OAUTH_TOKEN."
	case "cursor":
		return "Cursor is not installed or not logged in (no token in state.vscdb). Open Cursor and sign in."
	default:
		return fmt.Sprintf("%s is not installed.", agent)
	}
}

// fetchReports dispatches to one agent's fetcher. It returns a slice because a
// single agent can meter more than one independent pool — Cursor reports its
// Auto/Composer and usage-based API allowances as two separate rows.
func fetchReports(ctx context.Context, agent string) ([]Report, error) {
	switch agent {
	case "claude":
		return single(fetchClaudeReport(ctx))
	case "codex":
		return single(fetchCodexReport(ctx))
	case "grok":
		return single(fetchGrokReport(ctx))
	case "kimi":
		return single(fetchKimiReport(ctx))
	case "cursor":
		return fetchCursorReports(ctx)
	default:
		return nil, fmt.Errorf("unsupported agent %q", agent)
	}
}

// single adapts a one-report fetcher to the dispatcher's slice signature.
func single(r Report, err error) ([]Report, error) {
	if err != nil {
		return nil, err
	}
	return []Report{r}, nil
}

func resolveAgents(arg string) ([]string, error) {
	arg = strings.TrimSpace(strings.ToLower(arg))
	switch arg {
	case "", "all":
		return []string{"claude", "codex", "grok", "kimi", "cursor"}, nil
	case "claude", "codex", "grok", "kimi", "cursor":
		return []string{arg}, nil
	default:
		return nil, fmt.Errorf("unsupported agent %q (use claude, codex, grok, kimi, cursor, or all)", arg)
	}
}
