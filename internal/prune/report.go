package prune

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/mattn/go-isatty"
)

var (
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#B4BEFE"))
	styleAgent  = lipgloss.NewStyle().Foreground(lipgloss.Color("#89B4FA"))
	styleCount  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F9E2AF")).Bold(true)
	styleMuted  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C7086"))
	styleBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("#45475A"))
	styleFooter = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#94A3B8"))

	danger = lipgloss.Color("#F38BA8")

	styleDisclaimerBox = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder()).
				BorderForeground(danger).
				Foreground(lipgloss.Color("#FAB387")).
				Padding(1, 2).
				Margin(1, 0)

	styleDisclaimerTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(danger).
				Underline(true)

	zen = lipgloss.Color("#A6E3A1")

	styleZenBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(zen).
			Foreground(zen).
			Padding(1, 3).
			Margin(1, 0).
			Align(lipgloss.Center)

	styleZenSub = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("#94E2D5"))

	reasonStyles = map[string]lipgloss.Style{
		"orphaned": lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8")),
		"old":      lipgloss.NewStyle().Foreground(lipgloss.Color("#FAB387")),
	}
)

// plain is true when output is not a terminal (pipe, redirect) — in that
// case lipgloss still degrades colors, but we also drop borders for a
// cleaner piped output.
var plain = !isatty.IsTerminal(os.Stdout.Fd())

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

	t := newTable().Headers("AGENT", "PROJECT", "COUNT", "OLDEST", "NEWEST")
	for _, k := range keys {
		b := buckets[k]
		t.Row(
			styleAgent.Render(k.agent),
			k.project,
			styleCount.Render(fmt.Sprintf("%d", b.count)),
			styleMuted.Render(formatWhen(b.oldest)),
			styleMuted.Render(formatWhen(b.newest)),
		)
	}

	fmt.Println(t)
	fmt.Println(styleFooter.Render(fmt.Sprintf("Total: %d session(s) across %d project group(s) — %s on disk.",
		len(candidates), len(buckets), humanBytes(totalBytes(candidates)))))
}

// printVerboseTable prints one row per file.
func printVerboseTable(candidates []Candidate) {
	t := newTable().Headers("AGENT", "LAST ACTIVITY", "REASON", "PROJECT", "FILE")
	for _, c := range candidates {
		t.Row(
			styleAgent.Render(c.Session.Agent),
			styleMuted.Render(formatWhen(c.Session.LastActivity)),
			renderReasons(c.Reasons),
			displayProject(c.Session.CWD),
			styleMuted.Render(filepath.Base(c.Session.JSONLPath)),
		)
	}

	fmt.Println(t)
	fmt.Println(styleFooter.Render(fmt.Sprintf("Total: %d session(s) — %s on disk.",
		len(candidates), humanBytes(totalBytes(candidates)))))
}

// totalBytes stats each candidate's JSONL file and sums the sizes. Missing or
// unreadable files contribute zero — we prefer an underestimate over failing.
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

// printNothingToPrune prints a friendly green box when no session matches the
// filters — there is literally nothing to do.
func printNothingToPrune() {
	title := lipgloss.NewStyle().Bold(true).Foreground(zen).Render("✽  All clean  ✽")
	body := "No chat sessions matched your filters.\nYour agent history is already tidy."
	sub := styleZenSub.Render("“The fewer files, the freer the mind.”")
	fmt.Println(styleZenBox.Render(title + "\n\n" + body + "\n\n" + sub))
}

// printDestructiveDisclaimer prints a prominent red/orange warning box before
// the user is asked to confirm a real deletion.
func printDestructiveDisclaimer() {
	title := styleDisclaimerTitle.Render("⚠  DESTRUCTIVE OPERATION  ⚠")
	body := strings.Join([]string{
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
	}, "\n")
	fmt.Fprintln(os.Stderr, styleDisclaimerBox.Render(title+"\n\n"+body))
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	suffix := "KMGTPE"[exp]
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), suffix)
}

func newTable() *table.Table {
	border := lipgloss.RoundedBorder()
	if plain {
		border = lipgloss.HiddenBorder()
	}
	return table.New().
		Border(border).
		BorderStyle(styleBorder).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return styleHeader.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
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
		return styleMuted.Render("(unknown)")
	}
	return cwd
}

func formatWhen(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}
