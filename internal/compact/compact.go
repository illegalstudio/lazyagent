// Package compact implements the `lazyagent compact` subcommand, which
// shrinks chat session files in place by truncating oversized tool results,
// thinking blocks, and image payloads. The conversation text, message
// structure, and tool call metadata are preserved so sessions remain
// resumable with the originating agent.
package compact

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

// supportedAgents lists the agent keys that compact knows how to rewrite.
// Extending this requires adding a Rewriter implementation for the agent's
// JSONL schema.
var supportedAgents = []chatops.Agent{
	{Key: "claude", Label: "Claude Code", Color: "#E7A15E"},
	{Key: "pi", Label: "pi coding agent", Color: "#F38BA8"},
	{Key: "codex", Label: "Codex CLI", Color: "#A6E3A1"},
}

// defaultThresholdBytes is the maximum length (in bytes) of a single JSON
// string value before it gets truncated. Matches the article's default.
const defaultThresholdBytes = 10 * 1024

// activeWindow skips files touched very recently — the originating agent
// could still be writing to them and an in-place rewrite would corrupt state.
const activeWindow = 5 * time.Minute

// minSizeBytes is the default lower bound below which compact skips a file.
// Rewriting a 40 KB JSONL saves nothing meaningful and wastes I/O.
const minSizeBytes = 512 * 1024

type options struct {
	days       int
	daysSet    bool
	minSize    int64
	threshold  int64
	agentsArg  string
	dryRun     bool
	dryVerbose bool
	yes        bool
	backup     bool
}

