package compact

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/chatops"
)

var styleSavings = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1")).Bold(true)

// printSummaryTable groups candidates by (agent, project) with per-group
// totals. Matches the prune layout visually so both commands feel related.
func printSummaryTable(candidates []Candidate) {
	type key struct {
		agent   string
		project string
	}
	type bucket struct {
		count  int
		before int64
		after  int64
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
		b.before += c.SizeBefore
		b.after += c.SizeAfter
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

	t := chatops.NewTable().Headers("AGENT", "PROJECT", "FILES", "BEFORE", "AFTER", "SAVED")
	var totBefore, totAfter int64
	for _, k := range keys {
		b := buckets[k]
		totBefore += b.before
		totAfter += b.after
		t.Row(
			chatops.StyleAgent.Render(k.agent),
			k.project,
			chatops.StyleCount.Render(fmt.Sprintf("%d", b.count)),
			chatops.StyleMuted.Render(chatops.HumanBytes(b.before)),
			chatops.StyleMuted.Render(chatops.HumanBytes(b.after)),
			styleSavings.Render(chatops.HumanBytes(b.before-b.after)),
		)
	}

	fmt.Println(t)
	fmt.Println(chatops.StyleFooter.Render(fmt.Sprintf(
		"Total: %d file(s) — %s → %s (%s reclaimed).",
		len(candidates),
		chatops.HumanBytes(totBefore),
		chatops.HumanBytes(totAfter),
		chatops.HumanBytes(totBefore-totAfter),
	)))
}

func printVerboseTable(candidates []Candidate) {
	t := chatops.NewTable().Headers("AGENT", "BEFORE", "AFTER", "SAVED", "PROJECT", "FILE")
	var totBefore, totAfter int64
	for _, c := range candidates {
		totBefore += c.SizeBefore
		totAfter += c.SizeAfter
		saved := c.SizeBefore - c.SizeAfter
		t.Row(
			chatops.StyleAgent.Render(c.Session.Agent),
			chatops.StyleMuted.Render(chatops.HumanBytes(c.SizeBefore)),
			chatops.StyleMuted.Render(chatops.HumanBytes(c.SizeAfter)),
			styleSavings.Render(chatops.HumanBytes(saved)),
			displayProject(c.Session.CWD),
			chatops.StyleMuted.Render(filepath.Base(c.Session.JSONLPath)),
		)
	}
	fmt.Println(t)
	fmt.Println(chatops.StyleFooter.Render(fmt.Sprintf(
		"Total: %d file(s) — %s → %s (%s reclaimed).",
		len(candidates),
		chatops.HumanBytes(totBefore),
		chatops.HumanBytes(totAfter),
		chatops.HumanBytes(totBefore-totAfter),
	)))
}

func displayProject(cwd string) string {
	if cwd == "" {
		return chatops.StyleMuted.Render("(unknown)")
	}
	return cwd
}

func printNothingToCompact() {
	chatops.PrintZenBox(
		"✽  Nothing to shrink  ✽",
		"No session files matched the filters, or every match was already\nsmaller than the minimum size threshold.",
		"“Fewer bytes, fewer worries.”",
	)
}

func printDestructiveDisclaimer() {
	chatops.PrintDestructiveDisclaimer("IN-PLACE FILE REWRITE", strings.Join([]string{
		"You are about to REWRITE chat session files in place.",
		"",
		"Tool outputs, thinking blocks, and embedded images larger than the",
		"threshold will be replaced with short markers. The conversation text,",
		"message graph, and tool call metadata are preserved so sessions stay",
		"resumable — but any truncated payload CANNOT be recovered unless",
		"you restore from the .bak sidecar (on by default).",
		"",
		"Each rewrite is validated: the line count before and after must match,",
		"otherwise the original file is left untouched.",
		"",
		"Proceed ONLY if you accept that truncated tool outputs are gone from",
		"your local transcripts. The authors of lazyagent provide NO WARRANTY.",
	}, "\n"))
}
