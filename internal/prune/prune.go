// Package prune implements the `lazyagent prune` subcommand, which deletes
// old or orphaned chat sessions from supported coding agents.
//
// Supported agents (v1): claude, pi, codex. Amp is skipped because local
// thread files are re-synced from the remote. Cursor and OpenCode store
// sessions inside SQLite databases owned by third-party apps; deleting rows
// there is deferred to a future version.
package prune

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/chatops"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// SupportedAgents lists the agent names that prune can safely clean up.
var SupportedAgents = []string{"claude", "pi", "codex"}

// activeWindow skips files touched very recently (a live session might be
// writing to them). Anything younger than this is silently excluded.
const activeWindow = 5 * time.Minute

type options struct {
	days       int
	daysSet    bool
	orphaned   bool
	agentsArg  string
	dryRun     bool
	dryVerbose bool
	yes        bool
}

// Run parses the prune subcommand args and executes the command.
// Returns a process exit code.
func Run(args []string) int {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var opts options
	fs.IntVar(&opts.days, "days", 0, "Delete sessions whose last activity is older than N days")
	fs.BoolVar(&opts.orphaned, "orphaned", false, "Delete sessions whose project folder no longer exists")
	fs.StringVar(&opts.agentsArg, "agent", "", "Comma-separated list of agents (claude,pi,codex). If empty an interactive picker is shown")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "Print a per-project count table and exit without deleting")
	fs.BoolVar(&opts.dryVerbose, "dry-run-verbose", false, "Print one line per file that would be deleted and exit without deleting")
	fs.BoolVar(&opts.yes, "yes", false, "Skip confirmation prompt")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `lazyagent prune — delete old or orphaned chat sessions

Usage:
  lazyagent prune --days N [flags]
  lazyagent prune --orphaned [flags]
  lazyagent prune --days N --orphaned [flags]

At least one of --days or --orphaned is required.

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Supported agents: %s
`, strings.Join(SupportedAgents, ", "))
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	// `--days` is only considered explicitly set when the user passed a value > 0.
	opts.daysSet = opts.days > 0

	if !opts.daysSet && !opts.orphaned {
		fmt.Fprintln(os.Stderr, "Error: --days N or --orphaned (or both) is required")
		fs.Usage()
		return 2
	}
	if opts.dryRun && opts.dryVerbose {
		fmt.Fprintln(os.Stderr, "Error: --dry-run and --dry-run-verbose are mutually exclusive")
		return 2
	}

	agents, err := resolveAgents(opts.agentsArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	if len(agents) == 0 {
		fmt.Fprintln(os.Stderr, "No agents selected — nothing to do.")
		return 0
	}

	candidates, discoverErrs := collectCandidates(agents, opts)
	for _, e := range discoverErrs {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", e)
	}

	if len(candidates) == 0 {
		printNothingToPrune()
		return 0
	}

	if opts.dryVerbose {
		printVerboseTable(candidates)
		return 0
	}
	if opts.dryRun {
		printSummaryTable(candidates)
		return 0
	}

	// Real delete: show summary, disclaimer, ask for confirmation, then delete.
	printSummaryTable(candidates)
	if !opts.yes {
		printDestructiveDisclaimer()
		if !chatops.Confirm(fmt.Sprintf("Delete %d session(s)?", len(candidates))) {
			fmt.Println("Aborted.")
			return 0
		}
	}

	deleted, failed := executeDelete(candidates)
	fmt.Printf("\nDeleted %d session(s).\n", deleted)
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "%d deletion(s) failed.\n", failed)
		return 1
	}
	return 0
}

// resolveAgents returns the list of agent names to operate on. When the CLI
// flag is empty, it launches the interactive picker (tty required).
func resolveAgents(arg string) ([]string, error) {
	if arg == "" {
		return pickAgents()
	}
	requested := strings.Split(arg, ",")
	out := make([]string, 0, len(requested))
	seen := make(map[string]bool)
	for _, a := range requested {
		a = strings.TrimSpace(strings.ToLower(a))
		if a == "" {
			continue
		}
		if !isSupported(a) {
			return nil, fmt.Errorf("unsupported agent %q (supported: %s)", a, strings.Join(SupportedAgents, ", "))
		}
		if seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out, nil
}

func isSupported(a string) bool {
	for _, s := range SupportedAgents {
		if s == a {
			return true
		}
	}
	return false
}

// Candidate is a session flagged for deletion.
type Candidate struct {
	Session *model.Session
	Reasons []string // "orphaned", "old"
}

// collectCandidates discovers sessions for each agent and applies the filters.
// Returns candidates and any per-agent discovery errors encountered.
func collectCandidates(agents []string, opts options) ([]Candidate, []error) {
	cfg := core.LoadConfig()
	cutoff := time.Now().Add(-time.Duration(opts.days) * 24 * time.Hour)
	activeCutoff := time.Now().Add(-activeWindow)

	var all []Candidate
	var errs []error

	for _, agent := range agents {
		provider := core.BuildProvider(agent, cfg)
		sessions, err := provider.DiscoverSessions()
		if err != nil {
			errs = append(errs, fmt.Errorf("discover %s sessions: %w", agent, err))
			continue
		}
		for _, s := range sessions {
			if s == nil {
				continue
			}
			// Skip sub-agents: their JSONL lives inside the parent's folder and
			// deleting them directly would corrupt the parent session's transcript.
			if s.IsSidechain {
				continue
			}
			// Never touch a file that was written within the last few minutes.
			if !s.LastActivity.IsZero() && s.LastActivity.After(activeCutoff) {
				continue
			}

			var reasons []string
			if opts.orphaned && isOrphan(s.CWD) {
				reasons = append(reasons, "orphaned")
			}
			if opts.daysSet && !s.LastActivity.IsZero() && s.LastActivity.Before(cutoff) {
				reasons = append(reasons, "old")
			}
			if len(reasons) == 0 {
				continue
			}
			all = append(all, Candidate{Session: s, Reasons: reasons})
		}
	}

	sort.Slice(all, func(i, j int) bool {
		a, b := all[i].Session, all[j].Session
		if a.Agent != b.Agent {
			return a.Agent < b.Agent
		}
		if a.CWD != b.CWD {
			return a.CWD < b.CWD
		}
		return a.LastActivity.Before(b.LastActivity)
	})
	return all, errs
}

// isOrphan returns true when cwd is empty or does not resolve to an existing
// directory on disk. An empty CWD is treated as orphaned because the session
// cannot be matched back to a project anyway.
func isOrphan(cwd string) bool {
	if cwd == "" {
		return true
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return os.IsNotExist(err)
	}
	return !info.IsDir()
}