// Run parses the compact subcommand args and executes the command.
// Returns a process exit code.
func Run(args []string) int {
	fs := flag.NewFlagSet("compact", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var opts options
	var threshold int
	var minSize int
	fs.IntVar(&opts.days, "days", 0, "Only compact sessions whose last activity is older than N days (0 = no age filter)")
	fs.IntVar(&minSize, "min-size-kb", minSizeBytes/1024, "Skip files smaller than this many KiB")
	fs.IntVar(&threshold, "threshold-kb", defaultThresholdBytes/1024, "Truncate JSON string values larger than this many KiB")
	fs.StringVar(&opts.agentsArg, "agent", "", "Comma-separated agent keys (claude,codex). If empty an interactive picker is shown")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "Print a per-file before/after size table, do not modify any file")
	fs.BoolVar(&opts.dryVerbose, "dry-run-verbose", false, "Same as --dry-run with one row per file (no grouping)")
	fs.BoolVar(&opts.yes, "yes", false, "Skip confirmation prompt")
	var noBackup bool
	fs.BoolVar(&noBackup, "no-backup", false, "Skip writing a .bak sidecar before rewriting (default: write it)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `lazyagent compact — shrink chat session files by truncating bulky payloads

Usage:
  lazyagent compact [flags]

Compact rewrites each JSONL line, truncating tool outputs, thinking blocks
and image data above the threshold. The conversation text and structure are
preserved so sessions remain resumable with the originating agent.

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Supported agents: %s
`, strings.Join(agentKeys(), ", "))
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	opts.daysSet = opts.days > 0
	opts.minSize = int64(minSize) * 1024
	opts.threshold = int64(threshold) * 1024
	if opts.threshold <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --threshold-kb must be > 0")
		return 2
	}
	opts.backup = !noBackup
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

	candidates, warnings := collectCandidates(agents, opts)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", w)
	}
	if len(candidates) == 0 {
		printNothingToCompact()
		return 0
	}

	// Simulate the rewrite in memory to estimate savings. Drop anything
	// that wouldn't shrink (JSON key re-ordering can add a handful of bytes
	// when nothing got truncated — showing those as candidates would be
	// actively misleading).
	estimateSizes(candidates, opts.threshold)
	candidates = filterShrinkable(candidates)
	if len(candidates) == 0 {
		printNothingToCompact()
		return 0
	}

	groups := buildSummaryGroups(candidates)

	if opts.dryVerbose {
		printVerboseTable(candidates)
		return 0
	}
	if opts.dryRun {
		printSummaryTable(candidates, groups)
		return 0
	}

	printSummaryTable(candidates, groups)
	if !opts.yes {
		printDestructiveDisclaimer()
		choice := chatops.ConfirmOrPick(
			fmt.Sprintf("Compact %d file(s)? Enter y for all, a row # to target a single project, or N to abort",
				len(candidates)),
			len(groups),
		)
		switch choice.Kind {
		case chatops.ChoiceAbort:
			fmt.Println("Aborted.")
			return 0
		case chatops.ChoiceIndex:
			g := groups[choice.Index-1]
			candidates = filterByGroup(candidates, g)
			fmt.Printf("\nSelected row %d: %s  %s  (%d file(s))\n",
				choice.Index, g.Agent, g.Project, g.Count)
		case chatops.ChoiceAll:
			// proceed with every candidate
		}
	}

	processed, failed, saved := executeCompact(candidates, opts)
	fmt.Printf("\nCompacted %d file(s), reclaimed %s.\n", processed, chatops.HumanBytes(saved))
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "%d file(s) failed.\n", failed)
		return 1
	}
	return 0
}

func agentKeys() []string {
	out := make([]string, len(supportedAgents))
	for i, a := range supportedAgents {
		out[i] = a.Key
	}
	return out
}

func isSupported(key string) bool {
	for _, a := range supportedAgents {
		if a.Key == key {
			return true
		}
	}
	return false
}

// resolveAgents returns the list of agent keys to operate on. An empty --agent
// falls back to the interactive picker.
func resolveAgents(arg string) ([]string, error) {
	if arg == "" {
		return chatops.PickAgents(
			supportedAgents,
			"Select agents to compact",
			"Only agents with JSONL transcripts that can be rewritten are shown.",
		)
	}
	requested := strings.Split(arg, ",")
	seen := make(map[string]bool)
	out := make([]string, 0, len(requested))
	for _, a := range requested {
		a = strings.TrimSpace(strings.ToLower(a))
		if a == "" {
			continue
		}
		if !isSupported(a) {
			return nil, fmt.Errorf("unsupported agent %q (supported: %s)", a, strings.Join(agentKeys(), ", "))
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out, nil
}

// Candidate is a session flagged for compaction.
type Candidate struct {
	Session   *model.Session
	SizeBefore int64
	SizeAfter  int64 // populated by estimateSizes (dry-run) or executeCompact (real)
}

// collectCandidates discovers sessions across the selected agents and
// filters them by size, age, and active-window.
func collectCandidates(agents []string, opts options) ([]Candidate, []error) {
	cfg := core.LoadConfig()
	cutoff := time.Now().Add(-time.Duration(opts.days) * 24 * time.Hour)
	activeCutoff := time.Now().Add(-activeWindow)

	var all []Candidate
	var warnings []error

	for _, agent := range agents {
		provider := core.BuildProvider(agent, cfg)
		sessions, err := provider.DiscoverSessions()
		if err != nil {
			warnings = append(warnings, fmt.Errorf("discover %s sessions: %w", agent, err))
			continue
		}
		for _, s := range sessions {
			if s == nil || s.JSONLPath == "" {
				continue
			}
			// Sub-agents share transcript structure with their parent; leave
			// them alone to avoid breaking the parent session's file.
			if s.IsSidechain {
				continue
			}
			if !s.LastActivity.IsZero() && s.LastActivity.After(activeCutoff) {
				continue
			}
			if opts.daysSet && !s.LastActivity.IsZero() && s.LastActivity.After(cutoff) {
				continue
			}
			info, err := os.Stat(s.JSONLPath)
			if err != nil {
				continue
			}
			if info.Size() < opts.minSize {
				continue
			}
			all = append(all, Candidate{Session: s, SizeBefore: info.Size()})
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].SizeBefore > all[j].SizeBefore
	})
	return all, warnings
}

// filterShrinkable drops candidates that wouldn't shrink at all. A small
// tolerance is applied so negligible post-rewrite growth (due to map key
// re-ordering) doesn't count as "no savings".
func filterShrinkable(candidates []Candidate) []Candidate {
	out := candidates[:0]
	for _, c := range candidates {
		if c.SizeAfter < c.SizeBefore {
			out = append(out, c)
		}
	}
	return out
}
