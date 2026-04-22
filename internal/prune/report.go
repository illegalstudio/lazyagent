package prune

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/chatops"
)

var reasonStyles = map[string]lipgloss.Style{
	"orphaned": lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8")),
	"old":      lipgloss.NewStyle().Foreground(lipgloss.Color("#FAB387")),
}

// printSummaryTable groups candidates by (agent, project) and prints one row
// per group with count and oldest/newest activity.
func printSummaryTable(candidates []Candidate) {
	type key struct {
		agent   string
		project string
	}
	type bucket struct {
		count  int
		oldest time.Time
		newest time.Time
	}

	buckets := make(map[key]*bucket)
	for _, c := range candidates {
		k := key{agent: c.Session.Agent, project: displayProject(c.Session.CWD)}
		b, ok := buckets[k]
		if !ok {
			b = &bucket{}
			buckets[k] = b
		}
		b.count++
		la := c.Session.LastActivity
		if b.oldest.IsZero() || la.Before(b.oldest) {
			b.oldest = la
		}
		if la.After(b.newest) {
			b.newest = la
		}
	}

	keys := make([]key, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].agent != keys[j].agent {
			return keys[i].agent < keys[j].agent
		}
		return keys[i].project < keys[j].project
	})

	t := chatops.NewTable().Headers("AGENT", "PROJECT", "COUNT", "OLDEST", "NEWEST")
	for _, k := range keys {
		b := buckets[k]
		t.Row(
			chatops.StyleAgent.Render(k.agent),
			k.project,
			chatops.StyleCount.Render(fmt.Sprintf("%d", b.count)),
			chatops.StyleMuted.Render(formatWhen(b.oldest)),
			chatops.StyleMuted.Render(formatWhen(b.newest)),
		)
	}

	fmt.Println(t)
	fmt.Println(chatops.StyleFooter.Render(fmt.Sprintf(
		"Total: %d session(s) across %d project group(s) — %s on disk.",
		len(candidates), len(buckets), chatops.HumanBytes(totalBytes(candidates)),
	)))
}

// printVerboseTable prints one row per file.
func printVerboseTable(candidates []Candidate) {
	t := chatops.NewTable().Headers("AGENT", "LAST ACTIVITY", "REASON", "PROJECT", "FILE")
	for _, c := range candidates {
		t.Row(
			chatops.StyleAgent.Render(c.Session.Agent),
			chatops.StyleMuted.Render(formatWhen(c.Session.LastActivity)),
			renderReasons(c.Reasons),
			displayProject(c.Session.CWD),
			chatops.StyleMuted.Render(filepath.Base(c.Session.JSONLPath)),
		)
	}

	fmt.Println(t)
	fmt.Println(chatops.StyleFooter.Render(fmt.Sprintf(
		"Total: %d session(s) — %s on disk.",
		len(candidates), chatops.HumanBytes(totalBytes(candidates)),
	)))
}

// totalBytes stats each candidate's JSONL file and sums the sizes. Missing
// or unreadable files contribute zero — we prefer an underestimate to
// failing the report.
func totalBytes(candidates []Candidate) int64 {
	var total int64
	for _, c := range candidates {
		if c.Session.JSONLPath == "" {
			continue
		}
		info, err := os.Stat(c.Session.JSONLPath)
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}

func renderReasons(reasons []string) string {
	parts := make([]string, 0, len(reasons))
	for _, r := range reasons {
		if s, ok := reasonStyles[r]; ok {
			parts = append(parts, s.Render(r))
		} else {
			parts = append(parts, r)
		}
	}
	return strings.Join(parts, "+")
}

func displayProject(cwd string) string {
	if cwd == "" {
		return chatops.StyleMuted.Render("(unknown)")
	}
	return cwd
}

func formatWhen(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// printNothingToPrune replaces the empty-state with a friendly green panel.
func printNothingToPrune() {
	chatops.PrintZenBox(
		"✽  All clean  ✽",
		"No chat sessions matched your filters.\nYour agent history is already tidy.",
		"“The fewer files, the freer the mind.”",
	)
}

// printDestructiveDisclaimer prints the red/orange disclaimer before the
// final y/N confirmation.
func printDestructiveDisclaimer() {
	chatops.PrintDestructiveDisclaimer("DESTRUCTIVE OPERATION", strings.Join([]string{
		"You are about to PERMANENTLY DELETE chat session files from your disk.",
		"",
		"This action CANNOT be undone. Once these files are removed, the",
		"conversations they contain — including prompts, code, and context —",
		"are gone for good, regardless of any sync or cloud backup the agent",
		"may have provided.",
		"",
		"Proceed ONLY if you have carefully reviewed the list above, you know",
		"exactly which sessions are being removed, and you accept full",
		"responsibility for any resulting data loss. The authors of lazyagent",
		"provide NO WARRANTY and assume NO LIABILITY for deleted content.",
		"",
		"If you are unsure, cancel now and re-run with --dry-run-verbose to",
		"inspect every file that would be affected.",
	}, "\n"))
}
